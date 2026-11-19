// Command nemura is Nemura's one image (PRD-002 ADR-N1, NM-ARC-02): the optional people platform, with its own database
// and sign-in, in the same repository as Fyniq. The same static binary runs as each of its services, chosen by
// NEMURA_ROLE (NM-ARC-06): api, worker, executor, assistant, and the migrate job. Each role is its own service, with
// its own identity, database role and scaling, from one image and one digest.
//
//	NEMURA_ROLE=api nemura          the REST API (this release: the HTTP shell and its database check)
//	NEMURA_ROLE=migrate nemura up   apply Nemura's migrations under nemura_migrator, then exit
//	NEMURA_ROLE=migrate nemura status   list the migrations and whether each is applied
//	NEMURA_ROLE=migrate nemura setup-code   create the one-time first-run setup code and print it, once
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
	"net/url"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/nemura/role"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The local build sets these (local.go): the database URL of a role when NEMURA_DATABASE_URL is unset, what the migrate
// job does before it (create the migrator and the database on an empty local server) and after it (let each service role
// log in).
var (
	defaultDatabaseURL = func(role.Role) string { return "" }
	before             func(ctx context.Context) error
	after              func(ctx context.Context, migratorURL string) error
)

// taskWiring builds what the worker class needs to send and receive tasks: the credentials a task carries and the queue that
// delivers it. The local build (local.go) wires the local substitutes; the release build's, Cloud Tasks with OIDC tokens, is
// the cloud code of p1-nemura-platform task 10.1 [live-verify], and until then refuses to start the worker.
var taskWiring = func(app.Deps, func(string) string) (ports.TaskTokens, ports.Queue, error) {
	return nil, nil, fmt.Errorf("the release flavour's task queue and credentials arrive with the cloud code (p1-nemura-platform task 10.1)")
}

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
	return app.Serve(r.Service(), func(s *httpserver.Server, deps app.Deps) error { return routes(r, s, deps, getenv) })
}

func databaseURL(r role.Role, getenv func(string) string) (string, error) {
	raw := cmp.Or(getenv("NEMURA_DATABASE_URL"), defaultDatabaseURL(r))
	if raw == "" {
		return "", fmt.Errorf("NEMURA_DATABASE_URL is not set: give the URL of the %s database role", r.DatabaseRole())
	}
	// A service holds its own database role and no other (NM-ARC-06, SEC-03): a URL for another role is refused, so a
	// service started with another service's environment cannot borrow its grants.
	if u, err := url.Parse(raw); err != nil || u.User == nil || u.User.Username() != r.DatabaseRole() {
		return "", &runtimeError{fmt.Sprintf("NEMURA_DATABASE_URL is not the %s role's: the %s service connects as %s and no other role", r.DatabaseRole(), r, r.DatabaseRole())}
	}
	return raw, nil
}

// runtimeError is a start-up refusal with Nemura's stable code.
type runtimeError struct{ msg string }

func (e *runtimeError) Error() string { return "NEMURA-ROLE-02: " + e.msg }

// routes adds the role's own handlers. Every role answers /healthz, /readyz and /version from the shell; the API's
// operations arrive with the API foundation (p1-nemura-platform, task 4).
func routes(r role.Role, s *httpserver.Server, deps app.Deps, getenv func(string) string) error {
	if r != role.API && r != role.Worker {
		return fmt.Errorf("the %s role has no routes", r)
	}
	dbURL, err := databaseURL(r, getenv)
	if err != nil {
		return err
	}
	store, err := nemura.Open(context.Background(), dbURL)
	if err != nil {
		return err
	}
	s.AddReadiness("database", store.Ping)
	if r == role.Worker {
		return workerRoutes(s, deps, getenv, store)
	}
	api.New(s) // the public API under /v1 (NM-ARC-03)
	return nil
}

// workerRoutes is the worker class (ADR-T7): POST /tasks delivers one outbox row's work, POST /tick sweeps the rows that never
// left or overran. Both need the worker class's credential. No work kind is registered yet: each Nemura change registers its
// own as it adds work.
func workerRoutes(s *httpserver.Server, deps app.Deps, getenv func(string) string, store *nemura.Store) error {
	tokens, queue, err := taskWiring(deps, getenv)
	if err != nil {
		return err
	}
	disp := outbox.Dispatcher{Store: store, Queue: queue, Clock: deps.Clock}
	s.Handle("POST /tasks", &outbox.Handler{Class: outbox.ClassWorker, Store: store, Tokens: tokens, Clock: deps.Clock,
		Holder: app.Holder("nemura-worker"), Lease: 10 * time.Minute, Kinds: map[string]outbox.Work{}})
	s.Handle("POST /tick", outbox.Tick{Dispatcher: disp, Tokens: tokens, Class: outbox.ClassWorker})
	return nil
}

// migrate is the migrate role: `up` applies every pending migration, `status` lists them.
func migrate(ctx context.Context, getenv func(string) string, args []string) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "status" && args[0] != "setup-code") {
		return fmt.Errorf("usage: NEMURA_ROLE=migrate nemura up | nemura status | nemura setup-code")
	}
	url, err := databaseURL(role.Migrate, getenv)
	if err != nil {
		return err
	}
	if args[0] == "status" {
		return nemura.MigrationStatus(ctx, url, os.Stdout)
	}
	if args[0] == "setup-code" {
		// A new code replaces an unused one; once setup is done the database refuses (NM100).
		display, hash := identity.NewSetupCode()
		if err := nemura.SetSetupCode(ctx, url, hash); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "The one-time setup code (it is shown once; keep it until setup is done):")
		fmt.Println(display)
		return nil
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
