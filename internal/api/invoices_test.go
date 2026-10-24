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
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// customer sets e's books up for invoices, once: the accounts receivable, GST control, deferred revenue and FX accounts
// by role (each unless the books have it), and a customer with 14 days' terms. It returns the customer's ID.
func (e *env) customer() string {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var id string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		cs, err := u.Contacts().Page(ctx, uuid.Nil, "customer", 10)
		if err != nil {
			return err
		}
		if len(cs) > 0 {
			id = cs[0].ID.String()
			return nil
		}
		have := map[string]bool{}
		accounts, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			have[string(a.Role)] = true
		}
		for _, a := range []storage.NewAccount{
			{Code: "1100", Name: "Accounts receivable", Type: storage.Asset, Role: storage.AccountRole(ledger.RoleAccountsReceivable)},
			{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)},
			{Code: "2400", Name: "Deferred revenue", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleDeferredRevenue)},
			{Code: "7030", Name: "FX & rounding differences", Type: storage.Expense, Role: storage.AccountRole(ledger.RoleFXRounding)},
		} {
			if have[string(a.Role)] {
				continue
			}
			if _, err := u.Accounts().Create(ctx, a); err != nil {
				return err
			}
		}
		fourteen := 14
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Acme Analytics Pte Ltd", Customer: true, PaymentTermsDays: &fourteen})
		id = c.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// invoiceDraft is an invoice to the customer of quantity × price on the sales account, coded SR.
func (e *env) invoiceDraft(quantity, price string) map[string]any {
	return map[string]any{
		"customer_id": e.customer(), "reference": "PO-" + price, "date": "2026-10-14", "currency": "SGD",
		"lines": []map[string]any{{"account_id": e.books.Sales.ID.String(), "description": "Analytics platform", "gst_code": "SR",
			"quantity": quantity, "unit_price": price}},
	}
}

// invoiceEdit is an edit of the customer's invoice to quantity × price, with a reason.
func (e *env) invoiceEdit(quantity, price, reason string) map[string]any {
	d := e.invoiceDraft(quantity, price)
	d["reason"] = reason
	return d
}

// createInvoice saves a draft invoice as an Accountant and returns its ID and ETag.
func (e *env) createInvoice(quantity, price string) (id, etag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), e.invoiceDraft(quantity, price))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createInvoice = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// issuedInvoice saves and issues a draft invoice as an Accountant and returns its ID and ETag.
func (e *env) issuedInvoice(quantity, price string) (id, etag string) {
	e.t.Helper()
	id, tag := e.createInvoice(quantity, price)
	r := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", e.as(authz.Accountant), nil, "If-Match", tag)
	if r.Status != http.StatusOK {
		e.t.Fatalf("issueInvoice = %d: %s", r.Status, r.Raw)
	}
	return id, r.Header.Get("ETag")
}

