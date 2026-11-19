package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

const capLeave = "Leave requests"

// ErrOwnRequest is a person deciding their own request: another approver decides it.
var ErrOwnRequest = errors.New("a person cannot decide their own leave request")

// LeaveScope is whose leave requests a caller reads and decides: everyone's (the people team and the Owner), their reports' (a line manager), and always their own.
type LeaveScope struct {
	All, Reports bool
	// Decide is whether the caller may decide requests in scope: the people team and Owner, and a line manager for reports. Payroll reads and does not decide.
	Decide bool
}

// LeaveAsk is a request for leave as the form sends it.
type LeaveAsk struct {
	Type      string `json:"type" doc:"The leave type: annual." example:"annual"`
	From      string `json:"from" format:"date" doc:"The first day." example:"2026-04-06"`
	To        string `json:"to" format:"date" doc:"The last day; the same as the first for one day." example:"2026-04-10"`
	StartHalf bool   `json:"start_half,omitempty" doc:"Only half of the first day." example:"false"`
	EndHalf   bool   `json:"end_half,omitempty" doc:"Only half of the last day." example:"false"`
	Note      string `json:"note,omitempty" maxLength:"500" doc:"A note for the approver." example:"Family trip."`
}

// DayCost is one day of a request: what it costs or why it costs nothing.
type DayCost struct {
	Date string `json:"date" format:"date" doc:"The day." example:"2026-04-03"`
	Cost string `json:"cost" doc:"Days it costs: 1.00, 0.50 or 0.00." example:"0.00"`
	Why  string `json:"why,omitempty" doc:"Why it costs nothing: a rest day or the name of the public holiday." example:"Good Friday"`
}

// AwayPerson is a colleague who is away on days that overlap the request: never the leave type.
type AwayPerson struct {
	Name string `json:"name" doc:"The colleague." example:"Ben Lim"`
	From string `json:"from" format:"date" doc:"Their first day away." example:"2026-04-06"`
	To   string `json:"to" format:"date" doc:"Their last day away." example:"2026-04-08"`
}

// RequestPreview is what the form shows before a request is sent.
type RequestPreview struct {
	Days   string       `json:"days" doc:"The days the request costs." example:"5.00"`
	Left   string       `json:"left_after" doc:"The balance after this request, which is negative when it is beyond the balance." example:"9.00"`
	Enough bool         `json:"enough" doc:"Whether the balance covers the request." example:"true"`
	Basis  string       `json:"counting" enum:"working_days,calendar_days" doc:"How the days were counted." example:"working_days"`
	Counts []DayCost    `json:"days_counted" doc:"Each day of the request with its cost, public holidays and rest days at none."`
	Away   []AwayPerson `json:"away" doc:"Colleagues in the team away on those days: the name and the dates only."`
}

