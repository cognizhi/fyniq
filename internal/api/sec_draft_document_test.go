//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// sec.draft_document (Appendix E; p1-company-secretary task 4.2, design D6): the AI proposes a corporate document from
// one of Fyniq's templates. The dry run shows its text, filled from the records, and drafts nothing; a document that does
// not fit, or that no one in the records could sign, is invalid, naming the field. Once the Company secretary approves
// it, the executor drafts it as them, once, naming the proposal: a draft only, never signed, filed or recorded in the
// minute book. When a director is appointed after the dry run, the text a person approved is no longer what would be
// drafted: the proposal is stale and drafts nothing (AI-P5).
// req: ai/task-catalogue sec.draft_document Draft a corporate document
// req: ai/task-catalogue sec.draft_document Tier 1
// req: ai/task-catalogue sec.draft_document Maps to DraftCorporateDocument
// req: ai/task-catalogue sec.draft_document Draft only
// req: ai/task-catalogue sec.draft_document Approver is Owner or Company secretary
// req: FR-CS-05
// req: AI-11
// asvs: V8.2.2
func TestTheAIDraftsACorporateDocument(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC)) // before the sessions, so their step-up is fresh
	f := foundersIn(e)
	aiSwitchedOn(e)
	enableTaskTypes(e, "sec.draft_document", 1)
	prep, kow := f.as(f.weiling, authz.Accountant), f.as(f.ahkow)
	recordCompany(t, e, prep, kow)
	secretary, _ := companySecretary(e, f)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	turn := proposeTurn(e, f.ahkow)
	propose := func(payload map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "sec.draft_document", "payload": payload,
			"rationale": "The statements for 2026 are ready for the directors.", "model": "claude-opus-5-5", "prompt": "chat@3"}, ServiceTokenHeader, service)
	}
	documents := func() []any {
		docs, _ := e.call(http.MethodGet, "/v1/corporate-profile", prep, nil).Body["documents"].([]any)
		return docs
	}
	get := func(id string) storage.AIProposal {
		t.Helper()
		var pr storage.AIProposal
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			pr, err = u.AIProposals().Get(ctx, uuid.MustParse(id))
			return err
		})
		return pr
	}
	executor := proposal.Executor{Store: executorStore(t), Catalogue: proposal.Default(), Commands: proposal.Release, Clock: e.clock,
		Outbox: outbox.Dispatcher{Store: executorStore(t), Queue: e.queue, Clock: e.clock}}
	execute := func(pr storage.AIProposal) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"proposal_id": pr.ID.String()})
		if err := executor.Work(context.Background(), e.books.Org, storage.OutboxRow{ID: uuid.New(), Class: storage.ClassExecutor, Kind: proposal.ExecutorKind,
			Payload: body}); err != nil {
			t.Fatal(err)
		}
	}
	approve := func(pr storage.AIProposal) {
		t.Helper()
		shown := e.call(http.MethodGet, "/v1/approvals/"+pr.ApprovalRequest.String(), secretary, nil)
		if d := f.decide(secretary, pr.ApprovalRequest.String(), "approve", hashOf(shown)); d.Status != http.StatusOK {
			t.Fatalf("the Company secretary approving: %d %s", d.Status, d.Raw)
		}
	}

	// The dry run shows the text and drafts nothing.
	made := propose(map[string]any{"kind": "directors.approve_statements", "fields": map[string]any{"fy_end": "2026-12-31"}})
	if made.Status != http.StatusCreated || made.str("status") != storage.ProposalPending || made.str("summary") != "Drafts the directors' resolution in writing "+
		"(directors.approve_statements) from the records, for the directors and the company secretary to review; nothing is signed or filed" {
		t.Fatalf("the proposal: %d %s", made.Status, made.Raw)
	}
	if impact := string(get(made.str("id")).Impact); !strings.Contains(impact, "That the financial statements of the company for the financial year ended 31 December 2026 be approved.") ||
		!strings.Contains(impact, `"change": "draft"`) {
		t.Errorf("the dry run's text: %s", impact)
	}
	if len(documents()) != 0 {
		t.Fatal("a document was drafted before anyone approved it")
	}
	if bad := propose(map[string]any{"kind": "directors.approve_statements", "fields": map[string]any{"place": "Here"}}); bad.str("status") != storage.ProposalInvalid ||
		!strings.Contains(bad.str("reason"), "payload.fields.place") || !strings.Contains(bad.str("reason"), "payload.fields.fy_end") {
		t.Errorf("fields its kind does not ask for: %s", bad.Raw)
	}
	if none := propose(map[string]any{"kind": "members.written_resolution", "fields": map[string]any{"resolution_kind": "special",
		"resolutions": []string{"That the name be changed."}, "circulated_on": "2027-01-05"}}); none.str("status") != storage.ProposalInvalid ||
		!strings.Contains(none.str("reason"), "no member holds shares") {
		t.Errorf("a members' resolution with no member: %s", none.Raw)
	}

	// Approved by the Company secretary, the executor drafts it once, as them.
	pr := get(made.str("id"))
	approve(pr)
	execute(pr)
	execute(pr)
	pr = get(pr.ID.String())
	docs := documents()
	if pr.Status != storage.ProposalExecuted || len(docs) != 1 {
		t.Fatalf("executed: %s %s, %d documents", pr.Status, pr.Reason, len(docs))
	}
	doc := docs[0].(map[string]any)
	if doc["status"] != "draft" || doc["proposal_id"] != pr.ID.String() || doc["kind"] != "directors.approve_statements" ||
		!strings.Contains(string(pr.Execution), "corporate_document:"+doc["id"].(string)) {
		t.Errorf("the draft: %v; the execution %s", doc, pr.Execution)
	}

	// A director appointed after the dry run changes the text approved: the proposal is stale.
	second := propose(map[string]any{"kind": "directors.approve_statements", "fields": map[string]any{"fy_end": "2025-12-31"}})
	pr = get(second.str("id"))
	approve(pr)
	d := e.call(http.MethodPost, "/v1/corporate-profile/officers", prep, map[string]any{"role": "director", "name": "Sarah Ng", "kind": "individual",
		"appointed_on": "2027-01-05", "nominee": false})
	at := "/v1/corporate-profile/officers/" + d.str("id")
	e.upload(at+"/evidence", prep, map[string]string{"evidence_type": "Consent to act"}, "consent.pdf", []byte("%PDF-1.7 consent"), "If-Match", d.Header.Get("ETag"))
	cur := e.call(http.MethodGet, at, prep, nil)
	req := e.call(http.MethodPost, at+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", kow, hashOf(req), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("appointing Sarah Ng: %d %s", r.Status, r.Raw)
	}
	execute(pr)
	if pr = get(pr.ID.String()); pr.Status != storage.ProposalStale || len(documents()) != 1 {
		t.Errorf("after the text changed: %s %s, %d documents", pr.Status, pr.Reason, len(documents()))
	}
}
