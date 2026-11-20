// Package wiring builds what the services need for background work (ADR-T7) from the environment:
// the store as the service's own database role, the task credentials, the queue, the job runner, the
// bucket and the signing key. The local build uses the local adapters (local.go); the release build
// needs the GCP adapters, which arrive with p0-infra-opentofu (release.go).
package wiring

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/canary"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/backup"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// Kit is the background-work wiring of one service.
type Kit struct {
	Store *postgres.Store
	// Release is the release this build is, which the Compliance Center's CI checks read (CMP-01): the commands set it
	// from version.Release. Zero: a development build.
	Release controls.Release
	// Stamp is the platform version and the build's commit, which every exported report names (REL-05): the commands set
	// it from version.Version and version.Commit. Empty: a development build.
	Stamp  string
	Tokens ports.TaskTokens
	// OAuthClients is the OAuth server's client registry, for the job that removes the AI apps' registrations that lapsed (AI-90). Nil:
	// they are marked expired and the OAuth server is left as it is.
	OAuthClients ports.OAuthClients
	Queue        ports.Queue
	Jobs         ports.JobRunner
	Blobs        ports.BlobStore
	Signer       ports.Signer
	// Fields seals the most sensitive fields (SEC-11): vendor bank account numbers. Nil: none can be kept.
	Fields ports.Cipher
	// Staging keeps uploads until their record is final; Evidence is the retention-locked store they are
	// committed to (FR-DOC-13, DR-07). Scanner checks uploads for malware (FR-DOC-01). Any of them nil: no
	// file can be attached.
	Staging, Evidence ports.BlobStore
	Scanner           ports.Scanner
	// Converter turns a HEIC photo into a JPEG (FR-DOC-08). Nil: a HEIC photo is refused, with a message to export it as JPEG.
	Converter ports.Converter
	// Origin is the web app's origin, such as https://fyniq.example.com: the links in notifications' email (FR-NTF-01).
	Origin string
	// SandboxHosts are the only hosts a sandbox organisation's webhooks may reach (FR-INT-18); none by default.
	SandboxHosts []string
	// Mail is the outbound email (FR-INT-21): its providers in order (none: no email is sent), the platform's
	// sender, and the domains an organisation may choose its senders on.
	Mail MailConfig
	// Rates is the daily exchange-rate feed (FR-GL-05); nil until one is configured: the MAS adapter arrives at the
	// cloud gate, and until then organisations enter their own rates.
	Rates ports.RateFeed
	// AccessPoint is the InvoiceNow access point (FR-AR-12); nil until one is connected (question 2): local mode has a stand-in,
	// and the release build answers "not available here".
	AccessPoint ports.AccessPoint
	// IRASFiler files returns with IRAS through its API (FR-TAX-03); nil until IRAS's API is connected (its payloads are [live-verify]): local mode has a stand-in, a release has none.
	IRASFiler ports.IRASFiler
	// Prices is the public model price catalogue read daily (AI-82, AI-85): LiteLLM's through the platform's egress
	// guard in a release, a directory of files locally; nil: every model stays unpriced.
	Prices ports.PriceCatalogue
	// Billing is Google's Cloud Billing Catalog for Agent Platform's prices (AI-82 (b)); nil locally.
	Billing ports.BillingCatalog
	// Render is the private render service (PRD-001 §12.3, rev 0.96), which compiles template versions to PDF; nil when
	// none is configured: then no preview renders and no template with IRAS particulars publishes.
	Render render.Renderer
	// Connectors is this release's connector catalogue (FR-INT-25); ConnectorSecrets the store their keys are in (Secret
	// Manager, or files locally); ConnectorNames which secret holds each installation connector secret
	// (FYNIQ_CONNECTOR_SECRETS, rev 0.139); Egress the guard their traffic goes through (SEC-36).
	Connectors       *connector.Catalogue
	ConnectorSecrets ports.SecretStore
	ConnectorNames   map[string]string
	Egress           *egress.Guard
	// ModelTokens are the service's own account's tokens, for an AI connection that authenticates with workload identity
	// (AI-72): the metadata server's in the cloud; nil locally, where such a connection cannot be read.
	ModelTokens oauth2.TokenSource
	// Assistant is the AI service, which answers the assistant's turns (p1-ai-assistant design D3): its URL, the API's token
	// for it (of the class assistant.Class) and the client it is reached through; no URL: every turn fails, saying so.
	AssistantURL   string
	AssistantToken func(ctx context.Context) (string, error)
	AssistantHTTP  *http.Client
	// AgentModel is the API's internal operations as the worker calls them for a scheduled agent's model step
	// (PRD-001 rev 0.281; p1-scheduled-agents design D5): their URL, the worker's token for them (of the class
	// agents.ModelClass) and the client it is reached through; no URL: the runs stay deterministic, saying the model was
	// not used.
	AgentModelURL   string
	AgentModelToken func(ctx context.Context) (string, error)
	AgentModelHTTP  *http.Client
	// KBCompiler is the API's compiler operations as the knowledge-base compiler's job calls them (PRD-001 AI-57; p1-knowledge-base
	// task 8.1): their URL, the compiler's token (of the class kbcompile.Class, never the worker's own) and the client. The local build
	// signs it with the local key; the release build leaves it unset until the compiler has its own Cloud Run job and service account
	// [live-verify], and a recompile is then refused rather than run as another identity.
	KBCompilerURL   string
	KBCompilerToken func(ctx context.Context) (string, error)
	KBCompilerHTTP  *http.Client
	// Screening screens what the AI sends a model and what it answers (AI-18, AI-P7; p1-ai-assistant design D8): Model
	// Armor in the cloud, local mode's stub of it; nil: no AI call is sent (fail closed).
	Screening ports.Screening
	// Posture and Logs are the platform's open cloud findings and a sample of its logs, which the Compliance Center's
	// platform verifications read (SEC-33, CTL-DP-03): Security Command Center and Cloud Logging in the cloud, a fake
	// and the files of a directory locally. Nil: those verifications do not run.
	Posture ports.Posture
	Logs    ports.LogReader
	// Canary posts and verifies the night's synthetic transactions in the canary organisation through the public API
	// (QA-PROD-01): nil until its client is configured, which waits for the integration clients (p1-public-api task 3.1).
	Canary *canary.Canary
	// Backup exports the database each week, into Exports' buckets (DR-02, DR-04; p1-operations-dr design D1): Cloud SQL's
	// export in the cloud, pg_dump locally. Nil: the export job fails, so its heartbeat's absence alerts.
	Backup        ports.DatabaseBackup
	ExportBuckets backup.Buckets
	// Clone makes the monthly restore drill's copy of the database, and OpenClone opens it as this service's own role
	// (DR-06, CTL-RES-02; p1-operations-dr design D2); nil: the drill fails, which its heartbeat shows.
	// Scheduling reads the Scheduler's jobs and the heartbeat's alert policies (CTL-RES-05).
	Scheduling ports.Scheduling
	Clone      ports.DatabaseClone
	OpenClone  func(ctx context.Context, t ports.CloneTarget) (storage.Store, func(), error)
	// DrillOnDemand keeps the restore drill off the schedule: locally a copy from a template stops every connection to
	// the database while it copies, so the stack runs it only when asked (make restore-drill).
	DrillOnDemand bool
}
