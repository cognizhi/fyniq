// Command migrate applies the database migrations in db/migrations as a run-to-completion job under
// its own role, fyniq_migrator (PRD-001 §12.4, SEC-03). FYNIQ_DATABASE_URL is the migrator's URL.
//
//	migrate up       apply every pending migration
//	migrate status   list the migrations and whether each is applied
//
// The local build also prepares an empty local database first (local.go).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/app"
)

// The local build sets these (local.go): before creates fyniq_migrator, after lets the service
// roles log in, and databaseURL supplies the local URL when FYNIQ_DATABASE_URL is unset.
var (
	before      func(ctx context.Context) error
	after       func(ctx context.Context, url string) error
	databaseURL = func() string { return os.Getenv("FYNIQ_DATABASE_URL") }
)

func main() {
	os.Exit(app.Run("migrate", func(ctx context.Context, _ app.Deps, args []string) error {
		if len(args) != 1 || (args[0] != "up" && args[0] != "status") {
			return fmt.Errorf("usage: migrate up | migrate status")
		}
		url := databaseURL()
		if url == "" {
			return fmt.Errorf("FYNIQ_DATABASE_URL is not set: give the URL of the fyniq_migrator role")
		}
		if args[0] == "status" {
			return postgres.MigrationStatus(ctx, url, os.Stdout)
		}
		if before != nil {
			if err := before(ctx); err != nil {
				return err
			}
		}
		if err := postgres.Migrate(ctx, url, os.Stderr); err != nil {
			return err
		}
		if after != nil {
			return after(ctx, url)
		}
		return nil
	}))
}
