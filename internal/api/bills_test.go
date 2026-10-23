//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

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

// foreignSupplier sets e's books up for bills and adds, once, a vendor resident for tax in the United States with
// 2330 Withholding tax payable (FR-AP-19). It returns the vendor's ID.
func (e *env) foreignSupplier() string {
	e.t.Helper()
	e.supplier()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var id string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		cs, err := u.Contacts().Page(ctx, uuid.Nil, "vendor", 50)
		if err != nil {
			return err
		}
		for _, c := range cs {
			if c.Country == "US" {
				id = c.ID.String()
				return nil
			}
		}
		if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2330", Name: "Withholding tax payable", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleWithholdingTax)}); err != nil {
			return err
		}
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Ava Consulting LLC", Vendor: true, Country: "US"})
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

// FR-GL-03 through the API (rev 0.83): a line spread over months shows its releases, all projected while the bill is a
// draft; once it posts, those due by the organisation's today are in the ledger at once and the rest stay projections
// (ENG-05). A credit note's line is never spread (LEDGER-28).
// req: FR-GL-03
// req: ENG-05
func TestSpreadBillsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	e.supplier()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var prepaid storage.Account
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		prepaid, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1210", Name: "Prepayments", Type: storage.Asset, Role: storage.AccountRole(ledger.RolePrepayments)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	releases := func(r response) string {
		var out []string
		for _, x := range r.Body["releases"].([]any) {
			m := x.(map[string]any)
			out = append(out, m["month"].(string)+" "+m["date"].(string)+" "+m["amount"].(map[string]any)["amount"].(string)+" "+m["status"].(string))
		}
		return strings.Join(out, "; ")
	}
	d := e.billDraft("1200.00", "108.00")
	d["lines"].([]map[string]any)[0]["spread_months"], d["lines"].([]map[string]any)[0]["spread_from"] = 3, "2026-09-20"
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
	if r.Status != 201 || !strings.Contains(r.Raw, `"spread_months":3,"spread_from":"2026-09-20"`) {
		t.Fatalf("saving a spread bill: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	g := e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil)
	if got := releases(g); got != "2026-09 2026-10-14 436.00 projected; 2026-10 2026-10-14 436.00 projected; 2026-11 2026-11-01 436.00 projected" ||
		g.Body["ledger_impact"].([]any)[0].(map[string]any)["account_id"] != prepaid.ID.String() {
		t.Fatalf("the draft's releases: %s\n%s", got, g.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/bills/"+id+"/post", e.as(authz.Accountant), nil, "If-Match", tag); p.Status != 200 {
		t.Fatalf("posting: %d %s", p.Status, p.Raw)
	}
	g = e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Auditor), nil)
	if got := releases(g); got != "2026-09 2026-10-14 436.00 posted; 2026-10 2026-10-14 436.00 posted; 2026-11 2026-11-01 436.00 projected" ||
		!strings.Contains(g.Raw, `"entry_id":"`) {
		t.Fatalf("the posted bill's releases: %s\n%s", got, g.Raw)
	}

	c := e.billDraft("100.00", "9.00")
	c["kind"], c["supplier_ref"] = "vendor_credit", "CN-13"
	c["lines"].([]map[string]any)[0]["spread_months"] = 2
	r = e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), c)
	if g := e.call(http.MethodGet, "/v1/bills/"+r.str("id"), e.as(authz.Accountant), nil); !strings.Contains(g.Raw, `"code":"`+ledger.CodeSpread+`"`) ||
		len(g.Body["releases"].([]any)) != 0 {
		t.Errorf("a spread credit note: %s", g.Raw)
	}
}

