//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// appDraft is a proposal an AI app made for b's member over MCP: no turn, no run.
func (b proposalBooks) appDraft(payload string) storage.AIProposal {
	d := b.draft(payload, 0)
	d.Turn, d.Agent, d.MCPApp, d.Prompt, d.Model = nil, "mcp", "claude-code", "mcp@1", "claude-code claude-code/2.1.327"
	return d
}

func (b proposalBooks) optIn(t *testing.T, in bool) {
	t.Helper()
	if err := store(t, "fyniq_api").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MCPOptIns().Set(ctx, storage.MCPOptIn{UserID: b.user, OptedIn: in, SetAt: clock.System{}.Now().UTC()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// A proposal an AI app makes over MCP is its own course, for a person who allowed AI apps and nothing else (migration 000587;
// AI-16, AI-35, AI-89 rev 0.298): it needs the person's opt-in as it is made, comes from exactly one of a turn, a run and an
// app, is the agent mcp's, and never changes its app. Whatever SQL is sent.
// req: AI-16
// req: AI-35
// req: AI-89
// asvs: V8.2.2, V4.1.3
func TestAttackAnAppsProposalIsForAPersonWhoAllowedAIApps(t *testing.T) {
	ctx := context.Background()
	b := newProposalBooks(t, "Attack app proposals Pte Ltd")
	create := func(d storage.AIProposal) error {
		_, err := b.run(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, d) })
		return err
	}
	// The person has not opted in: the database refuses it, whatever the API checked.
	if err := create(b.appDraft(`{"note":"One"}`)); storage.CodeOf(err) != storage.CodeAIProposal {
		t.Errorf("a proposal for a person who never opted in: %v", err)
	}
	b.optIn(t, false)
	if err := create(b.appDraft(`{"note":"One"}`)); storage.CodeOf(err) != storage.CodeAIProposal {
		t.Errorf("a proposal for a person who opted out: %v", err)
	}
	b.optIn(t, true)
	made, err := b.run(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Create(ctx, b.appDraft(`{"note":"One"}`))
	})
	if err != nil || made.MCPApp != "claude-code" || made.Turn != nil || made.Run != nil || made.Agent != "mcp" {
		t.Fatalf("a proposal for a person who opted in: %+v %v", made, err)
	}
	// Exactly one course; the agent is mcp; the app's name is a name.
	both := b.appDraft(`{"note":"Two"}`)
	turn := b.turn
	both.Turn = &turn
	if err := create(both); err == nil {
		t.Error("a proposal from a turn and an app was kept")
	}
	if err := create(func() storage.AIProposal { d := b.appDraft(`{"note":"Three"}`); d.Agent = "chat"; return d }()); storage.CodeOf(err) != storage.CodeAIProposal {
		t.Errorf("an app's proposal as another agent's: %v", err)
	}
	if err := create(func() storage.AIProposal { d := b.appDraft(`{"note":"Four"}`); d.MCPApp = "Claude Code!"; return d }()); err == nil {
		t.Error("an app that is not a name was kept")
	}
	// The app never changes once made.
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE fyniq.ai_proposal SET mcp_app = 'other-app', status = 'invalid', reason = 'x', version = version + 1 WHERE id = $1`, made.ID)
		return err
	})
	wantState(t, "changing the app a proposal came from", err, "42501", "FY342") // no grant on the column, and the trigger would refuse it
}

// The delegated token never stands for the person at the signature (migration 000587; AI-89 rev 0.298, option B): the request is
// asked with the method mcp_token for an app's proposal alone, and a turn's or a run's method is refused for it; the person an app
// proposed for signs their own tier-1 proposal only on a step-up taken after it was proposed, where a turn's person signs on any
// step-up of the last 5 minutes. Whatever SQL is sent.
// req: AI-89
// req: AI-10
// req: FM-1
// asvs: V8.2.2, V8.3.1
func TestAttackAnAppsProposalIsSignedOnAFreshStepUpOrByAnotherMember(t *testing.T) {
	ctx := context.Background()
	b := newProposalBooks(t, "Attack app proposal signatures Pte Ltd")
	b.optIn(t, true)
	api := store(t, "fyniq_api")
	request := func(pr storage.AIProposal, method string, at time.Time) (storage.ApprovalRequest, error) {
		envelope := `{"object":{"id":"` + pr.ID.String() + `","type":"ai_proposal"}}`
		sum := sha256.Sum256([]byte(envelope))
		var req storage.ApprovalRequest
		err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			req, err = u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: "ai_proposal", ObjectKey: pr.ID.String(),
				Operation: "ai_proposal.execute", Summary: "A proposal", Change: []byte(`{"execute":{}}`), Envelope: envelope, EnvelopeHash: sum[:],
				PolicyVersion: "founder/ai_proposal@2026-11-15.1", PolicySteps: []storage.ApprovalStep{{Quorum: 1}}, RequestedBy: b.user,
				RequestedAt: at, RequesterMFAMethod: method, RequesterMFAAt: at, ExpiresAt: pr.ExpiresAt})
			if err != nil {
				return err
			}
			_, err = u.AIProposals().Pending(ctx, pr.ID, pr.Version, json.RawMessage(`{}`), nil, &req.ID)
			return err
		})
		return req, err
	}
	sign := func(req storage.ApprovalRequest, stepUp, at time.Time) error {
		return api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: req.ID, Step: 1, Signer: b.user, Decision: "approve",
				EnvelopeHash: req.EnvelopeHash, MFAMethod: "session_step_up", MFAAt: stepUp, SignedAt: at})
			return err
		})
	}
	app := func(payload string) storage.AIProposal {
		return b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, b.appDraft(payload)) })
	}
	asked := b.now.Add(-time.Minute)

	// The method is the course's: mcp_token for an app's proposal, never for a turn's; ai_turn never for an app's.
	if _, err := request(app(`{"note":"One"}`), "ai_turn", asked); storage.CodeOf(err) != storage.CodeAIProposal {
		t.Errorf("an app's proposal asked with a turn's method: %v", err)
	}
	turnMade := b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Create(ctx, b.draft(`{"note":"Turn"}`, 0))
	})
	if _, err := request(turnMade, "mcp_token", asked); storage.CodeOf(err) != storage.CodeAIProposal {
		t.Errorf("a turn's proposal asked with an app's method: %v", err)
	}
	envelope := `{"object":{"id":"bank.tolerance_base","type":"organisation_setting"}}`
	sum := sha256.Sum256([]byte(envelope))
	err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change, envelope, envelope_hash, policy_version,
		  requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at)
		  VALUES ('organisation_setting', 'bank.tolerance_base', 'organisation_setting.change', 'x', '{}', $1, $2, 'p', $3, $4, 'mcp_token', $4, $5)`,
			envelope, sum[:], b.user, b.now, b.now.Add(time.Hour))
		return err
	})
	wantState(t, "a setting's request authenticated by an app's token", err, "23514")

	// The person signs an app's tier-1 proposal only on a step-up taken after it was proposed.
	req, err := request(app(`{"note":"Two"}`), "mcp_token", asked)
	if err != nil {
		t.Fatalf("an app's proposal asked with its own method: %v", err)
	}
	for _, c := range []struct {
		what   string
		stepUp time.Time
	}{{"a step-up from before it was proposed", asked.Add(-time.Minute)}, {"a step-up at the very moment", asked}} {
		if err := sign(req, c.stepUp, b.now); sqlstate(err) != "FY342" && !strings.Contains(fmt.Sprint(err), "after it was proposed") {
			t.Errorf("signing on %s: %v", c.what, err)
		}
	}
	if err := sign(req, b.now, b.now); err != nil {
		t.Errorf("signing on a step-up after it was proposed: %v", err)
	}
	// A turn's person is as before: any step-up of the window signs their own tier-1 proposal (000377).
	turnReq, err := request(b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) {
		return r.Create(ctx, b.draft(`{"note":"Turn two"}`, 0))
	}), "ai_turn", asked)
	if err != nil {
		t.Fatal(err)
	}
	if err := sign(turnReq, asked.Add(-time.Minute), b.now); err != nil {
		t.Errorf("a turn's person on an earlier step-up: %v", err)
	}
	// The app's proposal of tier 2 is never signed by the person at all.
	two := b.appDraft(`{"note":"Three"}`)
	two.RiskTier = 2
	reqTwo, err := request(b.must(t, "fyniq_api", func(r storage.AIProposals) (storage.AIProposal, error) { return r.Create(ctx, two) }), "mcp_token", asked)
	if err != nil {
		t.Fatal(err)
	}
	if err := sign(reqTwo, b.now, b.now); sqlstate(err) != "FY101" && !strings.Contains(fmt.Sprint(err), "requester never signs") {
		t.Errorf("the person signing an app's tier-2 proposal: %v", err)
	}
}
