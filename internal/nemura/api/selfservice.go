package api

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/selfservice"
)

// Problem codes of self-service (the catalogue is in problem.go).
const (
	CodeRequestInvalid  = selfservice.CodeInvalid
	CodeRequestConflict = selfservice.CodeConflict
)

// EmergencyContact is someone to call about a person.
type EmergencyContact struct {
	Name         string `json:"name" minLength:"1" maxLength:"120" doc:"Their name." example:"Mei Lim"`
	Relationship string `json:"relationship" minLength:"1" maxLength:"60" doc:"How they are related." example:"spouse"`
	Phone        string `json:"phone" minLength:"3" maxLength:"30" doc:"Their phone number." example:"+65 9123 4567"`
}

// MyDetails are a person's own details.
type MyDetails struct {
	Phone         string             `json:"phone,omitempty" maxLength:"30" doc:"Your phone number." example:"+65 9123 4567"`
	PersonalEmail string             `json:"personal_email,omitempty" maxLength:"200" doc:"Your personal email." example:"ben@example.com"`
	Address       string             `json:"address,omitempty" maxLength:"300" doc:"Your home address." example:"1 Raffles Place #20-01"`
	PostalCode    string             `json:"postal_code,omitempty" maxLength:"12" doc:"Its postal code." example:"048616"`
	Emergency     []EmergencyContact `json:"emergency_contacts" maxItems:"3" doc:"Up to three people to call, in order."`
}

// ChangeEvidence is the file that backs a change of details.
type ChangeEvidence struct {
	Title         string `json:"title" minLength:"1" maxLength:"200" doc:"What the file is." example:"Deed poll"`
	ContentType   string `json:"content_type" doc:"The file's type." example:"application/pdf"`
	ContentBase64 string `json:"content_base64" doc:"The file, base64-encoded; up to 5 MB." example:"JVBERi0x"`
}

// DetailsChange asks to change what needs evidence and approval. It holds no NRIC or FIN.
type DetailsChange struct {
	Kind      string         `json:"kind" enum:"legal_name,residency,nric_fin" doc:"What changes." example:"legal_name"`
	LegalName string         `json:"legal_name,omitempty" maxLength:"200" doc:"The new legal name, for a legal-name change." example:"Ben Lim Wei"`
	Residency string         `json:"residency,omitempty" enum:"citizen,permanent_resident,foreigner" doc:"The new residency, for a residency change." example:"permanent_resident"`
	PRStart   string         `json:"pr_start,omitempty" format:"date" doc:"The day permanent residence began, for a permanent resident." example:"2024-03-01"`
	Note      string         `json:"note" minLength:"1" maxLength:"500" doc:"What changed and why. Never an NRIC or FIN number." example:"My FIN became an NRIC when I became a citizen."`
	Evidence  ChangeEvidence `json:"evidence" doc:"The evidence the people team checks."`
}

// RequestRow is one request on a person's list.
type RequestRow struct {
	ID     string `json:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
	Kind   string `json:"kind" enum:"letter,flexible_work,details_change,question" doc:"What kind of request it is." example:"letter"`
	Title  string `json:"title" doc:"What it is about: the letter type, the arrangement, the kind of change, or question." example:"employment_confirmation"`
	State  string `json:"state" doc:"Where it is in its own states." example:"requested"`
	Step   string `json:"step" enum:"received,with_hr,ready,closed" doc:"Where it is for the person: received, with HR, ready, or closed." example:"received"`
	Asked  string `json:"asked_at" format:"date-time" doc:"When it was made." example:"2026-11-20T10:00:00Z"`
	Due    string `json:"due_on" format:"date" doc:"The day it is due." example:"2026-11-25"`
	Reply  string `json:"reply,omitempty" doc:"The reply, or the reason it was refused." example:"Thank you: the new address is on your record."`
	Detail string `json:"detail,omitempty" doc:"What the person wrote." example:"A bank loan."`
}

// QueueRow is a request that waits for the people team.
type QueueRow struct {
	RequestRow
	PersonID   string `json:"person_id" format:"uuid" doc:"The person who asked." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person     string `json:"person" doc:"Their legal name." example:"Ben Lim"`
	ChangeKind string `json:"change_kind,omitempty" doc:"For a change of details, what changes." example:"legal_name"`
	EvidenceID string `json:"evidence_id,omitempty" format:"uuid" doc:"For a change of details, the evidence document on the person's file." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef9"`
}

