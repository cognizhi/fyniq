//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// These suites run in the local build only (`go test -tags local`, part of `make test`): the
// adapter has no build-tagged code, and the database is prepared by the local bootstrap.

var db *pgtest.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	db, err = pgtest.Start(ctx)
	if err != nil {
		// Never skip: the invariant attack suite blocks merge (QA-ENG-05).
		fmt.Fprintf(os.Stderr, "postgres adapter tests need Docker: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	db.Stop()
	os.Exit(code)
}

// store connects a Store as role.
func store(t *testing.T, role string, opts ...postgres.Option) *postgres.Store {
	t.Helper()
	s, err := postgres.Open(context.Background(), db.URL(role), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// conn opens a plain connection as role, bypassing the adapter as an attacker would.
func conn(t *testing.T, role string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), db.URL(role))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// pool opens a pool of one connection as role, so every transaction reuses the same connection.
func pool(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(db.URL(role))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// books creates an organisation with porttest.Books through the API's role.
func books(t *testing.T, name string) porttest.Books {
	t.Helper()
	return porttest.NewBooks(t, store(t, "fyniq_api"), name)
}

// asOrg runs sql in one transaction on c with the tenant set to org first, as the unit of work does.
func asOrg(ctx context.Context, c *pgx.Conn, org fmt.Stringer, fn func(tx pgx.Tx) error) error {
	tx, err := c.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", org.String()); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// sqlstate returns the SQLSTATE of err, or "" when it is not a PostgreSQL error.
func sqlstate(err error) string {
	var pg interface{ SQLState() string }
	if errors.As(err, &pg) {
		return pg.SQLState()
	}
	return ""
}

// req: NFR-PORT-04
// req: NFR-PORT-02
func TestStorageConformance(t *testing.T) {
	porttest.Storage(t, func(t *testing.T, service string) storage.Store { return store(t, "fyniq_"+service) })
}

// The tests and `make dev` must use the same PostgreSQL image.
func TestImageMatchesTheLocalStack(t *testing.T) {
	compose, err := os.ReadFile("../../../deploy/local/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "image: "+pgtest.Image) {
		t.Fatalf("deploy/local/compose.yaml does not use %s; change both together", pgtest.Image)
	}
}
