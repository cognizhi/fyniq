// Package airwallex is the Airwallex adapter of the bank-data port (PRD-001 FR-BNK-18; p1-bank-import-reconcile task 8.4,
// design D11): a wallet's cash balances and their history, read with a scoped API key's access token, and Airwallex's
// webhooks verified with the endpoint's secret. The facts are from Airwallex's API reference and webhook guide,
// retrieved 26 Oct 2026 (contracts/partners/airwallex.yaml is written from them: Airwallex publishes no OpenAPI
// document).
//
// Read-only by construction: the client sends GET requests only, besides the one POST that exchanges the key for a
// token, and refuses any other request before it leaves. Airwallex offers scoped keys: Read on its Balances scope is
// all this needs. Amounts are read from the raw JSON as exact decimals, never as floats (ENG-12).
package airwallex

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The API's bases.
const (
	ProductionBase = "https://api.airwallex.com"
	SandboxBase    = "https://api.sandbox.airwallex.com"
)

const loginPath = "/api/v1/authentication/login"

// Window is the longest balance history read at once: Airwallex pages a range of up to 7 days.
const Window = 7 * 24 * time.Hour

// Tolerance is how old a webhook's timestamp may be (the task's five minutes; Airwallex leaves it to the receiver).
const Tolerance = 5 * time.Minute

// The refusals.
var (
	ErrSignature = errors.New("airwallex: the webhook's signature does not verify")
	ErrStale     = errors.New("airwallex: the webhook's timestamp is outside five minutes")
	ErrMethod    = errors.New("airwallex: only GET requests and the token exchange are sent: the connector is read-only")
)