// SelfService is a person's own details and requests behind the API (NM-ESS-03, NM-ESS-04).
type SelfService interface {
	Details(ctx context.Context, s Session) (MyDetails, error)
	SetDetails(ctx context.Context, s Session, d MyDetails) error
	SetEmergency(ctx context.Context, s Session, list []EmergencyContact) error
	RequestChange(ctx context.Context, s Session, c DetailsChange, content []byte) (id, due string, err error)
	Ask(ctx context.Context, s Session, question string) (id, due string, err error)
	Requests(ctx context.Context, s Session) ([]RequestRow, error)
	Withdraw(ctx context.Context, s Session, id uuid.UUID) error
	Queue(ctx context.Context, s Session) ([]QueueRow, error)
	Decide(ctx context.Context, s Session, id uuid.UUID, approve bool, reason string) error
	Reply(ctx context.Context, s Session, id uuid.UUID, reply string) error
}

func init() {
}

const maxEvidence = 5 << 20

func selfError(err error) error {
	var r *selfservice.Refusal
	var p *people.Refusal
	switch {
	case errors.As(err, &r):
		return Err(r.Code, r.Message)
	case errors.As(err, &p):
		return Err(p.Code, p.Message)
	case errors.Is(err, ErrUnavailable):
		return Err(CodeUnavailable, "This deployment has no file store for evidence yet: ask whoever runs Nemura.")
	}
	return huma.Error500InternalServerError("The request could not be handled.")
}

func selfErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return selfError(err)
}

type selfInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type detailsOutput struct{ Body MyDetails }

type detailsSetInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Phone         string `json:"phone,omitempty" maxLength:"30" doc:"Your phone number; empty clears it." example:"+65 9123 4567"`
		PersonalEmail string `json:"personal_email,omitempty" maxLength:"200" doc:"Your personal email; empty clears it." example:"ben@example.com"`
		Address       string `json:"address,omitempty" maxLength:"300" doc:"Your home address; empty clears it." example:"1 Raffles Place #20-01"`
		PostalCode    string `json:"postal_code,omitempty" maxLength:"12" doc:"Its postal code; empty clears it." example:"048616"`
	}
}

type emergencySetInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Contacts []EmergencyContact `json:"contacts" maxItems:"3" doc:"The whole list, in order; it replaces the one before."`
	}
}

type changeInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   DetailsChange
}

type questionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Question string `json:"question" minLength:"1" maxLength:"1000" doc:"What you want to ask the people team." example:"How many days of annual leave do I get next year?"`
	}
}

type requestsOutput struct {
	Body struct {
		Requests []RequestRow `json:"requests" doc:"The person's requests, newest first."`
	}
}

type selfRequestIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
}

type queueOutput struct {
	Body struct {
		Queue []QueueRow `json:"queue" doc:"What waits for the people team, soonest due first."`
	}
}

type detailsDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
	Body   struct {
		Approve bool   `json:"approve" doc:"Approve or refuse." example:"true"`
		Reason  string `json:"reason,omitempty" maxLength:"500" doc:"Why it is refused; required to refuse." example:"The deed poll is not legible: attach a clearer copy."`
	}
}

type replyInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The question." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
	Body   struct {
		Reply string `json:"reply" minLength:"1" maxLength:"2000" doc:"The answer." example:"You get 8 days from your second year."`
	}
}

