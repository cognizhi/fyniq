package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// Problem codes of onboarding (the catalogue is in problem.go).
const (
	CodeOnboardingInvalid  = onboarding.CodeInvalid
	CodeOnboardingConflict = onboarding.CodeConflict
)

// JoinTask is the task to invite a new person to Fyniq (NM-ONB-02).
type JoinTask struct {
	ID       string `json:"id" format:"uuid" doc:"The task." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f20"`
	PersonID string `json:"person_id" format:"uuid" doc:"The new person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person   string `json:"person" doc:"Their legal name." example:"Ben Lim"`
	Start    string `json:"start_date" format:"date" doc:"The day they start." example:"2026-12-01"`
	State    string `json:"state" enum:"open,done,dismissed" doc:"Waiting; the person was invited; or it does not apply to them." example:"open"`
	Raised   string `json:"raised_at" format:"date-time" doc:"When the task was raised." example:"2026-11-20T10:00:00Z"`
	Note     string `json:"note,omitempty" doc:"The note left when it was decided." example:"Invited as an employee."`
}

// Settlement is the annual leave a leaver is left with on their last day, in days; what it is worth comes from payroll.
type Settlement struct {
	Year     int      `json:"year" doc:"The leave year of the last day." example:"2026"`
	Entitled string   `json:"entitled" doc:"The annual leave prorated to the last day, as a decimal string." example:"13.00"`
	Taken    string   `json:"taken" doc:"The days taken." example:"10.00"`
	Booked   string   `json:"booked" doc:"The days still booked after the last day, to cancel." example:"0.00"`
	Untaken  string   `json:"untaken" doc:"Entitled less taken: negative when the person took more than the prorated entitlement." example:"3.00"`
	Basis    []string `json:"basis" doc:"The steps of the calculation, one plain line each."`
}

// Offboarding is a leaver's dates, checklist and leave settlement (NM-ONB-03).
type Offboarding struct {
	ID                   string      `json:"id" format:"uuid" doc:"The leaving." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f21"`
	PersonID             string      `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person               string      `json:"person" doc:"Their legal name." example:"Chen Yu"`
	Kind                 string      `json:"kind" enum:"resignation,termination,retrenchment,end_of_contract,retirement,other" doc:"How the employment ends." example:"resignation"`
	NoticeGiven          string      `json:"notice_given_on,omitempty" format:"date" doc:"When notice was given or the company learned of the cessation: it starts the tax-clearance hold." example:"2026-10-01"`
	LastDay              string      `json:"last_day" format:"date" doc:"The last day of employment, the notice period included." example:"2026-11-30"`
	RetrenchmentNotified string      `json:"retrenchment_notified_on,omitempty" format:"date" doc:"For a retrenchment, when employees were told." example:"2026-09-28"`
	EquipmentReturned    string      `json:"equipment_returned_on,omitempty" format:"date" doc:"When equipment was returned." example:"2026-11-30"`
	ExitInterview        string      `json:"exit_interview_on,omitempty" format:"date" doc:"When the exit interview was held." example:"2026-11-27"`
	FyniqAccessRemoved   string      `json:"fyniq_access_removed_on,omitempty" format:"date" doc:"When the person's Fyniq access was removed." example:"2026-11-30"`
	NemuraAccessNarrows  string      `json:"nemura_access_narrows_on" format:"date" doc:"The day the person's Nemura account becomes the narrow one for their own documents: the day after the last day." example:"2026-12-01"`
	ServiceLetter        string      `json:"service_letter" enum:"none,requested,checked,issued,refused,withdrawn" doc:"Where the person's service letter is: none asked for yet, or the latest request's state." example:"none"`
	Settlement           *Settlement `json:"leave_settlement,omitempty" doc:"The annual leave to the last day; left out when the person has no legal classification or no pack covers the year."`
}

// OffboardingInput is a leaving to record or change.
type OffboardingInput struct {
	Kind                 string `json:"kind" enum:"resignation,termination,retrenchment,end_of_contract,retirement,other" doc:"How the employment ends." example:"resignation"`
	NoticeGiven          string `json:"notice_given_on,omitempty" format:"date" doc:"When notice was given or the company learned of the cessation." example:"2026-10-01"`
	LastDay              string `json:"last_day" format:"date" doc:"The last day of employment, the notice period included." example:"2026-11-30"`
	RetrenchmentNotified string `json:"retrenchment_notified_on,omitempty" format:"date" doc:"For a retrenchment only, when employees were told." example:"2026-09-28"`
	EquipmentReturned    string `json:"equipment_returned_on,omitempty" format:"date" doc:"When equipment was returned." example:"2026-11-30"`
	ExitInterview        string `json:"exit_interview_on,omitempty" format:"date" doc:"When the exit interview was held." example:"2026-11-27"`
	FyniqAccessRemoved   string `json:"fyniq_access_removed_on,omitempty" format:"date" doc:"When the person's Fyniq access was removed." example:"2026-11-30"`
}

// Onboarding is the joiner's task and the leaver's dates behind the API.
type Onboarding interface {
	Settings(ctx context.Context, s Session) (inviteToFyniq bool, err error)
	SetSettings(ctx context.Context, s Session, inviteToFyniq bool) error
	Tasks(ctx context.Context, s Session) ([]JoinTask, error)
	DecideTask(ctx context.Context, s Session, id uuid.UUID, state, note string) error
	Offboardings(ctx context.Context, s Session) ([]Offboarding, error)
	Offboarding(ctx context.Context, s Session, person uuid.UUID) (Offboarding, bool, error)
	SetOffboarding(ctx context.Context, s Session, person uuid.UUID, in OffboardingInput) error
	CancelOffboarding(ctx context.Context, s Session, person uuid.UUID) error
}

func init() {
}

func onboardingError(err error) error {
	var r *onboarding.Refusal
	var p *people.Refusal
	switch {
	case errors.As(err, &r):
		return Err(r.Code, r.Message)
	case errors.As(err, &p):
		return Err(p.Code, p.Message)
	}
	return huma.Error500InternalServerError("The request could not be handled.")
}

func onboardingErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return onboardingError(err)
}

type onboardingSettingsOutput struct {
	Body struct {
		Invite bool `json:"invite_to_fyniq" doc:"Whether adding a person raises a task to invite them to Fyniq." example:"true"`
	}
}

type onboardingSettingsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Invite bool `json:"invite_to_fyniq" doc:"Raise a task to invite each new person to Fyniq." example:"true"`
	}
}

type joinTasksOutput struct {
	Body struct {
		Tasks []JoinTask `json:"tasks" doc:"The tasks, the open ones first."`
	}
}

type joinDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The task." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f20"`
	Body   struct {
		State string `json:"state" enum:"done,dismissed" doc:"Done once the person is invited; dismissed when it does not apply, such as a person who does not claim expenses." example:"done"`
		Note  string `json:"note,omitempty" maxLength:"300" doc:"A note." example:"Invited as an employee."`
	}
}

