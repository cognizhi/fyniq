// Command api serves the public REST API under /v1 (PRD-001 §12.3, internal/api) and its OpenAPI
// document at /v1/openapi.json. It also serves the Scheduler tick (§12.11): POST /internal/tick,
// called every 5 minutes by Cloud Scheduler, and in the local build by the API itself.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/cognizhi/fyniq/internal/api"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/version"
	"github.com/cognizhi/fyniq/internal/wiring"
)

// Set by the local build (local.go): the API's own tick, and the local issuer's dev tokens.
var (
	startLocalTick   func(t outbox.Ticker)
	newAuthenticator func(clock ports.Clock) authn.Authenticator
)

func main() {
	os.Exit(app.Serve("api", routes))
}

// routes adds the API's routes to s.
func routes(s *httpserver.Server, deps app.Deps) error {
	d := api.Deps{Clock: deps.Clock, Auth: authn.NotYet{}, Log: logging.New(os.Stderr, slog.LevelInfo), Version: version.Version}
	if newAuthenticator != nil {
		d.Auth = newAuthenticator(deps.Clock)
	}
	kit, err := wiring.Build(context.Background(), "api", deps.Clock)
	if err != nil {
		fmt.Fprintln(os.Stderr, "api: serving without a database or background work:", err)
		api.New(s, d) // without a store it accepts no credentials: the document only
		return nil
	}
	d.Store = kit.Store
	api.New(s, d)
	s.AddReadiness("database", kit.Store.Ping)
	t := outbox.Ticker{Store: kit.Store, Platform: kit.Store, Jobs: kit.Jobs, Clock: deps.Clock, Holder: app.Holder("api"),
		Schedules: outbox.PlatformSchedules, Dispatcher: outbox.Dispatcher{Store: kit.Store, Queue: kit.Queue, Clock: deps.Clock}}
	s.HandleFunc("POST /internal/tick", func(w http.ResponseWriter, r *http.Request) {
		if kit.Tokens.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "scheduler") != nil {
			http.Error(w, "only the Scheduler may call the tick", http.StatusForbidden)
			return
		}
		report, err := t.Tick(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(w, "%d started, %d dispatched, %d dead\n", len(report.Started), report.Dispatched, report.Dead)
	})
	if startLocalTick != nil {
		startLocalTick(t)
	}
	return nil
}
