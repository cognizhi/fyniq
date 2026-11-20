package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clientip"
	"github.com/cognizhi/fyniq/internal/mcpregister"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Client registration (PRD-001 AI-90; p1-mcp-first-party design D14): a registration endpoint of Fyniq's own, RFC 7591, in front of
// the OAuth server's admin API. It is closed until an Owner turns it on with a second approver (the setting mcp.registration) and
// while the MCP server is off in this deployment; it checks every request against the policy (internal/mcpregister), limits each
// network to so many an hour and a day and the installation to so many waiting, creates the client at the OAuth server as an
// agent-class public client with the default read scope alone, and records it as pending: it cannot connect until an Owner adds it
// to the apps that may connect (AI-35). The OAuth server's own registration stays off. Nothing a client names is fetched.

// The codes of a registration's refusal (ENG-23).
const (
	CodeRegClosed  = "MCP-REG-01" // registration is closed: no Owner has turned it on, or the MCP server is off
	CodeRegPolicy  = "MCP-REG-02" // the request breaks the policy
	CodeRegLimit   = "MCP-REG-03" // too many from this network, or too many waiting
	CodeRegBackend = "MCP-REG-04" // the OAuth server did not take it
)

// The limits of one registration's counting (design D14).
const (
	regPerHour    = 5
	regPerDay     = 20
	regPendingCap = 50
)

// OAuthClientRegistration is a client's registered metadata, as RFC 7591 answers it.
type OAuthClientRegistration struct {
	ClientID                string        `json:"client_id" doc:"The client's ID at Fyniq's OAuth server." example:"mcp-0123456789abcdef0123"`
	ClientIDIssuedAt        int64         `json:"client_id_issued_at" doc:"When it was issued, in seconds since 1970." example:"1763517600"`
	ClientName              string        `json:"client_name" doc:"The name it registered with." example:"Acme Books Assistant"`
	RedirectURIs            []RedirectURI `json:"redirect_uris" nullable:"false" maxItems:"5" doc:"The exact redirect URIs it may use."`
	GrantTypes              []OAuthWord   `json:"grant_types" nullable:"false" maxItems:"2" doc:"Always the authorisation code and refresh tokens."`
	ResponseTypes           []OAuthWord   `json:"response_types" nullable:"false" maxItems:"1" doc:"Always code."`
	TokenEndpointAuthMethod string        `json:"token_endpoint_auth_method" doc:"Always none: a public client with PKCE." example:"none"`
	Scope                   string        `json:"scope" doc:"What it may ask for: the default read access only." example:"openid offline_access fyniq.read.reports"`
}

// RedirectURI is an exact redirect URI of a registered client.
type RedirectURI string

// Schema implements huma.SchemaProvider.
func (RedirectURI) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An exact https or loopback redirect URI.", Examples: []any{"https://acme.example/oauth/callback"}}
}

// OAuthWord is a grant or response type of a registered client.
type OAuthWord string

// Schema implements huma.SchemaProvider.
func (OAuthWord) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An OAuth grant or response type.", Examples: []any{"authorization_code"}}
}

// RegistrationBody is a registration as the client sent it (RFC 7591), kept whole: the policy reads it strictly, so a field it does
// not know is refused by name instead of dropped.
type RegistrationBody struct{ json.RawMessage }

// Schema implements huma.SchemaProvider.
func (RegistrationBody) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "object", Description: "The client's metadata (RFC 7591): client_name, redirect_uris and, optionally, grant_types, response_types, token_endpoint_auth_method, scope and application_type. Anything else is refused.",
		Examples: []any{map[string]any{"client_name": "Acme Books Assistant", "redirect_uris": []any{"https://acme.example/oauth/callback"}, "token_endpoint_auth_method": "none"}}}
}

type registerClientInput struct {
	Body RegistrationBody
}

type registerClientOutput struct{ Body OAuthClientRegistration }

func registerOAuthRegistration(a *API) {
	register(a, authz.RegisterOAuthClient, exempt(exempt(huma.Operation{
		OperationID: "registerOAuthClient", Method: http.MethodPost, Path: "/v1/oauth/register", Tags: []string{"Connected apps"}, DefaultStatus: http.StatusCreated,
		Summary: "Register an AI app (RFC 7591)",
		Description: "Lets an AI app ask to be registered as an OAuth client (AI-90). Closed until an Owner turns it on with a second approver, " +
			"and while the MCP server is off. Every request is checked against the policy: a public client with PKCE, exact https or loopback " +
			"redirect URIs, nothing the server would fetch or trust, and a name that cannot pass for Fyniq or a known app. The client is created " +
			"as an agent with the default read scope alone, unverified, and cannot connect until an Owner adds it to the apps that may " +
			"connect. Limited by network and in total. Side effects: the client at the OAuth server and its record. Not idempotent: each call " +
			"is a new client. Never on the AI's or MCP's tool list.",
	}, "Idempotency-Key", "RFC 7591 clients send none, and each call is meant to make a new client, limited by network (AI-90)."),
		"Prefer", "A preview would show a client ID that does not exist and spend the network's registration allowance (AI-90)."), []string{CodeRegClosed, CodeRegPolicy, CodeRegLimit, CodeRegBackend, CodeRateLimited}, func(ctx context.Context, in *registerClientInput) (*registerClientOutput, error) {
		if !a.deps.MCPServerOn || a.deps.MCPClients == nil || a.deps.OAuthClients == nil || a.deps.MCPAudience == "" {
			return nil, regRefused(CodeRegClosed, "AI apps cannot register here.", "An Owner opens registration in Settings, Connected apps")
		}
		now := a.deps.Clock.Now()
		open, err := a.deps.MCPClients.RegistrationOpen(ctx, now)
		if err != nil {
			return nil, err
		}
		if !open {
			return nil, regRefused(CodeRegClosed, "Registration is closed: no Owner has turned it on.", "Ask an Owner to turn on \"AI apps may register themselves\"")
		}
		req, problems := mcpregister.Decode(bytes.NewReader(in.Body.RawMessage))
		if len(problems) == 0 {
			pol, err := mcpregister.Load()
			if err != nil {
				return nil, err
			}
			var reg mcpregister.Registration
			if reg, problems = pol.Check(req); len(problems) == 0 {
				return a.registerClient(ctx, reg, now)
			}
		}
		return nil, regPolicy(problems)
	})
}

