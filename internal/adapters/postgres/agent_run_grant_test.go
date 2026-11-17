//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// runningRun is a running run of the company secretary's, acting for b's member.
func (b proposalBooks) runningRun(t *testing.T, trigger string) storage.AgentRun {
	t.Helper()
	var run storage.AgentRun
	if err := store(t, "fyniq_worker").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		queued, _, err := u.AgentRuns().Queue(ctx, storage.NewAgentRun{Agent: "company-secretary", SkillVersion: 1, SkillSHA256: hexSHA("company-secretary@1"),
			Trigger: trigger, SlotAt: b.now.Truncate(time.Minute), ActsFor: b.user, QueuedAt: b.now})
		if err != nil {
			return err
		}
		run, err = u.AgentRuns().Start(ctx, queued.ID, queued.Version, b.now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

// A scheduled agent's run reads and proposes through a grant of its own (PRD-001 AI-63, AI-89 rev 0.281; p1-scheduled-agents
// design D6): a grant is a turn's or a run's, never both; a run's holds its tools and task types, lives at most 30
// minutes, one per run, and resolves to the Owner the run acts for and the run's agent; nothing of it changes but its end.
// req: AI-63
// req: QA-ENG-05
// asvs: V7.2.3, V8.2.2
func TestAttackARunsGrantIsItsRunsAlone(t *testing.T) {
	ctx := context.Background()
	b := newProposalBooks(t, "Attack run grants Pte Ltd")
	api := store(t, "fyniq_api")
	run := b.runningRun(t, "schedule:monitor")
	issue := func(g storage.AIGrant) (string, error) {
		token, hash, err := authn.NewAIToken(b.Org)
		if err != nil {
			t.Fatal(err)
		}
		g.TokenHash, g.UserID, g.IssuedAt = hash, b.user, b.now
		return token, api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.AIGrants().Issue(ctx, g)
			return err
		})
	}
	grant := storage.AIGrant{Run: run.ID, Tools: []string{"checks.list", "packs.get"}, TaskTypes: []string{"sec.create_task"},
		ExpiresAt: b.now.Add(authn.MaxRunDelegated), Propose: true}
	token, err := issue(grant)
	if err != nil {
		t.Fatal(err)
	}
	org, hash, _ := authn.ParseAIToken(token)
	g, err := api.AIGrant(ctx, org, hash, b.now.Add(20*time.Minute))
	if err != nil || g.UserID != b.user || g.Run != run.ID || g.Turn != uuid.Nil || g.Agent != "company_secretary" || !g.Propose ||
		len(g.Tools) != 2 || len(g.TaskTypes) != 1 || g.TaskTypes[0] != "sec.create_task" {
		t.Fatalf("a run's live grant: %+v %v", g, err)
	}
	if _, err := issue(grant); err == nil {
		t.Error("a second grant for the run")
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.AIGrants().End(ctx, run.ID, b.now) }); err != nil {
		t.Fatal(err)
	}
	if _, err := api.AIGrant(ctx, org, hash, b.now); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a run's ended grant: %v", err)
	}

	insert := `INSERT INTO fyniq.ai_grant (token_hash, user_id, turn_id, run_id, tools, task_types, issued_at, expires_at)
	  VALUES (sha256(gen_random_uuid()::text::bytea), '` + b.user.String() + `', `
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"a grant of a turn and a run", "fyniq_api", insert + `gen_random_uuid(), '` + run.ID.String() + `', '{}', '{}', now(), now() + interval '1 minute')`,
			[]string{"23514"}},
		{"a grant of neither", "fyniq_api", insert + `NULL, NULL, NULL, NULL, now(), now() + interval '1 minute')`, []string{"23514"}},
		{"a run's grant without its tools", "fyniq_api", insert + `NULL, '` + run.ID.String() + `', NULL, '{}', now(), now() + interval '1 minute')`,
			[]string{"23514"}},
		{"a turn's grant with tools", "fyniq_api", insert + `gen_random_uuid(), NULL, '{calc}', NULL, now(), now() + interval '1 minute')`,
			[]string{"23514"}},
		{"a run's grant of 31 minutes", "fyniq_api", insert + `NULL, gen_random_uuid(), '{}', '{}', now(), now() + interval '31 minutes')`,
			[]string{"23514"}},
		{"a turn's grant of 10 minutes", "fyniq_api", insert + `gen_random_uuid(), NULL, NULL, NULL, now(), now() + interval '10 minutes')`,
			[]string{"23514"}},
		{"a grant of a run that is none", "fyniq_api", insert + `NULL, gen_random_uuid(), '{}', '{}', now(), now() + interval '1 minute')`,
			[]string{"23503"}},
		{"the owner widening a run's tools", "fyniq_migrator", `UPDATE fyniq.ai_grant SET tools = tools || '{records.get}' WHERE run_id IS NOT NULL`,
			[]string{"FY340"}},
		{"the owner moving a grant to another run", "fyniq_migrator", `UPDATE fyniq.ai_grant SET run_id = gen_random_uuid() WHERE run_id IS NOT NULL`,
			[]string{"FY340", "23503"}},
		{"the API widening a run's task types", "fyniq_api", `UPDATE fyniq.ai_grant SET task_types = '{sec.draft_reminder}'`, []string{"42501"}},
		{"the worker issuing a run's grant", "fyniq_worker", insert + `NULL, '` + run.ID.String() + `', '{}', '{}', now(), now() + interval '1 minute')`,
			[]string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
}

