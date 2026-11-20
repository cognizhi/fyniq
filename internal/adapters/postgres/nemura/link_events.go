package nemura

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// ------------------------------------------------------------------ the people and bank-detail events to Fyniq (NM-INT-01, 05, 09; FR-PPL-06, 14)

// The event types Nemura sends (PRD-002 Appendix E).
const (
	TypePersonUpserted     = "people.person.upserted"
	TypeBankDetailsChanged = "people.bank_details.changed"
)

// PersonEvent is the payload of people.person.upserted, and the whole of it: what accounting needs of a person and nothing else. Never an NRIC or FIN, a date of birth, a salary, a bank account,
// a classification, a leaving reason, medical, disciplinary or review data (NM-INT-05). Fyniq refuses a field it does not list (PPL-09), so a field added here without the contract is refused
// there, and a test pins this list to the contract's.
type PersonEvent struct {
	PersonID            uuid.UUID  `json:"person_id"`
	DisplayName         string     `json:"display_name"`
	WorkEmail           string     `json:"work_email"`
	DepartmentID        *uuid.UUID `json:"department_id"`  // Fyniq's own dimension value, from what Fyniq told Nemura; null when Fyniq has no department by that name
	CostCentreID        *uuid.UUID `json:"cost_centre_id"` // likewise
	StartedOn           string     `json:"started_on"`
	LeftOn              string     `json:"left_on"`
	Status              string     `json:"status"`
	LineManagerPersonID *uuid.UUID `json:"line_manager_person_id"`
	Deleted             bool       `json:"deleted"`
}

// BankDetailsEvent is the payload of people.bank_details.changed: the person and the record that is now their salary account's, never the number (NM-INT-09). Its version is the event's object version.
type BankDetailsEvent struct {
	PersonID     uuid.UUID `json:"person_id"`
	BankDetailID uuid.UUID `json:"bank_detail_id"`
}

// fyniqEmail is the shape Fyniq's projection accepts for a work email; anything else is sent as no email rather than as a value its database refuses for ever.
var fyniqEmail = regexp.MustCompile(`^[^@\s]{1,100}@[^@\s]{1,200}$`)

// departmentOf finds Fyniq's department for the name a person's job gives, in the newest mapping version Fyniq told Nemura of: case and surrounding spaces do not matter. Departments come from
// Fyniq (NM-INT-01), so a name Fyniq does not have is no department, not a new one.
func departmentOf(ctx context.Context, tx pgx.Tx, name string) (dept, cost *uuid.UUID, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, nil
	}
	err = tx.QueryRow(ctx, `SELECT d.department_id, d.cost_centre_id FROM nemura.link_department d
WHERE d.mapping_version = (SELECT max(mapping_version) FROM nemura.link_mapping) AND lower(btrim(d.name)) = lower($1) ORDER BY d.department_id LIMIT 1`, name).Scan(&dept, &cost)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	return dept, cost, err
}

// PersonEventOf builds a person's payload from their record as it is now (their job on the day of sending), or a tombstone when the record is gone.
func PersonEventOf(ctx context.Context, tx pgx.Tx, person uuid.UUID, now time.Time) (PersonEvent, error) {
	p, err := PersonOn(ctx, tx, person, now)
	var ref *people.Refusal
	if errors.As(err, &ref) && ref.Code == people.CodeNotFound {
		return PersonEvent{PersonID: person, Deleted: true}, nil
	}
	if err != nil {
		return PersonEvent{}, err
	}
	ev := PersonEvent{PersonID: person, DisplayName: p.LegalName, StartedOn: day(p.Start), Status: "active", LineManagerPersonID: p.ManagerID}
	if p.PreferredName != "" {
		ev.DisplayName = p.PreferredName
	}
	if fyniqEmail.MatchString(p.WorkEmail) {
		ev.WorkEmail = p.WorkEmail
	}
	if p.LeavingDate != nil {
		ev.LeftOn = day(*p.LeavingDate)
		if !p.LeavingDate.After(now) {
			ev.Status = "ended"
		}
	}
	if p.Start.After(now) {
		ev.Status = "candidate"
	}
	if ev.DepartmentID, ev.CostCentreID, err = departmentOf(ctx, tx, p.Department); err != nil {
		return PersonEvent{}, err
	}
	return ev, nil
}

