// Command mcp serves the MCP endpoint for approved clients (PRD-001 §9.7). At launch it is a shell that exposes no tool
// and refuses every call, each refusal logged (internal/mcpshell); the MCP server opens with p1-mcp-first-party after
// go-live. The token path is proved in p0-identity-oauth-first-run.
package main

import (
	"log/slog"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/mcpshell"
)

func main() {
	os.Exit(app.Serve("mcp", func(s *httpserver.Server, _ app.Deps) error {
		mcpshell.Routes(s, logging.New(os.Stderr, slog.LevelInfo))
		return nil
	}))
}
