package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
)

// Onboarding plans (PRD-002 NM-ONB-01): templates of tasks for HR, the manager, IT and the new joiner, and a plan made from a template for a person, each task due a number of days from their start date.
// The HR team makes templates and plans; each person reads and decides the tasks that are theirs. Nothing here is on the AI or MCP surface.

// PlanTemplateTask is a task of a template.
type PlanTemplateTask struct {
	Title         string `json:"title" minLength:"1" maxLength:"200" doc:"What is to be done." example:"Prepare the laptop and equipment"`
	Detail        string `json:"detail,omitempty" maxLength:"1000" doc:"How, or what good looks like." example:"Order, set up and label what the role needs."`
	Assignee      string `json:"assignee" enum:"hr,manager,it,joiner" doc:"Who it is for: the HR team, the joiner's line manager, IT (an Administrator, HR administrator or the Owner) or the joiner." example:"it"`
	DueOffsetDays int    `json:"due_offset_days" minimum:"-60" maximum:"365" doc:"Days from the start date it is due: negative before it." example:"-3"`
	Closes        string `json:"closes,omitempty" enum:"kets,fyniq_invitation" doc:"The flow that closes the task by itself: the KETs letter being issued, or the person being invited to Fyniq. Such a task is not marked done by hand; the people team skips it when it does not apply." example:"kets"`
}

// PlanTemplate is a template of onboarding tasks.
type PlanTemplate struct {
	ID       string             `json:"id" format:"uuid" doc:"The template." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a01"`
	Name     string             `json:"name" doc:"What it is for." example:"Office joiner"`
	Archived bool               `json:"archived" doc:"Whether it was archived: plans already made from it are unchanged." example:"false"`
	Tasks    []PlanTemplateTask `json:"tasks" nullable:"false" doc:"Its tasks in order."`
}

