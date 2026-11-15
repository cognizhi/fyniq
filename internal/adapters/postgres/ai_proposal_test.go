//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// proposalBooks is an organisation with a member and a turn of theirs to propose from.
type proposalBooks struct {
	porttest.Books
	user, turn uuid.UUID
	now        time.Time
}

func newProposalBooks(t *testing.T, name string) proposalBooks {
	t.Helper()
	b := books(t, name)
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	pb := proposalBooks{Books: b, user: founder(t, st, b.Org, 7, now), now: now}
	if err := st.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.AIConversations().Create(ctx, storage.AIConversation{UserID: pb.user, Title: "Tasks", CreatedAt: now})
		if err != nil {
			return err
		}
		turn, err := u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: pb.user, Question: "Remind me of the AGM", AskedAt: now})
		pb.turn = turn.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return pb
}

func hexSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// draft is a test-only task type's proposal from b's turn.
func (b proposalBooks) draft(payload string, items int) storage.AIProposal {
	turn := b.turn
	return storage.AIProposal{TaskType: "test.annotate_note", TaskVersion: 1, TaskSHA256: hexSHA("test.annotate_note@1"), Payload: payload,
		PayloadSHA256: hexSHA(payload), ItemCount: items, Rationale: "The AGM is due on 30 June.", Evidence: []storage.AIEvidence{{Kind: "task",
			ID: "agm-2027", Title: "Hold the AGM"}}, RiskTier: 1, RequiredApprovals: 1, Agent: "chat", RequestedBy: b.user, Turn: &turn,
		Model: "claude-opus-5-5", Prompt: "chat@2", ToolsSHA256: hexSHA("tools"), ProposedAt: b.now, ExpiresAt: b.now.Add(72 * time.Hour)}
}

// run runs fn in a unit of work on b's organisation as role.
func (b proposalBooks) run(t *testing.T, role string, fn func(storage.AIProposals) (storage.AIProposal, error)) (storage.AIProposal, error) {
	t.Helper()
	var out storage.AIProposal
	err := store(t, role).Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := fn(u.AIProposals())
		out = p
		return err
	})
	return out, err
}

