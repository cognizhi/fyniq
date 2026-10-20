package postgres

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type closes struct{ u *unitOfWork }

func closeCheckOf(id, period uuid.UUID, by string, at time.Time, rev int64, result []byte, clean bool) storage.CloseCheck {
	return storage.CloseCheck{ID: id, CheckedBy: by, CheckedAt: at,
		NewCloseCheck: storage.NewCloseCheck{PeriodID: period, LedgerRevision: rev, Result: result, Clean: clean}}
}

func periodCloseOf(id, period, check uuid.UUID, by string, at time.Time, statement string, rev int64, snapshot string, hash []byte) storage.PeriodClose {
	return storage.PeriodClose{ID: id, AttestedBy: by, AttestedAt: at,
		NewClose: storage.NewClose{PeriodID: period, CheckID: check, Statement: statement, ClosedRevision: rev, Snapshot: []byte(snapshot), SnapshotHash: hash}}
}

func (r closes) RecordCheck(ctx context.Context, c storage.NewCloseCheck) (storage.CloseCheck, error) {
	if err := r.u.check(); err != nil {
		return storage.CloseCheck{}, err
	}
	row, err := r.u.q.InsertCloseCheck(ctx, sqlcdb.InsertCloseCheckParams{PeriodID: c.PeriodID, CheckedBy: r.u.actor(ctx), CheckedAt: r.u.store.clock.Now(),
		LedgerRevision: c.LedgerRevision, Result: c.Result, Clean: c.Clean})
	if err != nil {
		return storage.CloseCheck{}, mapError(err)
	}
	out := closeCheckOf(row.ID, row.PeriodID, row.CheckedBy, row.CheckedAt, row.LedgerRevision, row.Result, row.Clean)
	if err := r.u.event(ctx, "period.close_checked", "period_close_check", out.ID, nil, map[string][2]any{
		"period_id": {nil, out.PeriodID}, "ledger_revision": {nil, out.LedgerRevision}, "clean": {nil, out.Clean}}); err != nil {
		return storage.CloseCheck{}, err
	}
	return out, nil
}

func (r closes) Check(ctx context.Context, id uuid.UUID) (storage.CloseCheck, error) {
	if err := r.u.check(); err != nil {
		return storage.CloseCheck{}, err
	}
	row, err := r.u.q.GetCloseCheck(ctx, id)
	if err != nil {
		return storage.CloseCheck{}, mapError(err)
	}
	return closeCheckOf(row.ID, row.PeriodID, row.CheckedBy, row.CheckedAt, row.LedgerRevision, row.Result, row.Clean), nil
}

func (r closes) LatestCheck(ctx context.Context, period uuid.UUID) (storage.CloseCheck, error) {
	if err := r.u.check(); err != nil {
		return storage.CloseCheck{}, err
	}
	row, err := r.u.q.LatestCloseCheck(ctx, period)
	if err != nil {
		return storage.CloseCheck{}, mapError(err)
	}
	return closeCheckOf(row.ID, row.PeriodID, row.CheckedBy, row.CheckedAt, row.LedgerRevision, row.Result, row.Clean), nil
}

func (r closes) Record(ctx context.Context, c storage.NewClose) (storage.PeriodClose, error) {
	if err := r.u.check(); err != nil {
		return storage.PeriodClose{}, err
	}
	row, err := r.u.q.InsertPeriodClose(ctx, sqlcdb.InsertPeriodCloseParams{PeriodID: c.PeriodID, CheckID: c.CheckID, AttestedBy: r.u.actor(ctx),
		AttestedAt: r.u.store.clock.Now(), Statement: c.Statement, ClosedRevision: c.ClosedRevision, Snapshot: string(c.Snapshot), SnapshotHash: c.SnapshotHash})
	if err != nil {
		return storage.PeriodClose{}, mapError(err)
	}
	out := periodCloseOf(row.ID, row.PeriodID, row.CheckID, row.AttestedBy, row.AttestedAt, row.Statement, row.ClosedRevision, row.Snapshot, row.SnapshotHash)
	if err := r.u.event(ctx, "period.attested", "period_close", out.ID, nil, map[string][2]any{
		"period_id": {nil, out.PeriodID}, "check_id": {nil, out.CheckID}, "statement": {nil, out.Statement},
		"closed_revision": {nil, out.ClosedRevision}, "snapshot_sha256": {nil, hex.EncodeToString(out.SnapshotHash)}}); err != nil {
		return storage.PeriodClose{}, err
	}
	return out, nil
}

func (r closes) Latest(ctx context.Context, period uuid.UUID) (storage.PeriodClose, error) {
	if err := r.u.check(); err != nil {
		return storage.PeriodClose{}, err
	}
	row, err := r.u.q.LatestPeriodClose(ctx, period)
	if err != nil {
		return storage.PeriodClose{}, mapError(err)
	}
	return periodCloseOf(row.ID, row.PeriodID, row.CheckID, row.AttestedBy, row.AttestedAt, row.Statement, row.ClosedRevision, row.Snapshot, row.SnapshotHash), nil
}

func (r closes) ChangedSince(ctx context.Context, through time.Time, revision int64) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	changed, err := r.u.q.EntriesChangedSince(ctx, sqlcdb.EntriesChangedSinceParams{Through: dateOf(through), Revision: revision})
	return changed, mapError(err)
}
