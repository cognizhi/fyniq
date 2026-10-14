// Command migrate applies the database migrations as a run-to-completion job under its own role
// (PRD-001 §12.4, SEC-03). goose and the first migrations arrive with
// p0-database-tenancy-migrations; until then it reports that there is nothing to apply.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() {
	os.Exit(app.Run("migrate", func(_ context.Context, _ app.Deps, args []string) error {
		if len(args) == 0 || args[0] != "up" {
			return fmt.Errorf("usage: migrate up")
		}
		fmt.Fprintln(os.Stderr, "migrate: no migrations yet; p0-database-tenancy-migrations adds them")
		return nil
	}))
}
