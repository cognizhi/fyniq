package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/policy"
)

// Problem is an RFC 9457 problem details object (NM-ARC-03; PRD-001 FR-INT-07): a stable code from the catalogue, what went
// wrong this time in plain words naming the field, and how to fix it.
type Problem struct {
	Type   string         `json:"type" format:"uri" doc:"A URI naming the problem; one per code." example:"urn:nemura:problem:NM_NOT_FOUND"`
	Title  string         `json:"title" doc:"A short summary of the problem type; it never changes between occurrences." example:"Not found"`
	Status int            `json:"status" doc:"The HTTP status code." example:"404"`
	Detail string         `json:"detail" doc:"What went wrong this time, in plain English." example:"There is no error code NM_NOPE."`
	Code   string         `json:"code" doc:"The stable error code (ENG-23)." example:"NM_NOT_FOUND"`
	Fix    string         `json:"fix,omitempty" doc:"How to fix it." example:"Choose a code from the catalogue at GET /v1/errors."`
	Errors []FieldProblem `json:"errors,omitempty" doc:"Every invalid field, all at once."`
}

// FieldProblem is one invalid field.
type FieldProblem struct {
	Code    string `json:"code" doc:"The stable error code." example:"NM_INVALID"`
	Field   string `json:"field" doc:"The JSON path of the field: in the request body (body.…), the query (query.…), the path (path.…) or a header (header.…)." example:"path.code"`
	Message string `json:"message" doc:"What is wrong with it." example:"is not an error code"`
	Fix     string `json:"fix" doc:"How to fix it." example:"Error codes look like NM_NOT_FOUND."`
}

func (p *Problem) Error() string  { return p.Code + ": " + p.Detail }
func (p *Problem) GetStatus() int { return p.Status }

// ContentType makes every error response application/problem+json (RFC 9457).
func (p *Problem) ContentType(ct string) string {
	if ct == "application/json" {
		return "application/problem+json"
	}
	return ct
}

// Code is one entry of the catalogue: a stable code, its status, a title that never changes and the usual fix.
type Code struct {
	Code   string `json:"code" doc:"The stable error code." example:"NM_NOT_FOUND"`
	Status int    `json:"status" doc:"The HTTP status the code comes with." example:"404"`
	Title  string `json:"title" doc:"A short summary that never changes between occurrences." example:"Not found"`
	Fix    string `json:"fix" doc:"The usual way to fix it." example:"Choose a code from the catalogue."`
}

// The platform's codes. A code joins the catalogue with the change that triggers it, and a test triggers each one
// (TestEveryCodeInTheCatalogueIsTriggered). PRD-002 names more (NM_FEATURE_REQUIRED, NM_POLICY_BELOW_STATUTE,
// NM_FEATURE_OBLIGATIONS_OPEN): they arrive with the features that raise them.
const (
	CodeNotFound           = "NM_NOT_FOUND"           // no such record
	CodeInvalid            = "NM_INVALID"             // a field is invalid
	CodeUnauthenticated    = "NM_UNAUTHENTICATED"     // no valid session
	CodePasskeyRefused     = "NM_PASSKEY_REFUSED"     // #nosec G101 -- a problem code's name: the passkey's answer failed a check
	CodeNoCeremony         = "NM_PASSKEY_NO_CEREMONY" // the ceremony was not started, or its challenge was used or has expired
	CodeRoleRequired       = "NM_ROLE_REQUIRED"       // the person's roles do not allow this
	CodeStepUp             = "NM_STEP_UP_REQUIRED"    // confirm it is the person with a passkey, then try again
	CodeClientInvalid      = "NM_API_CLIENT_INVALID"  // the registration is not valid
	CodeClientExists       = "NM_API_CLIENT_EXISTS"   // a live client has that name
	CodePersonInvalid      = people.CodeInvalid
	CodePersonNotFound     = people.CodeNotFound
	CodePersonHistory      = people.CodeHistory
	CodePersonExists       = people.CodeExists
	CodeFeatureRequired    = features.CodeRequired
	CodeFeatureUnavailable = features.CodeUnavailable
	CodeFeatureObligations = features.CodeObligations
	CodeFeatureInvalid     = features.CodeInvalid
	CodePolicyBelow        = policy.CodeBelowStatute
	CodeUnavailable        = "NM_UNAVAILABLE" // the service is not configured for this here
	CodeNoPasskey          = "NM_NO_PASSKEY"  // #nosec G101 -- a problem code's name: no passkey to confirm with
)

