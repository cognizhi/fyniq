package api

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ChosenTemplate is a template a choice names, at its version in force.
type ChosenTemplate struct {
	ID               string `json:"id" format:"uuid" doc:"The template." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f10"`
	Name             string `json:"name" doc:"Its name." example:"Standard invoice"`
	PublishedVersion int    `json:"published_version" doc:"Its version in force, which renders the documents." example:"2"`
}

// DocumentTemplateChoice is the template chosen for a kind and document type (FR-TPL-05; the owner's decisions of 23 Oct 2026,
// rev 0.98): the organisation's default, or a contact's override.
type DocumentTemplateChoice struct {
	Kind         string          `json:"kind" enum:"document,email" doc:"document: the PDF; email: the message that carries it." example:"document"`
	DocumentType string          `json:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document type." example:"invoice"`
	Template     *ChosenTemplate `json:"template,omitempty" doc:"The template chosen. Absent: for a default, the platform's classic starter renders; for a contact, the default does."`
	SetBy        *Person         `json:"set_by,omitempty" doc:"Who chose it last; absent when nobody has."`
	SetAt        string          `json:"set_at,omitempty" format:"date-time" doc:"When." example:"2026-10-23T10:00:00Z"`
	ETag         string          `json:"etag" doc:"The choice's ETag: send it in If-Match to change a default. A contact's choice changes with the contact's ETag instead." example:"\"none\""`
}

// DocumentTemplateChoicePage is the choices of every kind and document type, a page at a time.
type DocumentTemplateChoicePage struct {
	Items      []DocumentTemplateChoice `json:"items" nullable:"false" doc:"One per kind and document type, documents first."`
	NextCursor string                   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6ImRvY3VtZW50L2ludm9pY2UifQ"`
}

// TemplateChoiceChange chooses a template, or none.
type TemplateChoiceChange struct {
	TemplateID string `json:"template_id,omitempty" format:"uuid" doc:"A published template of the kind and document type. Left out: no choice, so the default applies (for a contact), or the platform's classic starter (for a default)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f10"`
}

// AppliedTemplate is the template version that renders a document (FR-TPL-04, FR-TPL-05).
type AppliedTemplate struct {
	Kind         string          `json:"kind" doc:"The kind." example:"document"`
	DocumentType string          `json:"document_type" doc:"The document type." example:"invoice"`
	Source       string          `json:"source" enum:"contact,default,platform" doc:"contact: the contact's override; default: the organisation's default; platform: the platform's classic starter, as version 0." example:"default"`
	Template     *ChosenTemplate `json:"template,omitempty" doc:"The template; absent for the platform's starter."`
	Version      int             `json:"version" doc:"The version that renders, 0 for the platform's starter: what each document records." example:"2"`
	Hash         string          `json:"content_hash" doc:"The SHA-256 of that version's content, in hex." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

// TemplateKindPath names a kind and document type.
type TemplateKindPath struct {
	Kind         string `path:"kind" enum:"document,email" doc:"The kind." example:"document"`
	DocumentType string `path:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document type." example:"invoice"`
}

type templateDefaultsInput struct {
	PageQuery
}

type templateDefaultInput struct {
	TemplateKindPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body TemplateChoiceChange
}

type contactTemplatesInput struct {
	ContactPath
	PageQuery
}

type contactTemplateInput struct {
	ContactPath
	TemplateKindPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body TemplateChoiceChange
}

type appliedTemplateInput struct {
	Kind         string `query:"kind" required:"true" enum:"document,email" doc:"The kind." example:"document"`
	DocumentType string `query:"document_type" required:"true" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document type." example:"invoice"`
	ContactID    string `query:"contact_id" format:"uuid" doc:"The customer or supplier the document is for, whose override applies first." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
}

type templateChoicesOutput struct{ Body DocumentTemplateChoicePage }

type templateChoiceOutput struct {
	ETag     string `header:"ETag" doc:"The choice in force, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DocumentTemplateChoice
}

type appliedTemplateOutput struct{ Body AppliedTemplate }

