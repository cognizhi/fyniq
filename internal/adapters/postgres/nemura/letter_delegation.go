package nemura

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/ports"
)

// DelegationAuthority is what an issued letter names as its authority when a delegation gave the right to sign: "delegation:<id>".
const DelegationAuthority = "delegation:"

// Delegation is a signatory's standing permission to issue one type of letter without waiting (NM-LTR-04).
type Delegation struct {
	ID, TemplateID uuid.UUID
	Type, Who      string
	Fields         []string
	MonthlyLimit   int
	IssuedMonth    int // letters issued under it this month
	CreatedOn      time.Time
	EndsOn         time.Time
	CreatedBy      uuid.UUID
	RevokedAt      *time.Time
	RevokedReason  string
	State          string // active, revoked, ended, or replaced (the template changed)
}

func delegationError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "NM169" {
		return &letters.Refusal{Code: letters.CodeConflict, Message: "A delegation keeps its terms and is revoked once: make a new one."}
	}
	return letterError(err)
}

// CreateDelegation records the signatory's delegation of a type: the template version published now, the people it covers, a monthly limit and an end date at most twelve months
// away. A type that shows pay is never delegated, and a type has one live delegation at a time. The caller has checked that the signatory confirmed with a fresh passkey.
func CreateDelegation(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, key string, monthly int, endsOn time.Time) (uuid.UUID, error) {
	t, ok := letters.TypeOf(key)
	if !ok {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter type."}
	}
	if t.Salary || t.Pay {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeInvalid, Message: "A letter that shows pay is never issued automatically: the people team checks it and the signatory signs each one."}
	}
	today := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	if monthly < 1 || monthly > 1000 {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeInvalid, Message: "The monthly limit is from 1 to 1,000 letters."}
	}
	if !endsOn.After(today) || endsOn.After(today.AddDate(0, 12, 0)) {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeInvalid, Message: "The delegation ends after today and at most 12 months from today: choose an end date in that range."}
	}
	var tid uuid.UUID
	var body []byte
	if err := tx.QueryRow(ctx, "SELECT id, blocks FROM nemura.letter_template_version WHERE type_key = $1 AND state = 'published'", key).Scan(&tid, &body); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, &letters.Refusal{Code: letters.CodeConflict, Message: "The " + strings.ToLower(t.Title) + " template has no published version yet: publish one first."}
		}
		return uuid.Nil, err
	}
	var enabled bool
	if err := tx.QueryRow(ctx, "SELECT coalesce((SELECT enabled FROM nemura.letter_type WHERE type_key = $1), false)", key).Scan(&enabled); err != nil {
		return uuid.Nil, err
	}
	if !enabled {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeConflict, Message: "The company does not issue that letter yet: switch the type on first."}
	}
	var live int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM nemura.letter_delegation WHERE type_key = $1 AND revoked_at IS NULL AND ends_on > $2", key, day(today)).Scan(&live); err != nil {
		return uuid.Nil, err
	}
	if live > 0 {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeConflict, Message: "That letter type has a delegation already: revoke it first, then make a new one."}
	}
	var blocks []letters.Block
	if err := json.Unmarshal(body, &blocks); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.letter_delegation (id, type_key, template_id, fields, monthly_limit, created_on, ends_on, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, key, tid, letters.FieldsUsed(blocks), monthly, day(today), day(endsOn), by, at); err != nil {
		return uuid.Nil, delegationError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.letter_delegation.created", audit.Object{Type: "letter_delegation", ID: id},
		map[string]audit.Change{"type": {nil, key}, "template": {nil, tid.String()}, "monthly_limit": {nil, monthly}, "ends_on": {nil, day(endsOn)}}, "")
}

