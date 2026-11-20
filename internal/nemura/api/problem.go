package api

import (
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
	"github.com/cognizhi/fyniq/internal/nemura/performance"
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
	CodeLeaverAccount      = "NM_LEAVER_ACCOUNT"       // a leaver's account is for their own documents only
	CodeUnavailable        = "NM_UNAVAILABLE"          // the service is not configured for this here
	CodeNoPasskey          = "NM_NO_PASSKEY"           // #nosec G101 -- a problem code's name: no passkey to confirm with
	CodeLinkNone           = "NM_LINK_NONE"            // there is no active link, or the call is not from the link's client at its epoch
	CodeLinkClient         = "NM_LINK_CLIENT"          // the token is valid but not the link's client's, or lacks the link scope
	CodeLinkApprover       = "NM_LINK_SECOND_APPROVER" // an Owner starts the link and another Owner or Administrator approves it
	CodeLinkState          = "NM_LINK_STATE"           // the link is in a state that does not allow that
	CodeLinkRefused        = "NM_LINK_REFUSED"         // the signed event was refused: its own code says why
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
	{CodeLeaverAccount, http.StatusForbidden, "That is not part of your account now", "After the last day an account keeps the person's own documents and letters, and a way to ask the people team a question."},
	{CodeIdempotencyMismatch, http.StatusUnprocessableEntity, "That Idempotency-Key was used for a different request", "Use a new key for a new request, or send the same request again with the same key."},
	{CodeIdempotencyConflict, http.StatusConflict, "That request already ran or is still running", "Read the record for the result: an answer that may carry pay or restricted data is not stored, and an identical request may still be running."},
	{CodeAINever, http.StatusUnprocessableEntity, "The assistant never does that", "The assistant drafts and explains; a person approves, issues and changes records. Ask for a draft instead."},
	{CodeAIInvalid, http.StatusUnprocessableEntity, "The assistant's draft or settings are not valid", "Correct what the message names and send it again."},
	{CodeAIOff, http.StatusConflict, "The assistant is off", "The Owner turns the assistant, and each kind of draft, on in the assistant's settings."},
	{CodeAIConflict, http.StatusConflict, "That proposal cannot change now", "A proposal is decided once, within 72 hours; ask the assistant again after that."},
	{CodePayrollInvalid, http.StatusUnprocessableEntity, "The pay is not valid", "Correct what the message names and send it again."},
	{CodePayrollConflict, http.StatusConflict, "That cannot happen to the pay now", "A pay record and an election are history: start a new one from a later day. A name, a key and a year to date are used once."},
	{CodeOnboardingInvalid, http.StatusUnprocessableEntity, "The leaving or the task is not valid", "Correct what the message names and send it again."},
	{CodeOnboardingConflict, http.StatusConflict, "That cannot happen to the task or the leaving now", "A task is decided once; a leaving is withdrawn only before its last day."},
	{CodeRequestInvalid, http.StatusUnprocessableEntity, "The request is not valid", "Correct what the message names and send it again."},
	{CodeRequestConflict, http.StatusConflict, "That cannot happen to the request now", "A request keeps its words and its evidence and is decided or answered once: make a new request instead."},
	{CodeLetterConflict, http.StatusConflict, "That cannot happen to the letter now", "A published template, a checked letter and an issued letter are evidence: change them with a new version, a new request or a new letter."},
	{"NM_ERASURE_NOT_LEFT", http.StatusConflict, "That person has not left", "Only a former employee's data is erased: record the leaving date first."},
	{"NM_ERASURE_NOT_YET", http.StatusConflict, "The records must be kept longer", "The employment records and the payroll records have retention periods: ask again on the date the message names."},
	{"NM_ERASURE_HELD", http.StatusConflict, "A legal hold keeps the records", "Release the hold, or leave the records as they are."},
	{"NM_ERASURE_SELF", http.StatusForbidden, "A second person approves", "An Owner or an HR administrator other than the one who asked approves the erasure."},
	{"NM_ERASURE_NOT_APPROVED", http.StatusConflict, "The erasure is not approved", "A second Owner or HR administrator approves it first."},
	{"NM_ERASURE_ALREADY", http.StatusConflict, "That erasure was decided or done already", "An erasure is asked for, decided and done once."},
	{"NM_ERASURE_REGISTER", http.StatusConflict, "The register's entry does not verify", "Ask whoever runs the deployment: the register is checked before it is applied."},
	{hiring.CodeInvalid, http.StatusUnprocessableEntity, "The requisition, the post or the exemption is not valid", "Correct what the message names and send it again."},
	{hiring.CodeConflict, http.StatusConflict, "That has moved on", "A requisition is decided once, a published post never changes, and the advert's date and an exemption are recorded once: read it again."},
	{hiring.CodeWording, http.StatusUnprocessableEntity, "The post names criteria the guidelines ask an employer not to use", "Describe the skill or duty instead, or record the genuine occupational requirement for each criterion."},
	{performance.CodeInvalid, http.StatusUnprocessableEntity, "The goal, the key result or the value is not valid", "Correct what the message names and send it again."},
	{performance.CodeConflict, http.StatusConflict, "That goal has moved on", "A goal is done or cancelled once, its words and targets never change, and a closed goal's key results are no longer updated."},
	{"NM_GRIEVANCE_STATE", http.StatusConflict, "That grievance has moved on", "A grievance is decided once, and an unrouted one is taken once: read it again to see where it is."},
	{"NM_GRIEVANCE_NO_HANDLER", http.StatusConflict, "Nobody else can handle it", "Every HR administrator and the Owner is the complainant, is involved or is the one passing it on: it stays with its handler."},
	{"NM_ERASURE_ACCESS_REQUEST", http.StatusConflict, "The person has an access request to answer", "Answer the person's request for their own data first (NM-SEC-07)."},
	{"NM_RATE_LIMITED", http.StatusTooManyRequests, "Too many tries", "Wait a while and try again with the reference and the code exactly as printed on the letter."},
	{CodeUnavailable, http.StatusServiceUnavailable, "Not available here", "This deployment has not set it up: ask whoever runs Nemura."},
	{CodeRoleRequired, http.StatusForbidden, "Not allowed for your roles", "Ask the Owner to do it, or to change your roles."},
	{CodeStepUp, http.StatusForbidden, "Confirm it is you", "Confirm with your passkey (POST /v1/session/step-up), then try again within five minutes."},
	{CodeClientInvalid, http.StatusUnprocessableEntity, "The API client is not valid", "Correct the name, scopes, algorithm or public key named in the message."},
	{CodeClientExists, http.StatusConflict, "An API client has that name", "Choose another name, or revoke the first client."},
	{CodeLinkNone, http.StatusConflict, "Nemura is not linked to Fyniq", "Link the organisation to Fyniq first, or call with the client of the link that is live now."},
	{CodeLinkClient, http.StatusForbidden, "Not the link's client", "Call with the token the link's own client got for the audience nemura-api and the scope link.events.write."},
	{CodeLinkApprover, http.StatusForbidden, "The link needs a second person", "An Owner starts the link, and an Owner or Administrator other than them approves it, each with a passkey confirmed in the last five minutes."},
	{CodeLinkState, http.StatusConflict, "The link is not in a state for that", "A link is proposed once and approved once; unlink first to start again."},
	{CodeLinkRefused, http.StatusUnprocessableEntity, "The event was refused", "Send the event as Fyniq signed it, within five minutes, at the link's epoch, with only the fields PRD-002 Appendix E lists; the field's code says which check failed."},
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
