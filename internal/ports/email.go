package ports

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
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

// OutboundEmail is one plain-text message, with any files it carries. Key identifies it to the provider (an SMTP
// Message-ID, or the idempotency key of a provider that takes one). Reference is what the provider echoes in its
// webhooks, so an event finds its organisation and message: letters, digits, dashes and underscores only.
type OutboundEmail struct {
	Key         string
	Reference   string
	From        string // an RFC 5322 address, such as "Fyniq <no-reply@example.com>"
	To          []string
	Subject     string
	Text        string
	Date        time.Time
	Attachments []EmailAttachment // such as an invoice's PDF (FR-AR-06); none for most messages
}

// EmailAttachment is a file a message carries.
type EmailAttachment struct {
	Name        string // the file's name, as the recipient sees it, such as INV-0042.pdf
	ContentType string // one of AttachmentTypes
	Content     []byte
}

// AttachmentTypes are the files a message may carry: the PDFs Fyniq renders.
var AttachmentTypes = []string{"application/pdf"}

// MaxAttachments and MaxAttachmentBytes bound what one message carries, well inside every provider's limit.
const (
	MaxAttachments     = 5
	MaxAttachmentBytes = 10 << 20 // all of a message's files together
)

// attachmentName is a file name a header carries safely.
var attachmentName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,99}$`)

// CheckAttachments says why a message's files cannot go, wrapped in ErrEmailRefused: too many or too large, a name
// a header could not carry safely, an empty file or a type not in AttachmentTypes.
func CheckAttachments(as []EmailAttachment) error {
	if len(as) > MaxAttachments {
		return fmt.Errorf("%w: %d attachments; the most is %d", ErrEmailRefused, len(as), MaxAttachments)
	}
	total := 0
	for _, a := range as {
		total += len(a.Content)
		switch {
		case !attachmentName.MatchString(a.Name):
			return fmt.Errorf("%w: the attachment name %q is not one a message carries", ErrEmailRefused, a.Name)
		case !slices.Contains(AttachmentTypes, a.ContentType):
			return fmt.Errorf("%w: the attachment %s is a %s, not a PDF", ErrEmailRefused, a.Name, a.ContentType)
		case len(a.Content) == 0:
			return fmt.Errorf("%w: the attachment %s is empty", ErrEmailRefused, a.Name)
		}
	}
	if total > MaxAttachmentBytes {
		return fmt.Errorf("%w: the attachments come to %d bytes; the most is %d", ErrEmailRefused, total, MaxAttachmentBytes)
	}
	return nil
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
