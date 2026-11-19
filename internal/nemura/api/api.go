// Package api is Nemura's public API (PRD-002 NM-ARC-03, golden rule 4): every capability is an operation in
// contracts/openapi/nemura/ before it is a screen, with Fyniq's conventions: RFC 9457 errors with stable codes, Idempotency-Key
// on writes, ETag and If-Match on versioned records, additive changes inside /v1 (PRD-001 §8.18.1). The web app calls only the
// client generated from the contract. This is its foundation: the conventions and the first operations.
package api

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/version"
)

const description = `Nemura's public API: the optional people platform of PRD-002. Its web app uses only this API (QA-API-03).

Conventions (PRD-002 NM-ARC-03, PRD-001 §8.18.1):
- Errors are RFC 9457 problem details (application/problem+json) with a stable code from the catalogue at GET /v1/errors, what is
  wrong, and how to fix it; validation errors list every invalid field at once.
- Every write that creates or changes state accepts an Idempotency-Key header, kept 24 hours per person: a repeat returns the first
  response with Idempotent-Replayed: true, the same key with a different request returns 422, and two identical requests at once run
  once. A response that may carry pay, a bank account, an NRIC or the text of a question is not stored (only its status is): a repeat
  of such a write that returned a body answers 409 and the result is read from the record.
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
		{Name: "API clients", Description: "Systems that call Nemura's API on their own, registered by the Owner with a key pair they hold (NM-ARC-06)."},
		{Name: "Sign-in", Description: "What the sign-in page shows: the providers on offer and whether first-run setup is done."},
		{Name: "People", Description: "The people record: the directory everyone sees, and each person's record, job history and legal classification (NM-PPL-01, NM-PPL-06, NM-PPL-08)."},
		{Name: "Features", Description: "Each optional feature of an organisation: on, off or held by another system (NM-FEAT-01 to 05)."},
		{Name: "Packs", Description: "Country packs with their sources, the company's policy on top and the people checks (NM-PACK-01 to 08)."},
		{Name: "Leave", Description: "Working patterns, leave balances and the ledger behind them (NM-LV-01, NM-LV-02, NM-LV-13, NM-TIM-01)."},
		{Name: "Assistant", Description: "The people assistant's settings and the drafts it proposed for a person to decide (NM-AI-01, NM-AI-04)."},
		{Name: "Payroll", Description: "Pay groups, a person's pay, the year to date and one-off items: for the Payroll administrator and the Owner alone (NM-PAY-02, NM-PAY-03, FR-EMP-01)."},
		{Name: "Onboarding", Description: "The task to invite new people to Fyniq, and a leaver's dates, checklist and leave settlement (NM-ONB-02, NM-ONB-03)."},
		{Name: "Self-service", Description: "A person's own details and requests, and what waits for the people team (NM-ESS-03, NM-ESS-04)."},
		{Name: "Letters", Description: "Letters, their templates, and what an issued letter proves (NM-LTR-01, NM-LTR-02, NM-LTR-03, NM-LTR-07)."},
		{Name: "Session", Description: "Who is signed in, the roles they hold now and where they land (NM-ROLE-01, NM-ROLE-02)."},
	}
	return cfg
}

// Session is who a request's session is: the person, their organisation and the roles they hold now.
type Session struct {
	ID, Organisation, Member uuid.UUID
	StepUpAt                 *time.Time // when the person last confirmed the session with a passkey
	// Leaver is true when the person's last day has passed: the account is then narrow, for their own documents until LeaverUntil, and holds no role (NM-ESS-06).
	Leaver      bool
	LeaverUntil *time.Time
	Person      uuid.UUID // the person record the login is linked to, or the zero value
}

// StepUpWindow is how long a passkey confirmation stays fresh.
const StepUpWindow = 5 * time.Minute

// Fresh reports whether the session was confirmed with a passkey within the window.
func (s Session) Fresh(now time.Time) bool {
	return s.StepUpAt != nil && !s.StepUpAt.After(now) && now.Sub(*s.StepUpAt) <= StepUpWindow
}

// Passkeys are the passkey ceremonies of a signed-in person (NM-ARC-06): registering one and confirming a session with one. A ceremony is
// begun and completed, each answer used once.
type Passkeys interface {
	BeginRegistration(ctx context.Context, s Session) ([]byte, error)
	CompleteRegistration(ctx context.Context, s Session, name string, credential []byte) (uuid.UUID, error)
	BeginStepUp(ctx context.Context, s Session) ([]byte, error)
	CompleteStepUp(ctx context.Context, s Session, credential []byte) error
}

// The passkey service's refusals, which the API turns into its stable codes.
var (
	ErrPasskeyRefused = errors.New("the passkey's answer was refused")
	ErrNoCeremony     = errors.New("no passkey ceremony is open")
	ErrNoPasskey      = errors.New("the member has no passkey")
)

// Sessions is what the API needs of sign-in and the database to know who is asking. Roles are read again on every request, so a
// change of roles is seen on the next one.
type Sessions interface {
	Authenticate(ctx context.Context, token string, now time.Time) (Session, error)
	RolesAt(ctx context.Context, org, member uuid.UUID, on time.Time) ([]authz.Role, error)
}

// Provider is a sign-in provider people may use, in the order to offer them.
type Provider struct {
	ID   string `json:"id" doc:"The provider's ID, passed to /auth/signin." example:"google"`
	Name string `json:"name" doc:"As the sign-in page names it." example:"Google"`
}

// SignInInfo is what the sign-in page needs: the providers on offer and whether first-run setup is done.
type SignInInfo interface {
	Providers() []Provider
	SetupDone(ctx context.Context) (bool, error)
}

// Deps are what the API is given.
type Deps struct {
	Sessions    Sessions
	Passkeys    Passkeys
	Clients     APIClients
	People      People
	Features    Features
	Restricted  Restricted
	Packs       Packs
	Leave       Leave
	Requests    LeaveRequests
	Sick        Sick
	Government  Government
	Flex        Flex
	Time        Time
	Letters     Letters
	SelfService SelfService
	Onboarding  Onboarding
	Payroll     Payroll
	// Idempotency stores the Idempotency-Keys of writes; nil turns the header off (a deployment with no database for it never runs).
	Idempotency Idempotency
	Assistant   AssistantAdmin
	Asker       Asker
	SignIn      SignInInfo
	Clock       ports.Clock
	// CookieNames are the session cookies the API reads, in order: Nemura's own, with the __Host- prefix over https.
	CookieNames []string
}

// API is Nemura's API.
type API struct {
	huma.API
	d Deps
}

// New registers the API on mux (an http.ServeMux, or the service's httpserver.Server).
func New(mux humago.Mux, d Deps) *API {
	a := &API{d: d}
	a.API = humago.New(idemMux{Mux: mux, a: a}, Config(version.Version))
	a.OpenAPI().OnAddOperation = append(a.OpenAPI().OnAddOperation, addIdempotencyHeader)
	a.register()
	a.registerSession()
	a.registerPasskeys()
	a.registerClients()
	a.registerPeople()
	a.registerFeatures()
	a.registerRestricted()
	a.registerPacks()
	a.registerLeave()
	a.registerLeaveRequests()
	a.registerSick()
	a.registerGov()
	a.registerFlex()
	a.registerTime()
	a.registerAccrual()
	a.registerLetters()
	a.registerSelfService()
	a.registerOnboarding()
	a.registerPayroll()
	a.registerAssistant()
	a.registerSignIn()
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

// Landing is where a person lands, by their highest role (PRD-002 §3): the people team on People home, everyone else on My home. The
// pages arrive with the web app; the path is the contract's.
func Landing(roles []authz.Role) string {
	for _, r := range []authz.Role{authz.Owner, authz.Administrator, authz.PayrollAdmin, authz.HRAdmin, authz.HROfficer} {
		if slices.Contains(roles, r) {
			return "/people"
		}
	}
	return "/me"
}

type sessionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type sessionOutput struct {
	Body struct {
		Organisation string   `json:"organisation_id" format:"uuid" doc:"The organisation the session is in." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
		Member       string   `json:"member_id" format:"uuid" doc:"The person who is signed in." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
		Roles        []string `json:"roles" doc:"The roles the person holds today, from PRD-002 Appendix B." example:"[\"hr_admin\"]"`
		Landing      string   `json:"landing" doc:"The page the person lands on: /people for the people team, /me for everyone else." example:"/people"`
		Person       string   `json:"person_id,omitempty" format:"uuid" doc:"The person record the login is linked to, when there is one." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
		Leaver       bool     `json:"leaver" doc:"True after the person's last day: the account is for their own documents only." example:"false"`
		LeaverUntil  string   `json:"leaver_until,omitempty" format:"date" doc:"The day the leaver's account ends, when it is one." example:"2027-12-01"`
	}
}

