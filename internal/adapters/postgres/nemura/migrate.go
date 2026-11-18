// Package nemura is Nemura's PostgreSQL adapter (PRD-002 ADR-N1, NM-ARC-02): the only Go code that holds Nemura's SQL, for
// its own database and migration stream (db/nemura/migrations), apart from Fyniq's. It imports nothing of Fyniq's store.
// Nemura's domain reaches it through ports, never through pgx.
package nemura

import (
	"context"
	"io"

	nemuradb "github.com/cognizhi/fyniq/db/nemura"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/gooserun"
)

// MigrationsTable is where goose records Nemura's applied migrations: a schema of its own, which no service role can read.
const MigrationsTable = "nemura_migrations.goose_db_version"

var set = gooserun.Set{FS: nemuradb.Migrations, Dir: "migrations", Schema: "nemura_migrations", Table: MigrationsTable}

// Migrate applies every pending migration as the role in url, which must be nemura_migrator (SEC-03), and writes one line
// per migration to log. Nemura has no statutory packs to load until its packs exist (p1-nemura-records-packs).
func Migrate(ctx context.Context, url string, log io.Writer) error {
	return gooserun.Up(ctx, url, set, log)
}

// MigrationStatus writes each migration and whether it is applied.
func MigrationStatus(ctx context.Context, url string, log io.Writer) error {
	return gooserun.Status(ctx, url, set, log)
}
