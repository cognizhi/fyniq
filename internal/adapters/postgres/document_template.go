package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// documentTemplates keeps the organisation's templates (FR-TPL-04, FR-TPL-05; PRD-001 rev 0.95): each with its draft and
// its published versions, which never change (FY176).
type documentTemplates struct{ u *unitOfWork }

func (u *unitOfWork) DocumentTemplates() storage.DocumentTemplates { return documentTemplates{u} }

func templateOf(r sqlcdb.GetDocumentTemplateRow) storage.DocumentTemplate {
	return storage.DocumentTemplate{ID: r.ID, Kind: r.Kind, DocumentType: r.DocumentType, Name: r.Name, Draft: r.Draft, Version: int(r.Version),
		Published: int(r.Published), SavedBy: r.SavedBy, SavedAt: r.SavedAt, CreatedAt: r.CreatedAt}
}

func templateVersionOf(r sqlcdb.GetDocumentTemplateVersionRow) storage.TemplateVersion {
	return storage.TemplateVersion{ID: r.ID, TemplateID: r.TemplateID, Number: int(r.Number), Content: r.Content, Hash: r.ContentHash, Note: r.Note,
		RepublishedFrom: int(r.RepublishedFrom), PublishedBy: r.PublishedBy, PublishedAt: r.PublishedAt}
}

func (t documentTemplates) Create(ctx context.Context, n storage.NewDocumentTemplate) (storage.DocumentTemplate, error) {
	if err := t.u.check(); err != nil {
		return storage.DocumentTemplate{}, err
	}
	row, err := t.u.q.InsertDocumentTemplate(ctx, sqlcdb.InsertDocumentTemplateParams{Kind: n.Kind, DocumentType: n.DocumentType, Name: n.Name,
		Draft: n.Draft, SavedBy: t.u.actor(ctx), SavedAt: t.u.store.clock.Now()})
	if err != nil {
		return storage.DocumentTemplate{}, mapError(err)
	}
	out := templateOf(sqlcdb.GetDocumentTemplateRow(row))
	version := out.Version
	if err := t.u.event(ctx, "document_template.created", "document_template", out.ID, &version, map[string][2]any{
		"kind": {nil, out.Kind}, "document_type": {nil, out.DocumentType}, "name": {nil, out.Name}, "draft": {nil, json.RawMessage(out.Draft)}}); err != nil {
		return storage.DocumentTemplate{}, err
	}
	return out, nil
}

func (t documentTemplates) Get(ctx context.Context, id uuid.UUID) (storage.DocumentTemplate, error) {
	if err := t.u.check(); err != nil {
		return storage.DocumentTemplate{}, err
	}
	row, err := t.u.q.GetDocumentTemplate(ctx, id)
	if err != nil {
		return storage.DocumentTemplate{}, mapError(err)
	}
	return templateOf(row), nil
}

func (t documentTemplates) Lock(ctx context.Context, id uuid.UUID) (storage.DocumentTemplate, error) {
	if err := t.u.check(); err != nil {
		return storage.DocumentTemplate{}, err
	}
	row, err := t.u.q.LockDocumentTemplate(ctx, id)
	if err != nil {
		return storage.DocumentTemplate{}, mapError(err)
	}
	return templateOf(sqlcdb.GetDocumentTemplateRow(row)), nil
}

func (t documentTemplates) Page(ctx context.Context, after uuid.UUID, limit int, f storage.TemplateFilter) ([]storage.DocumentTemplate, error) {
	if err := t.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d templates", limit), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := t.u.q.PageDocumentTemplates(ctx, sqlcdb.PageDocumentTemplatesParams{After: after, Kind: opt(f.Kind), DocumentType: opt(f.DocumentType),
		PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.DocumentTemplate, 0, len(rows))
	for _, r := range rows {
		out = append(out, templateOf(sqlcdb.GetDocumentTemplateRow(r)))
	}
	return out, nil
}

// staleTemplate is the error for a template that changed since it was read (FR-INT-09).
func staleTemplate(cur storage.DocumentTemplate, version int) error {
	return &storage.Error{Code: storage.CodeVersion,
		Message: fmt.Sprintf("the template changed since it was read: it is at version %d, not %d", cur.Version, version)}
}

