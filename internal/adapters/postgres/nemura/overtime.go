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
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
)

// OvertimeAsk is a day's overtime, rest-day work or public-holiday work for a Part IV employee, entered by hand and without clocking (NM-TIM-04).
type OvertimeAsk struct {
	Person uuid.UUID
	Date   time.Time
	Kind   string // overtime, rest_day or public_holiday
	Hours  leave.Days
	Branch string // for rest-day work: employer_request or employee_request
	Note   string
}

func overtimeError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.Code == "NM154":
			return &leave.Refusal{Code: leave.CodeRole, Message: "Nobody decides their own overtime: another approver does."}
		case pg.Code == "NM153":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "An overtime record is evidence: it is decided once, used once, and never deleted."}
		case pg.Code == "23505" && pg.ConstraintName == "overtime_entry_once":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "That day already has a record of that kind for the person: correct it by refusing it and entering the right hours."}
		case pg.Code == "NM155":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "A pay input is evidence: it is written once and used once."}
		case pg.Code == "23505" && pg.ConstraintName == "pay_input_once_per_source":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "That is an input of the pay run already."}
		}
	}
	return leaveError(err)
}

// RecordOvertime records a day's hours for a Part IV employee (NM-TIM-02, NM-TIM-04). The person must be an employee covered by the Employment Act whose Part IV class is
// workman or non-workman on the day; the basic-salary thresholds are the pay run's to check, as salary is not held here. Overtime beyond the pack's monthly limit stays
// recorded, is flagged, and is never refused: the limit is for HR to act on (an exemption or a change of pattern). It starts pending.
func RecordOvertime(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, v pack.Version, a OvertimeAsk) (uuid.UUID, bool, error) {
	switch a.Kind {
	case "overtime", "rest_day", "public_holiday":
	default:
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "The kind is overtime, rest_day or public_holiday: choose one."}
	}
	if a.Hours <= 0 || a.Hours > leave.Whole(24) || a.Hours%25 != 0 {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "The hours are more than 0 and at most 24, in steps of a quarter hour: write them as 2.5."}
	}
	if (a.Kind == "rest_day") != (a.Branch != "") || (a.Branch != "" && a.Branch != "employer_request" && a.Branch != "employee_request") {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "Rest-day work says at whose request it was, employer_request or employee_request, and other work says nothing: correct the branch."}
	}
	c, ok, err := ClassificationOn(ctx, tx, a.Person, a.Date)
	if err != nil {
		return uuid.Nil, false, err
	}
	if !ok || !c.EACovered || (c.PartIVClass != "workman" && c.PartIVClass != "non_workman") {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "The person is not a Part IV employee on that day: Part IV's overtime rules cover an Employment Act employee whose class is workman or non-workman, as recorded on their legal classification."}
	}
	var start time.Time
	var left *time.Time
	if err := tx.QueryRow(ctx, "SELECT employment_start, leaving_date FROM nemura.person WHERE id = $1", a.Person).Scan(&start, &left); err != nil {
		return uuid.Nil, false, err
	}
	if a.Date.Before(start) || (left != nil && a.Date.After(*left)) {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "That day is outside the person's employment."}
	}
	over := false
	if a.Kind == "overtime" {
		limit, ok := pack.PartIV(v, "overtime_hours_per_month", a.Date)
		if !ok {
			return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "The country pack has no overtime limit for that day."}
		}
		first := time.Date(a.Date.Year(), a.Date.Month(), 1, 0, 0, 0, 0, time.UTC)
		var sofar string
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(hours), 0)::numeric(10,2)::text FROM nemura.overtime_entry WHERE person_id = $1 AND kind = 'overtime' AND state <> 'refused' AND work_date >= $2 AND work_date < $3`,
			a.Person, day(first), day(first.AddDate(0, 1, 0))).Scan(&sofar); err != nil {
			return uuid.Nil, false, err
		}
		used, err := leave.ParseDays(sofar)
		if err != nil {
			return uuid.Nil, false, err
		}
		over = used+a.Hours > leave.Whole(limit)
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.overtime_entry (id, person_id, work_date, kind, hours, branch, over_limit, note, requested_by, requested_at)
VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8, $9, $10)`, id, a.Person, day(a.Date), a.Kind, a.Hours.String(), orNil(a.Branch), over, orNil(a.Note), by, at); err != nil {
		return uuid.Nil, false, overtimeError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.overtime.recorded", audit.Object{Type: "overtime_entry", ID: id}, map[string]audit.Change{"person": {nil, a.Person.String()},
		"date": {nil, day(a.Date)}, "kind": {nil, a.Kind}, "hours": {nil, a.Hours.String()}, "over_limit": {nil, over}}, ""); err != nil {
		return uuid.Nil, false, err
	}
	return id, over, notice(ctx, tx, "overtime_recorded", a.Person, id, "overtime-recorded")
}

