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
	TypeLeaveTaken         = "leave.taken.changed"
)

// LeaveTakenEvent is the payload of leave.taken.changed, and the whole of it: the entry, the person, the dates, the half days and the days, and a tombstone when the leave was cancelled. Never the leave's type, a
// reason, a note or a balance (Appendix E; design D27). Fyniq refuses a field it does not list, so a test pins this list to Fyniq's. Appendix E says hours; Nemura counts leave in days, so it sends days
// [owner to confirm].
type LeaveTakenEvent struct {
	EntryID   uuid.UUID `json:"entry_id"`
	PersonID  uuid.UUID `json:"person_id"`
	StartDate string    `json:"start_date"`
	EndDate   string    `json:"end_date"`
	StartHalf bool      `json:"start_half"`
	EndHalf   bool      `json:"end_half"`
	Days      string    `json:"days"`
	Cancelled bool      `json:"cancelled"`
}

// QueueLeaveEvent queues the state a leave request is in now, when the link is active: approved, or cancelled after it was approved (a tombstone). Its version is the entry's own counter, which goes up with each
// word, so Fyniq keeps the newest. Called in the transaction of the decision or the cancellation; nothing is queued standalone.
func QueueLeaveEvent(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, request uuid.UUID) (bool, error) {
	if !linkActive(ctx, tx) {
		return false, nil
	}
	var v int
	if err := tx.QueryRow(ctx, `INSERT INTO nemura.link_object_version (object_type, object_id, version) VALUES ('leave_request', $1, 1)
ON CONFLICT (organisation_id, object_type, object_id) DO UPDATE SET version = nemura.link_object_version.version + 1 RETURNING version`, request.String()).Scan(&v); err != nil {
		return false, err
	}
	return QueueLinkEvent(ctx, tx, org, at, TypeLeaveTaken, "leave_request", request.String(), v)
}

// LeaveEventOf is leaveEventAt for the people who write the contract's test: the payload a leave request would send, and whether it is one Fyniq is told of.
func LeaveEventOf(ctx context.Context, tx pgx.Tx, request uuid.UUID) (LeaveTakenEvent, bool, error) {
	return leaveEventAt(ctx, tx, request)
}

// leaveEventAt reads the payload from the leave request and nothing else: false when the request is not one Fyniq is told of (pending or refused).
func leaveEventAt(ctx context.Context, tx pgx.Tx, request uuid.UUID) (LeaveTakenEvent, bool, error) {
	var ev LeaveTakenEvent
	var from, to time.Time
	var state string
	err := tx.QueryRow(ctx, `SELECT person_id, start_date, end_date, start_half, end_half, days::text, state FROM nemura.leave_request WHERE id = $1`, request).
		Scan(&ev.PersonID, &from, &to, &ev.StartHalf, &ev.EndHalf, &ev.Days, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ev, false, nil
	}
	if err != nil || (state != "approved" && state != "cancelled") {
		return ev, false, err
	}
	ev.EntryID, ev.StartDate, ev.EndDate, ev.Cancelled = request, from.Format(time.DateOnly), to.Format(time.DateOnly), state == "cancelled"
	return ev, true, nil
}

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
	var erased bool
	if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT erased_at IS NOT NULL FROM nemura.person WHERE id = $1), false)`, person).Scan(&erased); err != nil {
		return PersonEvent{}, err
	}
	if erased { // an erased person is a tombstone to Fyniq, which clears the name and email it held (NM-SEC-08)
		return PersonEvent{PersonID: person, Deleted: true}, nil
	}
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

// confirmedBankVersions is each confirmed change of a person's salary account with the version it made: the version it was confirmed with, or for one confirmed before versions were kept, its ordinal
// among the confirmed changes (those come first, so their ordinals are what they always were).
const confirmedBankVersions = `SELECT id, new_fingerprint, new_bank_code, coalesce(bank_version, row_number() OVER (ORDER BY confirmed_at, id)::int) AS v
FROM nemura.bank_account_change WHERE person_id = $1 AND state = 'confirmed'`

// bankVersionOf is a person's bank-detail version: the version of their latest confirmed change of salary account (0 with none). The details' record is that change, so the number never
// leaves Nemura and no column holds a second copy of what the changes already say (NM-INT-09).
func bankVersionOf(ctx context.Context, tx pgx.Tx, person uuid.UUID) (int, error) {
	var v int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(v), 0) FROM (`+confirmedBankVersions+`) c`, person).Scan(&v)
	return v, err
}

