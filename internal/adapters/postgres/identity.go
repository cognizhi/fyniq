package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var _ storage.Installation = (*Store)(nil)

// SetupDone implements storage.Installation.
func (s *Store) SetupDone(ctx context.Context) (bool, error) {
	done, err := s.q().SetupDone(ctx)
	return done, mapError(err)
}

// UserForIdentity implements storage.Installation.
func (s *Store) UserForIdentity(ctx context.Context, issuer, subject string) (uuid.UUID, error) {
	id, err := s.q().UserForIdentity(ctx, sqlcdb.UserForIdentityParams{Issuer: issuer, Subject: subject})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	if id == uuid.Nil {
		return uuid.Nil, ports.ErrNotFound
	}
	return id, nil
}

// CompleteSetup implements storage.Installation: one transaction that consumes the code, creates the
// Super admin and their identity, the installation's trail and the first organisation with its Owner.
// It switches the tenant once per organisation; each organisation's audit events are written under its
// own tenant, and the new user is the actor of every one (AUD-01).
func (s *Store) CompleteSetup(ctx context.Context, n storage.NewSetup) (storage.Setup, error) {
	var out storage.Setup
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlcdb.Queries) error {
		uid, err := q.ClaimSetup(ctx, sqlcdb.ClaimSetupParams{CodeHash: n.CodeHash, DisplayName: n.DisplayName, Issuer: n.Issuer, Subject: n.Subject})
		if err != nil {
			return err
		}
		audit := storage.AuditFrom(ctx)
		audit.Actor, audit.OnBehalfOf = &storage.Actor{Type: "user", ID: uid.String()}, nil
		actx := storage.WithAudit(ctx, audit)

		// The installation's trail: its organisation holds only installation events.
		inst, err := q.NewOrganisationID(ctx)
		if err != nil {
			return err
		}
		if err := s.setTenant(ctx, q, inst); err != nil {
			return err
		}
		if _, err := q.InsertInstallationOrganisation(ctx); err != nil {
			return err
		}
		ui := &unitOfWork{q: q, store: s, org: inst}
		version := 1
		for _, e := range []struct {
			action, objectType string
			id                 uuid.UUID
			version            *int
			changes            map[string][2]any
		}{
			{"organisation.created", "organisation", inst, &version, map[string][2]any{"name": {nil, "Installation"}, "kind": {nil, "installation"}}},
			{"setup.completed", "installation", inst, nil, map[string][2]any{"super_admin": {nil, uid}, "issuer": {nil, n.Issuer}}},
			{"super_admin.added", "super_admin", uid, nil, map[string][2]any{"display_name": {nil, n.DisplayName}, "added_by": {nil, "first-run setup"}}},
		} {
			if err := ui.event(actx, e.action, e.objectType, e.id, e.version, e.changes); err != nil {
				return err
			}
		}
		if err := ui.flush(actx); err != nil {
			return err
		}

		// The first organisation, with the new user as its Owner.
		org, err := q.NewOrganisationID(ctx)
		if err != nil {
			return err
		}
		if err := s.setTenant(ctx, q, org); err != nil {
			return err
		}
		row, err := q.InsertOrganisation(ctx, sqlcdb.InsertOrganisationParams{Name: n.OrganisationName, BaseCurrency: n.BaseCurrency})
		if errors.Is(err, pgx.ErrNoRows) {
			return &storage.Error{Code: storage.CodeAmount, Message: fmt.Sprintf("currency %q is not an ISO 4217 code Fyniq knows", n.BaseCurrency),
				Hint: "Choose the base currency from the list of currencies"}
		}
		if err != nil {
			return err
		}
		uc := &unitOfWork{q: q, store: s, org: org}
		if err := uc.event(actx, "organisation.created", "organisation", org, &version, map[string][2]any{
			"name": {nil, row.Name}, "base_currency": {nil, row.BaseCurrency}}); err != nil {
			return err
		}
		m, err := uc.Members().Add(actx, uid)
		if err != nil {
			return err
		}
		if _, err := uc.Members().Assign(actx, m.ID, "owner", nil); err != nil {
			return err
		}
		if err := uc.flush(actx); err != nil {
			return err
		}
		out = storage.Setup{UserID: uid, OrganisationID: org, InstallationOrganisationID: inst}
		return nil
	})
	return out, err
}

