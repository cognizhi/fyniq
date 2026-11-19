package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
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
	CodeMCPLimit     = "MCP-LIMIT-01" // the app made more calls than the organisation allows (AI-43)
	headerNeedScopes = "X-Fyniq-Needed-Scopes"
)

// MCPToolCall is a read tool the MCP service asks for on behalf of a client.
type MCPToolCall struct {
	App   string         `json:"app" minLength:"1" maxLength:"64" doc:"The OAuth client the MCP token was issued to: its name on the organisation's allowlist." example:"claude-code"`
	Tool  AIToolName     `json:"tool" doc:"The read tool, by its registry name." example:"records.search"`
	Input map[string]any `json:"input" doc:"The input the client gave it, checked against the tool's input schema."`
	// What the MCP service knows of the caller, for the trail (AI-44) and the detection (AI-45): never trusted for a decision.
	IP         string `json:"ip,omitempty" maxLength:"45" doc:"The client's address, as the MCP service saw it; kept only as its network." example:"203.0.113.7"`
	AppVersion string `json:"app_version,omitempty" maxLength:"200" doc:"The client as it reports itself (its user agent)." example:"claude-code/2.1.327"`
	TokenID    string `json:"token_id,omitempty" maxLength:"16" pattern:"^[0-9a-f]{16}$" doc:"The MCP token's identity: the first 16 hex characters of its SHA-256." example:"0123456789abcdef"`
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
	}, []string{CodeToolInput, CodeToolRefused, CodeToolTimeout, CodeMCPScope, CodeMCPNotAgent, CodeMCPTooLarge, CodeMCPLimit, CodeScreenIncomplete,
		mcpgate.CodeOrganisationOff, mcpgate.CodeNotAllowed, mcpgate.CodeNotOptedIn}, func(ctx context.Context, in *mcpToolCallInput) (*mcpToolCallOutput, error) {
		p, _ := authn.From(ctx)
		start := a.deps.Clock.Now()
		out, err := a.runMCPCall(ctx, p, in)
		a.recordMCPCall(ctx, p, in.Body, out, err, start)
		return out, err
	})
}

