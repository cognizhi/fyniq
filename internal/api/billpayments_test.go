//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// moneyAccount sets e's books up with an SGD current account, once, and returns its ID.
func (e *env) moneyAccount() string {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var id string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		for _, a := range all {
			if a.Code == "1010" {
				id = a.ID.String()
				return nil
			}
		}
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		id = a.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// payment is a payment of amount of a bill from the current account on 20 October.
func (e *env) payment(billID, amount string) map[string]any {
	return map[string]any{"date": "2026-10-20", "money_account_id": e.moneyAccount(), "currency": "SGD", "reference": "FAST 88231",
		"bills": []map[string]any{{"bill_id": billID, "amount": map[string]any{"amount": amount, "currency": "SGD"}}}}
}

// recordedPayment posts a bill and records a payment of it in full as an Accountant; it returns the payment's ID and
// ETag.
func (e *env) recordedPayment() (id, etag string) {
	e.t.Helper()
	b, _ := e.postedBill("10.00", "0.90")
	r := e.call(http.MethodPost, "/v1/bill-payments", e.as(authz.Accountant), e.payment(b, "10.90"))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("recordBillPayment = %d: %s", r.Status, r.Raw)
	}
	return r.str("payment", "id"), r.Header.Get("ETag")
}

// FR-AP-02 and FR-AR-18 through the API (p1-purchases task 3.1): a payment recorded by hand posts at once and settles
// the bill, which shows what is open and its payments; it never settles more than is open; a preview writes nothing; a
// paid bill is not voided until its payments are; and in Founder mode a payment's void is asked for and approved by a
// second person (FM-1), which reopens the bill.
// req: FR-AP-02
// req: FR-AR-18
// req: FM-1
func TestBillPaymentsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	b, _ := e.postedBill("1000.00", "90.00")

	// A part: posted, numbered, and the bill part-paid.
	r := e.call(http.MethodPost, "/v1/bill-payments", ahkow, e.payment(b, "400.00"))
	if r.Status != 201 || r.str("payment", "number") != "PAY-0001" || r.str("payment", "status") != "posted" || r.Header.Get("Location") == "" ||
		r.str("payment", "supplier", "name") != "Atlassian Pty Ltd" || r.str("payment", "money_account", "code") != "1010" ||
		len(r.Body["entry"].(map[string]any)["lines"].([]any)) != 2 || !strings.Contains(r.Raw, `"bill_number":"BILL-0001"`) {
		t.Fatalf("recording a part: %d %s", r.Status, r.Raw)
	}
	first, firstTag := r.str("payment", "id"), r.Header.Get("ETag")
	g := e.call(http.MethodGet, "/v1/bills/"+b, weiling, nil)
	if g.str("states", "payment") != "part_paid" || g.str("open", "amount") != "690.00" || !strings.Contains(g.Raw, `"payment_number":"PAY-0001"`) {
		t.Fatalf("the bill after a part: %s", g.Raw)
	}

	// More than is open is refused; a preview of the rest writes nothing; the rest pays it.
	e.problemIs(e.call(http.MethodPost, "/v1/bill-payments", ahkow, e.payment(b, "700.00")), "recordBillPayment", 422, billpayment.CodeOverpaid)
	if p := e.call(http.MethodPost, "/v1/bill-payments", ahkow, e.payment(b, "690.00"), "Prefer", "return=preview"); p.Status != 200 ||
		p.Header.Get("Preference-Applied") != "return=preview" || len(p.Body["entry"].(map[string]any)["lines"].([]any)) != 2 {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+b, weiling, nil); g.str("open", "amount") != "690.00" {
		t.Fatalf("the preview wrote: %s", g.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/bill-payments", ahkow, e.payment(b, "690.00")); r.Status != 201 || r.str("payment", "number") != "PAY-0002" {
		t.Fatalf("recording the rest: %d %s", r.Status, r.Raw)
	}
	g = e.call(http.MethodGet, "/v1/bills/"+b, weiling, nil)
	if g.str("states", "payment") != "paid" || g.str("open", "amount") != "0.00" || len(g.Body["payments"].([]any)) != 2 {
		t.Fatalf("the bill paid: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+b+"/void-approval", ahkow, map[string]any{"date": "2026-10-25", "reason": "Entered twice"},
		"If-Match", g.Header.Get("ETag")), "requestBillVoid", 409, bill.CodePaid)

	// Founder mode: a payment's void is refused, then asked for and approved; the bill reopens.
	void := map[string]any{"date": "2026-10-21", "reason": "Paid from the wrong account"}
	e.problemIs(e.call(http.MethodPost, "/v1/bill-payments/"+first+"/void", ahkow, void, "If-Match", firstTag), "voidBillPayment", 409, CodePaymentVoidApproval)
	e.problemIs(e.call(http.MethodPost, "/v1/bill-payments/"+first+"/void-approval", ahkow, map[string]any{"date": "2026-10-19", "reason": "Too early"},
		"If-Match", firstTag), "requestBillPaymentVoid", 422, billpayment.CodeVoidDate)
	asked := e.call(http.MethodPost, "/v1/bill-payments/"+first+"/void-approval", ahkow, void, "If-Match", firstTag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectPayment || asked.str("operation") != approval.OpPaymentVoid ||
		!strings.Contains(asked.str("envelope"), `"paid":"400.00 SGD"`) || !strings.Contains(asked.str("summary"), "PAY-0001 to Atlassian Pty Ltd") {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bill-payments/"+first, weiling, nil); g.str("approval_asked") != asked.str("id") || len(g.Body["ledger_impact"].([]any)) != 2 {
		t.Errorf("the payment does not show the request: %s", g.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bill-payments/"+first, weiling, nil); g.str("status") != "reversed" {
		t.Fatalf("the payment after the void: %s", g.Raw)
	}
	g = e.call(http.MethodGet, "/v1/bills/"+b, weiling, nil)
	if g.str("states", "payment") != "part_paid" || g.str("open", "amount") != "400.00" || !strings.Contains(g.Raw, `"voided":true`) {
		t.Fatalf("the bill after the void: %s", g.Raw)
	}

	// The list, all and posted only.
	if l := e.call(http.MethodGet, "/v1/bill-payments", weiling, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Errorf("the payments: %d %s", l.Status, l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/bill-payments?status=posted", weiling, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the posted payments: %s", l.Raw)
	}
}

// FR-AP-06 through the API (p1-purchases task 5.1): a bill of S$1,090.00 dated 14 October, due 13 November by the
// supplier's 30 days, paid S$400.00: at 31 October S$690.00 is current and due within 30 days; at 30 November it is
// 17 days past due (1–30) and overdue; the totals and the cash requirement say the same.
// req: FR-AP-06
func TestPayablesThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	b, _ := e.postedBill("1000.00", "90.00")
	if r := e.call(http.MethodPost, "/v1/bill-payments", e.as(authz.Accountant), e.payment(b, "400.00")); r.Status != 201 {
		t.Fatalf("the payment: %d %s", r.Status, r.Raw)
	}
	g := e.call(http.MethodGet, "/v1/payables?as_at=2026-10-31&within=30", e.as(authz.Auditor), nil)
	if g.Status != 200 || !strings.Contains(g.Raw, `"current":{"amount":"690.00","currency":"SGD"}`) ||
		!strings.Contains(g.Raw, `"due_date":"2026-11-13","open":{"amount":"690.00","currency":"SGD"},"overdue":false`) ||
		!strings.Contains(g.Raw, `"within":{"amount":"690.00","currency":"SGD"}`) {
		t.Fatalf("at 31 October: %d %s", g.Status, g.Raw)
	}
	g = e.call(http.MethodGet, "/v1/payables?as_at=2026-11-30", e.as(authz.Auditor), nil)
	if !strings.Contains(g.Raw, `"days_1_30":{"amount":"690.00","currency":"SGD"}`) || !strings.Contains(g.Raw, `"overdue":true`) ||
		!strings.Contains(g.Raw, `"overdue":{"amount":"690.00","currency":"SGD"}`) || !strings.Contains(g.Raw, `"name":"Atlassian Pty Ltd"`) {
		t.Fatalf("at 30 November: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/payables?as_at=31-10-2026", e.as(authz.Auditor), nil), "getPayables", 422, CodeInvalid)
}

// postedCredit saves and posts a vendor credit of net plus GST as an Accountant and returns its ID.
func (e *env) postedCredit(net, gst string) string {
	e.t.Helper()
	d := e.billDraft(net, gst)
	d["kind"], d["supplier_ref"] = "vendor_credit", "CN-"+net
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("a credit: %d %s", r.Status, r.Raw)
	}
	p := e.call(http.MethodPost, "/v1/bills/"+r.str("id")+"/post", e.as(authz.Accountant), nil, "If-Match", r.Header.Get("ETag"))
	if p.Status != http.StatusOK {
		e.t.Fatalf("posting the credit: %d %s", p.Status, p.Raw)
	}
	return r.str("id")
}

