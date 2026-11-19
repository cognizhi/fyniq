package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/dimension"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/project"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ projects (FR-PRJ-01; p1-projects task 1.3)
//
// A project is a value of the Project dimension with its attributes: a customer, a billing currency, dates, a status, a
// billing method, a budget in hours and in money, and the people who log time on it and the managers who approve that
// time. Its ID is the value's ID, so every line already tagged names it. Everything here is switched off with the
// organisation's feature switch (FR-ORG-06, design D7), whatever the caller's rights.

// CodeProjectsOff is answered by every operation of Projects and time while the organisation has them switched off.
const CodeProjectsOff = project.CodeOff

// ProjectWarning is something about a project that deserves a look, in words.
type ProjectWarning string

// Schema implements huma.SchemaProvider.
func (ProjectWarning) Schema(huma.Registry) *huma.Schema {
	most := 400
	return &huma.Schema{Type: "string", MaxLength: &most, Description: "What deserves a look, in plain words, such as a manager who holds no role that approves time.",
		Examples: []any{"Sam Tan is named a manager but holds no role that approves time (the Approver or the Controller): they cannot approve until they are given one"}}
}

// ProjectPerson is a person on a project.
type ProjectPerson struct {
	UserID      string `json:"user_id" format:"uuid" doc:"The member's user ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	DisplayName string `json:"display_name" doc:"Their name." example:"Priya Nair"`
	Manager     bool   `json:"manager" doc:"true for a manager: a member who approves the project's time (FR-PRJ-07). Naming a manager gives no right of its own: the person must hold the Approver or Controller role." example:"false"`
	CanApprove  bool   `json:"can_approve" doc:"true when their roles hold the right to approve time (the Approver or the Controller); a manager without it cannot approve until they are given the role." example:"true"`
}

// Project is a project as the organisation keeps it.
type Project struct {
	ID            string           `json:"id" format:"uuid" doc:"The project's ID: the ID of its value in the Project dimension, which lines carry as a dimension value (FR-DIM-02)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	DimensionID   string           `json:"dimension_id" format:"uuid" doc:"The Project dimension's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Code          string           `json:"code" doc:"Its code, unique among the projects and never changed." example:"ACME-01"`
	Name          string           `json:"name" doc:"Its name." example:"Acme rollout"`
	Configured    bool             `json:"configured" doc:"false for a value of the Project dimension that has no project record yet (it was added through the dimensions): send replaceProject with If-Match \"0\" to set it up." example:"true"`
	CustomerID    string           `json:"customer_id,omitempty" format:"uuid" doc:"The customer it is for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	CustomerName  string           `json:"customer_name,omitempty" doc:"The customer's name." example:"Acme Pte Ltd"`
	Currency      string           `json:"currency,omitempty" doc:"The currency it is billed in (FR-PRJ-06); it never changes." example:"SGD"`
	Status        string           `json:"status" enum:"planned,active,on_hold,closed" doc:"Where the project is. A closed project takes no new time and its dimension value goes out of use." example:"active"`
	BillingMethod string           `json:"billing_method,omitempty" enum:"time_and_materials,fixed_fee,retainer,internal" doc:"How it is billed (FR-PRJ-10): time and materials, fixed fee by milestone, retainer drawn down, or internal (never billed)." example:"time_and_materials"`
	StartsOn      string           `json:"starts_on,omitempty" format:"date" doc:"The first day of the work." example:"2026-11-01"`
	EndsOn        string           `json:"ends_on,omitempty" format:"date" doc:"The last day of the work." example:"2027-02-28"`
	BudgetMinutes *int             `json:"budget_minutes,omitempty" doc:"The hours budget in whole minutes (400 hours is 24000)." example:"24000"`
	BudgetAmount  string           `json:"budget_amount,omitempty" doc:"The money budget in the project's currency, a decimal string." example:"60000.00"`
	People        []ProjectPerson  `json:"people" nullable:"false" doc:"The members, managers first; a member logs time, a manager also approves it."`
	Warnings      []ProjectWarning `json:"warnings" nullable:"false" doc:"What deserves a look, such as a manager who holds no role that approves time."`
	Version       int              `json:"version" doc:"The project record's version, for If-Match; 0 when it is not configured." example:"1"`
}

// ProjectPersonDraft names a person of a project as it is set.
type ProjectPersonDraft struct {
	UserID  string `json:"user_id" format:"uuid" doc:"An active member's user ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	Manager bool   `json:"manager,omitempty" doc:"true to make them a manager of the project." example:"false"`
}

