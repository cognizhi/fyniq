package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
)

// Hiring, the first slice (PRD-002 NM-REC-01 to NM-REC-03): requisitions approved by the Owner, a job post for each checked for fair wording, and the Fair Consideration Framework's advert dates kept as
// evidence. Rights come from Appendix B's row Hiring (P2): the Recruiter, the HR administrator and the Owner request and write posts, the HR officer reads, the Owner approves. Nothing here is on the AI or MCP surface.

const nemuraHiring = "Hiring (P2)"

// WordingFinding is a criterion found in a text.
type WordingFinding struct {
	Category string `json:"category" enum:"age,nationality,sex,marital_status,pregnancy,caregiving,race,religion,language,disability,mental_health" doc:"The criterion the guidelines ask an employer not to use." example:"age"`
	Phrase   string `json:"phrase" doc:"What was found, as written." example:"young"`
}

// JobPost is the job post of a requisition.
type JobPost struct {
	ID                string           `json:"id" format:"uuid" doc:"The post." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3b01"`
	Title             string           `json:"title" doc:"The post's title." example:"Data scientist"`
	Body              string           `json:"body" doc:"The post's text." example:"You will analyse data and build models."`
	State             string           `json:"state" enum:"draft,published,closed" doc:"A draft, published or closed." example:"draft"`
	PublishedOn       string           `json:"published_on,omitempty" format:"date" doc:"The day it was published." example:"2026-10-05"`
	ClosedOn          string           `json:"closed_on,omitempty" format:"date" doc:"The day it was closed." example:"2026-11-05"`
	RequirementNote   string           `json:"requirement_note,omitempty" doc:"The genuine occupational requirement recorded for a flagged criterion that was kept." example:"The role handles Mandarin-language contracts."`
	AdvertPostedOn    string           `json:"advert_posted_on,omitempty" format:"date" doc:"The day the advert went up on MyCareersFuture, posted by a person: the 14 days run from it." example:"2026-10-05"`
	AdvertReference   string           `json:"advert_reference,omitempty" doc:"The advert's reference." example:"MCF-2026-0042"`
	AdvertClosedOn    string           `json:"advert_closed_on,omitempty" format:"date" doc:"The day the advert came down or expired, when it was recorded." example:"2026-10-19"`
	AdvertUsableUntil string           `json:"advert_usable_until,omitempty" format:"date" doc:"The last day the advert can be relied on: 3 months after it came down." example:"2027-01-19"`
	AdvertRenewals    int              `json:"advert_renewals" doc:"How many times the advert was replaced after a material change; the replaced adverts are kept as evidence." example:"0"`
	Exemption         string           `json:"exemption,omitempty" enum:"fewer_than_10_employees,ep_salary_22500,role_one_month_or_less,transferee,s_pass_exempt" doc:"The recorded exemption from the advert." example:"fewer_than_10_employees"`
	ExemptionNote     string           `json:"exemption_note,omitempty" doc:"The evidence for the exemption." example:"Headcount 7 on 5 Oct."`
	OfferFrom         string           `json:"offer_from,omitempty" format:"date" doc:"For a role that needs a pass, the first day an offer may be made: the advert's 14 consecutive days have run. Empty when no pass is needed, an exemption is recorded, or the advert is not yet recorded." example:"2026-10-19"`
	OfferBlocked      bool             `json:"offer_blocked" doc:"A role that needs a pass has neither an advert date nor an exemption recorded, so no offer may be made yet." example:"false"`
	Findings          []WordingFinding `json:"findings" nullable:"false" doc:"The criteria found in the current words, for the author to read."`
}

// AdvertInput is an advert to record or to record anew.
type AdvertInput struct {
	PostedOn, Reference, ClosedOn string // the day it went up, its reference and, when known, the day it came down
	Change, Note                  string // for a renewal: what changed (occupation, salary, vacancies or uen) and a note
}

