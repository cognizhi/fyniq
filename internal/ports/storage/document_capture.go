package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// The sources and statuses of a captured document (PRD-001 FR-DOC-12, FR-DOC-14; p1-document-capture design D1).
const (
	CaptureUpload = "upload"
	CapturePaste  = "paste"
	CaptureEmail  = "email"

	CaptureReceived  = "received"
	CaptureReading   = "reading"
	CaptureRead      = "read"
	CaptureDuplicate = "duplicate"
	CaptureDrafted   = "drafted"
	CaptureDismissed = "dismissed"
	CaptureFailed    = "failed"

	CaptureDraftBill  = "bill"
	CaptureDraftClaim = "claim"
)

// DocumentCapture is one file of the document inbox: what it is, who captured it, the model's reading of it and the platform's checks of
// that reading (data, never a figure), the draft a person made of it and how it ended.
type DocumentCapture struct {
	ID        uuid.UUID
	Source    string
	FileName  string
	MediaType string
	Size      int64
	SHA256    string
	BlobKey   string
	// MessageID and InboundFileID name the accepted email message and its file the document came from; nil for an upload or a paste.
	MessageID, InboundFileID *uuid.UUID
	// OwnerUserID is the member whose own address it arrived at: only they see it. Nil: the organisation's address, an upload or a paste.
	OwnerUserID                 *uuid.UUID
	CapturedBy                  string
	Status                      string
	Kind                        string
	Reading                     json.RawMessage
	ReadingModel, ReadingPrompt string
	Readings                    int
	ReadAt                      *time.Time
	Checks                      json.RawMessage
	DuplicateOf                 *uuid.UUID
	DraftKind                   string
	DraftID                     *uuid.UUID
	DraftedBy                   *uuid.UUID
	DraftedAt                   *time.Time
	DismissedBy                 *uuid.UUID
	DismissedAt                 *time.Time
	DismissReason               string
	LastError                   string
	CreatedAt                   time.Time
	Version                     int
}

// NewDocumentCapture is a file to capture. Status is CaptureReceived, or CaptureDuplicate with DuplicateOf the capture it duplicates.
type NewDocumentCapture struct {
	Source, FileName, MediaType, SHA256, BlobKey, CapturedBy, Status string
	Size                                                             int64
	MessageID, InboundFileID, OwnerUserID, DuplicateOf               *uuid.UUID
	At                                                               time.Time
}

// CapturedReading is what a reading made: the kind it found, the reading and its checks, as the model answered and the platform checked.
type CapturedReading struct {
	Kind          string
	Reading       json.RawMessage
	Model, Prompt string
	Checks        json.RawMessage
	At            time.Time
}

// DocumentCaptures keeps the document inbox. Each change is audited in its transaction; a change takes the version it read, and a capture moved
// meanwhile is CodeVersion. The database's guard allows only the lifecycle's moves (FY950).
type DocumentCaptures interface {
	// Capture adds a file; its ID is new.
	Capture(ctx context.Context, n NewDocumentCapture) (DocumentCapture, error)
	Get(ctx context.Context, id uuid.UUID) (DocumentCapture, error)
	// Lock is Get with the row locked until commit.
	Lock(ctx context.Context, id uuid.UUID) (DocumentCapture, error)
	// List is a page of the captures viewer may see, newest first: the organisation's and their own, never another member's. status "" is any.
	List(ctx context.Context, viewer uuid.UUID, status string, before uuid.UUID, limit int) ([]DocumentCapture, error)
	// OfHash is the live capture of the same file viewer may see, if any.
	OfHash(ctx context.Context, viewer uuid.UUID, sha256 string) (DocumentCapture, bool, error)
	// Reading moves a capture that is Reading to Read with what was read.
	Reading(ctx context.Context, id uuid.UUID, version int, r CapturedReading) (DocumentCapture, error)
	// Status moves a capture along the lifecycle, with why when it failed.
	Status(ctx context.Context, id uuid.UUID, version int, status, lastError string) (DocumentCapture, error)
	// Drafted records the draft a person made of a Read capture.
	Drafted(ctx context.Context, id uuid.UUID, version int, kind string, draft uuid.UUID, by uuid.UUID, at time.Time) (DocumentCapture, error)
	// Dismiss ends a capture nobody will draft, with a reason.
	Dismiss(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, reason string, at time.Time) (DocumentCapture, error)
}
