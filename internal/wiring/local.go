//go:build local

package wiring

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/adapters/clamav"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/smtp"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
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
	secrets, err := local.NewSecretFiles(filepath.Join(data, "secrets"))
	if err != nil {
		return nil, err
	}
	catalogue, err := connectors.Catalogue()
	if err != nil {
		return nil, err
	}
	names, err := ReadConnectorNames(os.Getenv)
	if err != nil {
		return nil, err
	}
	return &Kit{
		// The connectors' keys in files under .data/secrets; their traffic through the guard, which lets the local
		// substitutes (Mailpit, Prism) through on loopback and the Docker network, never the metadata service.
		Connectors: catalogue, ConnectorSecrets: secrets, ConnectorNames: names, Egress: &egress.Guard{Local: true},
		Store:  store,
		Tokens: tokens,
		Queue:  local.NewPush(map[string]string{"worker": worker, "executor": executor, "jobs": worker}, tokens, 5, time.Second),
		Jobs:   local.HTTPJobs{URL: worker, Tokens: tokens},
		Blobs:  blobs,
		Signer: signer,
		// Local mode keeps staging and committed evidence under their own prefixes of the same directory, which
		// enforces retention in code, and scans with clamd when FYNIQ_CLAMD_ADDR names one (the stack's scanner
		// profile), else with the EICAR fake (SEC-24).
		Staging: blobs, Evidence: blobs, Scanner: scanner(local.EICARScanner{}),
		Mail:   LocalMail(os.Getenv),
		Origin: cmp.Or(os.Getenv("FYNIQ_PUBLIC_ORIGIN"), "http://localhost:8080"),
		// The daily rate feed from files of synthetic rates, one per date (FR-GL-05).
		Rates: local.RateFiles{Dir: filepath.Join(data, "fx")},
		// The render service beside it (`make dev`: :8086; the stack: render:8080), called with a task token.
		Render: &render.Client{URL: cmp.Or(os.Getenv("FYNIQ_RENDER_URL"), "http://localhost:8086"),
			Token: func(context.Context) (string, error) { return tokens.Token(render.Class) }},
	}, nil
}

// scanner is clamd at FYNIQ_CLAMD_ADDR when it is set, else fallback.
func scanner(fallback ports.Scanner) ports.Scanner {
	if addr := os.Getenv("FYNIQ_CLAMD_ADDR"); addr != "" {
		return clamav.Scanner{Addr: addr}
	}
	return fallback
}

// LocalMail is local mode's email (ADR-T4, PRD-001 §8.24): Mailpit (`make deps-up`, or the stack's mailpit) for sending,
// spoken to in the clear, with senders on fyniq.local and example.com; Mailpit again for receiving, mail to the capture
// domain announced by its webhook and read through its API; a provider's webhooks only when a developer points its
// sandbox at a tunnel and sets its secret.
func LocalMail(env func(string) string) MailConfig {
	return MailConfig{Providers: []ports.Email{smtp.New(cmp.Or(env("FYNIQ_SMTP_ADDR"), "localhost:1025"), "fyniq.local", true)},
		From: cmp.Or(env("FYNIQ_MAIL_FROM"), "Fyniq <no-reply@fyniq.local>"), Domains: []string{"fyniq.local", "example.com"},
		ResendWebhookSecret: env("FYNIQ_RESEND_WEBHOOK_SECRET"), ZeptoMailWebhookKey: env("FYNIQ_ZEPTOMAIL_WEBHOOK_KEY"),
		CaptureDomain:  cmp.Or(env("FYNIQ_CAPTURE_DOMAIN"), "in.fyniq.local"),
		Inbound:        local.MailpitInbound{URL: cmp.Or(env("FYNIQ_MAILPIT_URL"), "http://localhost:8025")},
		MailpitWebhook: cmp.Or(env("FYNIQ_MAILPIT_WEBHOOK"), "fyniq:local-only")}
}

// mailEnv is the deployment's email settings the bootstrap reads, with local mode's: Mailpit at localhost:1025 unless
// FYNIQ_SMTP_ADDR names the stack's (rev 0.139).
func mailEnv(env func(string) string) func(string) string {
	defaults := map[string]string{"FYNIQ_EMAIL_PROVIDERS": "smtp", "FYNIQ_SMTP_ADDR": "localhost:1025", "FYNIQ_MAIL_DOMAIN": "fyniq.local"}
	return func(name string) string { return cmp.Or(env(name), defaults[name]) }
}
