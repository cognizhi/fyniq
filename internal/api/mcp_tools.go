package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/domain/aiexclusion"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/mcpsurface"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The MCP server's way in (PRD-001 §9.7, AI-16, AI-36, AI-37, AI-41; p1-mcp-first-party design D2, D3): one internal operation
// that runs a read tool for a person an MCP client acts for. The MCP service holds no database and no write credential, so
// every gate is here. A call needs the MCP service's own service token (a class no other service holds) and the delegated
// API token the MCP service obtained for the person (open question 40, option (b)): an agent-class principal with the
// person's rights now and the scopes the MCP token was granted. It then passes, in this order: the tool is one the manifest
// exposes; the token holds every scope this tool and this input need (a generic tool is mapped by what it reads); the
// organisation's switch, the allowlist and the person's opt-in allow the app (the same gate consent passed, asked again on
// every call, so a switch, an opt-out or a removal stops the next call at once); and the tool runs through its public read
// operation as the person, so the roles, masking and the AI exclusions apply, exactly as for the assistant. Its result is cut
// to the tool's limits and to the organisation's size ceiling, its free text is quoted as untrusted and screened, and an NRIC
// or a bank number in it is masked before it leaves.

// ServiceMCP is the class of the MCP service's service tokens: for the MCP operations alone.
const ServiceMCP = "mcp"

// The codes of an MCP call's refusals (ENG-23).
const (
	CodeMCPScope     = "MCP-SCOPE-01" // the token lacks a scope this tool and input need, or the tool is not exposed
	CodeMCPNotAgent  = "MCP-CALL-02"  // the caller is not an agent-class delegated token for a person
	CodeMCPTooLarge  = "MCP-CALL-03"  // the result is larger than the organisation's ceiling
	headerNeedScopes = "X-Fyniq-Needed-Scopes"
)

// MCPToolCall is a read tool the MCP service asks for on behalf of a client.
type MCPToolCall struct {
	App   string         `json:"app" minLength:"1" maxLength:"64" doc:"The OAuth client the MCP token was issued to: its name on the organisation's allowlist." example:"claude-code"`
	Tool  AIToolName     `json:"tool" doc:"The read tool, by its registry name." example:"records.search"`
	Input map[string]any `json:"input" doc:"The input the client gave it, checked against the tool's input schema."`
}

type mcpToolCallInput struct{ Body MCPToolCall }

type mcpToolCallOutput struct{ Body AIToolResult }

func registerMCPTools(a *API) {
	internalOp(a, huma.Operation{
		OperationID: "callMCPTool", Method: http.MethodPost, Path: internalBase + "/mcp/tool-calls", Tags: []string{"Assistant"},
		Summary: "Run a read tool for an MCP client",
		Description: "Runs one read tool for the person an MCP client acts for (AI-16, AI-36): the caller is the MCP service, with its own " +
			"service token and the delegated API token it obtained for that person. The tool must be one the MCP manifest exposes; the " +
			"token must hold every scope this tool and this input need (403 with the scopes in the X-Fyniq-Needed-Scopes header); the " +
			"organisation's switch, the allowlist and the person's opt-in must allow the app, checked on every call. The tool then runs " +
			"as the person, with the AI exclusions and masking, and its result is cut to the tool's limits and the organisation's size " +
			"ceiling, with an NRIC or a bank number masked. Nothing is written. Never reachable by the AI service or a turn's token.",
	}, []string{CodeToolInput, CodeToolRefused, CodeToolTimeout, CodeMCPScope, CodeMCPNotAgent, CodeMCPTooLarge, CodeScreenIncomplete,
		mcpgate.CodeOrganisationOff, mcpgate.CodeNotAllowed, mcpgate.CodeNotOptedIn}, func(ctx context.Context, in *mcpToolCallInput) (*mcpToolCallOutput, error) {
		p, _ := authn.From(ctx)
		manifest, scopes := mcpsurface.Default()
		tool, ok := aitool.Default().Get(string(in.Body.Tool))
		exposed := slices.ContainsFunc(manifest.Tools, func(d mcpsurface.Definition) bool { return d.Name == string(in.Body.Tool) })
		if !ok || !exposed {
			return nil, problem(CodeMCPScope, string(in.Body.Tool)+" is not a tool the MCP server exposes.", "List the tools and call one of them")
		}
		input := in.Body.Input
		if input == nil {
			input = map[string]any{}
		}
		need, err := scopes.Needs(tool.Name, input)
		if err != nil {
			return nil, problem(CodeMCPScope, "This call reads something the MCP server does not expose: "+err.Error()+".", "Name a kind of record or a measure the tool lists")
		}
		if missing := slices.DeleteFunc(slices.Clone(need), func(s string) bool { return slices.Contains(p.Scopes, s) }); len(missing) > 0 {
			return nil, problem(CodeMCPScope, "This call needs "+strings.Join(missing, " ")+", which the app was not granted.", "Connect the app again and allow that access").
				withHeader(headerNeedScopes, strings.Join(missing, " "))
		}
		if err := a.mcpAdmit(ctx, p, in.Body.App); err != nil {
			return nil, err
		}
		if err := checkToolInput(tool, input); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(tool.Limits.TimeoutMS)*time.Millisecond)
		defer cancel()
		out, err := a.runTool(ctx, tool, input)
		if ctx.Err() == context.DeadlineExceeded {
			return nil, problem(CodeToolTimeout, fmt.Sprintf("%s took longer than its %d ms.", tool.Name, tool.Limits.TimeoutMS), "Ask for less")
		}
		if err != nil {
			return nil, err
		}
		if err := a.screenResult(ctx, &out); err != nil {
			return nil, err
		}
		out.Tool, out.Version = tool.Name, tool.Version
		if err := a.mcpFit(ctx, p, &out); err != nil {
			return nil, err
		}
		return &mcpToolCallOutput{Body: out}, nil
	})
}

