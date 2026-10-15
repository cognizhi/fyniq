//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Locally the API calls its own tick (in production Cloud Scheduler does), every
// FYNIQ_TICK_INTERVAL (default 5m; "off" stops it). It runs the same tick and handlers (§12.8).
func init() {
	// Until the OAuth server arrives (p0-identity-oauth-first-run), the local API accepts the local
	// issuer's dev tokens, signed with FYNIQ_DEV_TOKEN_KEY (a local-only default).
	newAuthenticator = func(clock ports.Clock) authn.Authenticator {
		return local.NewDevTokens([]byte(cmp.Or(os.Getenv("FYNIQ_DEV_TOKEN_KEY"), local.DevTokenKey)), clock)
	}
	startLocalTick = func(t outbox.Ticker) {
		setting := os.Getenv("FYNIQ_TICK_INTERVAL")
		if setting == "off" {
			return
		}
		every := outbox.TickInterval
		if d, err := time.ParseDuration(setting); err == nil && d > 0 {
			every = d
		}
		go func() {
			for {
				time.Sleep(every) // after start-up, so a fresh `make dev` is not slowed by it
				if _, err := t.Tick(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, "api: tick:", err)
				}
			}
		}()
	}
}
