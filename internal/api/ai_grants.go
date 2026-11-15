package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// aiReadable is whether the assistant's delegated token may call op: only reads (PRD-001 §9.3 rev 0.277, AI-19). The
// read operations that take a body, such as the semantic layer's query, are named here as they arrive.
func aiReadable(op *huma.Operation) bool {
	return op.Method == http.MethodGet || op.OperationID == "queryMetrics"
}

// issueAIGrant issues the delegated read token a turn's tools read with (AI-01, AI-36; p1-ai-assistant design D2): as
// the person user, in the unit of work's organisation, for at most authn.MaxDelegated, never the person's session. Only
// its SHA-256 is kept; it ends with the turn (endAIGrant). While the organisation has a task type on that the release
// can propose (ai.task_types, AI-32), it may also make proposals (authn.AIProposeScope; p1-ai-proposals-core design D3),
// and nothing more.
func (a *API) issueAIGrant(ctx context.Context, u storage.UnitOfWork, org, user, turn uuid.UUID) (string, error) {
	token, hash, err := authn.NewAIToken(org)
	if err != nil {
		return "", err
	}
	types, err := a.proposable(ctx, u)
	if err != nil {
		return "", err
	}
	now := a.deps.Clock.Now()
	if _, err := u.AIGrants().Issue(ctx, storage.AIGrant{TokenHash: hash, UserID: user, Turn: turn, IssuedAt: now,
		ExpiresAt: now.Add(authn.MaxDelegated), Propose: len(types) > 0}); err != nil {
		return "", err
	}
	return token, nil
}

// issueRunGrant issues a scheduled agent's run its delegated token (AI-63; p1-scheduled-agents design D6): as the Owner the
// run acts for, in the unit of work's organisation, for at most authn.MaxRunDelegated, never anyone's session. It holds
// the skill's tools that the release's registry has, all within the skill's permission profile, and may propose only the
// skill's outputs that are task types on for the organisation, and only while one is. The AI exclusions and the masking
// apply to its reads as to a turn's. It ends with the run (endAIGrant).
func (a *API) issueRunGrant(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, run storage.AgentRun, skill agents.Skill) (string, storage.AIGrant, error) {
	if run.Status != storage.AgentRunRunning || run.Agent != skill.Name {
		return "", storage.AIGrant{}, fmt.Errorf("the run %s of %s is %s: only a running run of the skill's agent reads", run.ID, run.Agent, run.Status)
	}
	token, hash, err := authn.NewAIToken(org)
	if err != nil {
		return "", storage.AIGrant{}, err
	}
	on, err := a.proposable(ctx, u)
	if err != nil {
		return "", storage.AIGrant{}, err
	}
	var types []string
	for _, t := range skill.Proposes(a.catalogue()) {
		if slices.Contains(on, t) {
			types = append(types, t)
		}
	}
	now := a.deps.Clock.Now()
	g, err := u.AIGrants().Issue(ctx, storage.AIGrant{TokenHash: hash, UserID: run.ActsFor, Run: run.ID, Tools: skill.Offered(aitool.Default()),
		TaskTypes: types, IssuedAt: now, ExpiresAt: now.Add(authn.MaxRunDelegated), Propose: len(types) > 0})
	if err != nil {
		return "", storage.AIGrant{}, err
	}
	return token, g, nil
}

// endAIGrant ends a turn's or a run's delegated token, so nothing reads with it after the turn or the run.
func (a *API) endAIGrant(ctx context.Context, u storage.UnitOfWork, origin uuid.UUID) error {
	return u.AIGrants().End(ctx, origin, a.deps.Clock.Now())
}
