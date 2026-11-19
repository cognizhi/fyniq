package nemura

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
)

func accrualError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.Code == "NM156":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "An accrual draft is evidence: it is approved once and never edited or deleted."}
		case pg.Code == "23514" && pg.ConstraintName == "leave_accrual_not_own_approval":
			return &leave.Refusal{Code: leave.CodeRole, Message: "Nobody approves the accrual they drafted: another approver does."}
		case pg.Code == "23505":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "That month is drafted already: a month is drafted once."}
		}
	}
	return leaveError(err)
}

// DraftLeaveAccrual drafts a month's leave liability accrual (NM-LV-09): for each person, the carried-forward annual leave still untaken, which is what the company's
// accounting policy asks to recognise by default. The amounts are the pay structure's to put on it and the journal is the Fyniq link's; Nemura keeps the days. A month
// is drafted once. It returns how many people have days on the draft.
func DraftLeaveAccrual(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, month time.Time) (int, error) {
	return draftLeaveAccrual(ctx, tx, org, at, &by, month)
}

// DraftLeaveAccrualAsSystem is the worker's draft: the same accrual, with no member who made it (system_drafted), so a person approves it and the system never does.
func DraftLeaveAccrualAsSystem(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, month time.Time) (int, error) {
	return draftLeaveAccrual(ctx, tx, org, at, nil, month)
}

