//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ap.draft_bill (Appendix E, FR-AP-04, FR-AP-12; p1-ai-proposals task 1.1): the AI proposes a draft bill of a supplier's document; the dry run
// shows the lines, the total and what would stop it posting and makes nothing; once a person approves it the executor makes the same draft a
// person's would be, once, as the approving person, and posts nothing; a supplier who changed since the dry run is Stale and makes nothing.
// A supplier that is not one, an account that cannot be costed, a GST code that is not a purchase code and a person who may not enter bills are
// refused. Its input tax stays to confirm: the payload has no field to decide it (G4-029).
// req: ai/task-catalogue ap.draft_bill Draft a bill
// req: AI-11
// req: AI-30
// req: FR-AP-04
// asvs: V8.2.2
func TestTheAIDraftsABillThatAPersonApproves(t *testing.T) {
	e := newEnv(t)
	k := newTaskKit(e, "ap.draft_bill")
	apUser, _ := k.roleToken("Apu Ai", authz.APSpecialist)
	_, approver := k.roleToken("Acca Ai", authz.Accountant)
	_, employee := k.roleToken("Emp Ai", authz.Employee)
	_ = employee
	turn := proposeTurn(e, apUser.String())
	supplier := e.supplier()
	good := map[string]any{"supplier_id": supplier, "supplier_ref": "AI-INV-1", "date": "2026-10-14", "currency": "SGD",
		"lines": []map[string]any{{"account": e.books.Rent.Code, "description": "October rent", "net": "100.00", "gst_code": "TX", "gst": "9.00"}}}
	bills := func() int {
		items, _ := e.call(http.MethodGet, "/v1/bills", e.as(authz.Accountant), nil).Body["items"].([]any)
		return len(items)
	}
	before := bills()

	made := k.propose(turn, "ap.draft_bill", good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || !strings.HasPrefix(made.str("summary"), "Drafts a bill from ") ||
		!strings.Contains(made.str("summary"), "SGD 109.00") || bills() != before {
		t.Fatalf("the proposal: %d %s (%d bills, was %d)", made.Status, made.Raw, bills(), before)
	}
	// Refusals leave nothing waiting.
	bad := func(mutate func(p map[string]any)) {
		t.Helper()
		p := map[string]any{}
		for key, v := range good {
			p[key] = v
		}
		mutate(p)
		if r := k.propose(turn, "ap.draft_bill", p); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
			t.Errorf("a payload that should be refused made a proposal: %s", r.Raw)
		}
	}
	bad(func(p map[string]any) { p["supplier_id"] = e.customer() }) // a customer is no supplier
	bad(func(p map[string]any) { p["date"] = "yesterday" })
	bad(func(p map[string]any) { p["currency"] = "ZZZ" })
	bad(func(p map[string]any) {
		p["lines"] = []map[string]any{{"account": e.books.Sales.Code, "description": "x", "net": "1.00", "gst_code": "TX", "gst": "0.09"}}
	}) // revenue
	bad(func(p map[string]any) {
		p["lines"] = []map[string]any{{"account": e.books.Rent.Code, "description": "x", "net": "1.00", "gst_code": "SR", "gst": "0.09"}}
	}) // SR is a sales code
	bad(func(p map[string]any) { p["eligible"] = true }) // no field decides input tax
	if x := k.propose(proposeTurn(e, e.joinedIn(e.books.Org, "Cleo Ai", "employee").String()), "ap.draft_bill", good); x.Status < 400 && x.str("status") != storage.ProposalInvalid {
		t.Errorf("a draft for someone who may not enter bills: %s", x.Raw)
	}

	// Approved by a person who may enter bills, the executor drafts the bill once and posts nothing.
	k.approve(made.str("id"), approver)
	pr := k.execute(made.str("id"), 2)
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "bill:") || bills() != before+1 {
		t.Fatalf("the executed proposal: %s %s (%d bills, was %d)", pr.Status, pr.Execution, bills(), before)
	}
	list := e.call(http.MethodGet, "/v1/bills", e.as(authz.Accountant), nil).Body["items"].([]any)
	var draft map[string]any
	for _, it := range list {
		if m := it.(map[string]any); m["supplier_ref"] == "AI-INV-1" {
			draft = m
		}
	}
	if draft == nil || draft["status"] != "draft" {
		t.Fatalf("the drafted bill: %v", list)
	}

	// A supplier who changed since the dry run is Stale and makes nothing.
	second := k.propose(turn, "ap.draft_bill", map[string]any{"supplier_id": supplier, "supplier_ref": "AI-INV-2", "date": "2026-10-15", "currency": "SGD",
		"lines": []map[string]any{{"account": e.books.Rent.Code, "description": "Rent", "net": "50.00", "gst_code": "TX", "gst": "4.50"}}})
	if second.Status != http.StatusCreated {
		t.Fatalf("a second proposal: %d %s", second.Status, second.Raw)
	}
	c := e.call(http.MethodGet, "/v1/contacts/"+supplier, e.as(authz.Accountant), nil)
	body := map[string]any{"phone": "+65 6000 0000"}
	if r := e.call(http.MethodPatch, "/v1/contacts/"+supplier, e.as(authz.Accountant), body, "If-Match", c.Header.Get("ETag")); r.Status >= 300 {
		t.Fatalf("changing the supplier: %d %s", r.Status, r.Raw)
	}
	k.approve(second.str("id"), approver)
	if pr2 := k.execute(second.str("id"), 1); pr2.Status != storage.ProposalStale || bills() != before+1 {
		t.Errorf("the second proposal after the supplier changed: %s %s (%d bills)", pr2.Status, pr2.Reason, bills())
	}
}
