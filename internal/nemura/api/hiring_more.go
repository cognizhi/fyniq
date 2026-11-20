package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// What NM-REC-04 and NM-REC-05 left (PRD-002 rev 0.74): the line manager's own roles, the interviewer's own page and the list of interviewers to assign, and a candidate's own look at their data. The line
// manager reads only the requisitions that name them, as counts by stage (the database returns nothing else); an interviewer reads only the candidates assigned to them; the candidate has no sign-in, so the
// hiring team makes them a link (an access request under the PDPA) that shows what they gave and where they stand, never the team's notes or scores, and lets them withdraw the consent to be kept for
// future roles. None of it is on the AI or MCP surface.

// StageCounts is how many candidates stand at each stage of a role, with no name.
type StageCounts struct {
	Applied        int `json:"applied" doc:"Applied." example:"4"`
	Screening      int `json:"screening" doc:"In screening." example:"2"`
	Interview      int `json:"interview" doc:"At interview." example:"1"`
	Offer          int `json:"offer" doc:"With an offer." example:"0"`
	Hired          int `json:"hired" doc:"Hired." example:"0"`
	NotProgressing int `json:"not_progressing" doc:"Not progressing." example:"3"`
}

// HiringRole is a role a line manager is hiring for: where it stands and how many candidates are at each stage, and nothing of the salary or of a candidate.
type HiringRole struct {
	ID          string      `json:"id" format:"uuid" doc:"The requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	Title       string      `json:"title" doc:"The role." example:"Data scientist"`
	Department  string      `json:"department" doc:"The team." example:"Engineering"`
	State       string      `json:"state" enum:"requested,approved,rejected,filled,cancelled" doc:"Where the requisition is." example:"approved"`
	TargetStart string      `json:"target_start" format:"date" doc:"The start date aimed for." example:"2027-02-01"`
	PassNeeded  bool        `json:"pass_needed" doc:"An Employment Pass or S Pass application is expected." example:"false"`
	PostState   string      `json:"post_state,omitempty" enum:"draft,published,closed" doc:"Where its job post is, once one is written." example:"published"`
	PublishedOn string      `json:"published_on,omitempty" format:"date" doc:"The day the post was published." example:"2026-11-02"`
	ClosedOn    string      `json:"closed_on,omitempty" format:"date" doc:"The day the post was closed." example:"2026-12-02"`
	Candidates  StageCounts `json:"candidates" doc:"How many candidates stand at each stage."`
}

// MyInterview is a candidate assigned to the caller, with the criteria to score and those the caller has scored already.
type MyInterview struct {
	CandidateID      string   `json:"candidate_id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Name             string   `json:"name" doc:"The candidate's name." example:"Chen Yu"`
	Stage            string   `json:"stage" enum:"applied,screening,interview,offer,hired,not_progressing" doc:"Where they are on the board." example:"interview"`
	RequisitionID    string   `json:"requisition_id" format:"uuid" doc:"The role's requisition." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"`
	RequisitionTitle string   `json:"requisition_title" doc:"The role." example:"Data scientist"`
	Criteria         []string `json:"criteria" nullable:"false" doc:"The scorecard's criteria; empty until the recruiter saves the kit."`
	Scored           []string `json:"scored" nullable:"false" doc:"The criteria the caller has scored."`
}

// Interviewer is a member who holds the Interviewer role.
type Interviewer struct {
	MemberID string `json:"member_id" format:"uuid" doc:"The member." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1a01"`
	Name     string `json:"name" doc:"Their name." example:"Ian Interviewer"`
}

// CandidateAccessLink is a link made for a candidate to look at their own data; the token is not listed, it was shown once.
type CandidateAccessLink struct {
	ID        string `json:"id" format:"uuid" doc:"The link." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d6a01"`
	CreatedAt string `json:"created_at" format:"date-time" doc:"When it was made." example:"2026-11-20T10:00:00Z"`
	ExpiresAt string `json:"expires_at" format:"date-time" doc:"When it stops working." example:"2026-12-04T10:00:00Z"`
	RevokedAt string `json:"revoked_at,omitempty" format:"date-time" doc:"When it was revoked." example:"2026-11-21T10:00:00Z"`
}