func (b proposalBooks) must(t *testing.T, role string, fn func(storage.AIProposals) (storage.AIProposal, error)) storage.AIProposal {
	t.Helper()
	p, err := b.run(t, role, fn)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A proposal moves through its lifecycle, each step by its own role and audited in its transaction: the API makes a
// Draft and moves it to Invalid or Pending with its dry run, then to Approved (a batch's items re-hashed), Rejected or
// Superseded; the worker expires it once past its expiry; the executor alone runs it to Executed, Stale or Failed. What
// it proposed, its payload exactly and its hash, its rationale, evidence, tier and originator, is kept as made.
// req: ai/proposals Proposal lifecycle
// req: ai/proposals Proposal content and payload hash
// req: ai/proposals Proposal rationale, evidence and dry-run impact
// req: ai/proposals Proposal tier, originator, approvals and execution record
func TestAIProposalsMoveThroughTheirLifecycle(t *testing.T) {
	ctx := context.Background()
	b := newProposalBooks(t, "Proposals Pte Ltd")
	payload := `{"items":[{"note":"Book the room"},{"note":"Send the notice"}]}`
	impact := json.RawMessage(`{"documents":[{"kind":"task","change":"create"}]}`)
	base := map[string]int64{"task:agm-2027": 3}

	// Made, it is a Draft holding exactly what it proposes; the database checked the payload's hash.
	d := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, b.draft(payload, 2)) })
	if d.Status != storage.ProposalDraft || d.Version != 1 || d.Payload != payload || d.PayloadSHA256 != hexSHA(payload) || d.ItemCount != 2 ||
		d.Turn == nil || *d.Turn != b.turn || d.RequestedBy != b.user || d.Agent != "chat" || d.Prompt != "chat@2" || len(d.Evidence) != 1 ||
		!d.ExpiresAt.Equal(b.now.Add(72*time.Hour)) {
		t.Fatalf("the draft: %+v", d)
	}

	// Pending with its dry run, then approved for one of its two items: the subset is what executes, re-hashed.
	p := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Pending(ctx, d.ID, 1, impact, base, nil)
	})
	if p.Status != storage.ProposalPending || p.Version != 2 || p.BaseVersions["task:agm-2027"] != 3 || !strings.Contains(string(p.Impact), "documents") {
		t.Fatalf("pending: %+v", p)
	}
	subset := `{"items":[{"note":"Send the notice"}]}`
	a := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Approve(ctx, d.ID, 2, b.user, []int{2}, subset, b.now.Add(time.Minute))
	})
	if a.Status != storage.ProposalApproved || a.ApprovedPayload != subset || a.ApprovedSHA256 != hexSHA(subset) || len(a.ApprovedItems) != 1 ||
		a.ApprovedItems[0] != 2 || a.DecidedBy == nil || *a.DecidedBy != b.user || a.Payload != payload {
		t.Fatalf("approved: %+v", a)
	}

	// Only the executor runs it; another step at a version read before is a conflict.
	if _, err := b.run(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Start(ctx, d.ID, 3, b.now.Add(2*time.Minute))
	}); err == nil {
		t.Error("the API started an approved proposal")
	}
	if _, err := b.run(t, "fyniq_executor", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Start(ctx, d.ID, 2, b.now.Add(2*time.Minute))
	}); storage.CodeOf(err) != storage.CodeVersion {
		t.Errorf("a step at an old version: %v", err)
	}
	e := b.must(t, "fyniq_executor", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Start(ctx, d.ID, 3, b.now.Add(2*time.Minute))
	})
	execution := json.RawMessage(`{"run":"0193","records":["task:agm-2027-notice"],"entries":[]}`)
	x := b.must(t, "fyniq_executor", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Finish(ctx, d.ID, e.Version, storage.ProposalExecuted, "", execution, b.now.Add(3*time.Minute))
	})
	if x.Status != storage.ProposalExecuted || x.StartedAt == nil || x.FinishedAt == nil || !strings.Contains(string(x.Execution), "agm-2027-notice") {
		t.Fatalf("executed: %+v", x)
	}
	// Executed is final.
	if _, err := b.run(t, "fyniq_executor", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Finish(ctx, d.ID, x.Version, storage.ProposalFailed, "again", execution, b.now.Add(4*time.Minute))
	}); storage.CodeOf(err) != storage.CodeAIProposal {
		t.Errorf("an executed proposal moved on: %v", err)
	}

	// Invalid with why; rejected with a person's reason; superseded by its revision; Stale and Failed with why.
	inv := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Create(ctx, b.draft(`{"note":"x"}`, 0))
	})
	if got := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Invalid(ctx, inv.ID, 1, "note: is shorter than 2 characters")
	}); got.Status != storage.ProposalInvalid || got.Reason == "" {
		t.Errorf("invalid: %+v", got)
	}
	pending := func(body string) storage.AIProposal {
		t.Helper()
		c := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, b.draft(body, 0)) })
		return b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
			return r.Pending(ctx, c.ID, 1, impact, base, nil)
		})
	}
	rej := pending(`{"note":"Reject me"}`)
	if got := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Reject(ctx, rej.ID, rej.Version, b.user, "Not this year", b.now.Add(time.Minute))
	}); got.Status != storage.ProposalRejected || got.Reason != "Not this year" || got.ApprovedPayload != "" {
		t.Errorf("rejected: %+v", got)
	}
	old, revision := pending(`{"note":"First"}`), pending(`{"note":"Second"}`)
	if got := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Supersede(ctx, old.ID, old.Version, revision.ID, b.now.Add(time.Minute))
	}); got.Status != storage.ProposalSuperseded || got.SupersededBy == nil || *got.SupersededBy != revision.ID {
		t.Errorf("superseded: %+v", got)
	}
	for _, end := range []string{storage.ProposalStale, storage.ProposalFailed} {
		q := pending(`{"note":"` + end + `"}`)
		q = b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
			return r.Approve(ctx, q.ID, q.Version, b.user, nil, q.Payload, b.now.Add(time.Minute))
		})
		q = b.must(t, "fyniq_executor", func(r storage.AIProposals) (storage.AIProposal, error) {
			return r.Start(ctx, q.ID, q.Version, b.now.Add(time.Minute))
		})
		if _, err := b.run(t, "fyniq_executor", func(r storage.AIProposals) (storage.AIProposal, error) {
			return r.Finish(ctx, q.ID, q.Version, end, "", json.RawMessage(`{"error":"x"}`), b.now.Add(time.Minute))
		}); err == nil {
			t.Errorf("%s without a reason", end)
		}
		if got := b.must(t, "fyniq_executor", func(r storage.AIProposals) (storage.AIProposal, error) {
			return r.Finish(ctx, q.ID, q.Version, end, "task:agm-2027 changed since the dry run", json.RawMessage(`{"error":"stale"}`), b.now.Add(time.Minute))
		}); got.Status != end {
			t.Errorf("%s: %+v", end, got)
		}
	}

	// The worker expires a Pending proposal once past its expiry, and not before; the API cannot.
	exp := pending(`{"note":"Expire me"}`)
	if _, err := b.run(t, "fyniq_worker", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Expire(ctx, exp.ID, exp.Version, b.now.Add(71*time.Hour))
	}); err == nil {
		t.Error("expired before its expiry")
	}
	if _, err := b.run(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Expire(ctx, exp.ID, exp.Version, b.now.Add(72*time.Hour))
	}); err == nil {
		t.Error("the API expired a proposal")
	}
	if got := b.must(t, "fyniq_worker", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Expire(ctx, exp.ID, exp.Version, b.now.Add(72*time.Hour))
	}); got.Status != storage.ProposalExpired {
		t.Errorf("expired: %+v", got)
	}
	// An approval at or after the expiry is refused.
	late := pending(`{"note":"Too late"}`)
	if _, err := b.run(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Approve(ctx, late.ID, late.Version, b.user, nil, late.Payload, b.now.Add(72*time.Hour))
	}); err == nil {
		t.Error("approved at its expiry")
	}

	// Lists by status and by the person; every step is in the trail; another organisation sees none of it.
	var waiting []storage.AIProposal
	b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		var err error
		waiting, err = r.List(ctx, storage.AIProposalQuery{Statuses: []string{storage.ProposalPending}, RequestedBy: &b.user, Limit: 50})
		return storage.AIProposal{}, err
	})
	if len(waiting) != 2 || waiting[0].ID != late.ID { // the revision and the late one, newest first
		t.Errorf("pending: %d %v", len(waiting), waiting)
	}
	trail := ""
	for _, ev := range events(t, b.Org) {
		trail += ev.Body
	}
	for _, action := range []string{"proposed", "pending", "approved", "executing", "executed", "invalid", "rejected", "superseded", "stale",
		"failed", "expired"} {
		if !strings.Contains(trail, `"action":"ai_proposal.`+action+`"`) {
			t.Errorf("no ai_proposal.%s in the trail", action)
		}
	}
	if strings.Contains(trail, "Book the room") {
		t.Error("the trail holds the payload, not its hash")
	}
	// The conversation's purge after retention removes the turn; the proposals keep its ID.
	if err := store(t, "fyniq_worker").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, turns, err := u.AIConversations().Purge(ctx, b.now.Add(time.Hour))
		if err == nil && turns != 1 {
			t.Errorf("the purge removed %d turns", turns)
		}
		return err
	}); err != nil {
		t.Fatalf("purging the conversation a proposal came from: %v", err)
	}
	if kept := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Get(ctx, d.ID) }); kept.Turn == nil ||
		*kept.Turn != b.turn {
		t.Errorf("after the purge: %+v", kept)
	}

	other := books(t, "Proposals B Pte Ltd")
	if err := store(t, "fyniq_api").Do(signedIn(), other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIProposals().Get(ctx, d.ID)
		return err
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another organisation's proposal: %v", err)
	}
}

