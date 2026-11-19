//go:build local

package main

import (
	"cmp"
	"context"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/kbcompile"
	"github.com/cognizhi/fyniq/internal/ports"
)

// localKey is the local build's task-token key when FYNIQ_TASK_TOKEN_KEY is not set (internal/wiring/local.go).
const localKey = "fyniq-local-only-task-key"

func init() {
	kbCompilerTokens = func(clock ports.Clock) func(context.Context) (string, error) {
		tokens := local.NewHMACTokens([]byte(cmp.Or(os.Getenv("FYNIQ_TASK_TOKEN_KEY"), localKey)), clock, time.Hour)
		return func(context.Context) (string, error) { return tokens.Token(kbcompile.Class) }
	}
}
