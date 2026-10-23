//go:build local

package main

import (
	"cmp"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/ports"
)

// localKey is the local task tokens' key (internal/wiring): local only, valid nowhere else (SEC-12).
const localKey = "fyniq-local-only-task-key"

// callers are the render service's callers' credentials in local mode: the local task tokens, of the render class.
func callers(clock ports.Clock) (ports.TaskTokens, error) {
	return local.NewHMACTokens([]byte(cmp.Or(os.Getenv("FYNIQ_TASK_TOKEN_KEY"), localKey)), clock, time.Hour), nil
}
