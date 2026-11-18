package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aiexclusion"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The settings of the AI exclusion policy (AI-02; p1-ai-assistant design D8).
const (
	settingAIPayroll    = "ai.payroll_access"
	settingAIExclusions = "ai.exclusions"
)

// excludeForAI takes out of every answer to an AI principal what never reaches a model, after the masking of an M
// right (AI-02, AI-20; design D8): the never-send fields, pay unless AI payroll access is on and the person may see
// payroll, the Owner's own exclusions, and NRIC and bank-number shapes in the text left. It runs on every operation, so
// the read tools, and later MCP, read nothing it has not filtered.
func (a *API) excludeForAI(ctx huma.Context, _ string, v any) (any, error) {
	p, ok := authn.From(ctx.Context())
	if !ok || !p.AI() || v == nil || a.deps.Store == nil {
		return v, nil
	}
	policy, err := a.aiExclusionPolicy(ctx.Context(), p)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	tree, err := aitool.Decode(b)
	if err != nil {
		return nil, err
	}
	return aiexclusion.Apply(tree, policy), nil
}

// aiExclusionPolicy is the policy of p's organisation today: pay only with AI payroll access on and a payroll right of
// the person's own, and the Owner's added fields.
func (a *API) aiExclusionPolicy(ctx context.Context, p authn.Principal) (aiexclusion.Policy, error) {
	var policy aiexclusion.Policy
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		payroll, err := settingOn(ctx, u, settingAIPayroll, today)
		if err != nil {
			return err
		}
		extra, err := settingOn(ctx, u, settingAIExclusions, today)
		if err != nil {
			return err
		}
		mayPay := authz.Decide(authz.Policy{Operation: "ai.payroll", Capability: authz.PayRuns, Need: authz.View}, p.Roles, false).Allowed
		policy.Payroll = payroll == "true" && mayPay
		if extra != "" {
			policy.Extra = strings.Split(extra, ",")
		}
		return nil
	})
	return policy, err
}
