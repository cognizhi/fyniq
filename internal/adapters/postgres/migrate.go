package postgres

import (
	"context"
	"io"

	"github.com/cognizhi/fyniq/db"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/gooserun"
)

// MigrationsTable is where goose records the applied migrations: a schema of its own, which no
// service role can read.
const MigrationsTable = "fyniq_migrations.goose_db_version"

// fyniqSet is Fyniq's stream (db/migrations), run by the code Nemura's own stream shares (gooserun).
var fyniqSet = gooserun.Set{FS: db.Migrations, Dir: "migrations", Schema: "fyniq_migrations", Table: MigrationsTable}

// Migrate applies every pending migration as the role in url, which must be fyniq_migrator
// (SEC-03), then loads the statutory pack versions the release ships, and writes one line per
// migration and per newly loaded pack version to log.
func Migrate(ctx context.Context, url string, log io.Writer) error {
	if err := gooserun.Up(ctx, url, fyniqSet, log); err != nil {
		return err
	}
	// The statutory packs this release ships (ENG-30): written only here, by the migrator (§11.4).
	return LoadShippedPacks(ctx, url, log)
}

// MigrationStatus writes each migration and whether it is applied.
func MigrationStatus(ctx context.Context, url string, log io.Writer) error {
	return gooserun.Status(ctx, url, fyniqSet, log)
}
