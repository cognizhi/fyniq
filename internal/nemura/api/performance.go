package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/nemura/performance"
)

// Goals and KPIs (PRD-002 NM-PRF-01, NM-PRF-02): company, team and personal goals with measurable key results, and role KPIs, each with a target and updated by a person. Company and team goals are read by every
// member so that a personal goal can align with them; a personal goal and a KPI are read by the person, their line manager and the people team, and Nemura never holds a company financial figure. Nothing here is on
// the AI or MCP surface.

// Measure is a key result of a goal or a role KPI.
type Measure struct {
	ID       string `json:"id" format:"uuid" doc:"The measure." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4b01"`
	Title    string `json:"title" doc:"What is measured." example:"Pilot customers live"`
	Unit     string `json:"unit,omitempty" doc:"Its unit." example:"customers"`
	Start    string `json:"start_value" doc:"Where it started, a decimal string." example:"0"`
	Target   string `json:"target" doc:"Where it should get to, a decimal string." example:"3"`
	Current  string `json:"current" doc:"Its latest value, a decimal string." example:"2"`
	Down     bool   `json:"lower_is_better" doc:"Lower is better: the target is below the start value." example:"false"`
	Progress int    `json:"progress" minimum:"0" maximum:"100" doc:"How far from the start value to the target, a whole percentage rounded down." example:"66"`
	Status   string `json:"status" enum:"done,on_track,behind,not_yet" doc:"Done at 100%; on track when progress keeps up with the share of the period that has passed; behind otherwise; not yet for a KPI that has not reached its target." example:"on_track"`
}

// Goal is a company, team or personal goal.
type Goal struct {
	ID       string    `json:"id" format:"uuid" doc:"The goal." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Scope    string    `json:"scope" enum:"company,team,personal" doc:"Whose goal it is." example:"personal"`
	PersonID string    `json:"person_id,omitempty" format:"uuid" doc:"A personal goal's owner." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person   string    `json:"person,omitempty" doc:"Their legal name." example:"Ben Lim"`
	Team     string    `json:"team,omitempty" doc:"A team goal's team." example:"Engineering"`
	ParentID string    `json:"parent_id,omitempty" format:"uuid" doc:"The goal it aligns with." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a00"`
	Title    string    `json:"title" doc:"The goal." example:"3 pilot customers live by 31 December"`
	StartsOn string    `json:"starts_on" format:"date" doc:"When it began." example:"2026-07-01"`
	DueOn    string    `json:"due_on" format:"date" doc:"When it is due." example:"2026-12-31"`
	State    string    `json:"state" enum:"open,done,cancelled" doc:"Where it is." example:"open"`
	Progress int       `json:"progress" minimum:"0" maximum:"100" doc:"The average progress of its key results, a whole percentage rounded down; 0 with none." example:"60"`
	Measures []Measure `json:"measures" nullable:"false" doc:"Its key results."`
}

// MeasureInput is a key result or KPI to set.
type MeasureInput struct {
	Title  string `json:"title" minLength:"1" maxLength:"200" doc:"What is measured." example:"Pilot customers live"`
	Unit   string `json:"unit,omitempty" maxLength:"30" doc:"Its unit." example:"customers"`
	Start  string `json:"start_value,omitempty" pattern:"^-?[0-9]{1,12}(\\.[0-9]{1,4})?$" doc:"Where it starts, a decimal string; 0 when left out." example:"0"`
	Target string `json:"target" pattern:"^-?[0-9]{1,12}(\\.[0-9]{1,4})?$" doc:"Where it should get to, a decimal string; below the start value means lower is better." example:"3"`
}

// GoalInput is a goal to set.
type GoalInput struct {
	Scope    string         `json:"scope" enum:"company,team,personal" doc:"Whose goal it is." example:"personal"`
	PersonID string         `json:"person_id,omitempty" format:"uuid" doc:"For a personal goal, its owner; the caller when left out." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Team     string         `json:"team,omitempty" maxLength:"120" doc:"For a team goal, the team." example:"Engineering"`
	ParentID string         `json:"parent_id,omitempty" format:"uuid" doc:"The goal it aligns with." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a00"`
	Title    string         `json:"title" minLength:"1" maxLength:"200" doc:"The goal." example:"3 pilot customers live by 31 December"`
	DueOn    string         `json:"due_on" format:"date" doc:"When it is due: today or later." example:"2026-12-31"`
	Measures []MeasureInput `json:"measures,omitempty" maxItems:"10" doc:"Its key results."`
}

