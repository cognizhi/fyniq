package postgres

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the checks (FR-CTL-01, FR-CTL-05; p1-checks design D4)

func (u *unitOfWork) Checks() storage.Checks { return checks{u} }

type checks struct{ u *unitOfWork }

func toCheckState(r sqlcdb.FyniqCheckState) storage.CheckState {
	return storage.CheckState{ID: r.ID, Key: r.Key, Status: r.Status, Count: int(r.ItemCount), Items: r.Items, Detail: str(r.Detail), Fix: str(r.Fix),
		Needs: str(r.Needs), Setting: str(r.Setting), RunAt: r.RunAt, Trigger: r.RunTrigger, LedgerRevision: r.LedgerRevision, ChangedAt: r.ChangedAt,
		Version: int(r.Version)}
}

func toCheckRun(r sqlcdb.FyniqCheckRun) storage.CheckRun {
	return storage.CheckRun{ID: r.ID, NewCheckRun: storage.NewCheckRun{Trigger: r.RunTrigger, RequestedBy: str(r.RequestedBy), StartedAt: r.StartedAt,
		FinishedAt: r.FinishedAt, LedgerRevision: r.LedgerRevision, AuditSeq: r.AuditSeq, Keys: r.Keys, Results: r.Results}}
}

func toCheckOwner(r sqlcdb.FyniqCheckOwner) storage.CheckOwner {
	return storage.CheckOwner{ID: r.ID, Key: r.Key, Person: r.PersonUserID, SetBy: r.SetBy, SetAt: r.SetAt, Version: int(r.Version)}
}

func (r checks) States(ctx context.Context) ([]storage.CheckState, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListCheckStates(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CheckState, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCheckState(row))
	}
	return out, nil
}

func (r checks) Save(ctx context.Context, s storage.CheckState, version int) (storage.CheckState, error) {
	if err := r.u.check(); err != nil {
		return storage.CheckState{}, err
	}
	items := s.Items
	if len(items) == 0 {
		items = []byte("[]")
	}
	count := int32(min(max(s.Count, 0), 1<<30)) // #nosec G115 -- bounded
	if version == 0 {
		row, err := r.u.q.InsertCheckState(ctx, sqlcdb.InsertCheckStateParams{Key: s.Key, Status: s.Status, ItemCount: count, Items: items,
			Detail: opt(s.Detail), Fix: opt(s.Fix), Needs: opt(s.Needs), Setting: opt(s.Setting), RunAt: s.RunAt, RunTrigger: s.Trigger,
			LedgerRevision: s.LedgerRevision, ChangedAt: s.ChangedAt})
		if err != nil {
			return storage.CheckState{}, mapError(err) // a check written meanwhile is CodeVersion (check_state_pkey)
		}
		out := toCheckState(row)
		v := out.Version
		return out, r.u.event(ctx, "check.status_changed", "check_state", out.ID, &v, map[string][2]any{"key": {nil, s.Key},
			"status": {nil, s.Status}, "count": {nil, s.Count}})
	}
	before, err := r.u.q.GetCheckState(ctx, s.Key)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CheckState{}, &storage.Error{Code: storage.CodeVersion, Message: "the check has no state at that version"}
	}
	if err != nil {
		return storage.CheckState{}, mapError(err)
	}
	row, err := r.u.q.UpdateCheckState(ctx, sqlcdb.UpdateCheckStateParams{Key: s.Key, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		Status: s.Status, ItemCount: count, Items: items, Detail: opt(s.Detail), Fix: opt(s.Fix), Needs: opt(s.Needs), Setting: opt(s.Setting),
		RunAt: s.RunAt, RunTrigger: s.Trigger, LedgerRevision: s.LedgerRevision, ChangedAt: s.ChangedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CheckState{}, &storage.Error{Code: storage.CodeVersion, Message: "the check's state changed since it was read"}
	}
	if err != nil {
		return storage.CheckState{}, mapError(err)
	}
	out := toCheckState(row)
	if before.Status == s.Status {
		return out, nil // the same finding: when it ran moved, and the trail has nothing to add
	}
	v := out.Version
	return out, r.u.event(ctx, "check.status_changed", "check_state", out.ID, &v, map[string][2]any{"key": {s.Key, s.Key},
		"status": {before.Status, s.Status}, "count": {int(before.ItemCount), s.Count}})
}

func (r checks) RecordRun(ctx context.Context, n storage.NewCheckRun) (storage.CheckRun, error) {
	if err := r.u.check(); err != nil {
		return storage.CheckRun{}, err
	}
	keys := slices.Clone(n.Keys)
	if keys == nil {
		keys = []string{}
	}
	row, err := r.u.q.InsertCheckRun(ctx, sqlcdb.InsertCheckRunParams{RunTrigger: n.Trigger, RequestedBy: opt(n.RequestedBy), StartedAt: n.StartedAt,
		FinishedAt: n.FinishedAt, LedgerRevision: n.LedgerRevision, AuditSeq: n.AuditSeq, Keys: keys, Results: n.Results})
	if err != nil {
		return storage.CheckRun{}, mapError(err)
	}
	return toCheckRun(row), nil
}

