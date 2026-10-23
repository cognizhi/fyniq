package doctemplate

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The sources of the template that applies (FR-TPL-05).
const (
	SourceContact  = "contact"  // the contact's override
	SourceDefault  = "default"  // the organisation's default for the kind and type
	SourcePlatform = "platform" // the platform's classic starter, version 0
)

// Applied is the template version that renders a document: what the document records with it (FR-TPL-04, FR-DOC-04).
type Applied struct {
	Source     string
	TemplateID *uuid.UUID // nil for the platform's starter
	Name       string
	Version    int // 0 for the platform's starter
	Content    Content
	Hash       []byte
}

// Customers are the document types sent to customers; the others (remittance advice, purchase orders) go to suppliers.
var Customers = []string{Invoice, Quote, CreditNote, Receipt, Statement, Reminder}

// ForCustomers reports whether a document type is sent to customers.
func ForCustomers(documentType string) bool { return slices.Contains(Customers, documentType) }

// Applying is the template version that renders a document of kind and documentType for contact (nil when it has none):
// the contact's override, else the organisation's default, else the platform's classic starter as version 0 (the owner's
// decisions of 23 Oct 2026, rev 0.98). A chosen template renders at its version in force.
func Applying(ctx context.Context, u storage.UnitOfWork, kind, documentType string, contact *uuid.UUID) (Applied, error) {
	pick := func(cs []storage.TemplateChoice) *uuid.UUID {
		for _, c := range cs {
			if c.Kind == kind && c.DocumentType == documentType {
				return c.TemplateID
			}
		}
		return nil
	}
	source, id := SourcePlatform, (*uuid.UUID)(nil)
	if contact != nil {
		cs, err := u.DocumentTemplates().ContactTemplates(ctx, *contact)
		if err != nil {
			return Applied{}, err
		}
		if id = pick(cs); id != nil {
			source = SourceContact
		}
	}
	if id == nil {
		cs, err := u.DocumentTemplates().Defaults(ctx)
		if err != nil {
			return Applied{}, err
		}
		if id = pick(cs); id != nil {
			source = SourceDefault
		}
	}
	if id == nil {
		c := Default(kind, documentType)
		h, err := Hash(c)
		return Applied{Source: SourcePlatform, Name: Starters[0].Name, Content: c, Hash: h}, err
	}
	t, err := u.DocumentTemplates().Get(ctx, *id)
	if err != nil {
		return Applied{}, err
	}
	v, err := u.DocumentTemplates().Version(ctx, t.ID, t.Published) // a choice names a published template (FY178)
	if err != nil {
		return Applied{}, err
	}
	c, err := Read(v.Content)
	if err != nil {
		return Applied{}, err
	}
	return Applied{Source: source, TemplateID: id, Name: t.Name, Version: v.Number, Content: c, Hash: v.Hash}, nil
}
