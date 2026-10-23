package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The statuses of a message received at an email-in address (FR-INT-22; PRD-001 rev 0.106): received until the
// worker fetches it, then accepted, quarantined, rejected or failed; a quarantined one is later accepted, rejected or
// expired. The status moves forward only (FY185).
const (
	InboundReceived    = "received"
	InboundAccepted    = "accepted"
	InboundQuarantined = "quarantined"
	InboundRejected    = "rejected"
	InboundExpired     = "expired"
	InboundFailed      = "failed"
)

// InboundMessage is a message received at an email-in address: what it was received as, and its standing.
type InboundMessage struct {
	ID                   uuid.UUID
	AddressID            uuid.UUID
	Provider, ProviderID string
	Sender               string // in lower case
	Subject              string
	ReceivedAt           time.Time
	Status               string
	SPF, DKIM, DMARC     string // the receiving server's results; empty until fetched
	FetchedAt, ExpiresAt *time.Time
	DecidedBy            string
	DecidedAt            *time.Time
	LastError            string
}

// NewInboundMessage is a message a provider announced.
type NewInboundMessage struct {
	AddressID            uuid.UUID
	Provider, ProviderID string
	Sender, Subject      string
	ReceivedAt           time.Time
}

// InboundFetch is what fetching a message decided: its authentication results and its status.
type InboundFetch struct {
	SPF, DKIM, DMARC string
	Status           string // InboundAccepted, InboundQuarantined, InboundRejected or InboundFailed
	ExpiresAt        *time.Time
	LastError        string
}

// The kinds of a message's files.
const (
	InboundBody       = "body"
	InboundAttachment = "attachment"
)

// InboundFile is a message's body or one of its attachments, checked and scanned, kept as untrusted data (AI-18) in
// the staging store under BlobKey; only ever removed, once (FY186).
type InboundFile struct {
	ID, MessageID  uuid.UUID
	Kind           string
	FileName       string
	MediaType      string
	Size           int64
	SHA256         string
	OriginalSHA256 string
	BlobKey        string
	StoredAt       time.Time
	RemovedAt      *time.Time
}

// InboundFilter narrows a list of messages: by status ("" for any) and address (nil for any), newest first, after a
// message's ID.
type InboundFilter struct {
	Status  string
	Address *uuid.UUID
	Before  uuid.UUID // uuid.Nil: from the newest
	Limit   int
}

// InboundMessages are the organisation's received messages and their files. Each change is audited.
type InboundMessages interface {
	// Receive records a message the provider announced; false, and nothing recorded, when it was recorded already.
	Receive(ctx context.Context, n NewInboundMessage) (InboundMessage, bool, error)
	Get(ctx context.Context, id uuid.UUID) (InboundMessage, error)
	// Fetched records what the fetch found and decided, at at.
	Fetched(ctx context.Context, id uuid.UUID, f InboundFetch, at time.Time) (InboundMessage, error)
	// AddFile records a stored file of a received message.
	AddFile(ctx context.Context, f InboundFile) (InboundFile, error)
	// Files are a message's files, removed ones too.
	Files(ctx context.Context, message uuid.UUID) ([]InboundFile, error)
	// Decide moves a quarantined message to accepted, rejected or expired; CodeVersion when it is not quarantined.
	Decide(ctx context.Context, id uuid.UUID, status string) (InboundMessage, error)
	// RemoveFiles removes a message's files that are not removed yet, and returns them, for their blobs to be deleted.
	RemoveFiles(ctx context.Context, message uuid.UUID) ([]InboundFile, error)
	// List is a page of messages, newest first.
	List(ctx context.Context, f InboundFilter) ([]InboundMessage, error)
	// QuarantinedFrom are the quarantined messages from sender at an address.
	QuarantinedFrom(ctx context.Context, sender string, address uuid.UUID) ([]InboundMessage, error)
	// DueToExpire are the quarantined messages whose end is at or before now.
	DueToExpire(ctx context.Context, now time.Time) ([]InboundMessage, error)
	// MemberEmails are the organisation's active members' verified sign-in emails, by user.
	MemberEmails(ctx context.Context) (map[uuid.UUID]string, error)
	// ContactEmails are the organisation's active contacts' billing and remittance emails, in lower case.
	ContactEmails(ctx context.Context) ([]string, error)
}
