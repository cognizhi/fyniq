package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- groups of entities (FR-ENT-04; p3-multi-entity D2 and D3)

func (u *unitOfWork) EntityGroups() storage.EntityGroups { return entityGroups{u} }

type entityGroups struct{ u *unitOfWork }

func toGroupEntity(r sqlcdb.GetGroupEntityRow) storage.GroupEntity {
	e := storage.GroupEntity{ID: r.ID, Label: r.Label, EntityName: str(r.EntityName), EntityCurrency: str(r.EntityCurrency), State: r.State, InvitedBy: r.InvitedBy, InvitedAt: r.InvitedAt,
		EndedReason: str(r.EndedReason), Version: int(r.Version)}
	if r.EntityOrg != nil {
		e.EntityOrg = *r.EntityOrg
	}
	if r.LinkedBy != nil {
		e.LinkedBy = *r.LinkedBy
	}
	if r.LinkedAt != nil {
		e.LinkedAt = *r.LinkedAt
	}
	if r.EndedBy != nil {
		e.EndedBy = *r.EndedBy
	}
	if r.EndedAt != nil {
		e.EndedAt = *r.EndedAt
	}
	return e
}

func toGroupMembership(r sqlcdb.GetGroupMembershipRow) storage.GroupMembership {
	m := storage.GroupMembership{ID: r.ID, ParentOrg: r.ParentOrg, ParentName: r.ParentName, ParentRow: r.ParentRow, Scopes: r.Scopes, State: r.State, AcceptedBy: r.AcceptedBy,
		AcceptedAt: r.AcceptedAt, EndedReason: str(r.EndedReason), Version: int(r.Version)}
	if r.EndedBy != nil {
		m.EndedBy = *r.EndedBy
	}
	if r.EndedAt != nil {
		m.EndedAt = *r.EndedAt
	}
	return m
}

func (g entityGroups) Entities(ctx context.Context) ([]storage.GroupEntity, error) {
	if err := g.u.check(); err != nil {
		return nil, err
	}
	rows, err := g.u.q.ListGroupEntities(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.GroupEntity, len(rows))
	for i, r := range rows {
		out[i] = toGroupEntity(sqlcdb.GetGroupEntityRow(r))
	}
	return out, nil
}

func (g entityGroups) Memberships(ctx context.Context) ([]storage.GroupMembership, error) {
	if err := g.u.check(); err != nil {
		return nil, err
	}
	rows, err := g.u.q.ListGroupMemberships(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.GroupMembership, len(rows))
	for i, r := range rows {
		out[i] = toGroupMembership(sqlcdb.GetGroupMembershipRow(r))
	}
	return out, nil
}

func (g entityGroups) Invite(ctx context.Context, n storage.NewGroupInvitation) (storage.GroupEntity, error) {
	if err := g.u.check(); err != nil {
		return storage.GroupEntity{}, err
	}
	row, err := g.u.q.InsertGroupEntity(ctx, sqlcdb.InsertGroupEntityParams{Label: n.Label, InvitedBy: n.By, InvitedAt: n.At})
	if err != nil {
		return storage.GroupEntity{}, mapError(err)
	}
	e := toGroupEntity(sqlcdb.GetGroupEntityRow(row))
	if _, err := g.u.q.RegisterGroupInvitation(ctx, sqlcdb.RegisterGroupInvitationParams{RowID: e.ID, CodeHash: n.CodeHash, Expires: n.ExpiresAt, UserID: n.By, At: n.At}); err != nil {
		return storage.GroupEntity{}, mapError(err)
	}
	v := e.Version
	// The code and its hash are never in the trail (SEC-15); the trail says who invited, whom they call it, and until when.
	return e, g.u.event(ctx, "group_entity.invited", "group_entity", e.ID, &v, map[string][2]any{"label": {nil, e.Label}, "invited_by": {nil, n.By}, "expires_at": {nil, n.ExpiresAt}})
}

func (g entityGroups) Withdraw(ctx context.Context, id, by uuid.UUID, reason string, at time.Time) (bool, error) {
	if err := g.u.check(); err != nil {
		return false, err
	}
	before, err := g.u.q.GetGroupEntity(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapError(err)
	}
	done, err := g.u.q.WithdrawGroupInvitation(ctx, sqlcdb.WithdrawGroupInvitationParams{RowID: id, UserID: by, Reason: reason, At: at})
	if err != nil || !done {
		return false, mapError(err)
	}
	v := int(before.Version) + 1
	return true, g.u.event(ctx, "group_entity.withdrawn", "group_entity", id, &v, map[string][2]any{"state": {before.State, "ended"}, "ended_by": {nil, by}, "ended_reason": {nil, reason}})
}

// other writes the events queued for another organisation of the same transaction, then returns to this one.
func (g entityGroups) inOther(ctx context.Context, org uuid.UUID, events func(other *unitOfWork) error) error {
	if err := g.u.store.setTenant(ctx, g.u.q, org); err != nil {
		return err
	}
	other := &unitOfWork{q: g.u.q, store: g.u.store, org: org}
	if err := events(other); err != nil {
		return err
	}
	if err := other.flush(ctx); err != nil {
		return err
	}
	return g.u.store.setTenant(ctx, g.u.q, g.u.org)
}

