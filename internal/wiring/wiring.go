// Package wiring builds what the services need for background work (ADR-T7) from the environment:
// the store as the service's own database role, the task credentials, the queue, the job runner, the
// bucket and the signing key. The local build uses the local adapters (local.go); the release build
// needs the GCP adapters, which arrive with p0-infra-opentofu (release.go).
package wiring

import (
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ports"
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
}
