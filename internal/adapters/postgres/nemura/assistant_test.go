package nemura_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/assistant"
)

func aiCode(err error) string {
	var r *assistant.Refusal
	if err != nil && asRefusal(err, &r) {
		return r.Code
	}
	return ""
}

func asRefusal(err error, r **assistant.Refusal) bool {
	for err != nil {
		if x, ok := err.(*assistant.Refusal); ok {
			*r = x
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// The people assistant is off until the Owner turns it on, with a route and the task types; a proposal is inserted by its own role, which can write nothing else; a proposal is decided by a named human
// other than the person it acted for, within 72 hours, with the final text and its hash; what was proposed never changes; nothing but the hashes is in the audit trail (NM-AI-01, NM-AI-04, NM-ARC-06).
// req: NM-AI-01, NM-AI-04, NM-ARC-06
// asvs: V8.2.1, V8.3.1, V1.4.1
func TestThePeopleAssistantIsOffUntilTurnedOnAndOnlyEverInsertsProposalsAHumanDecides(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	runtime := store(t, "nemura_assistant")
	svc := nemura.AssistantService{Store: st, Clock: clock.NewFrozen(now), AllowScripted: true}
	releaseSvc := nemura.AssistantService{Store: st, Clock: clock.NewFrozen(now)}
	owner := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	hr := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	asker := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	reqID := uuid.New().String()
	secret := "ZXQ-CONFIDENTIAL-REASON"
	propose := func(task string, payload map[string]any) (uuid.UUID, error) {
		var id uuid.UUID
		err := runtime.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, err = nemura.Propose(ctx, tx, lt.c.org, now, nemura.ProposalInput{Asker: lt.benLogin, Task: task, Payload: payload, Rationale: "From the record.", ModelID: "scripted", PromptVersion: "people-assistant@1"})
			return err
		})
		return id, err
	}
	letter := map[string]any{"request_id": reqID, "fields": map[string]any{"job_title": "Analyst", "purpose": secret}}
	// Off by default.
	if s, err := svc.Settings(ctx, owner); err != nil || s.Enabled || s.Route != "none" || len(s.TaskTypes) != 0 || s.OwnPay || len(s.Catalogue) != 2 || !strings.Contains(s.OwnPayNote, "second approver") {
		t.Fatalf("default settings: %+v %v", s, err)
	}
	if _, err := propose("people.draft_letter", letter); aiCode(err) != assistant.CodeOff {
		t.Errorf("a proposal while the assistant is off: %v", err)
	}
	// What the settings refuse, in words.
	for name, tc := range map[string]struct {
		svc  nemura.AssistantService
		in   api.AISettingsInput
		text string
	}{
		"a task type outside the catalogue": {svc, api.AISettingsInput{Enabled: true, Route: "scripted", TaskTypes: []string{"people.approve_leave"}}, "not a task type of the catalogue"},
		"own pay":                           {svc, api.AISettingsInput{Enabled: true, Route: "scripted", TaskTypes: []string{"people.draft_letter"}, OwnPay: true}, "second approver"},
		"the scripted route in a release":   {releaseSvc, api.AISettingsInput{Enabled: true, Route: "scripted", TaskTypes: []string{"people.draft_letter"}}, "local mode only"},
		"on with no route":                  {svc, api.AISettingsInput{Enabled: true, Route: "none", TaskTypes: []string{"people.draft_letter"}}, "needs a model route"},
		"a route nobody evidenced":          {svc, api.AISettingsInput{Enabled: true, Route: "gemini", TaskTypes: []string{"people.draft_letter"}}, "evidenced in Singapore"},
	} {
		if err := tc.svc.SetSettings(ctx, owner, tc.in); aiCode(err) != assistant.CodeInvalid || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: %v", name, err)
		}
	}
	su := connect(t, db.SuperURL())
	if _, err := su.Exec(ctx, "INSERT INTO nemura.ai_settings (organisation_id, own_pay, updated_by, updated_at) VALUES ($1, true, $2, now())", lt.c.org, lt.c.owner); err == nil || !strings.Contains(err.Error(), "own_pay_needs_a_second_approver") {
		t.Errorf("the database lets own pay be turned on: %v", err)
	}
	if err := svc.SetSettings(ctx, owner, api.AISettingsInput{Enabled: true, Route: "scripted", TaskTypes: []string{"people.draft_letter"}}); err != nil {
		t.Fatal(err)
	}
	// Proposals: only what the catalogue holds and the Owner turned on, in the strict shape.
	if _, err := propose("people.draft_fwa_reply", map[string]any{"request_id": reqID, "reply": "Thank you."}); aiCode(err) != assistant.CodeOff {
		t.Errorf("a task type that is not turned on: %v", err)
	}
	if _, err := propose("people.approve_leave", letter); aiCode(err) != assistant.CodeNever {
		t.Errorf("a task the never-list names: %v", err)
	}
	if _, err := propose("people.draft_poem", letter); aiCode(err) != assistant.CodeInvalid {
		t.Errorf("a task that is not in the catalogue: %v", err)
	}
	if _, err := propose("people.draft_letter", map[string]any{"request_id": reqID, "fields": map[string]any{"a": "b"}, "approve": true}); aiCode(err) != assistant.CodeInvalid {
		t.Errorf("a payload with a field it may not have: %v", err)
	}
	id, err := propose("people.draft_letter", letter)
	if err != nil {
		t.Fatal(err)
	}
	// The assistant's role holds nothing else: it reads no people data, not even its own proposals, and changes nothing.
	for _, q := range []string{"SELECT count(*) FROM nemura.person", "SELECT count(*) FROM nemura.ai_proposal", "UPDATE nemura.ai_proposal SET state = 'approved'", "DELETE FROM nemura.ai_proposal",
		"INSERT INTO nemura.letter_request (id) VALUES (uuidv7())", "UPDATE nemura.person_pay SET basic_monthly = 1", "INSERT INTO nemura.ai_settings (enabled, route, updated_by, updated_at) VALUES (true, 'scripted', uuidv7(), now())"} {
		err := runtime.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("the assistant's role ran %q: %v", q, err)
		}
	}
	// The queue.
	list, err := svc.Proposals(ctx, hr, "pending")
	if err != nil || len(list) != 1 || list[0].Task != "people.draft_letter" || list[0].Asker == "" || list[0].Expired || list[0].State != "pending" || list[0].ExpiresAt == "" {
		t.Fatalf("pending: %+v %v", list, err)
	}
	// Deciding: not the person it acted for; a rejection needs a reason; an edit is checked against the shape.
	if err := svc.Decide(ctx, asker, id, api.AIDecision{Approve: true}); aiCode(err) != assistant.CodeRole {
		t.Errorf("the person the assistant acted for deciding: %v", err)
	}
	if err := svc.Decide(ctx, hr, id, api.AIDecision{Approve: false}); aiCode(err) != assistant.CodeInvalid {
		t.Errorf("a rejection with no reason: %v", err)
	}
	if err := svc.Decide(ctx, hr, id, api.AIDecision{Approve: true, Final: map[string]any{"request_id": reqID, "fields": map[string]any{"a": "b"}, "issue": true}}); aiCode(err) != assistant.CodeInvalid {
		t.Errorf("an edit that adds a field: %v", err)
	}
	edited := map[string]any{"request_id": reqID, "fields": map[string]any{"job_title": "Senior Analyst", "purpose": secret}}
	if err := svc.Decide(ctx, hr, id, api.AIDecision{Approve: true, Final: edited}); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Proposal(ctx, hr, id)
	if err != nil || p.State != "approved" || p.DecidedBy == "" || p.Final["fields"].(map[string]any)["job_title"] != "Senior Analyst" || p.Payload["fields"].(map[string]any)["job_title"] != "Analyst" {
		t.Fatalf("approved: %+v %v", p, err)
	}
	if err := svc.Decide(ctx, hr, id, api.AIDecision{Approve: false, Reason: "No."}); aiCode(err) != assistant.CodeConflict {
		t.Errorf("deciding twice: %v", err)
	}
	// Expiry: a proposal that waited more than 72 hours cannot be approved.
	id2, err := propose("people.draft_letter", letter)
	if err != nil {
		t.Fatal(err)
	}
	late := now.Add(73 * time.Hour)
	err = st.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideProposal(ctx, tx, lt.c.org, late, lt.c.pay, []string{"hr_officer"}, id2, true, "", nil)
	})
	if aiCode(err) != assistant.CodeConflict || !strings.Contains(err.Error(), "72 hours") {
		t.Errorf("approving after 72 hours: %v", err)
	}
	if l, _ := (nemura.AssistantService{Store: st, Clock: clock.NewFrozen(late)}).Proposals(ctx, hr, "pending"); len(l) != 1 || !l[0].Expired {
		t.Errorf("an expired proposal is marked: %+v", l)
	}
	// Evidence: what the assistant proposed never changes, nothing is deleted, no step is skipped, and the executor applies only what was approved.
	for _, q := range []string{"UPDATE nemura.ai_proposal SET payload = '{}'::jsonb", "DELETE FROM nemura.ai_proposal", "UPDATE nemura.ai_proposal SET state = 'applied', applied_at = now() WHERE state = 'pending'", "UPDATE nemura.ai_proposal SET final_payload = '{}'::jsonb WHERE state = 'approved'"} {
		if _, err := su.Exec(ctx, q); err == nil || !strings.Contains(err.Error(), "NM178") {
			t.Errorf("%s: %v", q, err)
		}
	}
	executor := store(t, "nemura_executor")
	if err := executor.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE nemura.ai_proposal SET state = 'applied', applied_at = $2 WHERE id = $1", id2, now)
		return err
	}); err == nil {
		t.Error("the executor applied a proposal nobody approved")
	}
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%'||$2||'%' OR body LIKE '%Senior Analyst%')`, lt.c.org, secret).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("the text of a draft in the audit trail: %d %v", leaks, err)
	}
	var proposed, approved int
	_ = su.QueryRow(ctx, `SELECT count(*) FILTER (WHERE body LIKE '%nemura.ai_proposal.proposed%'), count(*) FILTER (WHERE body LIKE '%nemura.ai_proposal.approved%') FROM nemura.audit_event WHERE organisation_id = $1`, lt.c.org).Scan(&proposed, &approved)
	if proposed != 2 || approved != 1 {
		t.Errorf("audit events: %d proposed, %d approved", proposed, approved)
	}
}
