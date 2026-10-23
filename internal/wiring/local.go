//go:build local

package wiring

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/clamav"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/smtp"
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
	return &Kit{
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
		// Mailpit (`make deps-up`, or the stack's mailpit), spoken to in the clear: local mode only. Senders may be
		// chosen on fyniq.local and example.com.
		Mail: MailConfig{Providers: []ports.Email{smtp.New(cmp.Or(os.Getenv("FYNIQ_SMTP_ADDR"), "localhost:1025"), "fyniq.local", true)},
			From: cmp.Or(os.Getenv("FYNIQ_MAIL_FROM"), "Fyniq <no-reply@fyniq.local>"), Domains: []string{"fyniq.local", "example.com"},
			// Webhooks only when a developer points a provider's sandbox at a tunnel and sets its secret.
			ResendWebhookSecret: os.Getenv("FYNIQ_RESEND_WEBHOOK_SECRET"), ZeptoMailWebhookKey: os.Getenv("FYNIQ_ZEPTOMAIL_WEBHOOK_KEY")},
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
