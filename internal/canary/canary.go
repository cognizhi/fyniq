// Package canary is QA-PROD-01's synthetic end-to-end transactions (p1-launch-readiness design D8). Each night, in the
// canary organisation, the canary posts an invoice, the receipt that settles it, a bill, the payment that settles it
// and a journal, through the public API as a client of it (the owner's decision of 16 Nov 2026: the jobs' database role
// writes no documents, and a client proves the whole stack), then reads them back: each document posted for its
// expected total and settled, and the trial balance in balance. Any difference fails the run, and the failed job raises
// the same alert as a failed ledger verification (the heartbeat check, NFR-OBS-02); the nightly ledger and audit-chain
// verifications then cover what it posted. The first run gives the organisation what it needs: the standard chart and
// the canary's customer and supplier. Amounts stay small, under any self-approval threshold, so nothing waits for a
// person; a document that does wait is a failure that names the setting.
package canary

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Job is the platform job that runs the canary (outbox.PlatformSchedules).
const Job = "canary.run"

// The canary's contacts and figures. The bill and the journal are at the organisation's standard accounts.
const (
	Customer = "Fyniq Canary Customer"
	Supplier = "Fyniq Canary Supplier"

	invoiceTotal = "100.00"
	billTotal    = "40.00"
	journalTotal = "5.00"
)

// TokenSource gives the canary client's access token for each run.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// Canary posts and verifies the night's transactions through the API at BaseURL, which serves /v1, with an HTTP client
// from internal/egress that reaches that host alone (SEC-36).
type Canary struct {
	BaseURL string
	Tokens  TokenSource
	HTTP    *http.Client
	Codes   Codes // the accounts it posts to; the standard chart's when empty
}

// Codes are the accounts the canary posts to, by code: the bank its money moves through, the sales of its invoice and
// the cost of its bill and journal.
type Codes struct{ Bank, Sales, Costs string }

// Standard are the standard chart's: 1020 Bank - Operating account, 4010 Subscription revenue and 6900 Miscellaneous
// expenses.
var Standard = Codes{Bank: "1020", Sales: "4010", Costs: "6900"}

// Report is what a run posted: each document's number.
type Report struct {
	Day                                      string
	Invoice, Receipt, Bill, Payment, Journal string
}

// answer is an API response: its status, ETag and body.
type answer struct {
	status int
	etag   string
	body   map[string]any
	raw    string
}

func (a answer) str(path ...string) string {
	var v any = a.body
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = m[p]
	}
	s, _ := v.(string)
	return s
}

