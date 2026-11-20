package nemura

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/access"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/roles"
)

func user(member uuid.UUID) audit.Actor { return audit.Actor{Type: "user", ID: member.String()} }

// AddMember adds a person to the organisation, with its audit event, and returns their ID.
func AddMember(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by audit.Actor, displayName string) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.member (id, display_name) VALUES ($1, $2)", id, displayName); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, by, "nemura.member.joined", audit.Object{Type: "member", ID: id},
		map[string]audit.Change{"display_name": {nil, displayName}}, "")
}

// ActiveRoles are the roles a member holds on a day: granted from a day on or before it and not yet ended, and none for a member
// who has been deactivated.
func ActiveRoles(ctx context.Context, tx pgx.Tx, member uuid.UUID, on time.Time) ([]authz.Role, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT a.role FROM nemura.role_assignment a JOIN nemura.member m ON m.organisation_id = a.organisation_id AND m.id = a.member_id
WHERE a.member_id = $1 AND m.deactivated_at IS NULL AND a.effective_from <= $2 AND (a.effective_to IS NULL OR a.effective_to > $2)
-- a leaver holds no role: after their last day the account is for their own documents only (NM-ESS-06)
AND NOT EXISTS (SELECT 1 FROM nemura.person p WHERE p.organisation_id = a.organisation_id AND p.member_id = a.member_id AND p.leaving_date < $2::date) ORDER BY a.role`, member, on.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []authz.Role
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, authz.Role(r))
	}
	return out, rows.Err()
}

// SetupOwner makes the first Owner of an organisation, which first-run setup does and nothing else may: no granter, no approval.
func SetupOwner(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member uuid.UUID, from time.Time) error {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.role_assignment (id, member_id, role, effective_from) VALUES ($1, $2, 'owner', $3)",
		id, member, from.Format(time.DateOnly)); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "nemura-setup"}, "nemura.role.granted", audit.Object{Type: "role_assignment", ID: id},
		map[string]audit.Change{"member": {nil, member.String()}, "role": {nil, "owner"}, "effective_from": {nil, from.Format(time.DateOnly)}}, "first-run setup")
}

// RequestRoleGrant asks for a privileged role (pay or signing rights) for subject from a day: the request waits for an Owner
// who is neither the one asking nor the one who would receive it (NM-ROLE-05).
func RequestRoleGrant(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, requester, subject uuid.UUID, role authz.Role, from, expires time.Time) (uuid.UUID, error) {
	actor, err := ActiveRoles(ctx, tx, requester, at)
	if err != nil {
		return uuid.Nil, err
	}
	needs, err := roles.CheckGrant(actor, requester, subject, role)
	if err != nil {
		return uuid.Nil, err
	}
	if !needs {
		return uuid.Nil, fmt.Errorf("the %s role needs no approval: grant it", role)
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.approval_request (id, object_type, object_id, digest, requested_by, required_role, expires_at)
VALUES ($1, 'role_assignment', $2, $3, $4, 'owner', $5)`, id, subject, roles.Digest(subject, role, from), requester, expires); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(requester), "nemura.approval.requested", audit.Object{Type: "approval_request", ID: id},
		map[string]audit.Change{"object_type": {nil, "role_assignment"}, "member": {nil, subject.String()}, "role": {nil, string(role)}}, "")
}

