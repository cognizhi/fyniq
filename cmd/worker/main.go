// Command worker runs background handlers dispatched from the outbox (ADR-T7). Handlers arrive with p0-outbox-background-work.
package main

import (
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() { os.Exit(app.Serve("worker", nil)) }
