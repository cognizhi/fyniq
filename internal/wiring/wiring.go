// Package wiring builds what the services need for background work (ADR-T7) from the environment:
// the store as the service's own database role, the task credentials, the queue, the job runner, the
// bucket and the signing key. The local build uses the local adapters (local.go); the release build
// needs the GCP adapters, which arrive with p0-infra-opentofu (release.go).
package wiring

import (
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
)

// Kit is the background-work wiring of one service.
type Kit struct {
	Store  *postgres.Store
	Tokens ports.TaskTokens
	Queue  ports.Queue
	Jobs   ports.JobRunner
	Blobs  ports.BlobStore
	Signer ports.Signer
	// Staging keeps uploads until their record is final; Evidence is the retention-locked store they are
	// committed to (FR-DOC-13, DR-07). Scanner checks uploads for malware (FR-DOC-01). Any of them nil: no
	// file can be attached.
	Staging, Evidence ports.BlobStore
	Scanner           ports.Scanner
	// Mail is the outbound email (FR-INT-21): its providers in order (none: no email is sent), the platform's
	// sender, and the domains an organisation may choose its senders on.
	Mail MailConfig
	// Rates is the daily exchange-rate feed (FR-GL-05); nil until one is configured: the MAS adapter arrives at the
	// cloud gate, and until then organisations enter their own rates.
	Rates ports.RateFeed
	// Render is the private render service (PRD-001 §12.3, rev 0.96), which compiles template versions to PDF; nil when
	// none is configured: then no preview renders and no template with IRAS particulars publishes.
	Render render.Renderer
}