// ProjectDraft is a project as added.
type ProjectDraft struct {
	Code          string               `json:"code" maxLength:"30" doc:"Its code: a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens, unique among the projects whatever the case." example:"ACME-01"`
	Name          string               `json:"name" maxLength:"200" doc:"Its name, unique among the projects." example:"Acme rollout"`
	CustomerID    string               `json:"customer_id,omitempty" format:"uuid" doc:"A customer contact (none for an internal project)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	Currency      string               `json:"currency" minLength:"3" maxLength:"3" doc:"The ISO 4217 code it is billed in; it never changes." example:"SGD"`
	Status        string               `json:"status,omitempty" enum:"planned,active,on_hold,closed" doc:"Left out, active." example:"active"`
	BillingMethod string               `json:"billing_method" enum:"time_and_materials,fixed_fee,retainer,internal" doc:"How it is billed." example:"time_and_materials"`
	StartsOn      string               `json:"starts_on,omitempty" format:"date" doc:"The first day of the work." example:"2026-11-01"`
	EndsOn        string               `json:"ends_on,omitempty" format:"date" doc:"The last day of the work, on or after the first." example:"2027-02-28"`
	BudgetMinutes *int                 `json:"budget_minutes,omitempty" minimum:"0" maximum:"100000000" doc:"The hours budget in whole minutes." example:"24000"`
	BudgetAmount  string               `json:"budget_amount,omitempty" doc:"The money budget in the project's currency, a plain decimal with at most its minor units." example:"60000.00"`
	People        []ProjectPersonDraft `json:"people,omitempty" maxItems:"200" doc:"The project's members and managers; a person not named is not on the project."`
}

// ProjectChange is a project as changed: everything but its code and currency.
type ProjectChange struct {
	Name          string               `json:"name" maxLength:"200" doc:"Its name, unique among the projects." example:"Acme rollout"`
	CustomerID    string               `json:"customer_id,omitempty" format:"uuid" doc:"A customer contact (none for an internal project)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	Currency      string               `json:"currency,omitempty" minLength:"3" maxLength:"3" doc:"Required when the project has no record yet; afterwards it must be the one it has." example:"SGD"`
	Status        string               `json:"status" enum:"planned,active,on_hold,closed" doc:"Closing a project puts its dimension value out of use; reopening puts it back." example:"active"`
	BillingMethod string               `json:"billing_method" enum:"time_and_materials,fixed_fee,retainer,internal" doc:"How it is billed." example:"time_and_materials"`
	StartsOn      string               `json:"starts_on,omitempty" format:"date" doc:"The first day of the work." example:"2026-11-01"`
	EndsOn        string               `json:"ends_on,omitempty" format:"date" doc:"The last day of the work." example:"2027-02-28"`
	BudgetMinutes *int                 `json:"budget_minutes,omitempty" minimum:"0" maximum:"100000000" doc:"The hours budget in whole minutes." example:"24000"`
	BudgetAmount  string               `json:"budget_amount,omitempty" doc:"The money budget in the project's currency." example:"60000.00"`
	People        []ProjectPersonDraft `json:"people,omitempty" maxItems:"200" doc:"The members and managers; this replaces the project's people."`
}

// ProjectPage is a page of the projects.
type ProjectPage struct {
	Items      []Project `json:"items" nullable:"false"`
	NextCursor string    `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// ProjectTask is what time on a project is logged against.
type ProjectTask struct {
	ID        string `json:"id" format:"uuid" doc:"The task's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e51"`
	ProjectID string `json:"project_id" format:"uuid" doc:"The project it belongs to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	Name      string `json:"name" doc:"Its name, unique among the project's live tasks." example:"Data migration"`
	Billable  bool   `json:"billable" doc:"Whether time on it is billable unless an entry says otherwise." example:"true"`
	Active    bool   `json:"active" doc:"false once put out of use: kept, as time names it." example:"true"`
	Version   int    `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// ProjectTaskDraft is a task as added.
type ProjectTaskDraft struct {
	ProjectID string `json:"project_id" format:"uuid" doc:"The project it is added to: a project with a record, not closed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	Name      string `json:"name" maxLength:"100" doc:"Its name, unique among the project's live tasks." example:"Data migration"`
	Billable  *bool  `json:"billable,omitempty" doc:"Left out, billable." example:"true"`
}

