package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
)

// SetSetupCode sets the one-time first-run setup code's hash, as the migrator at deployment; a new code replaces an unused one and
// setup done refuses it.
func SetSetupCode(ctx context.Context, url string, hash []byte) error {
	s, err := Open(ctx, url)
	if err != nil {
		return err
	}
	defer s.Close()
	_, err = s.pool.Exec(ctx, "SELECT nemura.set_setup_code($1)", hash)
	return err
}

// SetupDone reports whether first-run setup has been done: the sign-in page shows the setup form until it is.
func (s *Store) SetupDone(ctx context.Context) (bool, error) {
	var done bool
	err := s.pool.QueryRow(ctx, "SELECT nemura.setup_done()").Scan(&done)
	return done, err
}

// FirstRun creates the first organisation, its first Owner and their identity, in one transaction that claims the setup code: it
// works once, with the right code, and leaves nothing behind when it fails (NM_SETUP_REFUSED).
func (s *Store) FirstRun(ctx context.Context, code []byte, at time.Time, organisation, displayName string, who identity.Identity) (org, owner uuid.UUID, err error) {
	if !who.Valid() {
		return uuid.Nil, uuid.Nil, errors.New("the identity needs an issuer, a subject and a tenant")
	}
	org = uuid.Must(uuid.NewV7())
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var claimed bool
		if err := tx.QueryRow(ctx, "SELECT nemura.claim_setup($1)", code).Scan(&claimed); err != nil {
			return err
		}
		if !claimed {
			return &identity.Refusal{Code: identity.CodeSetupRefused, Message: "The setup code is not valid, or setup is already done."}
		}
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.organisation (name) VALUES ($1)", organisation); err != nil {
			return err
		}
		by := audit.Actor{Type: "service", ID: "nemura-setup"}
		if err := Audit(ctx, tx, org, at, by, "nemura.org.created", audit.Object{Type: "organisation", ID: org},
			map[string]audit.Change{"name": {nil, organisation}}, "first-run setup"); err != nil {
			return err
		}
		var err error
		if owner, err = AddMember(ctx, tx, org, at, by, displayName); err != nil {
			return err
		}
		if err := SetupOwner(ctx, tx, org, at, owner, at); err != nil {
			return err
		}
		return LinkIdentity(ctx, tx, org, at, by, owner, who)
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return org, owner, nil
}

// LinkIdentity links a person's identity to a member of the organisation, with its audit event. The identity is never an email.
func LinkIdentity(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by audit.Actor, member uuid.UUID, who identity.Identity) error {
	if _, err := tx.Exec(ctx, "SELECT nemura.link_identity($1, $2, $3, $4)", who.Issuer, who.Subject, who.Tenant, member); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, by, "nemura.member.identity_linked", audit.Object{Type: "member", ID: member},
		map[string]audit.Change{"issuer": {nil, who.Issuer}, "tenant": {nil, who.Tenant}}, "")
}

// Membership is one organisation a person belongs to.
type Membership struct{ Organisation, Member uuid.UUID }

// MembershipsOf lists the organisations an identity belongs to, for sign-in before an organisation is chosen. A deactivated member
// is not listed.
func (s *Store) MembershipsOf(ctx context.Context, who identity.Identity) ([]Membership, error) {
	rows, err := s.pool.Query(ctx, "SELECT organisation_id, member_id FROM nemura.members_for_identity($1, $2, $3)", who.Issuer, who.Subject, who.Tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.Organisation, &m.Member); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Invite invites a member (already added) to sign in: it returns the code to give them, valid for a week, kept only as its hash.
func Invite(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, member uuid.UUID) (string, error) {
	code, hash := identity.New(org)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.invitation (id, member_id, code_hash, created_by, expires_at) VALUES ($1, $2, $3, $4, $5)",
		id, member, hash, by, at.Add(identity.InvitationTTL)); err != nil {
		return "", err
	}
	return code, Audit(ctx, tx, org, at, user(by), "nemura.member.invited", audit.Object{Type: "invitation", ID: id},
		map[string]audit.Change{"member": {nil, member.String()}}, "")
}

// AcceptInvitation lets whoever holds the code link their identity to the invited member, once and before it expires. Every failure
// is the same refusal, so it tells an attacker nothing.
func (s *Store) AcceptInvitation(ctx context.Context, code string, at time.Time, who identity.Identity) (org, member uuid.UUID, err error) {
	org, hash, err := identity.Parse(code)
	if err != nil || !who.Valid() {
		return uuid.Nil, uuid.Nil, identity.Refused()
	}
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, "SELECT id, member_id FROM nemura.invitation WHERE code_hash = $1 AND accepted_at IS NULL AND expires_at > $2", hash, at).Scan(&id, &member)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.Refused()
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE nemura.invitation SET accepted_at = $2 WHERE id = $1", id, at); err != nil {
			return err
		}
		if err := Audit(ctx, tx, org, at, audit.Actor{Type: "user", ID: member.String()}, "nemura.member.joined_by_invitation", audit.Object{Type: "invitation", ID: id}, nil, ""); err != nil {
			return err
		}
		return LinkIdentity(ctx, tx, org, at, audit.Actor{Type: "user", ID: member.String()}, member, who)
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return org, member, nil
}

