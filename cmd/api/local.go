//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/oauthflow"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Locally the API calls its own tick (in production Cloud Scheduler does), every
// FYNIQ_TICK_INTERVAL (default 5m; "off" stops it). It runs the same tick and handlers (§12.8).
func init() {
	// Until the OAuth server arrives (p0-identity-oauth-first-run), the local API accepts the local
	// issuer's dev tokens, signed with FYNIQ_DEV_TOKEN_KEY (a local-only default).
	newAuthenticator = func(clock ports.Clock) authn.Authenticator {
		dev := devTokens(clock)
		admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL")
		if admin == "" {
			return dev
		}
		// Hydra's access tokens for the API's audience, introspected and cached for at most 30 s (ADR-T3).
		return authn.Local{Dev: dev, OAuth: &authn.Introspector{OAuth: hydra.New(admin), Audience: apiAudience(), Clock: clock, TTL: authn.MaxCache}}
	}
	// Hydra's login and consent pages. Who is signed in comes from the dev tokens until the SignIn port.
	registerOAuth = func(s *httpserver.Server, clock ports.Clock) {
		admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL")
		if admin == "" {
			return
		}
		oauthflow.Flow{OAuth: hydra.New(admin), SignedIn: devTokens(clock), Clock: clock, APIAudience: apiAudience(),
			MCPAudience: cmp.Or(os.Getenv("FYNIQ_MCP_AUDIENCE"), "http://localhost:8080/mcp"),
			Log:         logging.New(os.Stderr, slog.LevelInfo)}.Register(s)
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

func devTokens(clock ports.Clock) *local.DevTokens {
	return local.NewDevTokens([]byte(cmp.Or(os.Getenv("FYNIQ_DEV_TOKEN_KEY"), local.DevTokenKey)), clock)
}

func apiAudience() string {
	return cmp.Or(os.Getenv("FYNIQ_API_AUDIENCE"), "http://localhost:8080/api")
}