type offboardingsOutput struct {
	Body struct {
		Leavings []Offboarding `json:"leavings" doc:"The active leavings, the soonest last day first."`
	}
}

type offboardingOutput struct {
	Body struct {
		Recorded bool        `json:"recorded" doc:"Whether the person has a leaving recorded." example:"true"`
		Leaving  Offboarding `json:"leaving" doc:"The leaving."`
	}
}

type offboardingSetInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   OffboardingInput
}

func (a *API) registerOnboarding() {
	// inviter is whoever may be asked to invite someone to Fyniq: the HR administrator, the Administrator and the Owner.
	inviter := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		roles, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
		if err != nil {
			return s, huma.Error500InternalServerError("The roles could not be read.")
		}
		for _, r := range roles {
			if r == authz.HRAdmin || r == authz.Administrator || r == authz.Owner {
				return s, nil
			}
		}
		return s, Err(CodeRoleRequired, "Only the HR administrator, the Administrator and the Owner handle invitations.")
	}
	hrTeam := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return s, err
		}
		if !p.Team {
			return s, Err(CodeRoleRequired, "Only the HR officer, the HR administrator and the Owner record a person's leaving.")
		}
		return s, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "getOnboardingSettings", Method: "GET", Path: "/v1/onboarding/settings", Tags: []string{"Onboarding"}, Summary: "Whether new people are invited to Fyniq",
		Description: "Whether adding a person raises a task to invite them to Fyniq. The HR administrator, the Administrator and the Owner. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *selfInput) (*onboardingSettingsOutput, error) {
		s, err := inviter(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		on, err := a.d.Onboarding.Settings(ctx, s)
		if err != nil {
			return nil, onboardingError(err)
		}
		out := &onboardingSettingsOutput{}
		out.Body.Invite = on
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setOnboardingSettings", Method: "PUT", Path: "/v1/onboarding/settings", Tags: []string{"Onboarding"}, Summary: "Invite new people to Fyniq",
		Description: "Switches on or off the task to invite each new person to Fyniq. Switching on raises no task for people already added. The HR administrator and the Owner. It is audited.", Errors: []int{401, 403}, DefaultStatus: 204,
	}, func(ctx context.Context, in *onboardingSettingsInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return nil, err
		}
		if !p.Admin {
			return nil, Err(CodeRoleRequired, "Only the HR administrator and the Owner change this.")
		}
		return nil, onboardingErrorOrNil(a.d.Onboarding.SetSettings(ctx, s, in.Body.Invite))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listJoinTasks", Method: "GET", Path: "/v1/onboarding/tasks", Tags: []string{"Onboarding"}, Summary: "Tasks to invite new people to Fyniq",
		Description: "The tasks to invite new people to Fyniq with the Employee role, the open ones first. Nemura never grants a Fyniq role itself: whoever may invite does it in Fyniq and marks the task done. The HR administrator, the Administrator and the Owner. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *selfInput) (*joinTasksOutput, error) {
		s, err := inviter(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Onboarding.Tasks(ctx, s)
		if err != nil {
			return nil, onboardingError(err)
		}
		out := &joinTasksOutput{}
		out.Body.Tasks = list
		if list == nil {
			out.Body.Tasks = []JoinTask{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideJoinTask", Method: "POST", Path: "/v1/onboarding/tasks/{id}/decision", Tags: []string{"Onboarding"}, Summary: "Mark an invitation task done or dismissed",
		Description: "Marks a task done (the person was invited in Fyniq) or dismissed (it does not apply), once. The HR administrator, the Administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 422}, DefaultStatus: 204,
	}, func(ctx context.Context, in *joinDecisionInput) (*struct{}, error) {
		s, err := inviter(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such task waiting.")
		}
		return nil, onboardingErrorOrNil(a.d.Onboarding.DecideTask(ctx, s, id, in.Body.State, in.Body.Note))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLeavings", Method: "GET", Path: "/v1/offboardings", Tags: []string{"Onboarding"}, Summary: "People who are leaving",
		Description: "The active leavings, the soonest last day first, each with its dates kept apart, its checklist, where the service letter is and the leave settlement to the last day. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *selfInput) (*offboardingsOutput, error) {
		s, err := hrTeam(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Onboarding.Offboardings(ctx, s)
		if err != nil {
			return nil, onboardingError(err)
		}
		out := &offboardingsOutput{}
		out.Body.Leavings = list
		if list == nil {
			out.Body.Leavings = []Offboarding{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getLeaving", Method: "GET", Path: "/v1/people/{id}/offboarding", Tags: []string{"Onboarding"}, Summary: "A person's leaving",
		Description: "The person's active leaving, when there is one. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *idInput) (*offboardingOutput, error) {
		s, err := hrTeam(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		l, ok, err := a.d.Onboarding.Offboarding(ctx, s, id)
		if err != nil {
			return nil, onboardingError(err)
		}
		out := &offboardingOutput{}
		out.Body.Recorded, out.Body.Leaving = ok, l
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setLeaving", Method: "PUT", Path: "/v1/people/{id}/offboarding", Tags: []string{"Onboarding"}, Summary: "Record or change a person's leaving",
		Description: "Records how and when a person leaves: the day notice was given (it starts the tax-clearance hold), the last day (it prorates the leave and, the day after, narrows the account), for a retrenchment the day employees were told, and the checklist. It sets the leaving date on the person's record. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 422}, DefaultStatus: 204,
	}, func(ctx context.Context, in *offboardingSetInput) (*struct{}, error) {
		s, err := hrTeam(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		return nil, onboardingErrorOrNil(a.d.Onboarding.SetOffboarding(ctx, s, id, in.Body))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "cancelLeaving", Method: "POST", Path: "/v1/people/{id}/offboarding/cancellation", Tags: []string{"Onboarding"}, Summary: "Withdraw a person's leaving",
		Description: "Withdraws a leaving before its last day has passed, such as a resignation withdrawn: the leaving date on the record is cleared. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409}, DefaultStatus: 204,
	}, func(ctx context.Context, in *idInput) (*struct{}, error) {
		s, err := hrTeam(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		return nil, onboardingErrorOrNil(a.d.Onboarding.CancelOffboarding(ctx, s, id))
	})
}
