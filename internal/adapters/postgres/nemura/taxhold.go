package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// A tax-clearance hold (IRAS Form IR21; PRD-001 FR-PAY-10, PRD-002 NM-INT-08) is held while its latest version says so: every dollar due to the person in a pay run is held and none is disbursed. It is put on with
// a version of its own and released, with a document of the person as evidence, by the next. The versions are what Fyniq is told (`people.tax_hold.changed`).

func holdError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM177":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "There is a hold in force for that person already, or none to release: a hold is put on once and released once."}
		case "NM176":
			return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The evidence of a release is a document of the person: choose one of theirs."}
		case "NM127":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "A tax-clearance hold is history: a change is a new version."}
		}
	}
	return err
}

// TaxHoldRow is a person's latest hold version.
type TaxHoldRow struct {
	ID, HoldID uuid.UUID
	Version    int
	Held       bool
	From       time.Time
	Evidence   *uuid.UUID
	Reason     string
	RecordedAt time.Time
}

// TaxHoldOf returns the person's latest hold version, if they have had one.
func TaxHoldOf(ctx context.Context, tx pgx.Tx, person uuid.UUID) (TaxHoldRow, bool, error) {
	var r TaxHoldRow
	var status string
	var reason *string
	err := tx.QueryRow(ctx, "SELECT id, hold_id, version, status, effective_from, evidence_id, reason, recorded_at FROM nemura.tax_hold WHERE person_id = $1 ORDER BY recorded_at DESC, version DESC, id DESC LIMIT 1", person).
		Scan(&r.ID, &r.HoldID, &r.Version, &status, &r.From, &r.Evidence, &reason, &r.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if reason != nil {
		r.Reason = *reason
	}
	r.Held = status == "held"
	return r, err == nil, err
}

// PutTaxHold puts a hold on a person from a day. The audit event names the hold and its version and holds no amount.
func PutTaxHold(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, reason string) (uuid.UUID, error) {
	if len(reason) > 300 {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "reason: keep it to 300 characters."}
	}
	id := uuid.Must(uuid.NewV7())
	var rs *string
	if reason != "" {
		rs = &reason
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.tax_hold (id, hold_id, person_id, version, status, effective_from, reason, recorded_by, recorded_at) VALUES ($1, $1, $2, 1, 'held', $3, $4, $5, $6)`, id, person, day(from), rs, by, at); err != nil {
		return uuid.Nil, holdError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.tax_hold.put_on", audit.Object{Type: "tax_hold", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "version": {nil, 1}, "status": {nil, "held"}}, ""); err != nil {
		return uuid.Nil, err
	}
	// A linked organisation tells Fyniq, in the same transaction, so a hold is never lost between the two (NM-INT-08); none when standalone.
	_, err := QueueLinkEvent(ctx, tx, org, at, "people.tax_hold.changed", "tax_hold", person.String(), 1)
	return id, err
}

// ReleaseTaxHold releases the person's open hold from a day, with a document of the person as evidence (IRAS's clearance).
func ReleaseTaxHold(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person, evidence uuid.UUID, from time.Time, reason string) error {
	cur, ok, err := TaxHoldOf(ctx, tx, person)
	if err != nil {
		return err
	}
	if !ok || !cur.Held {
		return &payroll.Refusal{Code: payroll.CodeConflict, Message: "There is no hold in force for that person to release."}
	}
	id := uuid.Must(uuid.NewV7())
	var rs *string
	if reason != "" {
		rs = &reason
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.tax_hold (id, hold_id, person_id, version, status, effective_from, evidence_id, reason, recorded_by, recorded_at) VALUES ($1, $2, $3, $4, 'released', $5, $6, $7, $8, $9)`,
		id, cur.HoldID, person, cur.Version+1, day(from), evidence, rs, by, at); err != nil {
		return holdError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.tax_hold.released", audit.Object{Type: "tax_hold", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "hold": {nil, cur.HoldID.String()}, "version": {nil, cur.Version + 1}, "status": {nil, "released"}}, ""); err != nil {
		return err
	}
	_, err = QueueLinkEvent(ctx, tx, org, at, "people.tax_hold.changed", "tax_hold", person.String(), cur.Version+1)
	return err
}