// choiceKeys are every kind and document type, in the order the lists give them: documents first.
func choiceKeys() [][2]string {
	var out [][2]string
	for _, k := range doctemplate.Kinds {
		for _, t := range doctemplate.Types {
			out = append(out, [2]string{k, t})
		}
	}
	return out
}

// choiceETag is the ETag of the choice in force: its ID, or "none" when nothing was ever chosen.
func choiceETag(c *storage.TemplateChoice) string {
	if c == nil {
		return etag("none")
	}
	return etag(c.ID)
}

func inForce(cs []storage.TemplateChoice, kind, typ string) *storage.TemplateChoice {
	i := slices.IndexFunc(cs, func(c storage.TemplateChoice) bool { return c.Kind == kind && c.DocumentType == typ })
	if i < 0 {
		return nil
	}
	return &cs[i]
}

// choiceOf is a choice as the API shows it.
func choiceOf(ctx context.Context, u storage.UnitOfWork, kind, typ string, c *storage.TemplateChoice) (DocumentTemplateChoice, error) {
	out := DocumentTemplateChoice{Kind: kind, DocumentType: typ, ETag: choiceETag(c)}
	if c == nil {
		return out, nil
	}
	out.SetBy, out.SetAt = personOf(c.SetBy), c.SetAt.UTC().Format(time.RFC3339)
	if c.TemplateID != nil {
		t, err := u.DocumentTemplates().Get(ctx, *c.TemplateID)
		if err != nil {
			return DocumentTemplateChoice{}, err
		}
		out.Template = &ChosenTemplate{ID: t.ID.String(), Name: t.Name, PublishedVersion: t.Published}
	}
	return out, nil
}

// choicePage is one page of choices, after the key the cursor names.
func (a *API) choicePage(ctx context.Context, u storage.UnitOfWork, cs []storage.TemplateChoice, in PageQuery) (DocumentTemplateChoicePage, error) {
	out := DocumentTemplateChoicePage{Items: []DocumentTemplateChoice{}}
	keys := choiceKeys()
	if in.Cursor != "" {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return out, err
		}
		i := slices.IndexFunc(keys, func(k [2]string) bool { return k[0]+"/"+k[1] == after })
		if i < 0 {
			return out, badCursor()
		}
		keys = keys[i+1:]
	}
	if len(keys) > in.PageSize {
		keys = keys[:in.PageSize]
		out.NextCursor = encodeCursor(keys[len(keys)-1][0] + "/" + keys[len(keys)-1][1])
	}
	var people []*Person
	for _, k := range keys {
		x, err := choiceOf(ctx, u, k[0], k[1], inForce(cs, k[0], k[1]))
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, x)
	}
	for i := range out.Items {
		people = append(people, out.Items[i].SetBy)
	}
	return out, a.name(ctx, u, people...)
}

// chosen reads the template a change names: nil for none; refused unless it is a published template of kind and typ.
func chosen(ctx context.Context, u storage.UnitOfWork, kind, typ string, b TemplateChoiceChange) (*uuid.UUID, error) {
	if b.TemplateID == "" {
		return nil, nil
	}
	refuse := func(why string) error {
		return problem(doctemplate.CodeChoice, "The template "+why+".", "Choose a published "+kind+" template of a "+typ,
			FieldProblem{Code: doctemplate.CodeChoice, Field: "body.template_id", Message: why, Fix: "Publish it first, or choose another"})
	}
	id, err := uuid.Parse(b.TemplateID)
	if err != nil {
		return nil, refuse("is not an ID")
	}
	t, err := u.DocumentTemplates().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, refuse("does not exist in this organisation")
	}
	if err != nil {
		return nil, err
	}
	switch {
	case t.Kind != kind || t.DocumentType != typ:
		return nil, refuse(fmt.Sprintf("is a %s template of a %s, not a %s template of a %s", t.Kind, t.DocumentType, kind, typ))
	case t.Published == 0:
		return nil, refuse("has no published version")
	}
	return &id, nil
}