// FR-AP-20 through the API (rev 0.89): a bill saves its input-tax facts and each line's business share, shows every fact
// stated, and getBill shows how each line's input tax was decided, for a draft and once posted. A simplified tax
// invoice used 60% for the business claims 60% of its GST (RND-GST-SHARE); an agent's invoice credited to a director's
// loan waits for the tax agent (⚠). Facts the API does not know are refused.
// req: FR-AP-20
// req: RND-GST-SHARE
func TestBillInputTaxThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	e.supplier()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	decision := func(r response) map[string]any { return r.Body["input_tax_decisions"].([]any)[0].(map[string]any) }

	d := e.billDraft("50.00", "4.50")
	d["date"], d["input_tax"] = "2026-10-26", map[string]any{"document": "simplified"}
	d["lines"].([]map[string]any)[0]["business_share"] = "60"
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
	if r.Status != 201 || !strings.Contains(r.Raw, `"input_tax":{"supplier_registered":true,"document":"simplified","addressed_to":"company"}`) ||
		!strings.Contains(r.Raw, `"business_share":"60"`) {
		t.Fatalf("saving the facts: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	g := e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil)
	if x := decision(g); x["condition"] != "claimable" || x["reason"] != bill.ReasonBusinessShare || x["business_share"] != "60" ||
		!strings.Contains(g.Raw, `"amount":"2.70","currency":"SGD"`) {
		t.Fatalf("the draft's decision: %v\n%s", x, g.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/bills/"+id+"/post", e.as(authz.Accountant), nil, "If-Match", tag); p.Status != 200 {
		t.Fatalf("posting: %d %s", p.Status, p.Raw)
	}
	if g = e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Auditor), nil); decision(g)["reason"] != bill.ReasonBusinessShare {
		t.Errorf("the posted bill's decision: %s", g.Raw)
	}

	loan := e.billDraft("100.00", "9.00")
	loan["supplier_ref"], loan["input_tax"] = "PH-1", map[string]any{"addressed_to": "agent", "repaid": "director_loan"}
	r = e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), loan)
	g = e.call(http.MethodGet, "/v1/bills/"+r.str("id"), e.as(authz.Accountant), nil)
	if decision(g)["condition"] != "tax_agent" || !strings.Contains(g.Raw, `"code":"`+bill.WarnTaxAgent+`"`) ||
		!strings.Contains(g.Raw, `"repaid":"director_loan"`) {
		t.Errorf("a director's loan: %s", g.Raw)
	}

	for name, edit := range map[string]func(map[string]any){
		"an unknown document": func(d map[string]any) { d["input_tax"] = map[string]any{"document": "receipt"} },
		"a share over 100":    func(d map[string]any) { d["lines"].([]map[string]any)[0]["business_share"] = "120" },
		"repaid to a company": func(d map[string]any) { d["input_tax"] = map[string]any{"repaid": "director_loan"} },
	} {
		d := e.billDraft("10.00", "0.90")
		edit(d)
		if r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d); r.Status != 422 {
			t.Errorf("%s: %d %s", name, r.Status, r.Raw)
		}
	}
}

