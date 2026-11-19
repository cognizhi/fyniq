package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Connected apps (PRD-001 AI-44, AI-35, AI-90; p1-mcp-first-party task 9.1): what a person and an Owner see and control about the AI
// apps that work for them: the apps the organisation allows, the person's own connections with their scopes, the registrations waiting
// for an Owner, and the trail of calls. Switching MCP on, allowing an app and opening registration are settings (dual controlled); here
// a person revokes their own connection, an Owner denies a registration, and everyone reads. Never on the AI's or MCP's tool list.

// The codes of Connected apps' refusals (ENG-23).
const (
	CodeConnectedNoUser   = "MCP-APPS-01" // not a person who is a member
	CodeConnectedNoRegist = "MCP-APPS-02" // registration is not on for this organisation, or the registration is not pending
)

// MCPScopeName is a scope an AI app was granted.
type MCPScopeName string

// Schema implements huma.SchemaProvider.
func (MCPScopeName) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A scope an AI app was granted.", Examples: []any{"fyniq.read.reports"}}
}

// ConnectedApp is an AI app the organisation can allow.
type ConnectedApp struct {
	ID        string `json:"id" doc:"The app's client ID: its name on the allowlist for a first-party app, mcp-… for one that registered itself." example:"claude-code"`
	Name      string `json:"name" doc:"The app's name." example:"Claude Code"`
	Publisher string `json:"publisher,omitempty" doc:"The publisher Fyniq has verified; absent for an app that registered itself, which Fyniq has not verified." example:"Anthropic"`
	Verified  bool   `json:"verified" doc:"Whether Fyniq verified who publishes it: only the apps the release pre-registered." example:"true"`
	Allowed   bool   `json:"allowed" doc:"Whether the organisation's allowlist names it now." example:"true"`
	Status    string `json:"status" enum:"first_party,pending,approved,denied,expired" doc:"first_party: pre-registered by the release; the others are a self-registered app's course." example:"first_party"`
}

// MyConnection is what the caller has granted an AI app.
type MyConnection struct {
	ClientID  string         `json:"client_id" doc:"The app's client ID." example:"claude-code"`
	Name      string         `json:"name" doc:"The app's name." example:"Claude Code"`
	Scopes    []MCPScopeName `json:"scopes" nullable:"false" maxItems:"30" doc:"What you allowed it."`
	GrantedAt time.Time      `json:"granted_at" doc:"When you allowed it." example:"2026-11-19T09:30:00Z"`
	ExpiresAt time.Time      `json:"expires_at" doc:"When the consent must be renewed: 90 days after you gave it (AI-37)." example:"2027-02-17T09:30:00Z"`
}

// ConnectedAppsView is the Connected apps page.
type ConnectedAppsView struct {
	Access         MCPAccess      `json:"access" doc:"Your opt-in, the organisation's switch and its allowlist."`
	RegistrationOn bool           `json:"registration_on" doc:"Whether an Owner has turned on registration of AI apps (mcp.registration)." example:"false"`
	Apps           []ConnectedApp `json:"apps" nullable:"false" maxItems:"60" doc:"The apps this release pre-registers and those the organisation's allowlist names."`
	Connections    []MyConnection `json:"connections" nullable:"false" maxItems:"60" doc:"What you have allowed AI apps."`
}

type connectedAppsOutput struct{ Body ConnectedAppsView }

// MCPRegistration is a self-registered app waiting for an Owner.
type MCPRegistration struct {
	ID           string        `json:"id" doc:"Its client ID." example:"mcp-0123456789abcdef0123"`
	Name         string        `json:"name" doc:"The name it gave itself: Fyniq has not verified it." example:"Acme Books Assistant"`
	RedirectURIs []RedirectURI `json:"redirect_uris" nullable:"false" maxItems:"5" doc:"Where it sends you to sign in: check this before you allow it."`
	RegisteredAt time.Time     `json:"registered_at" doc:"When it registered." example:"2026-11-19T09:30:00Z"`
}

// MCPRegistrations are the registrations waiting for an Owner.
type MCPRegistrations struct {
	Registrations []MCPRegistration `json:"registrations" nullable:"false" maxItems:"100" doc:"Newest first; an Owner adds one to the apps that may connect to approve it, or denies it."`
}