// DecideOvertime approves or refuses a pending record: by someone other than the person (the database refuses their own). A refusal needs a reason.
func DecideOvertime(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, entry uuid.UUID, approve bool, reason string) error {
	state := "approved"
	if !approve {
		state = "refused"
		if len(reason) == 0 || len(reason) > 500 {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "A refusal needs a reason of up to 500 characters: tell the person why."}
		}
	}
	var person uuid.UUID
	tag, err := tx.Exec(ctx, "UPDATE nemura.overtime_entry SET state = $2, decided_by = $3, decided_at = $4, decision_reason = $5 WHERE id = $1 AND state = 'pending'", entry, state, by, at, orNil(reason))
	if err != nil {
		return overtimeError(err)
	}
	if tag.RowsAffected() != 1 {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "That record is decided already, or there is no such record: a decision is made once."}
	}
	if err := tx.QueryRow(ctx, "SELECT person_id FROM nemura.overtime_entry WHERE id = $1", entry).Scan(&person); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.overtime."+state, audit.Object{Type: "overtime_entry", ID: entry}, map[string]audit.Change{"state": {"pending", state}}, reason); err != nil {
		return err
	}
	return notice(ctx, tx, "overtime_"+state, person, entry, "overtime-decided")
}

// OvertimeRow is a record as it is listed.
type OvertimeRow struct {
	ID, Person               uuid.UUID
	PersonName, Kind, Branch string
	Date                     time.Time
	Hours                    leave.Days
	OverLimit                bool
	Note, State, Reason      string
	DecidedAt, UsedAt        *time.Time
}

// Overtime lists the records a scope may read, newest first; the scope is the leave requests' (own, reports', or all).
func Overtime(ctx context.Context, tx pgx.Tx, s LeaveScope, state string) ([]OvertimeRow, error) {
	rows, err := tx.Query(ctx, `SELECT o.id, o.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), o.kind, coalesce(o.branch, ''), o.work_date, o.hours::text, o.over_limit,
 coalesce(o.note, ''), o.state, coalesce(o.decision_reason, ''), o.decided_at, o.used_at
FROM nemura.overtime_entry o JOIN nemura.person p ON p.organisation_id = o.organisation_id AND p.id = o.person_id
WHERE `+scopeSQL(s, "$1", "$3::date")+` AND ($2 = '' OR o.state = $2) ORDER BY o.work_date DESC, o.id`, s.Member, state, day(s.On))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OvertimeRow
	for rows.Next() {
		var r OvertimeRow
		var hours string
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Kind, &r.Branch, &r.Date, &hours, &r.OverLimit, &r.Note, &r.State, &r.Reason, &r.DecidedAt, &r.UsedAt); err != nil {
			return nil, err
		}
		if r.Hours, err = leave.ParseDays(hours); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecideUnpaidSick records how the people team handles the days of a sick leave notification beyond the paid ceiling, once: as unpaid leave (a pay input) or taken
// from annual leave (NM-LV-08, NM-LV-12). A notification with no unpaid days has nothing to decide.
func DecideUnpaidSick(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, notification uuid.UUID, treatment string) (leave.Days, error) {
	if treatment != "unpaid" && treatment != "annual" {
		return 0, &leave.Refusal{Code: leave.CodeInvalid, Message: "The treatment is unpaid or annual: choose how the days beyond the ceiling are handled."}
	}
	var person uuid.UUID
	var unpaid string
	var from, to time.Time
	var year int
	if err := tx.QueryRow(ctx, "SELECT person_id, unpaid_days::text, start_date, end_date, leave_year FROM nemura.sick_notification WHERE id = $1", notification).Scan(&person, &unpaid, &from, &to, &year); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such sick leave notification."}
		}
		return 0, err
	}
	days, err := leave.ParseDays(unpaid)
	if err != nil {
		return 0, err
	}
	if days <= 0 {
		return 0, &leave.Refusal{Code: leave.CodeConflict, Message: "That notification has no days beyond the paid ceiling: there is nothing to decide."}
	}
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.sick_unpaid_decision (notification_id, treatment, decided_by, decided_at) VALUES ($1, $2, $3, $4)", notification, treatment, by, at); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return 0, &leave.Refusal{Code: leave.CodeConflict, Message: "Those days are decided already: a decision is made once."}
		}
		return 0, overtimeError(err)
	}
	switch treatment {
	case "unpaid":
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_input (id, person_id, kind, days, from_date, to_date, source_type, source_id, created_by, created_at)
VALUES ($1, $2, 'unpaid_leave', $3::numeric, $4, $5, 'sick_notification', $6, $7, $8)`, id, person, days.String(), day(from), day(to), notification, by, at); err != nil {
			return 0, overtimeError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.pay_input.created", audit.Object{Type: "pay_input", ID: id}, map[string]audit.Change{"kind": {nil, "unpaid_leave"}, "days": {nil, days.String()}, "person": {nil, person.String()}}, ""); err != nil {
			return 0, err
		}
	case "annual":
		if _, err := AppendLeave(ctx, tx, org, at, by, person, AnnualLeave, year, nil, leave.Entry{Kind: leave.Use, Amount: -days, Source: "sick-unpaid:" + notification.String()}, ""); err != nil {
			return 0, err
		}
	}
	return days, Audit(ctx, tx, org, at, user(by), "nemura.sick_leave.unpaid_decided", audit.Object{Type: "sick_unpaid_decision", ID: notification}, map[string]audit.Change{"treatment": {nil, treatment}, "days": {nil, days.String()}}, "")
}

// EncashLeave pays out days of a person's annual leave instead of their being taken (NM-LV-08): the ledger takes the days as an encashment, which the database refuses
// beyond the balance, and the pay run gets an input to pay them. It is off by default: the people team records it, with a note, for the days the company agreed.
func EncashLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, year int, days leave.Days, on time.Time, note string) (uuid.UUID, error) {
	if days <= 0 {
		return uuid.Nil, &leave.Refusal{Code: leave.CodeInvalid, Message: "The days to encash are more than zero: write them as 2 or 1.5."}
	}
	if len(note) > 500 {
		return uuid.Nil, &leave.Refusal{Code: leave.CodeInvalid, Message: "The note is at most 500 characters."}
	}
	entry, err := AppendLeave(ctx, tx, org, at, by, person, AnnualLeave, year, nil, leave.Entry{Kind: leave.Encashment, Amount: -days, Source: fmt.Sprintf("encashment:%s", day(on))}, note)
	if err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_input (id, person_id, kind, days, from_date, to_date, source_type, source_id, note, created_by, created_at)
VALUES ($1, $2, 'encashment', $3::numeric, $4, $4, 'leave_ledger', $5, $6, $7, $8)`, id, person, days.String(), day(on), entry, orNil(note), by, at); err != nil {
		return uuid.Nil, overtimeError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.pay_input.created", audit.Object{Type: "pay_input", ID: id}, map[string]audit.Change{"kind": {nil, "encashment"}, "days": {nil, days.String()}, "person": {nil, person.String()}}, "")
}

