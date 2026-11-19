package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) DocumentCaptures() storage.DocumentCaptures { return docCaptures{u} }

type docCaptures struct{ u *unitOfWork }

func toDocCapture(r sqlcdb.GetDocumentCaptureRow) storage.DocumentCapture {
	return storage.DocumentCapture{ID: r.ID, Source: r.Source, FileName: r.FileName, MediaType: r.MediaType, Size: r.SizeBytes, SHA256: r.Sha256, BlobKey: r.BlobKey,
		MessageID: r.MessageID, InboundFileID: r.InboundFileID, OwnerUserID: r.OwnerUserID, CapturedBy: r.CapturedBy, NRICFlagged: r.NricFlagged, StatusAt: r.StatusAt, Status: r.Status, Kind: str(r.Kind), Reading: r.Reading,
		ReadingModel: str(r.ReadingModel), ReadingPrompt: str(r.ReadingPrompt), Readings: int(r.Readings), ReadAt: r.ReadAt, Checks: r.Checks, DuplicateOf: r.DuplicateOf,
		DraftKind: str(r.DraftKind), DraftID: r.DraftID, DraftedBy: r.DraftedBy, DraftedAt: r.DraftedAt, DismissedBy: r.DismissedBy, DismissedAt: r.DismissedAt,
		DismissReason: str(r.DismissReason), LastError: str(r.LastError), CreatedAt: r.CreatedAt, Version: int(r.Version)}
}

func (r docCaptures) Capture(ctx context.Context, n storage.NewDocumentCapture) (storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, err
	}
	id, err := r.u.q.InsertDocumentCapture(ctx, sqlcdb.InsertDocumentCaptureParams{Source: n.Source, FileName: n.FileName, MediaType: n.MediaType, SizeBytes: n.Size, Sha256: n.SHA256,
		BlobKey: n.BlobKey, MessageID: n.MessageID, InboundFileID: n.InboundFileID, OwnerUserID: n.OwnerUserID, CapturedBy: n.CapturedBy, Status: n.Status, DuplicateOf: n.DuplicateOf,
		CreatedAt: n.At, NricFlagged: n.NRICFlagged})
	if err != nil {
		return storage.DocumentCapture{}, mapError(err)
	}
	out, err := r.Get(ctx, id)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "document_capture.captured", "document_capture", id, &out.Version, map[string][2]any{"source": {nil, n.Source}, "file_name": {nil, n.FileName},
		"sha256": {nil, n.SHA256}, "status": {nil, n.Status}})
}

func (r docCaptures) Get(ctx context.Context, id uuid.UUID) (storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, err
	}
	row, err := r.u.q.GetDocumentCapture(ctx, id)
	if err != nil {
		return storage.DocumentCapture{}, mapError(err)
	}
	return toDocCapture(row), nil
}

func (r docCaptures) Lock(ctx context.Context, id uuid.UUID) (storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, err
	}
	row, err := r.u.q.LockDocumentCapture(ctx, id)
	if err != nil {
		return storage.DocumentCapture{}, mapError(err)
	}
	return toDocCapture(sqlcdb.GetDocumentCaptureRow(row)), nil
}

func (r docCaptures) List(ctx context.Context, viewer uuid.UUID, status string, before uuid.UUID, limit int) ([]storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListDocumentCaptures(ctx, sqlcdb.ListDocumentCapturesParams{Status: status, Viewer: &viewer, Before: before, Max: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.DocumentCapture, 0, len(rows))
	for _, x := range rows {
		out = append(out, toDocCapture(sqlcdb.GetDocumentCaptureRow(x)))
	}
	return out, nil
}

func (r docCaptures) OfInboundFile(ctx context.Context, file uuid.UUID) (storage.DocumentCapture, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, false, err
	}
	ids, err := r.u.q.CaptureOfInboundFile(ctx, &file)
	if err != nil || len(ids) == 0 {
		return storage.DocumentCapture{}, false, mapError(err)
	}
	c, err := r.Get(ctx, ids[0])
	return c, err == nil, err
}

