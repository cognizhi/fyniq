// Command api serves the public REST API under /v1 (PRD-001 §12.3, internal/api) and its OpenAPI
// document at /v1/openapi.json. It also serves the Scheduler tick (§12.11): POST /internal/tick,
// called every 5 minutes by Cloud Scheduler, and in the local build by the API itself.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/api"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/maildns"
	"github.com/cognizhi/fyniq/internal/mailhooks"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/signin"
	"github.com/cognizhi/fyniq/internal/version"
	"github.com/cognizhi/fyniq/internal/wiring"
)

// Set by the local build (local.go): the API's own tick; the local issuer's dev tokens, the sessions of
// its dev sign-in page and, when the OAuth server is configured, its tokens and the login and consent
// flow. The release build accepts no credential until the Identity Platform adapter of the SignIn port
// arrives (p0-identity-oauth-first-run, slice 4b). sessions is nil without a database.
var (
	startLocalTick   func(t outbox.Ticker)
	newAuthenticator func(clock ports.Clock, sessions storage.Sessions) authn.Authenticator
	registerSignIn   func(s *httpserver.Server, clock ports.Clock, sessions storage.Sessions, alert signin.Alert) ports.SignIn
	// webOrigin is the web app's origin as the browser sees it, "" when the flavour has none configured: the passkey
	// ceremonies are bound to it (FR-ADM-02).
	webOrigin     func() string
	registerOAuth func(s *httpserver.Server, clock ports.Clock, sessions storage.Sessions)
)

func main() {
	os.Exit(app.Serve("api", routes))
}

// routes adds the API's routes to s.
func routes(s *httpserver.Server, deps app.Deps) error {
	d := api.Deps{Clock: deps.Clock, Auth: authn.NotYet{}, Log: logging.New(os.Stderr, slog.LevelInfo), Version: version.Version}
	kit, err := wiring.Build(context.Background(), "api", deps.Clock)
	var sessions storage.Sessions
	if err == nil {
		sessions = kit.Store
	}
	if newAuthenticator != nil {
		d.Auth = newAuthenticator(deps.Clock, sessions)
	}
	if registerSignIn != nil {
		var alert signin.Alert // a sign-in alert needs the database and the outbox
		if err == nil {
			out := outbox.Dispatcher{Store: kit.Store, Queue: kit.Queue, Clock: deps.Clock}
			alert = signin.Alerts(signin.EmailAlert(kit.Store, kit.Store, out), signin.NotifyAlert(kit.Store, kit.Store, out))
		}
		d.SignIn = registerSignIn(s, deps.Clock, sessions, alert)
	}
	if registerOAuth != nil {
		registerOAuth(s, deps.Clock, sessions)
	}
	if webOrigin != nil {
		d.Origin = webOrigin()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "api: serving without a database or background work:", err)
		api.New(s, d) // without a store it accepts no credentials: the document only
		return nil
	}
	d.Store, d.Installation = kit.Store, kit.Store
	d.Staging, d.Scanner = kit.Staging, kit.Scanner // nil until configured: then no file can be attached
	d.Renderer = kit.Render                         // nil until configured: then no document preview renders
	// The email connectors decide the providers and each domain's records once installed (rev 0.139), read per request.
	dns := &maildns.Cache{Resolver: net.DefaultResolver, Domains: kit.Mail.Domains, RecordsFrom: kit.MailDNS, TTL: time.Hour, Now: deps.Clock.Now}
	d.Mail = api.MailInfo{From: kit.Mail.From, Domains: kit.Mail.Domains, Check: dns.Results, CaptureDomain: kit.Mail.CaptureDomain,
		ProvidersFrom: kit.MailProviders}
	d.Outbox = &outbox.Dispatcher{Store: kit.Store, Queue: kit.Queue, Clock: deps.Clock}
	// The connectors (FR-INT-20, rev 0.139): the release's catalogue, the store their keys go to, and the runtime the
	// connection test goes through.
	d.Connectors = api.ConnectorDeps{Catalogue: kit.Connectors, Secrets: kit.ConnectorSecrets, Names: kit.ConnectorNames, Runtime: kit.Runtime(deps.Clock)}
	// The email providers' webhooks (FR-INT-21), each only with its secret: delivery events and the suppression list.
	// Each request reads its connector's keys: the current one and one still in its rotation overlap; a disabled
	// connector's webhooks are refused (FR-INT-26, rev 0.139).
	hooks := &mailhooks.Receiver{Store: kit.Store, Clock: deps.Clock, Keys: kit.WebhookKeys(connector.NewSecretCache(kit.ConnectorSecrets, deps.Clock), deps.Clock),
		MailpitSecret: kit.Mail.MailpitWebhook, Log: d.Log, Notify: d.Outbox}
	if kit.Mail.CaptureDomain != "" { // mail for the email-in addresses (FR-INT-22)
		hooks.Inbound = &mailhooks.Inbound{Domain: kit.Mail.CaptureDomain, Routes: kit.Store, Dispatcher: *d.Outbox}
	}
	hooks.Register(s)
	// The bank-data connectors' webhooks (FR-BNK-15, design D11): each verified by its connector and only prompting a sync.
	(&bankfeed.Hooks{Store: kit.Store, Routes: kit.Store, Runtime: kit.Runtime(deps.Clock), Dispatcher: *d.Outbox, Clock: deps.Clock, Log: d.Log}).Register(s)
	api.New(s, d)
	// The email connectors from the deployment's settings, once (rev 0.139); before first-run setup, the tick does it later.
	bootstrap := func(ctx context.Context) {
		if n, err := wiring.BootstrapConnectors(ctx, kit, os.Getenv, deps.Clock); err != nil {
			d.Log.Warn("the email connectors were not installed from the deployment's settings; email is sent as configured", "error", err)
		} else if n > 0 {
			d.Log.Info("installed the email connectors from the deployment's settings", "connectors", n)
		}
	}
	bootstrap(context.Background())
	if drift, err := wiring.EmailDrift(context.Background(), kit, os.Getenv); err == nil && drift != "" {
		d.Log.Warn(drift)
	}
	s.AddReadiness("database", kit.Store.Ping)
	t := outbox.Ticker{Store: kit.Store, Platform: kit.Store, Jobs: kit.Jobs, Clock: deps.Clock, Holder: app.Holder("api"),
		Schedules: outbox.PlatformSchedules, Dispatcher: *d.Outbox, Dead: notify.JobDead(*d.Outbox)}
	s.HandleFunc("POST /internal/tick", func(w http.ResponseWriter, r *http.Request) {
		if kit.Tokens.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "scheduler") != nil {
			http.Error(w, "only the Scheduler may call the tick", http.StatusForbidden)
			return
		}
		bootstrap(r.Context())
		report, err := t.Tick(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(w, "%d started, %d dispatched, %d dead\n", len(report.Started), report.Dispatched, report.Dead)
	})
	if startLocalTick != nil {
		startLocalTick(t)
	}
	return nil
}