// MemberConnection is what a person allowed an AI app, as an Owner sees it.
type MemberConnection struct {
	UserID string `json:"user_id" format:"uuid" doc:"Whose connection it is." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	Person string `json:"person" doc:"Their name." example:"Ah Kow"`
	MyConnection
}

// MemberConnections are everyone's connections.
type MemberConnections struct {
	Connections []MemberConnection `json:"connections" nullable:"false" maxItems:"600" doc:"Each person's allowed AI apps, by person."`
}

type (
	memberConnectionsOutput struct{ Body MemberConnections }
	// MemberConnectionPath names a person's connection to an app.
	MemberConnectionPath struct {
		UserID   string `path:"user_id" format:"uuid" doc:"The person." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
		ClientID string `path:"client_id" maxLength:"64" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The app's client ID." example:"claude-code"`
	}
	revokeMemberConnectionInput struct{ MemberConnectionPath }
)

// MCPCallRow is one request an AI app made.
type MCPCallRow struct {
	ID         string    `json:"id" format:"uuid" doc:"The call's ID." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	User       string    `json:"user_id" format:"uuid" doc:"Whose app it was." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	App        string    `json:"app" doc:"The app." example:"claude-code"`
	AppVersion string    `json:"app_version,omitempty" doc:"As the app reports itself." example:"claude-code/2.1.327"`
	Tool       string    `json:"tool" doc:"The tool." example:"metrics.query"`
	Outcome    string    `json:"outcome" enum:"read,refused,limited,failed" doc:"What became of it." example:"read"`
	Code       string    `json:"code,omitempty" doc:"The refusal's or failure's code." example:"MCP-SCOPE-01"`
	Facts      int       `json:"facts" doc:"How many figures it returned." example:"1"`
	Network    string    `json:"network,omitempty" doc:"The caller's network, never its address." example:"203.0.113.0/24"`
	At         time.Time `json:"at" doc:"When." example:"2026-11-19T09:30:00Z"`
}

// MCPActivity is the trail of AI apps' calls.
type MCPActivity struct {
	Calls []MCPCallRow `json:"calls" nullable:"false" maxItems:"100" doc:"Newest first: the last 100."`
}

type (
	ConnectedAppIDPath struct {
		ClientID string `path:"client_id" maxLength:"64" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The app's client ID." example:"claude-code"`
	}
	revokeConnectionInput struct{ ConnectedAppIDPath }
	denyRegistrationInput struct{ ConnectedAppIDPath }
	registrationsOutput   struct{ Body MCPRegistrations }
	activityInput         struct {
		All bool `query:"all" doc:"Every person's calls in the organisation, for an Owner; otherwise your own." example:"false"`
	}
	activityOutput struct{ Body MCPActivity }
	noContent      struct{}
)