// mcpAdmit asks the gate consent passed, again: the organisation's switch, the allowlist and the person's opt-in. A failure to
// ask refuses (fail closed).
func (a *API) mcpAdmit(ctx context.Context, p authn.Principal, app string) error {
	code, why, err := mcpgate.Gate{Store: a.deps.Store, Clock: a.deps.Clock}.Admit(ctx, p.Organisation, p.UserID, app)
	if err != nil {
		return err
	}
	if code != "" {
		return problem(code, why, "Ask an Owner, or open Settings, Connected apps")
	}
	return nil
}

// mcpFit prepares a result to leave: every text in it through the output filter (an NRIC or a bank number is masked, AI-20,
// AI-41) and the size the organisation allows (mcp.result_kb): a result larger than it is refused with the way to ask for less,
// never cut silently.
func (a *API) mcpFit(ctx context.Context, p authn.Principal, out *AIToolResult) error {
	out.Content = filterJSON(out.Content)
	for i := range out.Facts {
		out.Facts[i].MemberName = aiexclusion.Filter(out.Facts[i].MemberName)
	}
	out.Withheld = aiexclusion.Filter(out.Withheld)
	var kb int
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		changes, err := u.Settings().Changes(ctx, "")
		if err != nil {
			return err
		}
		d, _ := settings.Lookup("mcp.result_kb")
		kb, err = strconv.Atoi(settings.On(d, changes, today).Value)
		return err
	})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if len(raw) > kb<<10 {
		return problem(CodeMCPTooLarge, fmt.Sprintf("The result is %d KB, more than the %d KB an AI app may receive.", len(raw)>>10, kb), "Ask for a shorter period, one member or a narrower search")
	}
	return nil
}

// filterJSON masks, in every string of a decoded JSON value, what the output filter masks. Keys are the API's own names and
// are left alone.
func filterJSON(v any) any {
	switch t := v.(type) {
	case string:
		return aiexclusion.Filter(t)
	case []any:
		for i := range t {
			t[i] = filterJSON(t[i])
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = filterJSON(t[k])
		}
		return t
	}
	return v
}

// mcpAuthenticate accepts a call only from the MCP service, for an agent-class delegated token of a person: never the AI
// service's token, never a turn's or a run's grant, never a person's session, never an integration (AI-34, AI-36).
func (a *API) mcpAuthenticate(ctx huma.Context, next func(huma.Context), refuse func(int, *Problem)) {
	if a.deps.Services == nil || a.deps.Services.Verify(ctx.Header(ServiceTokenHeader), ServiceMCP) != nil {
		refuse(http.StatusUnauthorized, problem(CodeUnauthenticated, "The call is not from the MCP service.", "Only the MCP service calls this operation, with its service token."))
		return
	}
	p, err := a.deps.Auth.Authenticate(ctx.Context(), authn.Credentials{Authorization: ctx.Header("Authorization")})
	if err != nil && !errors.Is(err, authn.ErrUnauthenticated) {
		logFailure(ctx.Context(), err)
		refuse(http.StatusInternalServerError, problem(CodeFailure, "The delegated token could not be checked.", "Try again in a moment."))
		return
	}
	if err != nil {
		refuse(http.StatusUnauthorized, problem(CodeUnauthenticated, "The delegated token is not valid.", "The MCP service obtains one for the person on each call."))
		return
	}
	if p.Via != authn.ViaToken || p.Class != authn.ClassAgent || !p.AI() || p.UserID == uuid.Nil || p.Organisation == uuid.Nil || p.Delegated() != uuid.Nil || p.Scoped {
		refuse(http.StatusForbidden, problem(CodeMCPNotAgent, "Only the delegated token of an agent for a person runs an MCP tool.", "The MCP service obtains one for the person on each call."))
		return
	}
	c := context.WithValue(authn.WithPrincipal(ctx.Context(), p), turnTokenKey{}, ctx.Header("Authorization"))
	audit := storage.AuditFrom(c)
	audit.Actor, audit.OnBehalfOf = &storage.Actor{Type: "ai", ID: p.Client}, &storage.Actor{Type: "user", ID: p.UserID.String()}
	audit.Request.Session = p.Client
	next(huma.WithContext(ctx, storage.WithAudit(c, audit)))
}
