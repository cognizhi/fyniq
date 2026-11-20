package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/erasure"
)

// Erasure of a former employee's personal data (PRD-002 NM-SEC-08): an Owner or an HR administrator asks, a second one approves, the worker erases. By identity: no name, no value.

// ErasureOrderView is an erasure order.
type ErasureOrderView struct {
	ID          string  `json:"id" format:"uuid" doc:"The order." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"`
	PersonID    string  `json:"person_id" format:"uuid" doc:"The former employee." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	State       string  `json:"state" enum:"requested,approved,executed,refused" doc:"requested: waits for a second person; approved: the worker erases; executed: erased; refused." example:"requested"`
	Reason      string  `json:"reason" doc:"Why it is asked for." example:"Left in 2090; the retention periods have run."`
	EligibleOn  string  `json:"eligible_on" format:"date" doc:"The first day the retention periods allow it." example:"2096-01-01"`
	RequestedBy string  `json:"requested_by" format:"uuid" doc:"The member who asked." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e11"`
	ApprovedBy  *string `json:"approved_by,omitempty" format:"uuid" doc:"The second member who approved." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e12"`
	RequestedAt string  `json:"requested_at" format:"date-time" doc:"When it was asked for." example:"2096-01-02T09:00:00Z"`
	ExecutedAt  *string `json:"executed_at,omitempty" format:"date-time" doc:"When it was done." example:"2096-01-02T09:05:00Z"`
}

// ErasureOps is the erasure's requests, decisions and list.
type ErasureOps interface {
	Request(ctx context.Context, s Session, held []authz.Role, person uuid.UUID, reason string) (ErasureOrderView, error)
	Decide(ctx context.Context, s Session, held []authz.Role, order uuid.UUID, approve bool) error
	List(ctx context.Context, s Session) ([]ErasureOrderView, error)
}

type erasureRequestInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		PersonID string `json:"person_id" format:"uuid" doc:"The former employee." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
		Reason   string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the erasure is asked for." example:"Left in 2090; the retention periods have run."`
	}
}
type erasureOrderOutput struct{ Body ErasureOrderView }
type erasureDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The order." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"`
	Body   struct {
		Approve bool `json:"approve" doc:"True approves, false refuses." example:"true"`
	}
}
type erasureDecisionOutput struct {
	Body struct {
		Decided bool `json:"decided" doc:"True once the decision is recorded." example:"true"`
	}
}
type erasureListInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}
type erasureListOutput struct {
	Body struct {
		Items []ErasureOrderView `json:"items" nullable:"false" doc:"The orders, newest first."`
	}
}

func erasureError(err error) error {
	var r *erasure.Refusal
	if errors.As(err, &r) {
		return Err(r.Code, r.Message)
	}
	if errors.Is(err, erasure.ErrNotFound) {
		return Err(CodeNotFound, "There is no such erasure.")
	}
	return huma.Error500InternalServerError("The erasure could not be handled.")
}

func init() {
	IdempotencyExempt["POST /v1/erasures"] = "an erasure is asked for once for a person: a second request is refused while one is live and once it is done (NM_ERASURE_ALREADY)"
	IdempotencyExempt["POST /v1/erasures/{id}/decision"] = "an order is decided once: a second decision is refused (NM_ERASURE_ALREADY)"
}

func (a *API) registerErasure() {
	unavailable := func() error { return Err(CodeUnavailable, "Erasure is not set up in this deployment.") }
	roles := func(ctx context.Context, cookie string) (Session, []authz.Role, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return Session{}, nil, err
		}
		held, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
		if err != nil {
			return Session{}, nil, huma.Error500InternalServerError("The roles could not be read.")
		}
		return s, held, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listErasures", Method: http.MethodGet, Path: "/v1/erasures", Tags: []string{"Privacy"}, Summary: "List the erasures of former employees' data",
		Description: "The erasure orders, newest first, by identity: no name and no value. Only an Owner or an HR administrator. It changes nothing.", Errors: []int{401, 403, 503},
	}, func(ctx context.Context, in *erasureListInput) (*erasureListOutput, error) {
		s, held, err := roles(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if !holdsErasure(held, authz.Create) {
			return nil, Err(CodeRoleRequired, "Only an Owner or an HR administrator sees the erasures of former employees' data.")
		}
		if a.d.Erasure == nil {
			return nil, unavailable()
		}
		items, err := a.d.Erasure.List(ctx, s)
		if err != nil {
			return nil, erasureError(err)
		}
		out := &erasureListOutput{}
		out.Body.Items = items
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "requestErasure", Method: http.MethodPost, Path: "/v1/erasures", Tags: []string{"Privacy"}, DefaultStatus: http.StatusCreated, Summary: "Ask for a former employee's personal data to be erased",
		Description: "PRD-002 NM-SEC-08. An Owner or an HR administrator, with a passkey confirmed in the last five minutes, asks once the person has left and their retention periods have run (the employment records' " +
			"one year after leaving, the payroll records' five years: [accountant to confirm]); a legal hold or an unanswered access request refuses it. A second Owner or HR administrator approves. It is on the " +
			"never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *erasureRequestInput) (*erasureOrderOutput, error) {
		s, held, err := roles(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if !holdsErasure(held, authz.Create) {
			return nil, Err(CodeRoleRequired, "Only an Owner or an HR administrator asks for a former employee's data to be erased.")
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirm it is you with your passkey, then try again: an erasure removes a person's data for good.")
		}
		if a.d.Erasure == nil {
			return nil, unavailable()
		}
		person, err := uuid.Parse(in.Body.PersonID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		v, err := a.d.Erasure.Request(ctx, s, held, person, in.Body.Reason)
		if err != nil {
			return nil, erasureError(err)
		}
		return &erasureOrderOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideErasure", Method: http.MethodPost, Path: "/v1/erasures/{id}/decision", Tags: []string{"Privacy"}, DefaultStatus: http.StatusOK, Summary: "Approve or refuse an erasure",
		Description: "A second Owner or HR administrator, not the one who asked, with a passkey confirmed in the last five minutes. An approval queues the worker, which files a signed entry in the register outside the " +
			"database and then empties the person's personal data in the records that may change; records the database keeps append-only stay. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *erasureDecisionInput) (*erasureDecisionOutput, error) {
		s, held, err := roles(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if !holdsErasure(held, authz.Approve) {
			return nil, Err(CodeRoleRequired, "Only an Owner or an HR administrator approves an erasure.")
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirm it is you with your passkey, then try again: an erasure removes a person's data for good.")
		}
		if a.d.Erasure == nil {
			return nil, unavailable()
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such erasure.")
		}
		if err := a.d.Erasure.Decide(ctx, s, held, id, in.Body.Approve); err != nil {
			return nil, erasureError(err)
		}
		out := &erasureDecisionOutput{}
		out.Body.Decided = true
		return out, nil
	})
}

const nemuraErasure = "Erasure of a former employee's data (NM-SEC-08)"

func holdsErasure(held []authz.Role, r authz.Right) bool {
	for _, h := range held {
		if authz.Can(h, nemuraErasure, r) {
			return true
		}
	}
	return false
}
