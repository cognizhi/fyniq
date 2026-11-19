package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// Capabilities of PRD-002 Appendix B that the people record answers to.
const (
	capDirectory = "Directory and org chart"
	capRecords   = "People records (not restricted)"
)

// DirectoryEntry is one person as everyone sees them: a name, a title, a department, a work email and a manager, nothing restricted (NM-PPL-06).
type DirectoryEntry struct {
	ID            string `json:"id" format:"uuid" doc:"The person's ID." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Name          string `json:"name" doc:"The legal name." example:"Ana Tan"`
	PreferredName string `json:"preferred_name,omitempty" doc:"The name they go by." example:"Anna"`
	JobTitle      string `json:"job_title,omitempty" doc:"Their job title on the day asked." example:"Engineer"`
	Department    string `json:"department,omitempty" doc:"Their department on the day asked." example:"Engineering"`
	WorkEmail     string `json:"work_email,omitempty" doc:"Their work email." example:"ana@acme.example"`
	ManagerID     string `json:"manager_id,omitempty" format:"uuid" doc:"Their line manager's ID on the day asked." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e62"`
	ManagerName   string `json:"manager_name,omitempty" doc:"Their line manager's name." example:"Ben Lim"`
}

// Job is one row of a person's job history.
type Job struct {
	From           string `json:"effective_from" format:"date" doc:"The day this job record starts." example:"2026-04-01"`
	To             string `json:"effective_to,omitempty" format:"date" doc:"The day it ends (the day the next starts); empty while current." example:"2026-09-01"`
	JobTitle       string `json:"job_title" doc:"The job title." example:"Senior Engineer"`
	Department     string `json:"department,omitempty" doc:"The department." example:"Engineering"`
	Worksite       string `json:"worksite" doc:"The worksite's country code and optional region." example:"SG"`
	EmploymentType string `json:"employment_type" enum:"full_time,part_time,contract,intern" doc:"The kind of employment." example:"full_time"`
	ManagerID      string `json:"manager_id,omitempty" format:"uuid" doc:"The line manager's person ID." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e62"`
}

// Classification is a person's legal classification on a day (NM-PPL-08). It is for the people team: never in the directory.
type Classification struct {
	ContractType string `json:"contract_type" enum:"service,for_service" doc:"A contract of service (an employee) or for service (a contractor, who gets no statutory employee entitlement)." example:"service"`
	EACovered    bool   `json:"ea_covered" doc:"Whether the Employment Act covers the person." example:"true"`
	PartIVClass  string `json:"part_iv_class" enum:"none,workman,non_workman" doc:"The Part IV class, which needs Employment Act cover." example:"none"`
	Residency    string `json:"residency" enum:"citizen,permanent_resident,foreigner" doc:"Citizen, permanent resident or foreigner." example:"citizen"`
	PRStart      string `json:"pr_start_date,omitempty" format:"date" doc:"The day permanent residence began: for a permanent resident only." example:"2024-05-01"`
	BirthDate    string `json:"birth_date,omitempty" format:"date" doc:"Date of birth, for the retirement and re-employment cohort." example:"1980-03-04"`
}