// Catalogue is every stable code the API returns, in order. It is served at GET /v1/errors.
var Catalogue = []Code{
	{CodeNotFound, http.StatusNotFound, "Not found", "Check the identifier, and that it belongs to your organisation."},
	{CodeInvalid, http.StatusUnprocessableEntity, "A field is not valid", "Correct the fields listed in errors and send the request again."},
	{CodeUnauthenticated, http.StatusUnauthorized, "Sign-in required", "Sign in, then try again."},
	{CodePasskeyRefused, http.StatusUnprocessableEntity, "The passkey's answer was refused", "Try again with the passkey you registered, on this site, and confirm it is you."},
	{CodeNoCeremony, http.StatusConflict, "No passkey ceremony is open", "Start again: ask for the options, then answer them within five minutes."},
	{CodeNoPasskey, http.StatusConflict, "No passkey to confirm with", "Register a passkey first, then confirm again."},
	{CodePersonInvalid, http.StatusUnprocessableEntity, "The person's details are not valid", "Correct the field the message names and send it again."},
	{CodePersonNotFound, http.StatusNotFound, "No such person", "Check the person's ID: it is in the directory (GET /v1/people)."},
	{CodePersonHistory, http.StatusConflict, "That would rewrite history", "A job or classification is changed by starting a new one from a later day, never by editing the old one."},
	{CodePersonExists, http.StatusConflict, "That person exists already", "Use the person's ID with PUT /v1/people/{id} to change them, or a new ID to add another."},
	{CodeFeatureRequired, http.StatusConflict, "Another feature needs this", "Switch the feature the message names first."},
	{CodeFeatureUnavailable, http.StatusConflict, "That feature cannot be switched on yet", "It arrives in a later release, or needs an approval that is not built yet."},
	{CodeFeatureObligations, http.StatusConflict, "Work is still open in this feature", "Record how each open thing will be handled, then switch the feature off."},
	{CodeFeatureInvalid, http.StatusUnprocessableEntity, "The feature or its state is not valid", "Use a feature from GET /v1/features, a state of on, off or external, and a system name only for external."},
	{CodePolicyBelow, http.StatusUnprocessableEntity, "The policy gives less than the statute", "Raise each rule the message names to at least the statutory value."},
	{CodeLeaveInvalid, http.StatusUnprocessableEntity, "The leave details are not valid", "Correct the field the message names and send it again."},
	{CodeLeaveBalance, http.StatusConflict, "That is more than the balance", "Ask for no more than the days left, or ask the people team about the leave type."},
	{CodeLeaveConflict, http.StatusConflict, "That overlaps leave or a pattern already recorded", "Start the new record on or after the day the last one ends."},
	{CodeFeatureOff, http.StatusConflict, "That feature is switched off", "Switch the feature on under Settings, Features; its records are kept while it is off."},
	{CodeLetterInvalid, http.StatusUnprocessableEntity, "The letter is not valid", "Correct what the message names and send it again."},
	{CodeLetterConflict, http.StatusConflict, "That cannot happen to the letter now", "A published template, a checked letter and an issued letter are evidence: change them with a new version, a new request or a new letter."},
	{CodeUnavailable, http.StatusServiceUnavailable, "Not available here", "This deployment has not set it up: ask whoever runs Nemura."},
	{CodeRoleRequired, http.StatusForbidden, "Not allowed for your roles", "Ask the Owner to do it, or to change your roles."},
	{CodeStepUp, http.StatusForbidden, "Confirm it is you", "Confirm with your passkey (POST /v1/session/step-up), then try again within five minutes."},
	{CodeClientInvalid, http.StatusUnprocessableEntity, "The API client is not valid", "Correct the name, scopes, algorithm or public key named in the message."},
	{CodeClientExists, http.StatusConflict, "An API client has that name", "Choose another name, or revoke the first client."},
}

func lookup(code string) Code {
	for _, c := range Catalogue {
		if c.Code == code {
			return c
		}
	}
	panic("api: " + code + " is not in the catalogue")
}

// Err is a problem for a catalogue code, with what went wrong this time.
func Err(code, detail string, fields ...FieldProblem) *Problem {
	c := lookup(code)
	return &Problem{Type: "urn:nemura:problem:" + c.Code, Title: c.Title, Status: c.Status, Detail: detail, Code: c.Code, Fix: c.Fix, Errors: fields}
}

func init() {
	// huma's own errors (a body that cannot be read, a path that does not match) become problems with a catalogue code.
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		p := Err(CodeInvalid, msg)
		if status == http.StatusNotFound {
			p = Err(CodeNotFound, msg)
		}
		for _, e := range errs {
			if d, ok := e.(*huma.ErrorDetail); ok {
				p.Errors = append(p.Errors, FieldProblem{Code: CodeInvalid, Field: d.Location, Message: d.Message, Fix: "Correct the field and send the request again."})
			}
		}
		return p
	}
}
