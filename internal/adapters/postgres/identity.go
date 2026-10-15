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

// SuperAdminFor implements storage.Installation.
func (s *Store) SuperAdminFor(ctx context.Context, issuer, subject string) (uuid.UUID, error) {
	id, err := s.q().SuperAdminForIdentity(ctx, sqlcdb.SuperAdminForIdentityParams{Issuer: issuer, Subject: subject})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	if id == uuid.Nil {
		return uuid.Nil, ports.ErrNotFound
	}
	return id, nil
}

// InstallationOrganisation implements storage.Installation.
func (s *Store) InstallationOrganisation(ctx context.Context) (uuid.UUID, error) {
	id, err := s.q().InstallationOrg(ctx)
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	if id == uuid.Nil {
		return uuid.Nil, ports.ErrNotFound
	}
	return id, nil
}

// CountSuperAdmins implements storage.Installation.
func (s *Store) CountSuperAdmins(ctx context.Context) (int, error) {
	n, err := s.q().CountSuperAdmins(ctx)
	return int(n), mapError(err)
}

// CompleteSetup implements storage.Installation: one transaction that consumes the code, creates the
// Super admin with their identity and recovery codes, the installation's trail and the first
// organisation with its Owner.
// It switches the tenant once per organisation; each organisation's audit events are written under its
// own tenant, and the new user is the actor of every one (AUD-01).
func (s *Store) CompleteSetup(ctx context.Context, n storage.NewSetup) (storage.Setup, error) {
	var out storage.Setup
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlcdb.Queries) error {
		uid, err := q.ClaimSetup(ctx, sqlcdb.ClaimSetupParams{CodeHash: n.CodeHash, DisplayName: n.DisplayName, Issuer: n.Issuer, Subject: n.Subject})
		if err != nil {
			return err
		}
		if err := q.SetRecoveryCodes(ctx, sqlcdb.SetRecoveryCodesParams{UserID: uid, CodeHashes: n.RecoveryCodeHashes, Now: n.Now}); err != nil {
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
			{"super_admin.added", "super_admin", uid, nil, map[string][2]any{"display_name": {nil, n.DisplayName}, "added_by": {nil, "first-run setup"},
				"recovery_codes": {nil, len(n.RecoveryCodeHashes)}}},
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

// IssueDeploymentRecovery issues the deployment's two-person recovery for a Super admin
// (docs/runbooks/super-admin-recovery.md), as the deployment does it: connected as fyniq_migrator. It
// stores the code's SHA-256 and writes its event, naming the two operators, to the installation's trail.
func IssueDeploymentRecovery(ctx context.Context, migratorURL string, userID uuid.UUID, codeHash []byte, operators string, now, expires time.Time) error {
	s, err := Open(ctx, migratorURL, WithActor(storage.Actor{Type: "deployment", ID: "fyniq_migrator"}))
	if err != nil {
		return err
	}
	defer s.Close()
	org, err := s.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return &storage.Error{Code: storage.CodeSetup, Message: "the installation has not been set up, so it has no Super admin to recover",
			Hint: "Complete first-run setup with the setup code instead"}
	}
	if err != nil {
		return err
	}
	return s.Do(ctx, org, func(ctx context.Context, uow storage.UnitOfWork) error {
		u := uow.(*unitOfWork)
		if err := u.q.IssueDeploymentRecovery(ctx, sqlcdb.IssueDeploymentRecoveryParams{UserID: userID, CodeHash: codeHash,
			Operators: operators, Now: now, ExpiresAt: expires}); err != nil {
			return mapError(err)
		}
		return u.event(ctx, "super_admin.deployment_recovery_issued", "super_admin", userID, nil, map[string][2]any{
			"operators": {nil, operators}, "expires_at": {nil, expires}})
	})
}

// ------------------------------------------------------------------ Super admins

func (u *unitOfWork) SuperAdmins() storage.SuperAdmins { return superAdmins{u} }

type superAdmins struct{ u *unitOfWork }

// installation checks the unit of work is the installation organisation's, whose trail every Super
// admin change writes to.
func (r superAdmins) installation(ctx context.Context) error {
	if err := r.u.check(); err != nil {
		return err
	}
	org, err := r.u.q.InstallationOrg(ctx)
	if err != nil {
		return mapError(err)
	}
	if org == uuid.Nil || org != r.u.org {
		return &storage.Error{Code: storage.CodeNoTenant, Message: "Super admin changes run in a unit of work of the installation organisation",
			Hint: "Use Installation.InstallationOrganisation for the unit of work"}
	}
	return nil
}

// as returns ctx with the audit actor set to the user: someone who just became, or recovered, a Super
// admin acts as themselves (AUD-01).
func as(ctx context.Context, userID uuid.UUID) context.Context {
	a := storage.AuditFrom(ctx)
	a.Actor, a.OnBehalfOf = &storage.Actor{Type: "user", ID: userID.String()}, nil
	return storage.WithAudit(ctx, a)
}

func (r superAdmins) Invite(ctx context.Context, by uuid.UUID, codeHash []byte, now, expires time.Time) error {
	if err := r.installation(ctx); err != nil {
		return err
	}
	if err := r.u.q.InviteSuperAdmin(ctx, sqlcdb.InviteSuperAdminParams{ByUser: by, CodeHash: codeHash, Now: now, ExpiresAt: expires}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "super_admin.invitation_created", "super_admin", by, nil, map[string][2]any{"expires_at": {nil, expires}})
}

func (r superAdmins) AcceptInvitation(ctx context.Context, a storage.AcceptInvitation) (uuid.UUID, error) {
	if err := r.installation(ctx); err != nil {
		return uuid.Nil, err
	}
	uid, err := r.u.q.AcceptSuperAdminInvitation(ctx, sqlcdb.AcceptSuperAdminInvitationParams{CodeHash: a.CodeHash,
		DisplayName: a.DisplayName, Issuer: a.Issuer, Subject: a.Subject, Now: a.Now})
	if err != nil {
		return uuid.Nil, mapError(err)
	}
	if err := r.u.q.SetRecoveryCodes(ctx, sqlcdb.SetRecoveryCodesParams{UserID: uid, CodeHashes: a.RecoveryCodeHashes, Now: a.Now}); err != nil {
		return uuid.Nil, mapError(err)
	}
	return uid, r.u.event(as(ctx, uid), "super_admin.added", "super_admin", uid, nil, map[string][2]any{
		"display_name": {nil, a.DisplayName}, "added_by": {nil, "invitation"}, "issuer": {nil, a.Issuer},
		"recovery_codes": {nil, len(a.RecoveryCodeHashes)}})
}

func (r superAdmins) SetRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes [][]byte, now time.Time) error {
	if err := r.installation(ctx); err != nil {
		return err
	}
	if err := r.u.q.SetRecoveryCodes(ctx, sqlcdb.SetRecoveryCodesParams{UserID: userID, CodeHashes: hashes, Now: now}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "super_admin.recovery_codes_replaced", "super_admin", userID, nil, map[string][2]any{
		"recovery_codes": {nil, len(hashes)}})
}

