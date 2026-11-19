//go:build local

// Package nemuratest starts a PostgreSQL 18 server for Nemura's tests with Testcontainers and prepares it exactly as
// `NEMURA_ROLE=migrate` prepares a local one: nemura_migrator, the database `nemura`, its migrations, and a login for every
// service role. Docker must be running: the suites it serves block merge (QA-ENG-05), so a missing Docker fails the tests
// rather than skipping them.
package nemuratest

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/testcontainers/testcontainers-go"
	tclog "github.com/testcontainers/testcontainers-go/log"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
)

// Image is the PostgreSQL image of deploy/local/compose.yaml, pinned by digest (a test keeps them equal).
const Image = "postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"

// Admin is the container's superuser, and its database is where the local bootstrap connects. Only tests and the local
// bootstrap use them.
const (
	Admin      = "fyniq"
	AdminPass  = "fyniq-local-only"
	adminDB    = "postgres"
	ProbeRole  = "fyniq_probe" // a login that holds no Nemura role: the stand-in for a Fyniq role or a stray login
	probeLogin = "fyniq-probe-only"
)

// DB is a prepared test server.
type DB struct {
	adminURL string
	c        testcontainers.Container
}

// Start runs the container and prepares Nemura's database.
func Start(ctx context.Context) (*DB, error) {
	d, err := StartEmpty(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.prepare(ctx); err != nil {
		d.Stop()
		return nil, err
	}
	return d, nil
}

// StartEmpty runs the container and prepares nothing: a bare local server, for the test of the migrate job that prepares it.
func StartEmpty(ctx context.Context) (*DB, error) {
	tclog.SetDefault(tclog.NewNoopLogger())
	c, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase(adminDB),
		tcpostgres.WithUsername(Admin),
		tcpostgres.WithPassword(AdminPass),
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
	return &DB{adminURL: adminURL, c: c}, nil
}

func (d *DB) prepare(ctx context.Context) error {
	if err := nemura.BootstrapLocal(ctx, d.adminURL); err != nil {
		return err
	}
	if err := nemura.Migrate(ctx, d.URL(nemura.Migrator), io.Discard); err != nil {
		return err
	}
	return nemura.EnableLocalLogins(ctx, d.URL(nemura.Migrator))
}

// URL is the URL of Nemura's database for role: nemura_migrator or a service role.
func (d *DB) URL(role string) string { return d.urlOf(nemura.Database, role, nemura.LocalPassword) }

// AdminURL is the superuser's URL on the server's default database, for the tests that create what the environment does.
func (d *DB) AdminURL() string { return d.adminURL }

// SuperURL is the superuser's URL on Nemura's database, for the tests that read the catalogue.
func (d *DB) SuperURL() string { return d.urlOf(nemura.Database, Admin, AdminPass) }

// ProbeURL is the URL of Nemura's database for the probe role the test created.
func (d *DB) ProbeURL() string { return d.urlOf(nemura.Database, ProbeRole, probeLogin) }

// ProbeLogin is the probe role's password, for the test that creates the role.
func ProbeLogin() string { return probeLogin }

func (d *DB) urlOf(database, role, password string) string {
	u, err := url.Parse(d.adminURL)
	if err != nil {
		panic(err)
	}
	u.User = url.UserPassword(role, password)
	u.Path = "/" + database
	return u.String()
}

// Stop removes the container.
func (d *DB) Stop() { _ = testcontainers.TerminateContainer(d.c) }