func (r checks) LatestRun(ctx context.Context, trigger string) (storage.CheckRun, error) {
	if err := r.u.check(); err != nil {
		return storage.CheckRun{}, err
	}
	row, err := r.u.q.LatestCheckRun(ctx, trigger)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CheckRun{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.CheckRun{}, mapError(err)
	}
	return toCheckRun(row), nil
}

func (r checks) Watermark(ctx context.Context) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	seq, err := r.u.q.CheckWatermark(ctx)
	return seq, mapError(err)
}

func (r checks) Purge(ctx context.Context, before, nightlyBefore time.Time) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.PurgeCheckRuns(ctx, sqlcdb.PurgeCheckRunsParams{Before: before, NightlyBefore: nightlyBefore})
	return n, mapError(err)
}

func (r checks) Owners(ctx context.Context) ([]storage.CheckOwner, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListCheckOwners(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CheckOwner, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCheckOwner(row))
	}
	return out, nil
}

// personOf is the audit trail's record of a check's owner: the person's user, or nil for the catalogue's role.
func personOf(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return p.String()
}

func (r checks) SetOwner(ctx context.Context, key string, person *uuid.UUID, setBy string, at time.Time, version int) (storage.CheckOwner, error) {
	if err := r.u.check(); err != nil {
		return storage.CheckOwner{}, err
	}
	if version == 0 {
		row, err := r.u.q.InsertCheckOwner(ctx, sqlcdb.InsertCheckOwnerParams{Key: key, PersonUserID: person, SetBy: setBy, SetAt: at})
		if err != nil {
			return storage.CheckOwner{}, mapError(err) // an owner named meanwhile is CodeVersion (check_owner_pkey)
		}
		out := toCheckOwner(row)
		v := out.Version
		return out, r.u.event(ctx, "check.owner_set", "check_owner", out.ID, &v, map[string][2]any{"key": {nil, key}, "person": {nil, personOf(person)}})
	}
	before, err := r.u.q.GetCheckOwner(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CheckOwner{}, &storage.Error{Code: storage.CodeVersion, Message: "the check has no owner at that version"}
	}
	if err != nil {
		return storage.CheckOwner{}, mapError(err)
	}
	row, err := r.u.q.UpdateCheckOwner(ctx, sqlcdb.UpdateCheckOwnerParams{Key: key, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		PersonUserID: person, SetBy: setBy, SetAt: at})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CheckOwner{}, &storage.Error{Code: storage.CodeVersion, Message: "the check's owner changed since it was read"}
	}
	if err != nil {
		return storage.CheckOwner{}, mapError(err)
	}
	out := toCheckOwner(row)
	v := out.Version
	return out, r.u.event(ctx, "check.owner_set", "check_owner", out.ID, &v, map[string][2]any{"key": {key, key},
		"person": {personOf(before.PersonUserID), personOf(person)}})
}

func toCheckReview(r sqlcdb.FyniqCheckReview) storage.CheckReview {
	return storage.CheckReview{ID: r.ID, NewCheckReview: storage.NewCheckReview{Key: r.Key, ItemKind: r.ItemKind, ItemID: r.ItemID, ItemVersion: r.ItemVersion,
		Note: str(r.Note), ReviewedBy: r.ReviewedBy, ReviewedAt: r.ReviewedAt}}
}

func (r checks) Reviews(ctx context.Context) ([]storage.CheckReview, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListCheckReviews(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CheckReview, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCheckReview(row))
	}
	return out, nil
}

func (r checks) Review(ctx context.Context, n storage.NewCheckReview) (storage.CheckReview, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.CheckReview{}, false, err
	}
	row, err := r.u.q.InsertCheckReview(ctx, sqlcdb.InsertCheckReviewParams{Key: n.Key, ItemKind: n.ItemKind, ItemID: n.ItemID, ItemVersion: n.ItemVersion,
		Note: opt(n.Note), ReviewedBy: n.ReviewedBy, ReviewedAt: n.ReviewedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CheckReview{NewCheckReview: n}, false, nil // reviewed at this version already
	}
	if err != nil {
		return storage.CheckReview{}, false, mapError(err)
	}
	out := toCheckReview(row)
	return out, true, r.u.event(ctx, "check.item_reviewed", "check_review", out.ID, nil, map[string][2]any{"key": {nil, n.Key},
		"item": {nil, n.ItemKind + ":" + n.ItemID}, "item_version": {nil, n.ItemVersion}, "note": {nil, n.Note}})
}

func (r outbox) Waiting(ctx context.Context, kind string) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	waiting, err := r.u.q.OutboxWaiting(ctx, kind)
	return waiting, mapError(err)
}

// changedTypes are the object types of the events u recorded, sorted, each once.
func (u *unitOfWork) changedTypes() []string {
	var out []string
	for _, e := range u.pending {
		out = append(out, cmp.Or(e.Object.Type, "-"))
	}
	slices.Sort(out)
	return slices.Compact(out)
}
