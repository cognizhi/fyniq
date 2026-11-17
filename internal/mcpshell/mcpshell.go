// Package mcpshell is the MCP endpoint at launch (PRD-001 §9.7; p1-launch-readiness task 6.3): a shell that exposes no
// tool and refuses every call. The MCP server opens with p1-mcp-first-party after go-live, behind its client allowlist
// and QA-MCP-01's security suite; until then nothing an agent sends reaches a tool, a token check or the data. Each
// refusal is logged as a security event, with neither the request's credentials nor its body (SEC-15).
package mcpshell

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cognizhi/fyniq/internal/httpserver"
)

// Code is the stable error code of every refusal (ENG-23).
const Code = "MCP-OFF-01"

// problem is the RFC 9457 answer to every call (FR-INT-07), the same for each.
var problem = map[string]any{
	"type":   "urn:fyniq:problem:" + Code,
	"title":  "The MCP server is not open",
	"status": http.StatusForbidden,
	"detail": "Fyniq exposes no MCP tool yet: the MCP server opens after go-live, for the clients an Owner enables.",
	"code":   Code,
	"fix":    "Use the web app, or the public API under /api/v1, until an Owner enables your client.",
}

// Routes answers every request under /mcp, whatever its method, with Handler.
func Routes(s *httpserver.Server, log *slog.Logger) {
	h := Handler(log)
	s.Handle("/mcp", h)
	s.Handle("/mcp/", h)
}

// Handler refuses the request with 403 and the problem, and logs the refusal: its method, path and origin, and whether
// it carried a bearer token, never the token or the body.
func Handler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.WarnContext(r.Context(), "MCP call refused: the MCP server is not open", slog.String("event", "mcp.call_refused"),
			slog.String("code", Code), slog.String("method", r.Method), slog.String("path", r.URL.Path),
			slog.String("origin", r.Header.Get("Origin")), slog.Bool("bearer", strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")))
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(problem)
	})
}
