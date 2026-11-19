package api

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// GovAsk is a block of government-paid leave to record for a declared child (NM-LV-07, NM-LV-14).
type GovAsk struct {
	Scheme      string `json:"scheme" enum:"maternity,paternity,shared_parental" doc:"The scheme: maternity, paternity or shared parental leave." example:"paternity"`
	DependantID string `json:"dependant_id" format:"uuid" doc:"The child, as declared among the person's dependants." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"`
	ChildOrder  int    `json:"child_order" minimum:"1" maximum:"20" doc:"1 for a first child, 2 for a second and so on; maternity leave's payer split depends on it." example:"1"`
	From        string `json:"from" format:"date" doc:"The first day of the block." example:"2026-06-10"`
	To          string `json:"to" format:"date" doc:"The last day of the block." example:"2026-07-07"`
}

// GovClaim is the employer's claim for a block, as the portal has it.
type GovClaim struct {
	ID        string `json:"id" format:"uuid" doc:"The claim." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e95"`
	Amount    string `json:"amount" doc:"SGD claimed, to the cent." example:"6000.00"`
	Received  string `json:"received" doc:"SGD received so far." example:"2500.00"`
	FiledOn   string `json:"filed_on" format:"date" doc:"The day it was filed on the portal." example:"2026-07-20"`
	Reference string `json:"reference,omitempty" doc:"The portal's reference." example:"GPL-001"`
	State     string `json:"state" enum:"filed,part_paid,paid,rejected" doc:"Where it is: filed and waiting, part paid, paid in full or rejected." example:"part_paid"`
	OverGuide bool   `json:"over_guide" doc:"The amount was above what the scheme's cap suggests: recorded and flagged, never refused." example:"false"`
}

// GovBlock is a block of government-paid leave with its claims.
type GovBlock struct {
	ID       string     `json:"id" format:"uuid" doc:"The block." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e94"`
	PersonID string     `json:"person_id" format:"uuid" doc:"The person on leave." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person   string     `json:"person_name" doc:"Their name." example:"Ben Lim"`
	Scheme   string     `json:"scheme" enum:"maternity,paternity,shared_parental" doc:"The scheme." example:"paternity"`
	From     string     `json:"from" format:"date" doc:"The first day." example:"2026-06-10"`
	To       string     `json:"to" format:"date" doc:"The last day." example:"2026-07-07"`
	Days     int        `json:"days" doc:"Calendar days: a continuous block counts every day." example:"28"`
	Deadline string     `json:"claim_deadline" format:"date" doc:"The last day to file the employer's claim." example:"2026-10-07"`
	Guide    string     `json:"guide_maximum,omitempty" doc:"The most the scheme's cap suggests the Government reimburses for these days; the portal decides." example:"10000.00"`
	Claims   []GovClaim `json:"claims" doc:"The claims filed for it."`
}

// Government is government-paid leave behind the API.
type Government interface {
	Record(ctx context.Context, s Session, person uuid.UUID, a GovAsk) (string, error)
	List(ctx context.Context, s Session) ([]GovBlock, error)
	File(ctx context.Context, s Session, block uuid.UUID, amount string, filedOn time.Time, reference string) (id string, overGuide bool, err error)
	Event(ctx context.Context, s Session, claim uuid.UUID, kind, amount string, on time.Time, note string) error
}

func init() {
	IdempotencyExempt["POST /v1/people/{id}/government-paid-leave"] = "a repeat overlaps the first block's days, which the database refuses (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/government-paid-leave/{id}/claims"] = "a claim may be repeated or partial by design: each is its own record with the portal's reference"
	IdempotencyExempt["POST /v1/government-paid-claims/{id}/events"] = "receipts and rejections are separate records of what the portal answered, and may repeat"
}

type govRecordInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   GovAsk
}

type govListInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type govListOutput struct {
	Body struct {
		Blocks []GovBlock `json:"blocks" doc:"Every block of government-paid leave, soonest claim deadline first."`
	}
}

type claimInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The block." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e94"`
	Body   struct {
		Amount    string `json:"amount" doc:"SGD claimed, as a decimal string with at most two places." example:"6000.00"`
		FiledOn   string `json:"filed_on" format:"date" doc:"The day it was filed on the portal." example:"2026-07-20"`
		Reference string `json:"reference,omitempty" maxLength:"100" doc:"The portal's reference." example:"GPL-001"`
	}
}

