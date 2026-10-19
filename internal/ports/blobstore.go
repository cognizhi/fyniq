package ports

import (
	"context"
	"errors"
	"io"
	"time"
)

// BlobStore keeps files (documents, evidence, exports, audit anchors). Production: Cloud
// Storage. Local: a directory under .data/, or fake-gcs-server through the production adapter.
//
// Retention (FR-DOC-02, G4-068): a blob may carry a retain-until time, which can only move later, and a
// legal hold. While either holds, nobody can delete or replace the blob: Delete and Put return
// ErrRetained. In the cloud the bucket's Object Retention Lock enforces it as well; the database's
// retain-until stays authoritative, and the object's metadata never is.
type BlobStore interface {
	// Put writes r under key, replacing an existing blob; ErrRetained when that blob is under retention.
	Put(ctx context.Context, key string, r io.Reader) error
	// Create writes a blob that must not exist yet and returns ErrExists if it does: write-once,
	// for audit anchors and committed evidence (in the cloud the retention lock also stops deletion).
	Create(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// List returns the keys under a prefix, sorted.
	List(ctx context.Context, prefix string) ([]string, error)
	// Delete removes the blob at key (a missing blob is not an error); ErrRetained while its retain-until
	// is in the future or it is on legal hold.
	Delete(ctx context.Context, key string) error
	// SetRetainUntil keeps the blob until at least until: ErrNotFound for a missing blob, and
	// ErrRetentionShortened when until is earlier than its retain-until already is. Setting the same time
	// again is not an error.
	SetRetainUntil(ctx context.Context, key string, until time.Time) error
	// SetLegalHold puts the blob on legal hold or takes it off; ErrNotFound for a missing blob. A hold does
	// not move the retain-until.
	SetLegalHold(ctx context.Context, key string, hold bool) error
}

// ErrExists is returned by Create when the blob is already there.
var ErrExists = errors.New("the blob already exists")

// ErrRetained is returned by Delete and Put for a blob under retention or legal hold (FR-DOC-02).
var ErrRetained = errors.New("the blob is under retention or legal hold: it cannot be deleted or replaced")

// ErrRetentionShortened is returned by SetRetainUntil for a time earlier than the blob's retain-until:
// retention is only ever extended (FR-DOC-02).
var ErrRetentionShortened = errors.New("a blob's retain-until can only move later")

// Scanner checks a file for malware before it is attached (FR-DOC-01, SEC-24). Production: ClamAV as its
// own service; tests and local mode: a fake that flags the EICAR test file.
type Scanner interface {
	// Scan reads r to its end. A scanner that cannot decide returns an error, never Clean.
	Scan(ctx context.Context, r io.Reader) (ScanResult, error)
}

// ScanResult is a scanner's verdict.
type ScanResult struct {
	Clean     bool
	Signature string // what was found, when not clean, such as "Eicar-Test-Signature"
}
