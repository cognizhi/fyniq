package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------- the Compliance Center's verifications (CMP-01; p1-compliance-center D4)

func (u *unitOfWork) Compliance() storage.Compliance { return compliance{u} }

type compliance struct{ u *unitOfWork }

func toControlRun(r sqlcdb.FyniqControlRun) storage.ControlRun {
	return storage.ControlRun{ID: r.ID, Control: r.Control, Frequency: r.Frequency, Check: r.CheckHow, Since: r.Since, RanAt: r.RanAt,
		OK: r.Ok, Count: int(r.ItemCount), Items: r.Items, Detail: r.Detail}
}

func (c compliance) RecordRun(ctx context.Context, r storage.ControlRun) (storage.ControlRun, error) {
	if err := c.u.check(); err != nil {
		return storage.ControlRun{}, err
	}
	items := r.Items
	if len(items) == 0 {
		items = []byte("[]")
	}
	row, err := c.u.q.InsertControlRun(ctx, sqlcdb.InsertControlRunParams{Control: r.Control, Frequency: r.Frequency, CheckHow: r.Check,
		Since: r.Since, RanAt: r.RanAt, Ok: r.OK, ItemCount: int32(min(max(r.Count, 0), 1<<30)), Items: items, Detail: r.Detail}) // #nosec G115 -- bounded
	if err != nil {
		return storage.ControlRun{}, mapError(err)
	}
	return toControlRun(row), nil
}

func (c compliance) LatestRuns(ctx context.Context) ([]storage.ControlRun, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.LatestControlRuns(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ControlRun, len(rows))
	for i, r := range rows {
		out[i] = toControlRun(r)
	}
	return out, nil
}

func (c compliance) DualControl(ctx context.Context, since time.Time) ([]storage.DualControlApproval, []storage.DualControlRelease, error) {
	if err := c.u.check(); err != nil {
		return nil, nil, err
	}
	approvals, err := c.u.q.DualControlApprovals(ctx, &since)
	if err != nil {
		return nil, nil, mapError(err)
	}
	releases, err := c.u.q.DualControlReleases(ctx, since)
	if err != nil {
		return nil, nil, mapError(err)
	}
	a := make([]storage.DualControlApproval, len(approvals))
	for i, r := range approvals {
		a[i] = storage.DualControlApproval{ID: r.ID, ObjectType: r.ObjectType, ObjectKey: r.ObjectKey, DecidedAt: r.DecidedAt,
			Signatures: int(r.Signatures), OwnSignatures: int(r.OwnSignatures)}
	}
	rel := make([]storage.DualControlRelease, len(releases))
	for i, r := range releases {
		rel[i] = storage.DualControlRelease{BatchID: r.BatchID, ReleasedAt: r.ReleasedAt, ApprovalID: r.ApprovalID, BreakGlassID: r.BreakGlassID,
			Approved: r.Approved, Signatures: int(r.Signatures), OwnSignatures: int(r.OwnSignatures), BreakGlassUsed: r.BreakGlassUsed}
	}
	return a, rel, nil
}

func (c compliance) SecondFactors(ctx context.Context, since time.Time) ([]storage.MemberSecondFactor, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.MembersSecondFactor(ctx, since)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MemberSecondFactor, len(rows))
	for i, r := range rows {
		out[i] = storage.MemberSecondFactor{UserID: r.UserID, Passkeys: int(r.Passkeys), SessionsWith: int(r.SessionsWith),
			SessionsWithout: int(r.SessionsWithout)}
		if r.SessionsWithout > 0 {
			at := r.LastWithout
			out[i].LastWithout = &at
		}
	}
	return out, nil
}

func (c compliance) UnreconciledReads(ctx context.Context, since time.Time, actions []string, sample int) ([]storage.UnreconciledRead, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ExportsWithoutTheirRecords(ctx, sqlcdb.ExportsWithoutTheirRecordsParams{Since: since, Actions: actions,
		Sample: int32(min(max(sample, 1), 100000))}) // #nosec G115 -- bounded
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.UnreconciledRead, len(rows))
	for i, r := range rows {
		out[i] = storage.UnreconciledRead{Kind: r.Kind, ObjectKey: r.ObjectKey, At: r.At, Reason: r.Reason}
	}
	return out, nil
}

func toControlState(r sqlcdb.FyniqControlState) storage.ControlState {
	return storage.ControlState{ID: r.ID, Control: r.Control, Status: r.Status, Detail: r.Detail, Checks: r.Checks, FailingSince: r.FailingSince,
		RunAt: r.RunAt, ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

func (c compliance) States(ctx context.Context) ([]storage.ControlState, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListControlStates(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ControlState, len(rows))
	for i, r := range rows {
		out[i] = toControlState(r)
	}
	return out, nil
}

// failing reports whether a status is fail or overdue.
func failing(status string) bool { return status == "fail" || status == "overdue" }

func (c compliance) SaveState(ctx context.Context, s storage.ControlState, version int) (storage.ControlState, error) {
	if err := c.u.check(); err != nil {
		return storage.ControlState{}, err
	}
	checks := s.Checks
	if len(checks) == 0 {
		checks = []byte("[]")
	}
	if version == 0 {
		row, err := c.u.q.InsertControlState(ctx, sqlcdb.InsertControlStateParams{Control: s.Control, Status: s.Status, Detail: s.Detail,
			Checks: checks, FailingSince: s.FailingSince, RunAt: s.RunAt, ChangedAt: s.ChangedAt})
		if err != nil {
			return storage.ControlState{}, mapError(err) // a state written meanwhile is CodeVersion (control_state_one)
		}
		out := toControlState(row)
		v := out.Version
		action := "control.status_changed"
		if failing(s.Status) {
			action = "control.failed"
		}
		return out, c.u.event(ctx, action, "control_state", out.ID, &v, map[string][2]any{"control": {nil, s.Control},
			"status": {nil, s.Status}})
	}
	before, err := c.u.q.GetControlState(ctx, s.Control)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ControlState{}, &storage.Error{Code: storage.CodeVersion, Message: "the control has no state at that version"}
	}
	if err != nil {
		return storage.ControlState{}, mapError(err)
	}
	row, err := c.u.q.UpdateControlState(ctx, sqlcdb.UpdateControlStateParams{Control: s.Control, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		Status: s.Status, Detail: s.Detail, Checks: checks, FailingSince: s.FailingSince, RunAt: s.RunAt, ChangedAt: s.ChangedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ControlState{}, &storage.Error{Code: storage.CodeVersion, Message: "the control's state changed since it was read"}
	}
	if err != nil {
		return storage.ControlState{}, mapError(err)
	}
	out := toControlState(row)
	if before.Status == s.Status {
		return out, nil // the same status: when it ran moved, and the trail has nothing to add
	}
	action := "control.status_changed"
	switch {
	case failing(s.Status) && !failing(before.Status):
		action = "control.failed"
	case !failing(s.Status) && failing(before.Status):
		action = "control.recovered"
	}
	v := out.Version
	return out, c.u.event(ctx, action, "control_state", out.ID, &v, map[string][2]any{"control": {s.Control, s.Control},
		"status": {before.Status, s.Status}})
}
