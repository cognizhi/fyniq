package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The holds (AUD-11; 001300): placed and lifted, each audited in the same transaction, and the three reads the purges and the
// storage reconcile make of what the holds cover.

func (r retention) PlaceHold(ctx context.Context, h storage.NewHold) (storage.Hold, error) {
	if err := r.u.check(); err != nil {
		return storage.Hold{}, err
	}
	p := sqlcdb.InsertRetentionHoldParams{Kind: h.Kind, Reason: h.Reason, PlacedBy: h.PlacedBy, PlacedAt: h.PlacedAt}
	switch h.Kind {
	case storage.HoldRecord:
		p.DocumentID = &h.DocumentID
	case storage.HoldContact:
		p.ContactID = &h.ContactID
	case storage.HoldFinancialYear:
		if h.Year != nil {
			n := int32(min(max(h.Year.Number, 1), 1<<30)) // #nosec G115 -- bounded
			p.YearNumber = &n
			p.YearStartsOn = pgtype.Date{Time: h.Year.StartsOn, Valid: true}
			p.YearEndsOn = pgtype.Date{Time: h.Year.EndsOn, Valid: true}
		}
	}
	row, err := r.u.q.InsertRetentionHold(ctx, p)
	if err != nil {
		return storage.Hold{}, mapError(err)
	}
	out := holdOf(sqlcdb.GetRetentionHoldRow(row))
	changes := map[string][2]any{"kind": {nil, out.Kind}, "reason": {nil, out.Reason}, "placed_by": {nil, out.PlacedBy}}
	switch {
	case out.DocumentID != nil:
		changes["document_id"] = [2]any{nil, out.DocumentID.String()}
	case out.ContactID != nil:
		changes["contact_id"] = [2]any{nil, out.ContactID.String()}
	case out.Year != nil:
		changes["year_number"] = [2]any{nil, out.Year.Number}
		changes["year_starts_on"] = [2]any{nil, day(out.Year.StartsOn)}
		changes["year_ends_on"] = [2]any{nil, day(out.Year.EndsOn)}
	}
	return out, r.u.event(ctx, "retention_hold.placed", "retention_hold", out.ID, nil, changes)
}

func (r retention) LiftHold(ctx context.Context, id uuid.UUID, by, reason string, at time.Time) (storage.Hold, error) {
	if err := r.u.check(); err != nil {
		return storage.Hold{}, err
	}
	row, err := r.u.q.LiftRetentionHold(ctx, sqlcdb.LiftRetentionHoldParams{ID: id, LiftedBy: &by, LiftedAt: &at, LiftReason: &reason})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := r.u.q.GetRetentionHold(ctx, id); gerr == nil {
			return storage.Hold{}, &storage.Error{Code: storage.CodeHold, Message: "The hold was lifted already",
				Hint: "A lifted hold never stands again: place a new one to hold again (AUD-11)"}
		}
		return storage.Hold{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.Hold{}, mapError(err)
	}
	out := holdOf(sqlcdb.GetRetentionHoldRow(row))
	return out, r.u.event(ctx, "retention_hold.lifted", "retention_hold", out.ID, nil,
		map[string][2]any{"lifted_by": {nil, by}, "lifted_at": {nil, at.UTC().Format(time.RFC3339)}, "lift_reason": {nil, reason}})
}

func (r retention) Holds(ctx context.Context, inForceOnly bool) ([]storage.Hold, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListRetentionHolds(ctx, inForceOnly)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Hold, len(rows))
	for i, x := range rows {
		out[i] = holdOf(sqlcdb.GetRetentionHoldRow(x))
	}
	return out, nil
}

func (r retention) HeldLedgerDates(ctx context.Context) ([]time.Time, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	days, err := r.u.q.HeldLedgerDates(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	for i, d := range days {
		days[i] = dateOf(d)
	}
	return days, nil
}

func (r retention) HeldEvents(ctx context.Context, first, last int64) ([]int64, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	seqs, err := r.u.q.HeldAuditEvents(ctx, sqlcdb.HeldAuditEventsParams{First: first, Last: last})
	return seqs, mapError(err)
}

func (r retention) HeldFiles(ctx context.Context) ([]storage.HeldFile, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.HeldEvidenceFiles(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.HeldFile, len(rows))
	for i, x := range rows {
		out[i] = storage.HeldFile{EvidenceID: x.EvidenceID, LockedKey: x.LockedKey}
	}
	return out, nil
}

func (r retention) FreeFiles(ctx context.Context) ([]storage.HeldFile, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.FreeEvidenceFiles(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.HeldFile, len(rows))
	for i, x := range rows {
		out[i] = storage.HeldFile{EvidenceID: x.EvidenceID, LockedKey: x.LockedKey}
	}
	return out, nil
}

func holdOf(row sqlcdb.GetRetentionHoldRow) storage.Hold {
	h := storage.Hold{ID: row.ID, Kind: row.Kind, DocumentID: row.DocumentID, ContactID: row.ContactID, Reason: row.Reason, PlacedBy: row.PlacedBy,
		PlacedAt: row.PlacedAt, LiftedAt: row.LiftedAt, LiftedBy: str(row.LiftedBy), LiftReason: str(row.LiftReason)}
	if row.YearStartsOn.Valid && row.YearEndsOn.Valid && row.YearNumber != nil {
		h.Year = &storage.HoldYear{Number: int(*row.YearNumber), StartsOn: dateOf(row.YearStartsOn.Time), EndsOn: dateOf(row.YearEndsOn.Time)}
	}
	return h
}
