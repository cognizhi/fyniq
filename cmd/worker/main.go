// Command worker runs the background work dispatched from the outbox (ADR-T7): POST /tasks for the
// worker class's tasks and, locally, POST /jobs for the platform's long jobs (in production those run
// as Cloud Run jobs from the jobs image).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/wiring"
)

func main() {
	os.Exit(app.Serve("worker", func(s *httpserver.Server, deps app.Deps) error {
		kit, err := wiring.Build(context.Background(), "worker", deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "worker: serving without background work:", err)
			return nil
		}
		s.AddReadiness("database", kit.Store.Ping)
		s.Handle("POST /tasks", &outbox.Handler{Class: storage.ClassWorker, Store: kit.Store, Tokens: kit.Tokens,
			Clock: deps.Clock, Holder: app.Holder("worker"), Kinds: map[string]outbox.Work{}})
		s.Handle("POST /jobs", &outbox.JobsHandler{Tokens: kit.Tokens, Platform: kit.Store, Clock: deps.Clock,
			Jobs: kit.PlatformJobs(deps.Clock)})
		return nil
	}))
}
