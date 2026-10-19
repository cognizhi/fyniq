package postgres

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/cognizhi/fyniq/db"
)

// MigrationsTable is where goose records the applied migrations: a schema of its own, which no
// service role can read.
const MigrationsTable = "fyniq_migrations.goose_db_version"

// provider opens goose on url with the embedded migrations.
func provider(ctx context.Context, url string) (*goose.Provider, func() error, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, nil, fmt.Errorf("parse the database URL: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg)
	if _, err := sqlDB.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS fyniq_migrations"); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("prepare the migrations schema (connect as fyniq_migrator): %w", err)
	}
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	locker, err := lock.NewPostgresSessionLocker() // one migrate job at a time
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations,
		goose.WithTableName(MigrationsTable), goose.WithSessionLocker(locker))
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	return p, sqlDB.Close, nil
}

// Migrate applies every pending migration as the role in url, which must be fyniq_migrator
// (SEC-03), then loads the statutory pack versions the release ships, and writes one line per
// migration and per newly loaded pack version to log.
func Migrate(ctx context.Context, url string, log io.Writer) error {
	p, closeDB, err := provider(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	results, err := p.Up(ctx)
	for _, r := range results {
		fmt.Fprintf(log, "applied %s (%s)\n", r.Source.Path, r.Duration.Round(1e6))
	}
	if err != nil {
		return fmt.Errorf("apply the migrations: %w", err)
	}
	if len(results) == 0 {
		fmt.Fprintln(log, "the database is up to date")
	}
	// The statutory packs this release ships (ENG-30): written only here, by the migrator (§11.4).
	return LoadShippedPacks(ctx, url, log)
}

// MigrationStatus writes each migration and whether it is applied.
func MigrationStatus(ctx context.Context, url string, log io.Writer) error {
	p, closeDB, err := provider(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = closeDB() }()
	st, err := p.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range st {
		fmt.Fprintf(log, "%-8s %s\n", s.State, s.Source.Path)
	}
	return nil
}
