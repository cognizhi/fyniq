//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The short-term cash flow through the API (FR-PLN-09, FR-PLN-05, FR-PLN-08; p1-planning design D7), worked by hand. On
// 2 Nov 2026 the bank account holds 20,000.00 put in on 1 October and three receipts of 100.00. Payment behaviour over
// the last 12 months: Late Payer paid an invoice due 5 October on the 15th (10 days late) and one due 10 October on the
// 14th (4 days), 7 days late on average; Early Payer paid one due 20 October on the 12th, on time. Open:
//   - Late Payer's 100.00 due 10 November, expected 17 November (history, 7 days late);
//   - Early Payer's 100.00 due 20 November, expected then (history, on time);
//   - Quiet's 100.00 due 25 November, with no history (due), and its 100.00 due 25 October, overdue, so today;
//   - Quiet's USD 100.00 invoice at 1.34, carried at 134.00, due 28 November;
//   - Quiet's recurring invoice of 500.00 on the 15th of each month from November: 15 November's falls due 15 December, in
//     the 60 days; 15 December's falls due 14 January, after them;
//   - the supplier's bill of 109.00 in a batch approved to pay on 5 November, not counted again as a bill;
//   - its bill of 218.00 due 13 November (30 days' terms), and its bill of 54.50 planned for 12 November.
//
// The balance closes 20,400.00 today, 20,291.00 on the 5th, 20,236.50 on the 12th, 20,018.50 on the 13th, 20,118.50 on
// the 17th, 20,218.50 on the 20th, 20,318.50 on the 25th, 20,452.50 on the 28th and 20,952.50 from 15 December; the
// lowest is 20,018.50 on the 13th. No month is closed, so there is no buffer line. Then October is closed with 30,000.00
// of rent paid in it: its net burn is 30,000.00 less the 20,300.00 it took in, 9,700.00, so the line is 6 × 9,700.00 =
// 58,200.00, and the balance, 9,700.00 below zero today, is below it from today.
// req: FR-PLN-09
// req: FR-PLN-05
// req: FR-PLN-08
// req: QA-RPT-01
func TestTheCashProjectionThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.clock.Set(time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)) // 09:00 in Singapore
	accountant := f.as(f.ahkow, authz.Accountant)
	e.customer() // the chart's accounts receivable, and a customer with nothing open
	bank := e.cashAccount()
	e.plannedLine(bank.AccountID.String(), e.books.Sales.ID.String(), porttest.Day(10, 1), "20000.00")
	customer := func(name string) string {
		r := e.call(http.MethodPost, "/v1/contacts", accountant, map[string]any{"legal_name": name, "customer": true, "currency": "SGD"})
		if r.Status != http.StatusCreated {
			t.Fatalf("createContact = %d: %s", r.Status, r.Raw)
		}
		return r.str("id")
	}
	invoice := func(customer, date, due string) string {
		t.Helper()
		d := e.invoiceDraft("1", "100.00")
		d["customer_id"], d["date"], d["due_date"] = customer, date, due
		c := e.call(http.MethodPost, "/v1/invoices", accountant, d)
		r := e.call(http.MethodPost, "/v1/invoices/"+c.str("id")+"/issue", accountant, nil, "If-Match", c.Header.Get("ETag"))
		if r.Status != http.StatusOK {
			t.Fatalf("issuing %s: %d %s", date, r.Status, r.Raw)
		}
		return c.str("id")
	}
	receive := func(inv, on string) {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/customer-receipts", accountant, map[string]any{"date": on, "money_account_id": bank.AccountID.String(), "currency": "SGD",
			"reference": "FAST " + on, "invoices": []map[string]any{{"invoice_id": inv, "amount": map[string]any{"amount": "100.00", "currency": "SGD"}}}})
		if r.Status != http.StatusCreated {
			t.Fatalf("recordCustomerReceipt = %d: %s", r.Status, r.Raw)
		}
	}
	late, early, quiet := customer("Late Payer Pte Ltd"), customer("Early Payer Pte Ltd"), customer("Quiet Pte Ltd")
	receive(invoice(late, "2026-10-01", "2026-10-05"), "2026-10-15")
	receive(invoice(late, "2026-10-01", "2026-10-10"), "2026-10-14")
	receive(invoice(early, "2026-10-01", "2026-10-20"), "2026-10-12")
	lateOpen := invoice(late, "2026-10-20", "2026-11-10")
	earlyOpen := invoice(early, "2026-10-25", "2026-11-20")
	quietOpen := invoice(quiet, "2026-10-25", "2026-11-25")
	overdue := invoice(quiet, "2026-10-01", "2026-10-25")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 14), Rate: "1.34", Quote: "1.34",
			QuoteForm: "base-per-unit"})
		return err
	})
	usd := e.invoiceDraft("1", "100.00")
	usd["customer_id"], usd["currency"], usd["due_date"] = quiet, "USD", "2026-11-28"
	u := e.call(http.MethodPost, "/v1/invoices", accountant, usd)
	if r := e.call(http.MethodPost, "/v1/invoices/"+u.str("id")+"/issue", accountant, nil, "If-Match", u.Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("issuing the USD invoice: %d %s", r.Status, r.Raw)
	}
	foreign := u.str("id")
	rec := e.recurringInvoice("Quiet retainer", "500.00", monthly(15, "2026-11-01"))
	rec["customer_id"] = quiet
	ri := e.call(http.MethodPost, "/v1/recurring-invoices", accountant, rec)
	if ri.Status != http.StatusCreated {
		t.Fatalf("createRecurringInvoice = %d: %s", ri.Status, ri.Raw)
	}

	vendor := e.vendor("Northwind Analytics Pte Ltd")
	e.vendorAccount(vendor, "0123456789")
	inBatch, open, planned := e.billOf(vendor, "100.00", "9.00"), e.billOf(vendor, "200.00", "18.00"), e.billOf(vendor, "50.00", "4.50")
	if p := e.call(http.MethodPost, "/v1/bills/"+planned+"/payment-plan", accountant, map[string]any{"planned_on": "2026-11-12"}, "If-Match", "*"); p.Status != http.StatusOK {
		t.Fatalf("planning the bill: %d %s", p.Status, p.Raw)
	}
	body := batchBody(bank, "bill:"+inBatch)
	body["payment_date"] = "2026-11-05"
	b := e.call(http.MethodPost, "/v1/payment-batches", accountant, body)
	asked := e.askBatch(b.str("id"), accountant, b.Header.Get("ETag"), false)
	if d := f.decide(f.as(f.weiling), asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving the batch = %d: %s", d.Status, d.Raw)
	}

	revision := func() int64 {
		var n int64
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) { n, err = u.Ledger().Revision(ctx); return err })
		return n
	}
	before := revision()
	read := func(r response) CashProjection {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("the projection: %d %s", r.Status, r.Raw)
		}
		var p CashProjection
		if err := json.Unmarshal([]byte(r.Raw), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := read(e.call(http.MethodGet, "/v1/cash-projection", e.as(authz.Auditor), nil))
	if p.Today != "2026-11-02" || p.Days != 60 || len(p.Daily) != 60 || p.Opening.Amount != "20300.00" {
		t.Fatalf("the projection: today %s, %d days, opening %s", p.Today, len(p.Daily), p.Opening.Amount)
	}
	by := map[string]CashMovement{}
	for _, m := range p.Movements {
		by[m.ID] = m
	}
	for _, c := range []struct {
		id, kind, due, on, basis, amount string
		days                             int
	}{
		{lateOpen, "invoice", "2026-11-10", "2026-11-17", "history", "100.00", 7},
		{earlyOpen, "invoice", "2026-11-20", "2026-11-20", "history", "100.00", 0},
		{quietOpen, "invoice", "2026-11-25", "2026-11-25", "due", "100.00", 0},
		{overdue, "invoice", "2026-10-25", "2026-11-02", "overdue", "100.00", 0},
		{foreign, "invoice", "2026-11-28", "2026-11-28", "due", "134.00", 0},
		{ri.str("id"), "recurring_invoice", "2026-12-15", "2026-12-15", "due", "500.00", 0},
		{b.str("id"), "payment_batch", "", "2026-11-05", "scheduled", "-109.00", 0},
		{open, "bill", "2026-11-13", "2026-11-13", "due", "-218.00", 0},
		{planned, "bill", "2026-11-13", "2026-11-12", "planned", "-54.50", 0},
	} {
		m, ok := by[c.id]
		if !ok || m.Kind != c.kind || m.DueDate != c.due || m.ExpectedOn != c.on || m.Basis != c.basis || m.Amount.Amount != c.amount || m.DaysLate != c.days {
			t.Errorf("%s %s: %+v", c.kind, c.on, m)
		}
	}
	if _, twice := by[inBatch]; twice || len(p.Movements) != 9 {
		t.Errorf("the batch's bill counted again, or other movements: %+v", p.Movements)
	}
	if m := by[ri.str("id")]; m.Occurrence != "2026-11-15" || m.Key != ri.str("id")+"@2026-11-15" {
		t.Errorf("the recurring occurrence: %+v", m)
	}
	closes := map[string]string{"2026-11-02": "20400.00", "2026-11-05": "20291.00", "2026-11-12": "20236.50", "2026-11-13": "20018.50",
		"2026-11-17": "20118.50", "2026-11-20": "20218.50", "2026-11-25": "20318.50", "2026-11-28": "20452.50", "2026-12-15": "20952.50", "2026-12-31": "20952.50"}
	for _, d := range p.Daily {
		if want, ok := closes[d.Date]; ok && d.Close.Amount != want {
			t.Errorf("%s closes %s, want %s", d.Date, d.Close.Amount, want)
		}
	}
	if p.Lowest.Amount != "20018.50" || p.LowestOn != "2026-11-13" || p.BelowBufferOn != "" || p.Buffer.Line != nil ||
		p.Buffer.Why != "no month is closed yet" || p.Buffer.Months != 6 || len(p.Weekly) != 8 || len(p.NotIncluded) != 3 {
		t.Errorf("lowest %s on %s, buffer %+v, %d weeks, not included %+v", p.Lowest.Amount, p.LowestOn, p.Buffer, len(p.Weekly), p.NotIncluded)
	}
	// The 13 weeks.
	w := read(e.call(http.MethodGet, "/v1/cash-projection?days=91", accountant, nil))
	if len(w.Weekly) != 13 || w.Weekly[0].From != "2026-11-02" || w.Weekly[0].To != "2026-11-08" || w.Weekly[0].In.Amount != "100.00" ||
		w.Weekly[0].Out.Amount != "109.00" || w.Weekly[0].Close.Amount != "20291.00" || w.Weekly[12].To != "2027-01-31" {
		t.Errorf("the weeks: %+v", w.Weekly)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/cash-projection?days=45", accountant, nil), "getCashProjection", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodGet, "/v1/cash-projection", e.as(authz.Bookkeeper), nil), "getCashProjection", 403, CodeForbidden)

	// What if the 218.00 bill is delayed to 30 November and the late payer is chased, paying on the 10th? Nothing is saved.
	wi := read(e.call(http.MethodPost, "/v1/cash-projection:what-if", accountant, map[string]any{"moves": []any{
		map[string]any{"key": open, "to": "2026-11-30"}, map[string]any{"key": lateOpen, "to": "2026-11-10"}}}))
	if wi.Lowest.Amount != "20291.00" || wi.LowestOn != "2026-11-05" {
		t.Errorf("the what-if's lowest: %s on %s", wi.Lowest.Amount, wi.LowestOn)
	}
	for _, m := range wi.Movements {
		if m.ID == open && (m.ExpectedOn != "2026-11-30" || m.Basis != "moved") {
			t.Errorf("the moved bill: %+v", m)
		}
	}
	if r := e.call(http.MethodPost, "/v1/cash-projection:what-if", accountant, map[string]any{"moves": []any{
		map[string]any{"key": b.str("id"), "leave_out": true}}}); read(r).Lowest.Amount != "20127.50" {
		t.Errorf("without the batch: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/cash-projection:what-if", accountant, map[string]any{"moves": []any{map[string]any{"key": "nothing", "leave_out": true}}}),
		"whatIfCashProjection", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/cash-projection:what-if", accountant, map[string]any{"moves": []any{map[string]any{"key": open}}}),
		"whatIfCashProjection", 422, CodeInvalid)
	if after := revision(); after != before {
		t.Errorf("the projection and its what-ifs moved the ledger from revision %d to %d", before, after)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+open, accountant, nil); g.str("planned_payment_on") != "" {
		t.Errorf("a what-if planned the bill: %s", g.Raw)
	}

	// October closes with 30,000.00 of rent paid in it: a buffer line, crossed from today.
	e.plannedLine(e.books.Rent.ID.String(), bank.AccountID.String(), porttest.Day(10, 2), "30000.00")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, e.books.Oct.ID)
		return err
	})
	c := read(e.call(http.MethodGet, "/v1/cash-projection?days=30", accountant, nil))
	if c.Opening.Amount != "-9700.00" || c.Buffer.AverageBurn == nil || c.Buffer.AverageBurn.Amount != "9700.00" || c.Buffer.Line == nil ||
		c.Buffer.Line.Amount != "58200.00" || c.Buffer.Why != "" || c.BelowBufferOn != "2026-11-02" {
		t.Errorf("with October closed: opening %s, buffer %+v, below on %s", c.Opening.Amount, c.Buffer, c.BelowBufferOn)
	}
}