// runMCPCall is a call through every gate and the limits, then the tool.
func (a *API) runMCPCall(ctx context.Context, p authn.Principal, in *mcpToolCallInput) (*mcpToolCallOutput, error) {
	{
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
		if err := a.mcpLimit(ctx, p, in.Body.App); err != nil {
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
	}
}

// mcpAdmit asks the gate consent passed, again: the organisation's switch, the allowlist and the person's opt-in. A failure to
// ask refuses (fail closed).
func (a *API) mcpAdmit(ctx context.Context, p authn.Principal, app string) error {
	code, why, err := mcpgate.Gate{Store: a.deps.Store, Clock: a.deps.Clock, Clients: a.deps.MCPClients}.Admit(ctx, p.Organisation, p.UserID, app)
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

// mcpLimits are the organisation's limits on one person's app (AI-43), read from its settings.
type mcpLimits struct{ PerMinute, PerHour, PerDay int }

// mcpLimit refuses a call over the organisation's limits for this person's app: requests a minute, calls an hour and a day,
// counted from the trail, so every instance of the API and every MCP instance share one count. It says when to ask again.
func (a *API) mcpLimit(ctx context.Context, p authn.Principal, app string) error {
	var wait time.Duration
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		changes, err := u.Settings().Changes(ctx, "")
		if err != nil {
			return err
		}
		limit := func(key string) int {
			d, _ := settings.Lookup(key)
			n, _ := strconv.Atoi(settings.On(d, changes, today).Value)
			return n
		}
		now := a.deps.Clock.Now()
		for _, w := range []struct {
			key string
			dur time.Duration
		}{{"mcp.requests_per_minute", time.Minute}, {"mcp.tool_calls_per_hour", time.Hour}, {"mcp.tool_calls_per_day", 24 * time.Hour}} {
			n, oldest, err := u.MCPCalls().Count(ctx, p.UserID, app, now.Add(-w.dur), now)
			if err != nil {
				return err
			}
			if max := limit(w.key); max > 0 && n >= max {
				wait = max2(wait, oldest.Add(w.dur).Sub(now))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if wait > 0 {
		secs := int(wait/time.Second) + 1
		return problem(CodeMCPLimit, "This app has made as many requests as the organisation allows for now.", "Wait a little and ask again").
			withHeader("Retry-After", strconv.Itoa(secs))
	}
	return nil
}

func max2(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// recordMCPCall keeps the call in the trail, whatever became of it, in a unit of work of its own so a refusal is kept too, and
// raises the detection's alerts (AI-45): an app or a network this person has not used before, and denied scopes repeating. It
// records only hashes, never a token, an argument or a result. A failure to record is logged and never changes the answer.
func (a *API) recordMCPCall(ctx context.Context, p authn.Principal, call MCPToolCall, out *mcpToolCallOutput, callErr error, start time.Time) {
	c := storage.MCPCall{UserID: p.UserID, App: call.App, AppVersion: call.AppVersion, TokenID: call.TokenID, Tool: string(call.Tool),
		Network: networkOf(call.IP), At: start, DurationMS: int(a.deps.Clock.Now().Sub(start) / time.Millisecond), Outcome: storage.MCPRead}
	if raw, err := json.Marshal(call.Input); err == nil {
		c.InputSHA256 = sha256Hex(raw)
	}
	if out != nil {
		if raw, err := json.Marshal(out.Body); err == nil {
			c.OutputSHA = sha256Hex(raw)
		}
		c.Facts = len(out.Body.Facts)
	}
	if callErr != nil {
		code := CodeFailure
		var pr *Problem
		if errors.As(callErr, &pr) {
			code = pr.Code
		}
		c.Code, c.Outcome = code, storage.MCPRefused
		switch code {
		case CodeMCPLimit:
			c.Outcome = storage.MCPLimited
		case CodeFailure, CodeToolTimeout:
			c.Outcome = storage.MCPFailed
		}
	}
	if c.App == "" || !appName.MatchString(c.App) {
		c.App = "unknown"
	}
	if !toolName.MatchString(c.Tool) {
		return // the tool was not one the registry has: nothing to keep, and the record would be refused
	}
	var newApp, newNetwork, deniedRepeatedly bool
	err := a.deps.Store.Do(context.WithoutCancel(ctx), p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.MCPCalls().Record(ctx, c); err != nil {
			return err
		}
		var err error
		if newApp, err = u.MCPCalls().FirstSeen(ctx, p.UserID, "app", c.App, start); err != nil {
			return err
		}
		if c.Network != "" {
			if newNetwork, err = u.MCPCalls().FirstSeen(ctx, p.UserID, "network", c.Network, start); err != nil {
				return err
			}
		}
		if c.Code == CodeMCPScope {
			n, _, err := u.MCPCalls().Count(ctx, p.UserID, c.App, start.Add(-10*time.Minute), start)
			deniedRepeatedly = err == nil && n >= 3
		}
		return nil
	})
	if err != nil {
		a.deps.Log.ErrorContext(ctx, "mcp.call_not_recorded", "error", err.Error())
		return
	}
	for event, on := range map[string]bool{"mcp.new_app": newApp, "mcp.new_network": newNetwork, "mcp.scopes_denied_repeatedly": deniedRepeatedly} {
		if on { // SEC-31: a security event for the Owner's monitoring
			a.deps.Log.WarnContext(ctx, "AI app activity worth a look", "event", event, "user", p.UserID.String(), "organisation", p.Organisation.String(),
				"app", c.App, "network", c.Network, "tool", c.Tool)
		}
	}
}

var (
	appName  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	toolName = regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)?$`)
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// networkOf is an address as the network it is in, a /24 or a /48: enough to notice a new place, never the address itself.
func networkOf(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 24
	if addr.Is6() {
		bits = 48
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return prefix.String()
}