func (t documentTemplates) SaveDraft(ctx context.Context, id uuid.UUID, version int, name string, draft []byte) (storage.DocumentTemplate, error) {
	before, err := t.Lock(ctx, id)
	if err != nil {
		return storage.DocumentTemplate{}, err
	}
	if before.Version != version || version < 1 || version > math.MaxInt32 {
		return storage.DocumentTemplate{}, staleTemplate(before, version)
	}
	row, err := t.u.q.SaveDocumentTemplateDraft(ctx, sqlcdb.SaveDocumentTemplateDraftParams{ID: id, Version: int32(version), Name: name, Draft: draft,
		SavedBy: t.u.actor(ctx), SavedAt: t.u.store.clock.Now()})
	if err != nil {
		return storage.DocumentTemplate{}, mapError(err)
	}
	out := templateOf(sqlcdb.GetDocumentTemplateRow(row))
	changes := map[string][2]any{"draft": {json.RawMessage(before.Draft), json.RawMessage(out.Draft)}}
	if before.Name != out.Name {
		changes["name"] = [2]any{before.Name, out.Name}
	}
	v := out.Version
	if err := t.u.event(ctx, "document_template.draft_saved", "document_template", out.ID, &v, changes); err != nil {
		return storage.DocumentTemplate{}, err
	}
	return out, nil
}

func (t documentTemplates) Publish(ctx context.Context, id uuid.UUID, version int, n storage.NewTemplateVersion) (storage.DocumentTemplate, storage.TemplateVersion, error) {
	before, err := t.Lock(ctx, id)
	if err != nil {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, err
	}
	if before.Version != version || version < 1 || version > math.MaxInt32 {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, staleTemplate(before, version)
	}
	if before.Published < 0 || before.Published >= math.MaxInt32 || n.RepublishedFrom < 0 || n.RepublishedFrom > math.MaxInt32 || n.RepublishedFrom > before.Published {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, &storage.Error{Code: storage.CodeInvalid,
			Message: fmt.Sprintf("version %d is not a published version of the template", n.RepublishedFrom), Hint: "Republish one of its versions"}
	}
	var from *int32
	if n.RepublishedFrom > 0 {
		f := int32(n.RepublishedFrom)
		from = &f
	}
	vrow, err := t.u.q.InsertDocumentTemplateVersion(ctx, sqlcdb.InsertDocumentTemplateVersionParams{TemplateID: id, Number: int32(before.Published + 1),
		Content: n.Content, ContentHash: n.Hash, Note: opt(n.Note), RepublishedFrom: from, PublishedBy: t.u.actor(ctx), PublishedAt: t.u.store.clock.Now()})
	if err != nil {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, mapError(err)
	}
	pv := templateVersionOf(sqlcdb.GetDocumentTemplateVersionRow(vrow))
	row, err := t.u.q.MarkDocumentTemplatePublished(ctx, sqlcdb.MarkDocumentTemplatePublishedParams{ID: id, Version: int32(version), Number: &vrow.Number})
	if err != nil {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, mapError(err)
	}
	out := templateOf(sqlcdb.GetDocumentTemplateRow(row))
	changes := map[string][2]any{"published": {before.Published, out.Published}, "content_hash": {nil, hex.EncodeToString(pv.Hash)}}
	if pv.Note != "" {
		changes["note"] = [2]any{nil, pv.Note}
	}
	if pv.RepublishedFrom > 0 {
		changes["republished_from"] = [2]any{nil, pv.RepublishedFrom}
	}
	v := out.Version
	if err := t.u.event(ctx, "document_template.published", "document_template", out.ID, &v, changes); err != nil {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, err
	}
	return out, pv, nil
}

func (t documentTemplates) Versions(ctx context.Context, id uuid.UUID, before, limit int) ([]storage.TemplateVersion, error) {
	if err := t.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 || before < 0 || before > math.MaxInt32 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d versions before %d", limit, before), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := t.u.q.DocumentTemplateVersions(ctx, sqlcdb.DocumentTemplateVersionsParams{TemplateID: id, Before: int32(before), PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.TemplateVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, templateVersionOf(sqlcdb.GetDocumentTemplateVersionRow(r)))
	}
	return out, nil
}

