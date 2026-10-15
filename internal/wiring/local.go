//go:build local

package wiring

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ports"
)

// localKey signs local task credentials. Local only, valid nowhere else (SEC-12).
const localKey = "fyniq-local-only-task-key"

// Build wires service (api, worker, executor, jobs) for local mode. Unset settings default to the
// ports `make dev` uses; Compose sets them to the containers' names.
func Build(_ context.Context, service string, clock ports.Clock) (*Kit, error) {
	role := map[string]string{"api": "fyniq_api", "worker": "fyniq_worker", "executor": "fyniq_executor", "jobs": "fyniq_worker"}[service]
	if role == "" {
		return nil, fmt.Errorf("service %s does no background work", service)
	}
	url := cmp.Or(os.Getenv("FYNIQ_DATABASE_URL"),
		fmt.Sprintf("postgres://%s:%s@localhost:%s/fyniq?sslmode=disable", role, postgres.LocalPassword, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432")))
	store, err := postgres.Connect(url, postgres.WithClock(clock)) // lazily: readiness reports a missing database
	if err != nil {
		return nil, err
	}
	tokens := local.NewHMACTokens([]byte(cmp.Or(os.Getenv("FYNIQ_TASK_TOKEN_KEY"), localKey)), clock, time.Hour)
	worker := cmp.Or(os.Getenv("FYNIQ_WORKER_URL"), "http://localhost:8084")
	executor := cmp.Or(os.Getenv("FYNIQ_EXECUTOR_URL"), "http://localhost:8085")
	data := cmp.Or(os.Getenv("FYNIQ_DATA_DIR"), ".data")
	blobs, err := local.NewDir(filepath.Join(data, "blobs"))
	if err != nil {
		return nil, err
	}
	signer, err := local.LoadOrCreateEd25519(filepath.Join(data, "keys", "audit-anchor.ed25519"))
	if err != nil {
		return nil, err
	}
	return &Kit{
		Store:  store,
		Tokens: tokens,
		Queue:  local.NewPush(map[string]string{"worker": worker, "executor": executor, "jobs": worker}, tokens, 5, time.Second),
		Jobs:   local.HTTPJobs{URL: worker, Tokens: tokens},
		Blobs:  blobs,
		Signer: signer,
	}, nil
}
