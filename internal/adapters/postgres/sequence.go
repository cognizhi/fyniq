package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Sequences() storage.Sequences { return sequences{u} }

type sequences struct{ u *unitOfWork }

func sequenceOf(r sqlcdb.ListSequencesRow) storage.Sequence {
	return storage.Sequence{ID: r.ID, DocumentType: storage.DocumentType(r.DocumentType), Prefix: r.Prefix, NextNumber: r.NextNumber,
		Digits: int(r.Digits), GapFree: r.GapFree, Version: int(r.Version)}
}

func defaults(t storage.DocumentType) (storage.SequenceSettings, error) {
	d, ok := storage.DefaultSequences[t]
	if !ok {
		return storage.SequenceSettings{}, &storage.Error{Code: storage.CodeSequence, Message: fmt.Sprintf("%s documents are not numbered", t)}
	}
	return d, nil
}

// digits is a sequence's padding as the column holds it, refused outside 1 to 12.
func digits(n int) (int16, error) {
	if n < 1 || n > 12 {
		return 0, &storage.Error{Code: storage.CodeSequence, Message: fmt.Sprintf("%d is not 1 to 12 digits", n)}
	}
	return int16(n), nil // 1 to 12, checked above
}

func (r sequences) Get(ctx context.Context, t storage.DocumentType) (storage.Sequence, error) {
	if err := r.u.check(); err != nil {
		return storage.Sequence{}, err
	}
	row, err := r.u.q.FindSequence(ctx, string(t))
	if errors.Is(err, pgx.ErrNoRows) {
		d, err := defaults(t)
		return storage.Sequence{DocumentType: t, Prefix: d.Prefix, NextNumber: d.NextNumber, Digits: d.Digits, GapFree: d.GapFree}, err
	}
	if err != nil {
		return storage.Sequence{}, mapError(err)
	}
	return sequenceOf(sqlcdb.ListSequencesRow(row)), nil
}

// setUp inserts a document type's sequence with s, and audits it; ok is false when another unit of work
// set it up first.
func (r sequences) setUp(ctx context.Context, t storage.DocumentType, s storage.SequenceSettings) (storage.Sequence, bool, error) {
	d, err := digits(s.Digits)
	if err != nil {
		return storage.Sequence{}, false, err
	}
	row, err := r.u.q.InsertSequence(ctx, sqlcdb.InsertSequenceParams{DocumentType: string(t), Prefix: s.Prefix, NextNumber: s.NextNumber,
		Digits: d, GapFree: s.GapFree})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Sequence{}, false, nil
	}
	if err != nil {
		return storage.Sequence{}, false, mapError(err)
	}
	seq := sequenceOf(sqlcdb.ListSequencesRow(row))
	if err := r.u.event(ctx, "document_sequence.created", "document_sequence", seq.ID, &seq.Version, map[string][2]any{
		"document_type": {nil, string(t)}, "prefix": {nil, seq.Prefix}, "next_number": {nil, seq.NextNumber},
		"digits": {nil, seq.Digits}, "gap_free": {nil, seq.GapFree}}); err != nil {
		return storage.Sequence{}, false, err
	}
	return seq, true, nil
}

func (r sequences) Update(ctx context.Context, t storage.DocumentType, version int, s storage.SequenceSettings) (storage.Sequence, error) {
	if err := r.u.check(); err != nil {
		return storage.Sequence{}, err
	}
	if _, err := defaults(t); err != nil {
		return storage.Sequence{}, err
	}
	d, err := digits(s.Digits)
	if err != nil {
		return storage.Sequence{}, err
	}
	cur, err := r.u.q.LockSequence(ctx, string(t))
	if errors.Is(err, pgx.ErrNoRows) {
		if version != 0 {
			return storage.Sequence{}, &storage.Error{Code: storage.CodeVersion,
				Message: fmt.Sprintf("the %s sequence is not set up, so it has no version %d", t, version)}
		}
		seq, ok, err := r.setUp(ctx, t, s)
		if err != nil || ok {
			return seq, err
		}
		return storage.Sequence{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("the %s sequence was set up meanwhile", t)}
	}
	if err != nil {
		return storage.Sequence{}, mapError(err)
	}
	before := sequenceOf(sqlcdb.ListSequencesRow(cur))
	if before.Version != version {
		return storage.Sequence{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the %s sequence changed since it was read: it is at version %d, not %d", t, before.Version, version)}
	}
	row, err := r.u.q.UpdateSequence(ctx, sqlcdb.UpdateSequenceParams{DocumentType: string(t), Prefix: s.Prefix, NextNumber: s.NextNumber,
		Digits: d, GapFree: s.GapFree})
	if err != nil {
		return storage.Sequence{}, mapError(err)
	}
	after := sequenceOf(sqlcdb.ListSequencesRow(row))
	changes := map[string][2]any{"version": {before.Version, after.Version}}
	if before.Prefix != after.Prefix {
		changes["prefix"] = [2]any{before.Prefix, after.Prefix}
	}
	if before.NextNumber != after.NextNumber {
		changes["next_number"] = [2]any{before.NextNumber, after.NextNumber}
	}
	if before.Digits != after.Digits {
		changes["digits"] = [2]any{before.Digits, after.Digits}
	}
	if before.GapFree != after.GapFree {
		changes["gap_free"] = [2]any{before.GapFree, after.GapFree}
	}
	if err := r.u.event(ctx, "document_sequence.updated", "document_sequence", after.ID, &after.Version, changes); err != nil {
		return storage.Sequence{}, err
	}
	return after, nil
}

func (r sequences) Number(ctx context.Context, id uuid.UUID, t storage.DocumentType) (string, error) {
	if err := r.u.check(); err != nil {
		return "", err
	}
	if n, err := r.u.q.GetDocumentNumber(ctx, id); err == nil {
		return n, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", mapError(err)
	}
	cur, err := r.u.q.LockSequence(ctx, string(t))
	if errors.Is(err, pgx.ErrNoRows) {
		d, err := defaults(t)
		if err != nil {
			return "", err
		}
		if _, _, err := r.setUp(ctx, t, d); err != nil {
			return "", err
		}
		cur, err = r.u.q.LockSequence(ctx, string(t))
		if err != nil {
			return "", mapError(err)
		}
	} else if err != nil {
		return "", mapError(err)
	}
	seq := sequenceOf(sqlcdb.ListSequencesRow(cur))
	number := storage.FormatNumber(seq.Prefix, seq.NextNumber, seq.Digits)
	if err := r.u.q.InsertDocumentNumber(ctx, sqlcdb.InsertDocumentNumberParams{ID: id, DocumentType: string(t), Number: number}); err != nil {
		return "", mapError(err)
	}
	if err := r.u.q.AdvanceSequence(ctx, seq.ID); err != nil {
		return "", mapError(err)
	}
	if err := r.u.event(ctx, "document.numbered", "document", id, nil, map[string][2]any{"number": {nil, number}}); err != nil {
		return "", err
	}
	return number, nil
}
