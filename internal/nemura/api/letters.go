package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
)

// Problem codes of letters (the catalogue is in problem.go).
const (
	CodeLetterInvalid  = letters.CodeInvalid
	CodeLetterConflict = letters.CodeConflict
)

// LetterField is a merge field of the catalogue.
type LetterField struct {
	Key    string `json:"key" doc:"The field as a template writes it, between double braces." example:"person.legal_name"`
	Label  string `json:"label" doc:"What it is called." example:"Legal name"`
	Format string `json:"format" enum:"text,date" doc:"How its value is written." example:"text"`
}

// LetterType is a type of the catalogue with the company's setting on it.
type LetterType struct {
	Key       string   `json:"key" doc:"The type's key." example:"employment_confirmation"`
	Title     string   `json:"title" doc:"Its name." example:"Employment confirmation"`
	Statement string   `json:"statement" doc:"What the letter says, in a sentence." example:"Confirms that the person works for the company."`
	Enabled   bool     `json:"enabled" doc:"Whether the company issues it." example:"true"`
	DueDays   int      `json:"due_days" doc:"Working days to issue it." example:"3"`
	Version   int      `json:"published_version" doc:"The published template version; 0 when none." example:"1"`
	Needs     string   `json:"needs,omitempty" doc:"What the type waits for before it can be switched on: salary or terms." example:"salary"`
	Fields    []string `json:"fields" doc:"The merge fields a template of this type may use."`
}

// LetterBlock is a block of a template.
type LetterBlock struct {
	Kind string `json:"kind" enum:"letterhead,heading,paragraph,signature" doc:"The block's kind." example:"paragraph"`
	Text string `json:"text" maxLength:"2000" doc:"Its text, with {{field}} placeholders from the catalogue." example:"This confirms that {{person.legal_name}} works for {{company.name}}."`
}

// LetterTemplate is a template version.
type LetterTemplate struct {
	ID      string        `json:"id" format:"uuid" doc:"The version." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ee0"`
	Type    string        `json:"type" doc:"The letter type." example:"employment_confirmation"`
	Version int           `json:"version" doc:"Its number once published; 0 for a draft." example:"1"`
	State   string        `json:"state" enum:"draft,published,superseded" doc:"A draft, the published version, or one replaced by a newer." example:"published"`
	Note    string        `json:"note,omitempty" doc:"The author's note." example:"First version."`
	Blocks  []LetterBlock `json:"blocks" doc:"The blocks."`
}

// LetterRequest is a request for a letter and where it is.
type LetterRequest struct {
	ID        string `json:"id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
	PersonID  string `json:"person_id" format:"uuid" doc:"The person the letter is about." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person    string `json:"person_name" doc:"Their name." example:"Ben Lim"`
	Type      string `json:"type" doc:"The letter type." example:"employment_confirmation"`
	Purpose   string `json:"purpose" doc:"What the letter is for." example:"A bank loan."`
	State     string `json:"state" enum:"requested,checked,refused,issued,withdrawn" doc:"Received and waiting for HR; checked and waiting for the signatory; issued; refused; or withdrawn." example:"requested"`
	Due       string `json:"due_on" format:"date" doc:"The day it is due." example:"2026-11-25"`
	Requested string `json:"requested_at" format:"date-time" doc:"When it was asked for." example:"2026-11-20T10:00:00Z"`
	Reference string `json:"reference,omitempty" doc:"The letter's reference, given when it is checked." example:"EC-2026-0001"`
	SHA256    string `json:"sha256,omitempty" doc:"The SHA-256 of the exact PDF checked, which the signatory confirms." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Reason    string `json:"decision_reason,omitempty" doc:"Why it was refused." example:"The letter is for a different company."`
	IssuedID  string `json:"issued_id,omitempty" format:"uuid" doc:"The issued letter, once there is one." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f00"`
	IssuedAt  string `json:"issued_at,omitempty" format:"date-time" doc:"When it was issued." example:"2026-11-21T10:00:00Z"`
}

// LetterSettings is the letterhead.
type LetterSettings struct {
	Name    string `json:"company_name" minLength:"1" maxLength:"200" doc:"The company's name." example:"Acme Pte. Ltd."`
	UEN     string `json:"uen" minLength:"1" maxLength:"30" doc:"Its UEN." example:"201912345A"`
	Address string `json:"address" minLength:"1" maxLength:"400" doc:"Its address." example:"1 Raffles Place, Singapore 048616"`
}