// QueuePersonEvent queues a person's record for Fyniq in the transaction that changed it, when the link is active. A person's events number themselves: one more each time the record changes, because
// the record is updated in place. The payload is the record as it is when the event is sent, so a late event of an older version carries the newest figures and Fyniq's rule that a version no newer
// than the one held is ignored loses nothing.
func QueuePersonEvent(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person uuid.UUID) (bool, error) {
	if !linkActive(ctx, tx) {
		return false, nil
	}
	var v int
	if err := tx.QueryRow(ctx, `INSERT INTO nemura.link_object_version (object_type, object_id, version) VALUES ('person', $1, 1)
ON CONFLICT (organisation_id, object_type, object_id) DO UPDATE SET version = nemura.link_object_version.version + 1 RETURNING version`, person.String()).Scan(&v); err != nil {
		return false, err
	}
	return QueueLinkEvent(ctx, tx, org, at, TypePersonUpserted, "person", person.String(), v)
}

// linkActive reports whether the organisation has an active link: with none, or one that is proposed, suspended or ending, nothing is queued.
func linkActive(ctx context.Context, tx pgx.Tx) bool {
	l, err := LiveLink(ctx, tx)
	return err == nil && l.State == "active"
}

// bankVersionOf is a person's bank-detail version: how many changes of their salary account have been confirmed. The details' record is the latest confirmed change, so the number never
// leaves Nemura and no column holds a second copy of what the changes already say (NM-INT-09).
func bankVersionOf(ctx context.Context, tx pgx.Tx, person uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.bank_account_change WHERE person_id = $1 AND state = 'confirmed'`, person).Scan(&n)
	return n, err
}

// QueueBankEvent queues the version of a person's bank details that was just confirmed, when the link is active. It carries the record's ID and the version, never the account (NM-INT-09).
func QueueBankEvent(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person uuid.UUID) (bool, error) {
	if !linkActive(ctx, tx) {
		return false, nil
	}
	v, err := bankVersionOf(ctx, tx, person)
	if err != nil || v == 0 {
		return false, err
	}
	return QueueLinkEvent(ctx, tx, org, at, TypeBankDetailsChanged, "bank_details", person.String(), v)
}

// bankEventAt reads the payload for a version of a person's bank details: the record of the confirmed change that made that version, and nothing of the account.
func bankEventAt(ctx context.Context, tx pgx.Tx, person uuid.UUID, version int) (BankDetailsEvent, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM nemura.bank_account_change WHERE person_id = $1 AND state = 'confirmed' ORDER BY confirmed_at, id OFFSET $2 LIMIT 1`, person, version-1).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return BankDetailsEvent{}, false, nil
	}
	return BankDetailsEvent{PersonID: person, BankDetailID: id}, err == nil, err
}

// SnapshotLinkEvents queues what Nemura already knows when the link becomes active, so a person, a bank-detail version or a hold that existed before the link reaches Fyniq: every person, every
// person's confirmed bank-detail version and every person's latest hold version. It runs in the transaction that activates the link.
func SnapshotLinkEvents(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time) (int, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM nemura.person ORDER BY employment_start, id`)
	if err != nil {
		return 0, err
	}
	var persons []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		persons = append(persons, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, p := range persons {
		if ok, err := QueuePersonEvent(ctx, tx, org, at, p); err != nil {
			return n, err
		} else if ok {
			n++
		}
		if ok, err := QueueBankEvent(ctx, tx, org, at, p); err != nil {
			return n, err
		} else if ok {
			n++
		}
		h, ok, err := TaxHoldOf(ctx, tx, p)
		if err != nil {
			return n, err
		}
		if ok {
			if sent, err := QueueLinkEvent(ctx, tx, org, at, "people.tax_hold.changed", "tax_hold", p.String(), h.Version); err != nil {
				return n, err
			} else if sent {
				n++
			}
		}
	}
	return n, nil
}
