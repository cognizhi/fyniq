// Package api is Fyniq's public REST API under /v1 (PRD-001 §8.18, ADR-T1). huma serves it and
// generates its OpenAPI 3.1 document, which must equal the reviewed contract in
// contracts/openapi/fyniq.v1.yaml. Every operation is registered with its authorisation policy
// (SEC-02) and follows the conventions of §8.18.1: problem details (FR-INT-07), Idempotency-Key
// (FR-INT-08), ETag and If-Match (FR-INT-09), cursor lists and decimal strings for money (FR-INT-10).
//
// The handlers run the same domain commands as everything else, through the storage port; every
// write is one unit of work, audited with the caller as the actor (AUD-01).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/passkey"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Deps are what the API needs.
type Deps struct {
	Store        storage.Store
	Installation storage.Installation // first-run setup and Super admins; nil when there is no database
	Outbox       *outbox.Dispatcher   // queues background work, such as token revocations; nil when there is none
	Clock        ports.Clock
	Auth         authn.Authenticator
	SignIn       ports.SignIn // the providers people sign in with; nil when there are none (the release build until Identity Platform)
	Log          *slog.Logger
	Version      string // the platform version: the document's info.version
	Limits       Limits // the requests a client may make a minute in an organisation (FR-INT-16); zero: DefaultLimits
	// Staging keeps uploads until their record is final, and Scanner checks them for malware (FR-DOC-13,
	// FR-DOC-01); without either, no file can be attached.
	Staging ports.BlobStore
	Scanner ports.Scanner
	// Origin is the web app's origin as the browser sees it, such as https://fyniq.example.com: passkeys are registered
	// and asserted for it and its host (FR-ADM-02). Empty: no passkey ceremony runs.
	Origin string
}

// API is the registered API.
type API struct {
	huma.API
	deps     Deps
	policies map[string]authz.Policy
	journals journal.Service
	closing  closing.Service
	passkeys *passkey.RelyingParty // nil without an origin
}

func init() {
	huma.NewError = newError
	huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		p := newError(status, msg, errs...).(*Problem)
		if p.Instance == "" && ctx != nil {
			p.Instance = instance(ctx.Context())
		}
		return p
	}
}

const description = `Fyniq's public API. The web app uses only this API (FR-INT-01).

Conventions (PRD-001 §8.18.1):
- Errors are RFC 9457 problem details (application/problem+json) with a stable code, the field path,
  what is wrong and how to fix it; validation errors list every invalid field at once (FR-INT-07).
- Every POST that creates or changes state accepts an Idempotency-Key header, kept 24 hours per
  client: a repeat returns the original response; the same key with a different request returns 422
  (FR-INT-08). The installation's operations that take or return a one-time code (first-run setup,
  Super admins) take none: the code makes them one-time, and a response with a code is never stored.
- Versioned records return an ETag; commands that change one require If-Match and return 412 when
  the record changed since, 428 without it (FR-INT-09).
- Lists are cursor-paginated (page_size up to 500, an opaque next_cursor). Money is a decimal string
  with its currency, never a JSON number; dates are ISO 8601; IDs are UUIDv7 strings (FR-INT-10).
- Writes accept Prefer: return=preview: the command runs with every check its commit would, and nothing
  is saved; the answer carries Preference-Applied: return=preview (FR-INT-14). The one-time-code
  operations take no preview.
- Bulk operations take up to 250 items and answer with one result per item, in order (FR-INT-11).
- Each client may make 600 reads and 120 writes a minute in an organisation; RateLimit and
  RateLimit-Policy say what is left, and 429 comes with Retry-After (FR-INT-16).
- A deprecated operation carries Deprecation, Sunset and a Link to its migration note, at least six
  months before its sunset (FR-INT-06).
- Every operation states the right it needs (x-fyniq-permission, from PRD-001 Appendix D) and the
  error codes it returns (x-fyniq-error-codes).`