// nextBankVersion is the version the next confirmed change of a person's account takes: above the one they have and above the floor a restore put on it (bank_version_floor, which only rises), so it
// is numbered above what Fyniq holds (NM-INT-13).
func nextBankVersion(ctx context.Context, tx pgx.Tx, person uuid.UUID) (int, error) {
	var v int
	err := tx.QueryRow(ctx, `SELECT greatest((SELECT coalesce(max(v), 0) FROM (`+confirmedBankVersions+`) c), coalesce((SELECT floor FROM nemura.bank_version_floor WHERE person_id = $1), 0)) + 1`, person).Scan(&v)
	return v, err
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

// bankChange is a confirmed change of a person's salary account, as the version it made is read: the record, its fingerprint and the bank code it approved.
type bankChange struct {
	ID          uuid.UUID
	Fingerprint []byte
	Code        *string
}

// bankChangeAt reads the confirmed change that made a version of a person's bank details. A version a restore skipped, or beyond the changes there are, has none.
func bankChangeAt(ctx context.Context, tx pgx.Tx, person uuid.UUID, version int) (bankChange, bool, error) {
	var c bankChange
	err := tx.QueryRow(ctx, `SELECT id, new_fingerprint, new_bank_code FROM (`+confirmedBankVersions+`) c WHERE v = $2`, person, version).Scan(&c.ID, &c.Fingerprint, &c.Code)
	if errors.Is(err, pgx.ErrNoRows) {
		return bankChange{}, false, nil
	}
	return c, err == nil, err
}

// bankEventAt reads the payload for a version of a person's bank details: the record of the confirmed change that made that version, and nothing of the account.
func bankEventAt(ctx context.Context, tx pgx.Tx, person uuid.UUID, version int) (BankDetailsEvent, bool, error) {
	c, ok, err := bankChangeAt(ctx, tx, person, version)
	if !ok || err != nil {
		return BankDetailsEvent{}, false, err
	}
	return BankDetailsEvent{PersonID: person, BankDetailID: c.ID}, true, nil
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
	members, err := tx.Query(ctx, `SELECT id FROM nemura.member ORDER BY created_at, id`)
	if err != nil {
		return 0, err
	}
	var ids []uuid.UUID
	for members.Next() {
		var id uuid.UUID
		if err := members.Scan(&id); err != nil {
			members.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	members.Close()
	if err := members.Err(); err != nil {
		return 0, err
	}
	for _, m := range ids { // what each identity may enter, so Fyniq's switch and its passkey prompt are right from the first day
		k, err := QueueAccessEvents(ctx, tx, org, at, m)
		if err != nil {
			return n, err
		}
		n += k
	}
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
	// Approved leave that has not ended, so who is away reaches Fyniq from the first day (design D27).
	lrows, err := tx.Query(ctx, `SELECT id FROM nemura.leave_request WHERE state = 'approved' AND end_date >= $1 ORDER BY start_date, id`, at.Format(time.DateOnly))
	if err != nil {
		return n, err
	}
	var requests []uuid.UUID
	for lrows.Next() {
		var id uuid.UUID
		if err := lrows.Scan(&id); err != nil {
			lrows.Close()
			return n, err
		}
		requests = append(requests, id)
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return n, err
	}
	for _, r := range requests {
		if ok, err := QueueLeaveEvent(ctx, tx, org, at, r); err != nil {
			return n, err
		} else if ok {
			n++
		}
	}
	return n, nil
}

// requeueLeave queues the leave entries of a person again at the version each has now, for repair and replay: no version moves, so Fyniq applies what it missed and ignores what it holds.
func requeueLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person, request uuid.UUID) (int, error) {
	if !linkActive(ctx, tx) {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT l.id::text, v.version FROM nemura.leave_request l JOIN nemura.link_object_version v ON v.object_type = 'leave_request' AND v.object_id = l.id::text
WHERE ($1::uuid = '00000000-0000-0000-0000-000000000000' OR l.person_id = $1) AND ($2::uuid = '00000000-0000-0000-0000-000000000000' OR l.id = $2) AND l.state IN ('approved', 'cancelled')`, person, request)
	if err != nil {
		return 0, err
	}
	type entry struct {
		id      string
		version int
	}
	var all []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.version); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, e := range all {
		if ok, err := QueueLinkEvent(ctx, tx, org, at, TypeLeaveTaken, "leave_request", e.id, e.version); err != nil {
			return n, err
		} else if ok {
			n++
		}
	}
	return n, nil
}
