package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RecordComment is a comment on a record, a document or a contact (FR-UX-13): who wrote it, what they wrote and whom they
// mentioned. It is never changed or removed.
type RecordComment struct {
	ID         uuid.UUID
	RecordType string
	RecordID   uuid.UUID
	Author     uuid.UUID
	Body       string
	Mentions   []uuid.UUID
	CreatedAt  time.Time
}

// NewRecordComment is what RecordComments.Add needs; CreatedAt is the API's clock.
type NewRecordComment struct {
	RecordType string
	RecordID   uuid.UUID
	Author     uuid.UUID
	Body       string
	Mentions   []uuid.UUID
	CreatedAt  time.Time
}

// MaxThread is the most comments a record's thread reads at once.
const MaxThread = 1000

// RecordComments are the comments on records: audited, append-only. Who may read or write one is the caller's decision
// from the record's own row of Appendix D; the port holds the thread.
type RecordComments interface {
	// Add keeps a comment as the next of its record's thread.
	Add(ctx context.Context, n NewRecordComment) (RecordComment, error)
	// Thread is a record's comments, oldest first, at most MaxThread.
	Thread(ctx context.Context, recordType string, recordID uuid.UUID) ([]RecordComment, error)
}
