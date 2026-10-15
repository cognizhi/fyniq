// Command api serves the public REST API under /v1 (PRD-001 §12.3); contracts and routes arrive with
// p0-api-conventions-authz. It also serves the Scheduler tick (§12.11): POST /internal/tick, called
// every 5 minutes by Cloud Scheduler, and in the local build by the API itself.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/wiring"
)

// startLocalTick is set by the local build (local.go).
var startLocalTick func(t outbox.Ticker)

func main() {
	os.Exit(app.Serve("api", func(s *httpserver.Server, deps app.Deps) error {
		kit, err := wiring.Build(context.Background(), "api", deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "api: serving without background work:", err)
			return nil
		}
		s.AddReadiness("database", kit.Store.Ping)
		t := outbox.Ticker{Store: kit.Store, Platform: kit.Store, Jobs: kit.Jobs, Clock: deps.Clock, Holder: app.Holder("api"),
			Schedules: outbox.PlatformSchedules, Dispatcher: outbox.Dispatcher{Store: kit.Store, Queue: kit.Queue, Clock: deps.Clock}}
		s.Mux.HandleFunc("POST /internal/tick", func(w http.ResponseWriter, r *http.Request) {
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
	}))
}
