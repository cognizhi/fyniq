package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DocumentTemplate is a document or email template (FR-TPL-04, FR-TPL-05; PRD-001 rev 0.95): its draft, saved over in
// place at the next version, and the number of its latest published version, 0 before the first. A template is never
// deleted (FY176).
type DocumentTemplate struct {
	ID                 uuid.UUID
	Kind, DocumentType string
	Name               string
	Draft              []byte // the draft's content, as JSON
	Version            int
	Published          int
	SavedBy            string
	SavedAt, CreatedAt time.Time
}

// NewDocumentTemplate is a template to create, as a draft at version 1.
type NewDocumentTemplate struct {
	Kind, DocumentType, Name string
	Draft                    []byte
}

// TemplateVersion is a published version of a template: its content and SHA-256, never changed (FY176).
type TemplateVersion struct {
	ID              uuid.UUID
	TemplateID      uuid.UUID
	Number          int
	Content         []byte
	Hash            []byte
	Note            string
	RepublishedFrom int // a rollback: the earlier version whose content this is; 0 otherwise
	PublishedBy     string
	PublishedAt     time.Time
}

// NewTemplateVersion is a version to publish: the template's next number, from its version read.
type NewTemplateVersion struct {
	Content         []byte
	Hash            []byte
	Note            string
	RepublishedFrom int
}

// TemplateFilter narrows a page of templates.
type TemplateFilter struct {
	Kind, DocumentType string // "" for any
}

// DocumentTemplates are the organisation's templates. Every change is audited on the template.
type DocumentTemplates interface {
	Create(ctx context.Context, t NewDocumentTemplate) (DocumentTemplate, error)
	Get(ctx context.Context, id uuid.UUID) (DocumentTemplate, error)
	// Lock reads a template FOR UPDATE.
	Lock(ctx context.Context, id uuid.UUID) (DocumentTemplate, error)
	// Page returns at most limit templates after the ID after (uuid.Nil for the first page), by ID.
	Page(ctx context.Context, after uuid.UUID, limit int, f TemplateFilter) ([]DocumentTemplate, error)
	// SaveDraft saves a template's name and draft at its version, and returns it at its next: CodeVersion when it
	// changed since.
	SaveDraft(ctx context.Context, id uuid.UUID, version int, name string, draft []byte) (DocumentTemplate, error)
	// Publish publishes v as a template's next version, from its version read: CodeVersion when it changed since.
	Publish(ctx context.Context, id uuid.UUID, version int, v NewTemplateVersion) (DocumentTemplate, TemplateVersion, error)
	// Versions returns at most limit published versions numbered below before (0 for the latest), newest first.
	Versions(ctx context.Context, id uuid.UUID, before, limit int) ([]TemplateVersion, error)
	// Version returns one published version: ports.ErrNotFound when there is none.
	Version(ctx context.Context, id uuid.UUID, number int) (TemplateVersion, error)
}
