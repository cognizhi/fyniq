package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CaptureAddress is an email-in address documents are captured from (FR-DOC-12, FR-INT-22; PRD-001 rev 0.105): the
// organisation's, or a member's own, a readable name on the deployment's capture domain. It is only ever retired
// (FY182), and its name never passes to another organisation (FY183, CodeCaptureName).
type CaptureAddress struct {
	ID        uuid.UUID
	LocalPart string
	UserID    *uuid.UUID // a member's own; nil: the organisation's
	CreatedBy string
	CreatedAt time.Time
	RetiredAt *time.Time
}

// CaptureRoute is where mail for a live name goes: its organisation, address and, for a member's own, the member.
type CaptureRoute struct {
	Organisation uuid.UUID
	Address      uuid.UUID
	UserID       *uuid.UUID
}

// The decisions about a sender at an email-in address.
const (
	SenderAccepted = "accepted"
	SenderRejected = "rejected"
)

// CaptureSender is a decision about a sender at the organisation's address (UserID nil) or a member's: the latest is in
// force, and none is changed (FY184).
type CaptureSender struct {
	ID        uuid.UUID
	Address   string // in lower case
	UserID    *uuid.UUID
	Decision  string
	DecidedBy string
	DecidedAt time.Time
}

// CaptureAddresses are the organisation's email-in addresses and its decisions about senders. Each change is audited.
type CaptureAddresses interface {
	// Create makes the live address named localPart of user, or of the organisation when user is nil, retiring the one
	// it replaces; CodeCaptureName when another organisation has the name.
	Create(ctx context.Context, localPart string, user *uuid.UUID) (CaptureAddress, error)
	// Live are the organisation's live addresses.
	Live(ctx context.Context) ([]CaptureAddress, error)
	// Retire retires a live address; CodeVersion when it was retired already.
	Retire(ctx context.Context, id uuid.UUID) (CaptureAddress, error)
	// Decide records a decision about sender at the organisation's address (user nil) or user's.
	Decide(ctx context.Context, sender string, user *uuid.UUID, decision string) (CaptureSender, error)
	// Decisions are the decisions in force: the latest for each sender and address.
	Decisions(ctx context.Context) ([]CaptureSender, error)
}

// CaptureRoutes find where inbound mail goes, before any tenant is set.
type CaptureRoutes interface {
	// CaptureRoute is the route of the live address named localPart; ports.ErrNotFound when there is none.
	CaptureRoute(ctx context.Context, localPart string) (CaptureRoute, error)
}