// A run's proposal is the run's alone (AI-89 rev 0.281; design D6): it comes from the running run, for the Owner the
// run acts for, and its request is authenticated by the run (ai_run), as a turn's proposal's is by the turn (ai_turn),
// never the other's; that Owner signs their own tier-1 proposal; no other request is authenticated by a run.
// req: AI-89
// req: AI-63
// req: QA-ENG-05
// asvs: V8.2.2, V8.3.1
func TestAttackARunsProposalIsAskedByItsRun(t *testing.T) {
	ctx := context.Background()
	b := newProposalBooks(t, "Attack run proposals Pte Ltd")
	api := store(t, "fyniq_api")
	run := b.runningRun(t, "schedule:monitor")
	fromRun := func(note string) storage.AIProposal {
		d := b.draft(`{"note":"`+note+`"}`, 0)
		d.Turn, d.Run, d.Agent = nil, &run.ID, "company_secretary"
		return d
	}
	request := func(pr storage.AIProposal, method string) (storage.ApprovalRequest, error) {
		envelope := `{"object":{"id":"` + pr.ID.String() + `","type":"ai_proposal"}}`
		sum := sha256.Sum256([]byte(envelope))
		var req storage.ApprovalRequest
		return req, api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			req, err = u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: "ai_proposal", ObjectKey: pr.ID.String(),
				Operation: "ai_proposal.execute", Summary: "A proposal", Change: []byte(`{"execute":{}}`), Envelope: envelope, EnvelopeHash: sum[:],
				PolicyVersion: "founder/ai_proposal@2026-11-15.1", PolicySteps: []storage.ApprovalStep{{Quorum: 1}}, RequestedBy: b.user,
				RequestedAt: b.now, RequesterMFAMethod: method, RequesterMFAAt: b.now, ExpiresAt: pr.ExpiresAt})
			if err != nil {
				return err
			}
			_, err = u.AIProposals().Pending(ctx, pr.ID, pr.Version, json.RawMessage(`{}`), nil, &req.ID)
			return err
		})
	}

	// The run's proposal, asked by the run, signed by the Owner it acts for: tier 1.
	mine := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, fromRun("Mine")) })
	req, err := request(mine, "ai_run")
	if err != nil {
		t.Fatalf("the run asking for its proposal's approval: %v", err)
	}
	if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: req.ID, Step: 1, Signer: b.user, Decision: "approve",
			EnvelopeHash: req.EnvelopeHash, MFAMethod: "session_step_up", MFAAt: b.now, SignedAt: b.now})
		return err
	}); err != nil {
		t.Errorf("the Owner signing their agent's tier-1 proposal: %v", err)
	}

	// Each origin authenticates its own proposals alone.
	runs := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, fromRun("Run's")) })
	if _, err := request(runs, "ai_turn"); sqlstate(err) != "FY342" {
		t.Errorf("a run's proposal asked as by a turn: %v", err)
	}
	turns := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Create(ctx, b.draft(`{"note":"Turn's"}`, 0))
	})
	if _, err := request(turns, "ai_run"); sqlstate(err) != "FY342" {
		t.Errorf("a turn's proposal asked as by a run: %v", err)
	}

	// A run proposes for the Owner it acts for, while it runs.
	other := fromRun("Someone else's")
	other.RequestedBy = founder(t, api, b.Org, 9, b.now)
	if _, err := b.run(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, other) }); sqlstate(err) != "FY342" {
		t.Errorf("a run proposing for someone it does not act for: %v", err)
	}
	if err := store(t, "fyniq_worker").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AgentRuns().Fail(ctx, run.ID, run.Version, "the model step did not finish", b.now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.run(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, fromRun("Late")) }); sqlstate(err) != "FY342" {
		t.Errorf("a run proposing once it ended: %v", err)
	}

	// No other request is authenticated by a run.
	envelope := `{"object":{"id":"bank.tolerance_base","type":"organisation_setting"}}`
	sum := sha256.Sum256([]byte(envelope))
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change, envelope, envelope_hash,
		  policy_version, requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at)
		  VALUES ('organisation_setting', 'bank.tolerance_base', 'organisation_setting.change', 'x', '{}', $1, $2, 'p', $3, $4, 'ai_run', $4, $5)`,
			envelope, sum[:], b.user, b.now, b.now.Add(time.Hour))
		return err
	})
	wantState(t, "a setting's request authenticated by a run", err, "23514")
}