func (r docCaptures) OfHash(ctx context.Context, viewer uuid.UUID, sha256 string) (storage.DocumentCapture, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, false, err
	}
	ids, err := r.u.q.CaptureOfHash(ctx, sqlcdb.CaptureOfHashParams{Sha256: sha256, Viewer: &viewer})
	if err != nil || len(ids) == 0 {
		return storage.DocumentCapture{}, false, mapError(err)
	}
	c, err := r.Get(ctx, ids[0])
	return c, err == nil, err
}

// moved is the capture after a versioned update: ports.ErrConflict when the row moved since it was read.
func (r docCaptures) moved(ctx context.Context, id uuid.UUID, err error) (storage.DocumentCapture, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := r.u.q.GetDocumentCapture(ctx, id); gerr != nil {
			return storage.DocumentCapture{}, mapError(gerr)
		}
		return storage.DocumentCapture{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("the captured document %s moved since it was read", id)}
	}
	if err != nil {
		return storage.DocumentCapture{}, mapError(err)
	}
	return r.Get(ctx, id)
}

func (r docCaptures) event(ctx context.Context, action string, c storage.DocumentCapture, changes map[string][2]any) (storage.DocumentCapture, error) {
	return c, r.u.event(ctx, action, "document_capture", c.ID, &c.Version, changes)
}

func (r docCaptures) Reading(ctx context.Context, id uuid.UUID, version int, d storage.CapturedReading) (storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, err
	}
	kind, model, prompt := optional(d.Kind), optional(d.Model), optional(d.Prompt)
	_, err := r.u.q.SetDocumentCaptureReading(ctx, sqlcdb.SetDocumentCaptureReadingParams{ID: id, Version: int32(version), Status: storage.CaptureRead, Kind: kind, Reading: d.Reading,
		ReadingModel: model, ReadingPrompt: prompt, ReadAt: &d.At, Checks: d.Checks})
	c, err := r.moved(ctx, id, err)
	if err != nil {
		return c, err
	}
	return r.event(ctx, "document_capture.read", c, map[string][2]any{"status": {storage.CaptureReading, storage.CaptureRead}, "kind": {nil, d.Kind}, "model": {nil, d.Model}})
}

func (r docCaptures) Status(ctx context.Context, id uuid.UUID, version int, status, lastError string) (storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, err
	}
	now := r.u.store.clock.Now()
	_, err := r.u.q.SetDocumentCaptureStatus(ctx, sqlcdb.SetDocumentCaptureStatusParams{ID: id, Version: int32(version), Status: status, LastError: optional(clipTo(lastError, 500)), StatusAt: &now})
	c, err := r.moved(ctx, id, err)
	if err != nil {
		return c, err
	}
	return r.event(ctx, "document_capture."+status, c, map[string][2]any{"status": {nil, status}})
}

func (r docCaptures) Drafted(ctx context.Context, id uuid.UUID, version int, kind string, draft, by uuid.UUID, at time.Time) (storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, err
	}
	_, err := r.u.q.DraftDocumentCapture(ctx, sqlcdb.DraftDocumentCaptureParams{ID: id, Version: int32(version), DraftKind: &kind, DraftID: &draft, DraftedBy: &by, DraftedAt: &at})
	c, err := r.moved(ctx, id, err)
	if err != nil {
		return c, err
	}
	return r.event(ctx, "document_capture.drafted", c, map[string][2]any{"status": {storage.CaptureRead, storage.CaptureDrafted}, "draft_kind": {nil, kind}, "draft_id": {nil, draft.String()}})
}

func (r docCaptures) Dismiss(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, reason string, at time.Time) (storage.DocumentCapture, error) {
	if err := r.u.check(); err != nil {
		return storage.DocumentCapture{}, err
	}
	_, err := r.u.q.DismissDocumentCapture(ctx, sqlcdb.DismissDocumentCaptureParams{ID: id, Version: int32(version), DismissedBy: &by, DismissedAt: &at, DismissReason: optional(reason)})
	c, err := r.moved(ctx, id, err)
	if err != nil {
		return c, err
	}
	return r.event(ctx, "document_capture.dismissed", c, map[string][2]any{"status": {nil, storage.CaptureDismissed}, "reason": {nil, reason}})
}
