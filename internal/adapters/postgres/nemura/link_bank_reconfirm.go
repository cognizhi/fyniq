package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
)

// After a restore of Nemura's database, Fyniq may hold a bank-detail version Nemura lost (PRD-002 NM-INT-13; PRD-001 FR-PPL-19). A version is the ordinal of a person's confirmed changes, so a
// restored Nemura numbers the next confirmed change with an ordinal Fyniq already holds for another record, and Fyniq ignores it. Fyniq answers an ignored bank-details event with the version and the
// record it holds; NoteBankReceipt compares them with Nemura's own record at that version. The record itself never leaves Nemura: only an ID and a number are compared.

// NoteBankReceipt is called with Fyniq's answer to a bank-details event that it ignored as no newer than what it holds: the version and the record Fyniq holds. If Nemura's own record at that version is
// the one Fyniq holds, the event was a repeat or a late older one and nothing is wrong. If it is not, and Nemura's version is no higher than Fyniq's (so Fyniq will not take what Nemura has),
// the restore lost a change: the person's version floor is raised so the next confirmed change is numbered above Fyniq's, and the person is recorded for the people team to re-confirm. It reports whether it recorded one.
func NoteBankReceipt(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person uuid.UUID, heldVersion int, heldRecord uuid.UUID) (bool, error) {
	if heldVersion < 1 || heldRecord == uuid.Nil {
		return false, nil
	}
	own, ok, err := bankChangeAt(ctx, tx, person, heldVersion)
	if err != nil {
		return false, err
	}
	if ok && own.ID == heldRecord {
		return false, nil
	}
	current, err := bankVersionOf(ctx, tx, person)
	if err != nil {
		return false, err
	}
	if current > heldVersion {
		return false, nil // Fyniq takes Nemura's newer version when it is sent: that corrects it
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.bank_version_floor (person_id, floor) VALUES ($1, $2)
ON CONFLICT (organisation_id, person_id) DO UPDATE SET floor = EXCLUDED.floor, updated_at = $3 WHERE nemura.bank_version_floor.floor < EXCLUDED.floor`, person, heldVersion, at); err != nil {
		return false, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO nemura.bank_reconfirm (person_id, held_version, held_record_id, nemura_version, found_at) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (organisation_id, person_id) WHERE state = 'open' DO NOTHING RETURNING id`, person, heldVersion, heldRecord, current, at).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // already waiting for the people team
	}
	if err != nil {
		return false, err
	}
	return true, Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "link"}, "nemura.bank_reconfirm.found", audit.Object{Type: "bank_reconfirm", ID: id},
		map[string]audit.Change{"person_id": {nil, person.String()}, "held_version": {nil, heldVersion}, "nemura_version": {nil, current}}, "Fyniq holds a bank-detail version this database does not: re-confirm the person's bank details")
}

// ResolveBankReconfirm ends a person's open re-confirmation when a change of their bank details is confirmed with a version above the one Fyniq held: it is the ordinary change (proposed, approved by
// a second person, confirmed by the employee's passkey) that resolves it, never a person's word. It runs in the transaction that confirms the change.
func ResolveBankReconfirm(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person, change uuid.UUID) error {
	version, err := bankVersionOf(ctx, tx, person)
	if err != nil {
		return err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `UPDATE nemura.bank_reconfirm SET state = 'resolved', resolved_at = $2, resolved_change_id = $3 WHERE person_id = $1 AND state = 'open' AND held_version < $4 RETURNING id`, person, at, change, version).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.bank_reconfirm.resolved", audit.Object{Type: "bank_reconfirm", ID: id},
		map[string]audit.Change{"person_id": {nil, person.String()}, "change_id": {nil, change.String()}, "version": {nil, version}}, "")
}

// BankReconfirmRow is a person whose bank details wait for the people team, by identity: never the account.
type BankReconfirmRow struct {
	ID            uuid.UUID
	Person        uuid.UUID
	HeldVersion   int
	NemuraVersion int
	FoundAt       time.Time
}

// OpenBankReconfirms lists the people whose bank details wait to be re-confirmed after a restore, oldest first.
func OpenBankReconfirms(ctx context.Context, tx pgx.Tx) ([]BankReconfirmRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, person_id, held_version, nemura_version, found_at FROM nemura.bank_reconfirm WHERE state = 'open' ORDER BY found_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BankReconfirmRow
	for rows.Next() {
		var r BankReconfirmRow
		if err := rows.Scan(&r.ID, &r.Person, &r.HeldVersion, &r.NemuraVersion, &r.FoundAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// bankNeedsReconfirm reports whether a person's bank details wait for the people team: no payment line of theirs is made, by file or by Fyniq, until the change that re-confirms them.
func bankNeedsReconfirm(ctx context.Context, tx pgx.Tx, person uuid.UUID) (bool, error) {
	var open bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.bank_reconfirm WHERE person_id = $1 AND state = 'open')`, person).Scan(&open)
	return open, err
}
