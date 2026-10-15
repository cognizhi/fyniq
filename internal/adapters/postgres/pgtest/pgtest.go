//go:build local

// Package pgtest starts a PostgreSQL 18 database for the adapter's tests with Testcontainers and
// prepares it exactly as `migrate up` prepares a local database: fyniq_migrator, the migrations, and
// a login for every service role. Docker must be running: the suites it serves block merge
// (QA-ENG-05), so a missing Docker fails the tests rather than skipping them.
package pgtest

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/testcontainers/testcontainers-go"
	tclog "github.com/testcontainers/testcontainers-go/log"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
)

// Image is the PostgreSQL image of deploy/local/compose.yaml, pinned by digest (a test keeps them equal).
const Image = "postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"

// Admin is the container's superuser. Only tests and the local bootstrap use it.
const Admin = "fyniq"

// DB is a prepared test database.
type DB struct {
	adminURL string
	c        testcontainers.Container
}

// Start runs the container and prepares the database.
func Start(ctx context.Context) (*DB, error) {
	tclog.SetDefault(tclog.NewNoopLogger())
	c, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase("fyniq"),
		tcpostgres.WithUsername(Admin),
		tcpostgres.WithPassword(postgres.LocalPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("start PostgreSQL with Testcontainers (is Docker running?): %w", err)
	}
	adminURL, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, err
	}
	d := &DB{adminURL: adminURL, c: c}
	if err := postgres.BootstrapLocal(ctx, adminURL); err != nil {
		d.Stop()
		return nil, err
	}
	if err := postgres.Migrate(ctx, d.URL("fyniq_migrator"), io.Discard); err != nil {
		d.Stop()
		return nil, err
	}
	if err := postgres.EnableLocalLogins(ctx, d.URL("fyniq_migrator")); err != nil {
		d.Stop()
		return nil, err
	}
	return d, nil
}

// URL is the database URL for role: fyniq (the superuser), fyniq_migrator or a service role.
func (d *DB) URL(role string) string {
	u, err := url.Parse(d.adminURL)
	if err != nil {
		panic(err)
	}
	u.User = url.UserPassword(role, postgres.LocalPassword)
	return u.String()
}

// Stop removes the container.
func (d *DB) Stop() { _ = testcontainers.TerminateContainer(d.c) }