func registerConnectedApps(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetConnectedApps, huma.Operation{
		OperationID: "getConnectedApps", Method: http.MethodGet, Path: "/v1/connected-apps", Tags: []string{"Connected apps"},
		Summary: "See the AI apps that work for you",
		Description: "The Connected apps page (AI-44): your opt-in, the organisation's switch and its allowlist, the apps it can allow (those the " +
			"release pre-registers and any that registered themselves and were listed), and what you have allowed each. Read-only; no side effects.",
	}, append([]string{CodeConnectedNoUser}, locks...), func(ctx context.Context, _ *struct{}) (*connectedAppsOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeConnectedNoUser, "Only a person who is a member sees their connected apps.", "Sign in as yourself")
		}
		var view ConnectedAppsView
		var listed []string
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, ok, err := u.MCPOptIns().Latest(ctx, p.UserID)
			if err != nil {
				return err
			}
			on, apps, err := mcpOrganisation(ctx, a, u)
			if err != nil {
				return err
			}
			if ok {
				view.Access = mcpAccessOf(&o, on, apps)
			} else {
				view.Access = mcpAccessOf(nil, on, apps)
			}
			listed = apps
			view.RegistrationOn, err = a.registrationOn(ctx, u)
			return err
		})
		if err != nil {
			return nil, err
		}
		view.Apps, err = a.connectedApps(ctx, listed)
		if err != nil {
			return nil, err
		}
		view.Connections = []MyConnection{}
		if a.deps.OAuthConsents != nil {
			consents, err := a.deps.OAuthConsents.AgentConsents(ctx, p.UserID.String())
			if err != nil {
				return nil, err
			}
			names := map[string]string{}
			for _, app := range view.Apps {
				names[app.ID] = app.Name
			}
			for _, c := range consents {
				scopes := []MCPScopeName{}
				for _, s := range c.Scopes {
					scopes = append(scopes, MCPScopeName(s))
				}
				name := c.ClientName
				if n := names[c.ClientID]; n != "" {
					name = n
				}
				view.Connections = append(view.Connections, MyConnection{ClientID: c.ClientID, Name: name, Scopes: scopes, GrantedAt: c.GrantedAt.UTC(),
					ExpiresAt: c.GrantedAt.UTC().Add(90 * 24 * time.Hour)})
			}
		}
		return &connectedAppsOutput{Body: view}, nil
	})

	register(a, authz.RevokeMyConnection, exempt(huma.Operation{
		OperationID: "revokeMyConnection", Method: http.MethodDelete, Path: "/v1/connected-apps/connections/{client_id}", Tags: []string{"Connected apps"},
		Summary: "Disconnect an AI app", DefaultStatus: http.StatusNoContent,
		Description: "Revokes what you allowed one AI app: its access and refresh tokens end at once (AI-44). Only your own; idempotent. " +
			"Side effects: the app's tokens at the OAuth server. Never on the AI's or MCP's tool list.",
	}, "Prefer", "A revocation is idempotent and takes effect at once; a preview would only repeat the answer (AI-44)."), append([]string{CodeConnectedNoUser}, locks...), func(ctx context.Context, in *revokeConnectionInput) (*noContent, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser || a.deps.OAuthConsents == nil {
			return nil, problem(CodeConnectedNoUser, "Only a person who is a member disconnects their own apps.", "Sign in as yourself")
		}
		if err := a.deps.OAuthConsents.RevokeClientConsent(ctx, p.UserID.String(), in.ClientID); err != nil {
			return nil, err
		}
		return &noContent{}, nil
	})

	register(a, authz.ListMCPConnections, exempt(huma.Operation{
		OperationID: "listMCPConnections", Method: http.MethodGet, Path: "/v1/connected-apps/connections", Tags: []string{"Connected apps"},
		Summary: "See what everyone allowed AI apps",
		Description: "For an Owner (AI-44): each member's allowed AI apps with the scopes they gave and when the consent must be renewed. Read-only; no side " +
			"effects. Never on the AI's or MCP's tool list.",
	}, "cursor", "A connection is a person and an app, so the list is a few dozen read whole; it is capped at 600 (AI-44)."), locks, func(ctx context.Context, _ *struct{}) (*memberConnectionsOutput, error) {
		p, _ := authn.From(ctx)
		out := &memberConnectionsOutput{Body: MemberConnections{Connections: []MemberConnection{}}}
		if a.deps.OAuthConsents == nil {
			return out, nil
		}
		var members []storage.Member
		names := map[uuid.UUID]string{}
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if members, err = u.Members().List(ctx, a.deps.Clock.Now()); err != nil {
				return err
			}
			names, err = memberNames(ctx, u, a.deps.Clock.Now())
			return err
		}); err != nil {
			return nil, err
		}
		for _, m := range members {
			consents, err := a.deps.OAuthConsents.AgentConsents(ctx, m.UserID.String())
			if err != nil {
				return nil, err
			}
			for _, c := range consents {
				scopes := []MCPScopeName{}
				for _, s := range c.Scopes {
					scopes = append(scopes, MCPScopeName(s))
				}
				out.Body.Connections = append(out.Body.Connections, MemberConnection{UserID: m.UserID.String(), Person: names[m.UserID], MyConnection: MyConnection{
					ClientID: c.ClientID, Name: c.ClientName, Scopes: scopes, GrantedAt: c.GrantedAt.UTC(), ExpiresAt: c.GrantedAt.UTC().Add(90 * 24 * time.Hour)}})
			}
		}
		return out, nil
	})

	register(a, authz.RevokeMemberConnection, exempt(exempt(huma.Operation{
		OperationID: "revokeMemberConnection", Method: http.MethodDelete, Path: "/v1/connected-apps/people/{user_id}/connections/{client_id}", Tags: []string{"Connected apps"},
		Summary: "Disconnect a person's AI app", DefaultStatus: http.StatusNoContent,
		Description: "An Owner revokes what one member allowed one AI app: its access and refresh tokens end at once (AI-44, AI-38). It narrows, so it is never held " +
			"for a second person; idempotent. Only a member of this organisation. Side effects: the app's tokens at the OAuth server and an audit event. Never on " +
			"the AI's or MCP's tool list.",
	}, "Idempotency-Key", "A revocation is idempotent, so a retry is safe without a key (AI-44)."), "Prefer", "A revocation narrows and takes effect at once; a preview would only repeat the answer (AI-44)."),
		append([]string{CodeNotFound, CodeConnectedNoUser, CodeInvalid}, locks...), func(ctx context.Context, in *revokeMemberConnectionInput) (*noContent, error) {
			p, _ := authn.From(ctx)
			user, err := uuid.Parse(in.UserID)
			if err != nil || a.deps.OAuthConsents == nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "user_id", Message: "is not a person's ID", Fix: "Use an ID from the connections list"}})
			}
			var member bool
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				ms, err := u.Members().List(ctx, a.deps.Clock.Now())
				for _, m := range ms {
					member = member || m.UserID == user
				}
				if err != nil || !member {
					return err
				}
				return u.Audit().Record(ctx, storage.AuditRecord{Action: "mcp.connection_revoked", ObjectType: "organisation", ObjectID: p.Organisation,
					Changes: map[string][2]any{"user": {nil, user.String()}, "client": {nil, in.ClientID}}})
			}); err != nil {
				return nil, err
			}
			if !member {
				return nil, ports.ErrNotFound
			}
			if err := a.deps.OAuthConsents.RevokeClientConsent(ctx, user.String(), in.ClientID); err != nil {
				return nil, err
			}
			return &noContent{}, nil
		})

	register(a, authz.ListMCPRegistrations, exempt(huma.Operation{
		OperationID: "listMCPRegistrations", Method: http.MethodGet, Path: "/v1/connected-apps/registrations", Tags: []string{"Connected apps"},
		Summary: "See the AI apps waiting for an Owner",
		Description: "The self-registered apps nobody has approved yet (AI-90), only while registration is on for the organisation: each one's name, which " +
			"Fyniq has not verified, and where it sends people to sign in. An Owner approves one by adding its ID to the apps that may connect, " +
			"or denies it. Read-only; no side effects.",
	}, "cursor", "The registrations waiting for an Owner are capped at 100 and read whole, like a short inbox (AI-90)."), locks, func(ctx context.Context, _ *struct{}) (*registrationsOutput, error) {
		p, _ := authn.From(ctx)
		out := &registrationsOutput{Body: MCPRegistrations{Registrations: []MCPRegistration{}}}
		var on bool
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			on, err = a.registrationOn(ctx, u)
			return err
		})
		if err != nil || !on || a.deps.MCPClients == nil {
			return out, err
		}
		a.expireMCPClients(ctx, a.deps.Clock.Now())
		pending, err := a.deps.MCPClients.PendingClients(ctx)
		if err != nil {
			return nil, err
		}
		for _, c := range pending {
			out.Body.Registrations = append(out.Body.Registrations, MCPRegistration{ID: c.ID, Name: c.Name, RedirectURIs: redirects(c.RedirectURIs), RegisteredAt: c.RegisteredAt.UTC()})
		}
		return out, nil
	})

	register(a, authz.DenyMCPRegistration, exempt(exempt(huma.Operation{
		OperationID: "denyMCPRegistration", Method: http.MethodPost, Path: "/v1/connected-apps/registrations/{client_id}/deny", Tags: []string{"Connected apps"},
		Summary: "Deny a registered AI app", DefaultStatus: http.StatusNoContent,
		Description: "Denies a pending registration, once: the app is removed from the OAuth server and can never connect (AI-90). Never held back for a second " +
			"person: it narrows. Side effects: the registration's status and the client at the OAuth server. Never on the AI's or MCP's tool list.",
	}, "Idempotency-Key", "A denial happens once and a second call answers that nothing is waiting, so a retry is safe without a key (AI-90)."), "Prefer", "A denial narrows and takes effect at once, never held back for a second person (AI-90)."), append([]string{CodeConnectedNoRegist}, locks...), func(ctx context.Context, in *denyRegistrationInput) (*noContent, error) {
		p, _ := authn.From(ctx)
		var on bool
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			on, err = a.registrationOn(ctx, u)
			return err
		}); err != nil {
			return nil, err
		}
		if !on || a.deps.MCPClients == nil || !strings.HasPrefix(in.ClientID, "mcp-") {
			return nil, problem(CodeConnectedNoRegist, "There is no registration waiting under that ID.", "Open the registrations waiting for an Owner")
		}
		done, err := a.deps.MCPClients.DecideClient(ctx, in.ClientID, storage.MCPClientDenied, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		if !done {
			return nil, problem(CodeConnectedNoRegist, "That registration is not waiting: it was decided or has expired.", "Open the registrations waiting for an Owner")
		}
		a.deps.Log.InfoContext(ctx, "AI app registration denied", "event", "mcp.registration_denied", "client", in.ClientID, "by", p.UserID.String())
		if a.deps.OAuthClients != nil {
			if err := a.deps.OAuthClients.RemoveClient(ctx, in.ClientID); err != nil {
				a.deps.Log.WarnContext(ctx, "mcp.denied_client_not_removed", "client", in.ClientID, "error", err.Error())
			}
		}
		return &noContent{}, nil
	})

	register(a, authz.ListMCPActivity, exempt(huma.Operation{
		OperationID: "listMCPActivity", Method: http.MethodGet, Path: "/v1/connected-apps/activity", Tags: []string{"Connected apps"},
		Summary: "See what AI apps did",
		Description: "The trail of AI apps' calls (AI-44): which app, which tool, what became of it, and from which network, newest first, the last 100. " +
			"Yours; with all=true, everyone's in the organisation, for the people who may see Connected apps. It holds no token, argument or result. " +
			"Read-only; no side effects.",
	}, "cursor", "The trail is the latest 100 calls, read whole: the full record is in the activity log's export (AI-44)."), append([]string{CodeForbidden}, locks...), func(ctx context.Context, in *activityInput) (*activityOutput, error) {
		p, _ := authn.From(ctx)
		who := p.UserID
		if in.All {
			if !authz.Decide(authz.ListIntegrationClients, p.Roles, p.AI()).Allowed {
				return nil, problem(CodeForbidden, "Only those who may see Connected apps see everyone's activity.", "Ask an Owner")
			}
			who = uuid.Nil
		} else if p.UserID == uuid.Nil {
			return nil, problem(CodeConnectedNoUser, "Only a person who is a member sees their own apps' activity.", "Sign in as yourself")
		}
		out := &activityOutput{Body: MCPActivity{Calls: []MCPCallRow{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rows, err := u.MCPCalls().List(ctx, who, 100)
			for _, r := range rows {
				out.Body.Calls = append(out.Body.Calls, MCPCallRow{ID: r.ID.String(), User: r.UserID.String(), App: r.App, AppVersion: r.AppVersion, Tool: r.Tool,
					Outcome: r.Outcome, Code: r.Code, Facts: r.Facts, Network: r.Network, At: r.At.UTC()})
			}
			return err
		})
		return out, err
	})
}