type claimOutput struct {
	Body struct {
		ID        string `json:"id" format:"uuid" doc:"The new claim." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e95"`
		OverGuide bool   `json:"over_guide" doc:"The amount is above what the scheme's cap suggests." example:"false"`
	}
}

type claimEventInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The claim." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e95"`
	Body   struct {
		Kind   string `json:"kind" enum:"receipt,rejection" doc:"A receipt of some or all of the amount, or a rejection." example:"receipt"`
		Amount string `json:"amount,omitempty" doc:"SGD received, for a receipt; none for a rejection." example:"2500.00"`
		On     string `json:"on" format:"date" doc:"The day it happened." example:"2026-08-10"`
		Note   string `json:"note,omitempty" maxLength:"500" doc:"What the portal said." example:"Paid in two parts."`
	}
}

func (a *API) registerGov() {
	// Reading is for the people team, the Owner and Payroll; writing for the HR officer and the HR administrator.
	reader := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return Session{}, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return s, err
		}
		sc, err := a.leaveScope(ctx, s)
		if err != nil {
			return s, err
		}
		if !sc.All {
			return s, Err(CodeRoleRequired, "Only the people team, the Owner and the Payroll administrator see government-paid leave and its claims.")
		}
		return s, nil
	}
	writer := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return Session{}, err
		}
		if err := a.mayCreate(ctx, s); err != nil {
			return s, err
		}
		return s, a.leaveOn(ctx, s)
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "recordGovernmentPaidLeave", Method: "POST", Path: "/v1/people/{id}/government-paid-leave", Tags: []string{"Leave"}, Summary: "Record a block of government-paid leave",
		Description: "Records maternity, paternity or shared parental leave for a child the person declared, under the country pack's rule for the child's birth date: a Singapore Citizen child, the months of service the scheme asks for, its window and weeks. A block never overlaps other leave. The claim deadline is worked out. The HR officer and the HR administrator. It is audited; the child's date of birth is not in the audit event.",
		Errors:      []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *govRecordInput) (*createdOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		block, err := a.d.Government.Record(ctx, s, id, in.Body)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &createdOutput{}
		out.Body.ID = block
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listGovernmentPaidLeave", Method: "GET", Path: "/v1/government-paid-leave", Tags: []string{"Leave"}, Summary: "Government-paid leave and its claims",
		Description: "Lists each block with its claim deadline, the most the scheme's cap suggests the Government reimburses, and the claims filed with what has been received. The people team, the Owner and the Payroll administrator. It changes nothing.",
		Errors:      []int{401, 403, 409},
	}, func(ctx context.Context, in *govListInput) (*govListOutput, error) {
		s, err := reader(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Government.List(ctx, s)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &govListOutput{}
		out.Body.Blocks = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "fileGovernmentPaidClaim", Method: "POST", Path: "/v1/government-paid-leave/{id}/claims", Tags: []string{"Leave"}, Summary: "Record a claim filed on the portal",
		Description: "Records the employer's claim a person filed on the Government-Paid Leave portal: the amount in SGD, the day and the portal's reference. A claim above what the scheme's cap suggests is recorded and flagged, never refused; partial and repeated claims are allowed. Nemura moves no money. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *claimInput) (*claimOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such government-paid leave record.")
		}
		on, err := date("filed_on", in.Body.FiledOn, true)
		if err != nil {
			return nil, Err(CodeLeaveInvalid, "filed_on is not a date: write it as 2026-07-20.")
		}
		claim, over, err := a.d.Government.File(ctx, s, id, in.Body.Amount, *on, in.Body.Reference)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &claimOutput{}
		out.Body.ID, out.Body.OverGuide = claim, over
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "recordGovernmentPaidClaimEvent", Method: "POST", Path: "/v1/government-paid-claims/{id}/events", Tags: []string{"Leave"}, Summary: "Record a receipt or a rejection of a claim",
		Description: "Records what the portal answered: a receipt of some or all of the amount, or a rejection. A claim's state is worked out from its events. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *claimEventInput) (*struct{}, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such claim.")
		}
		on, err := date("on", in.Body.On, true)
		if err != nil {
			return nil, Err(CodeLeaveInvalid, "on is not a date: write it as 2026-08-10.")
		}
		return nil, requestsErrorOrNil(a.d.Government.Event(ctx, s, id, in.Body.Kind, in.Body.Amount, *on, in.Body.Note))
	})
}
