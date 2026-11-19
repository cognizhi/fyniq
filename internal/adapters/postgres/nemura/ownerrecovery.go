package nemura

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
)

// IssueOwnerRecovery is the deployment's two-person recovery of an Owner, run with the migrator credential in url (NM-ARC-01; FR-ADM-09 as
// Fyniq does it): it issues a one-time code for the member, valid 24 hours, and records both operators' names in the organisation's audit trail
// in the same transaction. It refuses anything but two different names and a member who is not an active Owner. A new code replaces an unused
// one: only a member's newest code works. The code is returned once and never kept.
func IssueOwnerRecovery(ctx context.Context, url string, org, member uuid.UUID, operators []string, at time.Time) (code string, expires time.Time, err error) {
	if len(operators) != 2 {
		return "", time.Time{}, &identity.Refusal{Code: identity.CodeRecoveryOperators, Message: "A recovery needs exactly two different operators, by name."}
	}
	s, err := Open(ctx, url)
	if err != nil {
		return "", time.Time{}, err
	}
	defer s.Close()
	id := uuid.Must(uuid.NewV7())
	code, hash := identity.New(org)
	e, err := audit.New(org, at, audit.Actor{Type: "service", ID: "nemura-deployment-recovery"}, "nemura.owner.recovery_issued",
		audit.Object{Type: "owner_recovery", ID: id}, map[string]audit.Change{"member": {nil, member.String()}, "operators": {nil, operators}})
	if err != nil {
		return "", time.Time{}, err
	}
	e.Schema = AuditSchema
	e.Reason = "deployment recovery of an Owner"
	body, err := e.Canonical()
	if err != nil {
		return "", time.Time{}, err
	}
	err = s.pool.QueryRow(ctx, "SELECT nemura.issue_owner_recovery($1, $2, $3, $4, $5, $6, $7, $8)", org, id, member, hash, operators, e.EventID, string(body), at.Format(time.DateOnly)).Scan(&expires)
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.Code == "NM110":
		return "", time.Time{}, &identity.Refusal{Code: identity.CodeRecoveryOperators, Message: "A recovery needs exactly two different operators, by name."}
	case errors.As(err, &pg) && pg.Code == "NM111":
		return "", time.Time{}, &identity.Refusal{Code: identity.CodeRecoveryMember, Message: "That member is not an active Owner of that organisation."}
	case err != nil:
		return "", time.Time{}, fmt.Errorf("issue the recovery: %w", err)
	}
	return code, expires, nil
}

// RecoverOwner uses a recovery code with the account the person has just signed in with: that account becomes the member's only sign-in, every
// session of theirs ends and every passkey of theirs is revoked, each with its audit event, in one transaction (the code is used first, so a
// second use, an expired code, an older code or another organisation's code is the one refusal and changes nothing). The account may not already
// belong to another member of the organisation.
func (s *Store) RecoverOwner(ctx context.Context, code string, at time.Time, who identity.Identity) (org, member uuid.UUID, err error) {
	org, hash, err := identity.Parse(code)
	if err != nil || !who.Valid() {
		return uuid.Nil, uuid.Nil, identity.Refused()
	}
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		// Only the member's newest code, unused and not expired by the database's own clock.
		err := tx.QueryRow(ctx, `SELECT r.id, r.member_id FROM nemura.owner_recovery r
WHERE r.code_hash = $1 AND r.used_at IS NULL AND r.expires_at > now()
  AND r.id = (SELECT n.id FROM nemura.owner_recovery n WHERE n.member_id = r.member_id ORDER BY n.created_at DESC, n.id DESC LIMIT 1)
FOR UPDATE`, hash).Scan(&id, &member)
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.Refused()
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE nemura.owner_recovery SET used_at = now() WHERE id = $1", id); err != nil {
			return err
		}
		var replaced int
		if err := tx.QueryRow(ctx, "SELECT nemura.replace_member_identities($1, $2, $3, $4)", member, who.Issuer, who.Subject, who.Tenant).Scan(&replaced); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "NM112" {
				return identity.Refused() // another member's account: the code stays unused (the transaction rolls back)
			}
			return err
		}
		sessions, err := activeSessions(ctx, tx, member)
		if err != nil {
			return err
		}
		for _, sid := range sessions {
			if err := Revoke(ctx, tx, org, at, member, sid); err != nil {
				return err
			}
		}
		keys, err := tx.Query(ctx, "UPDATE nemura.passkey SET revoked_at = $2 WHERE member_id = $1 AND revoked_at IS NULL RETURNING id", member, at)
		if err != nil {
			return err
		}
		var passkeys []uuid.UUID
		for keys.Next() {
			var k uuid.UUID
			if err := keys.Scan(&k); err != nil {
				keys.Close()
				return err
			}
			passkeys = append(passkeys, k)
		}
		keys.Close()
		if err := keys.Err(); err != nil {
			return err
		}
		for _, k := range passkeys {
			if err := Audit(ctx, tx, org, at, user(member), "nemura.passkey.revoked", audit.Object{Type: "passkey", ID: k}, nil, "owner recovery"); err != nil {
				return err
			}
		}
		return Audit(ctx, tx, org, at, user(member), "nemura.owner.recovered", audit.Object{Type: "owner_recovery", ID: id},
			map[string]audit.Change{"identities_replaced": {nil, replaced}, "sessions_ended": {nil, len(sessions)}, "passkeys_revoked": {nil, len(passkeys)}}, "deployment recovery of an Owner")
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return org, member, nil
}

func activeSessions(ctx context.Context, tx pgx.Tx, member uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.session WHERE member_id = $1 AND revoked_at IS NULL", member)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
