package ports

import (
	"context"
	"errors"
	"time"
)

// Email sends outbound email (FR-INT-21). Adapters: SMTP (Mailpit locally, or any relay), ZeptoMail and Resend;
// the deployment names them in order, the first primary and the next its failover (PRD-001 rev 0.102). A send
// goes through the outbox, so it happens once, or Fyniq knows it may not have (G4-073).
type Email interface {
	// Name names the provider, for the message's record.
	Name() string
	// Send hands the message to the provider. A nil error means the provider accepted it. ErrEmailRefused
	// means it did not, and sending again is safe; any other error leaves it unknown whether it was accepted.
	Send(ctx context.Context, m OutboundEmail) (EmailReceipt, error)
	// Status asks the provider what became of m, which an attempt begun at attempted may have handed over, before
	// any retry: a nil error means it accepted m; ErrEmailRefused that it never did, so sending is safe;
	// ErrEmailUnknown that it cannot say (SMTP and ZeptoMail never can). A provider that keeps idempotency keys
	// answers by sending m again under its key while it still keeps the key (Resend: 24 hours).
	Status(ctx context.Context, m OutboundEmail, attempted time.Time) (EmailReceipt, error)
}

// OutboundEmail is one plain-text message. Key identifies it to the provider (an SMTP Message-ID, or the
// idempotency key of a provider that takes one).
type OutboundEmail struct {
	Key     string
	From    string // an RFC 5322 address, such as "Fyniq <no-reply@example.com>"
	To      []string
	Subject string
	Text    string
	Date    time.Time
}

// EmailReceipt is the provider's acceptance of a message.
type EmailReceipt struct {
	ProviderID string // the provider's reference, such as the Message-ID it accepted
}

// The email outcomes an adapter reports.
var (
	ErrEmailRefused = errors.New("the email provider refused the message")
	ErrEmailUnknown = errors.New("the email provider cannot say what became of the message")
)