func (a *API) cookie(header string) string {
	for _, name := range a.d.CookieNames {
		for _, part := range strings.Split(header, ";") {
			if k, v, ok := strings.Cut(strings.TrimSpace(part), "="); ok && k == name {
				return v
			}
		}
	}
	return ""
}

func (a *API) registerSession() {
	huma.Register(a.API, huma.Operation{
		OperationID: "getSession", Method: "GET", Path: "/v1/session", Tags: []string{"Session"}, Summary: "Who is signed in",
		Description: "Returns the person the session cookie names, the organisation they are in, the roles they hold today and the page they land on. Roles are read again on each request, so a change is seen at once. It changes nothing; without a valid session it is NM_UNAUTHENTICATED.",
		Errors:      []int{401},
	}, func(ctx context.Context, in *sessionInput) (*sessionOutput, error) {
		now := a.d.Clock.Now()
		s, err := a.d.Sessions.Authenticate(ctx, a.cookie(in.Cookie), now)
		if err != nil {
			return nil, Err(CodeUnauthenticated, "There is no valid session in this request.")
		}
		roles, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, now)
		if err != nil {
			return nil, huma.Error500InternalServerError("The roles could not be read.")
		}
		out := &sessionOutput{}
		out.Body.Organisation, out.Body.Member, out.Body.Landing = s.Organisation.String(), s.Member.String(), Landing(roles)
		out.Body.Leaver = s.Leaver
		if s.Person != uuid.Nil {
			out.Body.Person = s.Person.String()
		}
		if s.LeaverUntil != nil {
			out.Body.LeaverUntil = s.LeaverUntil.Format(time.DateOnly)
		}
		out.Body.Roles = make([]string, len(roles))
		for i, r := range roles {
			out.Body.Roles[i] = string(r)
		}
		return out, nil
	})
}
