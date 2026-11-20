package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// The pipeline (PRD-002 NM-REC-04, NM-REC-05): candidates for a requisition on a board, each move kept; notes; interview kits with scorecards; interviewers assigned to a candidate; and a candidate's
// consent and retention. The Recruiter, the HR administrator and the Owner write; the HR officer reads; an interviewer reads only the candidates assigned to them and scores them (Appendix B: Hiring). The
// database scopes every read (migration 000069), so a missed check here cannot read more. Nothing here is on the AI or MCP surface: the assistant never screens, ranks or scores a candidate (NM-AI-05).

// CandidateMove is one move on the board.
type CandidateMove struct {
	From   string `json:"from,omitempty" doc:"Where the candidate was." example:"applied"`
	To     string `json:"to" doc:"Where the candidate went." example:"screening"`
	Reason string `json:"reason,omitempty" doc:"Why, required when the candidate is not progressing." example:"Not enough experience with the tools the role needs."`
	By     string `json:"by,omitempty" doc:"Who moved them; empty for the application itself." example:"Rita Recruiter"`
	At     string `json:"at" format:"date-time" doc:"When." example:"2026-11-02T10:00:00Z"`
}

// CandidateNote is a note of the hiring team.
type CandidateNote struct {
	Body string `json:"body" doc:"The note." example:"Strong on forecasting."`
	By   string `json:"by" doc:"Who wrote it." example:"Rita Recruiter"`
	At   string `json:"at" format:"date-time" doc:"When." example:"2026-11-02T10:00:00Z"`
}

// CandidateScore is one score of a scorecard criterion.
type CandidateScore struct {
	Criterion string `json:"criterion" doc:"The criterion." example:"Analytical skill"`
	Score     int    `json:"score" minimum:"1" maximum:"5" doc:"1 to 5." example:"4"`
	Comment   string `json:"comment,omitempty" doc:"What the score rests on." example:"Worked the case study well."`
	By        string `json:"by" doc:"The interviewer." example:"Ian Interviewer"`
	At        string `json:"at" format:"date-time" doc:"When." example:"2026-11-05T10:00:00Z"`
}

// Candidate is a candidate for a requisition.
type Candidate struct {
	ID                 string           `json:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	RequisitionID      string           `json:"requisition_id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Name               string           `json:"name" doc:"The candidate's name." example:"Chen Yu"`
	Email              string           `json:"email" doc:"Their email address." example:"chen.yu@example.com"`
	CV                 string           `json:"cv,omitempty" doc:"The CV as text, in the detail only." example:"Five years in FP&A."`
	Stage              string           `json:"stage" enum:"applied,screening,interview,offer,hired,not_progressing" doc:"Where they are on the board." example:"screening"`
	StageReason        string           `json:"stage_reason,omitempty" doc:"The reason of the last decision." example:"Not enough experience."`
	StageAt            string           `json:"stage_at" format:"date-time" doc:"When the stage last changed." example:"2026-11-02T10:00:00Z"`
	AppliedVia         string           `json:"applied_via" enum:"careers_page,recruiter" doc:"How they came." example:"careers_page"`
	AppliedAt          string           `json:"applied_at" format:"date-time" doc:"When." example:"2026-11-01T10:00:00Z"`
	ConsentFuture      bool             `json:"consent_future" doc:"They consented to be kept for future roles, and have not withdrawn it." example:"false"`
	RetentionUntil     string           `json:"retention_until,omitempty" format:"date" doc:"For a candidate who is not progressing without that consent: the day their data is due for deletion, six months after the decision." example:"2027-05-02"`
	Moves              []CandidateMove  `json:"moves,omitempty" doc:"Every move, in the detail only."`
	Notes              []CandidateNote  `json:"notes,omitempty" doc:"The hiring team's notes, in the detail and for the hiring team only. Opinions formed to evaluate a candidate are not released on an access request."`
	Scores             []CandidateScore `json:"scores,omitempty" doc:"The scorecard's scores: the hiring team reads all of them, an interviewer only their own."`
	InterviewerNames   []string         `json:"interviewers,omitempty" doc:"The interviewers assigned, in the detail."`
	Hire               *HireView        `json:"hire,omitempty" doc:"The hire, once the candidate was hired."`
	ConsentFutureAt    string           `json:"consent_future_at,omitempty" format:"date-time" doc:"When they consented to be kept for future roles." example:"2026-11-01T10:00:00Z"`
	ConsentWithdrawnAt string           `json:"consent_future_withdrawn_at,omitempty" format:"date-time" doc:"When they withdrew it." example:"2026-12-01T10:00:00Z"`
}