// RevokeDelegation ends a delegation, once, from now: letters already issued under it stay as they are.
func RevokeDelegation(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 500 {
		return &letters.Refusal{Code: letters.CodeInvalid, Message: "The reason is up to 500 characters."}
	}
	tag, err := tx.Exec(ctx, "UPDATE nemura.letter_delegation SET revoked_at = $2, revoked_by = $3, revoked_reason = $4 WHERE id = $1 AND revoked_at IS NULL", id, at, by, orNil(reason))
	if err != nil {
		return delegationError(err)
	}
	if tag.RowsAffected() != 1 {
		return &letters.Refusal{Code: letters.CodeConflict, Message: "That delegation is revoked already, or there is no such delegation."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.letter_delegation.revoked", audit.Object{Type: "letter_delegation", ID: id}, map[string]audit.Change{"revoked": {false, true}}, reason)
}

// endDelegationsOf ends the live delegations of a type when its template changes: the signatory approved a version, not the next one (NM-LTR-04).
func endDelegationsOf(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, key string) error {
	rows, err := tx.Query(ctx, "UPDATE nemura.letter_delegation SET revoked_at = $2, revoked_by = $3, revoked_reason = 'The template changed: the signatory delegates again for the new version.' WHERE type_key = $1 AND revoked_at IS NULL RETURNING id", key, at, by)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := Audit(ctx, tx, org, at, user(by), "nemura.letter_delegation.ended", audit.Object{Type: "letter_delegation", ID: id}, map[string]audit.Change{"revoked": {false, true}}, "template changed"); err != nil {
			return err
		}
	}
	return nil
}

// Delegations lists the delegations with their state on a day and the letters issued under each this month, newest first.
func Delegations(ctx context.Context, tx pgx.Tx, at time.Time) ([]Delegation, error) {
	today := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	rows, err := tx.Query(ctx, `SELECT d.id, d.type_key, d.template_id, d.who, d.fields, d.monthly_limit, d.created_on, d.ends_on, d.created_by, d.revoked_at, coalesce(d.revoked_reason, ''),
  (SELECT count(*) FROM nemura.letter_issued i WHERE i.authority = $1 || d.id::text AND i.issued_at >= date_trunc('month', $2::timestamptz) AND i.issued_at < date_trunc('month', $2::timestamptz) + interval '1 month'),
  (SELECT v.state FROM nemura.letter_template_version v WHERE v.organisation_id = d.organisation_id AND v.id = d.template_id)
FROM nemura.letter_delegation d ORDER BY d.created_at DESC, d.id`, DelegationAuthority, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delegation
	for rows.Next() {
		var d Delegation
		var tstate string
		if err := rows.Scan(&d.ID, &d.Type, &d.TemplateID, &d.Who, &d.Fields, &d.MonthlyLimit, &d.CreatedOn, &d.EndsOn, &d.CreatedBy, &d.RevokedAt, &d.RevokedReason, &d.IssuedMonth, &tstate); err != nil {
			return nil, err
		}
		switch {
		case d.RevokedAt != nil && strings.HasPrefix(d.RevokedReason, "The template changed"):
			d.State = "replaced"
		case d.RevokedAt != nil:
			d.State = "revoked"
		case !d.EndsOn.After(today):
			d.State = "ended"
		default:
			d.State = "active"
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AutoIssue issues a request at once under a live delegation, when one covers it: the delegation is live, the request is filled from the template it approved, the person is an
// active employee not serving notice, the monthly limit is not reached, and the delegator is not the person (nobody checks or signs their own letter). The letter is checked
// and signed as the delegator, with no passkey, and its authority names the delegation. When any condition fails the request stays as it is, for the people team and the
// signatory to handle the ordinary way. It reports whether it issued.
func AutoIssue(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, request uuid.UUID, r letters.Renderer, blobs ports.BlobStore) (bool, error) {
	if r == nil || blobs == nil {
		return false, nil
	}
	var key string
	var tid, person uuid.UUID
	var start time.Time
	var leaving *time.Time
	var member *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT q.type_key, q.template_id, q.person_id, p.employment_start, p.leaving_date, p.member_id FROM nemura.letter_request q
JOIN nemura.person p ON p.organisation_id = q.organisation_id AND p.id = q.person_id WHERE q.id = $1 AND q.state = 'requested'`, request).Scan(&key, &tid, &person, &start, &leaving, &member)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	today := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	if leaving != nil || start.After(today) {
		return false, nil // serving notice, or not started yet
	}
	var del uuid.UUID
	var by uuid.UUID
	var limit int
	err = tx.QueryRow(ctx, `SELECT id, created_by, monthly_limit FROM nemura.letter_delegation WHERE type_key = $1 AND template_id = $2 AND revoked_at IS NULL AND ends_on > $3 ORDER BY created_at DESC LIMIT 1`,
		key, tid, day(today)).Scan(&del, &by, &limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if member != nil && *member == by {
		return false, nil
	}
	// The limit is counted under a lock on the delegation so that two requests cannot both take the last place.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "delegation:"+del.String()); err != nil {
		return false, err
	}
	var used int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.letter_issued WHERE authority = $1 AND issued_at >= date_trunc('month', $2::timestamptz) AND issued_at < date_trunc('month', $2::timestamptz) + interval '1 month'`,
		DelegationAuthority+del.String(), at).Scan(&used); err != nil {
		return false, err
	}
	if used >= limit {
		return false, nil
	}
	_, sum, err := CheckLetter(ctx, tx, org, at, by, request, r, blobs)
	if err != nil {
		var ref *letters.Refusal
		if errors.As(err, &ref) {
			return false, nil // it stays requested: the people team handles it the ordinary way
		}
		return false, err
	}
	if _, err := SignLetter(ctx, tx, org, at, by, request, sum, time.Time{}, DelegationAuthority+del.String(), blobs); err != nil {
		return false, err
	}
	return true, nil
}