// DecideRoleGrant approves or refuses a request. Only an Owner decides, and not their own request nor a role they would receive.
func DecideRoleGrant(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, approver, request uuid.UUID, approve bool, reason string) error {
	var requester, subject uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT requested_by, object_id FROM nemura.approval_request WHERE id = $1 AND state = 'pending'", request).Scan(&requester, &subject); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("there is no pending request with that ID")
		}
		return err
	}
	actor, err := ActiveRoles(ctx, tx, approver, at)
	if err != nil {
		return err
	}
	if err := roles.CheckDecision(actor, approver, requester, subject); err != nil {
		return err
	}
	state, action := "refused", "nemura.approval.refused"
	if approve {
		state, action = "approved", "nemura.approval.approved"
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.approval_request SET state = $2, decided_by = $3, decided_at = $4, reason = $5 WHERE id = $1",
		request, state, approver, at, nullable(reason)); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(approver), action, audit.Object{Type: "approval_request", ID: request},
		map[string]audit.Change{"state": {"pending", state}}, reason)
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GrantRole gives subject a role from a day. A privileged role stands only on an approved request (approval); the database
// refuses one without it, whoever asks.
func GrantRole(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, granter, subject uuid.UUID, role authz.Role, from time.Time, approval *uuid.UUID) (uuid.UUID, error) {
	actor, err := ActiveRoles(ctx, tx, granter, at)
	if err != nil {
		return uuid.Nil, err
	}
	needs, err := roles.CheckGrant(actor, granter, subject, role)
	if err != nil {
		return uuid.Nil, err
	}
	if needs && approval == nil {
		return uuid.Nil, &roles.Refusal{Code: roles.CodeApprovalRequired, Message: "Pay and signing rights need an Owner's approval first. Ask for the approval, then grant the role."}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.role_assignment (id, member_id, role, effective_from, granted_by, approval_id)
VALUES ($1, $2, $3, $4, $5, $6)`, id, subject, string(role), from.Format(time.DateOnly), granter, approval); err != nil {
		return uuid.Nil, err
	}
	if err := Audit(ctx, tx, org, at, user(granter), "nemura.role.granted", audit.Object{Type: "role_assignment", ID: id},
		map[string]audit.Change{"member": {nil, subject.String()}, "role": {nil, string(role)}, "effective_from": {nil, from.Format(time.DateOnly)}}, ""); err != nil {
		return uuid.Nil, err
	}
	_, err = QueueAccessEvents(ctx, tx, org, at, subject) // Fyniq is told whether the person now approves pay runs
	return id, err
}

// EndRole ends a grant on a day: history is kept, and nobody ends their own Owner role.
func EndRole(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, assignment uuid.UUID, on time.Time) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.role_assignment SET effective_to = $2 WHERE id = $1 AND effective_to IS NULL", assignment, on.Format(time.DateOnly))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no open role assignment with that ID")
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.role.revoked", audit.Object{Type: "role_assignment", ID: assignment},
		map[string]audit.Change{"effective_to": {nil, on.Format(time.DateOnly)}}, ""); err != nil {
		return err
	}
	return queueAccessOfAssignment(ctx, tx, org, at, assignment)
}

// RestrictedRefusal is the plain refusal of a restricted field the person may not see (NM_RESTRICTED_FIELD).
type RestrictedRefusal struct{ Capability string }

func (r *RestrictedRefusal) Error() string {
	return "NM_RESTRICTED_FIELD: " + r.Capability + " is not for your role. Ask the person who holds it, or the HR administrator."
}

// ReadRestricted records that actor read field of capability for subject at the level given, and returns it as the level allows
// (NM-PPL-02, NM-SEC-02): every read of a restricted field is audited, even one that is only masked, and one the person may not
// see is refused and also audited as a refusal: the caller commits that event (it recognises the refusal with errors.As, answers
// the person and returns nil), so a refusal is never lost to a rollback. The value never enters the event, only who, what and at
// which level.
func ReadRestricted(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor uuid.UUID, held []authz.Role, capability string, subject uuid.UUID, field, value string) (string, error) {
	level := access.Of(held, capability, actor == subject)
	action := "nemura.restricted.read"
	if level == access.Hidden {
		action = "nemura.restricted.refused"
	}
	if err := Audit(ctx, tx, org, at, user(actor), action, audit.Object{Type: "member", ID: subject},
		map[string]audit.Change{"capability": {nil, capability}, "field": {nil, field}, "level": {nil, level.String()}}, ""); err != nil {
		return "", err
	}
	shown, ok := access.Release(value, level)
	if !ok {
		return "", &RestrictedRefusal{Capability: capability}
	}
	return shown, nil
}

// AddReportingLine makes manager the line manager of report from a day (NM-ROLE-03), with its audit event; the people record
// (p1-nemura-records-packs) edits it through this.
func AddReportingLine(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, report, manager uuid.UUID, from time.Time) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.reporting_line (id, report_id, manager_id, effective_from) VALUES ($1, $2, $3, $4)",
		id, report, manager, from.Format(time.DateOnly)); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.reporting_line.set", audit.Object{Type: "reporting_line", ID: id},
		map[string]audit.Change{"report": {nil, report.String()}, "manager": {nil, manager.String()}, "effective_from": {nil, from.Format(time.DateOnly)}}, "")
}

// EndReportingLine ends a line on a day: history is kept.
func EndReportingLine(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, line uuid.UUID, on time.Time) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.reporting_line SET effective_to = $2 WHERE id = $1 AND effective_to IS NULL", line, on.Format(time.DateOnly))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("there is no open reporting line with that ID")
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.reporting_line.ended", audit.Object{Type: "reporting_line", ID: line},
		map[string]audit.Change{"effective_to": {nil, on.Format(time.DateOnly)}}, "")
}
