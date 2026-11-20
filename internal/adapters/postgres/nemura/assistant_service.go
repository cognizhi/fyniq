package nemura

import (
	"context"
	"slices"
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
	// Runtime is the store of the assistant's own role: the one that may insert a proposal. Without it a draft cannot be proposed.
	Runtime *Store
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

// Draft makes a proposal for an open request: the request is read as the person who asked (the HR team), the scripted writer drafts from it, and the proposal is inserted as the assistant's own role.
func (a AssistantService) Draft(ctx context.Context, s api.Session, in api.AIDraftInput) (api.AIDraftResult, error) {
	var out api.AIDraftResult
	req, err := uuid.Parse(in.RequestID)
	if err != nil {
		return out, &assistant.Refusal{Code: assistant.CodeNotFound, Message: "There is no such request."}
	}
	if a.Runtime == nil {
		return out, api.ErrUnavailable
	}
	var facts assistant.RequestFacts
	var route string
	if err := a.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		set, err := AISettings(ctx, tx)
		if err != nil {
			return err
		}
		route = set.Route
		if !set.Enabled || !slices.Contains(set.TaskTypes, in.Task) {
			return &assistant.Refusal{Code: assistant.CodeOff, Message: "The assistant is off, or this kind of draft is: the Owner turns it on in the assistant's settings."}
		}
		facts, err = RequestFactsOf(ctx, tx, in.Task, req)
		return err
	}); err != nil {
		return out, err
	}
	payload, why, err := assistant.Draft(in.Task, facts)
	if err != nil {
		return out, err
	}
	if in.Task == "people.draft_job_post" || in.Task == "people.draft_interview_kit" {
		payload["requisition_id"] = req.String()
	} else {
		payload["request_id"] = req.String()
	}
	err = a.Runtime.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		id, err := Propose(ctx, tx, s.Organisation, a.Clock.Now(), ProposalInput{Asker: s.Member, Task: in.Task, Payload: payload, Rationale: why, ModelID: route, PromptVersion: "people-assistant@1"})
		out.ProposalID = id.String()
		return err
	})
	return out, err
}

// Drafts lists the drafts the executor attached to a request.
func (a AssistantService) Drafts(ctx context.Context, s api.Session, request uuid.UUID) ([]api.AIDraft, error) {
	out := []api.AIDraft{}
	err := a.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := DraftsFor(ctx, tx, request)
		for _, r := range rows {
			out = append(out, api.AIDraft{ID: r.ID.String(), ProposalID: r.Proposal.String(), Kind: r.Kind, Payload: r.Payload, AppliedAt: r.AppliedAt.UTC().Format(time.RFC3339)})
		}
		return err
	})
	return out, err
}

// ExecutorService applies approved proposals under the executor's own role (NM-ARC-06): the sweep the executor process runs, and the same command a queued item calls.
type ExecutorService struct {
	Store *Store
	Clock ports.Clock
}

// ApplyApproved applies every approved proposal of an organisation that nobody has applied, each in its own transaction, and returns how many were applied and what refused.
func (e ExecutorService) ApplyApproved(ctx context.Context, org uuid.UUID) (int, []error) {
	var ids []uuid.UUID
	if err := e.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ids, err = ApprovedProposals(ctx, tx)
		return err
	}); err != nil {
		return 0, []error{err}
	}
	applied := 0
	var refused []error
	for _, id := range ids {
		if err := e.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error { return ApplyProposal(ctx, tx, org, e.Clock.Now(), id) }); err != nil {
			refused = append(refused, err)
			continue
		}
		applied++
	}
	return applied, refused
}
