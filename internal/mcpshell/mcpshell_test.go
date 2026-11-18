package mcpshell

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/version"
)

// QA-MCP-01 at launch (p1-launch-readiness task 6.3): the MCP shell exposes no tool and refuses every call. An agent's
// initialize, tools/list and tools/call, with a token or without, a stream opened by GET and a session closed by
// DELETE each get 403 with MCP-OFF-01 and no tool's name, and each refusal is logged as a security event without the
// token or the body. The service's own probes still answer. The suite in full arrives with p1-mcp-first-party after
// go-live.
// req: QA-MCP-01
// asvs: V8.2.1, V16.3.2
func TestTheShellExposesNoToolAndRefusesEveryCall(t *testing.T) {
	var logs bytes.Buffer
	s := httpserver.New(version.Info{})
	Routes(s, logging.New(&logs, slog.LevelInfo))
	const token = "opaque-agent-token-for-the-test"
	calls := []struct {
		method, path, body string
		bearer             bool
	}{
		{http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, false},
		{http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, true},
		{http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ledger.post","arguments":{}}}`, true},
		{http.MethodGet, "/mcp", "", true},
		{http.MethodDelete, "/mcp", "", true},
		{http.MethodPost, "/mcp/messages", `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`, false},
	}
	for _, c := range calls {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		req.Header.Set("Origin", "https://agent.example")
		if c.bearer {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		var p map[string]any
		if rec.Code != http.StatusForbidden || rec.Header().Get("Content-Type") != "application/problem+json" ||
			json.Unmarshal(rec.Body.Bytes(), &p) != nil || p["code"] != Code || p["status"] != float64(http.StatusForbidden) {
			t.Errorf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "tools") || strings.Contains(rec.Body.String(), "ledger.post") {
			t.Errorf("%s %s: the answer names a tool: %s", c.method, c.path, rec.Body)
		}
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != len(calls) {
		t.Fatalf("%d refusals logged; want %d:\n%s", len(lines), len(calls), logs.String())
	}
	for _, l := range lines {
		var e map[string]any
		if json.Unmarshal([]byte(l), &e) != nil || e["event"] != "mcp.call_refused" || e["code"] != Code || e["origin"] != "https://agent.example" {
			t.Errorf("a refusal's log line: %s", l)
		}
	}
	if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "tools/call") {
		t.Errorf("the log carries the token or the body:\n%s", logs.String())
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz: %d", rec.Code)
	}
}
