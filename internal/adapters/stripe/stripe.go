// Package stripe is the Stripe adapter of the bank-data and payment-provider ports (PRD-001 FR-PSP-01, FR-PSP-03,
// FR-PSP-06; p1-payment-providers design D1 to D3, D12): a Stripe account's balance transactions as the statement of a
// money account, its objects by ID, and its signed webhooks, read through the Stripe API at the version pinned below.
//
// Read-only by construction: the client sends GET requests only and refuses any other method before it leaves, and the
// connector is given a restricted key with read permissions only (the manifest says so) [live-verify: which permissions
// the list calls need]. Amounts are Stripe's integer minor units, converted with the currency's own scale and never
// divided as floats (ENG-10): a no-float test holds this package to it. Retrieved from docs.stripe.com on 19 Nov 2026:
// the webhook signature (`t=` and `v1=`, HMAC-SHA256 over "t.body", five minutes' tolerance, several `v1` while a secret
// rolls) and the delivery rules (up to three days of retries, no ordering, deduplicate on the event ID).
package stripe

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
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Base is the API's address (test mode and live mode share it; the key says which) and APIVersion the version Fyniq is
// written against, sent on every request so a change of the account's default never changes what the parser reads.
// [live-verify] the version against the account's own.
const (
	Base       = "https://api.stripe.com"
	APIVersion = "2025-09-30.clover"
)

// ParserVersion is the version of the parser of this package's events, stored with each event (FR-PSP-01).
const ParserVersion = "stripe@1"

// Tolerance is how far a webhook's timestamp may be from the platform's clock: Stripe's libraries' default.
const Tolerance = 5 * time.Minute

// MaxWebhookBody is the largest webhook the receiver reads: Stripe's events are well under it [verify].
const MaxWebhookBody = 512 << 10

// Errors of the adapter.
var (
	ErrSignature = errors.New("stripe: the webhook's signature does not verify")
	ErrMethod    = errors.New("stripe: only GET requests are sent: the connector is read-only")
	ErrWindow    = errors.New("stripe: more objects in the period than one call reads: narrow the period")
)

// Client reads one Stripe account.
type Client struct {
	Base string // Base, or a mock's
	Key  string // a restricted key with read permissions only
	HTTP *http.Client
	Now  func() time.Time
	// History is how far back the first statement read starts when the money account gives no start.
	History time.Duration
}

var (
	_ ports.BankData        = (*Client)(nil)
	_ ports.PaymentProvider = (*Client)(nil)
)

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
	if base == nil || base.Transport == nil {
		base = egress.Public()
	}
	wrapped := *base
	wrapped.Transport = getOnly{next: base.Transport}
	return &wrapped
}

// Name implements ports.PaymentProvider.
func (c *Client) Name() string { return "stripe" }

// get reads path into a decoded document. A 404 is ports.ErrProviderNotFound; any other refusal is an error that never
// carries the body, which may name a customer.
func (c *Client) get(ctx context.Context, path string, q url.Values) (map[string]any, []byte, error) {
	u := strings.TrimSuffix(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Stripe-Version", APIVersion)
	req.Header.Set("Accept", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("stripe: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("stripe: reading the answer: %w", err)
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, nil, ports.ErrProviderNotFound
	case res.StatusCode != http.StatusOK:
		return nil, nil, fmt.Errorf("stripe: %s answered %d", path, res.StatusCode)
	}
	doc, err := decode(body)
	if err != nil {
		return nil, nil, fmt.Errorf("stripe: %s answered what is not its JSON: %w", path, err)
	}
	return doc, body, nil
}

// decode reads a JSON object keeping numbers as their text (json.Number), never as floats (ENG-10).
func decode(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case map[string]any: // an expanded object: its ID
		s, _ := v["id"].(string)
		return s
	}
	return ""
}

// minor reads an integer field of minor units.
func minor(m map[string]any, k string) (int64, error) {
	n, ok := m[k].(json.Number)
	if !ok {
		if m[k] == nil {
			return 0, nil
		}
		return 0, fmt.Errorf("stripe: %s is not a number", k)
	}
	v, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("stripe: %s %q is not whole minor units", k, n.String())
	}
	return v, nil
}

