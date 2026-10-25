// Package wise is the Wise adapter of the bank-data port (PRD-001 FR-BNK-18; p1-bank-import-reconcile task 8.3, design
// D11): a business profile's balances and their statements, read through the Wise Platform API (2026Q4; the extract in
// contracts/partners/wise.yaml, retrieved 26 Oct 2026), and Wise's webhooks verified against its published keys.
//
// Read-only by construction: the client sends GET requests only and refuses any other method before it leaves, because
// Wise's documented personal tokens may also create quotes, recipients and transfers, and a read-only token is not in
// Wise's documentation [verify with Wise before relying on the token's own scope]. Amounts are read from the raw JSON
// as exact decimals, never as floats (ENG-12). Balance statements by API are supported for profiles based in
// Singapore, and need no strong customer authentication there (docs.wise.com, retrieved 26 Oct 2026).
package wise

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The API's bases, with the version Fyniq is written against.
const (
	ProductionBase = "https://api.wise.com/2026Q4"
	SandboxBase    = "https://api.wise-sandbox.com/2026Q4"
)

// MaxInterval is the longest statement Wise gives in one request (469 days).
const MaxInterval = 469 * 24 * time.Hour

// Wise's webhook signing keys, as its event-handling guide publishes them (retrieved 26 Oct 2026).
const (
	productionKey = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvO8vXV+JksBzZAY6GhSO
XdoTCfhXaaiZ+qAbtaDBiu2AGkGVpmEygFmWP4Li9m5+Ni85BhVvZOodM9epgW3F
bA5Q1SexvAF1PPjX4JpMstak/QhAgl1qMSqEevL8cmUeTgcMuVWCJmlge9h7B1CS
D4rtlimGZozG39rUBDg6Qt2K+P4wBfLblL0k4C4YUdLnpGYEDIth+i8XsRpFlogx
CAFyH9+knYsDbR43UJ9shtc42Ybd40Afihj8KnYKXzchyQ42aC8aZ/h5hyZ28yVy
Oj3Vos0VdBIs/gAyJ/4yyQFCXYte64I7ssrlbGRaco4nKF3HmaNhxwyKyJafz19e
HwIDAQAB
-----END PUBLIC KEY-----`
	sandboxKey = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwpb91cEYuyJNQepZAVfP
ZIlPZfNUefH+n6w9SW3fykqKu938cR7WadQv87oF2VuT+fDt7kqeRziTmPSUhqPU
ys/V2Q1rlfJuXbE+Gga37t7zwd0egQ+KyOEHQOpcTwKmtZ81ieGHynAQzsn1We3j
wt760MsCPJ7GMT141ByQM+yW1Bx+4SG3IGjXWyqOWrcXsxAvIXkpUD/jK/L958Cg
nZEgz0BSEh0QxYLITnW1lLokSx/dTianWPFEhMC9BgijempgNXHNfcVirg1lPSyg
z7KqoKUN0oHqWLr2U1A+7kqrl6O2nx3CKs1bj1hToT1+p4kcMoHXA7kA+VBLUpEs
VwIDAQAB
-----END PUBLIC KEY-----`
)

// ErrSignature is a webhook whose X-Signature-SHA256 does not verify.
var ErrSignature = errors.New("wise: the webhook's signature does not verify")

// ErrMethod is a request other than GET, which the client never sends.
var ErrMethod = errors.New("wise: only GET requests are sent: the connector is read-only")

// Client reads one business profile's balances.
type Client struct {
	Base    string // ProductionBase or SandboxBase
	Token   string // the profile's API token
	Profile string // the business profile's ID
	History time.Duration
	HTTP    *http.Client
	Now     func() time.Time

	mu         sync.Mutex
	currencies map[string]string // balance ID → currency, from the last balance list
}

var _ ports.BankData = (*Client)(nil)

// getOnly refuses any request but a GET before it leaves.
type getOnly struct{ next http.RoundTripper }

func (g getOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		return nil, fmt.Errorf("%w: refused %s %s", ErrMethod, r.Method, r.URL.Path)
	}
	return g.next.RoundTrip(r)
}