// LeaveRequest is a request as it is listed.
type LeaveRequest struct {
	ID        string `json:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80"`
	PersonID  string `json:"person_id" format:"uuid" doc:"The person asking." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person    string `json:"person_name" doc:"Their name." example:"Ben Lim"`
	Type      string `json:"type" doc:"The leave type." example:"annual"`
	From      string `json:"from" format:"date" doc:"The first day." example:"2026-04-06"`
	To        string `json:"to" format:"date" doc:"The last day." example:"2026-04-10"`
	StartHalf bool   `json:"start_half" doc:"Only half of the first day." example:"false"`
	EndHalf   bool   `json:"end_half" doc:"Only half of the last day." example:"false"`
	Days      string `json:"days" doc:"The days it costs." example:"5.00"`
	Note      string `json:"note,omitempty" doc:"The person's note." example:"Family trip."`
	State     string `json:"state" enum:"pending,approved,refused,cancelled" doc:"Where the request is." example:"pending"`
	Requested string `json:"requested_at" format:"date-time" doc:"When it was made." example:"2026-03-30T09:00:00Z"`
	Decided   string `json:"decided_at,omitempty" format:"date-time" doc:"When it was decided or cancelled." example:"2026-03-31T09:00:00Z"`
	Reason    string `json:"decision_reason,omitempty" doc:"The approver's reason; a refusal always has one." example:"The release freezes that week."`
}

// LeaveRequests is leave requests behind the API: the API decides the scope, the service reads and writes in one transaction and audits.
type LeaveRequests interface {
	Preview(ctx context.Context, s Session, a LeaveAsk) (RequestPreview, error)
	Apply(ctx context.Context, s Session, a LeaveAsk) (id, days string, err error)
	List(ctx context.Context, s Session, sc LeaveScope, state string) ([]LeaveRequest, error)
	Get(ctx context.Context, s Session, sc LeaveScope, id uuid.UUID) (LeaveRequest, error)
	Decide(ctx context.Context, s Session, sc LeaveScope, id uuid.UUID, approve bool, reason string) error
	Cancel(ctx context.Context, s Session, id uuid.UUID) error
	Settle(ctx context.Context, s Session, on time.Time) (requests int, days string, err error)
}

func init() {
	IdempotencyExempt["POST /v1/leave-requests/{id}/decision"] = "a decision is made once: a repeat is refused (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/leave-requests/{id}/cancellation"] = "a request is cancelled once: a repeat is refused (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/leave/settlement"] = "naturally idempotent: a second run finds nothing left to settle"
	IdempotencyExempt["POST /v1/me/leave-requests/preview"] = "it changes nothing: a preview is a read that takes a body"
	IdempotencyExempt["POST /v1/me/leave-requests"] = "a repeat overlaps the first request's days, which the database refuses (NM_LEAVE_CONFLICT)"
}

type askInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   LeaveAsk
}

type previewOutput struct{ Body RequestPreview }

type appliedOutput struct {
	Body struct {
		ID   string `json:"id" format:"uuid" doc:"The new request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80"`
		Days string `json:"days" doc:"The days reserved for it." example:"5.00"`
	}
}

type requestListInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	State  string `query:"state" enum:"pending,approved,refused,cancelled" doc:"Only requests in this state; all when absent." example:"pending"`
}

type requestListOutput struct {
	Body struct {
		Requests []LeaveRequest `json:"requests" doc:"The requests the caller may read, newest first."`
	}
}

type requestIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80"`
}

type requestOutput struct{ Body LeaveRequest }

type decisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80"`
	Body   struct {
		Decision string `json:"decision" enum:"approve,refuse" doc:"Approve or refuse." example:"approve"`
		Reason   string `json:"reason,omitempty" maxLength:"500" doc:"Why; required to refuse." example:"The release freezes that week."`
	}
}

type settleInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		On string `json:"on,omitempty" format:"date" doc:"Settle leave that ended before this day; today when absent." example:"2026-05-01"`
	}
}

type settleOutput struct {
	Body struct {
		Requests int    `json:"requests" doc:"How many requests were settled." example:"1"`
		Days     string `json:"days" doc:"The days that became taken." example:"3.00"`
	}
}

// leaveScope is the rights a caller holds on leave requests: the people team and the Owner decide on anyone's, Payroll reads anyone's, a line manager decides on
// their reports', and everyone has their own.
func (a *API) leaveScope(ctx context.Context, s Session) (LeaveScope, error) {
	roles, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
	if err != nil {
		return LeaveScope{}, huma.Error500InternalServerError("The roles could not be read.")
	}
	var sc LeaveScope
	for _, r := range roles {
		rights := authz.Rights(r, capLeave)
		switch {
		case r == authz.LineManager && rights[authz.Approve]:
			sc.Reports, sc.Decide = true, true
		case rights[authz.Approve]:
			sc.All, sc.Decide = true, true
		case rights[authz.View]:
			sc.All = true
		}
	}
	return sc, nil
}

func requestsError(err error) error {
	if errors.Is(err, ErrOwnRequest) {
		return Err(CodeRoleRequired, "You cannot decide your own leave request: another approver decides it.")
	}
	return leaveError(err)
}