// HireView is what a hire made: the person, their plan and the pass application.
type HireView struct {
	PersonID          string `json:"person_id" format:"uuid" doc:"The person record made at the hire." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	PlanID            string `json:"plan_id,omitempty" format:"uuid" doc:"The onboarding plan started for them." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d5a01"`
	StartDate         string `json:"start_date" format:"date" doc:"Their first day." example:"2027-02-01"`
	PassNeeded        bool   `json:"pass_needed" doc:"An Employment Pass or S Pass application is expected." example:"true"`
	PassApplicationOn string `json:"pass_application_on,omitempty" format:"date" doc:"The day the pass application was made, recorded once." example:"2027-01-15"`
}

// HireInput is a hire: what the person record and plan are made from.
type HireInput struct {
	StartDate      string `json:"start_date" format:"date" doc:"The first day of employment, today or later." example:"2027-02-01"`
	EmploymentType string `json:"employment_type" enum:"full_time,part_time,contract,intern" doc:"The kind of employment." example:"full_time"`
	Worksite       string `json:"worksite,omitempty" maxLength:"11" doc:"The country code of the worksite; SG when left out." example:"SG"`
	JobTitle       string `json:"job_title,omitempty" maxLength:"200" doc:"The job title; the requisition's role when left out." example:"Data scientist"`
	Department     string `json:"department,omitempty" maxLength:"200" doc:"The team; the requisition's team when left out." example:"Engineering"`
	TemplateID     string `json:"template_id,omitempty" format:"uuid" doc:"The onboarding template; the standard one when left out." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d5b01"`
}

// InterviewKit is a requisition's questions and scorecard.
type InterviewKit struct {
	Exists          bool             `json:"exists" doc:"False until a kit is saved." example:"true"`
	Questions       []string         `json:"questions" nullable:"false" doc:"The questions, 1 to 30."`
	Criteria        []string         `json:"criteria" nullable:"false" doc:"The scorecard's criteria, 1 to 10."`
	RequirementNote string           `json:"requirement_note,omitempty" doc:"The genuine occupational requirement recorded for a flagged criterion that was kept." example:"The role is about the Mandarin market."`
	Findings        []WordingFinding `json:"findings" nullable:"false" doc:"The criteria found in the questions and criteria, for the author to read."`
}

// CandidateInput is a candidate to add or one applying.
type CandidateInput struct {
	Name          string `json:"name" minLength:"1" maxLength:"120" doc:"The candidate's name." example:"Chen Yu"`
	Email         string `json:"email" minLength:"3" maxLength:"200" doc:"Their email address." example:"chen.yu@example.com"`
	CV            string `json:"cv,omitempty" maxLength:"20000" doc:"The CV as text." example:"Five years in FP&A."`
	ConsentFuture bool   `json:"consent_future,omitempty" doc:"They consent to be kept for future roles, separately from this application; they may withdraw it." example:"false"`
}

// Pipeline is the candidates behind the API.
type Pipeline interface {
	Candidates(ctx context.Context, s Session, requisition uuid.UUID) ([]Candidate, error)
	Candidate(ctx context.Context, s Session, id uuid.UUID) (Candidate, error)
	Add(ctx context.Context, s Session, requisition uuid.UUID, in CandidateInput) (Candidate, error)
	Move(ctx context.Context, s Session, id uuid.UUID, to, reason string) (Candidate, error)
	Note(ctx context.Context, s Session, id uuid.UUID, body string) (Candidate, error)
	Assign(ctx context.Context, s Session, id, member uuid.UUID) (Candidate, error)
	Score(ctx context.Context, s Session, id uuid.UUID, criterion string, score int, comment string) (Candidate, error)
	Consent(ctx context.Context, s Session, id uuid.UUID, given bool) (Candidate, error)
	Hire(ctx context.Context, s Session, id uuid.UUID, in HireInput) (Candidate, error)
	PassApplication(ctx context.Context, s Session, id uuid.UUID, on string) (Candidate, error)
	Kit(ctx context.Context, s Session, requisition uuid.UUID) (InterviewKit, error)
	SaveKit(ctx context.Context, s Session, requisition uuid.UUID, in InterviewKit) (InterviewKit, error)
	// Apply records an application made on the careers page: false when the address and the post do not name an open post.
	Apply(ctx context.Context, slug string, post uuid.UUID, in CandidateInput) (bool, error)
}

