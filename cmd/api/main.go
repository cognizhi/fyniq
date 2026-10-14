// Command api serves the public REST API under /v1 (PRD-001 §12.3). P0 starts with the HTTP
// shell; contracts and routes arrive with p0-api-conventions-authz.
package main

import (
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() { os.Exit(app.Serve("api", nil)) }
