// Package hitpay is the HitPay adapter of the bank-data and payment-provider ports (PRD-001 FR-PSP-01, FR-PSP-06;
// p1-payment-providers design D1 to D3, D12): a HitPay account's charges, fees and payouts as the statement of a money
// account, its objects by ID, and its signed webhooks, read through the HitPay API (docs.hitpayapp.com, retrieved 19 Nov
// 2026; the extract is contracts/partners/hitpay.yaml).
//
// Read-only by construction: the client sends GET requests only. HitPay's amounts are JSON numbers: they are read from
// the raw JSON text as exact decimals and never as floats (ENG-10). What HitPay's pages leave open is tagged [verify]
// below and left for the live account: which fields make up a charge's fee, whether a refunded charge carries its
// refund's amount, and the balance's reserve.
package hitpay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The API's bases.
const (
	ProductionBase = "https://api.hit-pay.com"
	SandboxBase    = "https://api.sandbox.hit-pay.com"
)

// ParserVersion is the version of the parser of this package's events, stored with each event (FR-PSP-01).
const ParserVersion = "hitpay@1"

// MaxWebhookBody is the largest webhook the receiver reads [verify: HitPay documents no size].
const MaxWebhookBody = 256 << 10

// Window is how many days one Transactions call reads.
const Window = 7

// Errors of the adapter.
var (
	ErrSignature = errors.New("hitpay: the webhook's signature does not verify")
	ErrMethod    = errors.New("hitpay: only GET requests are sent: the connector is read-only")
)

// Client reads one HitPay business.
type Client struct {
	Base    string // ProductionBase or SandboxBase, or a mock's
	Key     string // the business API key
	HTTP    *http.Client
	Now     func() time.Time
	History time.Duration
}

var (
	_ ports.BankData        = (*Client)(nil)
	_ ports.PaymentProvider = (*Client)(nil)
)

type getOnly struct{ next http.RoundTripper }

func (g getOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		return nil, fmt.Errorf("%w: refused %s %s", ErrMethod, r.Method, r.URL.Path)
	}
	return g.next.RoundTrip(r)
}

func (c *Client) client() *http.Client {
	base := c.HTTP
	if base == nil || base.Transport == nil {
		base = egress.Public()
	}
	wrapped := *base
	wrapped.Transport = getOnly{next: base.Transport}
	return &wrapped
}

// Name implements ports.PaymentProvider.
func (c *Client) Name() string { return "hitpay" }

// getBody reads path; a 404 is ports.ErrProviderNotFound, any other refusal an error that never carries the body.
func (c *Client) getBody(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := strings.TrimSuffix(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-BUSINESS-API-KEY", c.Key)
	req.Header.Set("Accept", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("hitpay: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("hitpay: reading the answer: %w", err)
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, ports.ErrProviderNotFound
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("hitpay: %s answered %d", path, res.StatusCode)
	}
	return body, nil
}

// decode reads JSON keeping numbers as their text, never as floats (ENG-10).
func decode(body []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	return dec.Decode(into)
}

type obj = map[string]any

func get(ctx context.Context, c *Client, path string, q url.Values) (obj, []byte, error) {
	body, err := c.getBody(ctx, path, q)
	if err != nil {
		return nil, nil, err
	}
	var doc obj
	if err := decode(body, &doc); err != nil {
		return nil, nil, fmt.Errorf("hitpay: %s answered what is not its JSON: %w", path, err)
	}
	return doc, body, nil
}

func str(m obj, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return ""
}

// amount reads a JSON number field as an exact decimal string in cur's scale; absent or null is "0". A number with more
// decimals than the currency has is refused, never rounded (ENG-10).
func amount(m obj, k string, cur money.Currency) (money.Money, error) {
	n, ok := m[k].(json.Number)
	if !ok {
		return money.Zero(cur), nil
	}
	v, err := money.Parse(n.String(), cur)
	if err != nil {
		return money.Money{}, fmt.Errorf("hitpay: %s %q: %w", k, n.String(), err)
	}
	return v, nil
}

func when(m obj, keys ...string) time.Time {
	for _, k := range keys {
		if s := str(m, k); s != "" {
			for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-07:00", "2006-01-02"} {
				if t, err := time.Parse(layout, s); err == nil {
					return t.UTC()
				}
			}
		}
	}
	return time.Time{}
}

// pages reads every page of a cursor-paginated list in full.
func (c *Client) pages(ctx context.Context, path string, q url.Values) ([]obj, error) {
	q = cloneValues(q)
	q.Set("per_page", "100")
	var all []obj
	for range 200 {
		doc, _, err := get(ctx, c, path, q)
		if err != nil {
			return nil, err
		}
		data, _ := doc["data"].([]any)
		for _, e := range data {
			if m, ok := e.(obj); ok {
				all = append(all, m)
			}
		}
		meta, _ := doc["meta"].(obj)
		next := str(meta, "next_cursor")
		if next == "" || len(data) == 0 {
			return all, nil
		}
		q.Set("cursor", next)
	}
	return nil, errors.New("hitpay: more pages than one call reads: narrow the period")
}

func cloneValues(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = slices.Clone(v)
	}
	return out
}