// appliedCredit applies a posted credit of S$10.90 to a posted bill of S$10.90 as an Accountant and returns the
// application's ID and ETag.
func (e *env) appliedCredit() (id, etag string) {
	e.t.Helper()
	b, _ := e.postedBill("10.00", "0.90")
	c := e.postedCredit("10.00", "0.90")
	r := e.call(http.MethodPost, "/v1/credit-applications", e.as(authz.Accountant), map[string]any{"credit_id": c, "bill_id": b, "date": "2026-10-20",
		"amount": map[string]any{"amount": "10.90", "currency": "SGD"}})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("applyCredit = %d: %s", r.Status, r.Raw)
	}
	return r.str("application", "id"), r.Header.Get("ETag")
}

// FR-AP-03 and FR-AR-18 through the API (p1-purchases task 4.1): a vendor credit of S$32.70 applied to a bill of
// S$109.00 settles both by S$32.70 and, in the base currency, posts nothing; the bill lists it among its payments with
// the credit's number, and the credit with the bill's; more than is left is refused; in Founder mode a void is asked for
// and approved by a second person (FM-1), which reopens both.
// req: FR-AP-03
// req: FR-AR-18
// req: FM-1
func TestCreditApplicationsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	b, _ := e.postedBill("100.00", "9.00")
	c := e.postedCredit("30.00", "2.70")
	apply := func(amount string) response {
		return e.call(http.MethodPost, "/v1/credit-applications", ahkow, map[string]any{"credit_id": c, "bill_id": b, "date": "2026-10-20",
			"amount": map[string]any{"amount": amount, "currency": "SGD"}})
	}
	r := apply("32.70")
	if r.Status != 201 || r.Body["entry"] != nil || r.str("application", "credit", "number") != "VC-0001" || r.str("application", "bill", "number") != "BILL-0001" ||
		r.str("application", "realised_fx", "amount") != "0.00" {
		t.Fatalf("applying the credit: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("application", "id"), r.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/bills/"+b, weiling, nil); g.str("states", "payment") != "part_paid" || g.str("open", "amount") != "76.30" ||
		!strings.Contains(g.Raw, `"via":"application","payment_id":"`+id+`","payment_number":"VC-0001"`) {
		t.Fatalf("the bill: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+c, weiling, nil); g.str("states", "payment") != "paid" || g.str("open", "amount") != "0.00" ||
		!strings.Contains(g.Raw, `"payment_number":"BILL-0001"`) {
		t.Fatalf("the credit: %s", g.Raw)
	}
	e.problemIs(apply("0.01"), "applyCredit", 422, credits.CodeTooMuch)

	void := map[string]any{"date": "2026-10-21", "reason": "Applied to the wrong bill"}
	e.problemIs(e.call(http.MethodPost, "/v1/credit-applications/"+id+"/void", ahkow, void, "If-Match", tag), "voidCreditApplication", 409, CodeApplicationVoidApproval)
	asked := e.call(http.MethodPost, "/v1/credit-applications/"+id+"/void-approval", ahkow, void, "If-Match", tag)
	if asked.Status != 202 || asked.str("operation") != approval.OpApplicationVoid || !strings.Contains(asked.str("envelope"), `"applied":"32.70 SGD"`) {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/credit-applications/"+id, weiling, nil); g.str("status") != "reversed" {
		t.Fatalf("the application after the void: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+b, weiling, nil); g.str("open", "amount") != "109.00" || g.str("states", "payment") != "unpaid" {
		t.Fatalf("the bill after the void: %s", g.Raw)
	}
}

// refundedCredit records a refund of a posted credit of S$10.90 in full as an Accountant and returns the refund's ID
// and ETag.
func (e *env) refundedCredit() (id, etag string) {
	e.t.Helper()
	c := e.postedCredit("10.00", "0.90")
	r := e.call(http.MethodPost, "/v1/supplier-refunds", e.as(authz.Accountant), map[string]any{"credit_id": c, "date": "2026-10-22",
		"money_account_id": e.moneyAccount(), "amount": map[string]any{"amount": "10.90", "currency": "SGD"}})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("recordSupplierRefund = %d: %s", r.Status, r.Raw)
	}
	return r.str("refund", "id"), r.Header.Get("ETag")
}

// FR-AP-03 through the API (p1-purchases task 4.1): a supplier pays back S$20.00 of a credit note of S$32.70, recorded
// as a refund (RF-0001) that posts Dr the current account, Cr 2010; the credit lists it and has S$12.70 left; more
// than is left is refused; in Founder mode a void is asked for and approved by a second person, which reopens it.
// req: FR-AP-03
// req: FM-1
func TestSupplierRefundsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	c := e.postedCredit("30.00", "2.70")
	record := func(amount string) response {
		return e.call(http.MethodPost, "/v1/supplier-refunds", ahkow, map[string]any{"credit_id": c, "date": "2026-10-22", "money_account_id": e.moneyAccount(),
			"reference": "FAST 99102", "amount": map[string]any{"amount": amount, "currency": "SGD"}})
	}
	r := record("20.00")
	if r.Status != 201 || r.str("refund", "number") != "RF-0001" || r.str("refund", "credit", "number") != "VC-0001" ||
		len(r.Body["entry"].(map[string]any)["lines"].([]any)) != 2 || r.str("refund", "money_account", "code") != "1010" {
		t.Fatalf("recording a refund: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("refund", "id"), r.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/bills/"+c, weiling, nil); g.str("open", "amount") != "12.70" || !strings.Contains(g.Raw, `"via":"refund","payment_id":"`+id+`","payment_number":"RF-0001"`) {
		t.Fatalf("the credit: %s", g.Raw)
	}
	e.problemIs(record("12.71"), "recordSupplierRefund", 422, credits.CodeTooMuch)

	void := map[string]any{"date": "2026-10-23", "reason": "Bounced"}
	e.problemIs(e.call(http.MethodPost, "/v1/supplier-refunds/"+id+"/void", ahkow, void, "If-Match", tag), "voidSupplierRefund", 409, CodeRefundVoidApproval)
	asked := e.call(http.MethodPost, "/v1/supplier-refunds/"+id+"/void-approval", ahkow, void, "If-Match", tag)
	if asked.Status != 202 || asked.str("operation") != approval.OpRefundVoid || !strings.Contains(asked.str("envelope"), `"refunded":"20.00 SGD"`) {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/supplier-refunds/"+id, weiling, nil); g.str("status") != "reversed" {
		t.Fatalf("the refund after the void: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+c, weiling, nil); g.str("open", "amount") != "32.70" {
		t.Fatalf("the credit after the void: %s", g.Raw)
	}
}