// CandidateAccessCreated is a new link, with its token shown once.
type CandidateAccessCreated struct {
	ID        string `json:"id" format:"uuid" doc:"The link." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d6a01"`
	Token     string `json:"token" doc:"The token, shown once; the candidate opens the careers site's /my-application page and pastes it, or follows a link that carries it after the #." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d0001.xxxxxxxxxx"`
	ExpiresAt string `json:"expires_at" format:"date-time" doc:"When it stops working." example:"2026-12-04T10:00:00Z"`
	Path      string `json:"path" doc:"The page the candidate opens; the token goes after a # so it is never sent in a URL." example:"/my-application"`
}

// CandidateOwnView is what a candidate sees of themselves through a live link: what they gave and where they stand. The team's notes and scores are never shown.
type CandidateOwnView struct {
	Found          bool   `json:"found" doc:"False when the link is unknown, expired or revoked, all alike." example:"true"`
	Name           string `json:"name,omitempty" doc:"The name they gave." example:"Chen Yu"`
	Email          string `json:"email,omitempty" doc:"The email address they gave." example:"chen.yu@example.com"`
	CV             string `json:"cv,omitempty" doc:"The CV they gave, as text." example:"Five years in FP&A."`
	Role           string `json:"role,omitempty" doc:"The role they applied for." example:"Data scientist"`
	Stage          string `json:"stage,omitempty" enum:"applied,screening,interview,offer,hired,not_progressing" doc:"Where they are." example:"interview"`
	StageAt        string `json:"stage_at,omitempty" format:"date-time" doc:"When that last changed." example:"2026-11-02T10:00:00Z"`
	AppliedAt      string `json:"applied_at,omitempty" format:"date-time" doc:"When they applied." example:"2026-11-01T10:00:00Z"`
	AppliedVia     string `json:"applied_via,omitempty" enum:"careers_page,recruiter" doc:"How they came." example:"careers_page"`
	ConsentFuture  bool   `json:"consent_future" doc:"They consent to be kept for future roles, and have not withdrawn it." example:"false"`
	RetentionUntil string `json:"retention_until,omitempty" format:"date" doc:"The day their data is due for deletion, six months after the decision (or, for a hire, after the start date); empty while they are progressing or have consented to be kept." example:"2027-05-02"`
	LinkExpiresAt  string `json:"link_expires_at,omitempty" format:"date-time" doc:"When this link stops working." example:"2026-12-04T10:00:00Z"`
	Withdrawn      bool   `json:"consent_withdrawn" doc:"The consent to be kept for future roles was withdrawn." example:"false"`
}

type hiringRolesOutput struct {
	Body struct {
		Roles []HiringRole `json:"roles" nullable:"false" doc:"The requisitions that name the caller as hiring manager, the newest first."`
	}
}
type myInterviewsOutput struct {
	Body struct {
		Interviews []MyInterview `json:"interviews" nullable:"false" doc:"The candidates assigned to the caller, the latest stage change first."`
	}
}
type interviewersOutput struct {
	Body struct {
		Interviewers []Interviewer `json:"interviewers" nullable:"false" doc:"The members who hold the Interviewer role today."`
	}
}
type candidateAccessLinksOutput struct {
	Body struct {
		Links []CandidateAccessLink `json:"links" nullable:"false" doc:"The links made for the candidate, the newest first, with no token."`
	}
}
type candidateAccessCreateInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The candidate." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d4a01"`
	Body   struct {
		Days int `json:"days,omitempty" minimum:"1" maximum:"30" doc:"How many days the link works: 1 to 30, 14 by default." example:"14"`
	}
}
type candidateAccessCreatedOutput struct{ Body CandidateAccessCreated }
type candidateAccessRevokeInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The link." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d6a01"`
}
type candidateAccessRevokedOutput struct {
	Body struct {
		Revoked bool `json:"revoked" doc:"The link no longer works." example:"true"`
	}
}
type candidateOwnInput struct {
	Body struct {
		Token string `json:"token" minLength:"8" maxLength:"200" doc:"The token of the link the hiring team gave the candidate. It is in the body so it is never in a URL or a log." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d0001.xxxxxxxxxx"`
	}
}
type candidateOwnOutput struct{ Body CandidateOwnView }

