package ports

import (
	"context"
	"errors"
)

// InboundMail fetches what a provider received for an email-in address (FR-INT-22): the message, the receiving
// server's SPF, DKIM and DMARC results, and its attachments. Adapters: Resend receiving (PRD-001 open question 20).
// Everything it returns is untrusted data (AI-18).
type InboundMail interface {
	// Name names the provider, as its webhooks do.
	Name() string
	// Fetch reads the received message with the provider's id.
	Fetch(ctx context.Context, id string) (InboundEmail, error)
	// Attachment reads one of its attachments, at most limit bytes: ErrTooLarge beyond.
	Attachment(ctx context.Context, id string, a InboundAttachment, limit int64) ([]byte, error)
}

// InboundEmail is a received message.
type InboundEmail struct {
	From, Subject    string
	Text, HTML       string
	SPF, DKIM, DMARC string // the receiving server's results, such as "pass"; empty when it gave none
	Attachments      []InboundAttachment
}

// InboundAttachment is a received message's attachment, as the provider describes it.
type InboundAttachment struct {
	ID, FileName, ContentType string
	Size                      int64
}

// ErrTooLarge means an attachment is larger than the caller takes.
var ErrTooLarge = errors.New("the attachment is too large")