func (a *API) registerClient(ctx context.Context, reg mcpregister.Registration, now time.Time) (*registerClientOutput, error) {
	network := ""
	if from, ok := clientip.From(ctx); ok {
		network = networkOf(from.IP.String())
	}
	return a.registerClientFrom(ctx, reg, now, network)
}

// registerClientFrom records and creates the client for a registration counted against network: the registering address's /24 or /48 for a client that registered
// itself (AI-90), the organisation's own count for one an Administrator registered by hand (AI-46).
func (a *API) registerClientFrom(ctx context.Context, reg mcpregister.Registration, now time.Time, network string) (*registerClientOutput, error) {
	a.expireMCPClients(ctx, now)
	id := "mcp-" + hex.EncodeToString(randomBytes(10))
	refused, err := a.deps.MCPClients.Register(ctx, storage.MCPClientRegistration{ID: id, Name: reg.Name, RedirectURIs: reg.RedirectURIs, Network: network, At: now},
		storage.MCPClientLimits{PerHour: regPerHour, PerDay: regPerDay, PendingCap: regPendingCap})
	if err != nil {
		return nil, err
	}
	if refused != "" {
		retry, why := time.Hour, "This network has registered as many apps as it may in an hour."
		switch refused {
		case "limit_day":
			retry, why = 24*time.Hour, "This network has registered as many apps as it may in a day."
		case "pending_full":
			retry, why = time.Hour, "Too many registrations are waiting for an Owner."
		}
		a.deps.Log.WarnContext(ctx, "AI app registration refused", "event", "mcp.registration_limited", "why", refused, "network", network)
		return nil, regRefused(CodeRegLimit, why, "Try again later").withHeader("Retry-After", strconv.Itoa(int(retry/time.Second)))
	}
	if _, err := a.deps.OAuthClients.RegisterPublicClient(ctx, ports.PublicRegistration{ID: id, Name: reg.Name, Class: "agent", Audience: a.deps.MCPAudience,
		Scopes: reg.Scopes, RedirectURIs: reg.RedirectURIs}); err != nil {
		a.deps.Log.ErrorContext(ctx, "mcp.registration_not_created", "client", id, "error", err.Error())
		return nil, regRefused(CodeRegBackend, "The sign-in service did not take the registration.", "Try again in a moment")
	}
	a.deps.Log.InfoContext(ctx, "AI app registered", "event", "mcp.registered", "client", id, "name", reg.Name, "network", network, "redirects", len(reg.RedirectURIs))
	return &registerClientOutput{Body: OAuthClientRegistration{ClientID: id, ClientIDIssuedAt: now.Unix(), ClientName: reg.Name,
		RedirectURIs: redirects(reg.RedirectURIs), GrantTypes: []OAuthWord{"authorization_code", "refresh_token"}, ResponseTypes: []OAuthWord{"code"}, TokenEndpointAuthMethod: "none",
		Scope: strings.Join(reg.Scopes, " ")}}, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("api: no randomness: " + err.Error())
	}
	return b
}

// regRefused is a refusal as RFC 7591 says it: the problem, and the OAuth error beside it.
func regRefused(code, detail, fix string) *Problem {
	p := problem(code, detail, fix)
	p.OAuthError, p.OAuthErrorDescription = "invalid_client_metadata", detail
	if code == CodeRegClosed {
		p.OAuthError = "access_denied"
	}
	return p
}

// regPolicy is every problem of a registration at once.
func regPolicy(ps []mcpregister.Problem) *Problem {
	fields := make([]FieldProblem, len(ps))
	for i, x := range ps {
		fields[i] = FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix}
	}
	p := problem(CodeRegPolicy, "The registration breaks the policy: "+ps[0].Message, ps[0].Fix)
	p.Errors = fields
	p.OAuthError, p.OAuthErrorDescription = ps[0].Code, ps[0].Message
	return p
}

// expireMCPClients marks the registrations that lapsed as expired and removes their clients from the OAuth server, so a lapsed
// registration cannot sign anyone in. It runs as registrations come in and when an Owner looks at the pending ones, and hourly as
// the platform job mcp.registrations.expire; a failure is logged and never stops the request: the next run takes what is left.
func (a *API) expireMCPClients(ctx context.Context, now time.Time) {
	if a.deps.MCPClients == nil || a.deps.OAuthClients == nil {
		return
	}
	if _, err := mcpregister.Expire(ctx, a.deps.MCPClients, a.deps.OAuthClients, now, a.deps.Log); err != nil {
		a.deps.Log.WarnContext(ctx, "mcp.registrations_not_expired", "error", err.Error())
	}
}

func redirects(uris []string) []RedirectURI {
	out := make([]RedirectURI, len(uris))
	for i, u := range uris {
		out[i] = RedirectURI(u)
	}
	return out
}