func init() {
	IdempotencyExempt["POST /v1/candidates/{id}/access-links"] = "making a second link for a candidate is harmless: each has its own token, shown once, and the team revokes any of them"
	IdempotencyExempt["POST /v1/candidate-access-links/{id}/revocation"] = "a link is revoked once: a second revocation is refused (NM_HIRING_CONFLICT)"
	IdempotencyExempt["POST /v1/candidate-data"] = "a look-up changes nothing the caller can see (it is audited and counted against the rate limit)"
	IdempotencyExempt["POST /v1/candidate-data/consent-withdrawal"] = "a consent is withdrawn once: a second withdrawal is refused (NM_HIRING_CONFLICT)"
}

// allowCandidatePublic counts a candidate's look-up against its own token's limit and the limit of all of them, both shared by every API instance. A limit it cannot count fails closed.
func (a *API) allowCandidatePublic(ctx context.Context, key string) error {
	if a.d.LetterVerify == nil {
		return Err(CodeUnavailable, "Candidate look-ups are not set up in this deployment.")
	}
	one, err := a.d.LetterVerify.Allow(ctx, "candidate-token", key, time.Hour, rateToken)
	if err != nil {
		return Err(CodeUnavailable, "Your application could not be looked up just now. Try again in a while.")
	}
	all, err := a.d.LetterVerify.Allow(ctx, "candidate-public", "all", time.Minute, rateAll)
	if err != nil {
		return Err(CodeUnavailable, "Your application could not be looked up just now. Try again in a while.")
	}
	if !one || !all {
		return Err("NM_RATE_LIMITED", "Too many tries just now. Wait a while and try again with the token exactly as the hiring team gave it.")
	}
	return nil
}

