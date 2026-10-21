package postgres

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// accountingBasis keeps the accounting framework and policies (FR-ORG-10; PRD-001 rev 0.62).
type accountingBasis struct{ u *unitOfWork }

func (u *unitOfWork) AccountingBasis() storage.AccountingBasis { return accountingBasis{u} }

func toFramework(r sqlcdb.ListAccountingFrameworksRow) storage.FrameworkVersion {
	return storage.FrameworkVersion{ID: r.ID, Version: int(r.Version), FromOn: r.FromOn, Framework: r.Framework, Edition: r.Edition,
		WrittenBy: r.WrittenBy, WrittenAt: r.WrittenAt, ConfirmedBy: r.ConfirmedBy, ConfirmedAt: r.ConfirmedAt}
}

func toPolicy(r sqlcdb.ListAccountingPoliciesRow) storage.PolicyVersion {
	return storage.PolicyVersion{ID: r.ID, Area: r.Area, Version: int(r.Version), FromOn: r.FromOn, Wording: r.Wording,
		WrittenBy: r.WrittenBy, WrittenAt: r.WrittenAt, ApprovedBy: r.ApprovedBy, ApprovedAt: r.ApprovedAt}
}

func (a accountingBasis) RecordFramework(ctx context.Context, from time.Time, framework, edition string) (storage.FrameworkVersion, error) {
	if err := a.u.check(); err != nil {
		return storage.FrameworkVersion{}, err
	}
	row, err := a.u.q.InsertAccountingFramework(ctx, sqlcdb.InsertAccountingFrameworkParams{FromOn: from, Framework: framework,
		Edition: edition, WrittenBy: a.u.actor(ctx)})
	if err != nil {
		return storage.FrameworkVersion{}, mapError(err)
	}
	f := toFramework(sqlcdb.ListAccountingFrameworksRow(row))
	if err := a.u.event(ctx, "accounting_framework.recorded", "accounting_framework", f.ID, &f.Version, map[string][2]any{
		"from_on": {nil, day(f.FromOn)}, "framework": {nil, f.Framework}, "edition": {nil, f.Edition}}); err != nil {
		return storage.FrameworkVersion{}, err
	}
	return f, nil
}

func (a accountingBasis) Frameworks(ctx context.Context) ([]storage.FrameworkVersion, error) {
	if err := a.u.check(); err != nil {
		return nil, err
	}
	rows, err := a.u.q.ListAccountingFrameworks(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.FrameworkVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, toFramework(r))
	}
	return out, nil
}

func (a accountingBasis) ConfirmFramework(ctx context.Context, version int) (storage.FrameworkVersion, error) {
	if err := a.u.check(); err != nil {
		return storage.FrameworkVersion{}, err
	}
	by := a.u.actor(ctx)
	row, err := a.u.q.ConfirmAccountingFramework(ctx, sqlcdb.ConfirmAccountingFrameworkParams{Version: int32(version), ConfirmedBy: &by}) // #nosec G115 -- a version number, checked positive by the API
	if err != nil {
		return storage.FrameworkVersion{}, mapError(err)
	}
	f := toFramework(sqlcdb.ListAccountingFrameworksRow(row))
	if err := a.u.event(ctx, "accounting_framework.confirmed", "accounting_framework", f.ID, &f.Version, map[string][2]any{
		"confirmed_by": {nil, by}}); err != nil {
		return storage.FrameworkVersion{}, err
	}
	return f, nil
}

func (a accountingBasis) RecordPolicy(ctx context.Context, area string, from time.Time, wording string) (storage.PolicyVersion, error) {
	if err := a.u.check(); err != nil {
		return storage.PolicyVersion{}, err
	}
	row, err := a.u.q.InsertAccountingPolicy(ctx, sqlcdb.InsertAccountingPolicyParams{Area: area, FromOn: from, Wording: wording,
		WrittenBy: a.u.actor(ctx)})
	if err != nil {
		return storage.PolicyVersion{}, mapError(err)
	}
	p := toPolicy(sqlcdb.ListAccountingPoliciesRow(row))
	if err := a.u.event(ctx, "accounting_policy.recorded", "accounting_policy", p.ID, &p.Version, map[string][2]any{
		"area": {nil, p.Area}, "from_on": {nil, day(p.FromOn)}, "wording": {nil, p.Wording}}); err != nil {
		return storage.PolicyVersion{}, err
	}
	return p, nil
}

func (a accountingBasis) Policies(ctx context.Context) ([]storage.PolicyVersion, error) {
	if err := a.u.check(); err != nil {
		return nil, err
	}
	rows, err := a.u.q.ListAccountingPolicies(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PolicyVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPolicy(r))
	}
	return out, nil
}

func (a accountingBasis) ApprovePolicy(ctx context.Context, area string, version int) (storage.PolicyVersion, error) {
	if err := a.u.check(); err != nil {
		return storage.PolicyVersion{}, err
	}
	by := a.u.actor(ctx)
	row, err := a.u.q.ApproveAccountingPolicy(ctx, sqlcdb.ApproveAccountingPolicyParams{Area: area, Version: int32(version), ApprovedBy: &by}) // #nosec G115 -- a version number, checked positive by the API
	if err != nil {
		return storage.PolicyVersion{}, mapError(err)
	}
	p := toPolicy(sqlcdb.ListAccountingPoliciesRow(row))
	if err := a.u.event(ctx, "accounting_policy.approved", "accounting_policy", p.ID, &p.Version, map[string][2]any{
		"approved_by": {nil, by}}); err != nil {
		return storage.PolicyVersion{}, err
	}
	return p, nil
}
