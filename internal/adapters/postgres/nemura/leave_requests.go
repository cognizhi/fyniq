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
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
)

// LeaveAsk is a request for leave as it is made (NM-LV-03).
type LeaveAsk struct {
	Person             uuid.UUID
	Type               string
	From, To           time.Time
	StartHalf, EndHalf bool
	Note               string
}

// Away is a colleague who is away on days that overlap a request: the name and the dates, never the type (NM-LV-05).
type Away struct {
	Person   uuid.UUID
	Name     string
	From, To time.Time
}

// LeavePreview is what the form shows before the request is made: the days it costs and how they were counted, the balance after it, and who else is away.
type LeavePreview struct {
	Charge    leave.Charge
	Left      leave.Days // the balance after this request
	Enough    bool
	Away      []Away
	Basis     string
	LeaveYear int
}

func requestError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM145":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "You already have leave requested or approved on some of those days: cancel it first, or choose other days."}
		case "NM144":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "That request cannot change that way: a decided request is evidence."}
		}
	}
	return leaveError(err)
}

func countingOf(typeKey string) (string, error) {
	if typeKey == AnnualLeave {
		return leave.WorkingDays, nil
	}
	return "", &leave.Refusal{Code: leave.CodeInvalid, Message: fmt.Sprintf("The leave type %q is not one you can ask for: choose Annual leave.", typeKey)}
}

// PreviewLeave works out what a request would cost and leave, and who else is away, without writing anything. The person's working pattern in force on the first
// day is needed to count their days: with none set it refuses and says who sets it.
func PreviewLeave(ctx context.Context, tx pgx.Tx, v pack.Version, a LeaveAsk, now time.Time) (LeavePreview, error) {
	var pv LeavePreview
	basis, err := countingOf(a.Type)
	if err != nil {
		return pv, err
	}
	if a.From.Year() != a.To.Year() {
		return pv, &leave.Refusal{Code: leave.CodeInvalid, Message: "The request runs over the new year: ask for each year on its own, so each comes out of its own balance."}
	}
	pv.LeaveYear, pv.Basis = a.From.Year(), basis
	var start time.Time
	var left *time.Time
	if err := tx.QueryRow(ctx, "SELECT employment_start, leaving_date FROM nemura.person WHERE id = $1", a.Person).Scan(&start, &left); err != nil {
		return pv, err
	}
	if a.From.Before(start) || (left != nil && a.To.After(*left)) {
		return pv, &leave.Refusal{Code: leave.CodeInvalid, Message: "Those dates are outside your employment: choose days from your first day to your last."}
	}
	p, ok, err := WorkingPatternOn(ctx, tx, a.Person, a.From)
	if err != nil {
		return pv, err
	}
	if !ok {
		return pv, &leave.Refusal{Code: leave.CodeInvalid, Message: "No working pattern is set for you on the first day: ask the people team to set it, so your days can be counted."}
	}
	if pv.Charge, err = leave.Count(v, p, basis, a.From, a.To, a.StartHalf, a.EndHalf); err != nil {
		return pv, err
	}
	b, err := LeaveBalance(ctx, tx, a.Person, a.Type, pv.LeaveYear)
	if err != nil {
		return pv, err
	}
	pv.Left = b.Left - pv.Charge.Days
	pv.Enough = pv.Left >= 0
	rows, err := tx.Query(ctx, `
SELECT p.id, coalesce(nullif(p.preferred_name, ''), p.legal_name), r.start_date, r.end_date
FROM nemura.leave_request r
JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.person_id
JOIN nemura.person_job j ON j.organisation_id = p.organisation_id AND j.person_id = p.id AND j.effective_from <= $4 AND (j.effective_to IS NULL OR j.effective_to > $4)
JOIN nemura.person_job me ON me.organisation_id = p.organisation_id AND me.person_id = $1 AND me.effective_from <= $4 AND (me.effective_to IS NULL OR me.effective_to > $4)
WHERE r.state = 'approved' AND r.person_id <> $1 AND daterange(r.start_date, r.end_date, '[]') && daterange($2, $3, '[]')
  AND ((me.manager_person_id IS NOT NULL AND j.manager_person_id = me.manager_person_id) OR (me.department IS NOT NULL AND j.department = me.department))
ORDER BY r.start_date, 2`, a.Person, day(a.From), day(a.To), day(now))
	if err != nil {
		return pv, err
	}
	defer rows.Close()
	for rows.Next() {
		var w Away
		if err := rows.Scan(&w.Person, &w.Name, &w.From, &w.To); err != nil {
			return pv, err
		}
		pv.Away = append(pv.Away, w)
	}
	return pv, rows.Err()
}

