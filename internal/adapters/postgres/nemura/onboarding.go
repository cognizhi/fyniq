package nemura

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func onboardingError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "NM170" {
		return &onboarding.Refusal{Code: onboarding.CodeConflict, Message: "That task is done or dismissed already: it is decided once."}
	}
	return err
}

// InviteToFyniq reports whether the company asks for a task to invite each new person to Fyniq.
func InviteToFyniq(ctx context.Context, tx pgx.Tx) (bool, error) {
	var on bool
	err := tx.QueryRow(ctx, "SELECT invite_to_fyniq FROM nemura.onboarding_settings").Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return on, err
}

// SetInviteToFyniq switches the invitation task on or off. Switching on does not raise tasks for people already added.
func SetInviteToFyniq(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, on bool) error {
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.onboarding_settings (invite_to_fyniq, updated_by, updated_at) VALUES ($1, $2, $3)
ON CONFLICT (organisation_id) DO UPDATE SET invite_to_fyniq = EXCLUDED.invite_to_fyniq, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`, on, by, at); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.onboarding.settings_changed", audit.Object{Type: "onboarding_settings", ID: org}, map[string]audit.Change{"invite_to_fyniq": {nil, on}}, "")
}

// RaiseJoinTask raises the task to invite a new person to Fyniq when the company asks for it; once for a person, and nothing when the setting is off (NM-ONB-02).
func RaiseJoinTask(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID) error {
	on, err := InviteToFyniq(ctx, tx)
	if err != nil || !on {
		return err
	}
	id := uuid.Must(uuid.NewV7())
	tag, err := tx.Exec(ctx, "INSERT INTO nemura.join_task (id, person_id, raised_at) VALUES ($1, $2, $3) ON CONFLICT (organisation_id, person_id, kind) DO NOTHING", id, person, at)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.join_task.raised", audit.Object{Type: "join_task", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "kind": {nil, "fyniq_invitation"}}, "")
}

// JoinTaskRow is a task with the person it is about.
type JoinTaskRow struct {
	ID, Person uuid.UUID
	PersonName string
	Start      time.Time
	State      string
	RaisedAt   time.Time
	Note       string
}

// JoinTasks lists the tasks, the open ones first, then by when they were raised.
func JoinTasks(ctx context.Context, tx pgx.Tx) ([]JoinTaskRow, error) {
	rows, err := tx.Query(ctx, `SELECT t.id, t.person_id, p.legal_name, p.employment_start, t.state, t.raised_at, coalesce(t.note, '') FROM nemura.join_task t
JOIN nemura.person p ON p.organisation_id = t.organisation_id AND p.id = t.person_id ORDER BY (t.state = 'open') DESC, t.raised_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JoinTaskRow
	for rows.Next() {
		var r JoinTaskRow
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Start, &r.State, &r.RaisedAt, &r.Note); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecideJoinTask marks a task done (the person was invited) or dismissed (they do not claim expenses), once.
func DecideJoinTask(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID, state, note string) error {
	if state != "done" && state != "dismissed" {
		return &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "A task is marked done or dismissed."}
	}
	note = strings.TrimSpace(note)
	if len(note) > 300 {
		return &onboarding.Refusal{Code: onboarding.CodeInvalid, Message: "The note is up to 300 characters."}
	}
	tag, err := tx.Exec(ctx, "UPDATE nemura.join_task SET state = $2, decided_by = $3, decided_at = $4, note = $5 WHERE id = $1 AND state = 'open'", id, state, by, at, orNil(note))
	if err != nil {
		return onboardingError(err)
	}
	if tag.RowsAffected() != 1 {
		return &onboarding.Refusal{Code: onboarding.CodeNotFound, Message: "There is no such task waiting."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.join_task."+state, audit.Object{Type: "join_task", ID: id}, map[string]audit.Change{"state": {"open", state}}, note)
}

// OffboardingRow is a person's offboarding as it is stored.
type OffboardingRow struct {
	ID, Person uuid.UUID
	PersonName string
	Start      time.Time
	onboarding.Leaving
	State string
}

func scanOffboarding(row pgx.Row) (OffboardingRow, error) {
	var r OffboardingRow
	err := row.Scan(&r.ID, &r.Person, &r.PersonName, &r.Start, &r.Kind, &r.NoticeGiven, &r.LastDay, &r.Retrenchment, &r.EquipmentReturned, &r.ExitInterview, &r.FyniqAccessRemoved, &r.State)
	return r, err
}

const offboardingColumns = `o.id, o.person_id, p.legal_name, p.employment_start, o.kind, o.notice_given_on, o.last_day, o.retrenchment_notified_on, o.equipment_returned_on, o.exit_interview_on,
 o.fyniq_access_removed_on, o.state`

// SetOffboarding records or changes a person's leaving: the dates that start statutory clocks kept apart and the checklist. It sets the person's leaving date and reason on the
// record, so leave is prorated to the last day and the account becomes a leaver's the day after (NM-ONB-03, NM-ESS-06). The last day is the one date that moves the record.
func SetOffboarding(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, l onboarding.Leaving) (uuid.UUID, error) {
	p, err := PersonOn(ctx, tx, person, at)
	if err != nil {
		return uuid.Nil, err
	}
	if err := onboarding.Check(l, p.Start); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	var old time.Time
	err = tx.QueryRow(ctx, "SELECT id, last_day FROM nemura.offboarding WHERE person_id = $1 AND state = 'active' FOR UPDATE", person).Scan(&id, &old)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		id = uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.offboarding (id, person_id, kind, notice_given_on, last_day, retrenchment_notified_on, equipment_returned_on, exit_interview_on, fyniq_access_removed_on, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)`, id, person, l.Kind, dayPtr(l.NoticeGiven), day(l.LastDay), dayPtr(l.Retrenchment), dayPtr(l.EquipmentReturned), dayPtr(l.ExitInterview), dayPtr(l.FyniqAccessRemoved), by, at); err != nil {
			return uuid.Nil, onboardingError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.offboarding.recorded", audit.Object{Type: "offboarding", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "kind": {nil, l.Kind}, "last_day": {nil, day(l.LastDay)}}, ""); err != nil {
			return uuid.Nil, err
		}
	case err != nil:
		return uuid.Nil, err
	default:
		if _, err := tx.Exec(ctx, `UPDATE nemura.offboarding SET kind = $2, notice_given_on = $3, last_day = $4, retrenchment_notified_on = $5, equipment_returned_on = $6, exit_interview_on = $7, fyniq_access_removed_on = $8, updated_at = $9 WHERE id = $1`,
			id, l.Kind, dayPtr(l.NoticeGiven), day(l.LastDay), dayPtr(l.Retrenchment), dayPtr(l.EquipmentReturned), dayPtr(l.ExitInterview), dayPtr(l.FyniqAccessRemoved), at); err != nil {
			return uuid.Nil, onboardingError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.offboarding.changed", audit.Object{Type: "offboarding", ID: id}, map[string]audit.Change{"last_day": {day(old), day(l.LastDay)}, "kind": {nil, l.Kind}}, ""); err != nil {
			return uuid.Nil, err
		}
	}
	ld := l.LastDay
	if err := UpdatePerson(ctx, tx, org, at, by, person, people.Person{LegalName: p.LegalName, PreferredName: p.PreferredName, WorkEmail: p.WorkEmail, Phone: p.Phone, EmploymentStart: p.Start,
		ProbationEnd: p.ProbationEnd, LeavingDate: &ld, LeavingReason: l.Kind}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// CancelOffboarding withdraws a leaving before its last day has passed (a resignation withdrawn): the person's leaving date is cleared and nothing else is undone.
func CancelOffboarding(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID) error {
	var id uuid.UUID
	var last time.Time
	err := tx.QueryRow(ctx, "SELECT id, last_day FROM nemura.offboarding WHERE person_id = $1 AND state = 'active' FOR UPDATE", person).Scan(&id, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return &onboarding.Refusal{Code: onboarding.CodeNotFound, Message: "There is no leaving recorded for that person."}
	}
	if err != nil {
		return err
	}
	if last.Before(time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)) {
		return &onboarding.Refusal{Code: onboarding.CodeConflict, Message: "The last day has passed: the person has left. Ask the Owner to correct the record if it was wrong."}
	}
	p, err := PersonOn(ctx, tx, person, at)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.offboarding SET state = 'cancelled', updated_at = $2 WHERE id = $1", id, at); err != nil {
		return err
	}
	if err := UpdatePerson(ctx, tx, org, at, by, person, people.Person{LegalName: p.LegalName, PreferredName: p.PreferredName, WorkEmail: p.WorkEmail, Phone: p.Phone, EmploymentStart: p.Start, ProbationEnd: p.ProbationEnd}); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.offboarding.cancelled", audit.Object{Type: "offboarding", ID: id}, map[string]audit.Change{"state": {"active", "cancelled"}}, "")
}

// Offboardings lists the active leavings, the soonest last day first.
func Offboardings(ctx context.Context, tx pgx.Tx) ([]OffboardingRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+offboardingColumns+" FROM nemura.offboarding o JOIN nemura.person p ON p.organisation_id = o.organisation_id AND p.id = o.person_id WHERE o.state = 'active' ORDER BY o.last_day, p.legal_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OffboardingRow
	for rows.Next() {
		r, err := scanOffboarding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// OffboardingOf is a person's active leaving, or false.
func OffboardingOf(ctx context.Context, tx pgx.Tx, person uuid.UUID) (OffboardingRow, bool, error) {
	r, err := scanOffboarding(tx.QueryRow(ctx, "SELECT "+offboardingColumns+" FROM nemura.offboarding o JOIN nemura.person p ON p.organisation_id = o.organisation_id AND p.id = o.person_id WHERE o.person_id = $1 AND o.state = 'active'", person))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

// Settlement is the annual leave a leaver is left with on their last day: the entitlement prorated to the last day (the pack's and the company's rules), what they took, what is
// still booked and the difference. What it is worth in money comes from payroll.
type Settlement struct {
	Year                             int
	Entitled, Taken, Booked, Untaken leave.Days
	Basis                            []string
}

// LeaveSettlement works out a leaver's annual leave to their last day without writing anything. The days untaken are negative when the person took more than the prorated
// entitlement; what to do about that is for the people team and the contract.
func LeaveSettlement(ctx context.Context, tx pgx.Tx, person uuid.UUID, last, at time.Time, v pack.Version, policyDays []int) (Settlement, error) {
	var s Settlement
	s.Year = last.Year()
	e, err := AnnualEntitlement(ctx, tx, person, s.Year, at, v, policyDays)
	if err != nil {
		return s, err
	}
	b, err := LeaveBalance(ctx, tx, person, AnnualLeave, s.Year)
	if err != nil {
		return s, err
	}
	s.Entitled, s.Taken, s.Booked, s.Basis = e.Days, b.Taken, b.Booked, e.Basis
	s.Untaken = s.Entitled - s.Taken
	return s, nil
}