// Letters is letters behind the API: the API decides who may call, the service reads and writes in one transaction, draws and keeps the letters, and audits.
type Letters interface {
	Settings(ctx context.Context, s Session) (LetterSettings, bool, error)
	SetSettings(ctx context.Context, s Session, v LetterSettings) error
	Types(ctx context.Context, s Session) ([]LetterType, error)
	SetType(ctx context.Context, s Session, key string, enabled bool, dueDays int) error
	Templates(ctx context.Context, s Session, key string) ([]LetterTemplate, error)
	SaveDraft(ctx context.Context, s Session, key string, blocks []LetterBlock, note string) (string, error)
	Publish(ctx context.Context, s Session, draft uuid.UUID) (int, error)
	Request(ctx context.Context, s Session, typ, purpose string) (id, due string, err error)
	List(ctx context.Context, s Session, all bool, state string) ([]LetterRequest, error)
	Check(ctx context.Context, s Session, id uuid.UUID) (reference, sha string, err error)
	Draft(ctx context.Context, s Session, id uuid.UUID) (reference string, pdf []byte, err error)
	Sign(ctx context.Context, s Session, id uuid.UUID, sha string) (issued string, err error)
	Refuse(ctx context.Context, s Session, id uuid.UUID, reason string, withdraw bool) error
	File(ctx context.Context, s Session, id uuid.UUID, all bool) (reference string, pdf []byte, err error)
	Link(ctx context.Context, s Session, id uuid.UUID, all bool) (token, expires string, err error)
	ByLink(ctx context.Context, token string) (reference string, pdf []byte, err error)
}

func init() {
	IdempotencyExempt["PUT /v1/letters/settings"] = "naturally idempotent: the same body leaves the same letterhead"
	IdempotencyExempt["PUT /v1/letters/types/{key}"] = "naturally idempotent: the same body leaves the same setting"
	IdempotencyExempt["PUT /v1/letters/types/{key}/template"] = "naturally idempotent: the same body leaves the same draft"
	IdempotencyExempt["POST /v1/letters/templates/{id}/publication"] = "a draft is published once: a repeat is refused (NM_NOT_FOUND)"
	IdempotencyExempt["POST /v1/me/letters"] = "each request is its own record with its own date: a person may ask for the same letter twice"
	IdempotencyExempt["POST /v1/letters/{id}/check"] = "a request is checked once: a repeat is refused (NM_LETTER_CONFLICT)"
	IdempotencyExempt["POST /v1/letters/{id}/signature"] = "a letter is issued once: a repeat is refused (NM_LETTER_CONFLICT)"
	IdempotencyExempt["POST /v1/letters/{id}/refusal"] = "a request is closed once: a repeat is refused (NM_LETTER_CONFLICT)"
	IdempotencyExempt["POST /v1/letters/{id}/withdrawal"] = "a request is closed once: a repeat is refused (NM_LETTER_CONFLICT)"
	IdempotencyExempt["POST /v1/letters/{id}/link"] = "each link is its own expiring token"
}

func lettersError(err error) error {
	var l *letters.Refusal
	switch {
	case errors.As(err, &l):
		return Err(l.Code, l.Message)
	case errors.Is(err, ErrUnavailable):
		return Err(CodeUnavailable, "This deployment has no file store or renderer for letters yet: ask whoever runs Nemura.")
	}
	return requestsError(err)
}

// letterStaff reports which of the letter powers the caller's roles give: the people team checks and refuses and writes templates; the HR administrator and the Owner
// approve templates and set the types and the letterhead; only the Owner (or a delegated signatory) signs.
type letterPowers struct{ Team, Admin, Sign bool }

func (a *API) letterPowers(ctx context.Context, s Session) (letterPowers, error) {
	roles, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
	if err != nil {
		return letterPowers{}, huma.Error500InternalServerError("The roles could not be read.")
	}
	var p letterPowers
	for _, r := range roles {
		switch r {
		case authz.HROfficer:
			p.Team = true
		case authz.HRAdmin:
			p.Team, p.Admin = true, true
		case authz.Owner:
			p.Team, p.Admin, p.Sign = true, true, true
		}
	}
	return p, nil
}

type lettersInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type letterKeyInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Key    string `path:"key" pattern:"^[a-z][a-z0-9_]{1,60}$" doc:"The letter type's key." example:"employment_confirmation"`
}

type letterSettingsOutput struct {
	Body struct {
		Set      bool           `json:"set" doc:"Whether a letterhead is set." example:"true"`
		Settings LetterSettings `json:"settings" doc:"The letterhead."`
	}
}

type letterSettingsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   LetterSettings
}

type letterTypesOutput struct {
	Body struct {
		Types  []LetterType  `json:"types" doc:"The catalogue with the company's settings."`
		Fields []LetterField `json:"fields" doc:"The merge fields a template may use."`
	}
}

type letterTypeSetInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Key    string `path:"key" pattern:"^[a-z][a-z0-9_]{1,60}$" doc:"The letter type's key." example:"employment_confirmation"`
	Body   struct {
		Enabled bool `json:"enabled" doc:"Whether the company issues this type." example:"true"`
		DueDays int  `json:"due_days" minimum:"1" maximum:"30" doc:"Working days to issue it." example:"3"`
	}
}

type letterTemplatesOutput struct {
	Body struct {
		Versions []LetterTemplate `json:"versions" doc:"The type's template versions, the open draft first."`
	}
}

type letterDraftInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Key    string `path:"key" pattern:"^[a-z][a-z0-9_]{1,60}$" doc:"The letter type's key." example:"employment_confirmation"`
	Body   struct {
		Blocks []LetterBlock `json:"blocks" minItems:"1" maxItems:"40" doc:"The template's blocks."`
		Note   string        `json:"note,omitempty" maxLength:"500" doc:"A note for the approver." example:"Adds the department."`
	}
}

type letterIDOutput struct {
	Body struct {
		ID string `json:"id" format:"uuid" doc:"The record." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ee0"`
	}
}

type letterPublishInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The draft." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ee0"`
}

type letterPublishOutput struct {
	Body struct {
		Version int `json:"version" doc:"The number it was published as." example:"2"`
	}
}

type letterAskInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Type    string `json:"type" doc:"The letter type." example:"employment_confirmation"`
		Purpose string `json:"purpose" minLength:"1" maxLength:"300" doc:"What the letter is for." example:"A bank loan."`
	}
}

type letterAskOutput struct {
	Body struct {
		ID  string `json:"id" format:"uuid" doc:"The new request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
		Due string `json:"due_on" format:"date" doc:"The day it is due." example:"2026-11-25"`
	}
}

type letterListInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	State  string `query:"state" enum:"requested,checked,refused,issued,withdrawn" doc:"Only requests in this state." example:"checked"`
	Mine   bool   `query:"mine" doc:"Only the caller's own." example:"true"`
}

type letterListOutput struct {
	Body struct {
		Requests []LetterRequest `json:"requests" doc:"The requests the caller may read: their own, or everyone's for the people team and the Owner."`
	}
}

type letterIDInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The letter request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
}

type letterCheckOutput struct {
	Body struct {
		Reference string `json:"reference" doc:"The letter's reference." example:"EC-2026-0001"`
		SHA256    string `json:"sha256" doc:"The SHA-256 of the exact PDF, which the signatory confirms." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	}
}

type letterFileOutput struct {
	Body struct {
		Reference     string `json:"reference" doc:"The letter's reference." example:"EC-2026-0001"`
		ContentType   string `json:"content_type" doc:"Its media type." example:"application/pdf"`
		ContentBase64 string `json:"content_base64" doc:"The PDF, base64."`
	}
}

type letterSignInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The letter request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
	Body   struct {
		SHA256 string `json:"sha256" pattern:"^[0-9a-f]{64}$" doc:"The SHA-256 the signatory saw for the exact PDF and confirms." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	}
}

type letterSignOutput struct {
	Body struct {
		IssuedID string `json:"issued_id" format:"uuid" doc:"The issued letter." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f00"`
	}
}

type letterRefuseInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The letter request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"`
	Body   struct {
		Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why; the person is told." example:"The letter is for a different company."`
	}
}