// ProjectTaskChange is a task as changed.
type ProjectTaskChange struct {
	Name     string `json:"name" maxLength:"100" doc:"Its name, unique among the project's live tasks." example:"Data migration"`
	Billable *bool  `json:"billable,omitempty" doc:"Left out, billable." example:"true"`
	Active   *bool  `json:"active,omitempty" doc:"false puts it out of use; left out, in use." example:"true"`
}

// ProjectTaskPage is a page of a project's tasks.
type ProjectTaskPage struct {
	Items      []ProjectTask `json:"items" nullable:"false"`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

type projectListInput struct {
	PageQuery
	Status string `query:"status" enum:"planned,active,on_hold,closed" doc:"Only the projects with this status; left out, every project." example:"active"`
}

type projectListOutput struct{ Body ProjectPage }

// ProjectPeoplePage is a page of the members a project can name.
type ProjectPeoplePage struct {
	Items      []ProjectPerson `json:"items" nullable:"false"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

type projectPeopleInput struct{ PageQuery }

type projectPeopleOutput struct{ Body ProjectPeoplePage }

type ProjectPath struct {
	ProjectID string `path:"project_id" format:"uuid" doc:"The project's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
}

type createProjectInput struct {
	KeyHeader
	PreferHeader
	Body ProjectDraft
}

type replaceProjectInput struct {
	ProjectPath
	IfMatchHeader
	PreferHeader
	Body ProjectChange
}

type getProjectInput struct{ ProjectPath }

type projectOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The project record's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Project
}

type projectTaskListInput struct {
	ProjectPath
	PageQuery
}

type projectTaskListOutput struct{ Body ProjectTaskPage }

type createProjectTaskInput struct {
	KeyHeader
	PreferHeader
	Body ProjectTaskDraft
}

type replaceProjectTaskInput struct {
	TaskID string `path:"task_id" format:"uuid" doc:"The task's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e51"`
	IfMatchHeader
	PreferHeader
	Body ProjectTaskChange
}

type projectTaskOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The task's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ProjectTask
}

// requireProjects refuses an operation while the organisation has Projects and time switched off (FR-ORG-06,
// FR-UX-40d): the server enforces what the web app hides.
func requireProjects(ctx context.Context, a *API, u storage.UnitOfWork) error {
	f, err := featuresOn(ctx, a, u)
	if err != nil {
		return err
	}
	if !f.Projects {
		return problem(CodeProjectsOff, "Projects and time are switched off for this organisation.", "An Owner or Administrator turns them on under Settings, Features")
	}
	return nil
}

// seesAllProjects says the caller reads every project: any right on "Timesheets and project time" without a qualifier. The
// Employee's "C (own)" and the Approver's "A (project manager)" read the projects they are on only (design D4).
func seesAllProjects(p authn.Principal) bool {
	return authz.Decide(authz.Policy{Operation: "view", Capability: authz.Timesheets, Need: authz.View, Unqualified: true}, p.Roles, p.AI()).Allowed
}

// canApproveTime says whether roles hold the right to approve time: A on "Timesheets and project time", with or
// without the Approver's project qualifier.
func canApproveTime(roles []storage.RoleAssignment) bool {
	rs := make([]authz.Role, 0, len(roles))
	for _, r := range roles {
		rs = append(rs, authz.Role(r.Role))
	}
	return authz.Decide(authz.Policy{Operation: "approve", Capability: authz.Timesheets, Need: authz.Approve}, rs, false).Allowed
}

func projectProblems(p []project.Problem) error {
	if len(p) == 0 {
		return nil
	}
	bad := make([]FieldProblem, 0, len(p))
	for _, x := range p {
		f := x.Field
		if !strings.HasPrefix(f, "body.") {
			f = "body." + f
		}
		bad = append(bad, FieldProblem{Code: x.Code, Field: f, Message: x.Message, Fix: x.Fix})
	}
	return invalid(bad)
}

// projectView is what building a project's body needs, read once per request.
type projectView struct {
	members   map[uuid.UUID]storage.Member
	customers map[uuid.UUID]string
}

