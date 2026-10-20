package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) ReviewLog() storage.ReviewLog { return reviewLog{u} }

type reviewLog struct{ u *unitOfWork }

func (r reviewLog) List(ctx context.Context) ([]storage.ReviewLogEntry, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListReviewLog(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ReviewLogEntry, len(rows))
	for i, x := range rows {
		out[i] = storage.ReviewLogEntry{ID: x.ID, Kind: x.Kind, ObjectType: x.ObjectType, ObjectKey: x.ObjectKey, Summary: x.Summary,
			Actor: x.Actor, ApprovalID: x.ApprovalID, LoggedAt: x.LoggedAt, ReviewedBy: x.ReviewedBy, ReviewedAt: x.ReviewedAt, Note: str(x.Note)}
	}
	return out, nil
}

func (r reviewLog) Log(ctx context.Context, n storage.NewReviewLogEntry) (uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return uuid.Nil, err
	}
	id, err := r.u.q.InsertReviewLogEntry(ctx, sqlcdb.InsertReviewLogEntryParams{Kind: n.Kind, ObjectType: n.ObjectType, ObjectKey: n.ObjectKey,
		Summary: n.Summary, Actor: n.Actor, ApprovalID: n.ApprovalID, LoggedAt: n.LoggedAt})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	return id, r.u.event(ctx, "review_log.logged", "review_log_entry", id, nil, map[string][2]any{
		"kind": {nil, n.Kind}, "object_type": {nil, n.ObjectType}, "object_key": {nil, n.ObjectKey}, "summary": {nil, n.Summary},
		"actor": {nil, n.Actor}, "approval_id": {nil, n.ApprovalID}, "logged_at": {nil, n.LoggedAt}})
}

func (r reviewLog) Review(ctx context.Context, entryID, by uuid.UUID, at time.Time, note string) error {
	if err := r.u.check(); err != nil {
		return err
	}
	id, err := r.u.q.InsertReviewLogReview(ctx, sqlcdb.InsertReviewLogReviewParams{EntryID: entryID, ReviewedBy: by, ReviewedAt: at, Note: opt(note)})
	if err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "review_log.reviewed", "review_log_review", id, nil, map[string][2]any{
		"entry_id": {nil, entryID}, "reviewed_by": {nil, by}, "reviewed_at": {nil, at}, "note": {nil, nilIfEmpty(note)}})
}
