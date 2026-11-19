package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeMCPNoUser refuses an MCP choice from a caller that is not a person who is a member: only a person opts in, never a
// client, an integration or an AI.
const CodeMCPNoUser = "MCP-OPT-01"

// MCPAccess is whether AI apps may work for the caller over MCP (AI-35): the organisation's switch and allowlist, and the
// caller's own opt-in. All three must hold, and the first two are the Owner's, under a second approver.
type MCPAccess struct {
	OptedIn        bool         `json:"opted_in" doc:"Whether you have opted in to AI apps acting for you over MCP: off until you do." example:"false"`
	SetAt          *time.Time   `json:"set_at,omitempty" doc:"When you last chose, if ever." example:"2026-11-19T09:30:00Z"`
	OrganisationOn bool         `json:"organisation_on" doc:"Whether an Owner has turned AI apps through MCP on for the organisation (the setting features.mcp, with a second approver)." example:"false"`
	Apps           []MCPAppName `json:"apps" nullable:"false" maxItems:"20" doc:"The AI apps the organisation allows (the setting mcp.clients): only these can connect." example:"[\"claude-code\"]"`
	CanConnect     bool         `json:"can_connect" doc:"Whether an AI app can connect as you now: the organisation is on, it allows at least one app, and you opted in." example:"false"`
	ETag           string       `json:"etag" doc:"The choice's version, for If-Match." example:"\"9f1c2b7a\""`
}

// MCPAppName is an AI app the release has pre-registered, as the allowlist names it.
type MCPAppName string

// Schema implements huma.SchemaProvider: a string, with an example.
func (MCPAppName) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An AI app the release has pre-registered, such as claude-code.", Examples: []any{"claude-code"}}
}

// MCPChoice is the caller's choice.
type MCPChoice struct {
	OptedIn bool `json:"opted_in" doc:"true to allow AI apps the organisation allows to act for you over MCP; false to stop them at once." example:"true"`
}

type mcpChoiceInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body MCPChoice
}

type mcpAccessOutput struct {
	ETag     string `header:"ETag" doc:"The choice's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     MCPAccess
}

type mcpAccessReadOutput struct {
	ETag string `header:"ETag" doc:"The choice's version, for If-Match."`
	Body MCPAccess
}

func mcpOptInETag(o *storage.MCPOptIn) string {
	if o == nil {
		return etag("none")
	}
	return etag(o.SetAt.UnixMicro(), o.OptedIn)
}

// mcpOrganisation reads the organisation's MCP switch and allowlist today (AI-35).
func mcpOrganisation(ctx context.Context, a *API, u storage.UnitOfWork) (on bool, apps []string, err error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return false, nil, err
	}
	changes, err := u.Settings().Changes(ctx, "")
	if err != nil {
		return false, nil, err
	}
	value := func(key string) string {
		d, _ := settings.Lookup(key)
		return settings.On(d, changes, today).Value
	}
	apps = []string{}
	for _, c := range strings.Split(value("mcp.clients"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			apps = append(apps, c)
		}
	}
	return value("features.mcp") == "true", apps, nil
}

// mcpAccessOf is a person's access as the API shows it.
func mcpAccessOf(o *storage.MCPOptIn, on bool, apps []string) MCPAccess {
	out := MCPAccess{OrganisationOn: on, Apps: []MCPAppName{}, ETag: mcpOptInETag(o)}
	for _, a := range apps {
		out.Apps = append(out.Apps, MCPAppName(a))
	}
	if o != nil {
		at := o.SetAt.UTC()
		out.OptedIn, out.SetAt = o.OptedIn, &at
	}
	out.CanConnect = out.OrganisationOn && len(out.Apps) > 0 && out.OptedIn
	return out
}

func registerMCPOptIn(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetMyMCP, huma.Operation{
		OperationID: "getMyMCP", Method: http.MethodGet, Path: "/v1/me/mcp", Tags: []string{"Connected apps"},
		Summary: "Get whether AI apps may work for you over MCP",
		Description: "Whether AI apps, such as Claude Code or Claude Desktop, can work with the books as you (AI-35): your own opt-in, " +
			"which is off until you choose, and the organisation's switch and allowlist, which an Owner sets with a second approver. " +
			"Everything an app does as you is still limited to what you may see, and it can only propose changes, which a person " +
			"approves in Fyniq (AI-16). Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*mcpAccessReadOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeMCPNoUser, "Only a person who is a member chooses whether AI apps work for them.", "Sign in as yourself")
		}
		var out MCPAccess
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
				out = mcpAccessOf(&o, on, apps)
			} else {
				out = mcpAccessOf(nil, on, apps)
			}
			return nil
		})
		return &mcpAccessReadOutput{ETag: out.ETag, Body: out}, err
	})

	register(a, authz.SetMyMCP, huma.Operation{
		OperationID: "setMyMCP", Method: http.MethodPut, Path: "/v1/me/mcp", Tags: []string{"Connected apps"},
		Summary: "Allow or stop AI apps working for you over MCP",
		Description: "Opts you in to AI apps acting for you over MCP, or out (AI-35). Opting out stops every app's next call at once. " +
			"It only records your choice: an app still needs the organisation's switch, its place on the allowlist and your consent " +
			"to its scopes, and it can only read what you may and propose what a person approves. Only a person chooses, never a " +
			"client, an integration or an AI (never-list, §9.6). Side effects: your choice and its audit event. Needs If-Match with " +
			"the choice's etag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeMCPNoUser}, locks...), func(ctx context.Context, in *mcpChoiceInput) (*mcpAccessOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeMCPNoUser, "Only a person who is a member chooses whether AI apps work for them.", "Sign in as yourself")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MCPAccess, error) {
			o, ok, err := u.MCPOptIns().Latest(ctx, p.UserID)
			if err != nil {
				return MCPAccess{}, err
			}
			var current *storage.MCPOptIn
			if ok {
				current = &o
			}
			if err := checkIfMatch(in.IfMatch, mcpOptInETag(current)); err != nil {
				return MCPAccess{}, err
			}
			saved, err := u.MCPOptIns().Set(ctx, storage.MCPOptIn{UserID: p.UserID, OptedIn: in.Body.OptedIn})
			if err != nil {
				return MCPAccess{}, err
			}
			on, apps, err := mcpOrganisation(ctx, a, u)
			if err != nil {
				return MCPAccess{}, err
			}
			return mcpAccessOf(&saved, on, apps), nil
		})
		if err != nil {
			return nil, err
		}
		return &mcpAccessOutput{ETag: w.Body.ETag, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