func (c Canary) call(ctx context.Context, token, method, path string, body any, ifMatch string) (answer, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return answer{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		k := make([]byte, 16)
		_, _ = rand.Read(k)
		req.Header.Set("Idempotency-Key", "canary-"+hex.EncodeToString(k))
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return answer{}, fmt.Errorf("canary: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return answer{}, err
	}
	a := answer{status: resp.StatusCode, etag: resp.Header.Get("ETag"), raw: string(raw)}
	_ = json.Unmarshal(raw, &a.body)
	return a, nil
}

// want fails unless the answer has the status; 202 is a document waiting for an approval, which the canary never gives.
func want(a answer, status int, what string) error {
	switch {
	case a.status == status:
		return nil
	case a.status == http.StatusAccepted:
		return fmt.Errorf("canary: %s waits for an approval: keep the canary organisation's self-approval threshold above its amounts", what)
	default:
		return fmt.Errorf("canary: %s answered %d: %.300s", what, a.status, a.raw)
	}
}

// Run posts the night's transactions dated day (the organisation's today) and verifies them.
func (c Canary) Run(ctx context.Context, day time.Time) (Report, error) {
	r := Report{Day: day.Format(time.DateOnly)}
	if c.BaseURL == "" || c.Tokens == nil || c.HTTP == nil {
		return r, errors.New("canary: no API, client or HTTP client is configured")
	}
	if c.Codes == (Codes{}) {
		c.Codes = Standard
	}
	bank, sales, costs := c.Codes.Bank, c.Codes.Sales, c.Codes.Costs
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return r, fmt.Errorf("canary: the client's token: %w", err)
	}
	accounts, err := c.accounts(ctx, token, bank, sales, costs)
	if err != nil {
		return r, err
	}
	customer, err := c.contact(ctx, token, Customer, "customer")
	if err != nil {
		return r, err
	}
	supplier, err := c.contact(ctx, token, Supplier, "vendor")
	if err != nil {
		return r, err
	}
	sgd := func(a string) map[string]any { return map[string]any{"amount": a, "currency": "SGD"} }
	date := r.Day

	// The invoice and the receipt that settles it.
	inv, err := c.call(ctx, token, http.MethodPost, "/v1/invoices", map[string]any{"customer_id": customer, "reference": "CANARY " + date,
		"date": date, "currency": "SGD", "lines": []map[string]any{{"account_id": accounts[sales], "description": "Canary sale",
			"gst_code": "SR", "quantity": "1", "unit_price": invoiceTotal}}}, "")
	if err != nil || want(inv, http.StatusCreated, "the invoice") != nil {
		return r, errors.Join(err, want(inv, http.StatusCreated, "the invoice"))
	}
	issued, err := c.call(ctx, token, http.MethodPost, "/v1/invoices/"+inv.str("id")+"/issue", nil, inv.etag)
	if err != nil || want(issued, http.StatusOK, "issuing the invoice") != nil {
		return r, errors.Join(err, want(issued, http.StatusOK, "issuing the invoice"))
	}
	rc, err := c.call(ctx, token, http.MethodPost, "/v1/customer-receipts", map[string]any{"date": date, "money_account_id": accounts[bank],
		"currency": "SGD", "reference": "CANARY " + date, "invoices": []map[string]any{{"invoice_id": inv.str("id"), "amount": sgd(invoiceTotal)}}}, "")
	if err != nil || want(rc, http.StatusCreated, "the receipt") != nil {
		return r, errors.Join(err, want(rc, http.StatusCreated, "the receipt"))
	}
	r.Receipt = rc.str("receipt", "number")

	// The bill and the payment that settles it.
	bill, err := c.call(ctx, token, http.MethodPost, "/v1/bills", map[string]any{"supplier_id": supplier, "supplier_ref": "CANARY " + date,
		"date": date, "currency": "SGD", "lines": []map[string]any{{"account_id": accounts[costs], "description": "Canary purchase",
			"gst_code": "NR", "net": sgd(billTotal), "gst": sgd("0.00")}}}, "")
	if err != nil || want(bill, http.StatusCreated, "the bill") != nil {
		return r, errors.Join(err, want(bill, http.StatusCreated, "the bill"))
	}
	posted, err := c.call(ctx, token, http.MethodPost, "/v1/bills/"+bill.str("id")+"/post", nil, bill.etag)
	if err != nil || want(posted, http.StatusOK, "posting the bill") != nil {
		return r, errors.Join(err, want(posted, http.StatusOK, "posting the bill"))
	}
	pay, err := c.call(ctx, token, http.MethodPost, "/v1/bill-payments", map[string]any{"date": date, "money_account_id": accounts[bank],
		"currency": "SGD", "reference": "CANARY " + date, "bills": []map[string]any{{"bill_id": bill.str("id"), "amount": sgd(billTotal)}}}, "")
	if err != nil || want(pay, http.StatusCreated, "the payment") != nil {
		return r, errors.Join(err, want(pay, http.StatusCreated, "the payment"))
	}
	r.Payment = pay.str("payment", "number")

	// The journal.
	jnl, err := c.call(ctx, token, http.MethodPost, "/v1/journals", map[string]any{"date": date, "memo": "Canary journal " + date,
		"lines": []map[string]any{{"account_id": accounts[costs], "amount": sgd(journalTotal), "description": "Canary"},
			{"account_id": accounts[bank], "amount": sgd("-" + journalTotal)}}}, "")
	if err != nil || want(jnl, http.StatusCreated, "the journal") != nil {
		return r, errors.Join(err, want(jnl, http.StatusCreated, "the journal"))
	}
	jp, err := c.call(ctx, token, http.MethodPost, "/v1/journals/"+jnl.str("id")+"/post", nil, jnl.etag)
	if err != nil || want(jp, http.StatusOK, "posting the journal") != nil {
		return r, errors.Join(err, want(jp, http.StatusOK, "posting the journal"))
	}

	// Read back: each document posted for its total and settled, and the books in balance.
	var problems []error
	check := func(path, what, total, state, settled string) string { // state: the key under "states" that says it is settled
		a, err := c.call(ctx, token, http.MethodGet, path, nil, "")
		switch {
		case err != nil:
			problems = append(problems, err)
		case a.status != http.StatusOK:
			problems = append(problems, fmt.Errorf("canary: reading %s answered %d", what, a.status))
		case a.str("status") != "posted":
			problems = append(problems, fmt.Errorf("canary: %s is %q, not posted", what, a.str("status")))
		case total != "" && a.str("total", "amount") != total:
			problems = append(problems, fmt.Errorf("canary: %s totals %s; it was posted for %s", what, a.str("total", "amount"), total))
		case settled != "" && a.str("states", state) != settled:
			problems = append(problems, fmt.Errorf("canary: %s is %q, not %s", what, a.str("states", state), settled))
		}
		return a.str("number")
	}
	r.Invoice = check("/v1/invoices/"+inv.str("id"), "the invoice", invoiceTotal, "settlement", "paid")
	r.Bill = check("/v1/bills/"+bill.str("id"), "the bill", billTotal, "payment", "paid")
	r.Journal = check("/v1/journals/"+jnl.str("id"), "the journal", "", "", "")
	tb, err := c.call(ctx, token, http.MethodGet, "/v1/reports/statements/tb?to="+date[:7], nil, "")
	switch {
	case err != nil:
		problems = append(problems, err)
	case tb.status != http.StatusOK:
		problems = append(problems, fmt.Errorf("canary: the trial balance answered %d", tb.status))
	case !balanced(tb.body):
		problems = append(problems, errors.New("canary: the trial balance's debits and credits differ"))
	}
	return r, errors.Join(problems...)
}

