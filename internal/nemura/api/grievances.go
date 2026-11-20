package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/grievance"
)

// Grievances (PRD-002 NM-SEC-06): a confidential channel. Any member files one, optionally naming the people it is about; it is routed to an HR administrator or the Owner who is neither the
// complainant nor anyone it is about, who keeps an inquiry record and gives a written outcome. The people it is about can never read it: the database's row-level policies hold that, and every
// operation here runs for a named acting person so a missed check cannot read more. Nothing here is on the AI or MCP surface and the assistant holds no grant on the tables.

// GrievanceFiled is what the complainant is told on filing.
type GrievanceFiled struct {
	ID      string `json:"id" format:"uuid" doc:"The grievance." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"`
	State   string `json:"state" enum:"open,unrouted" doc:"open: someone is handling it; unrouted: nobody who is not involved can handle it yet, and it waits." example:"open"`
	Handler string `json:"handler,omitempty" doc:"The name of the person handling it, when it has one." example:"Sarah Koh"`
}

// GrievanceMine is a grievance as its complainant sees it: its state and the written outcome, not the inquiry record.
type GrievanceMine struct {
	ID        string `json:"id" format:"uuid" doc:"The grievance." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"`
	FiledAt   string `json:"filed_at" format:"date-time" doc:"When it was filed." example:"2026-11-20T10:00:00Z"`
	State     string `json:"state" enum:"unrouted,open,decided" doc:"unrouted: waiting for someone who is not involved to take it; open: being handled; decided: the outcome is written." example:"open"`
	Handler   string `json:"handler,omitempty" doc:"Who handles it." example:"Sarah Koh"`
	Finding   string `json:"finding,omitempty" enum:"substantiated,partly_substantiated,not_substantiated,no_finding" doc:"What the outcome concludes, once decided." example:"partly_substantiated"`
	Outcome   string `json:"outcome,omitempty" doc:"The written outcome, once decided." example:"The inquiry found…"`
	DecidedAt string `json:"decided_at,omitempty" format:"date-time" doc:"When the outcome was written." example:"2026-12-04T10:00:00Z"`
}

// GrievanceRow is a grievance in a handler's list: by identity, with no words.
type GrievanceRow struct {
	ID      string `json:"id" format:"uuid" doc:"The grievance." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"`
	FiledAt string `json:"filed_at" format:"date-time" doc:"When it was filed." example:"2026-11-20T10:00:00Z"`
	State   string `json:"state" enum:"unrouted,open,decided" doc:"Where it is." example:"open"`
}

// GrievanceSubject is a person a grievance is about.
type GrievanceSubject struct {
	PersonID string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Name     string `json:"name" doc:"Their name." example:"Ben Lim"`
}

// GrievanceEntry is one line of the inquiry record.
type GrievanceEntry struct {
	ID   string `json:"id" format:"uuid" doc:"The entry." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea2"`
	Kind string `json:"kind" enum:"note,interview,finding,handover,routing" doc:"What kind of entry." example:"note"`
	Body string `json:"body" doc:"What was recorded." example:"Spoke with the complainant on the day."`
	By   string `json:"by" doc:"Who recorded it." example:"Sarah Koh"`
	At   string `json:"at" format:"date-time" doc:"When." example:"2026-11-21T10:00:00Z"`
}

// GrievanceDetail is a grievance as its handler reads it, with the inquiry record. Reading it is audited.
type GrievanceDetail struct {
	GrievanceRow
	Complainant string             `json:"complainant" doc:"Who filed it." example:"Nurul Aziz"`
	Summary     string             `json:"summary" doc:"What the complainant wrote." example:"On 12 November…"`
	About       []GrievanceSubject `json:"about" nullable:"false" doc:"The people it is about; empty when it is about a practice."`
	Finding     string             `json:"finding,omitempty" enum:"substantiated,partly_substantiated,not_substantiated,no_finding" doc:"What the outcome concludes." example:"not_substantiated"`
	Outcome     string             `json:"outcome,omitempty" doc:"The written outcome." example:"The inquiry found…"`
	DecidedAt   string             `json:"decided_at,omitempty" format:"date-time" doc:"When it was written." example:"2026-12-04T10:00:00Z"`
	Entries     []GrievanceEntry   `json:"entries" nullable:"false" doc:"The inquiry record, oldest first."`
}

