package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NewDocumentSend is a quote or invoice sent by email (FR-AR-06): the version sent, the email message that carries it,
// the evidence file of its PDF, the recipient, and the template version that rendered it.
type NewDocumentSend struct {
	DocumentID      uuid.UUID
	DocumentVersion int
	MessageID       uuid.UUID
	EvidenceID      uuid.UUID
	Recipient       string
	TemplateSource  string     // contact, default or platform
	TemplateID      *uuid.UUID // nil for the platform's starter
	TemplateVersion int
	TemplateHash    string // hex
	PDFSHA256       string // hex
}

// DocumentSend is a send as recorded, never changed (FY195), with how its message stands: the message's status
// (EmailQueued, EmailAccepted, EmailFailed, EmailUnknown) and the kind of the provider's latest event about it, if any.
type DocumentSend struct {
	ID uuid.UUID
	NewDocumentSend
	SentBy        string
	SentAt        time.Time
	MessageStatus string
	LastEvent     string
}

// DocumentSends are the organisation's quotes and invoices sent by email. Every send is audited.
type DocumentSends interface {
	Record(ctx context.Context, s NewDocumentSend) (DocumentSend, error)
	// OfDocument are a document's sends, newest first.
	OfDocument(ctx context.Context, documentID uuid.UUID) ([]DocumentSend, error)
	// Latest are the latest send of each document named that has one.
	Latest(ctx context.Context, documentIDs []uuid.UUID) (map[uuid.UUID]DocumentSend, error)
}