// balanced reports whether a statement's "balances" check holds: debits equal credits in every column.
func balanced(statement map[string]any) bool {
	checks, _ := statement["checks"].([]any)
	for _, x := range checks {
		if c, ok := x.(map[string]any); ok && c["key"] == "balances" {
			return c["ok"] == true
		}
	}
	return false
}

// accounts are the organisation's accounts by code, the standard chart loaded first when it has none.
func (c Canary) accounts(ctx context.Context, token string, codes ...string) (map[string]string, error) {
	list := func() (map[string]string, error) {
		a, err := c.call(ctx, token, http.MethodGet, "/v1/accounts", nil, "")
		if err != nil {
			return nil, err
		}
		if a.status != http.StatusOK {
			return nil, fmt.Errorf("canary: listing the accounts answered %d", a.status)
		}
		out := map[string]string{}
		items, _ := a.body["items"].([]any)
		for _, x := range items {
			if m, ok := x.(map[string]any); ok {
				code, _ := m["code"].(string)
				id, _ := m["id"].(string)
				out[code] = id
			}
		}
		return out, nil
	}
	out, err := list()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		loaded, err := c.call(ctx, token, http.MethodPost, "/v1/accounts:load-standard-chart", nil, "")
		if err != nil || want(loaded, http.StatusCreated, "loading the standard chart") != nil {
			return nil, errors.Join(err, want(loaded, http.StatusCreated, "loading the standard chart"))
		}
		if out, err = list(); err != nil {
			return nil, err
		}
	}
	for _, code := range codes {
		if out[code] == "" {
			return nil, fmt.Errorf("canary: the organisation has no account %s, which the canary posts to", code)
		}
	}
	return out, nil
}

// contact is the ID of the canary's contact of a role (customer or vendor), made on the first run.
func (c Canary) contact(ctx context.Context, token, name, role string) (string, error) {
	a, err := c.call(ctx, token, http.MethodGet, "/v1/contacts?role="+role+"&limit=100", nil, "")
	if err != nil {
		return "", err
	}
	if a.status != http.StatusOK {
		return "", fmt.Errorf("canary: listing the contacts answered %d", a.status)
	}
	items, _ := a.body["items"].([]any)
	for _, x := range items {
		if m, ok := x.(map[string]any); ok && m["legal_name"] == name {
			id, _ := m["id"].(string)
			return id, nil
		}
	}
	made, err := c.call(ctx, token, http.MethodPost, "/v1/contacts", map[string]any{"legal_name": name, role: true, "currency": "SGD"}, "")
	if err != nil || want(made, http.StatusCreated, "making "+name) != nil {
		return "", errors.Join(err, want(made, http.StatusCreated, "making "+name))
	}
	return made.str("id"), nil
}