func notice(ctx context.Context, tx pgx.Tx, kind string, person, request uuid.UUID, key string) error {
	_, err := Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: "people.notice", BusinessKey: key + ":" + request.String(),
		Payload: []byte(`{"notice":"` + kind + `","person":"` + person.String() + `","change":"` + request.String() + `"}`)})
	return err
}

// ApplyLeave records a request and reserves its days in the ledger in one transaction (NM-LV-03, design D2): the balance is checked against the person's locked
// head, so two requests cannot both take the last days, and a request beyond the balance is refused with the days left. It queues a notice for the approver.
func ApplyLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, v pack.Version, a LeaveAsk) (uuid.UUID, leave.Charge, error) {
	if _, err := tx.Exec(ctx, "SELECT nemura.lock_leave_head($1, $2, $3)", a.Person, a.Type, a.From.Year()); err != nil {
		return uuid.Nil, leave.Charge{}, leaveError(err)
	}
	pv, err := PreviewLeave(ctx, tx, v, a, at)
	if err != nil {
		return uuid.Nil, pv.Charge, err
	}
	if !pv.Enough {
		return uuid.Nil, pv.Charge, &leave.Refusal{Code: leave.CodeBalance, Message: fmt.Sprintf("That is %s days and you have %s left: ask for fewer days, or ask the people team about your balance.", pv.Charge.Days, pv.Left+pv.Charge.Days)}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.leave_request (id, person_id, type_key, leave_year, start_date, end_date, start_half, end_half, days, note, requested_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11)`, id, a.Person, a.Type, pv.LeaveYear, day(a.From), day(a.To), a.StartHalf, a.EndHalf, pv.Charge.Days.String(), orNil(a.Note), by); err != nil {
		return uuid.Nil, pv.Charge, requestError(err)
	}
	if _, err := AppendLeave(ctx, tx, org, at, by, a.Person, a.Type, pv.LeaveYear, &id, leave.Entry{Kind: leave.Reservation, Amount: -pv.Charge.Days, Source: "request:" + id.String()}, ""); err != nil {
		return uuid.Nil, pv.Charge, err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.leave.requested", audit.Object{Type: "leave_request", ID: id}, map[string]audit.Change{"person": {nil, a.Person.String()},
		"type": {nil, a.Type}, "from": {nil, day(a.From)}, "to": {nil, day(a.To)}, "days": {nil, pv.Charge.Days.String()}}, ""); err != nil {
		return uuid.Nil, pv.Charge, err
	}
	return id, pv.Charge, notice(ctx, tx, "leave_requested", a.Person, id, "leave-requested")
}

// LeaveRequestRow is a request as it is listed.
type LeaveRequestRow struct {
	ID, Person         uuid.UUID
	PersonName, Type   string
	From, To           time.Time
	StartHalf, EndHalf bool
	Days               leave.Days
	Note, State        string
	RequestedAt        time.Time
	DecidedAt          *time.Time
	Reason             string
}

const requestColumns = `r.id, r.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), r.type_key, r.start_date, r.end_date, r.start_half, r.end_half, r.days::text,
 coalesce(r.note, ''), r.state, r.requested_at, r.decided_at, coalesce(r.decision_reason, '')`

func scanRequest(row pgx.Row) (LeaveRequestRow, error) {
	var r LeaveRequestRow
	var days string
	if err := row.Scan(&r.ID, &r.Person, &r.PersonName, &r.Type, &r.From, &r.To, &r.StartHalf, &r.EndHalf, &days, &r.Note, &r.State, &r.RequestedAt, &r.DecidedAt, &r.Reason); err != nil {
		return r, err
	}
	var err error
	r.Days, err = leave.ParseDays(days)
	return r, err
}

// LeaveScope says whose requests a caller reads: their own, those of the people who report to them, or everyone's.
type LeaveScope struct {
	Member  uuid.UUID
	On      time.Time // the day the reporting line is read on
	All     bool      // the people team and the Owner
	Reports bool      // a line manager's reports
}

const requestFrom = ` FROM nemura.leave_request r JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.person_id`

// scopeSQL narrows a listing to what the scope may read: a request of the person's own login, a report's, or any.
func scopeSQL(s LeaveScope, member, on string) string {
	if s.All {
		return "(" + member + "::uuid IS NOT NULL AND " + on + " IS NOT NULL)" // every parameter is used, so the statement takes the same arguments for every scope
	}
	own := "p.member_id = " + member
	if !s.Reports {
		return "(" + own + " AND " + on + " IS NOT NULL)"
	}
	return "(" + own + ` OR p.member_id IN (SELECT l.report_id FROM nemura.reporting_line l WHERE l.manager_id = ` + member + ` AND l.effective_from <= ` + on + ` AND (l.effective_to IS NULL OR l.effective_to > ` + on + `)))`
}

// LeaveRequests lists the requests a scope may read, newest first, optionally of one state or one person.
func LeaveRequests(ctx context.Context, tx pgx.Tx, s LeaveScope, state string, person *uuid.UUID) ([]LeaveRequestRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+requestColumns+requestFrom+" WHERE "+scopeSQL(s, "$1", "$4::date")+" AND ($2 = '' OR r.state = $2) AND ($3::uuid IS NULL OR r.person_id = $3) ORDER BY r.start_date DESC, r.id", s.Member, state, person, day(s.On))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaveRequestRow
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LeaveRequestFor is one request a scope may read, or a not-found refusal naming no one else's.
func LeaveRequestFor(ctx context.Context, tx pgx.Tx, s LeaveScope, id uuid.UUID) (LeaveRequestRow, error) {
	r, err := scanRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+requestFrom+" WHERE r.id = $2 AND "+scopeSQL(s, "$1", "$3::date"), s.Member, id, day(s.On)))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such leave request in your view."}
	}
	return r, err
}

// DecideLeave approves or refuses a pending request (NM-LV-04). The caller has checked who may decide; the database refuses the requester deciding their own. An
// approval keeps the days reserved; a refusal needs a reason and releases them. Either is audited and tells the person.
func DecideLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, request uuid.UUID, approve bool, reason string) error {
	var r struct {
		person uuid.UUID
		typ    string
		year   int
		days   string
		state  string
	}
	if err := tx.QueryRow(ctx, "SELECT person_id, type_key, leave_year, days::text, state FROM nemura.leave_request WHERE id = $1 FOR UPDATE", request).Scan(&r.person, &r.typ, &r.year, &r.days, &r.state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such leave request in your view."}
		}
		return err
	}
	if r.state != "pending" {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "That request is " + r.state + " already: a decision is made once."}
	}
	state := "approved"
	if !approve {
		state = "refused"
		if len(reason) == 0 || len(reason) > 500 {
			return &leave.Refusal{Code: leave.CodeInvalid, Message: "A refusal needs a reason of up to 500 characters: tell the person why."}
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.leave_request SET state = $2, decided_by = $3, decided_at = $4, decision_reason = $5 WHERE id = $1", request, state, by, at, orNil(reason)); err != nil {
		return requestError(err)
	}
	if !approve {
		days, _ := leave.ParseDays(r.days)
		if _, err := AppendLeave(ctx, tx, org, at, by, r.person, r.typ, r.year, &request, leave.Entry{Kind: leave.Release, Amount: days, Source: "request:" + request.String()}, ""); err != nil {
			return err
		}
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.leave."+state, audit.Object{Type: "leave_request", ID: request}, map[string]audit.Change{"state": {"pending", state}}, reason); err != nil {
		return err
	}
	if approve { // a linked organisation tells Fyniq who is away, in the same transaction (design D27)
		if _, err := QueueLeaveEvent(ctx, tx, org, at, request); err != nil {
			return err
		}
	}
	return notice(ctx, tx, "leave_"+state, r.person, request, "leave-decided")
}

// CancelLeave withdraws a request of one's own: a pending one, or an approved one that has not started. It releases the days.
func CancelLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, request uuid.UUID, today time.Time) error {
	var r struct {
		person uuid.UUID
		typ    string
		year   int
		days   string
		state  string
		from   time.Time
		by     uuid.UUID
	}
	if err := tx.QueryRow(ctx, "SELECT person_id, type_key, leave_year, days::text, state, start_date, requested_by FROM nemura.leave_request WHERE id = $1 FOR UPDATE", request).
		Scan(&r.person, &r.typ, &r.year, &r.days, &r.state, &r.from, &r.by); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such leave request in your view."}
		}
		return err
	}
	if r.state != "pending" && r.state != "approved" {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "That request is " + r.state + ": there is nothing to cancel."}
	}
	if r.state == "approved" && !r.from.After(today) {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "That leave has started: ask the people team to correct it."}
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.leave_request SET state = 'cancelled', decided_by = $2, decided_at = $3 WHERE id = $1", request, by, at); err != nil {
		return requestError(err)
	}
	days, _ := leave.ParseDays(r.days)
	if _, err := AppendLeave(ctx, tx, org, at, by, r.person, r.typ, r.year, &request, leave.Entry{Kind: leave.Release, Amount: days, Source: "request:" + request.String()}, ""); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.leave.cancelled", audit.Object{Type: "leave_request", ID: request}, map[string]audit.Change{"state": {r.state, "cancelled"}}, ""); err != nil {
		return err
	}
	if r.state == "approved" { // Fyniq was told of it: it is told of the tombstone
		_, err := QueueLeaveEvent(ctx, tx, org, at, request)
		return err
	}
	return nil
}

// SettleLeave turns the reservation of each approved request whose last day is before on into a use, once: the days are taken. It returns how many requests and days
// it settled. The people team runs it; a worker job takes it over with the pay run's inputs (slice 4).
func SettleLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, on time.Time) (int, leave.Days, error) {
	rows, err := tx.Query(ctx, "SELECT id, person_id, type_key, leave_year, days::text FROM nemura.leave_request WHERE state = 'approved' AND settled_at IS NULL AND end_date < $1 ORDER BY end_date, id", day(on))
	if err != nil {
		return 0, 0, err
	}
	type due struct {
		id, person uuid.UUID
		typ        string
		year       int
		days       leave.Days
	}
	var list []due
	for rows.Next() {
		var d due
		var days string
		if err := rows.Scan(&d.id, &d.person, &d.typ, &d.year, &days); err != nil {
			rows.Close()
			return 0, 0, err
		}
		d.days, _ = leave.ParseDays(days)
		list = append(list, d)
	}
	rows.Close()
	var total leave.Days
	for _, d := range list {
		src := "request:" + d.id.String()
		if _, err := AppendLeave(ctx, tx, org, at, by, d.person, d.typ, d.year, &d.id, leave.Entry{Kind: leave.Release, Amount: d.days, Source: src}, ""); err != nil {
			return 0, 0, err
		}
		if _, err := AppendLeave(ctx, tx, org, at, by, d.person, d.typ, d.year, &d.id, leave.Entry{Kind: leave.Use, Amount: -d.days, Source: src}, ""); err != nil {
			return 0, 0, err
		}
		if _, err := tx.Exec(ctx, "UPDATE nemura.leave_request SET settled_at = $2 WHERE id = $1", d.id, at); err != nil {
			return 0, 0, requestError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.leave.settled", audit.Object{Type: "leave_request", ID: d.id}, map[string]audit.Change{"days": {nil, d.days.String()}}, ""); err != nil {
			return 0, 0, err
		}
		total += d.days
	}
	return len(list), total, nil
}