// Config is the API's huma configuration. It leaves out huma's $schema links, documentation page and
// schema routes, so the document is the contract and the responses carry only their fields.
func Config(version string) huma.Config {
	cfg := huma.DefaultConfig("Fyniq API", version)
	cfg.CreateHooks = nil
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.OpenAPIPath = "/v1/openapi"
	cfg.Info.Description = description
	cfg.Info.License = &huma.License{Name: "MIT", Identifier: "MIT"}
	cfg.Info.Contact = &huma.Contact{Name: "Fyniq", URL: "https://github.com/cognizhi/fyniq"}
	cfg.Servers = []*huma.Server{{URL: "{origin}", Description: "The installation's API: the web app's origin with /api " +
		"(one origin serves both), or the api service itself in make dev.",
		Variables: map[string]*huma.ServerVariable{"origin": {Default: "http://localhost:8080/api",
			Description: "http://localhost:8080/api behind Caddy (make up); http://localhost:8081 for the api service (make dev)"}}}}
	cfg.Tags = []*huma.Tag{
		{Name: "Installation", Description: "First-run setup of a new installation (FR-ADM-06)."},
		{Name: "Session", Description: "Signing in to the web app: the providers, and your own session (SEC-05, FR-ADM-08)."},
		{Name: "Organisation", Description: "The organisation your session is in."},
		{Name: "Chart of accounts", Description: "Accounts: what the ledger posts to (PRD-001 §8.2)."},
		{Name: "Journals", Description: "Manual journals: drafted, posted and reversed; posted history never changes (FR-GL-12)."},
		{Name: "Periods", Description: "Accounting periods and financial years: a posting's period is the month of its date (FR-ORG-02, FR-GL-08)."},
		{Name: "Reports", Description: "Reports read from the ledger at one revision: the general ledger (FR-GL-11), and the statements as they arrive."},
		{Name: "Contacts", Description: "Customers and vendors (FR-CT-01)."},
		{Name: "Members", Description: "Members of the organisation, their roles, and invitations to join it (FR-ADM-01)."},
		{Name: "Items", Description: "Products and services the organisation sells, with a price per currency (FR-ITM-01)."},
		{Name: "Approvals", Description: "Changes that wait for a second approver (FM-1): the envelope each approver signs, their decisions, and the policies that say who approves (FR-APR-01…05, FR-APR-09, FR-APR-10)."},
		{Name: "Statutory packs", Description: "Statutory parameters as versioned packs from official sources, with pinned tests, and the organisation's activation of each version (ENG-30, ENG-32)."},
		{Name: "Evidence", Description: "Files that prove what happened, attached to records: scanned, hashed, and append-only once the record is final (FR-DOC-05, FR-DOC-07)."},
	}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {Type: "http", Scheme: "bearer", Description: "An access token from Fyniq's OAuth server (ADR-T3), for integrations and MCP " +
			"clients. The local build also accepts the local issuer's dev tokens."},
		"session": {Type: "apiKey", In: "cookie", Name: authn.SessionCookie(true), Description: "The web app's first-party session (SEC-05, SEC-08), " +
			"set by signing in at /auth/signin: an HttpOnly cookie the web app never reads (fyniq_session on http://localhost). A request it " +
			"authenticates may change state only from the web app's own origin (Sec-Fetch-Site: same-origin)."},
	}
	cfg.Security = []map[string][]string{{"bearer": {}}, {"session": {}}}
	cfg.OnAddOperation = append(cfg.OnAddOperation, describeErrors, describeDeprecation, describePreview, describeRateLimits)
	return cfg
}

