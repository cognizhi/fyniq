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
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// AnnualLeave is the key of the annual leave type in the ledger (NM-LV-01).
const AnnualLeave = "annual"

func leaveError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM141":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "The person already has a working pattern on those days: start the new one on or after the day the last one ends."}
		case "NM143":
			return &leave.Refusal{Code: leave.CodeBalance, Message: "That is more than the balance: the leave type does not allow it to go below zero."}
		}
	}
	return err
}

// SetWorkingPattern ends the person's current working pattern the day before from and starts the new one on from, with the audit events (NM-TIM-01).
// A pattern is history: it is ended once and never edited.
func SetWorkingPattern(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, p leave.Pattern) error {
	if err := p.Check(); err != nil {
		return err
	}
	var ended uuid.UUID
	err := tx.QueryRow(ctx, "UPDATE nemura.working_pattern SET effective_to = $2 WHERE person_id = $1 AND effective_to IS NULL AND effective_from < $2 RETURNING id", person, day(from)).Scan(&ended)
	if err == nil {
		if err := Audit(ctx, tx, org, at, user(by), "nemura.working_pattern.ended", audit.Object{Type: "working_pattern", ID: ended}, map[string]audit.Change{"effective_to": {nil, day(from)}}, ""); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return leaveError(err)
	}
	id := uuid.Must(uuid.NewV7())
	weekdays := make([]int16, len(p.Weekdays))
	for i, d := range p.Weekdays {
		weekdays[i] = int16(d)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.working_pattern (id, person_id, effective_from, weekdays, hours_per_week) VALUES ($1, $2, $3, $4, $5::numeric / 10)", id, person, day(from), weekdays, p.HoursPerWeek); err != nil {
		return leaveError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.working_pattern.set", audit.Object{Type: "working_pattern", ID: id},
		map[string]audit.Change{"person": {nil, person.String()}, "effective_from": {nil, day(from)}, "weekdays": {nil, p.Weekdays}, "hours_per_week": {nil, fmt.Sprintf("%d.%d", p.HoursPerWeek/10, p.HoursPerWeek%10)}}, "")
}

// WorkingPatternOn is the person's working pattern on a day, or false when none is set.
func WorkingPatternOn(ctx context.Context, tx pgx.Tx, person uuid.UUID, on time.Time) (leave.Pattern, bool, error) {
	var days []int16
	var hours int
	err := tx.QueryRow(ctx, `SELECT weekdays, (hours_per_week * 10)::int FROM nemura.working_pattern
WHERE person_id = $1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, person, day(on)).Scan(&days, &hours)
	if errors.Is(err, pgx.ErrNoRows) {
		return leave.Pattern{}, false, nil
	}
	if err != nil {
		return leave.Pattern{}, false, err
	}
	p := leave.Pattern{HoursPerWeek: hours}
	for _, d := range days {
		p.Weekdays = append(p.Weekdays, int(d))
	}
	return p, true, nil
}

// AppendLeave adds one entry to a person's ledger for a leave type and year, with its audit event (NM-LV-13, design D1, D2). It locks the person's head row for
// the type and year first, so the balance the database checks and the entry it writes cannot be overtaken by another request. An entry that would take a type
// that forbids it below zero is refused with NM_LEAVE_BALANCE.
func AppendLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, typeKey string, year int, requestID *uuid.UUID, e leave.Entry, note string) (uuid.UUID, error) {
	if !leave.SignOK(e.Kind, e.Amount) {
		return uuid.Nil, &leave.Refusal{Code: leave.CodeInvalid, Message: "The amount of a " + e.Kind + " must be " + signWord(e.Kind) + ": correct the amount."}
	}
	if _, err := tx.Exec(ctx, "SELECT nemura.lock_leave_head($1, $2, $3)", person, typeKey, year); err != nil {
		return uuid.Nil, leaveError(err)
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.leave_ledger (id, person_id, type_key, leave_year, kind, amount, negative_ok, expires_on, request_id, source, note, created_by)
VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9, $10, $11, $12)`, id, person, typeKey, year, e.Kind, e.Amount.String(), e.Negative, orNil(e.Expires), request(requestID), e.Source, orNil(note), by); err != nil {
		return uuid.Nil, leaveError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.leave.entry", audit.Object{Type: "leave_ledger", ID: id}, map[string]audit.Change{"person": {nil, person.String()},
		"type": {nil, typeKey}, "leave_year": {nil, year}, "kind": {nil, e.Kind}, "amount": {nil, e.Amount.String()}, "source": {nil, e.Source}}, "")
}

func signWord(kind string) string {
	switch kind {
	case leave.Grant, leave.Accrual, leave.Release:
		return "more than zero"
	case leave.Transfer, leave.Correction:
		return "more or less than zero"
	}
	return "less than zero"
}

