package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// documentSends keeps the quotes and invoices sent by email (FR-AR-06): never changed (FY195), and only of a sent
// quote's or issued invoice's current version (FY196).
type documentSends struct{ u *unitOfWork }

func (u *unitOfWork) DocumentSends() storage.DocumentSends { return documentSends{u} }

func (d documentSends) Record(ctx context.Context, n storage.NewDocumentSend) (storage.DocumentSend, error) {
	if err := d.u.check(); err != nil {
		return storage.DocumentSend{}, err
	}
	actor := d.u.actor(ctx)
	row, err := d.u.q.InsertDocumentSend(ctx, sqlcdb.InsertDocumentSendParams{DocumentID: n.DocumentID, DocumentVersion: int32(n.DocumentVersion), // #nosec G115 -- a version is small
		MessageID: n.MessageID, EvidenceID: n.EvidenceID, Recipient: n.Recipient, TemplateSource: n.TemplateSource, TemplateID: n.TemplateID,
		TemplateVersion: int32(n.TemplateVersion), TemplateHash: n.TemplateHash, PdfSha256: n.PDFSHA256, SentBy: actor}) // #nosec G115 -- a version is small
	if err != nil {
		return storage.DocumentSend{}, mapError(err)
	}
	out := storage.DocumentSend{ID: row.ID, NewDocumentSend: n, SentBy: actor, SentAt: row.SentAt, MessageStatus: storage.EmailQueued}
	template := any(nil)
	if n.TemplateID != nil {
		template = n.TemplateID.String()
	}
	changes := map[string][2]any{"document_id": {nil, n.DocumentID.String()}, "document_version": {nil, n.DocumentVersion},
		"message_id": {nil, n.MessageID.String()}, "evidence_id": {nil, n.EvidenceID.String()}, "template_source": {nil, n.TemplateSource},
		"template_id": {nil, template}, "template_version": {nil, n.TemplateVersion}, "pdf_sha256": {nil, n.PDFSHA256}}
	if err := d.u.event(ctx, "document_send.recorded", "document_send", row.ID, nil, changes); err != nil {
		return storage.DocumentSend{}, err
	}
	return out, nil
}

func (d documentSends) of(ctx context.Context, ids []uuid.UUID) ([]storage.DocumentSend, error) {
	if err := d.u.check(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.u.q.DocumentSendsOf(ctx, ids)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.DocumentSend, len(rows))
	for i, r := range rows {
		out[i] = storage.DocumentSend{ID: r.ID, NewDocumentSend: storage.NewDocumentSend{DocumentID: r.DocumentID, DocumentVersion: int(r.DocumentVersion),
			MessageID: r.MessageID, EvidenceID: r.EvidenceID, Recipient: r.Recipient, TemplateSource: r.TemplateSource, TemplateID: r.TemplateID,
			TemplateVersion: int(r.TemplateVersion), TemplateHash: r.TemplateHash, PDFSHA256: r.PdfSha256},
			SentBy: r.SentBy, SentAt: r.SentAt, MessageStatus: r.MessageStatus, LastEvent: r.LastEvent}
	}
	return out, nil
}

func (d documentSends) OfDocument(ctx context.Context, documentID uuid.UUID) ([]storage.DocumentSend, error) {
	return d.of(ctx, []uuid.UUID{documentID})
}

func (d documentSends) Latest(ctx context.Context, documentIDs []uuid.UUID) (map[uuid.UUID]storage.DocumentSend, error) {
	all, err := d.of(ctx, documentIDs)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]storage.DocumentSend{}
	for _, s := range all { // newest first within each document
		if _, ok := out[s.DocumentID]; !ok {
			out[s.DocumentID] = s
		}
	}
	return out, nil
}