// New registers the API on mux (an http.ServeMux, or the service's httpserver.Server).
func New(mux humago.Mux, d Deps) *API {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Store == nil {
		d.Auth = authn.NotYet{} // without a database nobody gets past authentication
	}
	a := &API{deps: d, policies: map[string]authz.Policy{}, journals: journal.Service{Store: d.Store}, closing: closing.Service{Store: d.Store, Clock: d.Clock}}
	if d.Origin != "" {
		rp, err := passkey.New(d.Origin)
		if err != nil {
			d.Log.Error("api.passkeys.off", "reason", err.Error())
		}
		a.passkeys = rp
	}
	a.journals.AfterPosting = a.commitEvidence // a posted journal's files go to the locked store (FR-DOC-13)
	cfg := Config(d.Version)
	cfg.Transformers = append(cfg.Transformers, a.mask)
	a.API = humago.New(limitUploads{mux}, cfg)
	a.UseMiddleware(a.requestContext, a.authenticate, a.limit, a.authorise)
	registerOrganisation(a)
	registerProfile(a)
	registerGST(a)
	registerSequences(a)
	registerAccounts(a)
	registerChart(a)
	registerPeriods(a)
	registerOpening(a)
	registerJournalApprovals(a)
	registerJournalEdits(a)
	registerClose(a)
	registerReports(a)
	registerAccountEditing(a)
	registerContacts(a)
	registerItems(a)
	registerSettings(a)
	registerEmail(a)
	registerMembers(a)
	registerApprovals(a)
	registerApprovalPolicies(a)
	registerPacks(a)
	registerTeamMode(a)
	registerPasskeys(a)
	registerDevices(a)
	registerReviewLog(a)
	registerJournals(a)
	registerRecurringJournals(a)
	registerAccounting(a)
	registerEvidence(a)
	registerSetup(a)
	registerSuperAdmins(a)
	registerSession(a)
	return a
}

// Policies returns the policy of every registered operation.
func (a *API) Policies() map[string]authz.Policy { return a.policies }

// The codes every operation can return.
var commonCodes = []string{CodeUnauthenticated, CodeForbidden, CodeNoOrganisation, CodeNoPolicy, CodeInvalid, CodeFailure, CodeSecondFactor}

