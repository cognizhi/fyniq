package nemura

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// ------------------------------------------------------------------ one sign-in: what each app tells the other of an identity (NM-INT-02, NM-INT-12; FR-PPL-02, FR-PPL-03)

// TypeIdentityAccess is the event each app sends the other when what an identity may enter changes (PRD-002 Appendix E). It is minimal and signed: an issuer, a subject (never an email) and two yes or no
// answers, no role name. It is a door and a hint, never a grant: the other app uses it to show or hide the Finance | People switch and to warn an approver of a missing passkey, and its own sign-in
// and roles decide what the person may do.
const TypeIdentityAccess = "identity.access.changed"

// AccessEvent is the payload of identity.access.changed from Nemura, and the whole of it. PeopleAccess says the identity is an active member of the organisation here; PayrollApprover that it holds a
// role that approves a pay run (the Owner's or the Payroll administrator's), so Fyniq can ask the person to enrol the passkey the approval is signed with. Its object version is the event's sequence.
type AccessEvent struct {
	Issuer          string `json:"issuer"`
	Subject         string `json:"subject"`
	PeopleAccess    bool   `json:"people_access"`
	PayrollApprover bool   `json:"payroll_approver"`
}

// QueueAccessEvents tells Fyniq what each sign-in identity of a member may enter here now, in the transaction of the change (an identity linked, a role granted or ended, a member deactivated), when
// the link is active; with no live link it does nothing. Nothing is queued about a person's pay, roles by name or email.
func QueueAccessEvents(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID) (int, error) {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		if noLink(err) {
			return 0, nil
		}
		return 0, err
	}
	if l.State != "active" {
		return 0, nil
	}
	var deactivated *time.Time
	if err := tx.QueryRow(ctx, `SELECT deactivated_at FROM nemura.member WHERE id = $1`, member).Scan(&deactivated); err != nil {
		return 0, err
	}
	roles, err := ActiveRoles(ctx, tx, member, at)
	if err != nil {
		return 0, err
	}
	approver := deactivated == nil && (slices.Contains(roles, authz.Owner) || slices.Contains(roles, authz.PayrollAdmin))
	rows, err := tx.Query(ctx, `SELECT issuer, subject FROM nemura.member_identities($1)`, member)
	if err != nil {
		return 0, err
	}
	var events []AccessEvent
	for rows.Next() {
		var ev AccessEvent
		if err := rows.Scan(&ev.Issuer, &ev.Subject); err != nil {
			rows.Close()
			return 0, err
		}
		ev.PeopleAccess, ev.PayrollApprover = deactivated == nil, approver
		events = append(events, ev)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, ev := range events {
		if sent, err := queueLinkEvent(ctx, tx, org, at, TypeIdentityAccess, "identity", member.String(), 0, ev); err != nil {
			return n, err
		} else if sent {
			n++
		}
	}
	return n, nil
}

// queueAccessOfAssignment tells Fyniq of the member a role assignment belongs to.
func queueAccessOfAssignment(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, assignment uuid.UUID) error {
	var member uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT member_id FROM nemura.role_assignment WHERE id = $1`, assignment).Scan(&member); err != nil {
		return err
	}
	_, err := QueueAccessEvents(ctx, tx, org, at, member)
	return err
}

// identityAccessEvent is what Fyniq says of an identity: whether it holds a role in Fyniq's finance workspace and whether it has a passkey enrolled there.
type identityAccessEvent struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	Finance bool   `json:"finance"`
	Passkey bool   `json:"passkey"`
}

// applyIdentityAccess keeps what Fyniq said of an identity when it is newer than what Nemura holds (the version is Fyniq's event sequence), and answers ignored_older when it is not. The audit event
// says which answers changed and never whose.
func applyIdentityAccess(ctx context.Context, tx pgx.Tx, org uuid.UUID, now time.Time, ev identityAccessEvent, version int) (string, error) {
	if ev.Issuer == "" || ev.Subject == "" || len(ev.Issuer) > 500 || len(ev.Subject) > 500 || version < 1 {
		return "", &payroll.Refusal{Code: payroll.CodeInvalid, Message: "An identity event names an issuer and a subject and carries a version of 1 or more."}
	}
	var kept int64
	err := tx.QueryRow(ctx, `INSERT INTO nemura.link_identity_access (issuer, subject, finance, passkey, version, received_at) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (organisation_id, issuer, subject) DO UPDATE SET finance = EXCLUDED.finance, passkey = EXCLUDED.passkey, version = EXCLUDED.version, received_at = EXCLUDED.received_at
WHERE nemura.link_identity_access.version < EXCLUDED.version RETURNING version`, ev.Issuer, ev.Subject, ev.Finance, ev.Passkey, version, now).Scan(&kept)
	if errors.Is(err, pgx.ErrNoRows) {
		return "ignored_older", nil
	}
	if err != nil {
		return "", err
	}
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return "", err
	}
	return "applied", Audit(ctx, tx, org, now, audit.Actor{Type: "service", ID: "link"}, "nemura.fyniq_link.identity_access", audit.Object{Type: "fyniq_link", ID: l.ID},
		map[string]audit.Change{"finance": {nil, ev.Finance}, "passkey": {nil, ev.Passkey}}, "")
}

// FyniqAccessOf is what Fyniq last said of an identity: whether it holds a role in the finance workspace and has a passkey enrolled, and whether Fyniq has said anything at all.
func FyniqAccessOf(ctx context.Context, tx pgx.Tx, issuer, subject string) (finance, passkey, known bool, err error) {
	err = tx.QueryRow(ctx, `SELECT finance, passkey FROM nemura.link_identity_access WHERE issuer = $1 AND subject = $2`, issuer, subject).Scan(&finance, &passkey)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, false, nil
	}
	return finance, passkey, err == nil, err
}

// SessionRevokedEvent is the payload of identity.session.revoked from Nemura, and the whole of it: the identity by issuer and subject, never an email, and why (signed_out or removed). Fyniq ends the person's
// sessions when they are mapped to a person of the link, and only then (the owner's decision of 21 Nov 2026: the one scope identity.sessions.write).
type SessionRevokedEvent struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
}

// QueueSessionRevoked tells Fyniq that a person was signed out of Nemura with the identity they signed in with, in the transaction that ended the session, when the link is active; nothing is queued with no
// active link. Fyniq's own revocation, which Nemura applies by ending sessions in one function, never comes back this way.
func QueueSessionRevoked(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, issuer, subject, reason string) error {
	_, err := queueLinkEvent(ctx, tx, org, at, TypeSessionRevoked, "identity", uuid.NewString(), 1, SessionRevokedEvent{Issuer: issuer, Subject: subject, Reason: reason})
	return err
}

// QueueMemberSessionsRevoked tells Fyniq, for each sign-in identity of a member, that they were removed from Nemura.
func QueueMemberSessionsRevoked(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID, reason string) error {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		if noLink(err) {
			return nil
		}
		return err
	}
	if l.State != "active" {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT issuer, subject FROM nemura.member_identities($1)`, member)
	if err != nil {
		return err
	}
	var ids []SessionRevokedEvent
	for rows.Next() {
		var ev SessionRevokedEvent
		if err := rows.Scan(&ev.Issuer, &ev.Subject); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, ev)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, ev := range ids {
		if err := QueueSessionRevoked(ctx, tx, org, at, ev.Issuer, ev.Subject, reason); err != nil {
			return err
		}
	}
	return nil
}