func seconds(m map[string]any, k string) time.Time {
	if n, ok := m[k].(json.Number); ok {
		if v, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
			return time.Unix(v, 0).UTC()
		}
	}
	return time.Time{}
}

// Decimal is an amount of minor units as the exact decimal string of its currency's scale: 1250 SGD cents is "12.50",
// 1250 JPY is "1250", 1250 BHD fils is "1.250" (ENG-10).
func Decimal(units int64, currency string) (string, error) {
	cur, err := money.Lookup(strings.ToUpper(currency))
	if err != nil {
		return "", fmt.Errorf("stripe: currency %q: %w", currency, err)
	}
	return money.FromMinor(units, cur).String(), nil
}

// ------------------------------------------------------------------ bank data

// Accounts implements ports.BankData: the account's balances, one per currency; a money account names one by the
// currency's code.
func (c *Client) Accounts(ctx context.Context) ([]ports.BankDataAccount, error) {
	doc, _, err := c.get(ctx, "/v1/balance", nil)
	if err != nil {
		return nil, err
	}
	var out []ports.BankDataAccount
	seen := map[string]bool{}
	for _, k := range []string{"available", "pending"} {
		list, _ := doc[k].([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			code := strings.ToUpper(str(m, "currency"))
			if code != "" && !seen[code] {
				seen[code] = true
				out = append(out, ports.BankDataAccount{ID: code, Currency: code, Name: "Stripe " + code + " balance"})
			}
		}
	}
	slices.SortFunc(out, func(a, b ports.BankDataAccount) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// MaxStatementWindow is the longest span of balance transactions one Transactions call reads.
const MaxStatementWindow = 31 * 24 * time.Hour

// Transactions implements ports.BankData: the balance transactions of the currency account from cursor (a Unix time in
// seconds; with none, from, or History before now) up to but not including now, at most MaxStatementWindow at once,
// oldest first. Each is a line for its gross, and when it has a fee a second line for the fee, referenced "<id>:fee"
// (design D1). The window's end is the cursor, so the next call starts there and no transaction falls between two.
func (c *Client) Transactions(ctx context.Context, account, cursor string, from time.Time) (ports.BankDataPage, error) {
	now := c.Now().UTC()
	start := now.Add(-c.History)
	if !from.IsZero() {
		start = from.UTC()
	}
	if cursor != "" {
		v, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return ports.BankDataPage{}, fmt.Errorf("stripe: the cursor %q is not a time", cursor)
		}
		start = time.Unix(v, 0).UTC()
	}
	end := now
	if end.Sub(start) > MaxStatementWindow {
		end = start.Add(MaxStatementWindow)
	}
	items, err := c.list(ctx, "/v1/balance_transactions", url.Values{"currency": {strings.ToLower(account)}}, start, end)
	if err != nil {
		return ports.BankDataPage{}, err
	}
	out := ports.BankDataPage{Cursor: strconv.FormatInt(end.Unix(), 10), More: end.Before(now)}
	for _, t := range items {
		lines, err := lines(t)
		if err != nil {
			return ports.BankDataPage{}, err
		}
		out.Transactions = append(out.Transactions, lines...)
	}
	return out, nil
}

// lines are the statement lines of one balance transaction: its gross, and its fee as its own line.
func lines(t map[string]any) ([]ports.BankDataTransaction, error) {
	id, cur := str(t, "id"), strings.ToUpper(str(t, "currency"))
	amount, err := minor(t, "amount")
	if err != nil {
		return nil, err
	}
	fee, err := minor(t, "fee")
	if err != nil {
		return nil, err
	}
	at := seconds(t, "created")
	desc := strings.TrimSpace(strings.Join(nonEmpty(str(t, "reporting_category"), str(t, "source"), str(t, "description")), " "))
	var out []ports.BankDataTransaction
	g, err := Decimal(amount, cur)
	if err != nil {
		return nil, err
	}
	l := ports.BankDataTransaction{ID: id, BookingDate: at, Amount: g, Currency: cur, Counterparty: "Stripe", Description: desc}
	if avail := seconds(t, "available_on"); !avail.IsZero() {
		l.ValueDate = &avail
	}
	out = append(out, l)
	if fee != 0 {
		f, err := Decimal(-fee, cur)
		if err != nil {
			return nil, err
		}
		out = append(out, ports.BankDataTransaction{ID: id + ":fee", BookingDate: at, Amount: f, Currency: cur, Counterparty: "Stripe",
			Description: "Stripe fee " + id})
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

// list reads every object of a list endpoint created in [from, to), through Stripe's pages, and returns them oldest
// first (Stripe answers newest first); ErrWindow when the period holds more than maxObjects.
const maxObjects = 5000

func (c *Client) list(ctx context.Context, path string, q url.Values, from, to time.Time) ([]map[string]any, error) {
	q = cloneValues(q)
	q.Set("created[gte]", strconv.FormatInt(from.Unix(), 10))
	q.Set("created[lt]", strconv.FormatInt(to.Unix(), 10))
	q.Set("limit", "100")
	var all []map[string]any
	for {
		doc, _, err := c.get(ctx, path, q)
		if err != nil {
			return nil, err
		}
		data, _ := doc["data"].([]any)
		for _, e := range data {
			if m, ok := e.(map[string]any); ok {
				all = append(all, m)
			}
		}
		if len(all) > maxObjects {
			return nil, ErrWindow
		}
		more, _ := doc["has_more"].(bool)
		if !more || len(data) == 0 {
			break
		}
		last, _ := data[len(data)-1].(map[string]any)
		q.Set("starting_after", str(last, "id"))
	}
	slices.SortStableFunc(all, func(a, b map[string]any) int {
		if d := seconds(a, "created").Compare(seconds(b, "created")); d != 0 {
			return d
		}
		return strings.Compare(str(a, "id"), str(b, "id"))
	})
	return all, nil
}

func cloneValues(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = slices.Clone(v)
	}
	return out
}

// ------------------------------------------------------------------ the provider's objects

// resources are each kind's collection in the API.
var resources = map[ports.ProviderKind]string{
	ports.ProviderCharge: "charges", ports.ProviderRefund: "refunds", ports.ProviderDispute: "disputes", ports.ProviderPayout: "payouts",
	ports.ProviderInvoice: "invoices", ports.ProviderCreditNote: "credit_notes", ports.ProviderSubscription: "subscriptions",
	ports.ProviderCustomer: "customers", ports.ProviderBalanceTransaction: "balance_transactions",
}

// Object implements ports.PaymentProvider: the object as Stripe has it now, with the objects it belongs to as links.
func (c *Client) Object(ctx context.Context, kind ports.ProviderKind, id string) (ports.ProviderObject, error) {
	res, ok := resources[kind]
	if !ok {
		return ports.ProviderObject{}, fmt.Errorf("stripe: Fyniq reads no %s", kind)
	}
	if id == "" || strings.ContainsAny(id, "/?#% ") {
		return ports.ProviderObject{}, fmt.Errorf("stripe: %q is not a %s ID", id, kind)
	}
	doc, raw, err := c.get(ctx, "/v1/"+res+"/"+id, nil)
	if err != nil {
		return ports.ProviderObject{}, err
	}
	o, err := object(kind, doc, raw)
	if err != nil {
		return o, err
	}
	switch kind {
	case ports.ProviderPayout:
		ids, err := c.payoutContents(ctx, id)
		if err != nil {
			return o, err
		}
		o.Links[ports.ProviderBalanceTransaction] = ids
	case ports.ProviderCharge:
		if len(o.Links[ports.ProviderInvoice]) == 0 {
			if pi := str(doc, "payment_intent"); pi != "" {
				if inv, err := c.invoiceOfPaymentIntent(ctx, pi); err != nil {
					return o, err
				} else if inv != "" {
					o.Links[ports.ProviderInvoice] = []string{inv}
				}
			}
		}
	}
	return o, nil
}

// object reads the neutral view of a Stripe object.
func object(kind ports.ProviderKind, doc map[string]any, raw []byte) (ports.ProviderObject, error) {
	cur := str(doc, "currency")
	o := ports.ProviderObject{Kind: kind, ID: str(doc, "id"), Status: str(doc, "status"), Created: seconds(doc, "created"),
		Currency: strings.ToUpper(cur), Links: map[ports.ProviderKind][]string{}, Raw: raw}
	if kind == ports.ProviderBalanceTransaction {
		return balanceTransaction(doc, raw)
	}
	field := map[ports.ProviderKind]string{ports.ProviderCharge: "amount", ports.ProviderRefund: "amount", ports.ProviderDispute: "amount",
		ports.ProviderPayout: "amount", ports.ProviderInvoice: "total", ports.ProviderCreditNote: "total"}[kind]
	if field != "" && cur != "" {
		if _, ok := doc[field]; ok {
			n, err := minor(doc, field)
			if err != nil {
				return o, err
			}
			if o.Amount, err = Decimal(n, cur); err != nil {
				return o, err
			}
		}
	}
	link := func(to ports.ProviderKind, key string) {
		if v := str(doc, key); v != "" {
			o.Links[to] = append(o.Links[to], v)
		}
	}
	link(ports.ProviderInvoice, "invoice")
	link(ports.ProviderCharge, "charge")
	link(ports.ProviderCustomer, "customer")
	link(ports.ProviderSubscription, "subscription")
	return o, nil
}

// categories are how Stripe's reporting categories are read; any other is "other", which no rule posts by itself.
var categories = map[string]string{"charge": "charge", "refund": "refund", "dispute": "dispute", "dispute_reversal": "dispute_reversal",
	"fee": "fee", "payout": "payout"}

// sourceKind reads what a balance transaction is of from its source's ID prefix.
func sourceKind(id string) ports.ProviderKind {
	switch {
	case strings.HasPrefix(id, "ch_"), strings.HasPrefix(id, "py_"):
		return ports.ProviderCharge
	case strings.HasPrefix(id, "re_"), strings.HasPrefix(id, "pyr_"):
		return ports.ProviderRefund
	case strings.HasPrefix(id, "dp_"), strings.HasPrefix(id, "du_"):
		return ports.ProviderDispute
	case strings.HasPrefix(id, "po_"):
		return ports.ProviderPayout
	}
	return ""
}

// balanceTransaction reads a balance transaction: gross (signed), fee, net, how Fyniq reads its category, and its source.
func balanceTransaction(doc map[string]any, raw []byte) (ports.ProviderObject, error) {
	cur := str(doc, "currency")
	o := ports.ProviderObject{Kind: ports.ProviderBalanceTransaction, ID: str(doc, "id"), Status: str(doc, "status"), Created: seconds(doc, "created"),
		Currency: strings.ToUpper(cur), Type: str(doc, "type"), AvailableOn: seconds(doc, "available_on"), Links: map[ports.ProviderKind][]string{}, Raw: raw}
	cat, ok := categories[str(doc, "reporting_category")]
	if !ok {
		cat = "other"
	}
	o.Category = cat
	for field, dst := range map[string]*string{"amount": &o.Amount, "fee": &o.Fee, "net": &o.Net} {
		n, err := minor(doc, field)
		if err != nil {
			return o, err
		}
		if *dst, err = Decimal(n, cur); err != nil {
			return o, err
		}
	}
	if src := str(doc, "source"); src != "" {
		if k := sourceKind(src); k != "" {
			o.Links[k] = []string{src}
		}
	}
	return o, nil
}

// payoutContents are the IDs of the balance transactions a payout settles: Stripe's own account of it, which exists for
// automatic payouts (FR-PSP-06).
func (c *Client) payoutContents(ctx context.Context, payout string) ([]string, error) {
	q := url.Values{"payout": {payout}, "limit": {"100"}}
	var ids []string
	for {
		doc, _, err := c.get(ctx, "/v1/balance_transactions", q)
		if err != nil {
			return nil, err
		}
		data, _ := doc["data"].([]any)
		for _, e := range data {
			m, _ := e.(map[string]any)
			ids = append(ids, str(m, "id"))
		}
		more, _ := doc["has_more"].(bool)
		if !more || len(data) == 0 {
			break
		}
		last, _ := data[len(data)-1].(map[string]any)
		q.Set("starting_after", str(last, "id"))
		if len(ids) > maxObjects {
			return nil, ErrWindow
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// invoiceOfPaymentIntent finds the invoice a payment settles, as Stripe records it in its invoice payments [live-verify
// against API version 2025-09-30.clover, where a charge no longer names its invoice].
func (c *Client) invoiceOfPaymentIntent(ctx context.Context, pi string) (string, error) {
	doc, _, err := c.get(ctx, "/v1/invoice_payments", url.Values{"payment[type]": {"payment_intent"}, "payment[payment_intent]": {pi}, "limit": {"1"}})
	if err != nil {
		return "", err
	}
	data, _ := doc["data"].([]any)
	if len(data) == 0 {
		return "", nil
	}
	m, _ := data[0].(map[string]any)
	return str(m, "invoice"), nil
}

// List implements ports.PaymentProvider: the period's objects of a kind, oldest first, read whole (ErrWindow when the
// period holds too many: the sweep reads a day at a time).
func (c *Client) List(ctx context.Context, kind ports.ProviderKind, from, to time.Time, _ string) (ports.ProviderPage, error) {
	res, ok := resources[kind]
	if !ok {
		return ports.ProviderPage{}, fmt.Errorf("stripe: Fyniq reads no %s", kind)
	}
	q := url.Values{}
	if kind == ports.ProviderSubscription {
		q.Set("status", "all")
	}
	items, err := c.list(ctx, "/v1/"+res, q, from, to)
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
	return page, nil
}

// Balance implements ports.PaymentProvider: what Stripe holds not yet paid out, per currency: available and pending
// together (FR-PSP-06, check C.2 #50).
func (c *Client) Balance(ctx context.Context) ([]ports.ProviderBalance, error) {
	doc, _, err := c.get(ctx, "/v1/balance", nil)
	if err != nil {
		return nil, err
	}
	totals := map[string]int64{}
	for _, k := range []string{"available", "pending"} {
		list, _ := doc[k].([]any)
		for _, e := range list {
			m, _ := e.(map[string]any)
			n, err := minor(m, "amount")
			if err != nil {
				return nil, err
			}
			totals[strings.ToUpper(str(m, "currency"))] += n
		}
	}
	var out []ports.ProviderBalance
	for code, n := range totals {
		d, err := Decimal(n, code)
		if err != nil {
			return nil, err
		}
		out = append(out, ports.ProviderBalance{Currency: code, Amount: d})
	}
	slices.SortFunc(out, func(a, b ports.ProviderBalance) int { return strings.Compare(a.Currency, b.Currency) })
	return out, nil
}

// ------------------------------------------------------------------ webhooks

// Verify checks a webhook's Stripe-Signature against the endpoint's signing secret, on the raw body, at now: some `v1`
// entry must equal HMAC-SHA256(secret, t + "." + body), compared in constant time, and t must be within Tolerance of now
// either way. Other schemes and the debug entries are ignored (downgrade-safe).
func Verify(secret string, h http.Header, body []byte, now time.Time) error {
	if secret == "" {
		return ErrSignature
	}
	var ts string
	var sigs [][]byte
	for _, part := range strings.Split(h.Get("Stripe-Signature"), ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		switch {
		case !ok:
		case k == "t":
			ts = v
		case k == "v1":
			if b, err := hex.DecodeString(v); err == nil {
				sigs = append(sigs, b)
			}
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	want := mac.Sum(nil)
	matched := false
	for _, s := range sigs {
		if hmac.Equal(s, want) {
			matched = true
		}
	}
	if !matched {
		return ErrSignature
	}
	if d := now.Sub(time.Unix(t, 0)); d > Tolerance || d < -Tolerance {
		return ErrSignature
	}
	return nil
}

// handled are the event types the posting rules act on, each with the kind of object it names; informational are known
// event types that post nothing. An event type in neither is unknown: it is stored and parked for a person (UC-PSP-01).
var handled = map[string]ports.ProviderKind{
	"invoice.finalized": ports.ProviderInvoice, "invoice.paid": ports.ProviderInvoice, "invoice.payment_succeeded": ports.ProviderInvoice,
	"invoice.payment_failed": ports.ProviderInvoice, "invoice.voided": ports.ProviderInvoice, "invoice.marked_uncollectible": ports.ProviderInvoice,
	"credit_note.created": ports.ProviderCreditNote, "credit_note.updated": ports.ProviderCreditNote, "credit_note.voided": ports.ProviderCreditNote,
	"charge.succeeded": ports.ProviderCharge, "charge.refunded": ports.ProviderCharge, "refund.created": ports.ProviderRefund,
	"refund.updated": ports.ProviderRefund, "charge.dispute.created": ports.ProviderDispute, "charge.dispute.closed": ports.ProviderDispute,
	"charge.dispute.funds_withdrawn": ports.ProviderDispute, "charge.dispute.funds_reinstated": ports.ProviderDispute,
	"payout.created": ports.ProviderPayout, "payout.paid": ports.ProviderPayout, "payout.failed": ports.ProviderPayout,
	"payout.canceled": ports.ProviderPayout, "payout.updated": ports.ProviderPayout,
	"customer.subscription.created": ports.ProviderSubscription, "customer.subscription.updated": ports.ProviderSubscription,
	"customer.subscription.deleted": ports.ProviderSubscription, "customer.subscription.paused": ports.ProviderSubscription,
	"customer.subscription.resumed": ports.ProviderSubscription, "customer.created": ports.ProviderCustomer, "customer.updated": ports.ProviderCustomer,
}

var informational = []string{"invoice.created", "invoice.updated", "invoice.finalization_failed", "invoice.upcoming", "charge.failed",
	"charge.updated", "charge.pending", "payment_intent.succeeded", "payment_intent.created", "payment_intent.payment_failed",
	"customer.subscription.trial_will_end", "balance.available"}

// ParseEvent reads a verified webhook: its ID, type and API version, the object it names, and whether its type is known.
func ParseEvent(_ http.Header, body []byte) (ports.ProviderEvent, error) {
	doc, err := decode(body)
	if err != nil {
		return ports.ProviderEvent{}, fmt.Errorf("stripe: the webhook is not JSON: %w", err)
	}
	e := ports.ProviderEvent{ID: str(doc, "id"), Type: str(doc, "type"), APIVersion: str(doc, "api_version")}
	if e.ID == "" || e.Type == "" {
		return e, errors.New("stripe: the webhook has no event ID or type")
	}
	data, _ := doc["data"].(map[string]any)
	obj, _ := data["object"].(map[string]any)
	e.ObjectID = str(obj, "id")
	if kind, ok := handled[e.Type]; ok {
		e.Known, e.Handled, e.Kind = true, true, kind
		// A dispute's or refund's own ID is the object; a charge event's object is the charge.
		return e, nil
	}
	if slices.Contains(informational, e.Type) {
		e.Known = true
	}
	return e, nil
}