type candidatesOutput struct {
	Body struct {
		Candidates []Candidate `json:"candidates" nullable:"false" doc:"The candidates for the requisition, the newest first."`
	}
}
type candidateOutput struct{ Body Candidate }
type kitOutput struct{ Body InterviewKit }
type reqPath struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
}
type candPath struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
}
type candidateAddInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   CandidateInput
}
type candidateMoveInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		To     string `json:"to" enum:"screening,interview,offer,hired,not_progressing" doc:"Where to." example:"screening"`
		Reason string `json:"reason,omitempty" maxLength:"500" doc:"Why; required for not_progressing." example:"Not enough experience."`
	}
}
type candidateNoteInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		Body string `json:"body" minLength:"1" maxLength:"4000" doc:"The note." example:"Strong on forecasting."`
	}
}
type candidateAssignInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		Member string `json:"member_id" format:"uuid" doc:"The member who interviews, who must hold the Interviewer role." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e01"`
	}
}
type candidateScoreInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		Criterion string `json:"criterion" minLength:"1" maxLength:"120" doc:"A criterion of the kit's scorecard." example:"Analytical skill"`
		Score     int    `json:"score" minimum:"1" maximum:"5" doc:"1 to 5." example:"4"`
		Comment   string `json:"comment,omitempty" maxLength:"1000" doc:"What the score rests on." example:"Worked the case study well."`
	}
}
type candidateConsentInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		Given bool `json:"given" doc:"True when the candidate consents to be kept for future roles, false when they withdraw that consent." example:"true"`
	}
}
type candidateHireInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   HireInput
}
type candidatePassInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		On string `json:"on" format:"date" doc:"The day the Employment Pass or S Pass application was made." example:"2027-01-15"`
	}
}
type kitSaveInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Body   struct {
		Questions       []string `json:"questions" minItems:"1" maxItems:"30" doc:"The questions."`
		Criteria        []string `json:"criteria" minItems:"1" maxItems:"10" doc:"The scorecard's criteria."`
		RequirementNote string   `json:"requirement_note,omitempty" maxLength:"1000" doc:"The genuine occupational requirement for a flagged criterion that stays (10 or more characters)." example:"The role is about the Mandarin market."`
	}
}
type applyInput struct {
	Slug string `path:"slug" minLength:"3" maxLength:"40" pattern:"^[a-z0-9-]+$" doc:"The company's careers address." example:"acme-careers"`
	Body struct {
		Post string `json:"post_id" format:"uuid" doc:"The post applied for, from the careers page." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3b01"`
		CandidateInput
	}
}
type roleApplicationOutput struct {
	Body struct {
		Applied bool `json:"applied" doc:"True when the application is recorded; false for an address and a post that name no open role." example:"true"`
	}
}

const rateApply = 5 // applications from one email address to one company in an hour

func init() {
	IdempotencyExempt["POST /v1/requisitions/{id}/candidates"] = "a candidate added twice shows twice and the recruiter merges them; nothing they gave can change"
	IdempotencyExempt["POST /v1/candidates/{id}/moves"] = "a move is checked against where the candidate is now: a repeat is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/candidates/{id}/notes"] = "a note written twice is two notes, each kept as written"
	IdempotencyExempt["POST /v1/candidates/{id}/interviewers"] = "an interviewer is assigned once: a repeat is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/candidates/{id}/scores"] = "a score is appended; the latest of an interviewer's scores for a criterion counts"
	IdempotencyExempt["POST /v1/candidates/{id}/future-consent"] = "consent is given or withdrawn once each: a repeat is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/candidates/{id}/hire"] = "a candidate is hired once: a repeat is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/candidates/{id}/pass-application"] = "the day is recorded once: a repeat is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/careers/{slug}/applications"] = "an application made twice is two candidates the recruiter merges; it is rate-limited by email address"
}