// PayInput is something a pay run takes from leave and time: unpaid leave days, encashed days, or approved hours, each linked to its source.
type PayInput struct {
	ID, Person       uuid.UUID
	PersonName, Kind string // unpaid_leave, encashment, overtime, rest_day, public_holiday
	Quantity         leave.Days
	Unit             string // days or hours
	From, To         time.Time
	SourceType       string
	SourceID         uuid.UUID
}

// OpenPayInputs are the inputs no pay run has used, with a work or leave day on or before to, oldest first (NM-PAY-02): unpaid leave, encashment and approved overtime,
// rest-day and public-holiday hours. An approved record is open until a pay run marks it used, once.
func OpenPayInputs(ctx context.Context, tx pgx.Tx, to time.Time) ([]PayInput, error) {
	rows, err := tx.Query(ctx, `
SELECT i.id, i.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), i.kind, i.days::text, 'days', i.from_date, i.to_date, i.source_type, i.source_id
FROM nemura.pay_input i JOIN nemura.person p ON p.organisation_id = i.organisation_id AND p.id = i.person_id WHERE i.used_in_run IS NULL AND i.to_date <= $1
UNION ALL
SELECT o.id, o.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), o.kind, o.hours::text, 'hours', o.work_date, o.work_date, 'overtime_entry', o.id
FROM nemura.overtime_entry o JOIN nemura.person p ON p.organisation_id = o.organisation_id AND p.id = o.person_id WHERE o.state = 'approved' AND o.used_in_run IS NULL AND o.work_date <= $1
ORDER BY 7, 2, 1`, day(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayInput
	for rows.Next() {
		var r PayInput
		var qty string
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Kind, &qty, &r.Unit, &r.From, &r.To, &r.SourceType, &r.SourceID); err != nil {
			return nil, err
		}
		if r.Quantity, err = leave.ParseDays(qty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
