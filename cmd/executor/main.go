// Command executor carries out approved AI proposals (§9.3) under its own service identity and
// database role: POST /tasks accepts only the executor class's tasks (Proposal executor queue
// isolation), and runs only work it finds approved in the database. The proposal kinds arrive in P1.
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
	os.Exit(app.Serve("executor", func(s *httpserver.Server, deps app.Deps) error {
		kit, err := wiring.Build(context.Background(), "executor", deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "executor: serving without background work:", err)
			return nil
		}
		s.AddReadiness("database", kit.Store.Ping)
		s.Mux.Handle("POST /tasks", &outbox.Handler{Class: storage.ClassExecutor, Store: kit.Store, Tokens: kit.Tokens,
			Clock: deps.Clock, Holder: app.Holder("executor"), Kinds: map[string]outbox.Work{}})
		return nil
	}))
}
