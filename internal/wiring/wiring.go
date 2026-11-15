// Package wiring builds what the services need for background work (ADR-T7) from the environment:
// the store as the service's own database role, the task credentials, the queue, the job runner, the
// bucket and the signing key. The local build uses the local adapters (local.go); the release build
// needs the GCP adapters, which arrive with p0-infra-opentofu (release.go).
package wiring

import (
	"context"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
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
	Queue  ports.Queue
	Jobs   ports.JobRunner
	Blobs  ports.BlobStore
	Signer ports.Signer
	// Fields seals the most sensitive fields (SEC-11): vendor bank account numbers. Nil: none can be kept.
	Fields ports.Cipher
	// Staging keeps uploads until their record is final; Evidence is the retention-locked store they are
	// committed to (FR-DOC-13, DR-07). Scanner checks uploads for malware (FR-DOC-01). Any of them nil: no
	// file can be attached.
	Staging, Evidence ports.BlobStore
	Scanner           ports.Scanner
	// Origin is the web app's origin, such as https://fyniq.example.com: the links in notifications' email (FR-NTF-01).
	Origin string
	// Mail is the outbound email (FR-INT-21): its providers in order (none: no email is sent), the platform's
	// sender, and the domains an organisation may choose its senders on.
	Mail MailConfig
	// Rates is the daily exchange-rate feed (FR-GL-05); nil until one is configured: the MAS adapter arrives at the
	// cloud gate, and until then organisations enter their own rates.
	Rates ports.RateFeed
	// Prices is the public model price catalogue read daily (AI-82, AI-85): LiteLLM's through the platform's egress
	// guard in a release, a directory of files locally; nil: every model stays unpriced.
	Prices ports.PriceCatalogue
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
	// Posture and Logs are the platform's open cloud findings and a sample of its logs, which the Compliance Center's
	// platform verifications read (SEC-33, CTL-DP-03): Security Command Center and Cloud Logging in the cloud, a fake
	// and the files of a directory locally. Nil: those verifications do not run.
	Posture ports.Posture
	Logs    ports.LogReader
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
