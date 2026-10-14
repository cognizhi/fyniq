//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
)

// localURL is a local database URL for role on the port of `make deps-up`.
func localURL(role string) string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%s/fyniq?sslmode=disable",
		role, postgres.LocalPassword, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432"))
}

func init() {
	databaseURL = func() string { return cmp.Or(os.Getenv("FYNIQ_DATABASE_URL"), localURL("fyniq_migrator")) }
	// FYNIQ_DATABASE_ADMIN_URL is the local container's superuser (deploy/local/compose.yaml).
	before = func(ctx context.Context) error {
		return postgres.BootstrapLocal(ctx, cmp.Or(os.Getenv("FYNIQ_DATABASE_ADMIN_URL"), localURL("fyniq")))
	}
	after = postgres.EnableLocalLogins
}