// FR-AR-03 through the API (p1-sales tasks 1.3 and 1.4): a draft invoice shows its five states, its due date from the
// customer's terms, its totals, what issuing writes and no checks; issuing numbers it; above the self-approval threshold
// it is asked for and approved by another person, never by whoever prepared it (FM-2, SOD-3); at or below it a founder
// issues it alone, logged for the weekly review (FM-3); in Founder mode a void and an edit are asked for (FM-1).
// req: FR-AR-03
// req: FR-INT-01
// req: FM-1
// req: FM-2
// req: SOD-3
func TestInvoicesThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	draft := func(who, quantity, price string) (string, string) {
		r := e.call(http.MethodPost, "/v1/invoices", who, e.invoiceDraft(quantity, price))
		if r.Status != http.StatusCreated {
			t.Fatalf("an invoice of %s × %s: %d %s", quantity, price, r.Status, r.Raw)
		}
		return r.str("id"), r.Header.Get("ETag")
	}

	// A draft: no number, its states, its due date from the customer's terms, each line's net, what issuing writes (not
	// GST-registered: no GST on a standard-rated line), and no checks.
	id, tag := draft(ahkow, "2", "500.00")
	g := e.call(http.MethodGet, "/v1/invoices/"+id, weiling, nil)
	if g.Status != 200 || g.str("status") != "draft" || g.Body["number"] != nil || g.str("states", "approval") != "draft" ||
		g.str("states", "ledger") != "not_posted" || g.str("states", "delivery") != "not_sent" || g.str("states", "settlement") != "open" ||
		g.str("states", "due") != "not_due" || g.str("due_date") != "2026-10-28" || g.str("total", "amount") != "1000.00" ||
		g.str("receivable", "amount") != "1000.00" || g.str("customer", "name") != "Acme Analytics Pte Ltd" || g.Body["gst_charged"] != false ||
		!strings.Contains(g.Raw, `"net":{"amount":"1000.00","currency":"SGD"}`) || len(g.Body["ledger_impact"].([]any)) != 2 ||
		len(g.Body["checks"].([]any)) != 0 || len(g.Body["gst_subtotals"].([]any)) != 0 || g.Header.Get("ETag") != tag {
		t.Fatalf("the draft: %d %s", g.Status, g.Raw)
	}

	// A preview of issuing posts nothing and uses no number.
	if pv := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", ahkow, nil, "If-Match", tag, "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" || pv.str("invoice", "number") != "INV-0001" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}

	// At the threshold a founder issues it alone, numbered, and the review log has it.
	low, lowTag := draft(ahkow, "1", "2000.00")
	p := e.call(http.MethodPost, "/v1/invoices/"+low+"/issue", ahkow, nil, "If-Match", lowTag)
	if p.Status != 200 || p.str("invoice", "number") != "INV-0001" || p.str("invoice", "states", "ledger") != "posted" ||
		p.str("invoice", "open", "amount") != "2000.00" || p.str("entry", "rule_version") != ledger.RuleInvoice {
		t.Fatalf("issuing S$2,000.00 alone: %d %s", p.Status, p.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/review-log", weiling, nil); !strings.Contains(l.Raw, `"kind":"self_approved","object_type":"invoice","object_key":"`+low+`"`) {
		t.Fatalf("the self-approval is not in the review log: %s", l.Raw)
	}

	// Above it: refused, asked for, never approved by the asker or by whoever prepared it, then approved.
	big, bigTag := draft(weiling, "5", "1000.00")
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+big+"/issue", ahkow, nil, "If-Match", bigTag), "issueInvoice", 409, CodeInvoiceNeedsApproval)
	asked := e.call(http.MethodPost, "/v1/invoices/"+big+"/issue-approval", ahkow, nil, "If-Match", bigTag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectInvoice || asked.str("operation") != approval.OpInvoicePost ||
		!strings.Contains(asked.str("envelope"), `"receivable":"5000.00"`) || !strings.Contains(asked.str("summary"), "Acme Analytics Pte Ltd (PO-1000.00)") {
		t.Fatalf("asking to issue: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+big, ahkow, nil); g.str("states", "approval") != "awaiting_approval" || g.str("approval_asked") != asked.str("id") {
		t.Errorf("the invoice does not show the request: %s", g.Raw)
	}
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, CodeOwnInvoice)
	e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/withdraw", ahkow, nil, "If-Match", `"1"`)
	mine, mineTag := draft(ahkow, "3", "1000.00")
	asked = e.call(http.MethodPost, "/v1/invoices/"+mine+"/issue-approval", ahkow, nil, "If-Match", mineTag)
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the issue: %d %s", a.Status, a.Raw)
	}
	g = e.call(http.MethodGet, "/v1/invoices/"+mine, weiling, nil)
	if g.str("status") != "posted" || g.str("number") != "INV-0002" || g.str("posted_by", "id") != f.weiling || g.str("receivable", "amount") != "3000.00" {
		t.Fatalf("after the approval: %s", g.Raw)
	}

	// Founder mode: a void is refused, then asked for and approved; the invoice keeps its number.
	reversal := map[string]any{"date": "2026-10-25", "reason": "Raised twice"}
	posted := g.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+mine+"/void", ahkow, reversal, "If-Match", posted), "voidInvoice", 409, CodeInvoiceVoidApproval)
	void := e.call(http.MethodPost, "/v1/invoices/"+mine+"/void-approval", ahkow, reversal, "If-Match", posted)
	if a := f.decide(weiling, void.str("id"), "approve", hashOf(void)); a.Status != 200 {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+mine, weiling, nil); g.str("status") != "reversed" || g.str("states", "ledger") != "voided" ||
		g.str("number") != "INV-0002" || g.Body["open"] != nil {
		t.Fatalf("after the void: %s", g.Raw)
	}

	// Founder mode: an edit is refused, then asked for, listed line by line, and approved; it posts as the approver.
	small, smallTag := e.issuedInvoice("1", "100.00")
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+small+"/edit", ahkow, e.invoiceEdit("1", "90.00", "Agreed discount"), "If-Match", smallTag),
		"editInvoice", 409, CodeInvoiceEditApproval)
	asked = e.call(http.MethodPost, "/v1/invoices/"+small+"/edit-approval", ahkow, e.invoiceEdit("1", "90.00", "Agreed discount"), "If-Match", smallTag)
	if asked.Status != 202 || asked.str("operation") != approval.OpInvoiceEdit ||
		!strings.Contains(asked.Raw, `"field":"receivable","before":"100.00","after":"90.00"`) || !strings.Contains(asked.Raw, `"field":"line 1"`) {
		t.Fatalf("asking to edit: %d %s", asked.Status, asked.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 {
		t.Fatalf("approving the edit: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+small, weiling, nil); g.str("status") != "posted" || g.Body["version"] != float64(2) ||
		g.str("receivable", "amount") != "90.00" || g.str("total", "amount") != "90.00" || g.str("prepared_by", "id") != f.ahkow ||
		g.str("posted_by", "id") != f.weiling || g.str("number") != "INV-0003" {
		t.Fatalf("after the edit: %s", g.Raw)
	}

	// An invoice the rules refuse lists every problem; a refused issue uses no number; the list shows each invoice.
	bad := e.invoiceDraft("1", "10.00")
	bad["customer_id"] = nil
	bad["lines"].([]map[string]any)[0]["gst_code"] = "TX"
	r := e.call(http.MethodPost, "/v1/invoices", ahkow, bad)
	if r.Status != 201 {
		t.Fatalf("saving a bad draft: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+r.str("id"), ahkow, nil); !strings.Contains(g.Raw, invoice.CodeCustomer) ||
		!strings.Contains(g.Raw, `"field":"lines[0].gst_code"`) || g.Body["receivable"] != nil {
		t.Errorf("a bad draft's checks: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+r.str("id")+"/issue", ahkow, nil, "If-Match", r.Header.Get("ETag")), "issueInvoice", 422, invoice.CodeCustomer)
	if l := e.call(http.MethodGet, "/v1/invoices?status=posted", weiling, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Errorf("the issued invoices: %d %s", l.Status, l.Raw)
	}
	if n, _ := e.issuedInvoice("1", "10.00"); e.call(http.MethodGet, "/v1/invoices/"+n, weiling, nil).str("number") != "INV-0004" {
		t.Errorf("the refused issue used a number")
	}
}

// FR-AR-03 and RND-LINE through the API: a draft changes only with its ETag and while a draft; each line's net takes its
// discount in one rounding; once the organisation is GST-registered on the invoice's date a standard-rated line bears
// the pack's rate, by GST code under RND-GST-EXCL, and issuing posts it to 2200; a posted invoice is not changed as a
// draft; a field that cannot be read is refused with its path.
// req: FR-AR-03
// req: RND-LINE
// req: RND-GST-EXCL
func TestInvoiceDraftsAndGSTThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	e.customer()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: e.clock.Now()})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	d := e.invoiceDraft("3", "33.335")
	d["lines"] = append(d["lines"].([]map[string]any), map[string]any{"account_id": e.books.Sales.ID.String(), "gst_code": "ZR",
		"quantity": "1", "unit_price": "250.00", "discount_percent": "12.5"})
	r := e.call(http.MethodPost, "/v1/invoices", acc, d)
	if r.Status != 201 || r.Body["gst_charged"] != true || r.str("net", "amount") != "318.76" || r.str("gst", "amount") != "9.00" ||
		r.str("total", "amount") != "327.76" || !strings.Contains(r.Raw, `"gst_code":"SR","rate":"0.09","net":{"amount":"100.01","currency":"SGD"},"gst":{"amount":"9.00","currency":"SGD"}`) ||
		!strings.Contains(r.Raw, `"discount_percent":"12.5","account_id"`) || !strings.Contains(r.Raw, `"net":{"amount":"218.75","currency":"SGD"}`) {
		t.Fatalf("a GST-registered draft: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")

	// Changed with its ETag: a stale one is refused; the new version replaces it.
	e.problemIs(e.call(http.MethodPut, "/v1/invoices/"+id, acc, e.invoiceDraft("1", "50.00"), "If-Match", `"9-draft"`), "replaceInvoice", 412, CodeStale)
	u := e.call(http.MethodPut, "/v1/invoices/"+id, acc, d, "If-Match", tag)
	if u.Status != 200 || u.Body["version"] != float64(2) || u.Header.Get("ETag") == tag {
		t.Fatalf("replacing the draft: %d %s", u.Status, u.Raw)
	}
	g := e.call(http.MethodGet, "/v1/invoices/"+id, acc, nil)
	if lines := g.Body["ledger_impact"].([]any); len(lines) != 4 {
		t.Fatalf("issuing writes 1100, two revenue lines and 2200: %s", g.Raw)
	}
	p := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", acc, nil, "If-Match", u.Header.Get("ETag"))
	if p.Status != 200 || p.str("invoice", "receivable", "amount") != "327.76" || p.str("invoice", "gst", "amount") != "9.00" {
		t.Fatalf("issuing: %d %s", p.Status, p.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/invoices/"+id, acc, d, "If-Match", p.Header.Get("ETag")), "replaceInvoice", 409, invoice.CodeNotDraft)
	e.problemIs(e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", acc, nil, "If-Match", p.Header.Get("ETag")), "issueInvoice", 409, invoice.CodeNotDraft)

	// What cannot be read is refused, each with its path.
	bad := e.invoiceDraft("1.1234567", "abc")
	bad["customer_id"] = "nobody"
	r = e.call(http.MethodPost, "/v1/invoices", acc, bad)
	e.problemIs(r, "createInvoice", 422, CodeInvalid)
	if f := fields(r); f["body.customer_id"] == "" || f["body.lines[0].quantity"] == "" {
		t.Errorf("want each field with its code, got %v: %s", f, r.Raw)
	}
	// A bill is not an invoice.
	billID, _ := e.createBill("10.00", "0.90")
	e.problemIs(e.call(http.MethodGet, "/v1/invoices/"+billID, acc, nil), "getInvoice", 404, invoice.CodeNotInvoice)
}

// An invoice's evidence (FR-AR-03's attachments; p1-sales task 1.4), as a bill's: a file attached to a draft is listed
// and read with its ETag and comes off again; issuing the invoice queues the commit of its files to the locked store,
// after which a file never comes off. A bill is not an invoice.
// req: FR-DOC-05
// req: FR-DOC-07
// req: FR-DOC-13
func TestEvidenceOnAnInvoice(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createInvoice("1", "100.00")
	path := "/v1/invoices/" + id + "/evidence"
	acc := e.as(authz.Accountant)
	po := []byte("%PDF-1.7 Acme PO-100.00")

	r := e.upload(path, acc, map[string]string{"evidence_type": "contract"}, "PO.pdf", po, "If-Match", tag, "Idempotency-Key", "invoice-ev-1")
	if r.Status != http.StatusCreated || r.str("invoice_id") != id || r.str("media_type") != evidence.PDF || r.str("state") != "attached" ||
		r.Header.Get("Location") != path+"/"+r.str("id") {
		t.Fatalf("addInvoiceEvidence = %d: %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, path, e.as(authz.Auditor), nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("listInvoiceEvidence = %d: %s", l.Status, l.Raw)
	}
	if g := e.call(http.MethodGet, path+"/"+r.str("id"), acc, nil); g.Status != http.StatusOK || g.Header.Get("ETag") != `"1"` {
		t.Fatalf("getInvoiceEvidence = %d: %s", g.Status, g.Raw)
	}
	if rm := e.call(http.MethodDelete, path+"/"+r.str("id"), acc, nil, "If-Match", `"1"`); rm.Status != http.StatusOK || rm.str("state") != "removed" {
		t.Fatalf("removeInvoiceEvidence = %d: %s", rm.Status, rm.Raw)
	}

	kept := e.upload(path, acc, map[string]string{"evidence_type": "contract"}, "PO-2.pdf", []byte("%PDF-1.7 the signed order"), "If-Match", tag)
	if kept.Status != http.StatusCreated {
		t.Fatalf("a second file = %d: %s", kept.Status, kept.Raw)
	}
	drainWork(e.t, e)
	if p := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", acc, nil, "If-Match", tag); p.Status != http.StatusOK {
		t.Fatalf("issuing = %d: %s", p.Status, p.Raw)
	}
	if kind := nextWork(t, e); kind != evidence.CommitKind {
		t.Fatalf("issuing an invoice with files queued %q; want %s", kind, evidence.CommitKind)
	}
	if rm := e.call(http.MethodDelete, path+"/"+kept.str("id"), acc, nil, "If-Match", "*"); rm.Status != http.StatusConflict || rm.problemCode() != storage.CodeEvidence {
		t.Errorf("removing from an issued invoice = %d: %s", rm.Status, rm.Raw)
	}
	billID, _ := e.createBill("20.00", "1.80")
	e.problemIs(e.call(http.MethodGet, "/v1/invoices/"+billID+"/evidence", acc, nil), "listInvoiceEvidence", 404, invoice.CodeNotInvoice)
}

// FR-GL-03 through the API (rev 0.118): an invoice line spread over months shows its releases, all projected while the
// invoice is a draft; once it is issued, those due by the organisation's today are in the ledger at once and the rest
// stay projections (ENG-05); issuing posts the line's net to 2400 Deferred revenue.
// req: FR-GL-03
// req: ENG-05
func TestSpreadInvoicesThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	e.customer()
	releases := func(r response) string {
		var out []string
		for _, x := range r.Body["releases"].([]any) {
			m := x.(map[string]any)
			out = append(out, m["month"].(string)+" "+m["date"].(string)+" "+m["amount"].(map[string]any)["amount"].(string)+" "+m["status"].(string))
		}
		return strings.Join(out, "; ")
	}
	d := e.invoiceDraft("1", "1200.00")
	d["lines"].([]map[string]any)[0]["spread_months"], d["lines"].([]map[string]any)[0]["spread_from"] = 3, "2026-09-20"
	r := e.call(http.MethodPost, "/v1/invoices", acc, d)
	if r.Status != 201 || !strings.Contains(r.Raw, `"spread_months":3,"spread_from":"2026-09-20"`) {
		t.Fatalf("saving a spread invoice: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	g := e.call(http.MethodGet, "/v1/invoices/"+id, acc, nil)
	if got := releases(g); got != "2026-09 2026-10-14 400.00 projected; 2026-10 2026-10-14 400.00 projected; 2026-11 2026-11-01 400.00 projected" {
		t.Fatalf("the draft's releases: %s\n%s", got, g.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", acc, nil, "If-Match", tag); p.Status != 200 {
		t.Fatalf("issuing: %d %s", p.Status, p.Raw)
	}
	g = e.call(http.MethodGet, "/v1/invoices/"+id, e.as(authz.Auditor), nil)
	if got := releases(g); got != "2026-09 2026-10-14 400.00 posted; 2026-10 2026-10-14 400.00 posted; 2026-11 2026-11-01 400.00 projected" ||
		!strings.Contains(g.Raw, `"entry_id":"`) {
		t.Fatalf("the issued invoice's releases: %s\n%s", got, g.Raw)
	}
}
