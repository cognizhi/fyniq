package postgres

import (
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

func (r members) List(ctx context.Context, now time.Time) ([]storage.Member, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListMembers(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	names, err := r.u.q.MemberNames(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	roles, err := r.u.q.LiveRoleAssignments(ctx, &now)
	if err != nil {
		return nil, mapError(err)
	}
	name := make(map[uuid.UUID]string, len(names))
	for _, n := range names {
		name[n.UserID] = n.DisplayName
	}
	out := make([]storage.Member, 0, len(rows))
	for _, m := range rows {
		mem := storage.Member{Membership: storage.Membership{ID: m.ID, UserID: m.UserID, Status: m.Status, Version: int(m.Version)},
			DisplayName: name[m.UserID], Roles: []storage.RoleAssignment{}}
		for _, a := range roles {
			if a.MembershipID == m.ID {
				mem.Roles = append(mem.Roles, storage.RoleAssignment{ID: a.ID, MembershipID: a.MembershipID, Role: a.Role, ExpiresAt: a.ExpiresAt})
			}
		}
		out = append(out, mem)
	}
	return out, nil
}

func (r members) Count(ctx context.Context) (int, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	if err := r.u.q.LockMembers(ctx); err != nil {
		return 0, mapError(err)
	}
	n, err := r.u.q.CountActiveMembers(ctx)
	if err != nil {
		return 0, mapError(err)
	}
	return int(n), nil
}

func (u *unitOfWork) MemberInvitations() storage.MemberInvitations { return invitations{u} }

type invitations struct{ u *unitOfWork }

func toInvitation(r sqlcdb.GetMemberInvitationRow) storage.MemberInvitation {
	return storage.MemberInvitation{ID: r.ID, DisplayName: r.DisplayName, Roles: r.Roles, RolesExpireAt: r.RolesExpireAt,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, AcceptedAt: r.AcceptedAt, WithdrawnAt: r.WithdrawnAt}
}

func (r invitations) Create(ctx context.Context, n storage.NewMemberInvitation) (storage.MemberInvitation, error) {
	if err := r.u.check(); err != nil {
		return storage.MemberInvitation{}, err
	}
	row, err := r.u.q.InsertMemberInvitation(ctx, sqlcdb.InsertMemberInvitationParams{CodeHash: n.CodeHash, DisplayName: n.DisplayName,
		Roles: n.Roles, RolesExpireAt: n.RolesExpireAt, ExpiresAt: n.ExpiresAt})
	if err != nil {
		return storage.MemberInvitation{}, mapError(err)
	}
	inv := toInvitation(sqlcdb.GetMemberInvitationRow(row))
	if err := r.u.event(ctx, "member_invitation.created", "member_invitation", inv.ID, nil, map[string][2]any{
		"display_name": {nil, inv.DisplayName}, "roles": {nil, inv.Roles}, "roles_expire_at": {nil, inv.RolesExpireAt},
		"expires_at": {nil, inv.ExpiresAt}}); err != nil {
		return storage.MemberInvitation{}, err
	}
	return inv, nil
}

func (r invitations) Pending(ctx context.Context, now time.Time) ([]storage.MemberInvitation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PendingMemberInvitations(ctx, now)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MemberInvitation, len(rows))
	for i, row := range rows {
		out[i] = toInvitation(sqlcdb.GetMemberInvitationRow(row))
	}
	return out, nil
}

func (r invitations) Withdraw(ctx context.Context, id uuid.UUID, now time.Time) (storage.MemberInvitation, error) {
	if err := r.u.check(); err != nil {
		return storage.MemberInvitation{}, err
	}
	row, err := r.u.q.WithdrawMemberInvitation(ctx, sqlcdb.WithdrawMemberInvitationParams{ID: id, Now: &now})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.MemberInvitation{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.MemberInvitation{}, mapError(err)
	}
	inv := toInvitation(sqlcdb.GetMemberInvitationRow(row))
	if err := r.u.event(ctx, "member_invitation.withdrawn", "member_invitation", inv.ID, nil, map[string][2]any{
		"withdrawn_at": {nil, inv.WithdrawnAt}}); err != nil {
		return storage.MemberInvitation{}, err
	}
	return inv, nil
}

// JoinOrganisation implements storage.Installation: one transaction that redeems the invitation in whichever
// organisation holds it and audits the membership, its roles and the invitation there, with the joining user
// as the actor (AUD-01).
func (s *Store) JoinOrganisation(ctx context.Context, j storage.NewJoin) (storage.Joined, error) {
	var out storage.Joined
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlcdb.Queries) error {
		row, err := q.AcceptMemberInvitation(ctx, sqlcdb.AcceptMemberInvitationParams{CodeHash: j.CodeHash, DisplayName: j.DisplayName,
			Issuer: j.Issuer, Subject: j.Subject, Now: j.Now})
		if err != nil {
			return mapError(err)
		}
		if err := s.setTenant(ctx, q, row.OrganisationID); err != nil {
			return err
		}
		audit := storage.AuditFrom(ctx)
		audit.Actor, audit.OnBehalfOf = &storage.Actor{Type: "user", ID: row.UserID.String()}, nil
		actx := storage.WithAudit(ctx, audit)
		u := &unitOfWork{q: q, store: s, org: row.OrganisationID}
		inv, err := q.GetMemberInvitation(ctx, row.InvitationID)
		if err != nil {
			return mapError(err)
		}
		roles, err := q.LiveRoleAssignments(ctx, &j.Now)
		if err != nil {
			return mapError(err)
		}
		version := 1
		if err := u.event(actx, "user_membership.created", "user_membership", row.MembershipID, &version, map[string][2]any{
			"user_id": {nil, row.UserID}, "invitation_id": {nil, row.InvitationID}, "new_user": {nil, row.NewUser}}); err != nil {
			return err
		}
		for _, a := range roles {
			if a.MembershipID != row.MembershipID {
				continue
			}
			if err := u.event(actx, "role_assignment.created", "role_assignment", a.ID, nil, map[string][2]any{
				"membership_id": {nil, a.MembershipID}, "role": {nil, a.Role}, "expires_at": {nil, a.ExpiresAt}}); err != nil {
				return err
			}
			out.Roles = append(out.Roles, a.Role)
		}
		if err := u.event(actx, "member_invitation.accepted", "member_invitation", row.InvitationID, nil, map[string][2]any{
			"accepted_by": {nil, row.UserID}, "accepted_at": {nil, inv.AcceptedAt}}); err != nil {
			return err
		}
		if err := u.flush(actx); err != nil {
			return err
		}
		slices.Sort(out.Roles)
		out.OrganisationID, out.UserID, out.MembershipID, out.NewUser = row.OrganisationID, row.UserID, row.MembershipID, row.NewUser
		return nil
	})
	return out, err
}