type letterLinkOutput struct {
	Body struct {
		Token   string `json:"token" doc:"The token, shown once: GET /v1/letter-links/download?token=… delivers the letter until it expires." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44.abc"`
		Expires string `json:"expires_at" format:"date-time" doc:"When the link stops working." example:"2026-11-28T10:00:00Z"`
	}
}

type letterDownloadInput struct {
	Token string `query:"token" doc:"The token of a delivery link." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44.abc"`
}

func (a *API) registerLetters() {
	staff := func(ctx context.Context, cookie string, need func(letterPowers) bool, why string) (Session, letterPowers, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, letterPowers{}, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return s, p, err
		}
		if !need(p) {
			return s, p, Err(CodeRoleRequired, why)
		}
		return s, p, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "getLetterSettings", Method: "GET", Path: "/v1/letters/settings", Tags: []string{"Letters"}, Summary: "The letterhead",
		Description: "The company's name, UEN and address every letter carries. The people team. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *lettersInput) (*letterSettingsOutput, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Team }, "Only the people team and the Owner see the letterhead.")
		if err != nil {
			return nil, err
		}
		v, ok, err := a.d.Letters.Settings(ctx, s)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterSettingsOutput{}
		out.Body.Set, out.Body.Settings = ok, v
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setLetterSettings", Method: "PUT", Path: "/v1/letters/settings", Tags: []string{"Letters"}, Summary: "Set the letterhead",
		Description: "Sets the company's name, UEN and address for every letter. The HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 422},
	}, func(ctx context.Context, in *letterSettingsInput) (*struct{}, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Admin }, "Only the HR administrator and the Owner set the letterhead.")
		if err != nil {
			return nil, err
		}
		return nil, lettersErrorOrNil(a.d.Letters.SetSettings(ctx, s, in.Body))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLetterTypes", Method: "GET", Path: "/v1/letters/types", Tags: []string{"Letters"}, Summary: "The letter types and their settings",
		Description: "The catalogue of letter types with the company's setting on each (switched on, due days, the published template version) and the merge fields a template may use. Anyone signed in sees which types are on, so they can ask for one. It changes nothing.", Errors: []int{401},
	}, func(ctx context.Context, in *lettersInput) (*letterTypesOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		types, err := a.d.Letters.Types(ctx, s)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterTypesOutput{}
		out.Body.Types = types
		for _, f := range letters.Fields {
			out.Body.Fields = append(out.Body.Fields, LetterField{Key: f.Key, Label: f.Label, Format: f.Format})
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setLetterType", Method: "PUT", Path: "/v1/letters/types/{key}", Tags: []string{"Letters"}, Summary: "Switch a letter type on or off",
		Description: "Switches a type of the catalogue on or off for the company and sets how many working days it takes to issue. A type that needs the person's salary or terms cannot be switched on until Nemura holds them. The HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 422},
	}, func(ctx context.Context, in *letterTypeSetInput) (*struct{}, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Admin }, "Only the HR administrator and the Owner switch letter types on or off.")
		if err != nil {
			return nil, err
		}
		return nil, lettersErrorOrNil(a.d.Letters.SetType(ctx, s, in.Key, in.Body.Enabled, in.Body.DueDays))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLetterTemplates", Method: "GET", Path: "/v1/letters/types/{key}/templates", Tags: []string{"Letters"}, Summary: "A type's template versions",
		Description: "The type's template versions, the open draft first, each with its blocks. The people team. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *letterKeyInput) (*letterTemplatesOutput, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Team }, "Only the people team and the Owner see the templates.")
		if err != nil {
			return nil, err
		}
		v, err := a.d.Letters.Templates(ctx, s, in.Key)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterTemplatesOutput{}
		out.Body.Versions = v
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "saveLetterTemplateDraft", Method: "PUT", Path: "/v1/letters/types/{key}/template", Tags: []string{"Letters"}, Summary: "Save a draft of a type's template",
		Description: "Saves the type's open draft (replacing the last), after checking every block and field against the type: every problem is reported at once. A draft changes nothing until another person publishes it. The people team. It is audited.", Errors: []int{401, 403, 404, 422},
		DefaultStatus: 200,
	}, func(ctx context.Context, in *letterDraftInput) (*letterIDOutput, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Team }, "Only the people team and the Owner write templates.")
		if err != nil {
			return nil, err
		}
		id, err := a.d.Letters.SaveDraft(ctx, s, in.Key, in.Body.Blocks, in.Body.Note)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterIDOutput{}
		out.Body.ID = id
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "publishLetterTemplate", Method: "POST", Path: "/v1/letters/templates/{id}/publication", Tags: []string{"Letters"}, Summary: "Publish a template draft",
		Description: "The HR administrator or the Owner approves and publishes a draft as the next numbered version; it is immutable from then on and the version before is superseded. Nobody approves the template they wrote. It is audited.", Errors: []int{401, 403, 404, 409},
	}, func(ctx context.Context, in *letterPublishInput) (*letterPublishOutput, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Admin }, "Only the HR administrator and the Owner approve templates.")
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such draft.")
		}
		v, err := a.d.Letters.Publish(ctx, s, id)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterPublishOutput{}
		out.Body.Version = v
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "requestLetter", Method: "POST", Path: "/v1/me/letters", Tags: []string{"Letters"}, Summary: "Ask for a letter",
		Description: "Asks for a letter about the signed-in person: the published template of a type the company issues is filled from their record and the letterhead, and a fact the record does not hold is refused by name. The people team is told. It is audited.", Errors: []int{401, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *letterAskInput) (*letterAskOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, due, err := a.d.Letters.Request(ctx, s, in.Body.Type, in.Body.Purpose)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterAskOutput{}
		out.Body.ID, out.Body.Due = id, due
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLetters", Method: "GET", Path: "/v1/letters", Tags: []string{"Letters"}, Summary: "Letter requests the caller may read",
		Description: "The caller's own requests; the HR officer, the HR administrator and the Owner read everyone's, those still open first by due day. It changes nothing.", Errors: []int{401},
	}, func(ctx context.Context, in *letterListInput) (*letterListOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Letters.List(ctx, s, p.Team && !in.Mine, in.State)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterListOutput{}
		out.Body.Requests = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "checkLetter", Method: "POST", Path: "/v1/letters/{id}/check", Tags: []string{"Letters"}, Summary: "Check a letter request",
		Description: "The HR officer, the HR administrator or the Owner checks a request, never their own: the letter is drawn into a PDF, which is kept and hashed, and given its reference. The signatory then signs exactly those bytes. It is audited.", Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *letterIDInput) (*letterCheckOutput, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Team }, "Only the people team and the Owner check letters.")
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such letter request.")
		}
		ref, sha, err := a.d.Letters.Check(ctx, s, id)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterCheckOutput{}
		out.Body.Reference, out.Body.SHA256 = ref, sha
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getLetterDraft", Method: "GET", Path: "/v1/letters/{id}/draft", Tags: []string{"Letters"}, Summary: "The checked letter, to read before signing",
		Description: "The exact PDF of a checked letter, so the signatory reads what they are about to sign. The people team and the Owner. It changes nothing.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *letterIDInput) (*letterFileOutput, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Team }, "Only the people team and the Owner read a letter before it is signed.")
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such letter request.")
		}
		ref, pdf, err := a.d.Letters.Draft(ctx, s, id)
		if err != nil {
			return nil, lettersError(err)
		}
		return fileOutput(ref, pdf), nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "signLetter", Method: "POST", Path: "/v1/letters/{id}/signature", Tags: []string{"Letters"}, Summary: "Sign a checked letter",
		Description: "The signatory (the Owner) confirms the SHA-256 of the exact PDF they read, after a passkey confirmation in the last five minutes (POST /v1/session/step-up); the letter is then issued once and kept as evidence with the hash, the template version, the data, the signatory, their authority and what the passkey confirmed. It is audited.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *letterSignInput) (*letterSignOutput, error) {
		s, _, err := staff(ctx, in.Cookie, func(p letterPowers) bool { return p.Sign }, "Only the signatory signs a letter.")
		if err != nil {
			return nil, err
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Signing a letter needs a passkey confirmation in the last five minutes.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such letter request.")
		}
		issued, err := a.d.Letters.Sign(ctx, s, id, in.Body.SHA256)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterSignOutput{}
		out.Body.IssuedID = issued
		return out, nil
	})
	refuse := func(withdraw bool) func(context.Context, *letterRefuseInput) (*struct{}, error) {
		return func(ctx context.Context, in *letterRefuseInput) (*struct{}, error) {
			s, err := a.signedIn(ctx, in.Cookie)
			if err != nil {
				return nil, err
			}
			if !withdraw {
				if p, err := a.letterPowers(ctx, s); err != nil {
					return nil, err
				} else if !p.Team {
					return nil, Err(CodeRoleRequired, "Only the people team and the Owner refuse letters.")
				}
			}
			id, err := uuid.Parse(in.ID)
			if err != nil {
				return nil, Err(CodeNotFound, "There is no such letter request.")
			}
			return nil, lettersErrorOrNil(a.d.Letters.Refuse(ctx, s, id, in.Body.Reason, withdraw))
		}
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "refuseLetter", Method: "POST", Path: "/v1/letters/{id}/refusal", Tags: []string{"Letters"}, Summary: "Refuse a letter request",
		Description: "The people team or the Owner refuses a request that is not issued, with a reason the person is told. It is audited.", Errors: []int{401, 403, 404, 409, 422},
	}, refuse(false))
	huma.Register(a.API, huma.Operation{
		OperationID: "withdrawLetter", Method: "POST", Path: "/v1/letters/{id}/withdrawal", Tags: []string{"Letters"}, Summary: "Withdraw one's own letter request",
		Description: "The person withdraws their own request while it is not issued. It is audited.", Errors: []int{401, 404, 409},
	}, func(ctx context.Context, in *letterIDInput) (*struct{}, error) {
		return refuse(true)(ctx, &letterRefuseInput{Cookie: in.Cookie, ID: in.ID})
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getLetterFile", Method: "GET", Path: "/v1/letters/{id}/file", Tags: []string{"Letters"}, Summary: "An issued letter",
		Description: "The issued PDF, after its bytes are checked against the hash kept as evidence: a changed byte is never served. The person the letter is about, the people team and the Owner. It changes nothing.", Errors: []int{401, 404, 409, 503},
	}, func(ctx context.Context, in *letterIDInput) (*letterFileOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such letter in your view.")
		}
		ref, pdf, err := a.d.Letters.File(ctx, s, id, p.Team)
		if err != nil {
			return nil, lettersError(err)
		}
		return fileOutput(ref, pdf), nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "createLetterLink", Method: "POST", Path: "/v1/letters/{id}/link", Tags: []string{"Letters"}, Summary: "Make an expiring link to an issued letter",
		Description: "Makes a link that delivers the one letter for seven days: its token is shown once and only its hash is kept. The person the letter is about, the people team and the Owner. It is audited.", Errors: []int{401, 404, 409},
		DefaultStatus: 201,
	}, func(ctx context.Context, in *letterIDInput) (*letterLinkOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such letter in your view.")
		}
		token, exp, err := a.d.Letters.Link(ctx, s, id, p.Team)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterLinkOutput{}
		out.Body.Token, out.Body.Expires = token, exp
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "downloadLetterByLink", Method: "GET", Path: "/v1/letter-links/download", Tags: []string{"Letters"}, Summary: "Download a letter by its expiring link",
		Description: "Delivers the letter the token names, with no sign-in, until it expires; a token that is unknown, wrong or expired is refused the same way. The page never shows more than the letter. It changes nothing.", Errors: []int{404, 409, 503},
	}, func(ctx context.Context, in *letterDownloadInput) (*huma.StreamResponse, error) {
		ref, pdf, err := a.d.Letters.ByLink(ctx, strings.TrimSpace(in.Token))
		if err != nil {
			return nil, lettersError(err)
		}
		return &huma.StreamResponse{Body: func(hc huma.Context) {
			hc.SetHeader("Content-Type", "application/pdf")
			hc.SetHeader("Content-Disposition", `attachment; filename="`+ref+`.pdf"`)
			hc.SetHeader("Cache-Control", "no-store")
			hc.SetStatus(http.StatusOK)
			_, _ = hc.BodyWriter().Write(pdf)
		}}, nil
	})
}

func fileOutput(ref string, pdf []byte) *letterFileOutput {
	out := &letterFileOutput{}
	out.Body.Reference, out.Body.ContentType, out.Body.ContentBase64 = ref, "application/pdf", base64.StdEncoding.EncodeToString(pdf)
	return out
}

func lettersErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return lettersError(err)
}
