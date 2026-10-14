// Command jobs runs long work as Cloud Run jobs started by a task (ADR-T7): nightly verification,
// anchoring, imports and exports. The jobs arrive with p0-audit-chain and
// p0-outbox-background-work; until then it lists none.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() {
	os.Exit(app.Run("jobs", func(_ context.Context, _ app.Deps, args []string) error {
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "jobs: no jobs are registered yet")
			return nil
		}
		return fmt.Errorf("unknown job %q: no jobs are registered yet", args[0])
	}))
}
