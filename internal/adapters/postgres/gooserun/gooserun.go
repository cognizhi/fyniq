// Package gooserun applies a set of goose migrations to a PostgreSQL database as the role in the URL. Fyniq's migrations
// (db/migrations) and Nemura's (db/nemura/migrations, PRD-002 ADR-N1) are two sets with two tables, run by the same code,
// and the package imports nothing of either application, so a binary that runs one carries none of the other's store.
package gooserun

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Set is one stream of migrations and where goose records it.
type Set struct {
	// FS holds the migration files; Dir is their directory inside it ("migrations").
	FS  fs.FS
	Dir string
	// Schema and Table are where goose records what is applied ("fyniq_migrations", "fyniq_migrations.goose_db_version"):
	// a schema of its own, which no service role can read.
	Schema string
	Table  string
}

// Provider opens goose on url with the set.
func Provider(ctx context.Context, url string, set Set) (*goose.Provider, func() error, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, nil, fmt.Errorf("parse the database URL: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg)
	if _, err := sqlDB.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{set.Schema}.Sanitize()); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("prepare the migrations schema (connect as the migrator role): %w", err)
	}
	migrations, err := fs.Sub(set.FS, set.Dir)
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
		goose.WithTableName(set.Table), goose.WithSessionLocker(locker))
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	return p, sqlDB.Close, nil
}

// Up applies every pending migration of the set and writes one line per migration to log.
func Up(ctx context.Context, url string, set Set, log io.Writer) error {
	p, closeDB, err := Provider(ctx, url, set)
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
	return nil
}

// Status writes each migration of the set and whether it is applied.
func Status(ctx context.Context, url string, set Set, log io.Writer) error {
	p, closeDB, err := Provider(ctx, url, set)
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