func (a *API) registerSelfService() {
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
			return s, Err(CodeRoleRequired, "Only the HR officer, the HR administrator and the Owner handle requests.")
		}
		return s, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "getMyDetails", Method: "GET", Path: "/v1/me/details", Tags: []string{"Self-service"}, Summary: "My contact details and emergency contacts",
		Description: "The signed-in person's phone, personal email, address and up to three emergency contacts. It changes nothing.", Errors: []int{401, 404},
	}, func(ctx context.Context, in *selfInput) (*detailsOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		d, err := a.d.SelfService.Details(ctx, s)
		if err != nil {
			return nil, selfError(err)
		}
		if d.Emergency == nil {
			d.Emergency = []EmergencyContact{}
		}
		return &detailsOutput{Body: d}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setMyDetails", Method: "PUT", Path: "/v1/me/details", Tags: []string{"Self-service"}, Summary: "Change my contact details",
		Description: "Saves the signed-in person's phone, personal email, address and postal code at once. It is audited: the event names the fields that changed, never their values.", Errors: []int{401, 404, 422},
	}, func(ctx context.Context, in *detailsSetInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		return nil, selfErrorOrNil(a.d.SelfService.SetDetails(ctx, s, MyDetails{Phone: in.Body.Phone, PersonalEmail: in.Body.PersonalEmail, Address: in.Body.Address, PostalCode: in.Body.PostalCode}))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setMyEmergencyContacts", Method: "PUT", Path: "/v1/me/emergency-contacts", Tags: []string{"Self-service"}, Summary: "Change my emergency contacts",
		Description: "Replaces the signed-in person's emergency contacts with the list given, up to three, in order. It is audited: the event says how many there were and are, never who.", Errors: []int{401, 404, 422},
	}, func(ctx context.Context, in *emergencySetInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		return nil, selfErrorOrNil(a.d.SelfService.SetEmergency(ctx, s, in.Body.Contacts))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "requestDetailsChange", Method: "POST", Path: "/v1/me/details-changes", Tags: []string{"Self-service"}, Summary: "Ask to change my legal name, residency or NRIC or FIN",
		Description: "Asks the people team to change what needs evidence and approval: a legal name, a residency (with the day residence began for a permanent resident) or an NRIC or FIN. The request carries the evidence, up to 5 MB, and the note says what changed; it never carries an NRIC or FIN number. Another member of the people team approves or refuses it within five working days. It is audited.", Errors: []int{401, 404, 413, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *changeInput) (*letterAskOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		content, err := base64.StdEncoding.DecodeString(in.Body.Evidence.ContentBase64)
		if err != nil || len(content) == 0 {
			return nil, Err(CodeRequestInvalid, "The evidence is not a base64 file: attach the document that backs the change.")
		}
		if len(content) > maxEvidence {
			return nil, Err(CodeRequestInvalid, "The evidence is larger than 5 MB: attach a smaller file.")
		}
		id, due, err := a.d.SelfService.RequestChange(ctx, s, in.Body, content)
		if err != nil {
			return nil, selfError(err)
		}
		out := &letterAskOutput{}
		out.Body.ID, out.Body.Due = id, due
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "askHRQuestion", Method: "POST", Path: "/v1/me/questions", Tags: []string{"Self-service"}, Summary: "Ask the people team a question",
		Description: "Asks the people team a question in writing. Another member of the people team answers it within three working days, and the answer shows on the person's requests. It is audited.", Errors: []int{401, 404, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *questionInput) (*letterAskOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, due, err := a.d.SelfService.Ask(ctx, s, in.Body.Question)
		if err != nil {
			return nil, selfError(err)
		}
		out := &letterAskOutput{}
		out.Body.ID, out.Body.Due = id, due
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listMyRequests", Method: "GET", Path: "/v1/me/requests", Tags: []string{"Self-service"}, Summary: "All my requests and where each is",
		Description: "The signed-in person's letters, flexible work requests, changes of details and questions, newest first, each with its step (received, with HR, ready, or closed), its due day and the reply. It changes nothing.", Errors: []int{401, 404},
	}, func(ctx context.Context, in *selfInput) (*requestsOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.SelfService.Requests(ctx, s)
		if err != nil {
			return nil, selfError(err)
		}
		out := &requestsOutput{}
		out.Body.Requests = list
		if list == nil {
			out.Body.Requests = []RequestRow{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "withdrawMyRequest", Method: "POST", Path: "/v1/me/requests/{id}/withdrawal", Tags: []string{"Self-service"}, Summary: "Withdraw a request of mine",
		Description: "Withdraws a change of details or a question of the signed-in person that nobody has decided or answered. A letter is withdrawn on its own operation. It is audited.", Errors: []int{401, 404}, DefaultStatus: 204,
	}, func(ctx context.Context, in *selfRequestIDInput) (*struct{}, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such request of yours that is still waiting.")
		}
		return nil, selfErrorOrNil(a.d.SelfService.Withdraw(ctx, s, id))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listRequestQueue", Method: "GET", Path: "/v1/requests/queue", Tags: []string{"Self-service"}, Summary: "What waits for the people team",
		Description: "Changes of details and questions that nobody has decided or answered, soonest due first, with the person's name and, for a change, the evidence document on their file. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *selfInput) (*queueOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.SelfService.Queue(ctx, s)
		if err != nil {
			return nil, selfError(err)
		}
		out := &queueOutput{}
		out.Body.Queue = list
		if list == nil {
			out.Body.Queue = []QueueRow{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideDetailsChange", Method: "POST", Path: "/v1/details-changes/{id}/decision", Tags: []string{"Self-service"}, Summary: "Approve or refuse a change of details",
		Description: "Approves or refuses a change of details after reading its evidence. Nobody decides a change to their own details. Approving a legal name or a residency applies it to the person's record from the day of the decision; approving a change of NRIC or FIN records that the evidence was checked, and the new number is then recorded through the restricted fields. Refusing needs a reason. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 204,
	}, func(ctx context.Context, in *detailsDecisionInput) (*struct{}, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such request.")
		}
		return nil, selfErrorOrNil(a.d.SelfService.Decide(ctx, s, id, in.Body.Approve, in.Body.Reason))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "replyToQuestion", Method: "POST", Path: "/v1/questions/{id}/reply", Tags: []string{"Self-service"}, Summary: "Answer a question",
		Description: "Answers a person's question in writing, once. Nobody answers their own question. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 204,
	}, func(ctx context.Context, in *replyInput) (*struct{}, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such question.")
		}
		return nil, selfErrorOrNil(a.d.SelfService.Reply(ctx, s, id, in.Body.Reply))
	})
}
