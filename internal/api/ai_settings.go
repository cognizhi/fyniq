package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the AI's switch and budgets (AI-13, AI-77)
//
// The AI switch is the organisation setting features.ai_assistant (FR-ORG-06): off by default; on only with a second
// approver, as a setting under dual control is; off at once, by the Owner here or by anyone who may change settings,
// with no second approver (AI-13). While it is off the router starts no run, and a stream running when it goes off
// stops at its next check, within 10 seconds.

// AISettings are the AI's switch and budgets, as Settings › AI shows them.
type AISettings struct {
	Switch  Setting      `json:"switch" doc:"The AI switch, the setting features.ai_assistant: switched on only with a second approver; switched off at once (AI-13)."`
	Budgets AIBudgetList `json:"budgets" doc:"The monthly AI budgets and what this month used of each (AI-77, AI-86)."`
}

type (
	aiSettingsOutput struct{ Body AISettings }
	aiSwitchOffInput struct {
		KeyHeader
		PreferHeader
	}
	aiSwitchOffOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AISettings
	}
)

// aiSettings are the switch as it stands today and the budgets.
func (a *API) aiSettings(ctx context.Context, u storage.UnitOfWork) (AISettings, error) {
	d, err := lookupSetting(ai.Switch)
	if err != nil {
		return AISettings{}, err
	}
	changes, err := u.Settings().Changes(ctx, d.Key)
	if err != nil {
		return AISettings{}, err
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return AISettings{}, err
	}
	out := AISettings{Switch: settingOf(d, changes, today, true)}
	out.Budgets, err = a.aiBudgetList(ctx, u)
	return out, err
}

func registerAISettings(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAISettings, huma.Operation{
		OperationID: "getAISettings", Method: http.MethodGet, Path: "/v1/ai/settings", Tags: []string{"AI"},
		Summary: "Get the AI's switch and budgets",
		Description: "The AI switch (features.ai_assistant, AI-13) as it stands today, with its pending changes, and the monthly AI budgets " +
			"with what this month used of each (AI-77). Switch it on with updateSetting, which a second approver approves; switch it off " +
			"at once with switchOffAI. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*aiSettingsOutput, error) {
		p, _ := authn.From(ctx)
		var out AISettings
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.aiSettings(ctx, u)
			return err
		})
		return &aiSettingsOutput{Body: out}, err
	})

	register(a, authz.SwitchOffAI, huma.Operation{
		OperationID: "switchOffAI", Method: http.MethodPost, Path: "/v1/ai/settings/switch-off", Tags: []string{"AI"},
		Summary: "Switch the AI off at once",
		Description: "The Owner's kill switch (AI-13): turns features.ai_assistant off from today, with no second approver, and cancels any " +
			"change already approved to turn it on later. New calls are refused at once, and a stream running now stops at its next check, " +
			"within 10 seconds; accounting, checks and deadlines keep working. Switching it on again needs updateSetting and a second " +
			"approver. Idempotent with Idempotency-Key; Prefer: return=preview shows the result and saves nothing. Side effects: the " +
			"setting's next versions and their audit events. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeVersion}, locks...),
		func(ctx context.Context, in *aiSwitchOffInput) (*aiSwitchOffOutput, error) {
			resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AISettings, error) {
				d, err := lookupSetting(ai.Switch)
				if err != nil {
					return AISettings{}, err
				}
				changes, err := u.Settings().Changes(ctx, d.Key)
				if err != nil {
					return AISettings{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return AISettings{}, err
				}
				// Off from today, and off on each later day a change was to turn it on.
				days := []string{}
				if settings.On(d, changes, today).Value != "false" {
					days = append(days, dateOf(today))
				}
				for _, c := range changes {
					if c.Key == d.Key && c.EffectiveFrom.After(today) && c.Value == "true" {
						days = append(days, dateOf(c.EffectiveFrom))
					}
				}
				for _, day := range days {
					if _, err := a.recordSetting(ctx, u, d.Key, "false", day); err != nil {
						return AISettings{}, err
					}
				}
				return a.aiSettings(ctx, u)
			})
			if err != nil {
				return nil, err
			}
			return &aiSwitchOffOutput{Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
		})
}
