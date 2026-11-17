//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// matrixProposals are the authorisation matrix's rows for the AI's proposals and catalogue: each read as the caller's
// person.
var matrixProposals = map[string]func(e *env, tok string) (response, func() bool){
	"listAIProposals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/proposals", steppedUp(e, tok), nil), nil
	},
	"getAIProposal": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/proposals/"+uuid.NewString(), steppedUp(e, tok), nil), nil
	},
	"listAITaskTypes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/task-types", steppedUp(e, tok), nil), nil
	},
}

// A proposal as people review it (AI-07; design D5): the summary is the platform's, from the dry run, whatever the
// rationale says, which is shown apart as AI text; the items, the impact, the payload and its hash, the evidence and
// whether you may sign. Each member sees those made for them and those they may sign, the Owner and the DPO every one,
// the AI none. The AI tasks page lists the catalogue: each task type's tier, limits, approvers, and whether it is on and
// built (§9.6).
// req: AI-07
// req: ai/task-catalogue AI tasks page
// req: AI-32
// asvs: V8.2.1, V8.2.2
func TestTheProposalsAsPeopleReviewThem(t *testing.T) {
	e := newEnv(t)
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Staff", "roles": []string{"employee"}}).str("code")
	f := foundersIn(e)
	staffID := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "staff-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Staff"}).str("user_id")
	aiSwitchedOn(e)
	e.api.deps.Catalogue = fixtureCatalogue(t, "test.annotate_note", "test.tag_note")
	e.api.deps.Commands = proposal.Commands{"AnnotateNote": noteCommand{}}
	enableTaskTypes(e, "test.annotate_note", 1)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	staff := e.principal(authn.Principal{Subject: "staff", UserID: uuid.MustParse(staffID), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Employee}})
	turn := proposeTurn(e, f.ahkow)
	made := e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": "test.annotate_note",
		"payload": map[string]any{"note": "Book the room"}, "rationale": "This only adds a note, nothing else.", "evidence": []map[string]any{
			{"kind": "task", "id": "agm-2027", "title": "Hold the AGM"}}, "model": "claude-opus-5-5", "prompt": "chat@3"}, ServiceTokenHeader, service)
	if made.Status != http.StatusCreated {
		t.Fatalf("proposing: %d %s", made.Status, made.Raw)
	}
	id := made.str("id")

	// The review: the platform's summary tells the truth, whatever the rationale says (G4-095).
	d := e.call(http.MethodGet, "/v1/ai/proposals/"+id, weiling, nil)
	rationale, _ := d.Body["rationale"].(map[string]any)
	if d.Status != http.StatusOK || d.str("summary") != "Creates the customer Book the room" || rationale["origin"] != "ai" ||
		rationale["text"] != "This only adds a note, nothing else." || d.Body["you_may_approve"] != true || d.str("payload_sha256") != made.str("payload_sha256") ||
		d.str("approval_id") == "" || len(d.Body["evidence"].([]any)) != 1 || !strings.Contains(d.Raw, `"legal_name":"Book the room"`) {
		t.Fatalf("the review: %d %s", d.Status, d.Raw)
	}
	if strings.Contains(d.str("summary"), "only adds a note") {
		t.Fatal("the model's words reached the summary")
	}

	// Who sees it: its requester and the Owners; not the staff member, nor the AI.
	for who, tok := range map[string]string{"requester": ahkow, "another Owner": weiling} {
		l := e.call(http.MethodGet, "/v1/ai/proposals?status=pending", tok, nil)
		if items, _ := l.Body["items"].([]any); l.Status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["id"] != id {
			t.Errorf("%s's list: %d %s", who, l.Status, l.Raw)
		}
	}
	if l := e.call(http.MethodGet, "/v1/ai/proposals", staff, nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 0 {
		t.Errorf("the staff member's list: %d %s", l.Status, l.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/ai/proposals/"+id, staff, nil), "getAIProposal", 404, CodeNotFound)
	if r := e.call(http.MethodGet, "/v1/ai/proposals", turn, nil); r.Status != http.StatusForbidden {
		t.Errorf("the AI's list: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/ai/proposals?status=executed", weiling, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("filtered by state: %s", l.Raw)
	}

	// The catalogue: tier, limits, approvers, on and built.
	c := e.call(http.MethodGet, "/v1/ai/task-types", staff, nil)
	items, _ := c.Body["task_types"].([]any)
	if c.Status != http.StatusOK || len(items) != 2 {
		t.Fatalf("the catalogue: %d %s", c.Status, c.Raw)
	}
	for _, x := range items {
		tt := x.(map[string]any)
		on := tt["id"] == "test.annotate_note"
		if tt["on"] != on || tt["built"] != true || tt["tier"] != float64(1) || tt["max_items"] != float64(5) || tt["expiry_hours"] != float64(72) ||
			tt["approvers"].([]any)[0] != "owner" {
			t.Errorf("a task type: %v", tt)
		}
	}

	// Decided, it stays in its requester's and decider's history.
	var req string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		pr, err := u.AIProposals().Get(ctx, uuid.MustParse(id))
		req = pr.ApprovalRequest.String()
		return err
	})
	shown := e.call(http.MethodGet, "/v1/approvals/"+req, weiling, nil)
	if r := f.decide(weiling, req, "reject", map[string]any{"envelope_hash": shown.str("envelope_hash"), "reason": "Not this year"}); r.Status != http.StatusOK {
		t.Fatalf("rejecting: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/ai/proposals/"+id, ahkow, nil); g.str("status") != storage.ProposalRejected || g.str("reason") != "Not this year" ||
		g.Body["you_may_approve"] != false || g.str("decided_by") != f.weiling {
		t.Errorf("the decided proposal: %s", g.Raw)
	}
}