// ------------------------------------------------------------------ bank data

// Accounts implements ports.BankData: the business's balances, one per currency.
func (c *Client) Accounts(ctx context.Context) ([]ports.BankDataAccount, error) {
	body, err := c.getBody(ctx, "/v1/balances", nil)
	if err != nil {
		return nil, err
	}
	var list []obj
	if err := decode(body, &list); err != nil {
		return nil, fmt.Errorf("hitpay: /v1/balances answered what is not its JSON: %w", err)
	}
	var out []ports.BankDataAccount
	for _, b := range list {
		code := strings.ToUpper(str(b, "currency"))
		if code != "" {
			out = append(out, ports.BankDataAccount{ID: code, Currency: code, Name: "HitPay " + code + " balance"})
		}
	}
	slices.SortFunc(out, func(a, b ports.BankDataAccount) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// Transactions implements ports.BankData: the currency's succeeded charges, each with its fee as a line of its own
// ("<id>:fee"), and its paid payouts, from cursor (a day, YYYY-MM-DD; with none, from, or History before today) for at
// most Window days, oldest first. The cursor is the window's last day, which the next call reads again: a charge on it
// comes twice and its reference drops it on import (FR-BNK-04), so a charge made later that day is not missed.
func (c *Client) Transactions(ctx context.Context, account, cursor string, from time.Time) (ports.BankDataPage, error) {
	cur, err := money.Lookup(strings.ToUpper(account))
	if err != nil {
		return ports.BankDataPage{}, err
	}
	today := dayOf(c.Now())
	start := dayOf(c.Now().Add(-c.History))
	if !from.IsZero() {
		start = dayOf(from)
	}
	if cursor != "" {
		if start, err = time.Parse("2006-01-02", cursor); err != nil {
			return ports.BankDataPage{}, fmt.Errorf("hitpay: the cursor %q is not a day", cursor)
		}
	}
	end := start.AddDate(0, 0, Window-1)
	if end.After(today) {
		end = today
	}
	if start.After(end) {
		start = end
	}
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	charges, err := c.pages(ctx, "/v1/charges", url.Values{"date_from": {day(start)}, "date_to": {day(end)}, "status": {"succeeded"}})
	if err != nil {
		return ports.BankDataPage{}, err
	}
	payouts, err := c.pages(ctx, "/v1/payouts", url.Values{"created_at_from": {day(start)}, "created_at_to": {day(end)}, "status": {"paid"}})
	if err != nil {
		return ports.BankDataPage{}, err
	}
	out := ports.BankDataPage{Cursor: day(end), More: end.Before(today)}
	for _, ch := range charges {
		if !strings.EqualFold(str(ch, "currency"), cur.Code) {
			continue
		}
		ls, err := chargeLines(ch, cur)
		if err != nil {
			return ports.BankDataPage{}, err
		}
		out.Transactions = append(out.Transactions, ls...)
	}
	for _, po := range payouts {
		if !strings.EqualFold(str(po, "currency"), cur.Code) {
			continue
		}
		a, err := amount(po, "amount", cur)
		if err != nil {
			return ports.BankDataPage{}, err
		}
		at := when(po, "arrival_date", "created_at")
		out.Transactions = append(out.Transactions, ports.BankDataTransaction{ID: str(po, "id"), BookingDate: at, Amount: a.Neg().String(),
			Currency: cur.Code, Counterparty: "HitPay", Description: strings.TrimSpace("HitPay payout " + str(po, "reference"))})
	}
	slices.SortStableFunc(out.Transactions, func(a, b ports.BankDataTransaction) int {
		if d := a.BookingDate.Compare(b.BookingDate); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func dayOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// chargeLines are a charge's gross line and its fee line. The fee is the fixed fee and the percentage fee together
// [verify: whether HitPay's admin fee is the customer's or the merchant's, and any GST on fees]; the check that a
// payout nets to zero (C.2 #42) shows a fee that is wrong.
func chargeLines(ch obj, cur money.Currency) ([]ports.BankDataTransaction, error) {
	id := str(ch, "id")
	gross, err := amount(ch, "amount", cur)
	if err != nil {
		return nil, err
	}
	fixed, err := amount(ch, "fixed_fee", cur)
	if err != nil {
		return nil, err
	}
	pct, err := amount(ch, "discount_fee", cur)
	if err != nil {
		return nil, err
	}
	fee, err := fixed.Add(pct)
	if err != nil {
		return nil, err
	}
	at := when(ch, "closed_at", "created_at")
	desc := strings.TrimSpace(strings.Join(nonEmpty("HitPay charge "+id, str(ch, "order_reference_number"), str(ch, "remark")), " "))
	party := ""
	if cu, ok := ch["customer"].(obj); ok {
		party = str(cu, "name")
	}
	out := []ports.BankDataTransaction{{ID: id, BookingDate: at, Amount: gross.String(), Currency: cur.Code, Counterparty: party, Description: desc}}
	if !fee.IsZero() {
		out = append(out, ports.BankDataTransaction{ID: id + ":fee", BookingDate: at, Amount: fee.Neg().String(), Currency: cur.Code,
			Counterparty: "HitPay", Description: "HitPay fee " + id})
	}
	return out, nil
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ------------------------------------------------------------------ the provider's objects

// Object implements ports.PaymentProvider: a charge or a payout as HitPay has it now; a payout's links are the charges
// it settles, by the references of their statement lines. HitPay's recurring-billing objects are not read in P1.
func (c *Client) Object(ctx context.Context, kind ports.ProviderKind, id string) (ports.ProviderObject, error) {
	var path string
	switch kind {
	case ports.ProviderCharge:
		path = "/v1/charges/"
	case ports.ProviderPayout:
		path = "/v1/payouts/"
	default:
		return ports.ProviderObject{}, fmt.Errorf("hitpay: Fyniq reads no %s in P1", kind)
	}
	if id == "" || strings.ContainsAny(id, "/?#% ") {
		return ports.ProviderObject{}, fmt.Errorf("hitpay: %q is not a %s ID", id, kind)
	}
	doc, raw, err := get(ctx, c, path+id, nil)
	if err != nil {
		return ports.ProviderObject{}, err
	}
	o, err := object(kind, doc, raw)
	if err != nil {
		return o, err
	}
	if kind == ports.ProviderPayout {
		charges, err := c.pages(ctx, "/v1/payouts/"+id+"/charges", nil)
		if err != nil {
			return o, err
		}
		for _, ch := range charges {
			o.Links[ports.ProviderBalanceTransaction] = append(o.Links[ports.ProviderBalanceTransaction], str(ch, "id"))
		}
		slices.Sort(o.Links[ports.ProviderBalanceTransaction])
	}
	return o, nil
}

func object(kind ports.ProviderKind, doc obj, raw []byte) (ports.ProviderObject, error) {
	code := strings.ToUpper(str(doc, "currency"))
	o := ports.ProviderObject{Kind: kind, ID: str(doc, "id"), Status: str(doc, "status"), Currency: code,
		Created: when(doc, "created_at"), Links: map[ports.ProviderKind][]string{}, Raw: raw}
	if code != "" {
		cur, err := money.Lookup(code)
		if err != nil {
			return o, fmt.Errorf("hitpay: currency %q: %w", code, err)
		}
		if _, ok := doc["amount"]; ok {
			a, err := amount(doc, "amount", cur)
			if err != nil {
				return o, err
			}
			o.Amount = a.String()
		}
	}
	if pr := str(doc, "payment_request_id"); pr != "" {
		o.Links[ports.ProviderInvoice] = []string{pr}
	}
	return o, nil
}

// List implements ports.PaymentProvider: the period's charges or payouts, oldest first.
func (c *Client) List(ctx context.Context, kind ports.ProviderKind, from, to time.Time, _ string) (ports.ProviderPage, error) {
	if kind == ports.ProviderBalanceTransaction {
		return c.listTransactions(ctx, from, to)
	}
	var items []obj
	var err error
	day := func(t time.Time) string { return dayOf(t).Format("2006-01-02") }
	last := to.Add(-time.Second) // the period ends before to
	switch kind {
	case ports.ProviderCharge:
		items, err = c.pages(ctx, "/v1/charges", url.Values{"date_from": {day(from)}, "date_to": {day(last)}})
	case ports.ProviderPayout:
		items, err = c.pages(ctx, "/v1/payouts", url.Values{"created_at_from": {day(from)}, "created_at_to": {day(last)}})
	default:
		return ports.ProviderPage{}, fmt.Errorf("hitpay: Fyniq reads no %s in P1", kind)
	}
	if err != nil {
		return ports.ProviderPage{}, err
	}
	var page ports.ProviderPage
	for _, m := range items {
		raw, err := json.Marshal(m)
		if err != nil {
			return page, err
		}
		o, err := object(kind, m, raw)
		if err != nil {
			return page, err
		}
		page.Objects = append(page.Objects, o)
	}
	slices.SortStableFunc(page.Objects, func(a, b ports.ProviderObject) int {
		if d := a.Created.Compare(b.Created); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
	return page, nil
}

// listTransactions are the period's succeeded charges, each with its fee and net, and its paid payouts, as HitPay's
// balance transactions (HitPay's own list covers the cards balance only): the references are the IDs the statement lines
// carry.
func (c *Client) listTransactions(ctx context.Context, from, to time.Time) (ports.ProviderPage, error) {
	day := func(t time.Time) string { return dayOf(t).Format("2006-01-02") }
	last := to.Add(-time.Second)
	charges, err := c.pages(ctx, "/v1/charges", url.Values{"date_from": {day(from)}, "date_to": {day(last)}, "status": {"succeeded"}})
	if err != nil {
		return ports.ProviderPage{}, err
	}
	payouts, err := c.pages(ctx, "/v1/payouts", url.Values{"created_at_from": {day(from)}, "created_at_to": {day(last)}, "status": {"paid"}})
	if err != nil {
		return ports.ProviderPage{}, err
	}
	var page ports.ProviderPage
	for _, ch := range charges {
		code := strings.ToUpper(str(ch, "currency"))
		cur, err := money.Lookup(code)
		if err != nil {
			return page, fmt.Errorf("hitpay: currency %q: %w", code, err)
		}
		gross, err := amount(ch, "amount", cur)
		if err != nil {
			return page, err
		}
		fixed, err := amount(ch, "fixed_fee", cur)
		if err != nil {
			return page, err
		}
		pct, err := amount(ch, "discount_fee", cur)
		if err != nil {
			return page, err
		}
		fee, err := fixed.Add(pct)
		if err != nil {
			return page, err
		}
		net, err := gross.Sub(fee)
		if err != nil {
			return page, err
		}
		id := str(ch, "id")
		raw, _ := json.Marshal(ch)
		links := map[ports.ProviderKind][]string{ports.ProviderCharge: {id}}
		if pr := str(ch, "payment_request_id"); pr != "" { // the payment link the charge was made under (FR-PSP-02)
			links[ports.ProviderInvoice] = []string{pr}
		}
		page.Objects = append(page.Objects, ports.ProviderObject{Kind: ports.ProviderBalanceTransaction, ID: id, Status: str(ch, "status"),
			Created: when(ch, "closed_at", "created_at"), Amount: gross.String(), Currency: code, Type: "charge", Category: "charge", Fee: fee.String(),
			Net: net.String(), Links: links, Raw: raw})
	}
	for _, po := range payouts {
		code := strings.ToUpper(str(po, "currency"))
		cur, err := money.Lookup(code)
		if err != nil {
			return page, fmt.Errorf("hitpay: currency %q: %w", code, err)
		}
		a, err := amount(po, "amount", cur)
		if err != nil {
			return page, err
		}
		id := str(po, "id")
		raw, _ := json.Marshal(po)
		page.Objects = append(page.Objects, ports.ProviderObject{Kind: ports.ProviderBalanceTransaction, ID: id, Status: str(po, "status"),
			Created: when(po, "arrival_date", "created_at"), Amount: a.Neg().String(), Currency: code, Type: "payout", Category: "payout", Fee: money.Zero(cur).String(),
			Net: a.Neg().String(), Links: map[ports.ProviderKind][]string{ports.ProviderPayout: {id}}, Raw: raw})
	}
	slices.SortStableFunc(page.Objects, func(a, b ports.ProviderObject) int {
		if d := a.Created.Compare(b.Created); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
	return page, nil
}

// Balance implements ports.PaymentProvider: available, pending and reserve together, per currency [verify that the
// reserve is the merchant's and is not paid out until released].
func (c *Client) Balance(ctx context.Context) ([]ports.ProviderBalance, error) {
	body, err := c.getBody(ctx, "/v1/balances", nil)
	if err != nil {
		return nil, err
	}
	var list []obj
	if err := decode(body, &list); err != nil {
		return nil, fmt.Errorf("hitpay: /v1/balances answered what is not its JSON: %w", err)
	}
	var out []ports.ProviderBalance
	for _, b := range list {
		code := strings.ToUpper(str(b, "currency"))
		cur, err := money.Lookup(code)
		if err != nil {
			return nil, fmt.Errorf("hitpay: currency %q: %w", code, err)
		}
		w, _ := b["wallets"].(obj)
		total := money.Zero(cur)
		for _, k := range []string{"available", "pending", "reserve"} {
			v, err := amount(w, k, cur)
			if err != nil {
				return nil, err
			}
			if total, err = total.Add(v); err != nil {
				return nil, err
			}
		}
		out = append(out, ports.ProviderBalance{Currency: code, Amount: total.String()})
	}
	slices.SortFunc(out, func(a, b ports.ProviderBalance) int { return strings.Compare(a.Currency, b.Currency) })
	return out, nil
}

// ------------------------------------------------------------------ webhooks

// Verify checks a webhook's Hitpay-Signature: HMAC-SHA256 of the raw body under the webhook's salt, in hex, compared in
// constant time. HitPay signs no timestamp, so a replay is bounded only by the deduplication on receipt.
func Verify(salt string, h http.Header, body []byte) error {
	got, err := hex.DecodeString(strings.TrimSpace(h.Get("Hitpay-Signature")))
	if salt == "" || err != nil || len(got) == 0 {
		return ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return ErrSignature
	}
	return nil
}

// handled are the event kinds the rules act on, by the object header; informational ones are known and post nothing.
// HitPay's event body is the object itself, with no event ID or type, so the type comes from the Hitpay-Event-Object and
// Hitpay-Event-Type headers.
var (
	handledObjects = map[string]ports.ProviderKind{"charge": ports.ProviderCharge, "payout": ports.ProviderPayout}
	informational  = []string{"order", "invoice", "transfer", "payment_request", "recurring_billing"}
)

// HandledKinds are the kinds of object the events the adapter handles name: each needs a handler in the processor.
func HandledKinds() []ports.ProviderKind {
	var out []ports.ProviderKind
	for _, k := range handledObjects {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// ParseEvent reads a verified webhook. Its ID is made of the type, the object's ID and a digest of the body, because
// HitPay sends none: a redelivery of a body repeats it, and a changed object does not.
func ParseEvent(h http.Header, body []byte) (ports.ProviderEvent, error) {
	object := strings.ToLower(strings.TrimSpace(h.Get("Hitpay-Event-Object")))
	kind := strings.ToLower(strings.TrimSpace(h.Get("Hitpay-Event-Type")))
	if object == "" || kind == "" {
		return ports.ProviderEvent{}, errors.New("hitpay: the webhook names no object or event type")
	}
	var doc obj
	if err := decode(body, &doc); err != nil {
		return ports.ProviderEvent{}, fmt.Errorf("hitpay: the webhook is not JSON: %w", err)
	}
	objectID := str(doc, "id")
	sum := sha256.Sum256(body)
	e := ports.ProviderEvent{Type: object + "." + kind, ObjectID: objectID, APIVersion: strings.TrimSpace(h.Get("User-Agent"))}
	e.ID = e.Type + ":" + objectID + ":" + hex.EncodeToString(sum[:8])
	if k, ok := handledObjects[object]; ok && objectID != "" {
		e.Known, e.Handled, e.Kind = true, true, k
		return e, nil
	}
	if slices.Contains(informational, object) {
		e.Known = true
	}
	return e, nil
}