// registrationOn is whether this organisation has registration on today (mcp.registration).
func (a *API) registrationOn(ctx context.Context, u storage.UnitOfWork) (bool, error) {
	return settingOnBool(ctx, a, u, "mcp.registration")
}

// connectedApps are the apps the organisation can allow: the release's first-party apps, and the registered apps its allowlist names.
func (a *API) connectedApps(ctx context.Context, listed []string) ([]ConnectedApp, error) {
	first, err := mcpgate.Clients()
	if err != nil {
		return nil, err
	}
	out := make([]ConnectedApp, 0, len(first)+len(listed))
	var registered []string
	for _, c := range first {
		out = append(out, ConnectedApp{ID: c.ID, Name: c.Name, Publisher: c.Publisher, Verified: true, Allowed: slices.Contains(listed, c.ID), Status: "first_party"})
	}
	for _, id := range listed {
		if mcpgate.Registered(id) {
			registered = append(registered, id)
		}
	}
	if len(registered) > 0 && a.deps.MCPClients != nil {
		named, err := a.deps.MCPClients.NamedClients(ctx, registered)
		if err != nil {
			return nil, err
		}
		for _, n := range named {
			out = append(out, ConnectedApp{ID: n.ID, Name: n.Name, Allowed: true, Status: n.Status})
		}
	}
	return out, nil
}
