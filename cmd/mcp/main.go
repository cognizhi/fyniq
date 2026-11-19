// Command mcp serves the MCP endpoint for approved clients (PRD-001 §9.7). Until an operator sets FYNIQ_MCP_SERVER=on it is
// the launch shell (internal/mcpshell): it exposes no tool and refuses every call, each refusal logged (QA-MCP-01 at launch).
// With the switch on it is the MCP server (internal/mcpserver), an OAuth resource server that holds no database access and
// no write credential and reaches data only through the API. The switch is set after security review #4
// (p1-mcp-first-party design D1); a value that is neither on nor off stops the service from starting.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/mcpserver"
	"github.com/cognizhi/fyniq/internal/mcpshell"
	"github.com/cognizhi/fyniq/internal/ports"
)

func main() {
	os.Exit(app.Serve("mcp", func(s *httpserver.Server, deps app.Deps) error {
		return routes(s, deps.Clock, os.Getenv, introspector, backend)
	}))
}

// routes mounts the launch shell, or with the deployment switch on the MCP server. A switch that is neither on nor off, or an
// "on" the server cannot be built for, is an error: the service does not start, so the endpoint is never a half-open one.
func routes(s *httpserver.Server, clock ports.Clock, getenv func(string) string, introspect func(admin string) (mcpserver.Introspector, error),
	data func(clock ports.Clock, getenv func(string) string) (mcpserver.Backend, error)) error {
	log := logging.New(os.Stderr, slog.LevelInfo)
	on, err := mcpserver.Enabled(getenv)
	if err != nil {
		return err
	}
	if !on {
		mcpshell.Routes(s, log)
		return nil
	}
	oauth, err := introspect(getenv("FYNIQ_OAUTH_ADMIN_URL"))
	if err != nil {
		return err
	}
	calls, err := data(clock, getenv)
	if err != nil {
		return fmt.Errorf("the MCP server cannot reach the data: %w", err)
	}
	srv, err := mcpserver.FromEnv(getenv, mcpserver.Wiring{OAuth: oauth, Clock: clock, Log: log, Version: app.Info("mcp").Version, Calls: calls})
	if err != nil {
		return fmt.Errorf("the MCP server cannot start: %w", err)
	}
	srv.Mount(s)
	return nil
}