// Requisition is a request to hire for a role.
type Requisition struct {
	ID             string   `json:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Title          string   `json:"title" doc:"The role." example:"Data scientist"`
	Department     string   `json:"department" doc:"The team." example:"Engineering"`
	Reason         string   `json:"reason" enum:"new,replacement" doc:"A new role or a replacement." example:"replacement"`
	ReplacesID     string   `json:"replaces_person_id,omitempty" format:"uuid" doc:"The person a replacement replaces." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Replaces       string   `json:"replaces,omitempty" doc:"Their legal name." example:"Chen Yu"`
	SalaryMin      string   `json:"salary_min" doc:"The bottom of the range in Singapore dollars, as a decimal string." example:"6500.00"`
	SalaryMax      string   `json:"salary_max" doc:"The top of the range in Singapore dollars, as a decimal string." example:"8200.00"`
	TargetStart    string   `json:"target_start" format:"date" doc:"The start date aimed for." example:"2027-02-01"`
	PassNeeded     bool     `json:"pass_needed" doc:"An Employment Pass or S Pass application is expected, so the Fair Consideration Framework may apply." example:"false"`
	PlanStatus     string   `json:"plan_status" enum:"unknown,in_plan,not_in_plan" doc:"Whether the role is in Fyniq's plan; unknown until the link tells Nemura." example:"unknown"`
	State          string   `json:"state" enum:"requested,approved,rejected,filled,cancelled" doc:"Where it is." example:"requested"`
	RequestedBy    string   `json:"requested_by" doc:"Who asked." example:"Rita Recruiter"`
	RequestedAt    string   `json:"requested_at" format:"date-time" doc:"When." example:"2026-10-01T10:00:00Z"`
	DecidedBy      string   `json:"decided_by,omitempty" doc:"The Owner who decided." example:"Owen Owner"`
	DecidedAt      string   `json:"decided_at,omitempty" format:"date-time" doc:"When." example:"2026-10-02T10:00:00Z"`
	DecisionReason string   `json:"decision_reason,omitempty" doc:"Why." example:"In the plan for Q1."`
	JobPost        *JobPost `json:"job_post,omitempty" doc:"Its job post, once one is written."`
}