func registerTemplateChoices(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListDocumentTemplateDefaults, huma.Operation{
		OperationID: "listDocumentTemplateDefaults", Method: http.MethodGet, Path: "/v1/document-template-defaults", Tags: []string{"Templates"},
		Summary: "List the default templates",
		Description: "The default template of every kind and document type (FR-TPL-05), a page at a time: the published template chosen, " +
			"or none, when the platform's classic starter renders the type's documents as version 0. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *templateDefaultsInput) (*templateChoicesOutput, error) {
		p, _ := authn.From(ctx)
		var out DocumentTemplateChoicePage
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			cs, err := u.DocumentTemplates().Defaults(ctx)
			if err != nil {
				return err
			}
			out, err = a.choicePage(ctx, u, cs, in.PageQuery)
			return err
		})
		return &templateChoicesOutput{Body: out}, err
	})

	register(a, authz.SetDocumentTemplateDefault, huma.Operation{
		OperationID: "setDocumentTemplateDefault", Method: http.MethodPut, Path: "/v1/document-template-defaults/{kind}/{document_type}", Tags: []string{"Templates"},
		Summary: "Choose a default template",
		Description: "Chooses the default template of a kind and document type among its published templates, or none, so the platform's " +
			"classic starter renders (FR-TPL-05; the owner's decision of 23 Oct 2026): a person who holds the approve right chooses, with no " +
			"separate request, since each template was approved when it was published. A contact's override comes first. Refused for a " +
			"template that is not a published one of the kind and type (TPL-19). Side effects: the choice and its audit event. Needs If-Match " +
			"with the choice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, doctemplate.CodeChoice, storage.CodeTemplate}, locks...),
		func(ctx context.Context, in *templateDefaultInput) (*templateChoiceOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DocumentTemplateChoice, error) {
				cs, err := u.DocumentTemplates().Defaults(ctx)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				if err := checkIfMatch(in.IfMatch, choiceETag(inForce(cs, in.Kind, in.DocumentType))); err != nil {
					return DocumentTemplateChoice{}, err
				}
				id, err := chosen(ctx, u, in.Kind, in.DocumentType, in.Body)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				c, err := u.DocumentTemplates().SetDefault(ctx, in.Kind, in.DocumentType, id)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				out, err := choiceOf(ctx, u, in.Kind, in.DocumentType, &c)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				return out, a.name(ctx, u, out.SetBy)
			})
			if err != nil {
				return nil, err
			}
			out := &templateChoiceOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag = a.defaultETag(ctx, in.Kind, in.DocumentType)
			}
			return out, nil
		})

	register(a, authz.ListContactDocumentTemplates, huma.Operation{
		OperationID: "listContactDocumentTemplates", Method: http.MethodGet, Path: "/v1/contacts/{contact_id}/document-templates", Tags: []string{"Contacts"},
		Summary: "List a contact's templates",
		Description: "A contact's template of every kind and document type (FR-TPL-05), a page at a time: the published template chosen " +
			"for it, or none, when the organisation's default applies. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeCursor}, locks...), func(ctx context.Context, in *contactTemplatesInput) (*templateChoicesOutput, error) {
		id, err := parseID("contact_id", in.ContactID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out DocumentTemplateChoicePage
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Contacts().Get(ctx, id); err != nil {
				return err
			}
			cs, err := u.DocumentTemplates().ContactTemplates(ctx, id)
			if err != nil {
				return err
			}
			out, err = a.choicePage(ctx, u, cs, in.PageQuery)
			return err
		})
		return &templateChoicesOutput{Body: out}, err
	})

	register(a, authz.SetContactDocumentTemplate, huma.Operation{
		OperationID: "setContactDocumentTemplate", Method: http.MethodPut, Path: "/v1/contacts/{contact_id}/document-templates/{kind}/{document_type}", Tags: []string{"Contacts"},
		Summary: "Choose a contact's template",
		Description: "Chooses the template a contact's documents of a kind and document type render with, among the type's published " +
			"templates, or none, so the organisation's default applies (FR-TPL-05; the owner's decision of 23 Oct 2026): set on the contact " +
			"by whoever edits contacts. A customer's documents are invoices, quotes, credit notes, receipts, statements and reminders; a " +
			"supplier's, remittance advice and purchase orders (TPL-20). Refused for a template that is not a published one of the kind and " +
			"type (TPL-19). Side effects: the choice and its audit event on the contact; the contact's version does not change. Needs " +
			"If-Match with the contact's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, doctemplate.CodeChoice, doctemplate.CodeParty, storage.CodeTemplate}, locks...),
		func(ctx context.Context, in *contactTemplateInput) (*templateChoiceOutput, error) {
			contactID, err := parseID("contact_id", in.ContactID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DocumentTemplateChoice, error) {
				ct, err := u.Contacts().Get(ctx, contactID)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(ct.Version)); err != nil {
					return DocumentTemplateChoice{}, err
				}
				if customer := doctemplate.ForCustomers(in.DocumentType); (customer && !ct.Customer) || (!customer && !ct.Vendor) {
					who := map[bool]string{true: "a customer", false: "a supplier"}[customer]
					return DocumentTemplateChoice{}, problem(doctemplate.CodeParty, fmt.Sprintf("%s is not %s, so it receives no %s.", ct.LegalName, who,
						map[bool]string{true: "invoices, quotes, credit notes, receipts, statements or reminders", false: "remittance advice or purchase orders"}[customer]),
						"Mark the contact as "+who+" first, or choose a template of another document type")
				}
				id, err := chosen(ctx, u, in.Kind, in.DocumentType, in.Body)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				c, err := u.DocumentTemplates().SetContactTemplate(ctx, contactID, in.Kind, in.DocumentType, id)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				out, err := choiceOf(ctx, u, in.Kind, in.DocumentType, &c)
				if err != nil {
					return DocumentTemplateChoice{}, err
				}
				return out, a.name(ctx, u, out.SetBy)
			})
			if err != nil {
				return nil, err
			}
			out := &templateChoiceOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag = a.contactETag(ctx, contactID)
			}
			return out, nil
		})

	register(a, authz.GetAppliedDocumentTemplate, huma.Operation{
		OperationID: "getAppliedDocumentTemplate", Method: http.MethodGet, Path: "/v1/document-template-resolution", Tags: []string{"Templates"},
		Summary: "Find the template that applies",
		Description: "The template version that renders a document of a kind and document type, for a contact when named (FR-TPL-05): " +
			"the contact's override, else the organisation's default, else the platform's classic starter as version 0, with the " +
			"version and content hash each document records (FR-TPL-04). Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeInvalid}, locks...), func(ctx context.Context, in *appliedTemplateInput) (*appliedTemplateOutput, error) {
		var contact *uuid.UUID
		if in.ContactID != "" {
			id, err := uuid.Parse(in.ContactID)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.contact_id", Message: "is not an ID", Fix: "Send the contact's ID"}})
			}
			contact = &id
		}
		p, _ := authn.From(ctx)
		var out AppliedTemplate
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if contact != nil {
				if _, err := u.Contacts().Get(ctx, *contact); err != nil {
					return err
				}
			}
			ap, err := doctemplate.Applying(ctx, u, in.Kind, in.DocumentType, contact)
			if err != nil {
				return err
			}
			out = AppliedTemplate{Kind: in.Kind, DocumentType: in.DocumentType, Source: ap.Source, Version: ap.Version, Hash: hex.EncodeToString(ap.Hash)}
			if ap.TemplateID != nil {
				out.Template = &ChosenTemplate{ID: ap.TemplateID.String(), Name: ap.Name, PublishedVersion: ap.Version}
			}
			return nil
		})
		return &appliedTemplateOutput{Body: out}, err
	})
}

// defaultETag is the ETag of a kind and type's default after a change.
func (a *API) defaultETag(ctx context.Context, kind, typ string) string {
	p, _ := authn.From(ctx)
	tag := ""
	_ = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		cs, err := u.DocumentTemplates().Defaults(ctx)
		tag = choiceETag(inForce(cs, kind, typ))
		return err
	})
	return tag
}

// contactETag is a contact's ETag: its version, which a template choice does not change.
func (a *API) contactETag(ctx context.Context, id uuid.UUID) string {
	p, _ := authn.From(ctx)
	tag := ""
	_ = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Get(ctx, id)
		tag = etag(c.Version)
		return err
	})
	return tag
}