// FR-AP-19 through the API (rev 0.90): a supplier's tax residence; a non-resident supplier's bill without a nature of
// payment cannot post (BILL-16); with one and no decision it shows ⚠ and how withholding stands; once the tax agent's
// decision to withhold 15% is recorded, the draft shows the tax and posts it; the decision lists under the supplier;
// a decision Fyniq cannot record is refused (BILL-17).
// req: FR-AP-19
// req: G4-010
func TestWithholdingThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	id := e.foreignSupplier()
	c := e.call(http.MethodPatch, "/v1/contacts/"+id, e.as(authz.Accountant), map[string]any{"tax_residence": "AU"}, "If-Match", "*")
	if c.Status != 200 || c.str("tax_residence") != "AU" {
		t.Fatalf("setting the tax residence: %d %s", c.Status, c.Raw)
	}
	d := e.billDraft("5000.00", "0.00")
	d["supplier_id"], d["supplier_ref"] = id, "AVA-1"
	d["lines"].([]map[string]any)[0]["gst_code"] = "NR"
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
	g := e.call(http.MethodGet, "/v1/bills/"+r.str("id"), e.as(authz.Accountant), nil)
	if !strings.Contains(g.Raw, `"code":"`+bill.CodeNature+`"`) || !strings.Contains(g.Raw, `"withholding":{"residence":"AU"}`) {
		t.Fatalf("a bill with no nature: %s", g.Raw)
	}
	d["nature_of_payment"] = "services_in_singapore"
	u := e.call(http.MethodPut, "/v1/bills/"+r.str("id"), e.as(authz.Accountant), d, "If-Match", r.Header.Get("ETag"))
	g = e.call(http.MethodGet, "/v1/bills/"+r.str("id"), e.as(authz.Accountant), nil)
	if u.Status != 200 || !strings.Contains(g.Raw, `"code":"`+bill.WarnWHTUndecided+`"`) || strings.Contains(g.Raw, `"decision"`) {
		t.Fatalf("an undecided bill: %d %s", u.Status, g.Raw)
	}
	decision := map[string]any{"nature_of_payment": "services_in_singapore", "outcome": "withhold", "rate_percent": "15", "tax_agent": "Grace Ho",
		"bill_id": r.str("id")}
	contact := e.call(http.MethodGet, "/v1/contacts/"+id, e.as(authz.Accountant), nil)
	path := "/v1/contacts/" + id + "/withholding-decisions"
	if x := e.call(http.MethodPost, path, e.as(authz.Accountant), decision); x.Status != 428 {
		t.Errorf("without If-Match: %d %s", x.Status, x.Raw)
	}
	if x := e.call(http.MethodPost, path, e.as(authz.Accountant), decision, "If-Match", `"99"`); x.Status != 412 {
		t.Errorf("with a stale If-Match: %d %s", x.Status, x.Raw)
	}
	if x := e.call(http.MethodPost, path, e.as(authz.Accountant), decision, "If-Match", contact.Header.Get("ETag"), "Prefer", "return=preview"); x.Status != 200 ||
		x.Header.Get("Preference-Applied") != "return=preview" {
		t.Errorf("a preview: %d %s", x.Status, x.Raw)
	}
	rec := e.call(http.MethodPost, path, e.as(authz.Accountant), decision, "If-Match", contact.Header.Get("ETag"))
	if rec.Status != 201 || rec.str("outcome") != "withhold" || rec.str("rate_percent") != "15" {
		t.Fatalf("recording the decision: %d %s", rec.Status, rec.Raw)
	}
	g = e.call(http.MethodGet, "/v1/bills/"+r.str("id"), e.as(authz.Accountant), nil)
	if !strings.Contains(g.Raw, `"tax":{"amount":"750.00","currency":"SGD"}`) || !strings.Contains(g.Raw, `"payable":{"amount":"4250.00"`) {
		t.Fatalf("the decided bill: %s", g.Raw)
	}
	decision["outcome"], decision["rate_percent"] = "treaty", "10"
	if x := e.call(http.MethodPost, path, e.as(authz.Accountant), decision, "If-Match", "*"); x.Status != 201 {
		t.Fatalf("a second decision: %d %s", x.Status, x.Raw)
	}
	l := e.call(http.MethodGet, path+"?page_size=1", e.as(authz.Auditor), nil)
	if l.Status != 200 || !strings.Contains(l.Raw, `"outcome":"treaty"`) || l.str("next_cursor") == "" {
		t.Fatalf("the decisions' first page: %d %s", l.Status, l.Raw)
	}
	if n := e.call(http.MethodGet, path+"?page_size=1&cursor="+l.str("next_cursor"), e.as(authz.Auditor), nil); !strings.Contains(n.Raw, `"tax_agent":"Grace Ho"`) ||
		!strings.Contains(n.Raw, `"outcome":"withhold"`) || n.str("next_cursor") != "" {
		t.Errorf("the decisions' second page: %d %s", n.Status, n.Raw)
	}
	bad := e.call(http.MethodPost, "/v1/contacts/"+e.supplier()+"/withholding-decisions", e.as(authz.Accountant), map[string]any{"nature_of_payment": "interest",
		"outcome": "not_applicable", "tax_agent": "Grace Ho"}, "If-Match", "*")
	if bad.Status != 422 || !strings.Contains(bad.Raw, bill.CodeWithholding) {
		t.Errorf("a resident supplier's decision: %d %s", bad.Status, bad.Raw)
	}
}