// Grievances is the grievance channel behind the API. Every method runs for the session's member as the acting person.
type Grievances interface {
	File(ctx context.Context, s Session, summary string, about []uuid.UUID) (GrievanceFiled, error)
	Mine(ctx context.Context, s Session) ([]GrievanceMine, error)
	Handled(ctx context.Context, s Session) (assigned, unrouted []GrievanceRow, err error)
	Detail(ctx context.Context, s Session, id uuid.UUID) (GrievanceDetail, error)
	Record(ctx context.Context, s Session, id uuid.UUID, kind, note string) error
	Decide(ctx context.Context, s Session, id uuid.UUID, finding, outcome string) error
	HandOver(ctx context.Context, s Session, id uuid.UUID, reason string) error
	Take(ctx context.Context, s Session, id uuid.UUID) error
}

type grievanceFileInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Summary string   `json:"summary" minLength:"20" maxLength:"4000" doc:"What happened, when and who was there." example:"On 12 November my manager…"`
		About   []string `json:"about,omitempty" maxItems:"5" nullable:"false" items:"{\"format\":\"uuid\"}" doc:"The people it is about, by person ID; leave out when it is about a practice." example:"[\"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61\"]"`
	}
}
type grievanceFiledOutput struct{ Body GrievanceFiled }
type grievanceCookie struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}
type grievanceMineOutput struct {
	Body struct {
		Items []GrievanceMine `json:"items" nullable:"false" doc:"The caller's own grievances, newest first."`
	}
}
type grievanceHandledOutput struct {
	Body struct {
		Assigned []GrievanceRow `json:"assigned" nullable:"false" doc:"The grievances the caller handles, newest first."`
		Unrouted []GrievanceRow `json:"unrouted" nullable:"false" doc:"Grievances nobody who is not involved could handle: the caller, if not involved, may take one."`
	}
}
type grievanceIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The grievance." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"`
}
type grievanceDetailOutput struct{ Body GrievanceDetail }
type grievanceEntryInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The grievance." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"`
	Body   struct {
		Kind string `json:"kind" enum:"note,interview,finding" doc:"What kind of entry." example:"note"`
		Note string `json:"note" minLength:"1" maxLength:"4000" doc:"What to record." example:"Spoke with the complainant on the day."`
	}
}
type grievanceOutcomeInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The grievance." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"`
	Body   struct {
		Finding string `json:"finding" enum:"substantiated,partly_substantiated,not_substantiated,no_finding" doc:"What the inquiry concludes." example:"partly_substantiated"`
		Outcome string `json:"outcome" minLength:"20" maxLength:"4000" doc:"The written outcome, which the complainant reads." example:"The inquiry found…"`
	}
}
type grievanceHandoverInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The grievance." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea1"`
	Body   struct {
		Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is passed on; it goes in the inquiry record." example:"I have a conflict of interest."`
	}
}
type grievanceDoneOutput struct {
	Body struct {
		Done bool `json:"done" doc:"True once it is recorded." example:"true"`
	}
}

func grievanceError(err error) error {
	var r *grievance.Refusal
	if errors.As(err, &r) {
		return Err(r.Code, r.Message)
	}
	return huma.Error500InternalServerError("The grievance could not be handled.")
}

func init() {
	IdempotencyExempt["POST /v1/me/grievances"] = "filing twice makes two grievances, which the complainant can see and the handler can combine in the record"
	IdempotencyExempt["POST /v1/grievances/{id}/entries"] = "an entry is one more line of the inquiry record"
	IdempotencyExempt["POST /v1/grievances/{id}/outcome"] = "an outcome is written once: a second is refused (NM_GRIEVANCE_STATE)"
	IdempotencyExempt["POST /v1/grievances/{id}/handover"] = "a handover is to the next eligible person: a repeat is refused once the caller no longer handles it (NM_NOT_FOUND)"
	IdempotencyExempt["POST /v1/grievances/{id}/taking"] = "a grievance is taken once: a second take is refused (NM_GRIEVANCE_STATE)"
}

