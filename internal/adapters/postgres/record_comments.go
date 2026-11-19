package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) RecordComments() storage.RecordComments { return recordComments{u} }

type recordComments struct{ u *unitOfWork }

func commentOf(id uuid.UUID, recordType string, recordID, author uuid.UUID, body string, mentions []uuid.UUID, at storage.RecordComment) storage.RecordComment {
	at.ID, at.RecordType, at.RecordID, at.Author, at.Body, at.Mentions = id, recordType, recordID, author, body, mentions
	return at
}

func (r recordComments) Add(ctx context.Context, n storage.NewRecordComment) (storage.RecordComment, error) {
	if err := r.u.check(); err != nil {
		return storage.RecordComment{}, err
	}
	if n.Mentions == nil {
		n.Mentions = []uuid.UUID{}
	}
	row, err := r.u.q.InsertRecordComment(ctx, sqlcdb.InsertRecordCommentParams{RecordType: n.RecordType, RecordID: n.RecordID, Author: n.Author,
		Body: n.Body, Mentions: n.Mentions, CreatedAt: n.CreatedAt})
	if err != nil {
		return storage.RecordComment{}, mapError(err)
	}
	c := commentOf(row.ID, row.RecordType, row.RecordID, row.Author, row.Body, row.Mentions, storage.RecordComment{CreatedAt: row.CreatedAt})
	ids := make([]string, len(c.Mentions))
	for i, m := range c.Mentions {
		ids[i] = m.String()
	}
	if err := r.u.event(ctx, "record_comment.added", "record_comment", c.ID, nil, map[string][2]any{
		"record_type": {nil, c.RecordType}, "record_id": {nil, c.RecordID}, "author": {nil, c.Author}, "mentions": {nil, ids}}); err != nil {
		return storage.RecordComment{}, err
	}
	return c, nil
}

func (r recordComments) Thread(ctx context.Context, recordType string, recordID uuid.UUID) ([]storage.RecordComment, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.RecordThread(ctx, sqlcdb.RecordThreadParams{RecordType: recordType, RecordID: recordID, MaxRows: storage.MaxThread})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.RecordComment, len(rows))
	for i, x := range rows {
		out[i] = commentOf(x.ID, x.RecordType, x.RecordID, x.Author, x.Body, x.Mentions, storage.RecordComment{CreatedAt: x.CreatedAt})
	}
	return out, nil
}