// Record is a person's record as the people team, the person themselves or their manager sees it.
type Record struct {
	ID              string          `json:"id" format:"uuid" doc:"The person's ID." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	LegalName       string          `json:"legal_name" doc:"The legal name." example:"Ana Tan"`
	PreferredName   string          `json:"preferred_name,omitempty" doc:"The name they go by." example:"Anna"`
	WorkEmail       string          `json:"work_email,omitempty" doc:"The work email." example:"ana@acme.example"`
	Phone           string          `json:"phone,omitempty" doc:"The phone number." example:"+65 6000 0000"`
	EmploymentStart string          `json:"employment_start" format:"date" doc:"The first day of employment." example:"2026-01-05"`
	ProbationEnd    string          `json:"probation_end,omitempty" format:"date" doc:"The day probation ends." example:"2026-04-05"`
	LeavingDate     string          `json:"leaving_date,omitempty" format:"date" doc:"The last day, once it is known." example:"2026-12-31"`
	LeavingReason   string          `json:"leaving_reason,omitempty" doc:"Why they left." example:"Resigned"`
	HasLogin        bool            `json:"has_login" doc:"Whether the person has a login to Nemura." example:"true"`
	Jobs            []Job           `json:"jobs" doc:"Their job history, oldest first."`
	Classification  *Classification `json:"classification,omitempty" doc:"Their legal classification on the day asked; only the people team sees it."`
}

// PersonInput is a person's own fields.
type PersonInput struct {
	LegalName       string `json:"legal_name" minLength:"1" maxLength:"200" doc:"The legal name." example:"Ana Tan"`
	PreferredName   string `json:"preferred_name,omitempty" maxLength:"200" doc:"The name they go by." example:"Anna"`
	WorkEmail       string `json:"work_email,omitempty" maxLength:"254" doc:"The work email." example:"ana@acme.example"`
	Phone           string `json:"phone,omitempty" maxLength:"40" doc:"The phone number." example:"+65 6000 0000"`
	EmploymentStart string `json:"employment_start" format:"date" doc:"The first day of employment." example:"2026-01-05"`
	ProbationEnd    string `json:"probation_end,omitempty" format:"date" doc:"The day probation ends." example:"2026-04-05"`
	LeavingDate     string `json:"leaving_date,omitempty" format:"date" doc:"The last day, once it is known." example:"2026-12-31"`
	LeavingReason   string `json:"leaving_reason,omitempty" maxLength:"500" doc:"Why they left." example:"Resigned"`
}

// JobInput is a job record's fields.
type JobInput struct {
	JobTitle       string `json:"job_title" minLength:"1" maxLength:"200" doc:"The job title." example:"Engineer"`
	Department     string `json:"department,omitempty" maxLength:"200" doc:"The department." example:"Engineering"`
	Worksite       string `json:"worksite" doc:"The worksite's country code and optional region." example:"SG"`
	EmploymentType string `json:"employment_type" enum:"full_time,part_time,contract,intern" doc:"The kind of employment." example:"full_time"`
	ManagerID      string `json:"manager_id,omitempty" format:"uuid" doc:"The line manager's person ID." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e62"`
}

// People reads and writes the people record for a signed-in person; the API decides who may call, the service does the work and the audit.
type People interface {
	Directory(ctx context.Context, s Session, on time.Time) ([]DirectoryEntry, error)
	// Relation says whether the person asked about is the caller's own record, or a report of the caller (a line manager's scope).
	Relation(ctx context.Context, s Session, id uuid.UUID) (own, report bool, err error)
	Get(ctx context.Context, s Session, id uuid.UUID, on time.Time, classification bool) (Record, error)
	Create(ctx context.Context, s Session, id uuid.UUID, p people.Person, j people.Job, c *people.Classification) error
	Update(ctx context.Context, s Session, id uuid.UUID, p people.Person) error
	ChangeJob(ctx context.Context, s Session, id uuid.UUID, from time.Time, j people.Job) error
	SetClassification(ctx context.Context, s Session, id uuid.UUID, from time.Time, c people.Classification) error
}

// ErrPersonExists is returned by Create for an ID that is taken.
var ErrPersonExists = errors.New("a person with that ID exists already")

func init() {
}

type directoryInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	AsOf   string `query:"as_of" format:"date" required:"false" doc:"The day to show people and their jobs on; today when empty." example:"2026-06-01"`
}

type directoryOutput struct {
	Body struct {
		People []DirectoryEntry `json:"people" doc:"Everyone who works for the organisation on the day, by name."`
	}
}

type recordInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	AsOf   string `query:"as_of" format:"date" required:"false" doc:"The day to show the job and classification on; today when empty." example:"2026-06-01"`
}

type recordOutput struct{ Body Record }

type createPersonInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		ID             string          `json:"id" format:"uuid" doc:"The new person's ID, chosen by the caller so a repeat is refused, not done twice." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
		Person         PersonInput     `json:"person" doc:"Their own fields."`
		Job            JobInput        `json:"job" doc:"Their first job, from their start date."`
		Classification *Classification `json:"classification,omitempty" doc:"Their legal classification, from their start date."`
	}
}

type updatePersonInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   PersonInput
}

type changeJobInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		From string   `json:"effective_from" format:"date" doc:"The day the new job record starts; the current one ends that day." example:"2026-09-01"`
		Job  JobInput `json:"job" doc:"The new job record."`
	}
}

type setClassificationInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Body   struct {
		From           string         `json:"effective_from" format:"date" doc:"The day the new classification starts; the current one ends that day." example:"2026-09-01"`
		Classification Classification `json:"classification" doc:"The new classification."`
	}
}

func date(field, s string, required bool) (*time.Time, error) {
	if s == "" {
		if required {
			return nil, Err(CodePersonInvalid, field+" is required: enter a date such as 2026-09-01.")
		}
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, Err(CodePersonInvalid, field+" is not a date: write it as 2026-09-01.")
	}
	return &t, nil
}

func (in PersonInput) person() (people.Person, error) {
	start, err := date("employment_start", in.EmploymentStart, true)
	if err != nil {
		return people.Person{}, err
	}
	probation, err := date("probation_end", in.ProbationEnd, false)
	if err != nil {
		return people.Person{}, err
	}
	leaving, err := date("leaving_date", in.LeavingDate, false)
	if err != nil {
		return people.Person{}, err
	}
	return people.Person{LegalName: in.LegalName, PreferredName: in.PreferredName, WorkEmail: in.WorkEmail, Phone: in.Phone, EmploymentStart: *start, ProbationEnd: probation, LeavingDate: leaving, LeavingReason: in.LeavingReason}, nil
}

func (in JobInput) job() (people.Job, error) {
	j := people.Job{Title: in.JobTitle, Department: in.Department, Worksite: in.Worksite, EmploymentType: in.EmploymentType}
	if in.ManagerID != "" {
		m, err := uuid.Parse(in.ManagerID)
		if err != nil {
			return j, Err(CodePersonInvalid, "manager_id is not an ID.")
		}
		j.Manager = &m
	}
	return j, nil
}

func (in Classification) classification() (people.Classification, error) {
	pr, err := date("pr_start_date", in.PRStart, false)
	if err != nil {
		return people.Classification{}, err
	}
	b, err := date("birth_date", in.BirthDate, false)
	if err != nil {
		return people.Classification{}, err
	}
	return people.Classification{ContractType: in.ContractType, EACovered: in.EACovered, PartIVClass: in.PartIVClass, Residency: in.Residency, PRStart: pr, BirthDate: b}, nil
}

// personError turns the people record's refusals into the catalogue's codes.
func personError(err error) error {
	var r *people.Refusal
	switch {
	case errors.As(err, &r):
		return Err(r.Code, r.Message)
	case errors.Is(err, ErrPersonExists):
		return Err(CodePersonExists, "A person with that ID exists already.")
	}
	return huma.Error500InternalServerError("The people record could not be saved.")
}

// rights returns the roles the person holds today and whether any of them holds right on capability.
func (a *API) rights(ctx context.Context, s Session, capability string, right authz.Right) (bool, error) {
	roles, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
	if err != nil {
		return false, huma.Error500InternalServerError("The roles could not be read.")
	}
	for _, r := range roles {
		if authz.Can(r, capability, right) {
			return true, nil
		}
	}
	return false, nil
}

func (a *API) asOf(s string) (time.Time, error) {
	t, err := date("as_of", s, false)
	if err != nil {
		return time.Time{}, err
	}
	if t == nil {
		return people.Day(a.d.Clock.Now()), nil
	}
	return *t, nil
}

func (a *API) registerPeople() {
	huma.Register(a.API, huma.Operation{
		OperationID: "listPeople", Method: "GET", Path: "/v1/people", Tags: []string{"People"}, Summary: "The directory and org chart",
		Description: "Lists everyone who works for the organisation on a day (today when none is given): name, job title, department, work email and line manager, nothing restricted. Every signed-in person may read it (Appendix B: Directory and org chart). It changes nothing.",
		Errors:      []int{401, 422},
	}, func(ctx context.Context, in *directoryInput) (*directoryOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		on, err := a.asOf(in.AsOf)
		if err != nil {
			return nil, err
		}
		list, err := a.d.People.Directory(ctx, s, on)
		if err != nil {
			return nil, personError(err)
		}
		out := &directoryOutput{}
		out.Body.People = list
		if list == nil {
			out.Body.People = []DirectoryEntry{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPerson", Method: "GET", Path: "/v1/people/{id}", Tags: []string{"People"}, Summary: "A person's record",
		Description: "Returns a person's record with their job history and, for the people team only, their legal classification on a day. The people team (HR, Payroll administrator, Owner) reads anyone; a person reads their own record without the classification; a line manager reads their reports'. Anyone else is refused as if the person did not exist. It changes nothing.",
		Errors:      []int{401, 404, 422},
	}, func(ctx context.Context, in *recordInput) (*recordOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
		}
		on, err := a.asOf(in.AsOf)
		if err != nil {
			return nil, err
		}
		team, err := a.peopleTeam(ctx, s)
		if err != nil {
			return nil, err
		}
		if !team {
			own, report, err := a.d.People.Relation(ctx, s, id)
			if err != nil {
				return nil, personError(err)
			}
			if !own && !report {
				return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
			}
		}
		rec, err := a.d.People.Get(ctx, s, id, on, team)
		if err != nil {
			return nil, personError(err)
		}
		return &recordOutput{Body: rec}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "createPerson", Method: "POST", Path: "/v1/people", Tags: []string{"People"}, Summary: "Add a person",
		Description: "Adds a person with their first job and, when given, their legal classification, both from their start date. The caller chooses the ID, so a repeat is refused (NM_PERSON_EXISTS). The HR officer and the HR administrator create people (Appendix B: People records). It is audited.",
		Errors:      []int{401, 403, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *createPersonInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.mayCreate(ctx, s); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.Body.ID)
		if err != nil {
			return nil, Err(CodePersonInvalid, "id is not an ID.")
		}
		p, err := in.Body.Person.person()
		if err != nil {
			return nil, err
		}
		j, err := in.Body.Job.job()
		if err != nil {
			return nil, err
		}
		var c *people.Classification
		if in.Body.Classification != nil {
			cl, err := in.Body.Classification.classification()
			if err != nil {
				return nil, err
			}
			c = &cl
		}
		if err := a.d.People.Create(ctx, s, id, p, j, c); err != nil {
			return nil, personError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "updatePerson", Method: "PUT", Path: "/v1/people/{id}", Tags: []string{"People"}, Summary: "Change a person's own fields",
		Description: "Changes a person's name, contact, probation end or leaving date and reason. The start date and the login are set once; the job and the classification are history and change through their own operations. The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 422},
	}, func(ctx context.Context, in *updatePersonInput) (*struct{}, error) {
		s, id, err := a.writer(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		p, err := in.Body.person()
		if err != nil {
			return nil, err
		}
		if err := a.d.People.Update(ctx, s, id, p); err != nil {
			return nil, personError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "changePersonJob", Method: "POST", Path: "/v1/people/{id}/job", Tags: []string{"People"}, Summary: "Change a person's job, manager or department",
		Description: "Ends the person's current job record the day the new one starts and starts it, so job title, department, worksite, employment type and line manager are history, never edited. A day on or before the current record's start is refused (NM_PERSON_HISTORY). The HR officer and the HR administrator. It is audited.",
		Errors:      []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *changeJobInput) (*struct{}, error) {
		s, id, err := a.writer(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		from, err := date("effective_from", in.Body.From, true)
		if err != nil {
			return nil, err
		}
		j, err := in.Body.Job.job()
		if err != nil {
			return nil, err
		}
		if err := a.d.People.ChangeJob(ctx, s, id, *from, j); err != nil {
			return nil, personError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setPersonClassification", Method: "POST", Path: "/v1/people/{id}/classification", Tags: []string{"People"}, Summary: "Set a person's legal classification",
		Description: "Ends the current classification the day the new one starts and starts it. A contractor (a contract for service) is not covered by the Employment Act; a Part IV class needs cover; a permanent resident has the date residence began (NM-PPL-08). The HR officer and the HR administrator. It is audited; the date of birth never enters the audit event.",
		Errors:      []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *setClassificationInput) (*struct{}, error) {
		s, id, err := a.writer(ctx, in.Cookie, in.ID)
		if err != nil {
			return nil, err
		}
		from, err := date("effective_from", in.Body.From, true)
		if err != nil {
			return nil, err
		}
		c, err := in.Body.Classification.classification()
		if err != nil {
			return nil, err
		}
		if err := a.d.People.SetClassification(ctx, s, id, *from, c); err != nil {
			return nil, personError(err)
		}
		return nil, nil
	})
}

// peopleTeam reports whether the person holds a role that reads every record: the HR officer and administrator, the Payroll administrator and the
// Owner. A line manager's "V (reports)" is a scope, not this right, and the AI's is within the asking person's rights (Appendix B).
func (a *API) peopleTeam(ctx context.Context, s Session) (bool, error) {
	roles, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
	if err != nil {
		return false, huma.Error500InternalServerError("The roles could not be read.")
	}
	for _, r := range roles {
		switch r {
		case authz.HROfficer, authz.HRAdmin, authz.PayrollAdmin, authz.Owner:
			if len(authz.Rights(r, capRecords)) > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

func (a *API) mayCreate(ctx context.Context, s Session) error {
	ok, err := a.rights(ctx, s, capRecords, authz.Create)
	if err != nil {
		return err
	}
	if !ok {
		return Err(CodeRoleRequired, "Only the HR officer and the HR administrator change the people record.")
	}
	return nil
}

// writer is the session and the person for a write: the caller must hold the right to create on people records.
func (a *API) writer(ctx context.Context, cookie, id string) (Session, uuid.UUID, error) {
	s, err := a.signedIn(ctx, cookie)
	if err != nil {
		return Session{}, uuid.Nil, err
	}
	if err := a.mayCreate(ctx, s); err != nil {
		return Session{}, uuid.Nil, err
	}
	pid, err := uuid.Parse(id)
	if err != nil {
		return Session{}, uuid.Nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
	}
	return s, pid, nil
}
