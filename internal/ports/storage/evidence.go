package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EvidenceState is where an evidence file is in its two phases (FR-DOC-13).
type EvidenceState string

// The evidence states.
const (
	EvidenceAttached EvidenceState = "attached" // in the staging store, on its record
	EvidenceRemoved  EvidenceState = "removed"  // taken off a draft record; its staging copy is swept
	EvidenceFinal    EvidenceState = "final"    // committed to the retention-locked store: append-only
)

// EvidenceSource is how a file arrived (FR-DOC-07).
type EvidenceSource string

// The evidence sources.
const (
	SourceUpload EvidenceSource = "upload"
	SourcePaste  EvidenceSource = "paste"
	SourceEmail  EvidenceSource = "email"
	SourceAPI    EvidenceSource = "api"
)

// DataClass is the kind of personal data a file holds (FR-DOC-09).
type DataClass string

// The data classes.
const (
	ClassGeneral  DataClass = "general"
	ClassPersonal DataClass = "personal"
	ClassBank     DataClass = "bank"
	ClassPayroll  DataClass = "payroll"
	ClassIdentity DataClass = "identity"
)

// NewEvidence is a clean, checked file to attach to a source document. Empty text and a nil date are not set.
type NewEvidence struct {
	DocumentID     uuid.UUID
	EvidenceType   string // such as "receipt", "board resolution" or "screenshot"
	Note           string
	RelatesOn      *time.Time // the date it relates to
	FileName       string     // as the uploader named it: data, never a path
	MediaType      string     // decided from the content
	Size           int64
	SHA256         string // hex, of the file as stored
	OriginalSHA256 string // hex, of the file as received
	Source         EvidenceSource
	DataClass      DataClass
	StagingKey     string // where the staging store keeps it
	UploadedBy     string // the uploader's actor ID
}

// Evidence is a file on a record.
type Evidence struct {
	ID uuid.UUID
	NewEvidence
	UploadedAt  time.Time
	State       EvidenceState
	RemovedAt   *time.Time
	LockedKey   string     // where the locked store keeps it, once final
	RetainUntil *time.Time // once final; authoritative (FR-DOC-02)
	FinalisedAt *time.Time
	Version     int
}

// EvidenceFiles are the organisation's evidence files. Every change is audited with the file's hashes,
// uploader, source and parent (FR-DOC-07); the database keeps evidence append-only once its parent is not a
// draft, and never deletes it.
type EvidenceFiles interface {
	// Attach records a file on its document.
	Attach(ctx context.Context, e NewEvidence) (Evidence, error)
	Get(ctx context.Context, id uuid.UUID) (Evidence, error)
	// OfDocument returns at most limit of a document's files, by ID after after (uuid.Nil for the first page),
	// removed ones included.
	OfDocument(ctx context.Context, documentID, after uuid.UUID, limit int) ([]Evidence, error)
	// Remove takes a file off a draft record at its version, with the reason audited: CodeEvidence when the
	// record is not a draft, CodeVersion when the file changed since.
	Remove(ctx context.Context, id uuid.UUID, version int) (Evidence, error)
	// Finalise records the file's committed copy in the locked store and its retain-until: CodeEvidence while
	// its record is a draft.
	Finalise(ctx context.Context, id uuid.UUID, lockedKey string, retainUntil time.Time) (Evidence, error)
	// Unfinalised returns at most limit attached files whose record is no longer a draft, oldest first.
	Unfinalised(ctx context.Context, limit int) ([]Evidence, error)
	// LiveStagingKeys returns which of keys an attached file still uses: the sweeper keeps those.
	LiveStagingKeys(ctx context.Context, keys []string) (map[string]bool, error)
	// Known returns which of the locked store's keys a final file records: the rest are orphans to report.
	Known(ctx context.Context, lockedKeys []string) (map[string]bool, error)
	// Final returns at most limit committed files by ID after after (uuid.Nil for the first page), for the
	// nightly re-verification of their hashes.
	Final(ctx context.Context, after uuid.UUID, limit int) ([]Evidence, error)
}
