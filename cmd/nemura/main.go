// Command nemura is Nemura's one image (PRD-002 ADR-N1, NM-ARC-02): the optional people platform, with its own database
// and sign-in, in the same repository as Fyniq. The same static binary runs as each of its services, chosen by
// NEMURA_ROLE (NM-ARC-06): api, worker, executor, assistant, and the migrate job. Each role is its own service, with
// its own identity, database role and scaling, from one image and one digest.
//
//	NEMURA_ROLE=api nemura          the REST API (this release: the HTTP shell and its database check)
//	NEMURA_ROLE=migrate nemura up   apply Nemura's migrations under nemura_migrator, then exit
//	NEMURA_ROLE=migrate nemura status   list the migrations and whether each is applied
//	nemura --version                what the build is
//	nemura healthcheck              probe the running role's /readyz (a distroless image has no shell or curl)
//
// NEMURA_DATABASE_URL is the URL of the role's own database role (nemura_api for the api, nemura_migrator for migrate);
// the local build defaults it to the local database. The deployment's own settings keep their Fyniq names (FYNIQ_ENV,
// FYNIQ_BUSINESS_DATE, PORT) so the start-up checks of internal/app apply unchanged; Nemura's are NEMURA_*. A role that is
// unset, unknown or not built yet refuses to start with NEMURA-ROLE-01 and a plain fix.
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/nemura/role"
)

// The local build sets these (local.go): the database URL of a role when NEMURA_DATABASE_URL is unset, what the migrate
// job does before it (create the migrator and the database on an empty local server) and after it (let each service role
// log in).
var (
	defaultDatabaseURL = func(role.Role) string { return "" }
	before             func(ctx context.Context) error
	after              func(ctx context.Context, migratorURL string) error
)

func main() { os.Exit(run(os.Args[1:], os.Getenv)) }

// run starts the role the environment names, or reports why it cannot.
func run(args []string, getenv func(string) string) int {
	// The probe and the version need no role: they answer for any container of this image.
	if len(args) > 0 && (args[0] == "--version" || args[0] == "healthcheck") {
		return app.Serve("nemura", nil)
	}
	r, err := role.Parse(getenv(role.Env))
	if err != nil {
		fmt.Fprintf(os.Stderr, "nemura: %v\n", err)
		return 2
	}
	if r == role.Migrate {
		return app.Run(r.Service(), func(ctx context.Context, _ app.Deps, args []string) error { return migrate(ctx, getenv, args) })
	}
	return app.Serve(r.Service(), func(s *httpserver.Server, _ app.Deps) error { return routes(r, s, getenv) })
}

func databaseURL(r role.Role, getenv func(string) string) (string, error) {
	url := cmp.Or(getenv("NEMURA_DATABASE_URL"), defaultDatabaseURL(r))
	if url == "" {
		return "", fmt.Errorf("NEMURA_DATABASE_URL is not set: give the URL of the %s database role", r.DatabaseRole())
	}
	return url, nil
}

// routes adds the role's own handlers. Every role answers /healthz, /readyz and /version from the shell; the API's
// operations arrive with the API foundation (p1-nemura-platform, task 4).
func routes(r role.Role, s *httpserver.Server, getenv func(string) string) error {
	if r != role.API {
		return fmt.Errorf("the %s role has no routes", r)
	}
	url, err := databaseURL(r, getenv)
	if err != nil {
		return err
	}
	store, err := nemura.Open(context.Background(), url)
	if err != nil {
		return err
	}
	s.AddReadiness("database", store.Ping)
	return nil
}

// migrate is the migrate role: `up` applies every pending migration, `status` lists them.
func migrate(ctx context.Context, getenv func(string) string, args []string) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "status") {
		return fmt.Errorf("usage: NEMURA_ROLE=migrate nemura up | nemura status")
	}
	url, err := databaseURL(role.Migrate, getenv)
	if err != nil {
		return err
	}
	if args[0] == "status" {
		return nemura.MigrationStatus(ctx, url, os.Stdout)
	}
	if before != nil {
		if err := before(ctx); err != nil {
			return err
		}
	}
	if err := nemura.Migrate(ctx, url, os.Stderr); err != nil {
		return err
	}
	if after != nil {
		return after(ctx, url)
	}
	return nil
}
