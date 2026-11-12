package postgres

import (
	"context"
	"time"

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
