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
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
)

// GovAsk is a block of government-paid leave to record (NM-LV-07, NM-LV-14).
type GovAsk struct {
	Person, Dependant uuid.UUID
	Scheme            string
	ChildOrder        int
	From, To          time.Time
}

func govError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM150":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "The person has leave, sick leave or government-paid leave on some of those days: correct that first, or choose the days again."}
		case "NM149":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "A government-paid leave record, a claim and its events are evidence: a correction is a new record."}
		}
	}
	return leaveError(err)
}

// RecordGovLeave records a block of maternity, paternity or shared parental leave for a child the person declared (NM-LV-07, NM-LV-14). The scheme's rule is the
// country pack's for the child's birth date; the child must be a Singapore Citizen, the person must have served the months the scheme asks for before the birth, the
// dates must fall in the scheme's window and weeks, and the block must not overlap any other leave. The claim deadline is worked out and kept with it. The child's
// date of birth is read here and never written to the audit event.
func RecordGovLeave(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, v pack.Version, a GovAsk) (uuid.UUID, leave.ParentalRule, error) {
	var owner uuid.UUID
	var born *time.Time
	var citizenship string
	if err := tx.QueryRow(ctx, "SELECT person_id, birth_date, citizenship FROM nemura.person_dependant WHERE id = $1", a.Dependant).Scan(&owner, &born, &citizenship); err != nil || owner != a.Person {
		return uuid.Nil, leave.ParentalRule{}, &leave.Refusal{Code: leave.CodeInvalid, Message: "That child is not a declared dependant of this person: record the child under the person's dependants first."}
	}
	if born == nil {
		return uuid.Nil, leave.ParentalRule{}, &leave.Refusal{Code: leave.CodeInvalid, Message: "The dependant has no date of birth yet, only the intent to adopt: record the date of birth, or the formal intent to adopt date when the scheme uses it, before leave is recorded."}
	}
	var start time.Time
	var left *time.Time
	if err := tx.QueryRow(ctx, "SELECT employment_start, leaving_date FROM nemura.person WHERE id = $1", a.Person).Scan(&start, &left); err != nil {
		return uuid.Nil, leave.ParentalRule{}, err
	}
	key, err := leave.SchemeKey(v, a.Scheme, a.ChildOrder, *born)
	if err != nil {
		return uuid.Nil, leave.ParentalRule{}, err
	}
	rule, err := leave.RuleOf(v, key, *born)
	if err != nil {
		return uuid.Nil, rule, err
	}
	if err := leave.Eligible(rule, start, *born, citizenship == "citizen"); err != nil {
		return uuid.Nil, rule, err
	}
	var before int
	if err := tx.QueryRow(ctx, "SELECT coalesce(sum(days), 0)::int FROM nemura.gov_leave WHERE person_id = $1 AND dependant_id = $2 AND scheme = $3", a.Person, a.Dependant, a.Scheme).Scan(&before); err != nil {
		return uuid.Nil, rule, err
	}
	if err := leave.Window(a.Scheme, rule, *born, a.From, a.To, before); err != nil {
		return uuid.Nil, rule, err
	}
	if a.From.Before(start) || (left != nil && a.To.After(*left)) {
		return uuid.Nil, rule, &leave.Refusal{Code: leave.CodeInvalid, Message: "Those dates are outside the person's employment: choose days from the first day to the last."}
	}
	id := uuid.Must(uuid.NewV7())
	days := int(a.To.Sub(a.From).Hours()/24) + 1
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.gov_leave (id, person_id, scheme, scheme_key, pack_version, dependant_id, child_order, start_date, end_date, days, claim_deadline, recorded_by, recorded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`, id, a.Person, a.Scheme, key, v.ID, a.Dependant, a.ChildOrder, day(a.From), day(a.To), days, day(leave.ClaimDeadline(rule, a.To)), by, at); err != nil {
		return uuid.Nil, rule, govError(err)
	}
	return id, rule, Audit(ctx, tx, org, at, user(by), "nemura.gov_leave.recorded", audit.Object{Type: "gov_leave", ID: id}, map[string]audit.Change{"person": {nil, a.Person.String()},
		"scheme": {nil, a.Scheme}, "dependant": {nil, a.Dependant.String()}, "from": {nil, day(a.From)}, "to": {nil, day(a.To)}, "days": {nil, days}, "pack": {nil, v.ID}}, "")
}

// FileGovClaim records that a person filed the employer's claim on the portal: the amount, the day and the portal's reference (NM-LV-07). An amount above what the
// scheme's cap suggests is recorded and flagged, never refused: the portal decides, and the salary owed never depends on it.
func FileGovClaim(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, govLeave uuid.UUID, resolve func(string) (pack.Version, bool), amount leave.Cents, filedOn time.Time, reference string) (uuid.UUID, bool, error) {
	if amount <= 0 {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "The amount claimed must be more than zero: write it as 2500.00."}
	}
	if len(reference) > 100 {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "The portal's reference is at most 100 characters."}
	}
	var key, version string
	var days int
	var born time.Time
	err := tx.QueryRow(ctx, `SELECT g.scheme_key, g.pack_version, g.days, d.birth_date FROM nemura.gov_leave g JOIN nemura.person_dependant d ON d.organisation_id = g.organisation_id AND d.id = g.dependant_id WHERE g.id = $1`, govLeave).Scan(&key, &version, &days, &born)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such government-paid leave record."}
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	v, ok := resolve(version)
	if !ok {
		return uuid.Nil, false, &leave.Refusal{Code: leave.CodeInvalid, Message: "The country pack version the leave was recorded under is not loaded: ask whoever runs Nemura."}
	}
	rule, err := leave.RuleOf(v, key, born)
	if err != nil {
		return uuid.Nil, false, err
	}
	over := false
	if limit, ok := leave.MaxClaim(rule, days); ok {
		var sofar string
		if err := tx.QueryRow(ctx, "SELECT coalesce(sum(amount), 0)::numeric(20,2)::text FROM nemura.gov_claim WHERE gov_leave_id = $1", govLeave).Scan(&sofar); err != nil {
			return uuid.Nil, false, err
		}
		claimed, err := leave.ParseCents(sofar)
		if err != nil {
			return uuid.Nil, false, err
		}
		over = claimed+amount > limit
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.gov_claim (id, gov_leave_id, amount, filed_on, reference, over_guide, filed_by, created_at) VALUES ($1, $2, $3::numeric, $4, $5, $6, $7, $8)",
		id, govLeave, amount.String(), day(filedOn), orNil(reference), over, by, at); err != nil {
		return uuid.Nil, false, govError(err)
	}
	return id, over, Audit(ctx, tx, org, at, user(by), "nemura.gov_claim.filed", audit.Object{Type: "gov_claim", ID: id}, map[string]audit.Change{"gov_leave": {nil, govLeave.String()},
		"amount": {nil, amount.String()}, "filed_on": {nil, day(filedOn)}, "over_guide": {nil, over}}, "")
}

// RecordClaimEvent records a receipt (some or all of the amount arrived) or a rejection of a claim (NM-LV-14): partial receipts and rejections are separate
// records, and a claim's state is worked out from them.
func RecordClaimEvent(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, claim uuid.UUID, kind string, amount leave.Cents, on time.Time, note string) (uuid.UUID, error) {
	if kind != "receipt" && kind != "rejection" {
		return uuid.Nil, &leave.Refusal{Code: leave.CodeInvalid, Message: "An event is a receipt or a rejection: choose one."}
	}
	if (kind == "receipt") != (amount > 0) {
		return uuid.Nil, &leave.Refusal{Code: leave.CodeInvalid, Message: "A receipt needs the amount received and a rejection takes none: correct the amount."}
	}
	if len(note) > 500 {
		return uuid.Nil, &leave.Refusal{Code: leave.CodeInvalid, Message: "The note is at most 500 characters."}
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.gov_claim WHERE id = $1)", claim).Scan(&exists); err != nil || !exists {
		return uuid.Nil, &leave.Refusal{Code: leave.CodeNotFound, Message: "There is no such claim."}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.gov_claim_event (id, claim_id, kind, amount, happened_on, note, recorded_by, created_at) VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8)",
		id, claim, kind, amount.String(), day(on), orNil(note), by, at); err != nil {
		return uuid.Nil, govError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.gov_claim."+kind, audit.Object{Type: "gov_claim_event", ID: id}, map[string]audit.Change{"claim": {nil, claim.String()}, "amount": {nil, amount.String()}, "on": {nil, day(on)}}, "")
}

// ClaimRow is a claim with what has come of it.
type ClaimRow struct {
	ID               uuid.UUID
	Amount, Received leave.Cents
	FiledOn          time.Time
	Reference        string
	OverGuide        bool
	Rejected         bool
}

// State is where a claim is: paid in full, part paid, rejected, or filed and waiting.
func (c ClaimRow) State() string {
	switch {
	case c.Received >= c.Amount:
		return "paid"
	case c.Rejected:
		return "rejected"
	case c.Received > 0:
		return "part_paid"
	}
	return "filed"
}

// GovRow is a block of government-paid leave with its claims.
type GovRow struct {
	ID, Person   uuid.UUID
	PersonName   string
	Scheme, Key  string
	From, To     time.Time
	Days         int
	Deadline     time.Time
	Claims       []ClaimRow
	GuideMaximum *leave.Cents
}

// GovLeaves lists every block with its claims, soonest deadline first.
func GovLeaves(ctx context.Context, tx pgx.Tx, v func(string) (pack.Version, bool)) ([]GovRow, error) {
	rows, err := tx.Query(ctx, `SELECT g.id, g.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), g.scheme, g.scheme_key, g.pack_version, g.start_date, g.end_date, g.days, g.claim_deadline, d.birth_date
FROM nemura.gov_leave g JOIN nemura.person p ON p.organisation_id = g.organisation_id AND p.id = g.person_id
JOIN nemura.person_dependant d ON d.organisation_id = g.organisation_id AND d.id = g.dependant_id ORDER BY g.claim_deadline, g.id`)
	if err != nil {
		return nil, err
	}
	var out []GovRow
	for rows.Next() {
		var r GovRow
		var version string
		var born *time.Time
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Scheme, &r.Key, &version, &r.From, &r.To, &r.Days, &r.Deadline, &born); err != nil {
			rows.Close()
			return nil, err
		}
		if pv, ok := v(version); ok && born != nil {
			if rule, err := leave.RuleOf(pv, r.Key, *born); err == nil {
				if m, ok := leave.MaxClaim(rule, r.Days); ok {
					r.GuideMaximum = &m
				}
			}
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		cr, err := tx.Query(ctx, `SELECT c.id, c.amount::numeric(20,2)::text, c.filed_on, coalesce(c.reference, ''), c.over_guide,
 coalesce((SELECT sum(e.amount) FROM nemura.gov_claim_event e WHERE e.claim_id = c.id AND e.kind = 'receipt'), 0)::numeric(20,2)::text,
 EXISTS (SELECT 1 FROM nemura.gov_claim_event e WHERE e.claim_id = c.id AND e.kind = 'rejection')
FROM nemura.gov_claim c WHERE c.gov_leave_id = $1 ORDER BY c.filed_on, c.id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for cr.Next() {
			var c ClaimRow
			var amount, received string
			if err := cr.Scan(&c.ID, &amount, &c.FiledOn, &c.Reference, &c.OverGuide, &received, &c.Rejected); err != nil {
				cr.Close()
				return nil, err
			}
			if c.Amount, err = leave.ParseCents(amount); err != nil {
				cr.Close()
				return nil, fmt.Errorf("claim %s: %w", c.ID, err)
			}
			if c.Received, err = leave.ParseCents(received); err != nil {
				cr.Close()
				return nil, fmt.Errorf("claim %s: %w", c.ID, err)
			}
			out[i].Claims = append(out[i].Claims, c)
		}
		cr.Close()
		if err := cr.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GovClaimFacts are the blocks and whether a claim was filed for each, for N.1 #10.
func GovClaimFacts(ctx context.Context, tx pgx.Tx) ([]checks.GovClaim, error) {
	rows, err := tx.Query(ctx, `SELECT g.person_id::text, g.scheme, g.claim_deadline, EXISTS (SELECT 1 FROM nemura.gov_claim c WHERE c.gov_leave_id = g.id) FROM nemura.gov_leave g`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checks.GovClaim
	for rows.Next() {
		var c checks.GovClaim
		if err := rows.Scan(&c.Person, &c.Scheme, &c.Deadline, &c.Filed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LeaveFacts are the requests that have not been decided and the balances below zero where the type forbids it, for N.1 #4.
func LeaveFacts(ctx context.Context, tx pgx.Tx) ([]checks.Waiting, []checks.Negative, error) {
	rows, err := tx.Query(ctx, "SELECT person_id::text, requested_at FROM nemura.leave_request WHERE state = 'pending'")
	if err != nil {
		return nil, nil, err
	}
	var waiting []checks.Waiting
	for rows.Next() {
		var w checks.Waiting
		if err := rows.Scan(&w.Person, &w.Requested); err != nil {
			rows.Close()
			return nil, nil, err
		}
		waiting = append(waiting, w)
	}
	rows.Close()
	nr, err := tx.Query(ctx, `SELECT person_id::text, type_key, leave_year, sum(amount)::text FROM nemura.leave_ledger GROUP BY person_id, type_key, leave_year
HAVING sum(amount) < 0 AND NOT bool_or(negative_ok)`)
	if err != nil {
		return nil, nil, err
	}
	defer nr.Close()
	var neg []checks.Negative
	for nr.Next() {
		var n checks.Negative
		if err := nr.Scan(&n.Person, &n.Type, &n.Year, &n.Left); err != nil {
			return nil, nil, err
		}
		neg = append(neg, n)
	}
	return waiting, neg, nr.Err()
}
