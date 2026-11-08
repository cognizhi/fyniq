package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// retention is the ledger's purge after retention (AUD-10; 000292).
type retention struct{ u *unitOfWork }

func (u *unitOfWork) Retention() storage.Retention { return retention{u} }

func (r retention) LedgerBoundaries(ctx context.Context) ([]storage.LedgerBoundary, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListLedgerBoundaries(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.LedgerBoundary, 0, len(rows))
	for _, b := range rows {
		x, err := ledgerBoundaryOf(sqlcdb.InsertLedgerBoundaryRow(b))
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func (r retention) RecordLedgerBoundary(ctx context.Context, b storage.NewLedgerBoundary) (storage.LedgerBoundary, error) {
	if err := r.u.check(); err != nil {
		return storage.LedgerBoundary{}, err
	}
	counts, err := json.Marshal(b.Counts)
	if err != nil {
		return storage.LedgerBoundary{}, err
	}
	row, err := r.u.q.InsertLedgerBoundary(ctx, sqlcdb.InsertLedgerBoundaryParams{Boundary: b.Boundary, Approval: b.Approval, Counts: counts,
		PurgedHash: b.PurgedHash, AnchorKey: b.AnchorKey, WrittenAt: b.WrittenAt})
	if err != nil {
		return storage.LedgerBoundary{}, mapError(err)
	}
	out, err := ledgerBoundaryOf(row)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "ledger.boundary_written", "ledger_boundary", out.ID, nil, map[string][2]any{"boundary": {nil, day(b.Boundary)},
		"approval": {nil, b.Approval}, "counts": {nil, b.Counts}, "anchor_key": {nil, b.AnchorKey}})
}

func (r retention) EarliestEntryFrom(ctx context.Context, from time.Time) (time.Time, bool, error) {
	if err := r.u.check(); err != nil {
		return time.Time{}, false, err
	}
	d, err := r.u.q.EarliestEntryFrom(ctx, from)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, mapError(err)
	}
	return d, true, nil
}

func (r retention) PurgeLines(ctx context.Context, boundary time.Time) ([]storage.PurgeLine, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.LedgerPurgeLines(ctx, boundary)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PurgeLine, len(rows))
	for i, l := range rows {
		var values []uuid.UUID
		if len(l.DimensionValueIds) > 0 {
			values = l.DimensionValueIds
		}
		out[i] = storage.PurgeLine{EntryID: l.EntryID, PostingDate: l.PostingDate, Source: storage.Source{Type: l.SourceType, ID: l.SourceID},
			ReversesEntryID: l.ReversesEntryID, Named: l.Named, AccountID: l.AccountID, Currency: l.Currency,
			Amount: storage.Decimal(l.Amount), BaseAmount: storage.Decimal(l.BaseAmount), PartyID: l.PartyID, InstrumentID: l.InstrumentID,
			DimensionValues: values}
	}
	return out, nil
}

func (r retention) ReversalLinks(ctx context.Context, boundary time.Time) ([]storage.EntryLink, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ReversalLinksAcross(ctx, boundary)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.EntryLink, len(rows))
	for i, l := range rows {
		out[i] = storage.EntryLink{ID: l.ID, PostingDate: l.PostingDate, ReversesEntryID: l.ReversesEntryID}
	}
	return out, nil
}

func (r retention) PurgeLedger(ctx context.Context, boundary time.Time, entries []uuid.UUID) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.PurgeLedger(ctx, sqlcdb.PurgeLedgerParams{Boundary: boundary, Entries: uuids(entries)})
	return n, mapError(err)
}

func ledgerBoundaryOf(row sqlcdb.InsertLedgerBoundaryRow) (storage.LedgerBoundary, error) {
	out := storage.LedgerBoundary{ID: row.ID, NewLedgerBoundary: storage.NewLedgerBoundary{Boundary: row.Boundary, Approval: row.Approval,
		PurgedHash: row.PurgedHash, AnchorKey: row.AnchorKey, WrittenAt: row.WrittenAt}}
	if row.CarriedRevision != nil {
		out.CarriedRevision = *row.CarriedRevision
	}
	return out, json.Unmarshal(row.Counts, &out.Counts)
}
