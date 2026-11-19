package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/mcpserver"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/version"
)

type frozen struct{}

func (frozen) Now() time.Time { return time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC) }

type noOAuth struct{}

func (noOAuth) Introspect(context.Context, string) (ports.Introspection, error) {
	return ports.Introspection{}, nil
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func serve(t *testing.T, e map[string]string) (*httptest.Server, error) {
	t.Helper()
	s := httpserver.New(version.Info{})
	err := routes(s, frozen{}, env(e), func(string) (mcpserver.Introspector, error) { return noOAuth{}, nil },
		func(ports.Clock, func(string) string) (mcpserver.Backend, error) { return nil, nil })
	if err != nil {
		return nil, err
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, nil
}

func post(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// The launch shell is what answers until an operator sets the deployment switch to on (p1-mcp-first-party design D1):
// unset, off and, on a typo, nothing at all, never the server. With the switch off the server's metadata is not even routed.
// req: AI-35
// req: QA-MCP-01
// asvs: V13.1.1
func TestTheShellAnswersUntilTheDeploymentSwitchIsOn(t *testing.T) {
	for _, e := range []map[string]string{{}, {"FYNIQ_MCP_SERVER": "off"}, {"FYNIQ_MCP_SERVER": "", "FYNIQ_MCP_AUDIENCE": "http://localhost:8080/mcp"}} {
		srv, err := serve(t, e)
		if err != nil {
			t.Fatal(err)
		}
		if status, body := post(t, srv, "/mcp"); status != http.StatusForbidden || !strings.Contains(body, "MCP-OFF-01") {
			t.Errorf("switch %q: %d %s, want the shell's refusal", e["FYNIQ_MCP_SERVER"], status, body)
		}
		resp, err := http.Get(srv.URL + "/.well-known/oauth-protected-resource/mcp")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("switch %q: the server's metadata is routed with the shell: %d", e["FYNIQ_MCP_SERVER"], resp.StatusCode)
		}
	}
	for _, typo := range []string{"ON", "true", "1", "yes", "on "} {
		if _, err := serve(t, map[string]string{"FYNIQ_MCP_SERVER": typo}); err == nil {
			t.Errorf("the switch %q started a service: it must stop it", typo)
		}
	}
}

// With the switch on, the server answers: a call without a token is challenged with the metadata, and the metadata is
// published; but it will not start without what it needs, and the shell is gone.
// req: AI-36
// req: AI-15
func TestWithTheSwitchOnTheServerAnswers(t *testing.T) {
	full := map[string]string{"FYNIQ_MCP_SERVER": "on", "FYNIQ_MCP_AUDIENCE": "http://localhost:8080/mcp", "FYNIQ_OAUTH_ISSUER": "http://localhost:8080/oauth/",
		"FYNIQ_PUBLIC_ORIGIN": "http://localhost:8080", "FYNIQ_OAUTH_ADMIN_URL": "http://oauth:4445"}
	srv, err := serve(t, full)
	if err != nil {
		t.Fatal(err)
	}
	status, body := post(t, srv, "/mcp")
	if status != http.StatusUnauthorized || strings.Contains(body, "MCP-OFF-01") || !strings.Contains(body, "MCP-AUTH-01") {
		t.Errorf("a call without a token: %d %s", status, body)
	}
	resp, err := http.Get(srv.URL + "/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the metadata: %d", resp.StatusCode)
	}
	for _, missing := range []string{"FYNIQ_MCP_AUDIENCE", "FYNIQ_OAUTH_ISSUER", "FYNIQ_PUBLIC_ORIGIN"} {
		e := map[string]string{}
		for k, v := range full {
			e[k] = v
		}
		delete(e, missing)
		if _, err := serve(t, e); err == nil {
			t.Errorf("the server started without %s", missing)
		}
	}
	e := map[string]string{}
	for k, v := range full {
		e[k] = v
	}
	e["FYNIQ_MCP_AUDIENCE"] = "mcp.example.com"
	if _, err := serve(t, e); err == nil {
		t.Error("the server started for a resource that is not a canonical URL")
	}
}
