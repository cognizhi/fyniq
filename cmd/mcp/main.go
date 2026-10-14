// Command mcp serves the MCP endpoint for approved clients (PRD-001 §9.7). It is a shell until P1; the token path is proved in p0-identity-oauth-first-run.
package main

import (
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() { os.Exit(app.Serve("mcp", nil)) }