// StartSession opens a session for a member of the organisation and returns its token, valid for 12 hours; only its hash is kept.
// A deactivated member gets none.
func StartSession(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID) (string, error) {
	var deactivated *time.Time
	if err := tx.QueryRow(ctx, "SELECT deactivated_at FROM nemura.member WHERE id = $1", member).Scan(&deactivated); err != nil {
		return "", identity.Refused()
	}
	if deactivated != nil {
		return "", identity.Refused()
	}
	token, hash := identity.New(org)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.session (id, member_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)", id, member, hash, at.Add(identity.SessionTTL)); err != nil {
		return "", err
	}
	return token, Audit(ctx, tx, org, at, user(member), "nemura.session.started", audit.Object{Type: "session", ID: id}, nil, "")
}

// Session is a valid session as a request sees it.
type Session struct {
	ID, Organisation, Member uuid.UUID
	StepUpAt                 *time.Time
}

// Authenticate finds the session a token opens on a moment: not expired, not revoked, and its member not deactivated. Every failure is
// the same refusal. Roles are read again on every request (ActiveRoles), so a change of roles takes effect on the next one.
func (s *Store) Authenticate(ctx context.Context, token string, now time.Time) (Session, error) {
	org, hash, err := identity.Parse(token)
	if err != nil {
		return Session{}, identity.Refused()
	}
	var out Session
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT s.id, s.organisation_id, s.member_id, s.step_up_at FROM nemura.session s JOIN nemura.member m ON m.organisation_id = s.organisation_id AND m.id = s.member_id
WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > $2 AND m.deactivated_at IS NULL`, hash, now).Scan(&out.ID, &out.Organisation, &out.Member, &out.StepUpAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.Refused()
		}
		return err
	})
	return out, err
}

// RecordStepUp records that the person confirmed the session with a passkey just now; an approval needs it to be fresh.
func RecordStepUp(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, session, member uuid.UUID) error {
	if _, err := tx.Exec(ctx, "UPDATE nemura.session SET step_up_at = $2 WHERE id = $1 AND revoked_at IS NULL", session, at); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(member), "nemura.session.stepped_up", audit.Object{Type: "session", ID: session}, nil, "")
}

// FreshStepUp reports whether the session was confirmed within the step-up window of now.
func (s Session) FreshStepUp(now time.Time) error {
	if s.StepUpAt == nil || now.Sub(*s.StepUpAt) > identity.StepUpWindow || s.StepUpAt.After(now) {
		return &identity.Refusal{Code: identity.CodeStepUp, Message: "Confirm it's you with your passkey, then try again."}
	}
	return nil
}

// Revoke ends a session (signing out), with its audit event.
func Revoke(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, session uuid.UUID) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.session SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL", session, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // already ended
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.session.ended", audit.Object{Type: "session", ID: session}, nil, "")
}

// DeactivateMember deactivates a member: they cannot sign in, every session of theirs ends, and their roles end today, in one transaction
// (NM-ROLE-05: every role change is audited). Their records stay (history is kept); nothing is deleted.
func DeactivateMember(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, member uuid.UUID, on time.Time) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.member SET deactivated_at = $2 WHERE id = $1 AND deactivated_at IS NULL", member, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no active member with that ID")
	}
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.session WHERE member_id = $1 AND revoked_at IS NULL", member)
	if err != nil {
		return err
	}
	var sessions []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		sessions = append(sessions, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range sessions {
		if err := Revoke(ctx, tx, org, at, by, id); err != nil {
			return err
		}
	}
	// Their roles end today (or the day after they begin, for one that has not begun).
	lines, err := tx.Query(ctx, "UPDATE nemura.role_assignment SET effective_to = greatest($2::date, effective_from + 1) WHERE member_id = $1 AND effective_to IS NULL RETURNING id, role",
		member, day(on))
	if err != nil {
		return err
	}
	type ended struct {
		id   uuid.UUID
		role string
	}
	var open []ended
	for lines.Next() {
		var e ended
		if err := lines.Scan(&e.id, &e.role); err != nil {
			lines.Close()
			return err
		}
		open = append(open, e)
	}
	lines.Close()
	if err := lines.Err(); err != nil {
		return err
	}
	for _, e := range open {
		if err := Audit(ctx, tx, org, at, user(by), "nemura.role.revoked", audit.Object{Type: "role_assignment", ID: e.id},
			map[string]audit.Change{"role": {e.role, nil}, "effective_to": {nil, day(on)}}, "member deactivated"); err != nil {
			return err
		}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.member.deactivated", audit.Object{Type: "member", ID: member},
		map[string]audit.Change{"deactivated_at": {nil, at.UTC().Format(time.RFC3339)}}, "")
}

// OpenSession starts a session for a member of the organisation in a transaction of its own and returns its token.
func (s *Store) OpenSession(ctx context.Context, org, member uuid.UUID, now time.Time) (string, error) {
	var token string
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		token, err = StartSession(ctx, tx, org, now, member)
		return err
	})
	return token, err
}

// CloseSession ends the session a token opens (signing out), whatever its expiry; a token that opens none is not an error.
func (s *Store) CloseSession(ctx context.Context, token string, now time.Time) error {
	org, hash, err := identity.Parse(token)
	if err != nil {
		return nil
	}
	return s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var id, member uuid.UUID
		err := tx.QueryRow(ctx, "SELECT id, member_id FROM nemura.session WHERE token_hash = $1 AND revoked_at IS NULL", hash).Scan(&id, &member)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return Revoke(ctx, tx, org, now, member, id)
	})
}

// RolesAt are the roles a member holds on a day, in a transaction of their own: what a request reads again each time.
func (s *Store) RolesAt(ctx context.Context, org, member uuid.UUID, on time.Time) ([]authz.Role, error) {
	var out []authz.Role
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = ActiveRoles(ctx, tx, member, on)
		return err
	})
	return out, err
}
