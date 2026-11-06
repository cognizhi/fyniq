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
	// SourceGenerated is a file Fyniq made, such as the PDF of an invoice it sent (FR-AR-06).
	SourceGenerated EvidenceSource = "generated"
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
	NRICFlagged    bool   // its text shows an NRIC or FIN in full (FR-DOC-09, rev 0.183)
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
	// ByHash returns at most 100 files of the organisation, removed ones aside, whose SHA-256 as received or as stored
	// is hash (hexadecimal), earliest first: the same file on other records (FR-AP-15's duplicate receipts).
	ByHash(ctx context.Context, hash string) ([]Evidence, error)
	// Counts returns how many files each of documents holds, removed ones aside; a document with none is absent
	// (FR-DOC-06's missing evidence).
	Counts(ctx context.Context, documents []uuid.UUID) (map[uuid.UUID]int, error)
	// Search returns at most f.Limit files on documents of f's types, removed ones aside, newest record date first, then
	// newest file, after the cursor's (date, id) (FR-DOC-10).
	Search(ctx context.Context, f EvidenceSearch) ([]EvidenceHit, error)
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

// EvidenceSearch is what an evidence search asks for (FR-DOC-10): the document types the person may see, and the
// filters they set; "" or nil sets none.
type EvidenceSearch struct {
	Types      []DocumentType
	Kind       string // in the evidence type, as typed, case aside
	From, To   string // the record's date, YYYY-MM-DD, inclusive
	UploadedBy string // the uploader's actor ID
	AfterDate  string // the cursor: the record date and ID of the last file of the previous page
	AfterID    *uuid.UUID
	Limit      int
}

// EvidenceHit is a file an evidence search found, with the record it is on.
type EvidenceHit struct {
	Evidence
	DocumentType   DocumentType
	DocumentStatus DocumentStatus
	DocumentNumber string
	DocumentDate   string // YYYY-MM-DD, or "" for a record without one
}
