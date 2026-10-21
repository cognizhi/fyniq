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

func (r closes) RecordReopen(ctx context.Context, n storage.NewReopen) (storage.PeriodReopen, error) {
	if err := r.u.check(); err != nil {
		return storage.PeriodReopen{}, err
	}
	row, err := r.u.q.InsertPeriodReopen(ctx, sqlcdb.InsertPeriodReopenParams{PeriodID: n.PeriodID, CloseID: n.CloseID, ApprovalID: n.ApprovalID,
		Reason: n.Reason, ReopenedBy: r.u.actor(ctx), ReopenedAt: r.u.store.clock.Now(), ReopenedRevision: n.ReopenedRevision, BeforeHash: n.BeforeHash,
		AfterSnapshot: string(n.AfterSnapshot), AfterHash: n.AfterHash})
	if err != nil {
		return storage.PeriodReopen{}, mapError(err)
	}
	out := storage.PeriodReopen{NewReopen: n, ID: row.ID, ReopenedBy: row.ReopenedBy, ReopenedAt: row.ReopenedAt}
	if err := r.u.event(ctx, "period.reopen_recorded", "period_reopen", out.ID, nil, map[string][2]any{
		"period_id": {nil, n.PeriodID}, "close_id": {nil, n.CloseID}, "approval_id": {nil, n.ApprovalID}, "reason": {nil, n.Reason},
		"reopened_revision": {nil, n.ReopenedRevision}, "before_sha256": {nil, hex.EncodeToString(n.BeforeHash)},
		"after_sha256": {nil, hex.EncodeToString(n.AfterHash)}}); err != nil {
		return storage.PeriodReopen{}, err
	}
	return out, nil
}

func (r closes) LatestReopen(ctx context.Context, period uuid.UUID) (storage.PeriodReopen, error) {
	if err := r.u.check(); err != nil {
		return storage.PeriodReopen{}, err
	}
	row, err := r.u.q.LatestPeriodReopen(ctx, period)
	if err != nil {
		return storage.PeriodReopen{}, mapError(err)
	}
	return storage.PeriodReopen{ID: row.ID, ReopenedBy: row.ReopenedBy, ReopenedAt: row.ReopenedAt,
		NewReopen: storage.NewReopen{PeriodID: row.PeriodID, CloseID: row.CloseID, ApprovalID: row.ApprovalID, Reason: row.Reason,
			ReopenedRevision: row.ReopenedRevision, BeforeHash: row.BeforeHash, AfterHash: row.AfterHash}}, nil
}

func varianceExplanationOf(id, period, account uuid.UUID, current, previous, explanation, by string, at time.Time) storage.VarianceExplanation {
	return storage.VarianceExplanation{ID: id, WrittenBy: by, WrittenAt: at, NewVarianceExplanation: storage.NewVarianceExplanation{PeriodID: period,
		AccountID: account, Current: storage.Decimal(current), Previous: storage.Decimal(previous), Explanation: explanation}}
}

func (r closes) ExplainVariance(ctx context.Context, e storage.NewVarianceExplanation) (storage.VarianceExplanation, error) {
	if err := r.u.check(); err != nil {
		return storage.VarianceExplanation{}, err
	}
	row, err := r.u.q.InsertVarianceExplanation(ctx, sqlcdb.InsertVarianceExplanationParams{PeriodID: e.PeriodID, AccountID: e.AccountID,
		CurrentAmount: string(e.Current), PreviousAmount: string(e.Previous), Explanation: e.Explanation, WrittenBy: r.u.actor(ctx),
		WrittenAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.VarianceExplanation{}, mapError(err)
	}
	out := varianceExplanationOf(row.ID, row.PeriodID, row.AccountID, row.CurrentAmount, row.PreviousAmount, row.Explanation, row.WrittenBy, row.WrittenAt)
	if err := r.u.event(ctx, "period.variance_explained", "period_variance_explanation", out.ID, nil, map[string][2]any{
		"period_id": {nil, out.PeriodID}, "account_id": {nil, out.AccountID}, "current_amount": {nil, string(e.Current)},
		"previous_amount": {nil, string(e.Previous)}, "explanation": {nil, out.Explanation}}); err != nil {
		return storage.VarianceExplanation{}, err
	}
	return out, nil
}

func (r closes) VarianceExplanations(ctx context.Context, period uuid.UUID) ([]storage.VarianceExplanation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.LatestVarianceExplanations(ctx, period)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.VarianceExplanation, 0, len(rows))
	for _, x := range rows {
		out = append(out, varianceExplanationOf(x.ID, x.PeriodID, x.AccountID, x.CurrentAmount, x.PreviousAmount, x.Explanation, x.WrittenBy, x.WrittenAt))
	}
	return out, nil
}