func request(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

// LeaveEntries is a person's ledger for a leave type and year, oldest first.
func LeaveEntries(ctx context.Context, tx pgx.Tx, person uuid.UUID, typeKey string, year int) ([]leave.Entry, error) {
	rows, err := tx.Query(ctx, `SELECT kind, amount::text, source, coalesce(expires_on::text, ''), negative_ok FROM nemura.leave_ledger
WHERE person_id = $1 AND type_key = $2 AND leave_year = $3 ORDER BY created_at, id`, person, typeKey, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []leave.Entry
	for rows.Next() {
		var e leave.Entry
		var amount string
		if err := rows.Scan(&e.Kind, &amount, &e.Source, &e.Expires, &e.Negative); err != nil {
			return nil, err
		}
		if e.Amount, err = leave.ParseDays(amount); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LeaveBalance is the four sums of a person's ledger for a leave type and year.
func LeaveBalance(ctx context.Context, tx pgx.Tx, person uuid.UUID, typeKey string, year int) (leave.Balance, error) {
	entries, err := LeaveEntries(ctx, tx, person, typeKey, year)
	return leave.Sum(entries), err
}

func annualSource(year int) string { return fmt.Sprintf("annual-leave:%d", year) }

// GrantAnnualLeave works out a person's annual leave for a calendar year from the pack and the company's policy (NM-LV-02) and writes it to the ledger: a grant
// the first time, and afterwards only the difference as a correction, so running it twice changes nothing and a changed policy or start date is a visible
// entry. It returns the entitlement with its working, and the amount written (zero when the ledger was already right).
func GrantAnnualLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, year int, v pack.Version, policyDays []int) (leave.Entitlement, leave.Days, error) {
	p, err := PersonOn(ctx, tx, person, at)
	if err != nil {
		return leave.Entitlement{}, 0, err
	}
	c, ok, err := ClassificationOn(ctx, tx, person, time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return leave.Entitlement{}, 0, err
	}
	if !ok {
		return leave.Entitlement{}, 0, &people.Refusal{Code: people.CodeInvalid, Message: "The person has no legal classification: set it on their record before annual leave is granted."}
	}
	g, err := leave.AnnualGrant(v, leave.Service{Start: p.Start, Leaving: p.LeavingDate, Employee: c.ContractType == "service", EACovered: c.EACovered}, policyDays, year)
	if err != nil {
		return g, 0, err
	}
	var granted leave.Days
	entries, err := LeaveEntries(ctx, tx, person, AnnualLeave, year)
	if err != nil {
		return g, 0, err
	}
	for _, e := range entries {
		if e.Source == annualSource(year) {
			granted += e.Amount
		}
	}
	diff := g.Days - granted
	if diff == 0 {
		return g, 0, nil
	}
	kind := leave.Grant
	if granted != 0 {
		kind = leave.Correction
	}
	_, err = AppendLeave(ctx, tx, org, at, by, person, AnnualLeave, year, nil, leave.Entry{Kind: kind, Amount: diff, Source: annualSource(year)}, "")
	return g, diff, err
}

func carrySource(year int) string { return fmt.Sprintf("carry-forward:%d", year) }

// CarryForwardLeave moves a person's unused annual leave of a year into the next as a transfer out and a transfer in, the carried days with their last day
// (NM-LV-02, owner decision O1). Running it again carries only what has become unused since. It returns the days carried.
func CarryForwardLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, year, months int) (leave.Days, error) {
	b, err := LeaveBalance(ctx, tx, person, AnnualLeave, year)
	if err != nil {
		return 0, err
	}
	carry, last := leave.CarryForward(b.Left, year, months)
	if carry == 0 {
		return 0, nil
	}
	src := carrySource(year)
	if _, err := AppendLeave(ctx, tx, org, at, by, person, AnnualLeave, year, nil, leave.Entry{Kind: leave.Transfer, Amount: -carry, Source: src}, "carried into "+fmt.Sprint(year+1)); err != nil {
		return 0, err
	}
	_, err = AppendLeave(ctx, tx, org, at, by, person, AnnualLeave, year+1, nil, leave.Entry{Kind: leave.Transfer, Amount: carry, Source: src, Expires: day(last)}, "carried from "+fmt.Sprint(year))
	return carry, err
}

// ExpireCarriedLeave lapses the carried days of a year that were not used by the last day they may be taken: the entry is an expiry in the year they were carried
// into. It returns the days that lapsed, and does nothing before the last day has passed or twice.
func ExpireCarriedLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, year int, on time.Time) (leave.Days, error) {
	entries, err := LeaveEntries(ctx, tx, person, AnnualLeave, year)
	if err != nil {
		return 0, err
	}
	var carried, expired leave.Days
	for _, e := range entries {
		switch {
		case e.Kind == leave.Transfer && e.Amount > 0 && e.Expires != "" && e.Expires < day(on):
			carried += e.Amount
		case e.Kind == leave.Expiry:
			expired += -e.Amount
		}
	}
	b := leave.Sum(entries)
	lapse := leave.ExpiredCarry(carried, b) - expired
	if lapse <= 0 {
		return 0, nil
	}
	_, err = AppendLeave(ctx, tx, org, at, by, person, AnnualLeave, year, nil, leave.Entry{Kind: leave.Expiry, Amount: -lapse, Source: "carry-forward-expiry"}, "")
	return lapse, err
}
