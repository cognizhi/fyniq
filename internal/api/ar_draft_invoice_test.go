//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// ar.draft_invoice (Appendix E, FR-PRJ-10; p1-projects, the AI's draft): the AI proposes a draft invoice of a time-and-materials project's
// unbilled work, naming it by its code; the dry run shows the lines and the total and posts nothing; once a person
// approves it, the executor makes the same draft a person's would be, once, as the approving person, and issues nothing; work that was
// billed or priced differently since the dry run is Stale and makes nothing. An internal project, a revenue account that is not one, a
// person without the right and the module switched off are refused, and no cost rate is in the dry run.
// Version 1 covers a project's work; an invoice from a quote, an order or a description is a later version, so no requirement is tagged.
// asvs: V8.2.2
func TestTheAIDraftsAProjectInvoiceThatAPersonApproves(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	f := foundersIn(e)
	aiSwitchedOn(e)
	enableTaskTypes(e, "ar.draft_invoice", 1)
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Ai", "approver")
	priya := e.joinedIn(e.books.Org, "Priya Ai", "employee")
	una := e.joinedIn(e.books.Org, "Una Ai", "controller")
	arUser := e.joinedIn(e.books.Org, "Ari Ai", "ar_specialist")
	approver := e.joinedIn(e.books.Org, "Acca Ai", "accountant")
	customer := e.customer()
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "AIB-1", "name": "AI billed", "currency": "SGD", "billing_method": "time_and_materials", "customer_id": customer,
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	tok := e.person(priya, authz.Employee)
	var entries []map[string]any
	for _, d := range []string{"2090-01-02", "2090-01-03"} {
		entries = append(entries, map[string]any{"project_id": pid, "task_id": task, "work_date": d, "minutes": 480})
	}
	if r := e.saveWeek(tok, "2090-01-02", entries...); r.Status != 200 {
		t.Fatalf("saving the week: %d %s", r.Status, r.Raw)
	}
	_, tag := e.weekOf(tok, "2090-01-02")
	s := e.call(http.MethodPost, "/v1/timesheet/2090-01-02/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
	ctl := e.person(una, authz.Controller)
	etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), ctl, nil).Header.Get("ETag")
	if r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", ctl, map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}
	e.call(http.MethodPost, "/v1/billing-rates", acc, map[string]any{"project_id": pid, "effective_from": "2026-01-01", "hourly_rate": "20"}, "Idempotency-Key", uuid.NewString())
	// A cost rate exists too: nothing the AI reads or drafts may carry it (G4-050).
	e.call(http.MethodPost, "/v1/cost-rates", e.as(authz.Owner), map[string]any{"user_id": priya.String(), "role": "Consultant", "effective_from": "2026-01-01", "hourly_rate": "73.31"}, "Idempotency-Key", uuid.NewString())

	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	turn := proposeTurn(e, arUser.String()) // who may draft a project's invoice: C on invoices and V on project reports
	propose := func(payload map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "ar.draft_invoice", "payload": payload, "rationale": "Its time is approved.",
			"model": "claude-opus-5-5", "prompt": "chat@4"}, ServiceTokenHeader, service)
	}
	good := map[string]any{"project": "AIB-1", "revenue_account": e.books.Sales.Code, "gst_code": "SR"}
	invoices := func() int {
		items, _ := e.call(http.MethodGet, "/v1/invoices", acc, nil).Body["items"].([]any)
		return len(items)
	}
	before := invoices()

	// The dry run shows the lines and the total, and posts and drafts nothing.
	made := propose(good)
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending ||
		!strings.HasPrefix(made.str("summary"), "Drafts an invoice to ") || !strings.Contains(made.str("summary"), "SGD 320.00 from 2 time entries and 0 costs of project AIB-1") {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	if strings.Contains(made.Raw, "73.31") || invoices() != before {
		t.Errorf("the dry run carried a cost rate or drafted an invoice: %s", made.Raw)
	}
	// Refusals: not a revenue account, no such project, the wrong shape; none makes a proposal that waits.
	for name, payload := range map[string]map[string]any{
		"a bank account": {"project": "AIB-1", "revenue_account": e.books.Bank.Code, "gst_code": "SR"},
		"no project":     {"project": "NOPE-9", "revenue_account": e.books.Sales.Code, "gst_code": "SR"},
		"a bad date":     {"project": "AIB-1", "revenue_account": e.books.Sales.Code, "gst_code": "SR", "date": "tomorrow"},
	} {
		if bad := propose(payload); bad.Status < 400 && bad.str("status") != storage.ProposalInvalid {
			t.Errorf("%s: %s", name, bad.Raw)
		}
	}
	// A person with no right to bill projects: the AI proposes nothing for them.
	if x := e.call(http.MethodPost, "/internal/v1/proposals", proposeTurn(e, priya.String()), map[string]any{"task_type": "ar.draft_invoice", "payload": good, "rationale": "x",
		"model": "m", "prompt": "chat@4"}, ServiceTokenHeader, service); x.Status < 400 && x.str("status") != storage.ProposalInvalid {
		t.Errorf("a draft for someone who may not bill: %s", x.Raw)
	}

	// Approved by another Owner, the executor drafts the invoice once, as the approver, and issues nothing.
	var pr storage.AIProposal
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pr, err = u.AIProposals().Get(ctx, uuid.MustParse(made.str("id")))
		return err
	})
	other := f.as(approver.String(), authz.Accountant) // a person who may do it themselves approves it (Appendix E)
	shown := e.call(http.MethodGet, "/v1/approvals/"+pr.ApprovalRequest.String(), other, nil)
	if d := f.decide(other, pr.ApprovalRequest.String(), "approve", hashOf(shown)); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	executor := proposal.Executor{Store: executorStore(t), Catalogue: proposal.Default(), Commands: proposal.Release, Clock: e.clock,
		Outbox: outbox.Dispatcher{Store: executorStore(t), Queue: e.queue, Clock: e.clock}}
	body, _ := json.Marshal(map[string]any{"proposal_id": pr.ID.String()})
	for range 2 {
		if err := executor.Work(context.Background(), e.books.Org, storage.OutboxRow{ID: uuid.New(), Class: storage.ClassExecutor, Kind: proposal.ExecutorKind, Payload: body}); err != nil {
			t.Fatal(err)
		}
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pr, err = u.AIProposals().Get(ctx, pr.ID)
		return err
	})
	if pr.Status != storage.ProposalExecuted || !strings.Contains(string(pr.Execution), "invoice:") || invoices() != before+1 {
		t.Fatalf("the executed proposal: %s %s (%d invoices, was %d)", pr.Status, pr.Execution, invoices(), before)
	}
	list := e.call(http.MethodGet, "/v1/invoices", acc, nil).Body["items"].([]any)
	var draft map[string]any
	for _, it := range list {
		if m := it.(map[string]any); m["status"] == "draft" {
			draft = m
		}
	}
	if draft == nil || draft["total"] == nil {
		t.Fatalf("the drafted invoice: %v", list)
	}
	inv := e.call(http.MethodGet, "/v1/invoices/"+draft["id"].(string), acc, nil)
	if inv.Status != 200 || inv.str("status") != "draft" || len(inv.Body["lines"].([]any)) != 1 || inv.str("total", "amount") != "320.00" && inv.str("total", "amount") != "348.80" {
		t.Errorf("the draft invoice: %d %s", inv.Status, inv.Raw)
	}
	if u := e.call(http.MethodGet, "/v1/projects/"+pid+"/unbilled", acc, nil); u.str("total", "amount") != "320.00" {
		t.Errorf("a draft bills nothing yet: %s", u.Raw)
	}

	// Work that changed since the dry run is Stale and makes nothing: a billable cost posts before the approval.
	second := propose(good)
	if second.Status != http.StatusCreated {
		t.Fatalf("a second proposal: %d %s", second.Status, second.Raw)
	}
	bd := e.billDraft("100.00", "0.00")
	bl := bd["lines"].([]map[string]any)[0]
	bl["billable"], bl["dimension_value_ids"] = true, []string{pid}
	bc := e.call(http.MethodPost, "/v1/bills", acc, bd)
	if bc.Status != 201 {
		t.Fatalf("the bill: %d %s", bc.Status, bc.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/bills/"+bc.str("id")+"/post", acc, nil, "If-Match", bc.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("posting the bill: %d %s", r.Status, r.Raw)
	}
	var pr2 storage.AIProposal
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pr2, err = u.AIProposals().Get(ctx, uuid.MustParse(second.str("id")))
		return err
	})
	shown2 := e.call(http.MethodGet, "/v1/approvals/"+pr2.ApprovalRequest.String(), other, nil)
	if d := f.decide(other, pr2.ApprovalRequest.String(), "approve", hashOf(shown2)); d.Status != http.StatusOK {
		t.Fatalf("approving the second: %d %s", d.Status, d.Raw)
	}
	body2, _ := json.Marshal(map[string]any{"proposal_id": pr2.ID.String()})
	if err := executor.Work(context.Background(), e.books.Org, storage.OutboxRow{ID: uuid.New(), Class: storage.ClassExecutor, Kind: proposal.ExecutorKind, Payload: body2}); err != nil {
		t.Fatal(err)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pr2, err = u.AIProposals().Get(ctx, pr2.ID)
		return err
	})
	if pr2.Status != storage.ProposalStale || invoices() != before+1 {
		t.Errorf("the second proposal after the work changed: %s %s (%d invoices)", pr2.Status, pr2.Reason, invoices())
	}
}
