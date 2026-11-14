//go:build local

package main

import (
	"cmp"
	"context"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// localKey is the local task tokens' key (internal/wiring): local only, valid nowhere else (SEC-12).
const localKey = "fyniq-local-only-task-key"

// wire is local mode's AI service: the local task tokens verify the API's calls (of assistant.Class) and make the AI
// service's own token for the API (of class ai), and the API is beside it (`make dev`: :8080; the stack: api:8080),
// reached through a guard that allows local addresses, as the platform's own guard does in local mode.
func wire(clock ports.Clock) (ports.TaskTokens, assistant.Backend, error) {
	tokens := local.NewHMACTokens([]byte(cmp.Or(os.Getenv("FYNIQ_TASK_TOKEN_KEY"), localKey)), clock, time.Hour)
	url := cmp.Or(os.Getenv("FYNIQ_API_INTERNAL_URL"), "http://localhost:8080/internal/v1")
	return tokens, &assistant.Client{URL: url, ServiceToken: func(context.Context) (string, error) { return tokens.Token("ai") },
		HTTP: (&egress.Guard{Local: true, Propagate: true}).Bounded(8<<20, 5*time.Minute).For(egress.HostOf(url))}, nil
}