// RequisitionInput is a requisition to ask for.
type RequisitionInput struct {
	Title       string `json:"title" minLength:"1" maxLength:"120" doc:"The role." example:"Data scientist"`
	Department  string `json:"department" minLength:"1" maxLength:"120" doc:"The team." example:"Engineering"`
	Reason      string `json:"reason" enum:"new,replacement" doc:"A new role or a replacement." example:"replacement"`
	ReplacesID  string `json:"replaces_person_id,omitempty" format:"uuid" doc:"The person a replacement replaces." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	SalaryMin   string `json:"salary_min" pattern:"^[0-9]{1,9}(\\.[0-9]{1,2})?$" doc:"The bottom of the range in Singapore dollars, as a decimal string." example:"6500.00"`
	SalaryMax   string `json:"salary_max" pattern:"^[0-9]{1,9}(\\.[0-9]{1,2})?$" doc:"The top of the range in Singapore dollars, as a decimal string." example:"8200.00"`
	TargetStart string `json:"target_start" format:"date" doc:"The start date aimed for." example:"2027-02-01"`
	PassNeeded  bool   `json:"pass_needed,omitempty" doc:"An Employment Pass or S Pass application is expected." example:"false"`
}

// Hiring is the requisitions and job posts behind the API.
type Hiring interface {
	Requisitions(ctx context.Context, s Session) ([]Requisition, error)
	Request(ctx context.Context, s Session, in RequisitionInput) (Requisition, error)
	Decide(ctx context.Context, s Session, id uuid.UUID, approve bool, reason string) (Requisition, error)
	WritePost(ctx context.Context, s Session, id uuid.UUID, title, body string) (Requisition, error)
	Publish(ctx context.Context, s Session, id uuid.UUID, requirementNote string) (Requisition, error)
	RecordAdvert(ctx context.Context, s Session, id uuid.UUID, in AdvertInput) (Requisition, error)
	RenewAdvert(ctx context.Context, s Session, id uuid.UUID, in AdvertInput) (Requisition, error)
	RecordExemption(ctx context.Context, s Session, id uuid.UUID, kind, note string) (Requisition, error)
	ClosePost(ctx context.Context, s Session, id uuid.UUID) (Requisition, error)
}

type requisitionsOutput struct {
	Body struct {
		Requisitions []Requisition `json:"requisitions" nullable:"false" doc:"The requisitions, the newest first, each with its job post."`
	}
}
type requisitionOutput struct{ Body Requisition }
type requisitionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   RequisitionInput
}
type requisitionIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
}
type requisitionDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   struct {
		Approve bool   `json:"approve" doc:"Approve it; false rejects it." example:"true"`
		Reason  string `json:"reason,omitempty" maxLength:"500" doc:"Why." example:"In the plan for Q1."`
	}
}
type jobPostInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   struct {
		Title string `json:"title" minLength:"1" maxLength:"120" doc:"The post's title." example:"Data scientist"`
		Body  string `json:"body" minLength:"1" maxLength:"8000" doc:"The post's text." example:"You will analyse data and build models."`
	}
}
type publishInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   struct {
		RequirementNote string `json:"requirement_note,omitempty" maxLength:"1000" doc:"The genuine occupational requirement, needed to keep a criterion the check flagged." example:"The role handles Mandarin-language contracts."`
	}
}
type advertInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   struct {
		PostedOn  string `json:"posted_on" format:"date" doc:"The day the advert went up on MyCareersFuture." example:"2026-10-05"`
		Reference string `json:"reference,omitempty" maxLength:"100" doc:"The advert's reference." example:"MCF-2026-0042"`
		ClosedOn  string `json:"closed_on,omitempty" format:"date" doc:"The day the advert came down or expired, when it already has: an advert closed more than 3 months ago cannot be used (NM_HIRING_INVALID)." example:"2026-10-19"`
	}
}
type renewalInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   struct {
		Change    string `json:"change" enum:"occupation,salary,vacancies,uen" doc:"What changed: the occupation, the salary, the number of vacancies or the employer's UEN, each of which needs a new advert." example:"salary"`
		Note      string `json:"note,omitempty" maxLength:"500" doc:"What the change was." example:"The top of the range rose to 9,000."`
		PostedOn  string `json:"posted_on" format:"date" doc:"The day the new advert went up on MyCareersFuture." example:"2026-11-02"`
		Reference string `json:"reference,omitempty" maxLength:"100" doc:"The new advert's reference." example:"MCF-2026-0099"`
	}
}
type exemptionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   struct {
		Kind string `json:"kind" enum:"fewer_than_10_employees,ep_salary_22500,role_one_month_or_less,transferee,s_pass_exempt" doc:"The exemption." example:"fewer_than_10_employees"`
		Note string `json:"note" minLength:"1" maxLength:"500" doc:"The evidence." example:"Headcount 7 on 5 Oct."`
	}
}
type wordingInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Text string `json:"text" minLength:"1" maxLength:"8000" doc:"A job post or an interview question." example:"We want a young, energetic digital native."`
	}
}
type wordingOutput struct {
	Body struct {
		Findings []WordingFinding `json:"findings" nullable:"false" doc:"The criteria found; empty when none."`
	}
}

func hiringError(err error) error {
	var r *hiring.Refusal
	if errors.As(err, &r) {
		return Err(r.Code, r.Message)
	}
	return huma.Error500InternalServerError("The hiring request could not be handled.")
}

func init() {
	IdempotencyExempt["POST /v1/requisitions"] = "a requisition asked for twice shows twice and the Owner decides each; its words cannot change"
	IdempotencyExempt["POST /v1/requisitions/{id}/decision"] = "a requisition is decided once: a second is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/requisitions/{id}/job-post/publication"] = "a post is published once: a second is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/requisitions/{id}/job-post/advert"] = "the advert's date is recorded once: a second is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/requisitions/{id}/job-post/advert/renewal"] = "a renewal needs a later day than the advert it replaces: a repeat is refused (NM_HIRING_INVALID)"
	IdempotencyExempt["POST /v1/requisitions/{id}/job-post/exemption"] = "an exemption is recorded once: a second is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/requisitions/{id}/job-post/closing"] = "a post is closed once: a second is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/fair-wording-check"] = "it only reads: the same text gives the same findings and nothing is kept"
}

func (a *API) registerHiring() {
	available := func() error { return Err(CodeUnavailable, "Hiring is not set up in this deployment.") }
	// who is what the caller may do on Hiring (P2) in Appendix B: Create (the Recruiter, the HR administrator and the Owner), View (the HR officer) and Approve (the Owner).
	type rights struct{ create, view, approve, interviewer, people bool }
	rightsOf := func(ctx context.Context, s Session) (rights, error) {
		held, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
		if err != nil {
			return rights{}, huma.Error500InternalServerError("The roles could not be read.")
		}
		var r rights
		for _, h := range held {
			if h == authz.Interviewer {
				r.interviewer = true // an interviewer reads the candidates assigned to them and scores them: the database scopes it (migration 000069)
			}
			if h == authz.LineManager || h == authz.Interviewer {
				continue // their rights are View with a condition (own roles; assigned candidates only) that needs the pipeline and the line: until it exists they read nothing here
			}
			set := authz.Rights(h, nemuraHiring)
			pr := authz.Rights(h, "People records (not restricted)")
			r.people = r.people || pr[authz.Create] || pr[authz.Approve]
			r.create = r.create || set[authz.Create]
			r.approve = r.approve || set[authz.Approve]
			r.view = r.view || set[authz.View] || set[authz.Create] || set[authz.Approve]
		}
		return r, nil
	}
	gate := func(ctx context.Context, cookie string, need func(rights) bool, say string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		r, err := rightsOf(ctx, s)
		if err != nil {
			return s, err
		}
		if !need(r) {
			return s, Err(CodeRoleRequired, say)
		}
		sw, err := a.d.Features.List(ctx, s)
		if err != nil {
			return s, huma.Error500InternalServerError("The feature switches could not be read.")
		}
		if sw["hiring"].State != features.On {
			return s, Err(CodeFeatureOff, "Hiring is switched off for this organisation: an Owner or the HR administrator switches it on under Settings, Features.")
		}
		if a.d.Hiring == nil {
			return s, available()
		}
		return s, nil
	}
	reader := func(ctx context.Context, c string) (Session, error) {
		return gate(ctx, c, func(r rights) bool { return r.view }, "Only the Recruiter, the HR officer, the HR administrator and the Owner read requisitions.")
	}
	writer := func(ctx context.Context, c string) (Session, error) {
		return gate(ctx, c, func(r rights) bool { return r.create }, "Only the Recruiter, the HR administrator and the Owner ask for a requisition or write a job post.")
	}
	hiringOrInterviewer := func(ctx context.Context, c string) (Session, error) {
		return gate(ctx, c, func(r rights) bool { return r.view || r.interviewer }, "Only the Recruiter, the HR officer, the HR administrator, the Owner and an interviewer read candidates.")
	}
	interviewer := func(ctx context.Context, c string) (Session, error) {
		return gate(ctx, c, func(r rights) bool { return r.interviewer }, "Only an interviewer scores a candidate, and only one assigned to them.")
	}
	hirer := func(ctx context.Context, c string) (Session, error) {
		return gate(ctx, c, func(r rights) bool { return r.create && r.people }, "Only the HR administrator and the Owner hire a candidate: it makes a person record.")
	}
	defer func() { a.registerPipeline(hiringOrInterviewer, writer, interviewer, hirer) }()
	id := func(raw string) (uuid.UUID, error) {
		u, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, Err(CodeNotFound, "There is no such requisition.")
		}
		return u, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listRequisitions", Method: http.MethodGet, Path: "/v1/requisitions", Tags: []string{"Hiring"}, Summary: "Requisitions",
		Description: "PRD-002 NM-REC-01. The requests to hire, the newest first, each with its job post, the dates of its advert and the day an offer may be made. The Recruiter, the HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*requisitionsOutput, error) {
		s, err := reader(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Hiring.Requisitions(ctx, s)
		if err != nil {
			return nil, hiringError(err)
		}
		out := &requisitionsOutput{}
		out.Body.Requisitions = append([]Requisition{}, list...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "requestRequisition", Method: http.MethodPost, Path: "/v1/requisitions", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Ask to hire for a role",
		Description: "Asks for a requisition: the role, the team, whether it is new or replaces someone, the salary range in Singapore dollars (decimal strings), the start date aimed for and whether a pass is expected. Whether the role is in Fyniq's plan stays unknown until the link tells Nemura. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *requisitionInput) (*requisitionOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.Request(ctx, s, in.Body)
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideRequisition", Method: http.MethodPost, Path: "/v1/requisitions/{id}/decision", Tags: []string{"Hiring"}, Summary: "Approve or reject a requisition",
		Description: "The Owner approves or rejects a requisition, once. Approval opens the role: a job post may then be published. The Owner. It is audited.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *requisitionDecisionInput) (*requisitionOutput, error) {
		s, err := gate(ctx, in.Cookie, func(r rights) bool { return r.approve }, "Only the Owner approves a requisition.")
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.Decide(ctx, s, u, in.Body.Approve, in.Body.Reason)
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "writeJobPost", Method: http.MethodPut, Path: "/v1/requisitions/{id}/job-post", Tags: []string{"Hiring"}, Summary: "Write or change a job post",
		Description: "Writes the requisition's job post as a draft, or changes the draft; a published post never changes. The response lists the criteria the guidelines ask an employer not to use that the check found in the words (NM-REC-03): it decides nothing. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *jobPostInput) (*requisitionOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.WritePost(ctx, s, u, in.Body.Title, in.Body.Body)
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "publishJobPost", Method: http.MethodPost, Path: "/v1/requisitions/{id}/job-post/publication", Tags: []string{"Hiring"}, Summary: "Publish a job post",
		Description: "Publishes the draft of an approved requisition. When the check flags a criterion, the post is published only with the genuine occupational requirement recorded (requirement_note); otherwise it is refused with the criteria named (NM_HIRING_WORDING). The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *publishInput) (*requisitionOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.Publish(ctx, s, u, in.Body.RequirementNote)
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "recordJobAdvert", Method: http.MethodPost, Path: "/v1/requisitions/{id}/job-post/advert", Tags: []string{"Hiring"}, Summary: "Record the day the advert went up",
		Description: "Records the day a person posted the advert on MyCareersFuture (there is no posting API), once. For a role that needs a pass the offer waits until 14 consecutive days have run (NM-REC-02). The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *advertInput) (*requisitionOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.RecordAdvert(ctx, s, u, AdvertInput{PostedOn: in.Body.PostedOn, Reference: in.Body.Reference, ClosedOn: in.Body.ClosedOn})
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "renewJobAdvert", Method: http.MethodPost, Path: "/v1/requisitions/{id}/job-post/advert/renewal", Tags: []string{"Hiring"}, Summary: "Record a new advert after a material change",
		Description: "PRD-002 NM-REC-02. A change of the occupation, the salary, the number of vacancies or the employer's UEN needs a new advert, and its 14 consecutive days start again. Records the new advert's day, archives the advert it replaces " +
			"with what changed, and counts the renewal. Only a post that has an advert recorded and is not closed; the new day is after the old one. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *renewalInput) (*requisitionOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.RenewAdvert(ctx, s, u, AdvertInput{PostedOn: in.Body.PostedOn, Reference: in.Body.Reference, Change: in.Body.Change, Note: in.Body.Note})
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "readCareersSettings", Method: http.MethodGet, Path: "/v1/careers-page", Tags: []string{"Hiring"}, Summary: "The organisation's careers page settings",
		Description: "PRD-002 NM-REC-02. The address, whether the page is on, the company's name and the words above the list. The Recruiter, the HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*careersSettingsOutput, error) {
		s, err := reader(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Careers == nil {
			return nil, available()
		}
		v, err := a.d.Careers.Settings(ctx, s)
		if err != nil {
			return nil, hiringError(err)
		}
		return &careersSettingsOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "saveCareersSettings", Method: http.MethodPut, Path: "/v1/careers-page", Tags: []string{"Hiring"}, Summary: "Choose the careers page's address and switch it on or off",
		Description: "PRD-002 NM-REC-02. Sets the address (3 to 40 lower-case letters, digits and single hyphens, one that no other company uses), the company's name and the words above the list, and whether anyone can read the page at /careers/<address>. " +
			"What the page shows is only each published post's title, team, text and day. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 409, 422, 503},
	}, func(ctx context.Context, in *careersSaveInput) (*careersSettingsOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Careers == nil {
			return nil, available()
		}
		v, err := a.d.Careers.Save(ctx, s, CareersSettings{Slug: in.Body.Slug, Enabled: in.Body.Enabled, Company: in.Body.Company, Intro: in.Body.Intro})
		if err != nil {
			return nil, hiringError(err)
		}
		return &careersSettingsOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "recordJobExemption", Method: http.MethodPost, Path: "/v1/requisitions/{id}/job-post/exemption", Tags: []string{"Hiring"}, Summary: "Record an exemption from the advert",
		Description: "Records, once, an exemption from the Fair Consideration Framework's advert with its evidence, for a role that needs a pass. The offer is then not held back. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *exemptionInput) (*requisitionOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.RecordExemption(ctx, s, u, in.Body.Kind, in.Body.Note)
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "closeJobPost", Method: http.MethodPost, Path: "/v1/requisitions/{id}/job-post/closing", Tags: []string{"Hiring"}, Summary: "Close a job post",
		Description: "Closes a published post, once. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *requisitionIDInput) (*requisitionOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Hiring.ClosePost(ctx, s, u)
		if err != nil {
			return nil, hiringError(err)
		}
		return &requisitionOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "checkFairWording", Method: http.MethodPost, Path: "/v1/fair-wording-check", Tags: []string{"Hiring"}, Summary: "Check wording for criteria the guidelines name",
		Description: "Lists the age, nationality, sex, marital-status, pregnancy, caregiving, race, religion, language, disability and mental-health criteria found in a job post or an interview question (NM-REC-03). It decides nothing and keeps nothing. The Recruiter, the HR officer, the HR administrator and the Owner.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *wordingInput) (*wordingOutput, error) {
		if _, err := reader(ctx, in.Cookie); err != nil {
			return nil, err
		}
		out := &wordingOutput{}
		out.Body.Findings = findings(hiring.FairWording(in.Body.Text))
		return out, nil
	})
}

func findings(in []hiring.Finding) []WordingFinding {
	out := make([]WordingFinding, 0, len(in))
	for _, f := range in {
		out = append(out, WordingFinding{Category: f.Category, Phrase: f.Phrase})
	}
	return out
}