// SetSetupCode stores the SHA-256 of the one-time setup code (FR-ADM-06), as the deployment does it:
// connected as fyniq_migrator. It refuses once setup is done.
func SetSetupCode(ctx context.Context, migratorURL string, hash []byte) error {
	conn, err := pgx.Connect(ctx, migratorURL)
	if err != nil {
		return fmt.Errorf("connect as fyniq_migrator: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	return mapError(sqlcdb.New(conn).SetSetupCode(ctx, hash))
}

// ------------------------------------------------------------------ members

func (u *unitOfWork) Members() storage.Members { return members{u} }

type members struct{ u *unitOfWork }

func (r members) Add(ctx context.Context, userID uuid.UUID) (storage.Membership, error) {
	if err := r.u.check(); err != nil {
		return storage.Membership{}, err
	}
	row, err := r.u.q.InsertMembership(ctx, userID)
	if err != nil {
		return storage.Membership{}, mapError(err)
	}
	v := int(row.Version)
	if err := r.u.event(ctx, "user_membership.created", "user_membership", row.ID, &v, map[string][2]any{
		"user_id": {nil, row.UserID}, "status": {nil, row.Status}}); err != nil {
		return storage.Membership{}, err
	}
	return storage.Membership{ID: row.ID, UserID: row.UserID, Status: row.Status, Version: v}, nil
}

func (r members) Membership(ctx context.Context, userID uuid.UUID) (storage.Membership, error) {
	if err := r.u.check(); err != nil {
		return storage.Membership{}, err
	}
	row, err := r.u.q.GetMembershipByUser(ctx, userID)
	if err != nil {
		return storage.Membership{}, mapError(err)
	}
	return storage.Membership{ID: row.ID, UserID: row.UserID, Status: row.Status, Version: int(row.Version)}, nil
}

func (r members) Assign(ctx context.Context, membershipID uuid.UUID, role string, expires *time.Time) (storage.RoleAssignment, error) {
	if err := r.u.check(); err != nil {
		return storage.RoleAssignment{}, err
	}
	row, err := r.u.q.InsertRoleAssignment(ctx, sqlcdb.InsertRoleAssignmentParams{MembershipID: membershipID, Role: role, ExpiresAt: expires})
	if err != nil {
		return storage.RoleAssignment{}, mapError(err)
	}
	if err := r.u.event(ctx, "role_assignment.created", "role_assignment", row.ID, nil, map[string][2]any{
		"membership_id": {nil, row.MembershipID}, "role": {nil, row.Role}, "expires_at": {nil, row.ExpiresAt}}); err != nil {
		return storage.RoleAssignment{}, err
	}
	return storage.RoleAssignment{ID: row.ID, MembershipID: row.MembershipID, Role: row.Role, ExpiresAt: row.ExpiresAt}, nil
}

func (r members) Revoke(ctx context.Context, assignmentID uuid.UUID) error {
	if err := r.u.check(); err != nil {
		return err
	}
	row, err := r.u.q.RevokeRoleAssignment(ctx, assignmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "role_assignment.revoked", "role_assignment", row.ID, nil, map[string][2]any{
		"membership_id": {row.MembershipID, row.MembershipID}, "role": {row.Role, nil}})
}

func (r members) Roles(ctx context.Context, userID uuid.UUID, now time.Time) ([]storage.RoleAssignment, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListLiveRoles(ctx, sqlcdb.ListLiveRolesParams{UserID: userID, Now: &now})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.RoleAssignment, len(rows))
	for i, a := range rows {
		out[i] = storage.RoleAssignment{ID: a.ID, MembershipID: a.MembershipID, Role: a.Role, ExpiresAt: a.ExpiresAt, RevokedAt: a.RevokedAt}
	}
	return out, nil
}
