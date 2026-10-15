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
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/signin"
)

// base is where the browser finds the API on the web app's origin: Caddy (make up) and Vite (make dev)
// both route /api to it.
const base = "/api"

// Locally the API calls its own tick (in production Cloud Scheduler does), every
// FYNIQ_TICK_INTERVAL (default 5m; "off" stops it). It runs the same tick and handlers (§12.8).
func init() {
	// The local build accepts the local issuer's dev tokens (signed with FYNIQ_DEV_TOKEN_KEY, a local-only
	// default; for tests and local tools), the sessions of its dev sign-in page and, when the OAuth server
	// is configured, Hydra's access tokens for the API's audience: each only by its own checker.
	newAuthenticator = func(clock ports.Clock, sessions storage.Sessions) authn.Authenticator {
		l := firstParty(clock, sessions)
		if admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL"); admin != "" {
			// Hydra's access tokens for the API's audience, introspected and cached for at most 30 s (ADR-T3).
			l.OAuth = &authn.Introspector{OAuth: hydra.New(admin), Audience: apiAudience(), Clock: clock, TTL: authn.MaxCache}
		}
		return l
	}
	// The dev sign-in page stands in for Identity Platform (§12.8): the browser signs in through it at
	// /auth/signin and gets a first-party session.
	registerSignIn = func(s *httpserver.Server, clock ports.Clock, sessions storage.Sessions) ports.SignIn {
		if sessions == nil {
			return nil
		}
		flow := signin.Flow{Sessions: sessions, Clock: clock, Origin: origin(), Base: base, Log: logging.New(os.Stderr, slog.LevelInfo)}
		dev, err := local.NewDevSignIn([]signin.Provider{local.DevSignInProvider}, base, flow.Callback(), clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "api: no sign-in:", err)
			return nil
		}
		flow.SignIn = dev
		flow.Register(s)
		dev.Register(s)
		return dev
	}
	// Hydra's login and consent pages: who is signed in comes from the session cookie, or a dev token.
	registerOAuth = func(s *httpserver.Server, clock ports.Clock, sessions storage.Sessions) {
		admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL")
		if admin == "" {
			return
		}
		oauthflow.Flow{OAuth: hydra.New(admin), SignedIn: firstParty(clock, sessions), Clock: clock, APIAudience: apiAudience(),
			MCPAudience: cmp.Or(os.Getenv("FYNIQ_MCP_AUDIENCE"), "http://localhost:8080/mcp"),
			Log:         logging.New(os.Stderr, slog.LevelInfo), SignInPage: "/", Base: base}.Register(s)
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

// firstParty is who is at the browser: a dev token, or the session cookie (nil sessions: none).
func firstParty(clock ports.Clock, sessions storage.Sessions) authn.Local {
	l := authn.Local{Dev: devTokens(clock)}
	if sessions != nil {
		l.Sessions = authn.Sessions{Store: sessions, Clock: clock, Cookie: authn.SessionCookie(signin.Flow{Origin: origin()}.Secure())}
	}
	return l
}

func devTokens(clock ports.Clock) *local.DevTokens {
	return local.NewDevTokens([]byte(cmp.Or(os.Getenv("FYNIQ_DEV_TOKEN_KEY"), local.DevTokenKey)), clock)
}

func apiAudience() string {
	return cmp.Or(os.Getenv("FYNIQ_API_AUDIENCE"), "http://localhost:8080/api")
}

// origin is the web app's origin as the browser sees it: Caddy's in make up, Vite's in make dev.
func origin() string {
	return cmp.Or(os.Getenv("FYNIQ_PUBLIC_ORIGIN"), "http://localhost:8080")
}
