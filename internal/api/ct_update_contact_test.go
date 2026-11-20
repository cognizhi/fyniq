//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ct.update_contact (Appendix E, FR-CT-01; p1-ai-proposals task 4.1) is the first task type that changes master data, so the first with an undo
// (AI-31, AI-91): the AI proposes six details of an active company contact, the dry run shows each before and after and changes nothing, and a
// person other than the requester approves it (AI-10); the executor then changes those six details only, once, as the approver. A bank detail, the
// remittance email, a legal name or any field the task type does not name is refused, so is the record of a person (an individual or a director) and
// an inactive contact; a contact changed since the dry run makes the proposal Stale. The person who holds the right to change contacts undoes it,
// once, while nobody has changed the contact since, and the undo restores the old values, is audited and leaves the proposal Executed; someone without the
// right, an undo after another change and an undo of what was never executed are refused, and the AI's tokens reach none of it.
// req: AI-31
// req: AI-91
// req: AI-10
// req: AI-11
// req: FR-CT-01
// req: ai/task-catalogue ct.update_contact Update contact details
// asvs: V8.2.2, V8.3.1
func TestTheAIUpdatesAContactAndAPersonUndoesIt(t *testing.T) {
	e := newEnv(t)
	k := newTaskKit(e, "ct.update_contact")
	arUser, _ := k.roleToken("Ari Ct", authz.ARSpecialist)
	_, approver := k.roleToken("Acca Ct", authz.Accountant)
	_, employee := k.roleToken("Emp Ct", authz.Employee)
	_, auditor := k.roleToken("Aud Ct", authz.Auditor)
	turn := proposeTurn(e, arUser.String())
	acc := e.as(authz.Accountant)
	customer := e.customer()
	get := func() response { return e.call(http.MethodGet, "/v1/contacts/"+customer, acc, nil) }
	// The contact starts with a remittance email, which must survive everything below.
	c := get()
	if r := e.call(http.MethodPatch, "/v1/contacts/"+customer, acc, map[string]any{"remittance_email": "pay@customer.example", "billing_email": "old@customer.example", "phone": "+65 6000 0001"},
		"If-Match", c.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("preparing the contact: %d %s", r.Status, r.Raw)
	}
	good := map[string]any{"contact_id": customer, "billing_email": "accounts@customer.example", "phone": "+65 6000 0000", "payment_terms_days": 45}

	made := k.propose(turn, "ct.update_contact", good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || !strings.HasPrefix(made.str("summary"), "Changes the billing email, phone, payment terms days of ") {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/ai/proposals/"+made.str("id"), approver, nil); !strings.Contains(r.Raw, "old@customer.example") || !strings.Contains(r.Raw, "accounts@customer.example") {
		t.Errorf("the review does not show each detail before and after: %d %s", r.Status, r.Raw)
	}
	if now := get(); now.str("billing_email") != "old@customer.example" {
		t.Errorf("the dry run changed the contact: %s", now.Raw)
	}
	// Refusals leave nothing waiting: a bank or any other field, a person's contact, an inactive one, nothing to change, a bad value.
	bad := func(what string, p map[string]any) {
		t.Helper()
		if r := k.propose(turn, "ct.update_contact", p); r.Status < 400 && r.str("status") != storage.ProposalInvalid {
			t.Errorf("%s made a proposal: %s", what, r.Raw)
		}
	}
	bad("a remittance email", map[string]any{"contact_id": customer, "remittance_email": "steal@evil.example"})
	bad("a bank account", map[string]any{"contact_id": customer, "bank_account_number": "123456789"})
	bad("a legal name", map[string]any{"contact_id": customer, "legal_name": "Evil Ltd"})
	bad("a tax residence", map[string]any{"contact_id": customer, "tax_residence": "ZZ"})
	bad("the same details", map[string]any{"contact_id": customer, "phone": "+65 6000 0001"})
	bad("no detail", map[string]any{"contact_id": customer})
	bad("a bad email", map[string]any{"contact_id": customer, "billing_email": "not an address"})
	bad("a revenue-less category", map[string]any{"contact_id": customer, "default_category": "0000"})
	bad("a contact that is none", map[string]any{"contact_id": uuid.NewString(), "phone": "+65 6000 7777"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for _, d := range []storage.ContactDetails{{LegalName: "Mei Lin Tan", Customer: true, Individual: true}, {LegalName: "Director Dan", Customer: true, Director: true}} {
			p, err := u.Contacts().Create(ctx, d)
			if err != nil {
				return err
			}
			bad("the contact of a person ("+d.LegalName+")", map[string]any{"contact_id": p.ID.String(), "phone": "+65 6000 5555"})
		}
		return nil
	})
	if x := k.propose(proposeTurn(e, e.joinedIn(e.books.Org, "Cleo Ct", "employee").String()), "ct.update_contact", good); x.Status < 400 && x.str("status") != storage.ProposalInvalid {
		t.Errorf("a change for someone who may not change contacts: %s", x.Raw)
	}
	_ = employee

	// Approved by a person who may change contacts, the executor changes the six details once and nothing else.
	k.approve(made.str("id"), approver)
	pr := k.execute(made.str("id"), 2)
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "contact:"+customer) {
		t.Fatalf("the executed proposal: %s %s %s", pr.Status, pr.Reason, pr.Execution)
	}
	now := get()
	if now.str("billing_email") != "accounts@customer.example" || now.str("phone") != "+65 6000 0000" || now.str("remittance_email") != "pay@customer.example" {
		t.Errorf("the contact after: %s", now.Raw)
	}
	// The review offers the undo to someone who may do it, and not to someone who may not.
	if r := e.call(http.MethodGet, "/v1/ai/proposals/"+made.str("id"), approver, nil); r.Body["can_undo"] != true {
		t.Errorf("the review does not offer the undo to an accountant: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/ai/proposals/"+made.str("id"), auditor, nil); r.Body["can_undo"] == true {
		t.Errorf("the review offers the undo to an auditor: %s", r.Raw)
	}
	// What the undo needs is not shown on the review.
	if r := e.call(http.MethodGet, "/v1/ai/proposals/"+made.str("id"), approver, nil); strings.Contains(r.Raw, `"undo"`) {
		t.Errorf("the review shows the undo's values: %s", r.Raw)
	}

	undo := func(tok, id, why string) response {
		return e.call(http.MethodPost, "/v1/ai/proposal-undos", tok, map[string]any{"proposal_id": id, "reason": why}, "Idempotency-Key", uuid.NewString())
	}
	// Who may not change contacts cannot undo; the AI's turn cannot; a proposal that did not execute has nothing to undo.
	if r := undo(auditor, made.str("id"), "x"); r.Status != http.StatusForbidden && r.Status != http.StatusNotFound {
		t.Errorf("an auditor undoing it: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/ai/proposal-undos", turn, map[string]any{"proposal_id": made.str("id"), "reason": "x"}); r.Status < 400 {
		t.Errorf("the AI's turn token undid a proposal: %d", r.Status)
	}
	waiting := k.propose(turn, "ct.update_contact", map[string]any{"contact_id": customer, "address": "1 Raffles Place"})
	if r := undo(approver, waiting.str("id"), "x"); r.Status != http.StatusConflict || r.str("code") != CodeUndoNotExecuted {
		t.Errorf("undoing a waiting proposal: %d %s", r.Status, r.Raw)
	}
	// The undo restores the old values, as the person, once; the proposal stays Executed; the trail has it.
	r := undo(approver, made.str("id"), "It was the wrong branch")
	if r.Status != http.StatusCreated || !strings.Contains(r.Raw, "contact:"+customer) {
		t.Fatalf("the undo: %d %s", r.Status, r.Raw)
	}
	back := get()
	if back.str("billing_email") != "old@customer.example" || back.str("phone") != "+65 6000 0001" || back.str("remittance_email") != "pay@customer.example" {
		t.Errorf("the contact after its undo: %s", back.Raw)
	}
	if after := k.stored(made.str("id")); after.Status != storage.ProposalExecuted {
		t.Errorf("the proposal after its undo: %s", after.Status)
	}
	if r := e.call(http.MethodGet, "/v1/ai/proposals/"+made.str("id"), approver, nil); r.Body["can_undo"] == true || r.str("undone_at") == "" || r.str("undo_reason") != "It was the wrong branch" {
		t.Errorf("the review after its undo: %s", r.Raw)
	}
	if again := undo(approver, made.str("id"), "again"); again.Status != http.StatusConflict || again.str("code") != CodeUndoAlready {
		t.Errorf("undoing twice: %d %s", again.Status, again.Raw)
	}
	if !strings.Contains(auditTrail(t, e), `"action":"ai_proposal.undone"`) {
		t.Error("the undo is not in the audit trail")
	}

	// A contact changed by anyone since the execution is not undone, and one changed since the dry run makes the proposal Stale.
	second := k.propose(turn, "ct.update_contact", map[string]any{"contact_id": customer, "address": "8 Marina Boulevard"})
	third := k.propose(turn, "ct.update_contact", map[string]any{"contact_id": customer, "default_gst_code": "SR"})
	k.approve(second.str("id"), approver)
	if done := k.execute(second.str("id"), 1); done.Status != storage.ProposalExecuted {
		t.Fatalf("the second: %s %s", done.Status, done.Reason)
	}
	c = get()
	if r := e.call(http.MethodPatch, "/v1/contacts/"+customer, acc, map[string]any{"phone": "+65 6000 3333"}, "If-Match", c.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("a person changes it: %d %s", r.Status, r.Raw)
	}
	if moved := undo(approver, second.str("id"), "x"); moved.Status != http.StatusConflict || moved.str("code") != CodeUndoMoved {
		t.Errorf("undoing what moved: %d %s", moved.Status, moved.Raw)
	}
	k.approve(third.str("id"), approver)
	if st := k.execute(third.str("id"), 1); st.Status != storage.ProposalStale {
		t.Errorf("the third proposal after the contact changed: %s %s", st.Status, st.Reason)
	}
}