func (c *Client) client() *http.Client {
	base := c.HTTP
	if base == nil {
		base = http.DefaultClient
	}
	next := base.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	return &http.Client{Transport: getOnly{next: next}, Timeout: 30 * time.Second, CheckRedirect: base.CheckRedirect, Jar: base.Jar}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, into any) error {
	u := strings.TrimSuffix(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("wise: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("wise: reading the answer: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("wise: %s answered %d", path, res.StatusCode) // never the body: it may name the account holder
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // amounts are JSON numbers: read their text, never a float (ENG-12)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("wise: %s answered what is not its JSON: %w", path, err)
	}
	return nil
}

type amount struct {
	Value    json.Number `json:"value"`
	Currency string      `json:"currency"`
}

// Accounts implements ports.BankData: the profile's standard balances, one per currency.
func (c *Client) Accounts(ctx context.Context) ([]ports.BankDataAccount, error) {
	var all []struct {
		ID       json.Number `json:"id"`
		Currency string      `json:"currency"`
		Name     *string     `json:"name"`
	}
	if err := c.get(ctx, "/profiles/"+url.PathEscape(c.Profile)+"/balances", url.Values{"types": {"STANDARD"}}, &all); err != nil {
		return nil, err
	}
	out := make([]ports.BankDataAccount, 0, len(all))
	currencies := map[string]string{}
	for _, b := range all {
		name := b.Currency + " balance"
		if b.Name != nil && *b.Name != "" {
			name = *b.Name
		}
		out = append(out, ports.BankDataAccount{ID: b.ID.String(), Currency: b.Currency, Name: name})
		currencies[b.ID.String()] = b.Currency
	}
	c.mu.Lock()
	c.currencies = currencies
	c.mu.Unlock()
	return out, nil
}

func (c *Client) currency(ctx context.Context, balance string) (string, error) {
	c.mu.Lock()
	cur, ok := c.currencies[balance]
	c.mu.Unlock()
	if ok {
		return cur, nil
	}
	if _, err := c.Accounts(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok = c.currencies[balance]; !ok {
		return "", fmt.Errorf("wise: the profile has no standard balance %s", balance)
	}
	return cur, nil
}

// Transactions implements ports.BankData: the balance's statement from cursor (a time; with none, from, or History
// before now when from is zero) to now, at most MaxInterval at once, oldest first. The cursor is the statement's end, so the next starts there; a
// transaction at the boundary comes again, and its reference drops it on import (FR-BNK-04).
func (c *Client) Transactions(ctx context.Context, balance, cursor string, from time.Time) (ports.BankDataPage, error) {
	cur, err := c.currency(ctx, balance)
	if err != nil {
		return ports.BankDataPage{}, err
	}
	now := c.Now().UTC()
	start := now.Add(-c.History)
	if !from.IsZero() {
		start = from.UTC()
	}
	if cursor != "" {
		if start, err = time.Parse(time.RFC3339Nano, cursor); err != nil {
			return ports.BankDataPage{}, fmt.Errorf("wise: the cursor %q is not a time", cursor)
		}
	}
	end := now
	if end.Sub(start) > MaxInterval {
		end = start.Add(MaxInterval)
	}
	var st struct {
		Transactions []struct {
			Date    time.Time `json:"date"`
			Amount  amount    `json:"amount"`
			Details struct {
				Description string `json:"description"`
				SenderName  string `json:"senderName"`
				Merchant    *struct {
					Name string `json:"name"`
				} `json:"merchant"`
			} `json:"details"`
			RunningBalance  amount `json:"runningBalance"`
			ReferenceNumber string `json:"referenceNumber"`
		} `json:"transactions"`
	}
	q := url.Values{"currency": {cur}, "intervalStart": {start.Format(time.RFC3339Nano)}, "intervalEnd": {end.Format(time.RFC3339Nano)}, "type": {"COMPACT"}}
	path := "/profiles/" + url.PathEscape(c.Profile) + "/balance-statements/" + url.PathEscape(balance) + "/statement.json"
	if err := c.get(ctx, path, q, &st); err != nil {
		return ports.BankDataPage{}, err
	}
	out := ports.BankDataPage{Cursor: end.Format(time.RFC3339Nano), More: end.Before(now)}
	for _, t := range st.Transactions {
		party := t.Details.SenderName
		if party == "" && t.Details.Merchant != nil {
			party = t.Details.Merchant.Name
		}
		out.Transactions = append(out.Transactions, ports.BankDataTransaction{ID: t.ReferenceNumber, BookingDate: t.Date, Amount: t.Amount.Value.String(),
			Currency: t.Amount.Currency, Counterparty: party, Description: t.Details.Description, RunningBalance: t.RunningBalance.Value.String()})
	}
	slices.SortStableFunc(out.Transactions, func(a, b ports.BankDataTransaction) int { return a.BookingDate.Compare(b.BookingDate) })
	return out, nil
}

// Verify checks a webhook's X-Signature-SHA256: RSA with SHA-256 over the raw body, PKCS #1 v1.5, Base64, under Wise's
// production or sandbox key (its event-handling guide and reference code, retrieved 26 Oct 2026).
func Verify(sandbox bool, h http.Header, body []byte) error {
	key := productionKey
	if sandbox {
		key = sandboxKey
	}
	block, _ := pem.Decode([]byte(key))
	if block == nil {
		return errors.New("wise: the published key does not read")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	rk, ok := pub.(*rsa.PublicKey)
	if !ok {
		return errors.New("wise: the published key is not RSA")
	}
	sig, err := base64.StdEncoding.DecodeString(h.Get("X-Signature-SHA256"))
	if err != nil || len(sig) == 0 {
		return ErrSignature
	}
	digest := sha256.Sum256(body)
	if rsa.VerifyPKCS1v15(rk, crypto.SHA256, digest[:], sig) != nil {
		return ErrSignature
	}
	return nil
}
