//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// supplier sets e's books up for bills, once: the accounts payable, GST control and FX accounts by role, and a
// supplier with 30 days' terms. It returns the supplier's ID.
func (e *env) supplier() string {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var id string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		cs, err := u.Contacts().Page(ctx, uuid.Nil, "vendor", 10)
		if err != nil {
			return err
		}
		if len(cs) > 0 {
			id = cs[0].ID.String()
			return nil
		}
		for _, a := range []storage.NewAccount{
			{Code: "2010", Name: "Accounts payable", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleAccountsPayable)},
			{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)},
			{Code: "7030", Name: "FX & rounding differences", Type: storage.Expense, Role: storage.AccountRole(ledger.RoleFXRounding)},
		} {
			if _, err := u.Accounts().Create(ctx, a); err != nil {
				return err
			}
		}
		thirty := 30
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Atlassian Pty Ltd", Vendor: true, PaymentTermsDays: &thirty})
		id = c.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// billDraft is a bill of the supplier for rent: net plus 9% GST, coded TX.
func (e *env) billDraft(net, gst string) map[string]any {
	return map[string]any{
		"supplier_id": e.supplier(), "supplier_ref": "AU-" + net, "date": "2026-10-14", "currency": "SGD",
		"lines": []map[string]any{{"account_id": e.books.Rent.ID.String(), "description": "Confluence", "gst_code": "TX",
			"net": map[string]any{"amount": net, "currency": "SGD"}, "gst": map[string]any{"amount": gst, "currency": "SGD"}}},
	}
}

// billEdit is an edit of the supplier's rent bill to net plus GST, with a reason.
func (e *env) billEdit(net, gst, reason string) map[string]any {
	d := e.billDraft(net, gst)
	d["reason"] = reason
	return d
}

// createBill saves a draft bill as an Accountant and returns its ID and ETag.
func (e *env) createBill(net, gst string) (id, etag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), e.billDraft(net, gst))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createBill = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// postedBill saves and posts a draft bill as an Accountant and returns its ID and ETag.
func (e *env) postedBill(net, gst string) (id, etag string) {
	e.t.Helper()
	id, tag := e.createBill(net, gst)
	r := e.call(http.MethodPost, "/v1/bills/"+id+"/post", e.as(authz.Accountant), nil, "If-Match", tag)
	if r.Status != http.StatusOK {
		e.t.Fatalf("postBill = %d: %s", r.Status, r.Raw)
	}
	return id, r.Header.Get("ETag")
}