func (a *API) registerHiringMore(seen, writer, interviewer, lineManager func(context.Context, string) (Session, error)) {
	needs := func() error { return Err(CodeUnavailable, "Hiring is not set up in this deployment.") }
	huma.Register(a.API, huma.Operation{
		OperationID: "listMyHiringRoles", Method: http.MethodGet, Path: "/v1/me/hiring-roles", Tags: []string{"Hiring"}, Summary: "The roles you are hiring for",
		Description: "PRD-002 NM-REC-01 (rev 0.74). A line manager's own roles: the requisitions that name them as hiring manager, each with where it stands, its post's dates and how many candidates are at each stage. It shows no salary " +
			"and no candidate: the database returns nothing else. A line manager, or the hiring team. It changes nothing and is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*hiringRolesOutput, error) {
		s, err := lineManager(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		l, err := a.d.Hiring.MyRoles(ctx, s)
		if err != nil {
			return nil, hiringError(err)
		}
		out := &hiringRolesOutput{}
		out.Body.Roles = append([]HiringRole{}, l...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listMyInterviews", Method: http.MethodGet, Path: "/v1/me/interviews", Tags: []string{"Hiring"}, Summary: "The candidates assigned to you to interview",
		Description: "PRD-002 NM-REC-04 (rev 0.74). An interviewer's own page: the candidates assigned to them, with the role, the stage, the scorecard's criteria and the criteria they have scored. The database returns only the candidates " +
			"assigned to the caller. An interviewer. It changes nothing and is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*myInterviewsOutput, error) {
		s, err := interviewer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		l, err := a.d.Pipeline.MyInterviews(ctx, s)
		if err != nil {
			return nil, hiringError(err)
		}
		out := &myInterviewsOutput{}
		out.Body.Interviews = append([]MyInterview{}, l...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listInterviewers", Method: http.MethodGet, Path: "/v1/interviewers", Tags: []string{"Hiring"}, Summary: "The members who can be assigned to interview",
		Description: "PRD-002 NM-REC-04 (rev 0.74). The members who hold the Interviewer role today, for the page that assigns them to a candidate. The Recruiter, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *selfInput) (*interviewersOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		l, err := a.d.Pipeline.Interviewers(ctx, s)
		if err != nil {
			return nil, hiringError(err)
		}
		out := &interviewersOutput{}
		out.Body.Interviewers = append([]Interviewer{}, l...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "createCandidateAccessLink", Method: http.MethodPost, Path: "/v1/candidates/{id}/access-links", Tags: []string{"Hiring"}, DefaultStatus: http.StatusCreated, Summary: "Make a link for a candidate to look at their own data",
		Description: "PRD-002 NM-REC-05 (rev 0.74): an access request under the PDPA. The hiring team makes a link that works for 1 to 30 days (14 by default); the token is shown once and only its hash is kept. With it the candidate sees what they " +
			"gave and where they stand, never the team's notes or scores, and can withdraw the consent to be kept for future roles. The Recruiter, the HR administrator and the Owner. It is audited and is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *candidateAccessCreateInput) (*candidateAccessCreatedOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such candidate.")
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		c, err := a.d.Pipeline.CreateAccess(ctx, s, u, in.Body.Days)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateAccessCreatedOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listCandidateAccessLinks", Method: http.MethodGet, Path: "/v1/candidates/{id}/access-links", Tags: []string{"Hiring"}, Summary: "The links made for a candidate",
		Description: "PRD-002 NM-REC-05 (rev 0.74). The links made for the candidate, the newest first, with when each expires or was revoked and never its token. The Recruiter, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *candPath) (*candidateAccessLinksOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such candidate.")
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		l, err := a.d.Pipeline.AccessLinks(ctx, s, u)
		if err != nil {
			return nil, hiringError(err)
		}
		out := &candidateAccessLinksOutput{}
		out.Body.Links = append([]CandidateAccessLink{}, l...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "revokeCandidateAccessLink", Method: http.MethodPost, Path: "/v1/candidate-access-links/{id}/revocation", Tags: []string{"Hiring"}, Summary: "Revoke a candidate's access link",
		Description: "PRD-002 NM-REC-05 (rev 0.74). A link stops working at once; it is revoked once. The Recruiter, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *candidateAccessRevokeInput) (*candidateAccessRevokedOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such link.")
		}
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		if err := a.d.Pipeline.RevokeAccess(ctx, s, u); err != nil {
			return nil, hiringError(err)
		}
		out := &candidateAccessRevokedOutput{}
		out.Body.Revoked = true
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "readCandidateData", Method: http.MethodPost, Path: "/v1/candidate-data", Tags: []string{"Hiring"}, DefaultStatus: http.StatusOK, Summary: "Read what a candidate's access link shows",
		Description: "PRD-002 NM-REC-05 (rev 0.74). With no sign-in: the token of a link the hiring team gave a candidate. It shows what they gave, the role, where they stand and whether they consent to be kept for future roles, never the team's " +
			"notes or scores; a link that is unknown, expired or revoked answers found false, all alike. The token is in the body so it is never in a URL or a log. Counted against shared limits (NM_RATE_LIMITED); every look-up is audited without the data. " +
			"It is on the never-list for the AI and MCP surfaces.", Errors: []int{422, 429, 503},
	}, func(ctx context.Context, in *candidateOwnInput) (*candidateOwnOutput, error) {
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		token := strings.TrimSpace(in.Body.Token)
		if err := a.allowCandidatePublic(ctx, token); err != nil {
			return nil, err
		}
		v, err := a.d.Pipeline.OwnData(ctx, token)
		if err != nil {
			return nil, huma.Error500InternalServerError("The link could not be looked up.")
		}
		return &candidateOwnOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "withdrawCandidateConsent", Method: http.MethodPost, Path: "/v1/candidate-data/consent-withdrawal", Tags: []string{"Hiring"}, DefaultStatus: http.StatusOK, Summary: "Withdraw the consent to be kept for future roles, as the candidate",
		Description: "PRD-002 NM-REC-05 (rev 0.74). With no sign-in: a candidate with a live link withdraws their consent to be kept for future roles, once and only after it was given. Their data is then listed for deletion six months from " +
			"the withdrawal if they are not progressing. Counted against the shared limits; audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{404, 409, 422, 429, 503},
	}, func(ctx context.Context, in *candidateOwnInput) (*candidateOwnOutput, error) {
		if a.d.Pipeline == nil {
			return nil, needs()
		}
		token := strings.TrimSpace(in.Body.Token)
		if err := a.allowCandidatePublic(ctx, token); err != nil {
			return nil, err
		}
		v, err := a.d.Pipeline.OwnWithdraw(ctx, token)
		if err != nil {
			return nil, hiringError(err)
		}
		return &candidateOwnOutput{Body: v}, nil
	})
}