func draftLeaveAccrual(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by *uuid.UUID, month time.Time) (int, error) {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	var drafted bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.leave_accrual WHERE month = $1)", day(first)).Scan(&drafted); err != nil {
		return 0, err
	}
	if drafted {
		return 0, &leave.Refusal{Code: leave.CodeConflict, Message: "That month is drafted already: a month is drafted once."}
	}
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.person WHERE employment_start <= $2 AND (leaving_date IS NULL OR leaving_date >= $1) ORDER BY id", day(first), day(last))
	if err != nil {
		return 0, err
	}
	var people []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		people = append(people, id)
	}
	rows.Close()
	n := 0
	for _, p := range people {
		entries, err := LeaveEntries(ctx, tx, p, AnnualLeave, first.Year())
		if err != nil {
			return n, err
		}
		var carried leave.Days
		for _, e := range entries {
			if e.Kind == leave.Transfer && e.Amount > 0 && e.Expires != "" && e.Expires >= day(last) {
				carried += e.Amount
			}
		}
		b := leave.Sum(entries)
		remaining := max(min(carried-b.Taken-b.Booked, b.Left), 0)
		if remaining == 0 {
			continue
		}
		id := uuid.Must(uuid.NewV7())
		var insertErr error
		if by == nil { // the worker's role may not name a member as the drafter: the column is not in its grant
			_, insertErr = tx.Exec(ctx, `INSERT INTO nemura.leave_accrual (id, month, person_id, carried_days, annual_left, system_drafted, drafted_at) VALUES ($1, $2, $3, $4::numeric, $5::numeric, true, $6)`,
				id, day(first), p, remaining.String(), max(b.Left, 0).String(), at)
		} else {
			_, insertErr = tx.Exec(ctx, `INSERT INTO nemura.leave_accrual (id, month, person_id, carried_days, annual_left, drafted_by, drafted_at) VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6, $7)`,
				id, day(first), p, remaining.String(), max(b.Left, 0).String(), *by, at)
		}
		if insertErr != nil {
			return n, accrualError(insertErr)
		}
		actor := audit.Actor{Type: "system", ID: "nemura-worker"}
		if by != nil {
			actor = user(*by)
		}
		if err := Audit(ctx, tx, org, at, actor, "nemura.leave_accrual.drafted", audit.Object{Type: "leave_accrual", ID: id}, map[string]audit.Change{"month": {nil, day(first)}, "person": {nil, p.String()}, "carried_days": {nil, remaining.String()}}, ""); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ApproveLeaveAccrual approves a month's draft, once, by someone other than the person who drafted it: the draft is then ready for the Fyniq link to take as a journal
// for approval there. It returns how many lines were approved.
func ApproveLeaveAccrual(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, month time.Time) (int, error) {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	rows, err := tx.Query(ctx, "UPDATE nemura.leave_accrual SET state = 'approved', approved_by = $2, approved_at = $3 WHERE month = $1 AND state = 'draft' RETURNING id", day(first), by, at)
	if err != nil {
		return 0, accrualError(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, accrualError(err)
	}
	if len(ids) == 0 {
		return 0, &leave.Refusal{Code: leave.CodeConflict, Message: "There is no draft waiting for that month: it is approved already, or was never drafted."}
	}
	for _, id := range ids {
		if err := Audit(ctx, tx, org, at, user(by), "nemura.leave_accrual.approved", audit.Object{Type: "leave_accrual", ID: id}, map[string]audit.Change{"state": {"draft", "approved"}}, ""); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// AccrualRow is one person's line of a month's accrual.
type AccrualRow struct {
	ID, Person          uuid.UUID
	PersonName, State   string
	Carried, AnnualLeft leave.Days
}

// LeaveAccruals lists a month's lines by name.
func LeaveAccruals(ctx context.Context, tx pgx.Tx, month time.Time) ([]AccrualRow, error) {
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	rows, err := tx.Query(ctx, `SELECT a.id, a.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), a.state, a.carried_days::text, a.annual_left::text
FROM nemura.leave_accrual a JOIN nemura.person p ON p.organisation_id = a.organisation_id AND p.id = a.person_id WHERE a.month = $1 ORDER BY lower(p.legal_name), a.id`, day(first))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccrualRow
	for rows.Next() {
		var r AccrualRow
		var carried, left string
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.State, &carried, &left); err != nil {
			return nil, err
		}
		if r.Carried, err = leave.ParseDays(carried); err != nil {
			return nil, err
		}
		if r.AnnualLeft, err = leave.ParseDays(left); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Availability is what a person has available to work between two days, for the hours Fyniq plans against (NM-TIM-01, NM-LV-09): their working days by pattern less
// public holidays, less the days they are away on approved leave, sick leave and government-paid leave; each away day costs the hours of a working day.
type Availability struct {
	WorkingDays leave.Days // working days by pattern, public holidays left out
	AwayDays    leave.Days // of them, the days away
	HoursPerDay leave.Days // hundredths of an hour in a working day: the week's hours over the days worked
	Hours       leave.Days // available hours, in hundredths
}

// AvailableHours works out a person's availability between two days, both included, from their working pattern on the first day, the pack's holidays, and their leave. A
// person with no working pattern is refused.
func AvailableHours(ctx context.Context, tx pgx.Tx, v pack.Version, person uuid.UUID, from, to time.Time) (Availability, error) {
	var a Availability
	if to.Before(from) || int(to.Sub(from).Hours()/24)+1 > leave.MaxSpan {
		return a, &leave.Refusal{Code: leave.CodeInvalid, Message: "The period is from one day to a later one, at most a year: choose the days again."}
	}
	p, ok, err := WorkingPatternOn(ctx, tx, person, from)
	if err != nil {
		return a, err
	}
	if !ok {
		return a, &leave.Refusal{Code: leave.CodeInvalid, Message: "No working pattern is set for the person on the first day: set it, so the hours can be worked out."}
	}
	holidays, err := leave.Observed(v, p, from, to)
	if err != nil {
		return a, err
	}
	a.HoursPerDay = leave.Days(p.HoursPerWeek) * 10 / leave.Days(p.DaysPerWeek())
	away := map[string]leave.Days{} // the fraction of each working day the person is away
	mark := func(start, end time.Time, startHalf, endHalf bool) {
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			key := d.Format(time.DateOnly)
			if _, holiday := holidays[key]; holiday || !p.Works(d.Weekday()) {
				continue
			}
			cost := leave.Whole(1)
			if (d.Equal(start) && startHalf) || (d.Equal(end) && endHalf) {
				cost = 50
			}
			away[key] = min(away[key]+cost, leave.Whole(1))
		}
	}
	type span struct {
		from, to time.Time
		sh, eh   bool
	}
	var spans []span
	rows, err := tx.Query(ctx, `SELECT start_date, end_date, start_half, end_half FROM nemura.leave_request WHERE person_id = $1 AND state = 'approved' AND end_date >= $2 AND start_date <= $3
UNION ALL SELECT start_date, end_date, false, false FROM nemura.sick_notification WHERE person_id = $1 AND end_date >= $2 AND start_date <= $3
UNION ALL SELECT start_date, end_date, false, false FROM nemura.gov_leave WHERE person_id = $1 AND end_date >= $2 AND start_date <= $3`, person, day(from), day(to))
	if err != nil {
		return a, err
	}
	for rows.Next() {
		var s span
		if err := rows.Scan(&s.from, &s.to, &s.sh, &s.eh); err != nil {
			rows.Close()
			return a, err
		}
		spans = append(spans, s)
	}
	rows.Close()
	for _, s := range spans {
		mark(maxDay(s.from, from), minDay(s.to, to), s.sh && !s.from.Before(from), s.eh && !s.to.After(to))
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format(time.DateOnly)
		if _, holiday := holidays[key]; holiday || !p.Works(d.Weekday()) {
			continue
		}
		a.WorkingDays += leave.Whole(1)
		a.AwayDays += away[key]
	}
	a.Hours = (a.WorkingDays - a.AwayDays) * a.HoursPerDay / 100
	return a, nil
}

// KindLeaveAccrual is the outbox kind of the monthly draft.
const KindLeaveAccrual = "leave.accrual"

// RaiseLeaveAccrual enqueues, once for each month, the job that drafts the previous month's leave accrual, while the Leave feature is on (NM-LV-09). The business key is
// the month, so a second sweep, or a restart, enqueues nothing: the draft is idempotent twice over (the outbox key and the table's one draft a month).
func (s *Store) RaiseLeaveAccrual(ctx context.Context, org uuid.UUID, now time.Time) (int, error) {
	prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	var n int
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		on, err := FeatureSwitches(ctx, tx)
		if err != nil || on["leave"].State != "on" {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO nemura.outbox (worker_class, kind, payload, business_key, max_attempts) VALUES ('worker', $1, jsonb_build_object('month', $2::text), $3, 5)
ON CONFLICT (organisation_id, kind, business_key) DO NOTHING`, KindLeaveAccrual, prev.Format("2006-01"), "accrual:"+prev.Format("2006-01"))
		n = int(tag.RowsAffected())
		return err
	})
	return n, err
}

// LeaveAccrualWork is the worker's delivery of the monthly draft: it drafts the month as the system, and a month already drafted is done, not an error.
func (s *Store) LeaveAccrualWork(clock ports.Clock) outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row outbox.Row) error {
		var p struct{ Month string }
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("the accrual row %s has no month: %w", row.ID, err)
		}
		month, err := time.Parse("2006-01", p.Month)
		if err != nil {
			return fmt.Errorf("the accrual row %s names no month: %w", row.ID, err)
		}
		return s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := DraftLeaveAccrualAsSystem(ctx, tx, org, clock.Now(), month)
			var r *leave.Refusal
			if errors.As(err, &r) && r.Code == leave.CodeConflict {
				return nil // drafted already: the job is done
			}
			return err
		})
	}
}
