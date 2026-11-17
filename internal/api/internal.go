package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The internal API (PRD-001 §9.3 rev 0.277; p1-ai-assistant design D4): the operations only the AI service calls, under
// /internal/v1 on the API's own port. The edge never routes them (Caddy and Cloud Armor refuse /api/internal/), and
// each call needs both the AI service's identity, its service token for ServiceAI, and the delegated read token of the
// turn it works on, which names the person it acts for. Its contract is contracts/openapi/fyniq-internal.v1.yaml.
const (
	// ServiceAI is the class of the AI service's service tokens.
	ServiceAI = "ai"
	// ServiceTokenHeader carries the calling service's token.
	ServiceTokenHeader = "X-Fyniq-Service-Token" // #nosec G101 -- a header's name, not a credential
	internalBase       = "/internal/v1"
)

// InternalConfig is the internal API's OpenAPI configuration: it serves no document of its own.
func InternalConfig(version string) huma.Config {
	cfg := huma.DefaultConfig("Fyniq internal API", version)
	cfg.CreateHooks = nil
	cfg.DocsPath, cfg.SchemasPath, cfg.OpenAPIPath = "", "", ""
	cfg.Info.Description = "The operations only the AI service calls (PRD-001 §9.3 rev 0.277): the model calls of the assistant's turns, " +
		"run by the API's model router so the provider keys, the budgets and the metering stay in the API. Never routed by the edge. " +
		"Each call carries the AI service's service token (" + ServiceTokenHeader + ") and the turn's delegated read token " +
		"(Authorization: Bearer fyai_…), which names the person the turn is for."
	cfg.Info.License = &huma.License{Name: "MIT", Identifier: "MIT"}
	cfg.Servers = []*huma.Server{{URL: "http://api:8080", Description: "The api service, from the AI service only: the Compose network, or Cloud Run's internal ingress."}}
	cfg.Tags = []*huma.Tag{{Name: "Assistant", Description: "What the AI service asks the API for while it answers a turn."}}
	return cfg
}

// registerInternal registers the internal API on mux.
func (a *API) registerInternal(mux humago.Mux) {
	a.internal = humago.New(mux, InternalConfig(a.deps.Version))
	a.internal.UseMiddleware(a.requestContext, a.internalAuthenticate)
	registerModelCalls(a)
}

// internalAuthenticate accepts a call only from the AI service, for a live turn: its service token, then the turn's
// delegated read token, whose person the call acts for, audited as the AI on their behalf.
func (a *API) internalAuthenticate(ctx huma.Context, next func(huma.Context)) {
	refuse := func(status int, p *Problem) {
		a.deps.Log.WarnContext(ctx.Context(), "api.internal.refused", "operation", ctx.Operation().OperationID, "code", p.Code,
			"request_id", requestID(ctx.Context()))
		_ = huma.WriteErr(a.internal, ctx, status, "", p)
	}
	if a.deps.Services == nil || a.deps.Services.Verify(ctx.Header(ServiceTokenHeader), ServiceAI) != nil {
		refuse(http.StatusUnauthorized, problem(CodeUnauthenticated, "The call is not from the AI service.",
			"Only the AI service calls the internal API, with its service token."))
		return
	}
	p, err := a.deps.Auth.Authenticate(ctx.Context(), authn.Credentials{Authorization: ctx.Header("Authorization")})
	if err != nil && !errors.Is(err, authn.ErrUnauthenticated) {
		logFailure(ctx.Context(), err)
		refuse(http.StatusInternalServerError, problem(CodeFailure, "The turn's token could not be checked.", "Try again in a moment."))
		return
	}
	if err != nil || p.Turn == uuid.Nil || !p.AI() {
		refuse(http.StatusUnauthorized, problem(CodeUnauthenticated, "The call has no live turn's token.",
			"Send the delegated read token the API gave the turn; it ends with the turn and after 5 minutes."))
		return
	}
	c := authn.WithPrincipal(ctx.Context(), p)
	audit := storage.AuditFrom(c)
	audit.Actor, audit.OnBehalfOf = &storage.Actor{Type: "ai", ID: p.Client}, &storage.Actor{Type: "user", ID: p.UserID.String()}
	audit.Request.Session = p.Client
	next(huma.WithContext(ctx, storage.WithAudit(c, audit)))
}

// internalOp registers an internal operation with the error codes it returns, as register does for the public API's:
// every refusal a problem with its stable code and the request's instance.
func internalOp[I, O any](a *API, op huma.Operation, codes []string, h func(context.Context, *I) (*O, error)) {
	withCodes(&op, codes)
	huma.Register(a.internal, op, func(ctx context.Context, in *I) (*O, error) {
		out, err := h(ctx, in)
		if err != nil {
			return nil, internalProblem(ctx, err)
		}
		return out, nil
	})
}

// withCodes names on op the error codes it returns, and their statuses.
func withCodes(op *huma.Operation, codes []string) {
	codes = slices.Compact(slices.Sorted(slices.Values(append([]string{CodeUnauthenticated, CodeFailure, CodeInvalid}, codes...))))
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
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-fyniq-error-codes"] = codes
}

// internalProblem is err as the internal API answers it: a problem with the request's instance.
func internalProblem(ctx context.Context, err error) *Problem {
	pr := problemFrom(ctx, err)
	if pr.Instance == "" {
		pr.Instance = instance(ctx)
	}
	return pr
}
