//go:build !local

package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	netmail "net/mail"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cognizhi/fyniq/internal/adapters/clamav"
	"github.com/cognizhi/fyniq/internal/adapters/gcp"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/smtp"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
)

// Config is the Google Cloud wiring the deployment writes (p0-infra-opentofu) as JSON in FYNIQ_GCP; the
// database comes from FYNIQ_DATABASE_URL, as the service's own role.
type Config struct {
	Tasks    map[string]gcp.TaskClass `json:"tasks"`    // worker class → its queue, handler and invoker
	Invokers map[string]gcp.Invoker   `json:"invokers"` // who may call each class's handler, and the tick ("scheduler")
	Anchors  struct {
		Bucket     string `json:"bucket"`      // the retention-locked bucket of the audit anchors
		KeyVersion string `json:"key_version"` // the KMS key version that signs them
	} `json:"anchors"`
	Job string `json:"job"` // the Cloud Run job that runs the jobs image: projects/…/locations/…/jobs/…
	// Evidence names the unlocked staging bucket and the retention-locked evidence bucket (FR-DOC-13, DR-07);
	// optional until the ClamAV service arrives: without them, and a scanner, no file can be attached.
	Evidence struct {
		Staging string `json:"staging"`
		Bucket  string `json:"bucket"`
	} `json:"evidence"`
	// Render is the private render service (PRD-001 §12.3, rev 0.96): its URL, its audience and its callers' accounts;
	// optional until the deployment adds it: without it no preview renders and no template with IRAS particulars publishes.
	Render gcp.Callers `json:"render"`
}

// Build wires a service's background work on Google Cloud (ADR-T7, ADR-T8): Cloud Tasks, the task
// handlers' OIDC checks, Cloud Run jobs, the anchors' bucket and key, all as the service's own account.
// Any gap in the configuration is an error, and the service serves without background work.
func Build(_ context.Context, service string, clock ports.Clock) (*Kit, error) {
	url := os.Getenv("FYNIQ_DATABASE_URL")
	if url == "" {
		return nil, errors.New("FYNIQ_DATABASE_URL is not set: the database, as this service's role")
	}
	var c Config
	if err := json.Unmarshal([]byte(os.Getenv("FYNIQ_GCP")), &c); err != nil {
		return nil, fmt.Errorf("FYNIQ_GCP is not the Google Cloud wiring as JSON: %w", err)
	}
	if err := c.check(); err != nil {
		return nil, err
	}
	store, err := postgres.Connect(url, postgres.WithClock(clock))
	if err != nil {
		return nil, err
	}
	tokens := gcp.MetadataTokens()
	// An SMTP relay only with STARTTLS (FR-INT-21); ZeptoMail and Resend arrive as connectors.
	var mail ports.Email
	if addr, from := os.Getenv("FYNIQ_SMTP_ADDR"), os.Getenv("FYNIQ_MAIL_FROM"); addr != "" && from != "" {
		domain := os.Getenv("FYNIQ_MAIL_DOMAIN") // the Message-ID's; the sender's domain when unset
		if a, err := netmail.ParseAddress(from); err == nil && domain == "" {
			domain = a.Address[strings.LastIndex(a.Address, "@")+1:]
		}
		mail = smtp.New(addr, domain, false)
	}
	kit := &Kit{
		Email:    mail,
		MailFrom: os.Getenv("FYNIQ_MAIL_FROM"),
		Store:    store,
		Tokens:   &gcp.TaskTokens{Invokers: c.Invokers, Keys: oidc.NewRemoteKeySet(context.Background(), gcp.GoogleCerts), Clock: clock},
		Queue:    gcp.NewTasks(gcp.TasksAPI, c.Tasks, tokens, nil),
		Jobs:     gcp.NewRunJobs(gcp.RunAPI, c.Job, tokens, nil),
		Blobs:    gcp.NewStorage(gcp.StorageAPI, c.Anchors.Bucket, tokens, nil),
		Signer:   gcp.NewKMS(gcp.KMSAPI, c.Anchors.KeyVersion, tokens, nil),
	}
	if c.Evidence.Staging != "" && c.Evidence.Bucket != "" {
		kit.Staging = gcp.NewStorage(gcp.StorageAPI, c.Evidence.Staging, tokens, nil)
		kit.Evidence = gcp.NewStorage(gcp.StorageAPI, c.Evidence.Bucket, tokens, nil)
	}
	if strings.HasPrefix(c.Render.URL, "https://") && c.Render.Audience != "" { // as the service's own account
		kit.Render = &render.Client{URL: c.Render.URL, Token: gcp.IdentityTokens(c.Render.Audience, nil)}
	}
	if addr := os.Getenv("FYNIQ_CLAMD_ADDR"); addr != "" { // clamd beside the service (SEC-24)
		kit.Scanner = clamav.Scanner{Addr: addr}
	}
	return kit, nil
}

// classes are the worker classes the outbox sends to (§12.11), and the callers the handlers check.
var classes = []string{"worker", "executor", "jobs"}

func (c Config) check() error {
	var gaps []string
	for _, class := range classes {
		t, ok := c.Tasks[class]
		if !ok || !strings.Contains(t.Queue, "/queues/") || !strings.HasPrefix(t.URL, "https://") || t.ServiceAccount == "" || t.Audience == "" {
			gaps = append(gaps, "tasks."+class)
		}
	}
	for _, caller := range append(classes, "scheduler") {
		if i, ok := c.Invokers[caller]; !ok || i.ServiceAccount == "" || i.Audience == "" {
			gaps = append(gaps, "invokers."+caller)
		}
	}
	// The executor accepts only its own invoker: never the worker's (Proposal executor queue isolation).
	if c.Invokers["executor"].ServiceAccount != "" && c.Invokers["executor"].ServiceAccount == c.Invokers["worker"].ServiceAccount {
		gaps = append(gaps, "invokers.executor (the same account as the worker's)")
	}
	if c.Anchors.Bucket == "" || !strings.Contains(c.Anchors.KeyVersion, "/cryptoKeyVersions/") {
		gaps = append(gaps, "anchors")
	}
	if !strings.Contains(c.Job, "/jobs/") {
		gaps = append(gaps, "job")
	}
	if len(gaps) > 0 {
		return fmt.Errorf("FYNIQ_GCP lacks %s", strings.Join(gaps, ", "))
	}
	return nil
}