const nemuraGrievance = "Grievances (NM-SEC-06)"

func (a *API) registerGrievances() {
	unavailable := func() error { return Err(CodeUnavailable, "The grievance channel is not set up in this deployment.") }
	// handler is the session of a person who may handle grievances: an HR administrator or the Owner. The database decides which grievances they may read.
	handler := func(ctx context.Context, cookie string, right authz.Right) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return Session{}, err
		}
		held, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
		if err != nil {
			return Session{}, huma.Error500InternalServerError("The roles could not be read.")
		}
		for _, h := range held {
			if authz.Can(h, nemuraGrievance, right) && h != authz.Employee {
				if a.d.Grievances == nil {
					return Session{}, unavailable()
				}
				return s, nil
			}
		}
		return Session{}, Err(CodeRoleRequired, "Only an HR administrator or the Owner handles a grievance, and only one they are not involved in.")
	}
	id := func(raw string) (uuid.UUID, error) {
		g, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, Err(CodeNotFound, "There is no such grievance in your view.")
		}
		return g, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "fileGrievance", Method: http.MethodPost, Path: "/v1/me/grievances", Tags: []string{"Grievances"}, DefaultStatus: http.StatusCreated, Summary: "File a grievance",
		Description: "PRD-002 NM-SEC-06. Any member who has not left files a confidential grievance, optionally naming the people it is about. It is routed to an HR administrator or the Owner who is neither the complainant nor anyone it is about; " +
			"when nobody is eligible it waits (state unrouted). The people it is about can never read it. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 422, 503},
	}, func(ctx context.Context, in *grievanceFileInput) (*grievanceFiledOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Grievances == nil {
			return nil, unavailable()
		}
		var about []uuid.UUID
		for _, raw := range in.Body.About {
			p, err := uuid.Parse(raw)
			if err != nil {
				return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
			}
			about = append(about, p)
		}
		v, err := a.d.Grievances.File(ctx, s, in.Body.Summary, about)
		if err != nil {
			return nil, grievanceError(err)
		}
		return &grievanceFiledOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listMyGrievances", Method: http.MethodGet, Path: "/v1/me/grievances", Tags: []string{"Grievances"}, Summary: "The grievances you filed",
		Description: "The caller's own grievances, newest first, with their state, who handles each and, once decided, the written outcome. The inquiry record is never shown to the complainant.", Errors: []int{401, 503},
	}, func(ctx context.Context, in *grievanceCookie) (*grievanceMineOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.Grievances == nil {
			return nil, unavailable()
		}
		items, err := a.d.Grievances.Mine(ctx, s)
		if err != nil {
			return nil, grievanceError(err)
		}
		out := &grievanceMineOutput{}
		out.Body.Items = items
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listGrievances", Method: http.MethodGet, Path: "/v1/grievances", Tags: []string{"Grievances"}, Summary: "The grievances you handle",
		Description: "The grievances assigned to the caller, and the unrouted ones the caller, if not involved, may take: by identity, with no words. Only an HR administrator or the Owner. It changes nothing.", Errors: []int{401, 403, 503},
	}, func(ctx context.Context, in *grievanceCookie) (*grievanceHandledOutput, error) {
		s, err := handler(ctx, in.Cookie, authz.Create)
		if err != nil {
			return nil, err
		}
		assigned, unrouted, err := a.d.Grievances.Handled(ctx, s)
		if err != nil {
			return nil, grievanceError(err)
		}
		out := &grievanceHandledOutput{}
		out.Body.Assigned, out.Body.Unrouted = assigned, unrouted
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getGrievance", Method: http.MethodGet, Path: "/v1/grievances/{id}", Tags: []string{"Grievances"}, Summary: "Read a grievance you handle",
		Description: "The grievance with its inquiry record, for its handler or for an authorised person who is not involved and reads an unrouted one. A grievance the caller is not entitled to is not found, whoever they are, " +
			"and a person it is about gets not found whatever role they hold. Reading it is audited.", Errors: []int{401, 403, 404, 503},
	}, func(ctx context.Context, in *grievanceIDInput) (*grievanceDetailOutput, error) {
		s, err := handler(ctx, in.Cookie, authz.Create)
		if err != nil {
			return nil, err
		}
		g, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		d, err := a.d.Grievances.Detail(ctx, s, g)
		if err != nil {
			return nil, grievanceError(err)
		}
		return &grievanceDetailOutput{Body: d}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "recordGrievanceEntry", Method: http.MethodPost, Path: "/v1/grievances/{id}/entries", Tags: []string{"Grievances"}, DefaultStatus: http.StatusCreated, Summary: "Add to the inquiry record",
		Description: "The handler records a note, an interview or a finding in the inquiry. Entries are never changed or removed. Audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *grievanceEntryInput) (*grievanceDoneOutput, error) {
		s, err := handler(ctx, in.Cookie, authz.Create)
		if err != nil {
			return nil, err
		}
		g, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.d.Grievances.Record(ctx, s, g, in.Body.Kind, in.Body.Note); err != nil {
			return nil, grievanceError(err)
		}
		out := &grievanceDoneOutput{}
		out.Body.Done = true
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideGrievance", Method: http.MethodPost, Path: "/v1/grievances/{id}/outcome", Tags: []string{"Grievances"}, DefaultStatus: http.StatusOK, Summary: "Write the outcome of a grievance",
		Description: "The handler, an HR administrator or the Owner, writes what the inquiry concludes and the outcome in words; the complainant then reads it. Once only. Audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *grievanceOutcomeInput) (*grievanceDoneOutput, error) {
		s, err := handler(ctx, in.Cookie, authz.Approve)
		if err != nil {
			return nil, err
		}
		g, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.d.Grievances.Decide(ctx, s, g, in.Body.Finding, in.Body.Outcome); err != nil {
			return nil, grievanceError(err)
		}
		out := &grievanceDoneOutput{}
		out.Body.Done = true
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "handOverGrievance", Method: http.MethodPost, Path: "/v1/grievances/{id}/handover", Tags: []string{"Grievances"}, DefaultStatus: http.StatusOK, Summary: "Pass a grievance to someone else",
		Description: "The handler passes it to the next eligible person (an HR administrator or the Owner who is neither the complainant nor anyone it is about, and not the caller), with a reason that goes in the inquiry record. " +
			"When nobody else is eligible it is refused (NM_GRIEVANCE_NO_HANDLER). Audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *grievanceHandoverInput) (*grievanceDoneOutput, error) {
		s, err := handler(ctx, in.Cookie, authz.Create)
		if err != nil {
			return nil, err
		}
		g, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.d.Grievances.HandOver(ctx, s, g, in.Body.Reason); err != nil {
			return nil, grievanceError(err)
		}
		out := &grievanceDoneOutput{}
		out.Body.Done = true
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "takeGrievance", Method: http.MethodPost, Path: "/v1/grievances/{id}/taking", Tags: []string{"Grievances"}, DefaultStatus: http.StatusOK, Summary: "Take an unrouted grievance",
		Description: "An HR administrator or the Owner who is neither the complainant nor anyone it is about takes a grievance nobody could handle at filing, and becomes its handler. Audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *grievanceIDInput) (*grievanceDoneOutput, error) {
		s, err := handler(ctx, in.Cookie, authz.Create)
		if err != nil {
			return nil, err
		}
		g, err := id(in.ID)
		if err != nil {
			return nil, err
		}
		if err := a.d.Grievances.Take(ctx, s, g); err != nil {
			return nil, grievanceError(err)
		}
		out := &grievanceDoneOutput{}
		out.Body.Done = true
		return out, nil
	})
}
