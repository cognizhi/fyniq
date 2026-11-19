// Package api is Nemura's public API (PRD-002 NM-ARC-03, golden rule 4): every capability is an operation in
// contracts/openapi/nemura/ before it is a screen, with Fyniq's conventions: RFC 9457 errors with stable codes, Idempotency-Key
// on writes, ETag and If-Match on versioned records, additive changes inside /v1 (PRD-001 §8.18.1). The web app calls only the
// client generated from the contract. This is its foundation: the conventions and the first operations.
package api

import (
	"context"
	"regexp"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/cognizhi/fyniq/internal/version"
)

const description = `Nemura's public API: the optional people platform of PRD-002. Its web app uses only this API (QA-API-03).

Conventions (PRD-002 NM-ARC-03, PRD-001 §8.18.1):
- Errors are RFC 9457 problem details (application/problem+json) with a stable code from the catalogue at GET /v1/errors, what is
  wrong, and how to fix it; validation errors list every invalid field at once.
- Every POST that creates or changes state accepts an Idempotency-Key header: a repeat returns the original response, and the same
  key with a different request returns 422.
- Versioned records return an ETag; commands that change one require If-Match and return 412 when the record changed since, 428
  without it.
- Changes inside /v1 are additive: a field, an operation or an enum value may be added, and nothing is removed or changed in meaning.
- Money is a decimal string with its currency, never a JSON number; dates are ISO 8601; IDs are UUIDv7 strings.`

// Config is the API's huma configuration. It leaves out huma's $schema links, documentation page and schema routes, so the
// document is the contract and the responses carry only their fields.
func Config(v string) huma.Config {
	cfg := huma.DefaultConfig("Nemura API", v)
	cfg.CreateHooks = nil
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.OpenAPIPath = "/v1/openapi"
	cfg.Info.Description = description
	cfg.Info.License = &huma.License{Name: "MIT", Identifier: "MIT"}
	cfg.Info.Contact = &huma.Contact{Name: "Nemura", URL: "https://github.com/cognizhi/fyniq"}
	cfg.Servers = []*huma.Server{{URL: "{origin}", Description: "Nemura's origin with /api (one origin serves the web app and the API), or the api service itself in make dev.",
		Variables: map[string]*huma.ServerVariable{"origin": {Default: "http://localhost:8180/api",
			Description: "http://localhost:8180/api behind Nemura's edge (PEOPLE=yes make up); http://localhost:8087 for the api service"}}}}
	cfg.Tags = []*huma.Tag{
		{Name: "Platform", Description: "What the service is and the error codes it returns (NM-ARC-03)."},
	}
	return cfg
}

// API is Nemura's API.
type API struct{ huma.API }

// New registers the API on mux (an http.ServeMux, or the service's httpserver.Server).
func New(mux humago.Mux) *API {
	a := &API{humago.New(mux, Config(version.Version))}
	a.register()
	return a
}

type infoOutput struct {
	Body struct {
		Service string `json:"service" doc:"The service this answers as." example:"nemura-api"`
		Version string `json:"version" doc:"The build's version." example:"0.1.0"`
	}
}

type errorsOutput struct {
	Body struct {
		Codes []Code `json:"codes" doc:"Every stable error code, in order."`
	}
}

type codeInput struct {
	Code string `path:"code" doc:"A stable error code." example:"NM_NOT_FOUND"`
}

type codeOutput struct{ Body Code }

var codeFormat = regexp.MustCompile(`^NM_[A-Z][A-Z_]*$`)

func (a *API) register() {
	huma.Register(a.API, huma.Operation{
		OperationID: "getInfo", Method: "GET", Path: "/v1/info", Tags: []string{"Platform"}, Summary: "What this service is",
		Description: "Names the service and its build. It needs no sign-in and changes nothing: a client uses it to check that it is talking to Nemura's API and which version.",
	}, func(context.Context, *struct{}) (*infoOutput, error) {
		out := &infoOutput{}
		out.Body.Service, out.Body.Version = "nemura-api", version.Version
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listErrorCodes", Method: "GET", Path: "/v1/errors", Tags: []string{"Platform"}, Summary: "The stable error codes",
		Description: "Lists every stable error code the API returns, with its HTTP status, its title and the usual fix. It needs no sign-in and changes nothing; a code never changes meaning inside /v1.",
	}, func(context.Context, *struct{}) (*errorsOutput, error) {
		out := &errorsOutput{}
		out.Body.Codes = Catalogue
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getErrorCode", Method: "GET", Path: "/v1/errors/{code}", Tags: []string{"Platform"}, Summary: "One stable error code",
		Description: "Returns one error code of the catalogue with its status, title and usual fix. It needs no sign-in and changes nothing. An unknown code is NM_NOT_FOUND, and a value that is not shaped like a code is NM_INVALID.",
		Errors:      []int{404, 422},
	}, func(_ context.Context, in *codeInput) (*codeOutput, error) {
		if !codeFormat.MatchString(in.Code) {
			return nil, Err(CodeInvalid, "path.code "+in.Code+" is not an error code.", FieldProblem{Code: CodeInvalid, Field: "path.code",
				Message: "is not an error code", Fix: "Error codes look like NM_NOT_FOUND."})
		}
		for _, c := range Catalogue {
			if c.Code == in.Code {
				return &codeOutput{Body: c}, nil
			}
		}
		return nil, Err(CodeNotFound, "There is no error code "+in.Code+".")
	})
}
