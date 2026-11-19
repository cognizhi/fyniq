package nemura

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
)

// AssistantService is the assistant's settings and proposals behind the API (NM-AI-01, NM-AI-04). The API has already checked the roles (the HR team decides, the Owner changes the settings); the runtime
// that proposes has its own store, connected as the assistant's role, and calls Propose.
type AssistantService struct {
	Store *Store
	Clock ports.Clock
	// AllowScripted lets an Owner choose local mode's scripted model as the route: true in the local build only, never in a release build.
	AllowScripted bool
}

var _ api.AssistantAdmin = AssistantService{}

func (a AssistantService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return a.Store.InOrg(ctx, s.Organisation, fn)
}

func (a AssistantService) Settings(ctx context.Context, s api.Session) (api.AISettings, error) {
	out := api.AISettings{TaskTypes: []string{}, Catalogue: []api.AITask{}, OwnPayNote: "Own-pay explanations need the Owner and a second approver, which arrives when Nemura is linked to Fyniq. Until then the assistant explains a payslip line from its rule and the page shows the figures."}
	err := a.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		r, err := AISettings(ctx, tx)
		out.Enabled, out.Route, out.TaskTypes, out.OwnPay = r.Enabled, r.Route, r.TaskTypes, r.OwnPay
		if !r.UpdatedAt.IsZero() {
			out.UpdatedAt = r.UpdatedAt.UTC().Format(time.RFC3339)
		}
		return err
	})
	for _, t := range assistant.Catalogue {
		out.Catalogue = append(out.Catalogue, api.AITask{ID: t.ID, What: t.What, Tier: t.Tier, ApproverRoles: t.ApproverRoles, ExpiryHours: t.ExpiryHours})
	}
	return out, err
}

func (a AssistantService) SetSettings(ctx context.Context, s api.Session, in api.AISettingsInput) error {
	return a.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return SetAISettings(ctx, tx, s.Organisation, a.Clock.Now(), s.Member, AISettingsRow{Enabled: in.Enabled, Route: in.Route, TaskTypes: in.TaskTypes, OwnPay: in.OwnPay}, a.AllowScripted)
	})
}

func (a AssistantService) view(r ProposalRow) api.AIProposal {
	what := ""
	if t, ok := assistant.Lookup(r.Task); ok {
		what = t.What
	}
	out := api.AIProposal{ID: r.ID.String(), Task: r.Task, What: what, Asker: r.AskerName, AskerID: r.Asker.String(), Payload: r.Payload, Final: r.Final, Rationale: r.Rationale, ModelID: r.ModelID, State: r.State,
		Expired: r.State == "pending" && a.Clock.Now().After(r.ExpiresAt), CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339), ExpiresAt: r.ExpiresAt.UTC().Format(time.RFC3339), DecidedBy: r.DeciderName, DecisionReason: r.DecisionReason}
	return out
}

func (a AssistantService) Proposals(ctx context.Context, s api.Session, state string) ([]api.AIProposal, error) {
	out := []api.AIProposal{}
	err := a.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := Proposals(ctx, tx, state)
		for _, r := range rows {
			out = append(out, a.view(r))
		}
		return err
	})
	return out, err
}

func (a AssistantService) Proposal(ctx context.Context, s api.Session, id uuid.UUID) (api.AIProposal, error) {
	var out api.AIProposal
	err := a.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		r, err := ProposalOf(ctx, tx, id)
		out = a.view(r)
		return err
	})
	return out, err
}

// Decide approves or rejects a pending proposal. The API has checked that the caller is the HR team or the Owner, which are the approver roles of both P1 task types.
func (a AssistantService) Decide(ctx context.Context, s api.Session, id uuid.UUID, in api.AIDecision) error {
	return a.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return DecideProposal(ctx, tx, s.Organisation, a.Clock.Now(), s.Member, []string{"hr_officer"}, id, in.Approve, in.Reason, in.Final)
	})
}