// A company that took in more cash than it used over its closed months has no buffer line, and the projection says why
// (FR-PLN-09, design D7): October, closed, brought in 1,000.00.
// req: FR-PLN-09
func TestTheCashProjectionNotBurning(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC))
	bank := e.cashAccount()
	e.plannedLine(bank.AccountID.String(), e.books.Sales.ID.String(), porttest.Day(10, 1), "1000.00")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, e.books.Oct.ID)
		return err
	})
	r := e.call(http.MethodGet, "/v1/cash-projection?days=30", e.as(authz.Accountant), nil)
	if r.Status != http.StatusOK || r.str("opening", "amount") != "1000.00" || r.str("buffer", "average_burn", "amount") != "-1000.00" ||
		r.Body["buffer"].(map[string]any)["line"] != nil || r.str("buffer", "why") != "the company is not burning cash: the last closed month brought in more cash than it used" ||
		r.str("below_buffer_on") != "" {
		t.Errorf("not burning: %d %s", r.Status, r.Raw)
	}
}

// cashAccount is a new SGD bank account in Banking on the standard chart's "Cash & cash equivalents" line, so the cash
// flow and its net burn read it as cash.
func (e *env) cashAccount() storage.MoneyAccount {
	e.t.Helper()
	var m storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1030", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD", CashFlowLine: "Cash & cash equivalents"}})
		if err != nil {
			return err
		}
		m, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: a.ID, Kind: storage.MoneyBank, Name: a.Name,
			Identifier: "072-1030", Currency: "SGD", At: e.clock.Now()})
		return err
	})
	return m
}

// matrixCashProjection are the authorisation matrix's rows for the cash projection.
var matrixCashProjection = map[string]func(e *env, tok string) (response, func() bool){
	"getCashProjection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/cash-projection?days=30", tok, nil), nil
	},
	"whatIfCashProjection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/cash-projection:what-if", tok, map[string]any{"days": 30, "moves": []any{}}), nil
	},
}