// FR-AP-13 through the API (rev 0.93): a supplier's discount terms give its bill an early-payment discount; a posted
// bill's payment date is planned with its ETag, previewed, and unscheduled; the bills to pay soon list it.
// req: FR-AP-13
func TestBillPlansThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	sup := e.supplier()
	c := e.call(http.MethodPatch, "/v1/contacts/"+sup, e.as(authz.Accountant), map[string]any{"discount_percent": "2", "discount_days": 10}, "If-Match", "*")
	if c.Status != 200 || c.str("discount_percent") != "2" {
		t.Fatalf("the discount terms: %d %s", c.Status, c.Raw)
	}
	if x := e.call(http.MethodPatch, "/v1/contacts/"+sup, e.as(authz.Accountant), map[string]any{"discount_percent": "2.5", "discount_days": -1}, "If-Match", "*"); x.Status != 422 {
		t.Errorf("a percentage without days: %d %s", x.Status, x.Raw)
	}
	id, tag := e.postedBill("1000.00", "90.00")
	g := e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil)
	if !strings.Contains(g.Raw, `"early_payment_discount":{"percent":"2","until":"2026-10-24","amount":{"amount":"21.80","currency":"SGD"}}`) {
		t.Fatalf("the bill's discount: %s", g.Raw)
	}
	path := "/v1/bills/" + id + "/payment-plan"
	plan := map[string]any{"planned_on": "2026-11-10"}
	if x := e.call(http.MethodPost, path, e.as(authz.Bookkeeper), plan); x.Status != 428 {
		t.Errorf("without If-Match: %d %s", x.Status, x.Raw)
	}
	if x := e.call(http.MethodPost, path, e.as(authz.Bookkeeper), plan, "If-Match", tag, "Prefer", "return=preview"); x.Status != 200 ||
		x.Header.Get("Preference-Applied") != "return=preview" || x.str("planned_payment_on") != "2026-11-10" {
		t.Errorf("a preview: %d %s", x.Status, x.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bills/"+id, e.as(authz.Accountant), nil); g.str("planned_payment_on") != "" {
		t.Errorf("the preview planned it: %s", g.Raw)
	}
	r := e.call(http.MethodPost, path, e.as(authz.Bookkeeper), plan, "If-Match", tag)
	if r.Status != 200 || r.str("planned_payment_on") != "2026-11-10" || r.Header.Get("ETag") != tag {
		t.Fatalf("planning: %d %v %s", r.Status, r.Header, r.Raw)
	}
	if x := e.call(http.MethodPost, path, e.as(authz.Bookkeeper), map[string]any{"planned_on": "2026-10-01"}, "If-Match", tag); x.Status != 422 ||
		!strings.Contains(x.Raw, bill.CodePlan) {
		t.Errorf("before the bill's date: %d %s", x.Status, x.Raw)
	}
	rem := e.call(http.MethodGet, "/v1/bill-reminders", e.as(authz.Accountant), nil)
	if rem.Status != 200 || rem.Body["days"] != float64(3) {
		t.Fatalf("the reminders: %d %s", rem.Status, rem.Raw)
	}
	if x := e.call(http.MethodPost, path, e.as(authz.Bookkeeper), nil, "If-Match", tag); x.Status != 200 || x.str("planned_payment_on") != "" {
		t.Errorf("unscheduling: %d %s", x.Status, x.Raw)
	}
}