// PlanTask is a task of a person's plan.
type PlanTask struct {
	ID        string `json:"id" format:"uuid" doc:"The task." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a11"`
	PlanID    string `json:"plan_id" format:"uuid" doc:"Its plan." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a21"`
	PersonID  string `json:"person_id" format:"uuid" doc:"The joiner." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person    string `json:"person" doc:"The joiner's legal name." example:"Ben Lim"`
	Title     string `json:"title" doc:"What is to be done." example:"Issue the key employment terms (KETs)"`
	Detail    string `json:"detail,omitempty" doc:"How, or what good looks like." example:"Within 14 days of the start date."`
	Assignee  string `json:"assignee" enum:"hr,manager,it,joiner" doc:"Who it is for." example:"hr"`
	DueOn     string `json:"due_on" format:"date" doc:"The day it is due." example:"2026-12-15"`
	State     string `json:"state" enum:"open,done,skipped" doc:"Where it is." example:"open"`
	Overdue   bool   `json:"overdue" doc:"An open task past its due date." example:"false"`
	DecidedBy string `json:"decided_by,omitempty" doc:"Who marked it done or skipped." example:"Sarah Koh"`
	DecidedAt string `json:"decided_at,omitempty" format:"date-time" doc:"When." example:"2026-12-02T10:00:00Z"`
	Note      string `json:"note,omitempty" doc:"The note left when it was decided." example:"Collected on the first day."`
	Closes    string `json:"closes,omitempty" enum:"kets,fyniq_invitation" doc:"The flow that closes this task by itself: it is not marked done by hand, and it is skipped when it does not apply." example:"kets"`
}

// Plan is a person's onboarding plan.
type Plan struct {
	ID        string     `json:"id" format:"uuid" doc:"The plan." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a21"`
	PersonID  string     `json:"person_id" format:"uuid" doc:"The joiner." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person    string     `json:"person" doc:"The joiner's legal name." example:"Ben Lim"`
	Template  string     `json:"template" doc:"The name of the template it was made from." example:"Office joiner"`
	StartDate string     `json:"start_date" format:"date" doc:"The start date its tasks are due from." example:"2026-12-01"`
	Done      int        `json:"done" doc:"Tasks done or skipped." example:"3"`
	Total     int        `json:"total" doc:"All its tasks." example:"8"`
	Tasks     []PlanTask `json:"tasks" nullable:"false" doc:"Its tasks, soonest due first."`
}

// PlanTaskInput is a task added to a plan after it was made.
type PlanTaskInput struct {
	Title    string `json:"title" minLength:"1" maxLength:"200" doc:"What is to be done." example:"Order a security pass"`
	Detail   string `json:"detail,omitempty" maxLength:"1000" doc:"How, or what good looks like." example:"The building office needs two days."`
	Assignee string `json:"assignee" enum:"hr,manager,it,joiner" doc:"Who it is for." example:"hr"`
	DueOn    string `json:"due_on" format:"date" doc:"The day it is due." example:"2026-12-03"`
}

// Plans is onboarding plans behind the API.
type Plans interface {
	Templates(ctx context.Context, s Session) ([]PlanTemplate, error)
	MakeTemplate(ctx context.Context, s Session, name string, tasks []PlanTemplateTask) (PlanTemplate, error)
	ArchiveTemplate(ctx context.Context, s Session, id uuid.UUID) error
	Start(ctx context.Context, s Session, person uuid.UUID, template *uuid.UUID) (Plan, error)
	List(ctx context.Context, s Session) ([]Plan, error)
	AddTask(ctx context.Context, s Session, plan uuid.UUID, in PlanTaskInput) (PlanTask, error)
	Mine(ctx context.Context, s Session, held []authz.Role) ([]PlanTask, error)
	Decide(ctx context.Context, s Session, held []authz.Role, task uuid.UUID, state, note string) error
}

type planTemplatesOutput struct {
	Body struct {
		Templates []PlanTemplate `json:"templates" nullable:"false" doc:"The templates, the live ones first."`
	}
}
type planTemplateInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Name  string             `json:"name" minLength:"1" maxLength:"100" doc:"What it is for." example:"Office joiner"`
		Tasks []PlanTemplateTask `json:"tasks,omitempty" maxItems:"60" doc:"Its tasks. Leave empty to start from the standard template, which you then own."`
	}
}
type planTemplateOutput struct{ Body PlanTemplate }
type planTemplateIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The template." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a01"`
}
type planStartInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person who joins." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		TemplateID string `json:"template_id,omitempty" format:"uuid" doc:"The template to use; empty uses the standard template." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a01"`
	}
}
type planOutput struct{ Body Plan }
type plansOutput struct {
	Body struct {
		Plans []Plan `json:"plans" nullable:"false" doc:"The plans, the ones with the most left first."`
	}
}
type planIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The plan." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a21"`
	Body   PlanTaskInput
}
type planTaskOutput struct{ Body PlanTask }
type planTasksOutput struct {
	Body struct {
		Tasks []PlanTask `json:"tasks" nullable:"false" doc:"The tasks the caller can act on, soonest due first, with those decided in the last 14 days."`
	}
}
type planDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The task." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d2a11"`
	Body   struct {
		State string `json:"state" enum:"done,skipped" doc:"Done; or skipped because it does not apply (the HR team only)." example:"done"`
		Note  string `json:"note,omitempty" maxLength:"300" doc:"A note." example:"Collected on the first day."`
	}
}

func planError(err error) error {
	var r *onboarding.Refusal
	if errors.As(err, &r) {
		return Err(r.Code, r.Message)
	}
	return huma.Error500InternalServerError("The onboarding plan could not be handled.")
}

func init() {
	IdempotencyExempt["POST /v1/onboarding/templates"] = "a second template with the same name is refused (NM_ONBOARDING_CONFLICT)"
	IdempotencyExempt["POST /v1/onboarding/templates/{id}/archive"] = "a template is archived once; a second is refused (NM_ONBOARDING_CONFLICT)"
	IdempotencyExempt["POST /v1/people/{id}/onboarding-plan"] = "a person has one plan: a second is refused (NM_ONBOARDING_CONFLICT)"
	IdempotencyExempt["POST /v1/onboarding/plans/{id}/tasks"] = "a task added twice shows twice and the HR team can see it; its due date and words are fixed once added"
	IdempotencyExempt["POST /v1/onboarding/plan-tasks/{id}/decision"] = "a task is decided once: a second is refused (NM_ONBOARDING_CONFLICT)"
}

func (a *API) registerPlans() {
	// team is the HR officer, the HR administrator and the Owner, with the Onboarding feature on.
	team := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return s, err
		}
		if !p.Team {
			return s, Err(CodeRoleRequired, "Only the HR officer, the HR administrator and the Owner make onboarding plans.")
		}
		return s, a.onboardingOn(ctx, s)
	}
	available := func() error { return Err(CodeUnavailable, "Onboarding plans are not set up in this deployment.") }
	huma.Register(a.API, huma.Operation{
		OperationID: "listOnboardingTemplates", Method: http.MethodGet, Path: "/v1/onboarding/templates", Tags: []string{"Onboarding"}, Summary: "Onboarding templates",
		Description: "PRD-002 NM-ONB-01. The company's templates of onboarding tasks, the live ones first. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*planTemplatesOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		list, err := a.d.Plans.Templates(ctx, s)
		if err != nil {
			return nil, planError(err)
		}
		out := &planTemplatesOutput{}
		out.Body.Templates = append([]PlanTemplate{}, list...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "makeOnboardingTemplate", Method: http.MethodPost, Path: "/v1/onboarding/templates", Tags: []string{"Onboarding"}, DefaultStatus: http.StatusCreated, Summary: "Make an onboarding template",
		Description: "Makes a template of tasks, each for HR, the manager, IT or the joiner and due a number of days from the start date. With no tasks it copies the standard template (offer documents, equipment, accounts, pass collection, introductions, bank and CPF details, the key employment terms within 14 days, a first check-in). A template is never edited: make a new one and archive the old. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 409, 422, 503},
	}, func(ctx context.Context, in *planTemplateInput) (*planTemplateOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		t, err := a.d.Plans.MakeTemplate(ctx, s, in.Body.Name, in.Body.Tasks)
		if err != nil {
			return nil, planError(err)
		}
		return &planTemplateOutput{Body: t}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "archiveOnboardingTemplate", Method: http.MethodPost, Path: "/v1/onboarding/templates/{id}/archive", Tags: []string{"Onboarding"}, DefaultStatus: http.StatusNoContent, Summary: "Archive an onboarding template",
		Description: "Archives a template so no new plan is made from it; plans already made are unchanged. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *planTemplateIDInput) (*struct{}, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such template.")
		}
		if err := a.d.Plans.ArchiveTemplate(ctx, s, id); err != nil {
			return nil, planError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "startOnboardingPlan", Method: http.MethodPost, Path: "/v1/people/{id}/onboarding-plan", Tags: []string{"Onboarding"}, DefaultStatus: http.StatusCreated, Summary: "Start a person's onboarding plan",
		Description: "Makes the person's plan from a template (the standard one when none is named): each task is due from the person's start date. A person has one plan. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *planStartInput) (*planOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		person, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		var tpl *uuid.UUID
		if in.Body.TemplateID != "" {
			t, err := uuid.Parse(in.Body.TemplateID)
			if err != nil {
				return nil, Err(CodeNotFound, "There is no such template.")
			}
			tpl = &t
		}
		p, err := a.d.Plans.Start(ctx, s, person, tpl)
		if err != nil {
			return nil, planError(err)
		}
		return &planOutput{Body: p}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listOnboardingPlans", Method: http.MethodGet, Path: "/v1/onboarding/plans", Tags: []string{"Onboarding"}, Summary: "Onboarding plans",
		Description: "Every person's plan with its tasks, the ones with the most left first. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*plansOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		list, err := a.d.Plans.List(ctx, s)
		if err != nil {
			return nil, planError(err)
		}
		out := &plansOutput{}
		out.Body.Plans = append([]Plan{}, list...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "addOnboardingTask", Method: http.MethodPost, Path: "/v1/onboarding/plans/{id}/tasks", Tags: []string{"Onboarding"}, DefaultStatus: http.StatusCreated, Summary: "Add a task to a plan",
		Description: "Adds one task to a person's plan, for HR, the manager, IT or the joiner, due on a day. Its words and date never change once added. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *planIDInput) (*planTaskOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such plan.")
		}
		t, err := a.d.Plans.AddTask(ctx, s, id, in.Body)
		if err != nil {
			return nil, planError(err)
		}
		return &planTaskOutput{Body: t}, nil
	})
	// mine: any signed-in member reads the tasks that are theirs: the joiner's own, their reports' joiners' (as line manager), IT's and the HR team's.
	rolesOf := func(ctx context.Context, s Session) ([]authz.Role, error) {
		held, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
		if err != nil {
			return nil, huma.Error500InternalServerError("The roles could not be read.")
		}
		return held, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listMyOnboardingTasks", Method: http.MethodGet, Path: "/v1/me/onboarding", Tags: []string{"Onboarding"}, Summary: "My onboarding tasks",
		Description: "The onboarding tasks the caller can act on: their own as a joiner, those of the people who report to them, IT's when they are an Administrator, HR administrator or the Owner, and HR's when they are in the people team; soonest due first, with those decided in the last 14 days. Any signed-in member. It changes nothing.", Errors: []int{401, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*planTasksOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		if err := a.onboardingOn(ctx, s); err != nil {
			return nil, err
		}
		held, err := rolesOf(ctx, s)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Plans.Mine(ctx, s, held)
		if err != nil {
			return nil, planError(err)
		}
		out := &planTasksOutput{}
		out.Body.Tasks = append([]PlanTask{}, list...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideOnboardingTask", Method: http.MethodPost, Path: "/v1/onboarding/plan-tasks/{id}/decision", Tags: []string{"Onboarding"}, DefaultStatus: http.StatusNoContent, Summary: "Mark an onboarding task done or skipped",
		Description: "Marks a task done, once. A joiner's task is done by the joiner, a manager's by the joiner's line manager, IT's by an Administrator, HR administrator or the Owner, and any by the people team, who alone may skip one that does not apply. Anyone else is told there is no such task. It is audited.", Errors: []int{401, 404, 409, 422, 503},
	}, func(ctx context.Context, in *planDecisionInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Plans == nil {
			return nil, available()
		}
		if err := a.onboardingOn(ctx, s); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such task for you.")
		}
		held, err := rolesOf(ctx, s)
		if err != nil {
			return nil, err
		}
		if err := a.d.Plans.Decide(ctx, s, held, id, in.Body.State, in.Body.Note); err != nil {
			return nil, planError(err)
		}
		return nil, nil
	})
}

// onboardingOn refuses a request while the Onboarding feature is off for the organisation: its records are kept (NM-FEAT-01).
func (a *API) onboardingOn(ctx context.Context, s Session) error {
	sw, err := a.d.Features.List(ctx, s)
	if err != nil {
		return huma.Error500InternalServerError("The feature switches could not be read.")
	}
	if sw["onboarding"].State != features.On {
		return Err(CodeFeatureOff, "Onboarding is switched off for this organisation: an Owner or the HR administrator switches it on under Settings, Features.")
	}
	return nil
}