// Client reads a wallet's cash balances.
type Client struct {
	Base     string
	ClientID string
	APIKey   string
	History  time.Duration
	HTTP     *http.Client
	Now      func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

var _ ports.BankData = (*Client)(nil)

// readOnly refuses any request but a GET, or the POST of the token exchange, before it leaves.
type readOnly struct{ next http.RoundTripper }

func (g readOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet && !(r.Method == http.MethodPost && r.URL.Path == loginPath) {
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
	return &http.Client{Transport: readOnly{next: next}, Timeout: 30 * time.Second, CheckRedirect: base.CheckRedirect, Jar: base.Jar}
}

func (c *Client) do(req *http.Request, path string, ok int, into any) error {
	res, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("airwallex: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("airwallex: reading the answer: %w", err)
	}
	if res.StatusCode != ok {
		return fmt.Errorf("airwallex: %s answered %d", path, res.StatusCode) // never the body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // amounts are JSON numbers: read their text, never a float (ENG-12)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("airwallex: %s answered what is not its JSON: %w", path, err)
	}
	return nil
}

// bearer is a token, exchanged for the key once and reused until a minute before it expires.
func (c *Client) bearer(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	if c.token != "" && now.Before(c.expires.Add(-time.Minute)) {
		return c.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.Base, "/")+loginPath, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("x-client-id", c.ClientID)
	req.Header.Set("x-api-key", c.APIKey)
	var out struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := c.do(req, loginPath, http.StatusCreated, &out); err != nil {
		return "", err
	}
	expires, err := when(out.ExpiresAt)
	if err != nil || out.Token == "" {
		return "", errors.New("airwallex: the token exchange answered no token or expiry")
	}
	c.token, c.expires = out.Token, expires
	return c.token, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, into any) error {
	token, err := c.bearer(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimSuffix(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return c.do(req, path, http.StatusOK, into)
}

// when reads Airwallex's ISO 8601 times, such as 2017-04-01T03:52:34+0000.
func when(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05-0700", "2006-01-02T15:04:05.999999999-0700", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("airwallex: %q is not a time", s)
}

// Accounts implements ports.BankData: one cash balance per currency, its ID the currency.
func (c *Client) Accounts(ctx context.Context) ([]ports.BankDataAccount, error) {
	var all []struct {
		AccountType string `json:"account_type"`
		Currency    string `json:"currency"`
	}
	if err := c.get(ctx, "/api/v1/balances/current", url.Values{"account_type": {"cash"}}, &all); err != nil {
		return nil, err
	}
	out := make([]ports.BankDataAccount, 0, len(all))
	for _, b := range all {
		if b.AccountType == "" || b.AccountType == "cash" {
			out = append(out, ports.BankDataAccount{ID: b.Currency, Currency: b.Currency, Name: b.Currency + " wallet"})
		}
	}
	return out, nil
}

// cursor is where a currency's history got to: the start of the window being read, and Airwallex's bookmark in it.
type cursor struct {
	From string `json:"from"`
	Page string `json:"page,omitempty"`
}

// Transactions implements ports.BankData: the currency's cash balance history from the cursor (with none, from start,
// or History before now when start is zero), a window of at most
// seven days at a time, a page at a time, oldest first. The cursor is opaque: the window's start and Airwallex's
// bookmark within it; once a window is read, the next starts at its end.
func (c *Client) Transactions(ctx context.Context, currency, raw string, start time.Time) (ports.BankDataPage, error) {
	now := c.Now().UTC()
	at := cursor{From: now.Add(-c.History).Format(time.RFC3339)}
	if !start.IsZero() {
		at.From = start.UTC().Format(time.RFC3339)
	}
	if raw != "" {
		b, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || json.Unmarshal(b, &at) != nil {
			return ports.BankDataPage{}, fmt.Errorf("airwallex: the cursor %q does not read", raw)
		}
	}
	from, err := time.Parse(time.RFC3339, at.From)
	if err != nil {
		return ports.BankDataPage{}, fmt.Errorf("airwallex: the cursor's start %q is not a time", at.From)
	}
	to := from.Add(Window)
	if to.After(now) {
		to = now
	}
	q := url.Values{"currency": {currency}, "account_type": {"cash"}, "from_post_at": {from.Format(time.RFC3339)}, "to_post_at": {to.Format(time.RFC3339)},
		"page_size": {"200"}}
	if at.Page != "" {
		q.Set("page", at.Page)
	}
	var h struct {
		HasMore   bool    `json:"has_more"`
		PageAfter *string `json:"page_after"`
		Items     []struct {
			ID          string      `json:"id"`
			Amount      json.Number `json:"amount"`
			Balance     json.Number `json:"balance"`
			Currency    string      `json:"currency"`
			Description string      `json:"description"`
			PostedAt    string      `json:"posted_at"`
			Reference   string      `json:"reference"`
			SourceType  string      `json:"source_type"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/api/v1/balances/history", q, &h); err != nil {
		return ports.BankDataPage{}, err
	}
	out := ports.BankDataPage{}
	for _, it := range h.Items {
		posted, err := when(it.PostedAt)
		if err != nil {
			return ports.BankDataPage{}, err
		}
		desc := it.Description
		if it.Reference != "" {
			desc = strings.TrimSpace(desc + " " + it.Reference)
		}
		out.Transactions = append(out.Transactions, ports.BankDataTransaction{ID: it.ID, BookingDate: posted, Amount: it.Amount.String(), Currency: it.Currency,
			Description: desc, RunningBalance: it.Balance.String()})
	}
	slices.SortStableFunc(out.Transactions, func(a, b ports.BankDataTransaction) int { return a.BookingDate.Compare(b.BookingDate) })
	next := cursor{From: to.Format(time.RFC3339)}
	if h.HasMore && h.PageAfter != nil && *h.PageAfter != "" {
		next = cursor{From: at.From, Page: *h.PageAfter}
		out.More = true
	} else {
		out.More = to.Before(now)
	}
	b, err := json.Marshal(next)
	if err != nil {
		return ports.BankDataPage{}, err
	}
	out.Cursor = base64.RawURLEncoding.EncodeToString(b)
	return out, nil
}

// Verify checks a webhook: x-signature is the hex HMAC-SHA256, under the endpoint's secret, of x-timestamp (Unix
// milliseconds, as sent) followed by the raw body; the timestamp is within Tolerance of now.
func Verify(secret string, h http.Header, body []byte, now time.Time) error {
	ts, sig := h.Get("x-timestamp"), h.Get("x-signature")
	if ts == "" || sig == "" || secret == "" {
		return ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(sig))) != 1 {
		return ErrSignature
	}
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrSignature
	}
	if d := now.Sub(time.UnixMilli(ms)); d > Tolerance || d < -Tolerance {
		return ErrStale
	}
	return nil
}