func (a *API) projectView(ctx context.Context, u storage.UnitOfWork) (*projectView, error) {
	ms, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return nil, err
	}
	v := &projectView{members: make(map[uuid.UUID]storage.Member, len(ms)), customers: map[uuid.UUID]string{}}
	for _, m := range ms {
		v.members[m.UserID] = m
	}
	return v, nil
}

func (v *projectView) customerName(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (string, error) {
	if n, ok := v.customers[id]; ok {
		return n, nil
	}
	c, err := u.Contacts().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	v.customers[id] = c.LegalName
	return c.LegalName, nil
}

func day(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

func (a *API) projectOf(ctx context.Context, u storage.UnitOfWork, v *projectView, p storage.Project) (Project, error) {
	out := Project{ID: p.ID.String(), DimensionID: p.DimensionID.String(), Code: p.Code, Name: p.Name, Configured: p.Configured, Currency: p.Currency,
		Status: p.Status, BillingMethod: p.BillingMethod, StartsOn: day(p.StartsOn), EndsOn: day(p.EndsOn), BudgetMinutes: p.BudgetMinutes,
		People: []ProjectPerson{}, Warnings: []ProjectWarning{}, Version: p.Version}
	if !p.Configured {
		out.BillingMethod = ""
		out.Warnings = append(out.Warnings, "This value of the Project dimension has no project record yet: set it up to log time on it")
	}
	if p.BudgetAmount != nil {
		out.BudgetAmount = string(*p.BudgetAmount)
		if cur, err := money.Lookup(p.Currency); err == nil {
			if m, err := money.ParseStored(string(*p.BudgetAmount), cur); err == nil {
				out.BudgetAmount = m.String()
			}
		}
	}
	if p.CustomerID != nil {
		out.CustomerID = p.CustomerID.String()
		name, err := v.customerName(ctx, u, *p.CustomerID)
		if err != nil {
			return out, err
		}
		out.CustomerName = name
	}
	if !p.Configured {
		return out, nil
	}
	ms, err := u.Projects().Members(ctx, p.ID)
	if err != nil {
		return out, err
	}
	for _, m := range ms {
		if !m.Active {
			continue
		}
		who, ok := v.members[m.UserID]
		if !ok {
			continue // no longer an active member of the organisation
		}
		can := canApproveTime(who.Roles)
		out.People = append(out.People, ProjectPerson{UserID: m.UserID.String(), DisplayName: who.DisplayName, Manager: m.Manager, CanApprove: can})
		if m.Manager && !can {
			out.Warnings = append(out.Warnings, ProjectWarning(who.DisplayName+" is named a manager but holds no role that approves time (the Approver or the Controller): they cannot approve until they are given one"))
		}
	}
	return out, nil
}

// projectKnow is what the check of a project needs of the organisation.
func projectKnow(ctx context.Context, u storage.UnitOfWork, v *projectView, customer string) (project.Know, error) {
	k := project.Know{Customers: map[uuid.UUID]bool{}, Members: make(map[uuid.UUID]bool, len(v.members))}
	for id := range v.members {
		k.Members[id] = true
	}
	if id, err := uuid.Parse(customer); err == nil {
		c, err := u.Contacts().Get(ctx, id)
		switch {
		case err == nil:
			k.Customers[id] = c.Customer && c.Active
		case !errors.Is(err, ports.ErrNotFound):
			return k, err
		}
	}
	return k, nil
}

func peopleOf(in []ProjectPersonDraft) []project.Person {
	out := make([]project.Person, 0, len(in))
	for _, p := range in {
		out = append(out, project.Person{UserID: p.UserID, Manager: p.Manager})
	}
	return out
}

// projectsOf is the caller's own projects when they read only those, else every project.
func (a *API) visibleProjects(ctx context.Context, u storage.UnitOfWork, p authn.Principal) (func(uuid.UUID) bool, error) {
	if seesAllProjects(p) {
		return func(uuid.UUID) bool { return true }, nil
	}
	ids, err := u.Projects().OfUser(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	mine := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		mine[id] = true
	}
	return func(id uuid.UUID) bool { return mine[id] }, nil
}

// ProjectCodes are the codes a write of a project may answer.
var ProjectCodes = []string{CodeProjectsOff, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, project.CodeStatus, project.CodeBilling, project.CodeDates,
	project.CodeBudget, project.CodeCustomer, project.CodeCurrency, project.CodePeople, project.CodeTask, dimension.CodeKind, dimension.CodeName,
	dimension.CodeCode, dimension.CodeDimension, storage.CodeDuplicate, storage.CodeProject, storage.CodeDimensionFixed, storage.CodeRetry, storage.CodeLockTimeout}

func registerProjects(a *API) {
	register(a, authz.ListProjects, huma.Operation{
		OperationID: "listProjects", Method: http.MethodGet, Path: "/v1/projects", Tags: []string{"Projects"},
		Summary: "List the projects",
		Description: "The values of the Project dimension with their project records (FR-PRJ-01), by code, a page at a time. A person who holds " +
			"a right on \"Timesheets and project time\" without a qualifier (the Owner, the Controller, the Accountant, the AR specialist, the " +
			"Auditor, the Tax agent) sees every project; an Employee and an Approver see the projects they are a member or manager of, " +
			"whatever they ask for. Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeCursor, CodeProjectsOff}, func(ctx context.Context, in *projectListInput) (*projectListOutput, error) {
		p, _ := authn.From(ctx)
		out := projectListOutput{Body: ProjectPage{Items: []Project{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			visible, err := a.visibleProjects(ctx, u, p)
			if err != nil {
				return err
			}
			all, err := u.Projects().List(ctx)
			if err != nil {
				return err
			}
			var shown []storage.Project
			for _, x := range all {
				if visible(x.ID) && (in.Status == "" || (x.Configured && x.Status == in.Status)) {
					shown = append(shown, x)
				}
			}
			from, to, next, err := pageAt(len(shown), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			v, err := a.projectView(ctx, u)
			if err != nil {
				return err
			}
			for _, x := range shown[from:to] {
				body, err := a.projectOf(ctx, u, v, x)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, body)
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.ListProjectPeople, huma.Operation{
		OperationID: "listProjectPeople", Method: http.MethodGet, Path: "/v1/project-people", Tags: []string{"Projects"},
		Summary: "List the members a project can name",
		Description: "The organisation's active members, by name, with whether their roles hold the right to approve time (the Approver or the " +
			"Controller), for the people who keep projects, who may not read the members list (Appendix D, \"Users, roles, team mode\"). " +
			"Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeCursor, CodeProjectsOff}, func(ctx context.Context, in *projectPeopleInput) (*projectPeopleOutput, error) {
		p, _ := authn.From(ctx)
		out := projectPeopleOutput{Body: ProjectPeoplePage{Items: []ProjectPerson{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			ms, err := u.Members().List(ctx, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			from, to, next, err := pageAt(len(ms), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, m := range ms[from:to] {
				out.Body.Items = append(out.Body.Items, ProjectPerson{UserID: m.UserID.String(), DisplayName: m.DisplayName, CanApprove: canApproveTime(m.Roles)})
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.GetProject, huma.Operation{
		OperationID: "getProject", Method: http.MethodGet, Path: "/v1/projects/{project_id}", Tags: []string{"Projects"},
		Summary: "Get a project",
		Description: "A project with its people and what deserves a look, and its ETag (FR-PRJ-01). A person who reads only the projects they are on " +
			"gets 404 for another one. Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeProjectsOff}, func(ctx context.Context, in *getProjectInput) (*projectOutput, error) {
		id, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := projectOutput{Status: http.StatusOK}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			visible, err := a.visibleProjects(ctx, u, p)
			if err != nil {
				return err
			}
			if !visible(id) {
				return ports.ErrNotFound
			}
			x, err := u.Projects().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.projectView(ctx, u)
			if err != nil {
				return err
			}
			out.Body, err = a.projectOf(ctx, u, v, x)
			out.ETag = etag(out.Body.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.CreateProject, huma.Operation{
		OperationID: "createProject", Method: http.MethodPost, Path: "/v1/projects", Tags: []string{"Projects"},
		DefaultStatus: http.StatusCreated, Summary: "Add a project",
		Description: "Adds a project (FR-PRJ-01): a value of the Project dimension, which is created too (and the dimension itself when the " +
			"organisation has none), and its record. Every problem comes back together. A project that is billed needs a manager, and a manager " +
			"holds the Approver or Controller role to approve time: the answer warns when they do not. Side effects: the dimension value, the " +
			"project record and its people, with their audit events. Refused with PRJ-09 while Projects and time are switched off. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, ProjectCodes...),
		func(ctx context.Context, in *createProjectInput) (*projectOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Project, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return Project{}, err
				}
				v, err := a.projectView(ctx, u)
				if err != nil {
					return Project{}, err
				}
				d := project.Draft{CustomerID: in.Body.CustomerID, Currency: in.Body.Currency, Status: cmpOr(in.Body.Status, storage.ProjectActive),
					BillingMethod: in.Body.BillingMethod, StartsOn: in.Body.StartsOn, EndsOn: in.Body.EndsOn, BudgetMinutes: in.Body.BudgetMinutes,
					BudgetAmount: in.Body.BudgetAmount, People: peopleOf(in.Body.People)}
				know, err := projectKnow(ctx, u, v, d.CustomerID)
				if err != nil {
					return Project{}, err
				}
				checked, probs := project.Check(d, nil, know)
				dims, err := u.Dimensions().List(ctx)
				if err != nil {
					return Project{}, err
				}
				dim := projectDimension(dims)
				var others []storage.DimensionValue
				n := storage.NewDimensionValue{Code: strings.TrimSpace(in.Body.Code), Name: strings.TrimSpace(in.Body.Name)}
				if dim != nil {
					n.DimensionID = dim.ID
					if others, err = u.Dimensions().Values(ctx, &dim.ID); err != nil {
						return Project{}, err
					}
				}
				for _, x := range dimension.NewValue(n, orDefaultDimension(dim), others) {
					probs = append(probs, project.Problem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
				}
				if err := projectProblems(probs); err != nil {
					return Project{}, err
				}
				if dim == nil { // the organisation's first project: its Project dimension
					nd, dprobs := dimension.New(storage.DimensionProject, "Project", dims)
					if err := dimensionProblems(dprobs); err != nil {
						return Project{}, err
					}
					made, err := u.Dimensions().Create(ctx, nd)
					if err != nil {
						return Project{}, err
					}
					n.DimensionID = made.ID
				}
				val, err := u.Dimensions().CreateValue(ctx, n)
				if err != nil {
					return Project{}, err
				}
				created, err := u.Projects().Create(ctx, storage.NewProject{ID: val.ID, CustomerID: checked.CustomerID, Currency: checked.Currency,
					Status: checked.Status, BillingMethod: checked.BillingMethod, StartsOn: checked.StartsOn, EndsOn: checked.EndsOn,
					BudgetMinutes: checked.BudgetMinutes, BudgetAmount: checked.BudgetAmount})
				if err != nil {
					return Project{}, err
				}
				if err := u.Projects().SetMembers(ctx, val.ID, checked.People); err != nil {
					return Project{}, err
				}
				if created.Status == storage.ProjectClosed {
					if _, err := u.Dimensions().ChangeValue(ctx, val.ID, val.Version, storage.DimensionChange{Name: val.Name, Active: false}); err != nil {
						return Project{}, err
					}
				}
				return a.projectNow(ctx, u, v, val.ID)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &projectOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &projectOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ReplaceProject, huma.Operation{
		OperationID: "replaceProject", Method: http.MethodPut, Path: "/v1/projects/{project_id}", Tags: []string{"Projects"},
		Summary: "Change a project, or set up a value of the Project dimension as one",
		Description: "Changes a project's name, customer, status, billing method, dates, budgets and, when people is sent, its members and " +
			"managers (it replaces them; left out, they stay). Its code and currency never change. Closing a project puts its dimension value out " +
			"of use, reopening puts it back. A value of the dimension that has no record yet (configured false) is set up with If-Match \"0\" and " +
			"its currency. Needs If-Match with the ETag. Side effects: the next version, the dimension value's name or use when they change, " +
			"and the audit events. Refused with PRJ-09 while Projects and time are switched off. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, ProjectCodes...),
		func(ctx context.Context, in *replaceProjectInput) (*projectOutput, error) {
			id, err := parseID("project_id", in.ProjectID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Project, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return Project{}, err
				}
				before, err := u.Projects().Get(ctx, id)
				if err != nil {
					return Project{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return Project{}, err
				}
				v, err := a.projectView(ctx, u)
				if err != nil {
					return Project{}, err
				}
				people := in.Body.People
				if people == nil && before.Configured { // left out: they stay
					ms, err := u.Projects().Members(ctx, id)
					if err != nil {
						return Project{}, err
					}
					for _, m := range ms {
						if m.Active {
							people = append(people, ProjectPersonDraft{UserID: m.UserID.String(), Manager: m.Manager})
						}
					}
				}
				d := project.Draft{CustomerID: in.Body.CustomerID, Currency: in.Body.Currency, Status: in.Body.Status, BillingMethod: in.Body.BillingMethod,
					StartsOn: in.Body.StartsOn, EndsOn: in.Body.EndsOn, BudgetMinutes: in.Body.BudgetMinutes, BudgetAmount: in.Body.BudgetAmount,
					People: peopleOf(people)}
				know, err := projectKnow(ctx, u, v, d.CustomerID)
				if err != nil {
					return Project{}, err
				}
				checked, probs := project.Check(d, &before, know)
				name := strings.TrimSpace(in.Body.Name)
				active := checked.Status != storage.ProjectClosed
				others, err := u.Dimensions().Values(ctx, &before.DimensionID)
				if err != nil {
					return Project{}, err
				}
				self := storage.DimensionValue{ID: before.ID, DimensionID: before.DimensionID, Code: before.Code, Name: before.Name, Active: before.Active, Version: before.ValueVersion}
				for _, x := range dimension.ChangeValue(self, storage.DimensionChange{Name: name, Active: active}, others) {
					probs = append(probs, project.Problem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
				}
				if err := projectProblems(probs); err != nil {
					return Project{}, err
				}
				if before.Configured {
					if _, err := u.Projects().Change(ctx, id, before.Version, storage.ProjectChange{CustomerID: checked.CustomerID, Status: checked.Status,
						BillingMethod: checked.BillingMethod, StartsOn: checked.StartsOn, EndsOn: checked.EndsOn, BudgetMinutes: checked.BudgetMinutes,
						BudgetAmount: checked.BudgetAmount}); err != nil {
						return Project{}, err
					}
				} else if _, err := u.Projects().Create(ctx, storage.NewProject{ID: id, CustomerID: checked.CustomerID, Currency: checked.Currency,
					Status: checked.Status, BillingMethod: checked.BillingMethod, StartsOn: checked.StartsOn, EndsOn: checked.EndsOn,
					BudgetMinutes: checked.BudgetMinutes, BudgetAmount: checked.BudgetAmount}); err != nil {
					return Project{}, err
				}
				if err := u.Projects().SetMembers(ctx, id, checked.People); err != nil {
					return Project{}, err
				}
				if name != before.Name || active != before.Active {
					if _, err := u.Dimensions().ChangeValue(ctx, id, before.ValueVersion, storage.DimensionChange{Name: name, Active: active}); err != nil {
						return Project{}, err
					}
				}
				return a.projectNow(ctx, u, v, id)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &projectOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &projectOutput{Status: http.StatusOK, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})

	registerProjectTasks(a)
	registerTimesheets(a)
	registerTimesheetOps(a)
	registerProjectRates(a)
	registerProjectReports(a)
}

// projectNow reads a project again, as it stands in the unit of work.
func (a *API) projectNow(ctx context.Context, u storage.UnitOfWork, v *projectView, id uuid.UUID) (Project, error) {
	x, err := u.Projects().Get(ctx, id)
	if err != nil {
		return Project{}, err
	}
	return a.projectOf(ctx, u, v, x)
}

// projectDimension is the organisation's Project dimension, when it has one.
func projectDimension(dims []storage.Dimension) *storage.Dimension {
	for _, d := range dims {
		if d.Kind == storage.DimensionProject {
			return &d
		}
	}
	return nil
}

// orDefaultDimension stands in for the Project dimension of an organisation that has none yet, which the first project
// creates.
func orDefaultDimension(d *storage.Dimension) *storage.Dimension {
	if d != nil {
		return d
	}
	return &storage.Dimension{Name: "Project", Kind: storage.DimensionProject, Active: true}
}

func projectTaskOf(t storage.ProjectTask) ProjectTask {
	return ProjectTask{ID: t.ID.String(), ProjectID: t.ProjectID.String(), Name: t.Name, Billable: t.Billable, Active: t.Active, Version: t.Version}
}

func registerProjectTasks(a *API) {
	register(a, authz.ListProjectTasks, huma.Operation{
		OperationID: "listProjectTasks", Method: http.MethodGet, Path: "/v1/projects/{project_id}/tasks", Tags: []string{"Projects"},
		Summary: "List a project's tasks",
		Description: "What time on the project is logged against (FR-PRJ-05): by name, those out of use after the live ones, a page at a time. " +
			"A person who reads only the projects they are on gets 404 for another one. Refused with PRJ-09 while Projects and time are switched " +
			"off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor, CodeProjectsOff}, func(ctx context.Context, in *projectTaskListInput) (*projectTaskListOutput, error) {
		id, err := parseID("project_id", in.ProjectID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := projectTaskListOutput{Body: ProjectTaskPage{Items: []ProjectTask{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			visible, err := a.visibleProjects(ctx, u, p)
			if err != nil {
				return err
			}
			if !visible(id) {
				return ports.ErrNotFound
			}
			if _, err := u.Projects().Get(ctx, id); err != nil {
				return err
			}
			ts, err := u.Projects().Tasks(ctx, id)
			if err != nil {
				return err
			}
			from, to, next, err := pageAt(len(ts), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, t := range ts[from:to] {
				out.Body.Items = append(out.Body.Items, projectTaskOf(t))
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateProjectTask, huma.Operation{
		OperationID: "createProjectTask", Method: http.MethodPost, Path: "/v1/project-tasks", Tags: []string{"Projects"},
		DefaultStatus: http.StatusCreated, Summary: "Add a task to a project",
		Description: "Adds a task time can be logged against (FR-PRJ-05), named uniquely among the project's live tasks; billable unless said " +
			"otherwise. A closed project takes no new task. Side effects: the task and its audit event under the project. Refused with PRJ-09 " +
			"while Projects and time are switched off. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, ProjectCodes...),
		func(ctx context.Context, in *createProjectTaskInput) (*projectTaskOutput, error) {
			id, err := parseID("project_id", in.Body.ProjectID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ProjectTask, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return ProjectTask{}, err
				}
				x, err := u.Projects().Get(ctx, id)
				if err != nil {
					return ProjectTask{}, err
				}
				if !x.Configured {
					return ProjectTask{}, invalid([]FieldProblem{{Code: project.CodeTask, Field: "body.project_id", Message: "has no project record yet",
						Fix: "Set the project up first with replaceProject"}})
				}
				ts, err := u.Projects().Tasks(ctx, id)
				if err != nil {
					return ProjectTask{}, err
				}
				name, probs := project.CheckTask(in.Body.Name, ts, uuid.Nil)
				if err := projectProblems(probs); err != nil {
					return ProjectTask{}, err
				}
				t, err := u.Projects().CreateTask(ctx, id, name, in.Body.Billable == nil || *in.Body.Billable)
				return projectTaskOf(t), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &projectTaskOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &projectTaskOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ReplaceProjectTask, huma.Operation{
		OperationID: "replaceProjectTask", Method: http.MethodPut, Path: "/v1/project-tasks/{task_id}", Tags: []string{"Projects"},
		Summary: "Rename a task, change whether it is billable, or put it out of use",
		Description: "A task is never deleted, as time names it: it is put out of use instead. Needs If-Match with its ETag. Side effects: its next " +
			"version and its audit event under the project. Refused with PRJ-09 while Projects and time are switched off. Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, ProjectCodes...),
		func(ctx context.Context, in *replaceProjectTaskInput) (*projectTaskOutput, error) {
			id, err := parseID("task_id", in.TaskID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ProjectTask, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return ProjectTask{}, err
				}
				before, err := u.Projects().GetTask(ctx, id)
				if err != nil {
					return ProjectTask{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return ProjectTask{}, err
				}
				ts, err := u.Projects().Tasks(ctx, before.ProjectID)
				if err != nil {
					return ProjectTask{}, err
				}
				active := in.Body.Active == nil || *in.Body.Active
				name, probs := project.CheckTask(in.Body.Name, ts, id)
				if !active {
					probs = nil // a task going out of use may share a live task's name
					name = strings.TrimSpace(in.Body.Name)
				}
				if err := projectProblems(probs); err != nil {
					return ProjectTask{}, err
				}
				t, err := u.Projects().ChangeTask(ctx, id, before.Version, name, in.Body.Billable == nil || *in.Body.Billable, active)
				return projectTaskOf(t), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &projectTaskOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &projectTaskOutput{Status: http.StatusOK, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