func (a *API) registerLeaveRequests() {
	ask := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return Session{}, err
		}
		return s, a.leaveOn(ctx, s)
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "previewLeaveRequest", Method: "POST", Path: "/v1/me/leave-requests/preview", Tags: []string{"Leave"}, Summary: "Preview a leave request",
		Description: "Works out what the signed-in person's request would cost, day by day (public holidays and rest days left out for a type counted in working days), the balance after it, and who in their team is away on those days: names and dates, never the leave type. It changes nothing.",
		Errors:      []int{401, 404, 409, 422},
	}, func(ctx context.Context, in *askInput) (*previewOutput, error) {
		s, err := ask(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := a.d.Requests.Preview(ctx, s, in.Body)
		if err != nil {
			return nil, requestsError(err)
		}
		return &previewOutput{Body: p}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "createLeaveRequest", Method: "POST", Path: "/v1/me/leave-requests", Tags: []string{"Leave"}, Summary: "Ask for leave",
		Description: "Asks for leave for the signed-in person: the days are counted and reserved in the ledger in the same transaction, a request beyond the balance is refused with the days left (NM_LEAVE_BALANCE), and one that overlaps leave already asked for is refused (NM_LEAVE_CONFLICT). The approver is told. It is audited.",
		Errors:      []int{401, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *askInput) (*appliedOutput, error) {
		s, err := ask(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, days, err := a.d.Requests.Apply(ctx, s, in.Body)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &appliedOutput{}
		out.Body.ID, out.Body.Days = id, days
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLeaveRequests", Method: "GET", Path: "/v1/leave-requests", Tags: []string{"Leave"}, Summary: "Leave requests the caller may read",
		Description: "Lists the requests in the caller's scope, newest first: their own, their reports' for a line manager, everyone's for the people team, the Owner and the Payroll administrator (Appendix B: Leave requests). Nobody sees a request outside their scope. It changes nothing.",
		Errors:      []int{401, 409},
	}, func(ctx context.Context, in *requestListInput) (*requestListOutput, error) {
		s, err := ask(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Requests.List(ctx, s, sc, in.State)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &requestListOutput{}
		out.Body.Requests = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getLeaveRequest", Method: "GET", Path: "/v1/leave-requests/{id}", Tags: []string{"Leave"}, Summary: "A leave request",
		Description: "Returns one request in the caller's scope; a request outside it is refused as if it did not exist. It changes nothing.",
		Errors:      []int{401, 404, 409},
	}, func(ctx context.Context, in *requestIDInput) (*requestOutput, error) {
		s, err := ask(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such leave request in your view.")
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Requests.Get(ctx, s, sc, id)
		if err != nil {
			return nil, requestsError(err)
		}
		return &requestOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideLeaveRequest", Method: "POST", Path: "/v1/leave-requests/{id}/decision", Tags: []string{"Leave"}, Summary: "Approve or refuse a leave request",
		Description: "The person's line manager decides on their reports' requests and the HR officer, the HR administrator and the Owner on anyone's; nobody decides their own. A refusal needs a reason and gives the reserved days back. A decision is made once (NM_LEAVE_CONFLICT). It is audited and the person is told.",
		Errors:      []int{401, 403, 404, 409, 422},
	}, func(ctx context.Context, in *decisionInput) (*struct{}, error) {
		s, err := ask(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		if !sc.Decide {
			return nil, Err(CodeRoleRequired, "Only a line manager for their reports, the people team and the Owner decide leave requests.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such leave request in your view.")
		}
		if err := a.d.Requests.Decide(ctx, s, sc, id, in.Body.Decision == "approve", in.Body.Reason); err != nil {
			return nil, requestsError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "cancelLeaveRequest", Method: "POST", Path: "/v1/leave-requests/{id}/cancellation", Tags: []string{"Leave"}, Summary: "Cancel one's own leave request",
		Description: "The person withdraws their own request while it is pending, or after it is approved and before it starts, and the reserved days come back. Leave that has started is corrected by the people team. It is audited.",
		Errors:      []int{401, 404, 409},
	}, func(ctx context.Context, in *requestIDInput) (*struct{}, error) {
		s, err := ask(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such leave request in your view.")
		}
		return nil, requestsErrorOrNil(a.d.Requests.Cancel(ctx, s, id))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "settleLeave", Method: "POST", Path: "/v1/leave/settlement", Tags: []string{"Leave"}, Summary: "Record the leave that has been taken",
		Description: "Turns the reserved days of each approved request that ended before the day into taken days, once. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 409, 422},
	}, func(ctx context.Context, in *settleInput) (*settleOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.mayCreate(ctx, s); err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		on, err := a.asOf(in.Body.On)
		if err != nil {
			return nil, err
		}
		n, days, err := a.d.Requests.Settle(ctx, s, on)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &settleOutput{}
		out.Body.Requests, out.Body.Days = n, days
		return out, nil
	})
}

func requestsErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return requestsError(err)
}