func (a *API) registerPipeline(seen, writer, interviewer, hirer func(context.Context, string) (Session, error)) {
	needs := func() error { return Err(CodeUnavailable, "Hiring is not set up in this deployment.") }
	id := func(raw string) (uuid.UUID, error) {
		u, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, Err(CodeNotFound, "There is no such requisition or candidate.")
		}
		return u, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listCandidates", Method: http.MethodGet, Path: "/v1/requisitions/{id}/candidates", Tags: []string{"Hiring"}, Summary: "The candidates for a requisition",
		Description: "PRD-002 NM-REC-04. The board: each candidate's stage, how they came and whether they consent to be kept for future roles, the newest first. The hiring team reads all of them; an interviewer only the candidates assigned to them (the database scopes it). Without the CV, notes and scores.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *reqPath) (*candidatesOutput, error) {
		s, err := seen(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		l, err := a.d.Pipeline.Candidates(ctx, s, u)
		if err != nil {
			return nil, hiringError(err)
		}
		out := &candidatesOutput{}
		out.Body.Candidates = append([]Candidate{}, l...)
		return out, nil
	})
	one := func(op huma.Operation, who func(context.Context, string) (Session, error), run func(ctx context.Context, s Session, u uuid.UUID, in *candPath) (Candidate, error)) {
		huma.Register(a.API, op, func(ctx context.Context, in *candPath) (*candidateOutput, error) {
			s, err := who(ctx, in.Cookie)
			if err != nil {
				return nil, err
			}
			u, err := id(in.ID)
			if err != nil {
				return nil, err
			}
			if a.d.Pipeline == nil {
				return nil, needs()
			}
			c, err := run(ctx, s, u, in)
			if err != nil {
				return nil, hiringError(err)
			}
			return &candidateOutput{Body: c}, nil
		})
	}
	one(huma.Operation{OperationID: "getCandidate", Method: http.MethodGet, Path: "/v1/candidates/{id}", Tags: []string{"Hiring"}, Summary: "A candidate",
		Description: "PRD-002 NM-REC-04. The profile with the CV, every move, the hiring team's notes (never an interviewer's view), the scores (an interviewer's own only) and the interviewers assigned. The hiring team, and an interviewer for a candidate assigned to them; any other is told there is none. It changes nothing.", Errors: []int{401, 403, 404, 409, 503}},
		seen, func(ctx context.Context, s Session, u uuid.UUID, _ *candPath) (Candidate, error) {
			return a.d.Pipeline.Candidate(ctx, s, u)
		})
	huma.Register(a.API, huma.Operation{
		OperationID: "addCandidate", Method: http.MethodPost, Path: "/v1/requisitions/{id}/candidates", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Add a candidate to a requisition",
		Description: "PRD-002 NM-REC-04, NM-REC-05. Adds a candidate at the applied stage for an approved requisition: name, email, CV as text and, separately, whether they consent to be kept for future roles. Applying for a role is deemed consent to assessing the application; keeping them for future roles needs their consent. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidateAddInput) (*candidateOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.Add(ctx, s, u, in.Body)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "moveCandidate", Method: http.MethodPost, Path: "/v1/candidates/{id}/moves", Tags: []string{"Hiring"}, Summary: "Move a candidate on the board",
		Description: "PRD-002 NM-REC-04. Moves a candidate along applied, screening, interview, offer and hired, or to not progressing with the reason of the decision. Hired is made only by the offer (NM-REC-06) and is refused here. Every move is kept with who made it. A candidate not progressing may be reopened to screening. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidateMoveInput) (*candidateOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.Move(ctx, s, u, in.Body.To, in.Body.Reason)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "noteCandidate", Method: http.MethodPost, Path: "/v1/candidates/{id}/notes", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Write a note about a candidate",
		Description: "PRD-002 NM-REC-04. A note of the hiring team, kept as written. Notes are opinions formed to evaluate a candidate: they are not released on an access request (PDPC). The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidateNoteInput) (*candidateOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.Note(ctx, s, u, in.Body.Body)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "assignInterviewer", Method: http.MethodPost, Path: "/v1/candidates/{id}/interviewers", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Assign an interviewer to a candidate",
		Description: "PRD-002 NM-REC-04. Gives a member who holds the Interviewer role the right to read this candidate and score them, and nothing else; once for each pair. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidateAssignInput) (*candidateOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		m, err := uuid.Parse(in.Body.Member)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such member.")
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.Assign(ctx, s, u, m)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "scoreCandidate", Method: http.MethodPost, Path: "/v1/candidates/{id}/scores", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Score a candidate on a scorecard criterion",
		Description: "PRD-002 NM-REC-04. An interviewer assigned to the candidate scores a criterion of the requisition's kit from 1 to 5 with the comment it rests on; a score is appended and never changed (the latest of one interviewer's scores for a criterion counts). The Interviewer only, and only for a candidate assigned to them. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidateScoreInput) (*candidateOutput, error) {
		s, err := interviewer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.Score(ctx, s, u, in.Body.Criterion, in.Body.Score, in.Body.Comment)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "recordCandidateConsent", Method: http.MethodPost, Path: "/v1/candidates/{id}/future-consent", Tags: []string{"Hiring"}, Summary: "Record a candidate's consent to be kept for future roles, or its withdrawal",
		Description: "PRD-002 NM-REC-05. Keeping a candidate for future roles needs their separate consent, which they can withdraw: given once and withdrawn once. While it stands a candidate who is not progressing is not due for deletion; withdrawn, the six months run from the decision, or from the withdrawal when that is later. The Recruiter, the HR administrator and the Owner, on the candidate's word. It is audited.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *candidateConsentInput) (*candidateOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.Consent(ctx, s, u, in.Body.Given)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "hireCandidate", Method: http.MethodPost, Path: "/v1/candidates/{id}/hire", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Hire a candidate whose offer is accepted",
		Description: "PRD-002 NM-REC-06. A candidate at the offer stage becomes a person record with the start date, the job and the employment type, an onboarding plan is started for them, the requisition is filled and the candidate is hired, once. For a role that needs an Employment Pass or S Pass, the hire waits until the advert's 14 consecutive days have run or an exemption is recorded, and an advert closed more than 3 months ago cannot be used (NM_HIRING_CONFLICT). The HR administrator and the Owner, who also hold the right to create a person record. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidateHireInput) (*candidateOutput, error) {
		s, err := hirer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.Hire(ctx, s, u, in.Body)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "recordPassApplication", Method: http.MethodPost, Path: "/v1/candidates/{id}/pass-application", Tags: []string{"Hiring"}, Summary: "Record the day the pass application was made",
		Description: "PRD-002 NM-REC-06. Tracks the day the Employment Pass or S Pass application was made for a hire whose role needs one, once, a day up to today. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidatePassInput) (*candidateOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.PassApplication(ctx, s, u, in.Body.On)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getInterviewKit", Method: http.MethodGet, Path: "/v1/requisitions/{id}/interview-kit", Tags: []string{"Hiring"}, Summary: "A requisition's interview kit",
		Description: "PRD-002 NM-REC-04. The questions and the scorecard's criteria, with any criterion the fair-wording check found. The hiring team and interviewers. It changes nothing.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *reqPath) (*kitOutput, error) {
		s, err := seen(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		k, err := a.d.Pipeline.Kit(ctx, s, u)
		if err != nil {
			return nil, hiringError(err)
		}
		return &kitOutput{Body: k}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "saveInterviewKit", Method: http.MethodPut, Path: "/v1/requisitions/{id}/interview-kit", Tags: []string{"Hiring"}, Summary: "Save a requisition's interview kit",
		Description: "PRD-002 NM-REC-03, NM-REC-04. Saves the questions and the scorecard's criteria, replacing the kit. They are checked against the Tripartite Guidelines on Fair Employment Practices like a job post: a question that names a protected criterion is saved only with the genuine occupational requirement recorded (NM_HIRING_WORDING otherwise, the findings in the kit's next read). The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *kitSaveInput) (*kitOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		k, err := a.d.Pipeline.SaveKit(ctx, s, u, InterviewKit{Questions: in.Body.Questions, Criteria: in.Body.Criteria, RequirementNote: in.Body.RequirementNote})
		if err != nil {
			return nil, hiringError(err)
		}
		return &kitOutput{Body: k}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "applyForRole", Method: http.MethodPost, Path: "/v1/careers/{slug}/applications", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Apply for a role on a careers page",
		Description: "PRD-002 NM-REC-04, NM-REC-05. With no sign-in: a person applies for a published role with their name, email, CV as text and, separately, whether they consent to be kept for future roles. Applying is deemed consent to assessing the application. An address and a post that name no open role answer applied false, alike. At most 5 applications from one email address to one company in an hour and 600 reads and applications in all in a minute, counted across every API instance (NM_RATE_LIMITED). It is on the never-list for the AI and MCP surfaces.", Errors: []int{422, 429, 503},
	}, func(ctx context.Context, in *applyInput) (*roleApplicationOutput, error) {
		if a.d.Pipeline == nil || a.d.Careers == nil {
			return nil, needs()
		}
		post, err := uuid.Parse(in.Body.Post)
		if err != nil {
			return nil, Err(CodeInvalid, "The post is the ID the careers page gave.")
		}
		for _, lim := range []struct {
			scope, key string
			window     time.Duration
			n          int
		}{{"careers-apply", in.Slug + "|" + strings.ToLower(strings.TrimSpace(in.Body.Email)), time.Hour, rateApply}, {"careers-public", "all", time.Minute, rateCareers}} {
			ok, err := a.d.Careers.Allow(ctx, lim.scope, lim.key, lim.window, lim.n)
			if err != nil {
				return nil, Err(CodeUnavailable, "The application could not be recorded just now. Try again in a while.")
			}
			if !ok {
				return nil, Err("NM_RATE_LIMITED", "Too many applications just now. Wait a while and try again.")
			}
		}
		done, err := a.d.Pipeline.Apply(ctx, in.Slug, post, in.Body.CandidateInput)
		if err != nil {
			return nil, hiringError(err)
		}
		out := &roleApplicationOutput{}
		out.Body.Applied = done
		return out, nil
	})
}
