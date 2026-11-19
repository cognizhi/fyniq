package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
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
	CodeNotFound = "NM_NOT_FOUND" // no such record
	CodeInvalid  = "NM_INVALID"   // a field is invalid
)

// Catalogue is every stable code the API returns, in order. It is served at GET /v1/errors.
var Catalogue = []Code{
	{CodeNotFound, http.StatusNotFound, "Not found", "Check the identifier, and that it belongs to your organisation."},
	{CodeInvalid, http.StatusUnprocessableEntity, "A field is not valid", "Correct the fields listed in errors and send the request again."},
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
