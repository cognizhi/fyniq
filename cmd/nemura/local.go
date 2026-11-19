//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/nemura/role"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The local server is the one of `make deps-up`, on its port (FYNIQ_PG_PORT, 55432): a database `nemura` beside Fyniq's,
// with roles of its own. NEMURA_DATABASE_ADMIN_URL is its superuser (deploy/local/compose.yaml), used only to create the
// migrator and the database the first time.
func localURL(user, password, database string) string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		user, password, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432"), database)
}

func init() {
	defaultDatabaseURL = func(r role.Role) string { return localURL(r.DatabaseRole(), nemura.LocalPassword, nemura.Database) }
	before = func(ctx context.Context) error {
		return nemura.BootstrapLocal(ctx, cmp.Or(os.Getenv("NEMURA_DATABASE_ADMIN_URL"), localURL("fyniq", "fyniq-local-only", "postgres")))
	}
	after = nemura.EnableLocalLogins
	taskWiring = localTasks
}

// localTasks is the local substitute for Cloud Tasks: HMAC credentials with a key the local services share
// (NEMURA_TASK_KEY, local only), and a push dispatcher that delivers each task to the worker's own /tasks
// (NEMURA_WORKER_URL, by default the worker's own port).
func localTasks(deps app.Deps, getenv func(string) string) (ports.TaskTokens, ports.Queue, error) {
	tokens := local.NewHMACTokens([]byte(cmp.Or(getenv("NEMURA_TASK_KEY"), "nemura-local-only-task-key")), deps.Clock, time.Hour)
	self := cmp.Or(getenv("NEMURA_WORKER_URL"), "http://127.0.0.1:"+cmp.Or(getenv("PORT"), "8080"))
	return tokens, local.NewPush(map[string]string{"worker": self}, tokens, deps.Clock, 5, time.Second), nil
}
