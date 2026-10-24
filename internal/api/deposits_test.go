//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// depositDraft is a deposit of the customer's into the money account, in SGD; it sets up 2410 with its role once.
func (e *env) depositDraft(kind, amount, code string) map[string]any {
	e.t.Helper()
	customer := e.customer()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		accounts, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			if a.Role == storage.AccountRole(ledger.RoleCustomerDeposits) {
				return nil
			}
		}
		_, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2410", Name: "Customer deposits & retainers", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleCustomerDeposits)})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	d := map[string]any{"customer_id": customer, "kind": kind, "date": "2026-10-20", "money_account_id": e.moneyAccount(), "currency": "SGD",
		"amount": map[string]any{"amount": amount, "currency": "SGD"}, "reference": "FAST 55120"}
	if code != "" {
		d["gst_code"] = code
	}
	return d
}

// recordedDeposit records a security deposit of S$100.00 as an Accountant and returns its ID and ETag.
func (e *env) recordedDeposit() (id, etag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/customer-deposits", e.as(authz.Accountant), e.depositDraft(deposit.KindSecurity, "100.00", ""))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("recordCustomerDeposit = %d: %s", r.Status, r.Raw)
	}
	return r.str("deposit", "id"), r.Header.Get("ETag")
}

// FR-AR-09 through the API (p1-sales task 3.2, part): a part payment of S$1,090.00 recorded by hand posts customer_deposit@1
// at once, numbered DP-0001, showing its base amount, what 2410 holds of it and what is left; a security deposit with a
// GST code is refused; the list and a preview; in Founder mode a void is asked for and approved by a second person (FM-1).
// req: FR-AR-09
// req: FR-INT-01
// req: FM-1
func TestCustomerDepositsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)

	if pv := e.call(http.MethodPost, "/v1/customer-deposits", ahkow, e.depositDraft(deposit.KindPartPayment, "1090.00", "SR"), "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" || pv.str("deposit", "number") != "DP-0001" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}
	r := e.call(http.MethodPost, "/v1/customer-deposits", ahkow, e.depositDraft(deposit.KindPartPayment, "1090.00", "SR"))
	if r.Status != 201 || r.str("deposit", "number") != "DP-0001" || r.str("deposit", "kind") != deposit.KindPartPayment || r.str("entry", "rule_version") != ledger.RuleCustomerDeposit ||
		r.str("deposit", "base", "amount") != "1090.00" || r.str("deposit", "left", "amount") != "1090.00" || r.str("deposit", "customer", "name") != "Acme Analytics Pte Ltd" ||
		r.Header.Get("Location") != "/v1/customer-deposits/"+r.str("deposit", "id") || len(r.Body["deposit"].(map[string]any)["ledger_impact"].([]any)) < 2 {
		t.Fatalf("recording the deposit: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("deposit", "id"), r.Header.Get("ETag")
	if r.Body["deposit"].(map[string]any)["gst_charged"] != false || r.str("deposit", "held", "amount") != "1090.00" {
		t.Errorf("a part payment before any GST registration bears none: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/customer-deposits", ahkow, e.depositDraft(deposit.KindSecurity, "10.00", "SR")), "recordCustomerDeposit", 422, deposit.CodeGSTCode)
	if l := e.call(http.MethodGet, "/v1/customer-deposits", weiling, nil); len(l.Body["items"].([]any)) != 1 || !strings.Contains(l.Raw, `"number":"DP-0001"`) {
		t.Errorf("the list: %s", l.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-deposits/"+id, weiling, nil); g.Status != 200 || g.Header.Get("ETag") != tag || g.str("money_account", "id") == "" {
		t.Errorf("getCustomerDeposit: %d %s", g.Status, g.Raw)
	}

	// In Founder mode a void is asked for and approved by the other founder.
	e.problemIs(e.call(http.MethodPost, "/v1/customer-deposits/"+id+"/void", ahkow, map[string]any{"date": "2026-10-23", "reason": "Recorded twice"}, "If-Match", tag),
		"voidCustomerDeposit", 409, CodeDepositVoidApproval)
	asked := e.call(http.MethodPost, "/v1/customer-deposits/"+id+"/void-approval", ahkow, map[string]any{"date": "2026-10-23", "reason": "Recorded twice"}, "If-Match", tag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectDeposit || asked.str("operation") != approval.OpDepositVoid ||
		!strings.Contains(asked.str("summary"), "DP-0001 from Acme Analytics Pte Ltd") {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-deposits/"+id, ahkow, nil); g.str("approval_asked") != asked.str("id") {
		t.Errorf("the deposit does not show the request: %s", g.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-deposits/"+id, ahkow, nil); g.str("status") != "reversed" {
		t.Errorf("the voided deposit: %s", g.Raw)
	}
}

// FR-AR-09 through the API (p1-sales task 3.2, part; the owner's decision of 24 Oct 2026): an invoice deducts S$500.00
// of a deposit of S$1,090.00; the draft shows the deposit's part and the balance; issuing debits 1100 with the balance
// and leaves S$590.00 of the deposit; a credit note that deducts one is refused.
// req: FR-AR-09
// req: FR-INT-01
func TestInvoicesDeductDepositsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	r := e.call(http.MethodPost, "/v1/customer-deposits", ahkow, e.depositDraft(deposit.KindPartPayment, "1090.00", "SR"))
	if r.Status != 201 {
		t.Fatalf("recording the deposit: %d %s", r.Status, r.Raw)
	}
	dep := r.str("deposit", "id")
	d := e.invoiceDraft("1", "2000.00")
	d["deposits"] = []map[string]any{{"deposit_id": dep, "amount": map[string]any{"amount": "500.00", "currency": "SGD"}}}
	c := e.call(http.MethodPost, "/v1/invoices", ahkow, d)
	if c.Status != 201 || !strings.Contains(c.Raw, `"deposits":[{"deposit_id":"`+dep+`","number":"DP-0001","amount":{"amount":"500.00","currency":"SGD"},"base":{"amount":"500.00","currency":"SGD"},"gst":{"amount":"0.00","currency":"SGD"}}]`) ||
		c.str("balance", "amount") != "1500.00" || c.str("receivable", "amount") != "1500.00" {
		t.Fatalf("a draft deducting the deposit: %d %s", c.Status, c.Raw)
	}
	p := e.call(http.MethodPost, "/v1/invoices/"+c.str("id")+"/issue", ahkow, nil, "If-Match", c.Header.Get("ETag"))
	if p.Status != 200 || p.str("invoice", "receivable", "amount") != "1500.00" || p.str("invoice", "open", "amount") != "1500.00" ||
		!strings.Contains(p.Raw, `"number":"DP-0001"`) || !strings.Contains(p.Raw, `"description":"Deposit DP-0001 deducted"`) {
		t.Fatalf("issuing it: %d %s", p.Status, p.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/customer-deposits/"+dep, ahkow, nil); g.str("left", "amount") != "590.00" || g.str("used", "amount") != "500.00" {
		t.Errorf("the deposit after the invoice: %s", g.Raw)
	}
	cn := e.creditNoteDraft("", "1", "10.00")
	cn["deposits"] = d["deposits"]
	x := e.call(http.MethodPost, "/v1/invoices", ahkow, cn)
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+x.str("id")+"/issue", ahkow, nil, "If-Match", x.Header.Get("ETag")), "issueInvoice", 422, invoice.CodeDeposit)
}