func (r superAdmins) ConfirmRecovery(ctx context.Context, by, of uuid.UUID, codeHash []byte, now, expires time.Time) error {
	if err := r.installation(ctx); err != nil {
		return err
	}
	if err := r.u.q.ConfirmSuperAdminRecovery(ctx, sqlcdb.ConfirmSuperAdminRecoveryParams{ByUser: by, ForUser: of, CodeHash: codeHash,
		Now: now, ExpiresAt: expires}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "super_admin.recovery_confirmed", "super_admin", of, nil, map[string][2]any{
		"confirmed_by": {nil, by}, "expires_at": {nil, expires}})
}

func (r superAdmins) Recover(ctx context.Context, rec storage.Recovery) (storage.Recovered, error) {
	if err := r.installation(ctx); err != nil {
		return storage.Recovered{}, err
	}
	row, err := r.u.q.RecoverSuperAdmin(ctx, sqlcdb.RecoverSuperAdminParams{RecoveryHash: rec.RecoveryCodeHash,
		ConfirmationHash: rec.ConfirmationHash, Issuer: rec.Issuer, Subject: rec.Subject, Now: rec.Now})
	if err != nil {
		return storage.Recovered{}, mapError(err)
	}
	if err := r.u.q.SetRecoveryCodes(ctx, sqlcdb.SetRecoveryCodesParams{UserID: row.UserID, CodeHashes: rec.RecoveryCodeHashes, Now: rec.Now}); err != nil {
		return storage.Recovered{}, mapError(err)
	}
	out := storage.Recovered{UserID: row.UserID, Via: row.Via, RemovedSubjects: row.RemovedSubjects}
	return out, r.u.event(as(ctx, row.UserID), "super_admin.recovered", "super_admin", row.UserID, nil, map[string][2]any{
		"via": {nil, row.Via}, "issuer": {nil, rec.Issuer}, "identities_replaced": {nil, len(row.RemovedSubjects)},
		"recovery_code_used": {nil, rec.RecoveryCodeHash != nil}, "recovery_codes": {nil, len(rec.RecoveryCodeHashes)}})
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
