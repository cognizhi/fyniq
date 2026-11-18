package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cognizhi/fyniq/internal/mcpsurface"
	"github.com/cognizhi/fyniq/internal/ports"
)

// SwitchEnv is the deployment switch of the MCP server (p1-mcp-first-party design D1): off unless an operator sets it to
// on, in every flavour, so the launch shell (internal/mcpshell) is what answers until security review #4 is signed. It is
// not the Owner's setting features.mcp: nobody enabling a setting in an app can open an endpoint the operator has not.
const SwitchEnv = "FYNIQ_MCP_SERVER"

// Enabled reports whether the deployment switch is on. Unset or off is the launch shell; anything but on or off is an error,
// so a typo stops the service from starting instead of choosing for it.
func Enabled(getenv func(string) string) (bool, error) {
	switch v := getenv(SwitchEnv); v {
	case "", "off":
		return false, nil
	case "on":
		return true, nil
	default:
		return false, fmt.Errorf("%s is %q: set it to on, or leave it off", SwitchEnv, v)
	}
}

// Wiring is what the server needs from its environment.
type Wiring struct {
	OAuth   Introspector
	Clock   ports.Clock
	Log     *slog.Logger
	Version string
	Calls   Backend // nil: the data path is not connected yet (Unavailable)
}

// FromEnv builds the server from the environment: its resource URL (FYNIQ_MCP_AUDIENCE, the audience its tokens carry), the
// OAuth server's issuer (FYNIQ_OAUTH_ISSUER) and the origin it accepts (FYNIQ_PUBLIC_ORIGIN). It refuses to start when the
// shipped manifest does not verify against the tools and the scope map, or when any of them is missing.
func FromEnv(getenv func(string) string, w Wiring) (*Server, error) {
	resource, issuer, origin := getenv("FYNIQ_MCP_AUDIENCE"), getenv("FYNIQ_OAUTH_ISSUER"), strings.TrimSuffix(getenv("FYNIQ_PUBLIC_ORIGIN"), "/")
	if resource == "" || issuer == "" || origin == "" {
		return nil, fmt.Errorf("the MCP server needs FYNIQ_MCP_AUDIENCE, FYNIQ_OAUTH_ISSUER and FYNIQ_PUBLIC_ORIGIN")
	}
	m, _, err := mcpsurface.Load()
	if err != nil {
		return nil, err
	}
	calls := w.Calls
	if calls == nil {
		calls = Unavailable{}
	}
	return New(Config{Resource: resource, Issuer: issuer, Origins: []string{origin}, Manifest: m, Calls: calls, Log: w.Log, Version: w.Version,
		Verify: OAuthVerifier{OAuth: w.OAuth, Resource: resource, Clock: w.Clock}})
}

// Unavailable is the data path before it is built (task 6.2): every call is refused with 503 and says so, so the server
// can be started and its authorisation checked without any data being reachable.
type Unavailable struct{}

// CallTool implements Backend.
func (Unavailable) CallTool(context.Context, Call) (Result, error) {
	return Result{}, &Refusal{Status: http.StatusServiceUnavailable, Code: CodeBackend, Detail: "Fyniq's tools are not connected to this server yet.",
		Fix: "Use the assistant in Fyniq, or the public API, until the MCP tools open"}
}

// Mount adds the server's routes to a service's mux: the endpoint, and the protected-resource metadata at the well-known
// paths.
func (s *Server) Mount(h interface{ Handle(string, http.Handler) }) {
	handler := s.Handler()
	h.Handle(s.path, handler)
	h.Handle("/.well-known/oauth-protected-resource", handler)
	h.Handle(strings.TrimPrefix(s.metadata, strings.SplitN(s.metadata, "/.well-known", 2)[0]), handler)
}
