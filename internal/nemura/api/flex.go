package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// FlexAsk is a formal flexible work arrangement request (NM-LV-10).
type FlexAsk struct {
	Arrangement string `json:"arrangement" enum:"flexi_time,flexi_place,flexi_load" doc:"Flexi-time (when), flexi-place (where) or flexi-load (how much)." example:"flexi_time"`
	From        string `json:"from" format:"date" doc:"The day the arrangement would start." example:"2027-01-04"`
	To          string `json:"to,omitempty" format:"date" doc:"The day it would end; open when absent." example:"2027-06-30"`
	Reason      string `json:"reason" minLength:"1" maxLength:"2000" doc:"Why the arrangement is wanted." example:"School run on Tuesdays."`
}

// FlexRequest is a request with where it is.
type FlexRequest struct {
	ID          string `json:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea0"`
	PersonID    string `json:"person_id" format:"uuid" doc:"The person asking." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person      string `json:"person_name" doc:"Their name." example:"Ben Lim"`
	Arrangement string `json:"arrangement" enum:"flexi_time,flexi_place,flexi_load" doc:"The kind of arrangement." example:"flexi_time"`
	From        string `json:"from" format:"date" doc:"The day it would start." example:"2027-01-04"`
	To          string `json:"to,omitempty" format:"date" doc:"The day it would end." example:"2027-06-30"`
	Reason      string `json:"reason" doc:"The person's reason." example:"School run on Tuesdays."`
	State       string `json:"state" enum:"received,replied,withdrawn" doc:"Received and waiting for the written reply, replied, or withdrawn." example:"received"`
	Received    string `json:"received_on" format:"date" doc:"The formal receipt date, which never restarts." example:"2026-11-20"`
	Due         string `json:"reply_due" format:"date" doc:"The last day for the written reply: the country pack's months after receipt." example:"2027-01-20"`
	Outcome     string `json:"outcome,omitempty" enum:"accepted,rejected,alternative" doc:"How the employer answered." example:"alternative"`
	Reply       string `json:"written_reply,omitempty" doc:"The employer's written reply, with its reason for a refusal." example:"We can offer a 9am start on Tuesdays."`
	RepliedAt   string `json:"replied_at,omitempty" format:"date-time" doc:"When it was answered." example:"2026-11-21T10:00:00Z"`
}

// Flex is flexible work requests behind the API.
type Flex interface {
	Request(ctx context.Context, s Session, a FlexAsk) (id, due string, err error)
	List(ctx context.Context, s Session, all bool) ([]FlexRequest, error)
	Reply(ctx context.Context, s Session, id uuid.UUID, outcome, written string) error
	Withdraw(ctx context.Context, s Session, id uuid.UUID) error
}

func init() {
}

type flexInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   FlexAsk
}

type flexRequestedOutput struct {
	Body struct {
		ID  string `json:"id" format:"uuid" doc:"The new request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea0"`
		Due string `json:"reply_due" format:"date" doc:"The last day for the employer's written reply." example:"2027-01-20"`
	}
}

type flexListOutput struct {
	Body struct {
		Requests []FlexRequest `json:"requests" doc:"The requests the caller may read: their own, or everyone's for the people team; those waiting first, by deadline."`
	}
}

type flexReplyInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea0"`
	Body   struct {
		Outcome string `json:"outcome" enum:"accepted,rejected,alternative" doc:"Accept, refuse, or offer an alternative." example:"alternative"`
		Reply   string `json:"written_reply" minLength:"1" maxLength:"4000" doc:"The written reply; a refusal or an alternative gives the business grounds." example:"We can offer a 9am start on Tuesdays."`
	}
}

func (a *API) registerFlex() {
	huma.Register(a.API, huma.Operation{
		OperationID: "requestFlexibleWork", Method: "POST", Path: "/v1/me/flexible-work-requests", Tags: []string{"Leave"}, Summary: "Make a formal flexible work request",
		Description: "Records the signed-in person's formal request (type, start, end and reason). Its receipt date is today and never restarts; the employer's written reply is due the country pack's months after (two). The people team is told. It is audited.",
		Errors:      []int{401, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *flexInput) (*flexRequestedOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		id, due, err := a.d.Flex.Request(ctx, s, in.Body)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &flexRequestedOutput{}
		out.Body.ID, out.Body.Due = id, due
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listFlexibleWorkRequests", Method: "GET", Path: "/v1/flexible-work-requests", Tags: []string{"Leave"}, Summary: "Flexible work requests the caller may read",
		Description: "Lists the caller's own requests; the people team (HR officer, HR administrator, Owner) reads everyone's. Requests waiting come first, by deadline. It changes nothing.",
		Errors:      []int{401, 409},
	}, func(ctx context.Context, in *requestListInput) (*flexListOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Flex.List(ctx, s, sc.All && sc.Decide && !in.Mine)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &flexListOutput{}
		out.Body.Requests = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "replyFlexibleWork", Method: "POST", Path: "/v1/flexible-work-requests/{id}/reply", Tags: []string{"Leave"}, Summary: "Answer a flexible work request in writing",
		Description: "The HR officer, the HR administrator or the Owner records the employer's written reply, once and never to their own request. A refusal or an alternative gives the business grounds. The reply is the record the guidelines ask the employer to keep. It is audited and the person is told.",
		Errors:      []int{401, 403, 404, 409, 422},
	}, func(ctx context.Context, in *flexReplyInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return nil, err
		}
		if !(sc.All && sc.Decide) {
			return nil, Err(CodeRoleRequired, "Only the HR officer, the HR administrator and the Owner answer flexible work requests.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such flexible work request.")
		}
		return nil, requestsErrorOrNil(a.d.Flex.Reply(ctx, s, id, in.Body.Outcome, in.Body.Reply))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "withdrawFlexibleWork", Method: "POST", Path: "/v1/flexible-work-requests/{id}/withdrawal", Tags: []string{"Leave"}, Summary: "Withdraw one's own flexible work request",
		Description: "The person withdraws their own request while it waits for a reply. It is audited.",
		Errors:      []int{401, 404, 409},
	}, func(ctx context.Context, in *requestIDInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such flexible work request.")
		}
		return nil, requestsErrorOrNil(a.d.Flex.Withdraw(ctx, s, id))
	})
}