// Performance is goals and KPIs behind the API.
type Performance interface {
	Mine(ctx context.Context, s Session) (company, team, mine []Goal, kpis []Measure, err error)
	OfPerson(ctx context.Context, s Session, held []authz.Role, person uuid.UUID) (goals []Goal, kpis []Measure, err error)
	SetGoal(ctx context.Context, s Session, held []authz.Role, in GoalInput) (Goal, error)
	CloseGoal(ctx context.Context, s Session, held []authz.Role, id uuid.UUID, state string) error
	Update(ctx context.Context, s Session, held []authz.Role, measure uuid.UUID, value, note string) (Measure, error)
	SetKPI(ctx context.Context, s Session, held []authz.Role, person uuid.UUID, in MeasureInput) (Measure, error)
}

type myGoalsOutput struct {
	Body struct {
		Company []Goal    `json:"company" nullable:"false" doc:"The company's goals."`
		Team    []Goal    `json:"team" nullable:"false" doc:"The teams' goals."`
		Mine    []Goal    `json:"mine" nullable:"false" doc:"The caller's own goals."`
		KPIs    []Measure `json:"kpis" nullable:"false" doc:"The caller's role KPIs."`
	}
}
type personGoalsOutput struct {
	Body struct {
		Goals []Goal    `json:"goals" nullable:"false" doc:"The person's goals."`
		KPIs  []Measure `json:"kpis" nullable:"false" doc:"The person's role KPIs."`
	}
}
type goalInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   GoalInput
}
type goalOutput struct{ Body Goal }
type goalClosingInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The goal." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		State string `json:"state" enum:"done,cancelled" doc:"Done, or cancelled when it no longer applies." example:"done"`
	}
}
type measureUpdateInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The measure." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4b01"`
	Body   struct {
		Value string `json:"value" pattern:"^-?[0-9]{1,12}(\\.[0-9]{1,4})?$" doc:"The value now, a decimal string." example:"2"`
		Note  string `json:"note,omitempty" maxLength:"300" doc:"A note." example:"Second pilot went live on Monday."`
	}
}
type measureOutput struct{ Body Measure }
type kpiInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   MeasureInput
}

func performanceError(err error) error {
	var r *performance.Refusal
	if errors.As(err, &r) {
		return Err(r.Code, r.Message)
	}
	return huma.Error500InternalServerError("The goal could not be handled.")
}

func init() {
	IdempotencyExempt["POST /v1/goals"] = "a goal set twice shows twice and can be cancelled; its words never change"
	IdempotencyExempt["POST /v1/goals/{id}/closing"] = "a goal is closed once: a second is refused (NM_PERFORMANCE_CONFLICT)"
	IdempotencyExempt["POST /v1/measures/{id}/updates"] = "an update is one more line of the measure's history; the latest value wins"
	IdempotencyExempt["POST /v1/people/{id}/kpis"] = "a KPI set twice shows twice; its target never changes"
}

func (a *API) registerPerformance() {
	available := func() error { return Err(CodeUnavailable, "Goals are not set up in this deployment.") }
	// member is any signed-in member with Performance on and the roles the rights are decided from; what each may read or change is decided with the goal and the person in the same transaction.
	member := func(ctx context.Context, cookie string) (Session, []authz.Role, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, nil, err
		}
		held, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
		if err != nil {
			return s, nil, huma.Error500InternalServerError("The roles could not be read.")
		}
		sw, err := a.d.Features.List(ctx, s)
		if err != nil {
			return s, nil, huma.Error500InternalServerError("The feature switches could not be read.")
		}
		if sw["performance"].State != features.On {
			return s, nil, Err(CodeFeatureOff, "Performance is switched off for this organisation: an Owner or the HR administrator switches it on under Settings, Features.")
		}
		if a.d.Performance == nil {
			return s, nil, available()
		}
		return s, held, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listMyGoals", Method: http.MethodGet, Path: "/v1/me/goals", Tags: []string{"Performance"}, Summary: "My goals",
		Description: "PRD-002 NM-PRF-01, NM-PRF-02. The company's and the teams' goals, which every member reads so that a personal goal can align with one, and the caller's own goals and role KPIs, each key result with its progress and where it stands. Any signed-in member. It changes nothing.", Errors: []int{401, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*myGoalsOutput, error) {
		s, _, err := member(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		co, te, mine, kpis, err := a.d.Performance.Mine(ctx, s)
		if err != nil {
			return nil, performanceError(err)
		}
		out := &myGoalsOutput{}
		out.Body.Company, out.Body.Team, out.Body.Mine, out.Body.KPIs = append([]Goal{}, co...), append([]Goal{}, te...), append([]Goal{}, mine...), append([]Measure{}, kpis...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listPersonGoals", Method: http.MethodGet, Path: "/v1/people/{id}/goals", Tags: []string{"Performance"}, Summary: "A person's goals and KPIs",
		Description: "A person's personal goals and role KPIs. The person reads their own, their line manager their reports', and the HR officer, the HR administrator and the Owner anyone's; anyone else is told there is no such person. It changes nothing.", Errors: []int{401, 404, 409, 503},
	}, func(ctx context.Context, in *idInput) (*personGoalsOutput, error) {
		s, held, err := member(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		goals, kpis, err := a.d.Performance.OfPerson(ctx, s, held, p)
		if err != nil {
			return nil, performanceError(err)
		}
		out := &personGoalsOutput{}
		out.Body.Goals, out.Body.KPIs = append([]Goal{}, goals...), append([]Measure{}, kpis...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setGoal", Method: http.MethodPost, Path: "/v1/goals", Tags: []string{"Performance"}, DefaultStatus: http.StatusCreated, Summary: "Set a goal",
		Description: "Sets a company, team or personal goal with up to ten key results, each a number from a start value to a target (decimal strings; a target below the start value means lower is better). The HR administrator or the Owner sets company goals; they and any line manager set team goals; a person sets their own goals, their line manager their reports', and the HR administrator and the Owner anyone's. A personal goal may align with a goal of the company or team. A goal's words never change. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *goalInput) (*goalOutput, error) {
		s, held, err := member(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		g, err := a.d.Performance.SetGoal(ctx, s, held, in.Body)
		if err != nil {
			return nil, performanceError(err)
		}
		return &goalOutput{Body: g}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "closeGoal", Method: http.MethodPost, Path: "/v1/goals/{id}/closing", Tags: []string{"Performance"}, DefaultStatus: http.StatusNoContent, Summary: "Mark a goal done or cancelled",
		Description: "Marks a goal done or cancelled, once, by whoever may change it. It is audited.", Errors: []int{401, 404, 409, 503},
	}, func(ctx context.Context, in *goalClosingInput) (*struct{}, error) {
		s, held, err := member(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such goal for you.")
		}
		return nil, performanceErrorOrNil(a.d.Performance.CloseGoal(ctx, s, held, id, in.Body.State))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "updateMeasure", Method: http.MethodPost, Path: "/v1/measures/{id}/updates", Tags: []string{"Performance"}, DefaultStatus: http.StatusCreated, Summary: "Update a key result or KPI",
		Description: "Records the value of a key result or role KPI now, with who gave it and when: the history is kept and the latest value is the measure's. By whoever may change the goal, or by the person for their own KPI. It is audited.", Errors: []int{401, 404, 409, 422, 503},
	}, func(ctx context.Context, in *measureUpdateInput) (*measureOutput, error) {
		s, held, err := member(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such measure for you.")
		}
		m, err := a.d.Performance.Update(ctx, s, held, id, in.Body.Value, in.Body.Note)
		if err != nil {
			return nil, performanceError(err)
		}
		return &measureOutput{Body: m}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setKPI", Method: http.MethodPost, Path: "/v1/people/{id}/kpis", Tags: []string{"Performance"}, DefaultStatus: http.StatusCreated, Summary: "Set a role KPI",
		Description: "Sets a role KPI for a person with its target, entered by a person; a KPI read from Fyniq's metrics arrives with the link. The person's line manager, the HR administrator and the Owner. It is audited.", Errors: []int{401, 404, 409, 422, 503},
	}, func(ctx context.Context, in *kpiInput) (*measureOutput, error) {
		s, held, err := member(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		m, err := a.d.Performance.SetKPI(ctx, s, held, p, in.Body)
		if err != nil {
			return nil, performanceError(err)
		}
		return &measureOutput{Body: m}, nil
	})
}

func performanceErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return performanceError(err)
}
