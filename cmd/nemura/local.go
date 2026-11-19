//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/role"
)

// The local server is the one of `make deps-up`, on its port (FYNIQ_PG_PORT, 55432): a database `nemura` beside Fyniq's,
// with roles of its own. NEMURA_DATABASE_ADMIN_URL is its superuser (deploy/local/compose.yaml), used only to create the
// migrator and the database the first time.
func localURL(user, password, database string) string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		user, password, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432"), database)
}

func init() {
	defaultDatabaseURL = func(r role.Role) string { return localURL(r.DatabaseRole(), nemura.LocalPassword, nemura.Database) }
	before = func(ctx context.Context) error {
		return nemura.BootstrapLocal(ctx, cmp.Or(os.Getenv("NEMURA_DATABASE_ADMIN_URL"), localURL("fyniq", "fyniq-local-only", "postgres")))
	}
	after = nemura.EnableLocalLogins
}