// register adds an operation with its policy: there is no other way to add one, so no operation
// exists without a policy (SEC-02), its permission and its error codes in the document
// (NFR-API-01).
func register[I, O any](a *API, p authz.Policy, op huma.Operation, codes []string, h func(ctx context.Context, in *I) (*O, error)) {
	if op.OperationID == "" || op.OperationID != p.Operation {
		panic(fmt.Sprintf("api: operation %q registered with the policy of %q", op.OperationID, p.Operation))
	}
	if _, dup := a.policies[p.Operation]; dup {
		panic("api: operation registered twice: " + p.Operation)
	}
	a.policies[p.Operation] = p
	codes = append(slices.Clone(codes), commonCodes...)
	if !p.Public && !authn.Safe(op.Method) {
		codes = append(codes, CodeCrossSite) // a write the session cookie authenticates must come from the web app
	}
	if !p.Public {
		codes = append(codes, CodeRateLimited) // FR-INT-16
	}
	slices.Sort(codes)
	codes = slices.Compact(codes)
	statuses := map[int]bool{}
	for _, c := range codes {
		f, ok := codeFacts[c]
		if !ok {
			panic("api: " + op.OperationID + " lists the unknown code " + c)
		}
		statuses[f.Status] = true
	}
	op.Errors = nil
	for s := range statuses {
		op.Errors = append(op.Errors, s)
	}
	slices.Sort(op.Errors)
	permission := map[string]any{"capability": string(p.Capability), "right": string(p.Need)}
	if p.Note != "" {
		permission["note"] = p.Note
	}
	if len(p.MaskedFields) > 0 {
		permission["masked_fields"] = p.MaskedFields
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-fyniq-permission"] = permission
	op.Extensions["x-fyniq-error-codes"] = codes
	huma.Register(a.API, op, func(ctx context.Context, in *I) (*O, error) {
		out, err := h(ctx, in)
		if err != nil {
			pr := problemFrom(ctx, err)
			if pr.Instance == "" {
				pr.Instance = instance(ctx)
			}
			return nil, pr
		}
		return out, nil
	})
}

// describeErrors names, on each error response of the document, the codes it carries.
func describeErrors(_ *huma.OpenAPI, op *huma.Operation) {
	codes, _ := op.Extensions["x-fyniq-error-codes"].([]string)
	for status, resp := range op.Responses {
		n, err := strconv.Atoi(status)
		if err != nil || n < 400 {
			continue
		}
		var mine []string
		for _, c := range codes {
			if codeFacts[c].Status == n {
				mine = append(mine, fmt.Sprintf("`%s` %s", c, codeFacts[c].Title))
			}
		}
		if len(mine) > 0 {
			resp.Description = http.StatusText(n) + ": " + strings.Join(mine, "; ") + "."
		}
	}
}

// ------------------------------------------------------------------ request context

type requestIDKey struct{}
type decisionKey struct{}

func instance(ctx context.Context) string {
	if id, _ := ctx.Value(requestIDKey{}).(string); id != "" {
		return "urn:fyniq:request:" + id
	}
	return ""
}

// requestContext names the request with a new UUIDv7, returned in X-Request-Id, and records where it
// came from for the audit trail. A caller's own X-Request-Id is never used: the audit trail must not
// carry an ID someone else chose.
func (a *API) requestContext(ctx huma.Context, next func(huma.Context)) {
	id := uuid.Must(uuid.NewV7()).String()
	ctx.SetHeader("X-Request-Id", id)
	ctx.SetHeader("Cache-Control", "no-store")
	for name, value := range deprecationHeaders(ctx.Operation()) {
		ctx.SetHeader(name, value)
	}
	host, _, err := net.SplitHostPort(ctx.RemoteAddr())
	if err != nil {
		host = ctx.RemoteAddr()
	}
	c := context.WithValue(ctx.Context(), requestIDKey{}, id)
	c = context.WithValue(c, loggerKey{}, a.deps.Log)
	c = storage.WithAudit(c, storage.AuditContext{Request: storage.Request{ID: id, IP: host, UserAgent: ctx.Header("User-Agent")}})
	next(huma.WithContext(ctx, c))
}

// authenticate refuses a request without valid credentials (401), except for a public operation.
func (a *API) authenticate(ctx huma.Context, next func(huma.Context)) {
	if a.policies[ctx.Operation().OperationID].Public {
		next(ctx)
		return
	}
	creds := authn.Credentials{Authorization: ctx.Header("Authorization"), Cookie: ctx.Header("Cookie")}
	p, err := a.deps.Auth.Authenticate(ctx.Context(), creds)
	if err != nil && !errors.Is(err, authn.ErrUnauthenticated) {
		logFailure(ctx.Context(), err)
		_ = huma.WriteErr(a.API, ctx, http.StatusInternalServerError, "", problem(CodeFailure,
			"Your sign-in could not be checked.", "Try again in a moment."))
		return
	}
	if err != nil {
		ctx.SetHeader("WWW-Authenticate", `Bearer realm="fyniq"`)
		_ = huma.WriteErr(a.API, ctx, http.StatusUnauthorized, "", problem(CodeUnauthenticated,
			"The request has no valid access token or session.", "Sign in again, or send the request with a bearer token."))
		return
	}
	// The session cookie is ambient: any site could make a browser send it. A request it authenticates may
	// change state only from the web app's own origin (CSRF; OWASP Fetch Metadata).
	if creds.Authorization == "" && !authn.Safe(ctx.Method()) && ctx.Header("Sec-Fetch-Site") != "same-origin" {
		a.deps.Log.Warn("api.refused", "operation", ctx.Operation().OperationID, "code", CodeCrossSite, "subject", p.Subject,
			"request_id", requestID(ctx.Context()))
		_ = huma.WriteErr(a.API, ctx, http.StatusForbidden, "", problem(CodeCrossSite,
			"The request was sent from another site with your Fyniq session.", "Do it in Fyniq itself."))
		return
	}
	c := authn.WithPrincipal(ctx.Context(), p)
	audit := storage.AuditFrom(c)
	actor := &storage.Actor{Type: "user", ID: p.Subject}
	if p.UserID != uuid.Nil {
		actor.ID = p.UserID.String() // a person is their user, whichever sign-in they used (AUD-01)
	}
	switch p.Class {
	case authn.ClassIntegration:
		actor.Type = "integration"
	case authn.ClassAgent:
		audit.OnBehalfOf = actor
		actor = &storage.Actor{Type: "ai", ID: p.Client}
	}
	audit.Actor = actor
	audit.Request.Session = p.Client
	next(huma.WithContext(ctx, storage.WithAudit(c, audit)))
}

// authorise refuses an operation without a policy, a caller outside an organisation, and a caller
// whose roles hold no right for it (403). A refusal goes to the security log, not the audit trail.
func (a *API) authorise(ctx huma.Context, next func(huma.Context)) {
	p, _ := authn.From(ctx.Context())
	op := ctx.Operation()
	policy, ok := a.policies[op.OperationID]
	var refusal *Problem
	switch {
	case !ok:
		refusal = problem(CodeNoPolicy, "This operation has no authorisation policy, so nobody may call it.",
			"Report it: every operation needs a policy (SEC-02).")
	case policy.Public:
		next(huma.WithContext(ctx, context.WithValue(ctx.Context(), operationKey{}, op.OperationID)))
		return
	case p.Via == authn.ViaSession && p.StepUpAt.IsZero() && !policy.SingleFactor:
		// Every user signs in with a second factor (SEC-01): a session that passed none only reads itself and steps up.
		refusal = problem(CodeSecondFactor, "Your sign-in passed no second factor, and every Fyniq user signs in with one (SEC-01).",
			"Sign in again with your authenticator app, or confirm it's you with a passkey.")
	case p.Organisation == uuid.Nil && !policy.NoOrganisation:
		refusal = problem(CodeNoOrganisation, "Your session is not in an organisation.", "Choose an organisation and sign in to it.")
	}
	var d authz.Decision
	if refusal == nil {
		if d = authz.Decide(policy, p.Roles, p.AI()); !d.Allowed {
			refusal = problem(CodeForbidden, fmt.Sprintf("Your roles hold no %q right on %q.", string(policy.Need), policy.Capability),
				"Ask an Owner or Administrator for a role with that right (PRD-001 Appendix D).")
		}
	}
	if refusal != nil {
		a.deps.Log.Warn("api.refused", "operation", op.OperationID, "code", refusal.Code, "subject", p.Subject,
			"client", p.Client, "organisation", p.Organisation.String(), "reason", d.Reason, "request_id", requestID(ctx.Context()))
		_ = huma.WriteErr(a.API, ctx, refusal.Status, "", refusal)
		return
	}
	c := context.WithValue(ctx.Context(), decisionKey{}, d)
	next(huma.WithContext(ctx, context.WithValue(c, operationKey{}, op.OperationID)))
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func logFailure(ctx context.Context, err error) {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		l.Error("api.failure", "error", err.Error(), "request_id", requestID(ctx))
	}
}

type loggerKey struct{}

// mask replaces the fields a masked view hides (Appendix D's M) in a successful response.
func (a *API) mask(ctx huma.Context, status string, v any) (any, error) {
	d, _ := ctx.Context().Value(decisionKey{}).(authz.Decision)
	if len(d.Masked) == 0 || !strings.HasPrefix(status, "2") || v == nil {
		return v, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var tree any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	return maskTree(tree, d.Masked), nil
}

// MaskedValue is what a masked field shows.
const MaskedValue = "••••"

func maskTree(v any, fields []string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if slices.Contains(fields, k) && x != nil {
				t[k] = MaskedValue
			} else {
				t[k] = maskTree(x, fields)
			}
		}
	case []any:
		for i := range t {
			t[i] = maskTree(t[i], fields)
		}
	}
	return v
}