func (g entityGroups) Accept(ctx context.Context, codeHash []byte, by uuid.UUID, at time.Time) (storage.GroupMembership, error) {
	if err := g.u.check(); err != nil {
		return storage.GroupMembership{}, err
	}
	row, err := g.u.q.AcceptGroupInvitation(ctx, sqlcdb.AcceptGroupInvitationParams{CodeHash: codeHash, UserID: by, At: at})
	if err != nil {
		return storage.GroupMembership{}, mapError(err)
	}
	m, err := g.u.q.GetGroupMembership(ctx, row.MembershipID)
	if err != nil {
		return storage.GroupMembership{}, mapError(err)
	}
	out := toGroupMembership(m)
	v := 1
	if err := g.u.event(ctx, "group_membership.accepted", "group_membership", out.ID, &v, map[string][2]any{"parent_org": {nil, out.ParentOrg}, "parent_name": {nil, out.ParentName},
		"scopes": {nil, out.Scopes}, "accepted_by": {nil, by}}); err != nil {
		return storage.GroupMembership{}, err
	}
	// The parent's trail, under its own tenant: its entity row became active.
	pv := 2
	err = g.inOther(ctx, row.ParentOrg, func(p *unitOfWork) error {
		return p.event(ctx, "group_entity.linked", "group_entity", row.ParentRow, &pv, map[string][2]any{"state": {"invited", "active"}, "entity_org": {nil, g.u.org},
			"entity_name": {nil, row.EntityName}, "entity_currency": {nil, row.EntityCurrency}, "linked_by": {nil, by}})
	})
	return out, err
}

func (g entityGroups) End(ctx context.Context, by, other uuid.UUID, reason string, at time.Time) (storage.EndedLink, error) {
	if err := g.u.check(); err != nil {
		return storage.EndedLink{}, err
	}
	row, err := g.u.q.EndGroupLink(ctx, sqlcdb.EndGroupLinkParams{UserID: by, Other: other, Reason: reason, At: at})
	if err != nil {
		return storage.EndedLink{}, mapError(err)
	}
	out := storage.EndedLink{Role: row.Role, EntityRow: row.ParentRow, MembershipID: row.MembershipID}
	changes := map[string][2]any{"state": {"active", "ended"}, "ended_by": {nil, by}, "ended_reason": {nil, reason}}
	v := 2
	entityEvent := func(u *unitOfWork) error {
		return u.event(ctx, "group_membership.ended", "group_membership", row.MembershipID, &v, changes)
	}
	parentEvent := func(u *unitOfWork) error {
		return u.event(ctx, "group_entity.ended", "group_entity", row.ParentRow, &v, changes)
	}
	mine, theirs := parentEvent, entityEvent
	if row.Role == "entity" {
		mine, theirs = entityEvent, parentEvent
	}
	if err := mine(g.u); err != nil {
		return storage.EndedLink{}, err
	}
	return out, g.inOther(ctx, other, theirs)
}

func (g entityGroups) Figures(ctx context.Context, entity uuid.UUID, through time.Time, by uuid.UUID) (storage.EntityFigures, error) {
	if err := g.u.check(); err != nil {
		return storage.EntityFigures{}, err
	}
	rows, err := g.u.q.GroupEntityFigures(ctx, sqlcdb.GroupEntityFiguresParams{Entity: entity, Through: through})
	if err != nil {
		return storage.EntityFigures{}, mapError(err)
	}
	periods, err := g.u.q.GroupEntityPeriods(ctx, sqlcdb.GroupEntityPeriodsParams{Entity: entity, Through: through})
	if err != nil {
		return storage.EntityFigures{}, mapError(err)
	}
	var out storage.EntityFigures
	for _, r := range rows {
		out.Currency = r.BaseCurrency
		out.Lines = append(out.Lines, storage.EntityFigureLine{Code: r.AccountCode, Name: r.AccountName, Type: r.AccountType, Currency: r.BaseCurrency, Month: r.Month, Net: storage.Decimal(r.NetBase)})
	}
	for _, p := range periods {
		out.Revision = p.LedgerRevision
		out.Periods = append(out.Periods, storage.EntityPeriod{Month: p.PeriodMonth, Status: p.PeriodStatus})
	}
	// Every read is in both trails: the parent's, and the entity's own (AUD-01, FR-ENT-04). No figure is written to either.
	read := map[string][2]any{"read_by": {nil, by}, "through": {nil, through.Format("2006-01")}, "accounts": {nil, len(out.Lines)}}
	if err := g.u.event(ctx, "group.figures_read", "organisation", entity, nil, read); err != nil {
		return storage.EntityFigures{}, err
	}
	return out, g.inOther(ctx, entity, func(e *unitOfWork) error {
		return e.event(ctx, "group.figures_read_by_parent", "organisation", g.u.org, nil, read)
	})
}

var _ = ports.ErrNotFound