// The database keeps a proposal to its course whatever SQL is sent (migration 000375): a proposal made in any state but
// Draft, a payload whose hash is not its own, a change to what it proposed or to a decision made, a step out of the
// lifecycle, a step by another role than its own, a delete or a truncate, and a step past a final state are refused; the
// AI's role reads nothing.
// req: ai/proposals Proposal lifecycle
// req: ai/proposals Proposal content and payload hash
// req: ai/proposals The orchestrator holds no write credential
// req: CMP-AI-01
// asvs: V8.2.2, V8.3.1
func TestAttackAIProposalsKeepTheirCourse(t *testing.T) {
	ctx := context.Background()
	b := newProposalBooks(t, "Attack proposals Pte Ltd")
	payload := `{"note":"Hold the AGM"}`
	d := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, b.draft(payload, 0)) })
	insert := `INSERT INTO fyniq.ai_proposal (task_type, task_version, task_sha256, payload, payload_sha256, risk_tier, agent, requested_by,
	  turn_id, model, prompt, tools_sha256, proposed_at, expires_at) VALUES ('test.annotate_note', 1, $1, $2, $3, 1, 'chat', $4, $5, 'm',
	  'chat@2', $1, now(), now() + interval '1 hour')`
	for _, c := range []struct {
		what  string
		role  string
		stmt  string
		args  []any
		state []string
	}{
		{"a payload whose hash is not its own", "fyniq_api", insert, []any{hexSHA("x"), payload, hexSHA("other"), b.user, b.turn}, []string{"23514"}},
		{"a payload that is no JSON object", "fyniq_api", insert, []any{hexSHA("x"), `[1]`, hexSHA(`[1]`), b.user, b.turn}, []string{"23514"}},
		{"an expiry past 72 hours", "fyniq_api", strings.Replace(insert, "interval '1 hour'", "interval '73 hours'", 1),
			[]any{hexSHA("x"), payload, hexSHA(payload), b.user, b.turn}, []string{"23514"}},
		{"a proposal made approved", "fyniq_migrator", strings.Replace(strings.Replace(insert, "expires_at)", "expires_at, status)", 1),
			"'1 hour')", "'1 hour', 'approved')", 1), []any{hexSHA("x"), payload, hexSHA(payload), b.user, b.turn}, []string{"FY342"}},
		{"the API changing the payload", "fyniq_api", `UPDATE fyniq.ai_proposal SET payload = '{"note":"Pay me"}' WHERE id = $1`,
			[]any{d.ID}, []string{"42501"}},
		{"changing the payload and its hash", "fyniq_migrator", `UPDATE fyniq.ai_proposal SET payload = '{"note":"Pay me"}',
			payload_sha256 = $2, version = version + 1 WHERE id = $1`, []any{d.ID, hexSHA(`{"note":"Pay me"}`)}, []string{"FY342"}},
		{"a Draft approved without a dry run", "fyniq_api", `UPDATE fyniq.ai_proposal SET status = 'approved', decided_by = $2,
			decided_at = now(), approved_payload = payload, approved_sha256 = payload_sha256, impact = '{}', version = version + 1 WHERE id = $1`,
			[]any{d.ID, b.user}, []string{"FY342"}},
		{"the executor ending a Draft", "fyniq_executor", `UPDATE fyniq.ai_proposal SET status = 'invalid', reason = 'x', version = version + 1
			WHERE id = $1`, []any{d.ID}, []string{"FY342"}},
		{"the worker deciding", "fyniq_worker", `UPDATE fyniq.ai_proposal SET decided_by = $2 WHERE id = $1`, []any{d.ID, b.user},
			[]string{"42501"}},
		{"a step that skips a version", "fyniq_api", `UPDATE fyniq.ai_proposal SET status = 'invalid', reason = 'x', version = version + 2
			WHERE id = $1`, []any{d.ID}, []string{"FY342"}},
		{"the API deleting a proposal", "fyniq_api", `DELETE FROM fyniq.ai_proposal WHERE id = $1`, []any{d.ID}, []string{"42501"}},
		{"deleting a proposal", "fyniq_migrator", `DELETE FROM fyniq.ai_proposal WHERE id = $1`, []any{d.ID}, []string{"FY342"}},
		{"truncating the proposals", "fyniq_migrator", `TRUNCATE fyniq.ai_proposal CASCADE`, nil, []string{"FY060"}},
		{"the AI reading proposals", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_proposal`, nil, []string{"42501"}},
		{"the AI making a proposal", "fyniq_ai", insert, []any{hexSHA("x"), payload, hexSHA(payload), b.user, b.turn}, []string{"42501"}},
		{"a proposal from a turn that is none", "fyniq_api", insert, []any{hexSHA("x"), payload, hexSHA(payload), b.user, uuid.New()},
			[]string{"FY342"}},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.state...)
	}

	// Once approved, the decision is kept, and only the executor moves it on.
	p := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Pending(ctx, d.ID, 1, json.RawMessage(`{}`), nil, nil)
	})
	p = b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Approve(ctx, p.ID, p.Version, b.user, nil, payload, b.now.Add(time.Minute))
	})
	for _, c := range []struct {
		what  string
		role  string
		stmt  string
		state []string
	}{
		{"changing what was approved", "fyniq_api", `UPDATE fyniq.ai_proposal SET approved_payload = '{"note":"Pay me"}',
			approved_sha256 = encode(sha256(convert_to('{"note":"Pay me"}', 'UTF8')), 'hex'), version = version + 1 WHERE id = $1`, []string{"FY342"}},
		{"the API rejecting an approved proposal", "fyniq_api", `UPDATE fyniq.ai_proposal SET status = 'rejected', reason = 'no',
			approved_payload = NULL, approved_sha256 = NULL, version = version + 1 WHERE id = $1`, []string{"FY342"}},
		{"the API starting it", "fyniq_api", `UPDATE fyniq.ai_proposal SET status = 'executing', version = version + 1 WHERE id = $1`,
			[]string{"FY342"}},
		{"the executor redoing the dry run", "fyniq_executor", `UPDATE fyniq.ai_proposal SET impact = '{}' WHERE id = $1`, []string{"42501"}},
		{"executed without starting", "fyniq_executor", `UPDATE fyniq.ai_proposal SET status = 'executed', execution = '{}',
			finished_at = now(), version = version + 1 WHERE id = $1`, []string{"FY342"}},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, p.ID); return err })
		wantState(t, c.what, err, c.state...)
	}
}
