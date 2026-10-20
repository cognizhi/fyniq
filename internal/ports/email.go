package ports

import (
	"context"
	"errors"
	"time"
)

// Email sends outbound email (FR-INT-21). Adapters: SMTP (Mailpit locally, or any relay); ZeptoMail and
// Resend arrive as connectors. A send goes through the outbox, so it happens once, or Fyniq knows it may
// not have (G4-073).
type Email interface {
	// Name names the provider, for the message's record.
	Name() string
	// Send hands the message to the provider. A nil error means the provider accepted it. ErrEmailRefused
	// means it did not, and sending again is safe; any other error leaves it unknown whether it was accepted.
	Send(ctx context.Context, m OutboundEmail) (EmailReceipt, error)
	// Status asks the provider what became of the message with key, before any retry: ErrEmailUnknown when
	// the provider cannot say (SMTP never can).
	Status(ctx context.Context, key string) (EmailReceipt, error)
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
