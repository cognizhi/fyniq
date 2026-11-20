package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- soft locks (FR-GL-09; p2-mobile-approvals-automation design D6)

func (u *unitOfWork) SoftLocks() storage.SoftLocks { return softLocks{u} }

type softLocks struct{ u *unitOfWork }

func (s softLocks) OfDate(ctx context.Context, date time.Time) (storage.Period, bool, error) {
	if err := s.u.check(); err != nil {
		return storage.Period{}, false, err
	}
	row, err := s.u.q.PeriodOfDate(ctx, dateOf(date))
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Period{}, false, nil
	}
	if err != nil {
		return storage.Period{}, false, mapError(err)
	}
	return toPeriod(row.ID, row.StartsOn, row.EndsOn, row.Status, row.ClosedRevision, row.Version), true, nil
}

// move changes a period's status under ENG-47's lock order (the ledger lock, then the period row) and audits it.
func (s softLocks) move(ctx context.Context, id uuid.UUID, from, to storage.PeriodStatus, action string) (storage.Period, error) {
	if err := s.u.check(); err != nil {
		return storage.Period{}, err
	}
	if _, err := s.u.q.LockLedger(ctx); err != nil {
		return storage.Period{}, mapError(err)
	}
	cur, err := s.u.q.LockPeriod(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Period{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.Period{}, mapError(err)
	}
	if cur.Status != string(from) {
		return storage.Period{}, &storage.Error{Code: storage.CodePeriodState,
			Message: fmt.Sprintf("the period %s to %s is %s, not %s", day(cur.StartsOn), day(cur.EndsOn), cur.Status, from),
			Hint:    "Read the period again to see its status; a closed period is reopened through its own request (FR-GL-08)"}
	}
	row, err := s.u.q.SetPeriodStatus(ctx, sqlcdb.SetPeriodStatusParams{ID: id, Status: string(to)})
	if err != nil {
		return storage.Period{}, mapError(err)
	}
	v := int(row.Version)
	if err := s.u.event(ctx, action, "period", row.ID, &v, map[string][2]any{"status": {cur.Status, row.Status}}); err != nil {
		return storage.Period{}, err
	}
	return toPeriod(row.ID, row.StartsOn, row.EndsOn, row.Status, row.ClosedRevision, row.Version), nil
}

func (s softLocks) SoftLock(ctx context.Context, id uuid.UUID) (storage.Period, error) {
	return s.move(ctx, id, storage.PeriodOpen, storage.PeriodSoftLocked, "period.soft_locked")
}

func (s softLocks) Unlock(ctx context.Context, id uuid.UUID) (storage.Period, error) {
	return s.move(ctx, id, storage.PeriodSoftLocked, storage.PeriodOpen, "period.unlocked")
}

func (s softLocks) LockedModules(ctx context.Context, period uuid.UUID) ([]string, error) {
	if err := s.u.check(); err != nil {
		return nil, err
	}
	out, err := s.u.q.LockedModulesOfPeriod(ctx, period)
	return out, mapError(err)
}

func toModuleLock(id, period uuid.UUID, module string, locked bool, version int32, by uuid.UUID, at time.Time) storage.ModuleLock {
	return storage.ModuleLock{ID: id, PeriodID: period, Module: module, Locked: locked, Version: int(version), ChangedBy: by, ChangedAt: at}
}

func (s softLocks) ModuleLocks(ctx context.Context) ([]storage.ModuleLock, error) {
	if err := s.u.check(); err != nil {
		return nil, err
	}
	rows, err := s.u.q.ListModuleLocks(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ModuleLock, len(rows))
	for i, r := range rows {
		out[i] = toModuleLock(r.ID, r.PeriodID, r.Module, r.Locked, r.Version, r.ChangedBy, r.ChangedAt)
	}
	return out, nil
}

func (s softLocks) SetModule(ctx context.Context, period uuid.UUID, module string, locked bool, by uuid.UUID, at time.Time) (storage.ModuleLock, error) {
	if err := s.u.check(); err != nil {
		return storage.ModuleLock{}, err
	}
	if !slices.Contains(storage.Modules, module) {
		return storage.ModuleLock{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("%q is not a module that can be locked", module),
			Hint: "Use ar, ap, banking or payroll"}
	}
	// ENG-47: the ledger lock first, so a lock and a posting never interleave.
	if _, err := s.u.q.LockLedger(ctx); err != nil {
		return storage.ModuleLock{}, mapError(err)
	}
	p, err := s.u.q.LockPeriod(ctx, period)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ModuleLock{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ModuleLock{}, mapError(err)
	}
	if p.Status == string(storage.PeriodClosed) {
		return storage.ModuleLock{}, &storage.Error{Code: storage.CodePeriodState,
			Message: fmt.Sprintf("the period %s to %s is closed, which locks every module", day(p.StartsOn), day(p.EndsOn)),
			Hint:    "Lock modules of a period that is not closed"}
	}
	r, err := s.u.q.SetModuleLock(ctx, sqlcdb.SetModuleLockParams{PeriodID: period, Module: module, Locked: locked, By: by, At: at})
	if err != nil {
		return storage.ModuleLock{}, mapError(err)
	}
	v := int(r.Version)
	l := toModuleLock(r.ID, r.PeriodID, r.Module, r.Locked, r.Version, r.ChangedBy, r.ChangedAt)
	return l, s.u.event(ctx, "period.module_lock_set", "period_module_lock", l.ID, &v,
		map[string][2]any{"period_id": {nil, period.String()}, "module": {nil, module}, "locked": {nil, locked}})
}

func (s softLocks) MakePass(ctx context.Context, n storage.NewSoftLockPass) (storage.SoftLockPass, error) {
	if err := s.u.check(); err != nil {
		return storage.SoftLockPass{}, err
	}
	if n.CreatedAt.IsZero() || n.ExpiresAt.IsZero() {
		n.CreatedAt = s.u.store.clock.Now()
		n.ExpiresAt = n.CreatedAt.Add(time.Duration(n.Hours) * time.Hour) // the database refuses 0 hours and more than 8
	}
	r, err := s.u.q.InsertSoftLockPass(ctx, sqlcdb.InsertSoftLockPassParams{PeriodID: n.PeriodID, UserID: n.UserID, Reason: n.Reason, CreatedAt: n.CreatedAt, ExpiresAt: n.ExpiresAt})
	if err != nil {
		return storage.SoftLockPass{}, mapError(err)
	}
	p := storage.SoftLockPass(r)
	return p, s.u.event(ctx, "period.soft_lock_pass_made", "soft_lock_pass", p.ID, nil, map[string][2]any{"period_id": {nil, p.PeriodID.String()},
		"user_id": {nil, p.UserID.String()}, "reason": {nil, p.Reason}, "expires_at": {nil, p.ExpiresAt.UTC().Format(time.RFC3339)}})
}

func (s softLocks) LivePass(ctx context.Context, period, user uuid.UUID) (storage.SoftLockPass, bool, error) {
	if err := s.u.check(); err != nil {
		return storage.SoftLockPass{}, false, err
	}
	r, err := s.u.q.LiveSoftLockPass(ctx, sqlcdb.LiveSoftLockPassParams{PeriodID: period, UserID: user, At: s.u.store.clock.Now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SoftLockPass{}, false, nil
	}
	if err != nil {
		return storage.SoftLockPass{}, false, mapError(err)
	}
	return storage.SoftLockPass(r), true, nil
}

func toException(r sqlcdb.InsertSoftLockExceptionRow) storage.SoftLockException {
	return storage.SoftLockException(r)
}

func (s softLocks) RecordException(ctx context.Context, e storage.SoftLockException) (storage.SoftLockException, error) {
	if err := s.u.check(); err != nil {
		return storage.SoftLockException{}, err
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.u.store.clock.Now()
	}
	r, err := s.u.q.InsertSoftLockException(ctx, sqlcdb.InsertSoftLockExceptionParams{PassID: e.PassID, PeriodID: e.PeriodID, EntryID: e.EntryID, UserID: e.UserID,
		Scope: e.Scope, SourceType: e.SourceType, PostingDate: dateOf(e.PostingDate), Reason: e.Reason, CreatedAt: e.CreatedAt})
	if err != nil {
		return storage.SoftLockException{}, mapError(err)
	}
	x := toException(r)
	return x, s.u.event(ctx, "period.soft_lock_exception", "soft_lock_exception", x.ID, nil, map[string][2]any{"entry_id": {nil, x.EntryID.String()}, "scope": {nil, x.Scope},
		"source_type": {nil, x.SourceType}, "posting_date": {nil, day(x.PostingDate)}, "reason": {nil, x.Reason}})
}

func (s softLocks) Exceptions(ctx context.Context, limit int) ([]storage.SoftLockException, error) {
	if err := s.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.u.q.ListSoftLockExceptions(ctx, int32(limit)) // 1 to 1,000, bounded above
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SoftLockException, len(rows))
	for i, r := range rows {
		out[i] = storage.SoftLockException(r)
	}
	return out, nil
}