// FR-AP-02 through the API (p1-purchases task 1.5): a draft bill shows its states, what stops it posting and the
// lines posting writes; it posts with its number; above the self-approval threshold it is asked for and approved by
// another person, never by whoever prepared it (FM-2, SOD-3); at or below it a founder posts it alone, logged for the
// weekly review (FM-3); in Founder mode a void is asked for (FM-1).
// req: FR-AP-02
// req: FM-1
// req: FM-2
// req: SOD-3
func TestBillsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	draft := func(who, net, gst string) (string, string) {
		r := e.call(http.MethodPost, "/v1/bills", who, e.billDraft(net, gst))
		if r.Status != http.StatusCreated {
			t.Fatalf("a bill of %s: %d %s", net, r.Status, r.Raw)
		}
		return r.str("id"), r.Header.Get("ETag")
	}

	// A draft: its states, its due date from the supplier's terms, what posting writes (not GST-registered: the GST in
	// the cost), and no checks.
	id, tag := draft(ahkow, "1000.00", "90.00")
	g := e.call(http.MethodGet, "/v1/bills/"+id, weiling, nil)
	if g.Status != 200 || g.str("status") != "draft" || g.str("states", "approval") != "draft" || g.str("states", "ledger") != "not_posted" ||
		g.str("states", "payment") != "unpaid" || g.str("due_date") != "2026-11-13" || g.str("total", "amount") != "1090.00" ||
		g.str("payable", "amount") != "1090.00" || g.str("supplier", "name") != "Atlassian Pty Ltd" || len(g.Body["ledger_impact"].([]any)) != 2 ||
		len(g.Body["checks"].([]any)) != 0 || g.Header.Get("ETag") != tag {
		t.Fatalf("the draft: %d %s", g.Status, g.Raw)
	}

	// At the threshold a founder posts it alone, numbered, and the review log has it.
	low, lowTag := draft(ahkow, "1834.86", "165.14") // S$2,000.00
	p := e.call(http.MethodPost, "/v1/bills/"+low+"/post", ahkow, nil, "If-Match", lowTag)
	if p.Status != 200 || p.str("bill", "number") != "BILL-0001" || p.str("bill", "states", "ledger") != "posted" || len(p.Body["entry"].(map[string]any)["lines"].([]any)) != 2 {
		t.Fatalf("posting S$2,000.00 alone: %d %s", p.Status, p.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/review-log", weiling, nil); !strings.Contains(l.Raw, `"kind":"self_approved","object_type":"bill","object_key":"`+low+`"`) {
		t.Fatalf("the self-approval is not in the review log: %s", l.Raw)
	}

	// Above it: refused, asked for, never approved by the asker or by whoever prepared it, then approved.
	big, bigTag := draft(weiling, "5000.00", "450.00")
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+big+"/post", ahkow, nil, "If-Match", bigTag), "postBill", 409, CodeBillNeedsApproval)
	asked := e.call(http.MethodPost, "/v1/bills/"+big+"/post-approval", ahkow, nil, "If-Match", bigTag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectBill || asked.str("operation") != approval.OpBillPost ||
		!strings.Contains(asked.str("envelope"), `"payable":"5450.00"`) || !strings.Contains(asked.str("summary"), "Atlassian Pty Ltd (AU-5000.00)") {
		t.Fatalf("asking to post: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+big, ahkow, nil); g.str("states", "approval") != "awaiting_approval" || g.str("approval_asked") != asked.str("id") {
		t.Errorf("the bill does not show the request: %s", g.Raw)
	}
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, CodeOwnBill)
	e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/withdraw", ahkow, nil, "If-Match", `"1"`)
	mine, mineTag := draft(ahkow, "3000.00", "270.00")
	asked = e.call(http.MethodPost, "/v1/bills/"+mine+"/post-approval", ahkow, nil, "If-Match", mineTag)
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the post: %d %s", a.Status, a.Raw)
	}
	g = e.call(http.MethodGet, "/v1/bills/"+mine, weiling, nil)
	if g.str("status") != "posted" || g.str("number") != "BILL-0002" || g.str("posted_by", "id") != f.weiling || g.str("payable", "amount") != "3270.00" {
		t.Fatalf("after the approval: %s", g.Raw)
	}

	// Founder mode: a void is refused, then asked for and approved.
	reversal := map[string]any{"date": "2026-10-25", "reason": "Entered twice"}
	posted := g.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+mine+"/void", ahkow, reversal, "If-Match", posted), "voidBill", 409, CodeBillVoidApproval)
	void := e.call(http.MethodPost, "/v1/bills/"+mine+"/void-approval", ahkow, reversal, "If-Match", posted)
	if a := f.decide(weiling, void.str("id"), "approve", hashOf(void)); a.Status != 200 {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+mine, weiling, nil); g.str("status") != "reversed" || g.str("states", "ledger") != "voided" {
		t.Fatalf("after the void: %s", g.Raw)
	}

	// Founder mode: an edit is refused, then asked for, listed line by line, and approved; it posts as the approver.
	small, smallTag := e.postedBill("100.00", "9.00")
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+small+"/edit", ahkow, e.billEdit("90.00", "8.10", "One seat less"), "If-Match", smallTag),
		"editBill", 409, CodeBillEditApproval)
	asked = e.call(http.MethodPost, "/v1/bills/"+small+"/edit-approval", ahkow, e.billEdit("90.00", "8.10", "One seat less"), "If-Match", smallTag)
	if asked.Status != 202 || asked.str("operation") != approval.OpBillEdit || !strings.Contains(asked.Raw, `"field":"payable","before":"109.00","after":"98.10"`) ||
		!strings.Contains(asked.Raw, `"field":"line 1"`) {
		t.Fatalf("asking to edit: %d %s", asked.Status, asked.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 {
		t.Fatalf("approving the edit: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+small, weiling, nil); g.str("status") != "posted" || g.Body["version"] != float64(2) ||
		g.str("payable", "amount") != "98.10" || g.str("prepared_by", "id") != f.ahkow || g.str("posted_by", "id") != f.weiling {
		t.Fatalf("after the edit: %s", g.Raw)
	}

	// A bill the rules refuse lists every problem; the list shows each bill.
	bad := e.billDraft("10.00", "0.90")
	bad["supplier_id"] = nil
	bad["lines"].([]map[string]any)[0]["gst_code"] = "SR"
	r := e.call(http.MethodPost, "/v1/bills", ahkow, bad)
	if r.Status != 201 {
		t.Fatalf("saving a bad draft: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+r.str("id"), ahkow, nil); !strings.Contains(g.Raw, bill.CodeSupplier) || !strings.Contains(g.Raw, bill.CodeGSTCode) || g.Body["payable"] != nil {
		t.Errorf("a bad draft's checks: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+r.str("id")+"/post", ahkow, nil, "If-Match", r.Header.Get("ETag")), "postBill", 422, bill.CodeSupplier)
	if l := e.call(http.MethodGet, "/v1/bills?status=posted", weiling, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Errorf("the posted bills: %d %s", l.Status, l.Raw)
	}
}

// FR-AP-03 through the API: a supplier's credit note is a bill of kind vendor_credit, saved, checked and posted as a
// bill is, numbered VC-; it is listed apart from the bills; and its kind never changes.
// req: FR-AP-03
func TestVendorCreditsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	credit := e.billDraft("200.00", "18.00")
	credit["kind"], credit["supplier_ref"] = "vendor_credit", "CN-12"
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), credit)
	if r.Status != 201 || r.str("kind") != "vendor_credit" {
		t.Fatalf("saving a credit: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil); g.str("payable", "amount") != "218.00" || !strings.Contains(g.Raw, `"amount":{"amount":"218.00","currency":"SGD"}`) {
		t.Fatalf("the credit's check: %s", g.Raw)
	}
	again := e.billDraft("200.00", "18.00")
	again["kind"] = "bill"
	e.problemIs(e.call(http.MethodPut, "/v1/bills/"+id, e.as(authz.Accountant), again, "If-Match", tag), "replaceBill", 422, CodeInvalid)
	p := e.call(http.MethodPost, "/v1/bills/"+id+"/post", e.as(authz.Accountant), nil, "If-Match", tag)
	if p.Status != 200 || p.str("bill", "number") != "VC-0001" || p.str("entry", "rule_version") != "vendor_credit@1" {
		t.Fatalf("posting the credit: %d %s", p.Status, p.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/bills?kind=vendor_credit", e.as(authz.Auditor), nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the credits: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/bills", e.as(authz.Auditor), nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("the bills list the credit: %s", l.Raw)
	}
}