func (t documentTemplates) Version(ctx context.Context, id uuid.UUID, number int) (storage.TemplateVersion, error) {
	if err := t.u.check(); err != nil {
		return storage.TemplateVersion{}, err
	}
	if number < 1 || number > math.MaxInt32 {
		return storage.TemplateVersion{}, ports.ErrNotFound // no template has such a version
	}
	row, err := t.u.q.GetDocumentTemplateVersion(ctx, sqlcdb.GetDocumentTemplateVersionParams{TemplateID: id, Number: int32(number)})
	if err != nil {
		return storage.TemplateVersion{}, mapError(err)
	}
	return templateVersionOf(row), nil
}

func optID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func (t documentTemplates) SetDefault(ctx context.Context, kind, documentType string, template *uuid.UUID) (storage.TemplateChoice, error) {
	if err := t.u.check(); err != nil {
		return storage.TemplateChoice{}, err
	}
	row, err := t.u.q.InsertDocumentTemplateDefault(ctx, sqlcdb.InsertDocumentTemplateDefaultParams{Kind: kind, DocumentType: documentType,
		TemplateID: template, SetBy: t.u.actor(ctx), SetAt: t.u.store.clock.Now()})
	if err != nil {
		return storage.TemplateChoice{}, mapError(err)
	}
	out := storage.TemplateChoice{ID: row.ID, Kind: row.Kind, DocumentType: row.DocumentType, TemplateID: row.TemplateID, SetBy: row.SetBy, SetAt: row.SetAt}
	if err := t.u.event(ctx, "document_template.default_set", "document_template_default", out.ID, nil, map[string][2]any{
		"kind": {nil, kind}, "document_type": {nil, documentType}, "template_id": {nil, optID(template)}}); err != nil {
		return storage.TemplateChoice{}, err
	}
	return out, nil
}

func (t documentTemplates) Defaults(ctx context.Context) ([]storage.TemplateChoice, error) {
	if err := t.u.check(); err != nil {
		return nil, err
	}
	rows, err := t.u.q.DocumentTemplateDefaults(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.TemplateChoice, 0, len(rows))
	for _, r := range rows {
		out = append(out, storage.TemplateChoice{ID: r.ID, Kind: r.Kind, DocumentType: r.DocumentType, TemplateID: r.TemplateID, SetBy: r.SetBy, SetAt: r.SetAt})
	}
	return out, nil
}

func (t documentTemplates) SetContactTemplate(ctx context.Context, contact uuid.UUID, kind, documentType string, template *uuid.UUID) (storage.TemplateChoice, error) {
	if err := t.u.check(); err != nil {
		return storage.TemplateChoice{}, err
	}
	row, err := t.u.q.InsertContactDocumentTemplate(ctx, sqlcdb.InsertContactDocumentTemplateParams{ContactID: contact, Kind: kind, DocumentType: documentType,
		TemplateID: template, SetBy: t.u.actor(ctx), SetAt: t.u.store.clock.Now()})
	if err != nil {
		return storage.TemplateChoice{}, mapError(err)
	}
	c := row.ContactID
	out := storage.TemplateChoice{ID: row.ID, ContactID: &c, Kind: row.Kind, DocumentType: row.DocumentType, TemplateID: row.TemplateID, SetBy: row.SetBy, SetAt: row.SetAt}
	if err := t.u.event(ctx, "contact.document_template_set", "contact", contact, nil, map[string][2]any{
		"kind": {nil, kind}, "document_type": {nil, documentType}, "template_id": {nil, optID(template)}}); err != nil {
		return storage.TemplateChoice{}, err
	}
	return out, nil
}

func (t documentTemplates) ContactTemplates(ctx context.Context, contact uuid.UUID) ([]storage.TemplateChoice, error) {
	if err := t.u.check(); err != nil {
		return nil, err
	}
	rows, err := t.u.q.ContactDocumentTemplates(ctx, contact)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.TemplateChoice, 0, len(rows))
	for _, r := range rows {
		c := r.ContactID
		out = append(out, storage.TemplateChoice{ID: r.ID, ContactID: &c, Kind: r.Kind, DocumentType: r.DocumentType, TemplateID: r.TemplateID, SetBy: r.SetBy, SetAt: r.SetAt})
	}
	return out, nil
}
