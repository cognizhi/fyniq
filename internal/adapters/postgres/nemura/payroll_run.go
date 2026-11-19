package nemura

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

func runError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM173":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "A pay run version keeps its figures and its digest: a change is the next version."}
		case "NM174":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "An override keeps its figure and its reason, and is withdrawn once."}
		case "23505":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "That month already has a run for that pay group: open it and calculate the next version."}
		case "23514":
			if strings.Contains(pg.ConstraintName, "segregation") {
				return &payroll.Refusal{Code: payroll.CodeRole, Message: "Nobody approves the run they prepared: another Payroll administrator or the Owner approves it."}
			}
		}
	}
	return err
}

func dec(s string) *apd.Decimal { return payroll.D(s) }

func monthEnd(month time.Time) time.Time { return month.AddDate(0, 1, -1) }

// payRec is one pay record of a person as the run reads it, with the days of the month it covers.
type payRec struct {
	Group     uuid.UUID
	From      time.Time
	To        *time.Time
	Basic     *apd.Decimal
	SHGFund   string
	SHGOptOut bool
	Items     []payroll.RecurringItem
}

// segmentDays is the calendar days a pay record covers in a month once the person's start and leaving days are taken into account (payroll.PartMonthMethod): from the later of the month's first day, the
// record's start and the person's start, to the earlier of the month's last day, the day before the record's end (an end is the first day of the next record) and the leaving day, both included.
func segmentDays(rec payRec, start time.Time, leaving *time.Time, month time.Time) int {
	from, to := month, monthEnd(month)
	if rec.From.After(from) {
		from = rec.From
	}
	if rec.To != nil && rec.To.AddDate(0, 0, -1).Before(to) {
		to = rec.To.AddDate(0, 0, -1)
	}
	if start.After(from) {
		from = start
	}
	if leaving != nil && leaving.Before(to) {
		to = *leaving
	}
	if to.Before(from) {
		return 0
	}
	return int(to.Sub(from).Hours()/24) + 1
}

// RunFact is a source row a pay item came from.
type RunFact struct{ Type, ID string }

// MonthBuild is a person's employee-month with the inputs it was built from.
type MonthBuild struct {
	Month   payroll.Month
	Sources map[string]RunFact // item key -> the record it came from
	Used    []usedInput        // the inputs the run would mark used when approved
	Flags   []payroll.Flag
}

type usedInput struct {
	Table string
	ID    uuid.UUID
}

// workingDays is the number of days of the month the person works by their pattern (Monday to Friday when none is set).
func workingDays(p leave.Pattern, set bool, month time.Time) int {
	n := 0
	for d := month; !d.After(monthEnd(month)); d = d.AddDate(0, 0, 1) {
		if !set && d.Weekday() != time.Saturday && d.Weekday() != time.Sunday || set && p.Works(d.Weekday()) {
			n++
		}
	}
	return n
}

// BuildMonth collects a person's employee-month for a run's month from the pay record, the one-off items, the unpaid leave and the approved overtime, each linked to its source
// (NM-PAY-02): a joiner's, a leaver's and a pay change's part month by the days each pay record covers (RND-PAY-RATIO). A fact the calculation needs and the record lacks refuses the run,
// naming the person and the field. An input is eligible when no run used it, or when this run did.
func BuildMonth(ctx context.Context, tx pgx.Tx, person, runID uuid.UUID, group uuid.UUID, month time.Time, pv pack.Version, hasPV bool) (MonthBuild, error) {
	var mb MonthBuild
	mb.Sources = map[string]RunFact{}
	p, err := PersonOn(ctx, tx, person, month)
	if err != nil {
		return mb, err
	}
	name := p.LegalName
	cls, ok, err := ClassificationOn(ctx, tx, person, monthEnd(month))
	if err != nil {
		return mb, err
	}
	if !ok {
		return mb, &payroll.Refusal{Code: payroll.CodeInvalid, Message: name + " has no legal classification: record it first, because CPF follows residency and the Employment Act's cover."}
	}
	m := payroll.Month{Month: month, Person: payroll.Person{Employee: cls.ContractType == "service", Residency: cls.Residency, PRStart: cls.PRStart}}
	if m.Person.Employee && cls.Residency != payroll.Foreigner {
		if cls.BirthDate == nil {
			return mb, &payroll.Refusal{Code: payroll.CodeInvalid, Message: name + " has no date of birth on their legal classification: record it first, because the CPF age band follows it."}
		}
		m.Person.BirthDate = *cls.BirthDate
	}
	if cls.BirthDate != nil {
		m.Person.BirthDate = *cls.BirthDate
	}
	var electKind string
	if err := tx.QueryRow(ctx, "SELECT kind FROM nemura.pr_election WHERE person_id = $1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)", person, day(monthEnd(month))).Scan(&electKind); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return mb, err
	}
	m.Person.JointElection = electKind != ""
	m.Person.FullEmployeeRate = electKind == "ff" // F/F: full rates for both, the full table's (sc_spr3)
	// The pay records of the month in this group, in order.
	rows, err := tx.Query(ctx, `SELECT id, pay_group_id, effective_from, effective_to, basic_monthly::text, coalesce(shg_fund, ''), shg_opt_out FROM nemura.person_pay
WHERE person_id = $1 AND pay_group_id = $2 AND effective_from <= $3 AND (effective_to IS NULL OR effective_to > $4) ORDER BY effective_from`, person, group, day(monthEnd(month)), day(month))
	if err != nil {
		return mb, err
	}
	var recs []payRec
	var ids []uuid.UUID
	for rows.Next() {
		var r payRec
		var id uuid.UUID
		var basic string
		if err := rows.Scan(&id, &r.Group, &r.From, &r.To, &basic, &r.SHGFund, &r.SHGOptOut); err != nil {
			rows.Close()
			return mb, err
		}
		r.Basic = dec(basic)
		recs = append(recs, r)
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return mb, err
	}
	days := monthEnd(month).Day()
	basic, itemSum := apd.New(0, 0), map[string]*payroll.Item{}
	dedSum := map[string]*payroll.Deduction{}
	var order []string
	for i, r := range recs {
		n := segmentDays(r, p.Start, p.LeavingDate, month)
		if n == 0 {
			continue
		}
		_, _ = ids, i
		part := payroll.Ratio(r.Basic, n, days)
		basic = addDec(basic, part)
		m.Person.SHGFund, m.Person.SHGOptOut = r.SHGFund, r.SHGOptOut
		irows, err := tx.Query(ctx, "SELECT key, name, kind, nature, amount::text, cpf_wages FROM nemura.person_pay_item WHERE pay_id = $1 ORDER BY key", ids[i])
		if err != nil {
			return mb, err
		}
		for irows.Next() {
			var it payroll.RecurringItem
			if err := irows.Scan(&it.Key, &it.Name, &it.Kind, &it.Nature, &it.Amount, &it.CPFWages); err != nil {
				irows.Close()
				return mb, err
			}
			amt := payroll.Ratio(dec(it.Amount), n, days)
			if it.Kind == payroll.KindAllowance {
				if cur, ok := itemSum[it.Key]; ok {
					cur.Amount = addDec(cur.Amount, amt)
				} else {
					itemSum[it.Key] = &payroll.Item{Key: it.Key, Name: it.Name, Nature: it.Nature, Amount: amt, CPFWages: it.CPFWages, Suggested: payroll.Ordinary}
					order = append(order, it.Key)
				}
			} else {
				kind := map[string]string{"accommodation": payroll.DedAccommodation, "cooperative": payroll.DedCooperative, "other_deduction": payroll.DedOther}[it.Nature]
				if cur, ok := dedSum[it.Key]; ok {
					cur.Amount = addDec(cur.Amount, amt)
				} else {
					dedSum[it.Key] = &payroll.Deduction{Key: it.Key, Kind: kind, Amount: amt}
				}
			}
		}
		irows.Close()
		if err := irows.Err(); err != nil {
			return mb, err
		}
	}
	if len(recs) == 0 || basic == nil {
		return mb, &payroll.Refusal{Code: payroll.CodeInvalid, Message: name + " has no pay record in this pay group for the month."}
	}
	m.Items = append(m.Items, payroll.Item{Key: "basic", Name: "Basic salary", Nature: "basic", Amount: basic, CPFWages: true, Suggested: payroll.Ordinary})
	mb.Sources["basic"] = RunFact{"person_pay", ids[0].String()}
	for _, k := range order {
		m.Items = append(m.Items, *itemSum[k])
		mb.Sources[k] = RunFact{"person_pay_item", k}
	}
	keys := make([]string, 0, len(dedSum))
	for k := range dedSum {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m.Deductions = append(m.Deductions, *dedSum[k])
	}
	// The one-off items of the month.
	orows, err := tx.Query(ctx, `SELECT id, key, name, nature, amount::text, cpf_wages, for_month, payable_on FROM nemura.pay_one_off WHERE person_id = $1 AND month = $2 AND (used_in_run IS NULL OR used_in_run = $3) ORDER BY key`, person, day(month), runID)
	if err != nil {
		return mb, err
	}
	for orows.Next() {
		var id uuid.UUID
		var key, nm, nature, amt string
		var cpfw bool
		var forM, payable *time.Time
		if err := orows.Scan(&id, &key, &nm, &nature, &amt, &cpfw, &forM, &payable); err != nil {
			orows.Close()
			return mb, err
		}
		it := payroll.Item{Key: key, Name: nm, Nature: nature, Amount: dec(amt), CPFWages: cpfw, Suggested: map[string]string{"bonus": payroll.Additional, "commission": payroll.Additional, "allowance": payroll.Ordinary, "other": "", payroll.NatureLeavePay: "", payroll.NatureNoticePay: "", payroll.NatureRetrenchment: ""}[nature]}
		if forM != nil {
			it.ForMonth = *forM
		}
		if payable != nil {
			it.PayableOn = *payable
		}
		m.Items = append(m.Items, it)
		mb.Sources[key] = RunFact{"pay_one_off", id.String()}
		mb.Used = append(mb.Used, usedInput{"pay_one_off", id})
	}
	orows.Close()
	if err := orows.Err(); err != nil {
		return mb, err
	}
	// Unpaid leave: the day's share of the month's working days by the person's pattern.
	pat, set, err := WorkingPatternOn(ctx, tx, person, monthEnd(month))
	if err != nil {
		return mb, err
	}
	wd := workingDays(pat, set, month)
	urows, err := tx.Query(ctx, `SELECT id, days::text, from_date FROM nemura.pay_input WHERE person_id = $1 AND kind = 'unpaid_leave' AND from_date >= $2 AND from_date <= $3 AND (used_in_run IS NULL OR used_in_run = $4) ORDER BY from_date, id`,
		person, day(month), day(monthEnd(month)), runID)
	if err != nil {
		return mb, err
	}
	for urows.Next() {
		var id uuid.UUID
		var d string
		var from time.Time
		if err := urows.Scan(&id, &d, &from); err != nil {
			urows.Close()
			return mb, err
		}
		monthly := recAt(recs, from)
		if monthly == nil || wd == 0 {
			continue
		}
		key := "unpaid-" + id.String()[:8]
		m.Items = append(m.Items, payroll.Item{Key: key, Name: "Unpaid leave", Nature: "unpaid_leave", Amount: negDec(payroll.RatioDays(monthly, dec(d), wd)), CPFWages: true, Suggested: payroll.Ordinary})
		mb.Sources[key] = RunFact{"pay_input", id.String()}
		mb.Used = append(mb.Used, usedInput{"pay_input", id})
	}
	urows.Close()
	if err := urows.Err(); err != nil {
		return mb, err
	}
	// Approved overtime: hours at the pack's multiple of the hourly basic rate; rest-day and public-holiday work is flagged for a one-off item, never guessed.
	trows, err := tx.Query(ctx, `SELECT id, kind, hours::text, work_date FROM nemura.overtime_entry WHERE person_id = $1 AND state = 'approved' AND work_date >= $2 AND work_date <= $3 AND (used_in_run IS NULL OR used_in_run = $4) ORDER BY work_date, id`,
		person, day(month), day(monthEnd(month)), runID)
	if err != nil {
		return mb, err
	}
	for trows.Next() {
		var id uuid.UUID
		var kind, hours string
		var on time.Time
		if err := trows.Scan(&id, &kind, &hours, &on); err != nil {
			trows.Close()
			return mb, err
		}
		if kind != "overtime" {
			mb.Flags = append(mb.Flags, payroll.Flag{Code: "OVERTIME_REST_DAY", Message: fmt.Sprintf("%s: work on %s (%s) is paid by a one-off item: add it for the month, then approve.", name, on.Format(time.DateOnly), kind)})
			continue
		}
		monthly := recAt(recs, on)
		weekly := apd.New(44, 0)
		if set && pat.HoursPerWeek > 0 {
			weekly = apd.New(int64(pat.HoursPerWeek), -1)
		}
		mult := apd.New(15, -1)
		if hasPV {
			if n, found := pack.PartIV(pv, "overtime_rate_hundredths", on); found {
				mult = apd.New(int64(n), -2)
			}
		}
		if monthly == nil {
			continue
		}
		key := "ot-" + id.String()[:8]
		m.Items = append(m.Items, payroll.Item{Key: key, Name: "Overtime", Nature: "overtime", Amount: payroll.OvertimePay(monthly, weekly, mult, dec(hours)), CPFWages: true, Suggested: payroll.Ordinary})
		mb.Sources[key] = RunFact{"overtime_entry", id.String()}
		mb.Used = append(mb.Used, usedInput{"overtime_entry", id})
	}
	trows.Close()
	if err := trows.Err(); err != nil {
		return mb, err
	}
	m.LastMonth = month.Month() == time.December || (p.LeavingDate != nil && !p.LeavingDate.Before(month) && !p.LeavingDate.After(monthEnd(month)))
	// A person leaving in the month is paid their final pay: the 50% limit on deductions does not apply (FR-PAY-16). A tax-clearance hold in force holds every dollar due (FR-PAY-10).
	m.Final = p.LeavingDate != nil && !p.LeavingDate.Before(month) && !p.LeavingDate.After(monthEnd(month))
	if hold, found, err := TaxHoldOf(ctx, tx, person); err != nil {
		return mb, err
	} else if found && hold.Held && !hold.From.After(monthEnd(month)) {
		m.HoldAll = true
	}
	mb.Month = m
	return mb, nil
}

func recAt(recs []payRec, on time.Time) *apd.Decimal {
	for _, r := range recs {
		if !r.From.After(on) && (r.To == nil || r.To.After(on)) {
			return r.Basic
		}
	}
	return nil
}

func addDec(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(60).Add(out, a, b); err != nil {
		panic(err)
	}
	return out
}

func negDec(a *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	out.Neg(a)
	return out
}

// YearToDateFor is the CPF wages this employer has counted for the person before the month: the imported year to date up to its last month, then the lines of the latest approved version of
// each month after it (FR-PAY-14). Known is false when a month in between has neither, which is ✖ for a month that pays additional wages. The months of this run are never counted.
func YearToDateFor(ctx context.Context, tx pgx.Tx, person uuid.UUID, month time.Time, p PersonRow) (payroll.YearToDate, error) {
	y := payroll.YearToDate{OW: apd.New(0, 0), AWSubject: apd.New(0, 0), AWPaid: apd.New(0, 0)}
	jan := time.Date(month.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	first := jan
	if p.Start.After(first) {
		first = time.Date(p.Start.Year(), p.Start.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	if !first.Before(month) {
		y.Known = true // nothing before the month to count
		return y, nil
	}
	from := first
	if imp, has, err := YTDOf(ctx, tx, person, month.Year()); err != nil {
		return y, err
	} else if has {
		y.OW, y.AWSubject, y.AWPaid = dec(imp.OW), dec(imp.AWSubject), dec(imp.AWPaid)
		from = imp.Through.AddDate(0, 1, 0)
	}
	rows, err := tx.Query(ctx, `SELECT r.month, l.ow_subject::text, l.aw_subject::text, l.aw_paid::text FROM nemura.pay_run_line l
JOIN nemura.pay_run_version v ON v.organisation_id = l.organisation_id AND v.id = l.version_id JOIN nemura.pay_run r ON r.organisation_id = v.organisation_id AND r.id = v.run_id
WHERE l.person_id = $1 AND r.month >= $2 AND r.month < $3 AND v.approved_by IS NOT NULL
AND v.version = (SELECT max(v2.version) FROM nemura.pay_run_version v2 WHERE v2.run_id = v.run_id AND v2.approved_by IS NOT NULL)`, person, day(from), day(month))
	if err != nil {
		return y, err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var m time.Time
		var ow, aw, paid string
		if err := rows.Scan(&m, &ow, &aw, &paid); err != nil {
			return y, err
		}
		have[m.Format("2006-01")] = true
		y.OW, y.AWSubject, y.AWPaid = addDec(y.OW, dec(ow)), addDec(y.AWSubject, dec(aw)), addDec(y.AWPaid, dec(paid))
	}
	if err := rows.Err(); err != nil {
		return y, err
	}
	y.Known = true
	for m := from; m.Before(month); m = m.AddDate(0, 1, 0) {
		if !have[m.Format("2006-01")] {
			y.Known = false
		}
	}
	return y, nil
}

// RunLine is one person's month in a version as stored and shown.
type RunLine struct {
	Person                                                                  uuid.UUID
	PersonName                                                              string
	Gross, OWSubject, AWSubject, AWPaid, CPFTotal, CPFEmployee, CPFEmployer string
	SDL, SHG, Deductions, Reimbursements, Held, Net, Disburse, EmployerCost string
	Items                                                                   []RunItem
	Working                                                                 []payroll.Step
	Flags                                                                   []payroll.Flag
	Overridden                                                              []string
	Computed                                                                map[string]string // the figures the engine worked out, where an override replaced one
}

// RunItem is a pay item of a line with its class and its source.
type RunItem struct {
	Key, Name, Class, Amount, Why, SourceType, SourceID, Nature string
	Mismatch                                                    bool
}

// RunTotals are a version's totals.
type RunTotals struct {
	Gross, CPFEmployee, CPFEmployer, SDLPayable, SHG, Deductions, Reimbursements, Net, Disburse, EmployerCost string
}

// OverrideRow is an override on a run.
type OverrideRow struct {
	ID         uuid.UUID
	Person     uuid.UUID
	PersonName string
	Field      string
	Value      string
	Reason     string
	CreatedAt  time.Time
}

// canonical is what a version's digest covers: the figures, the overrides and the packs, in an order that never varies.
type canonical struct {
	Month     string          `json:"month"`
	Group     string          `json:"group"`
	Packs     []string        `json:"packs"`
	Lines     []canonicalLine `json:"lines"`
	Overrides []string        `json:"overrides"`
}

type canonicalLine struct {
	Person string     `json:"person"`
	Figs   [15]string `json:"figures"`
	Flags  []string   `json:"flags"`
}

// CalculateRun calculates the next version of a run: every person in the group with pay for the month, from their pay record and the inputs, with the run's overrides applied, and stores it
// as a read-only version with a digest. The first version needs no reason; a version after an approved one is a correction and needs one (NM-PAY-09). A calculated version that is not
// approved is superseded by the new one. It returns the version and the number of ✖ and ⚠ flags; the audit event names the run, the version and the digest, never an amount.
func CalculateRun(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, runID uuid.UUID, reason string, stat []pack.Version, people pack.Version, hasPeople bool) (uuid.UUID, int, int, error) {
	// One calculation of a run at a time: a version is numbered from the one before.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "pay-run:"+runID.String()); err != nil {
		return uuid.Nil, 0, 0, err
	}
	var month time.Time
	var group uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT month, pay_group_id FROM nemura.pay_run WHERE id = $1", runID).Scan(&month, &group); errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, 0, &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such pay run."}
	} else if err != nil {
		return uuid.Nil, 0, 0, err
	}
	var prev *uuid.UUID
	var prevN int
	var prevState string
	err := tx.QueryRow(ctx, "SELECT id, version, state FROM nemura.pay_run_version WHERE run_id = $1 ORDER BY version DESC LIMIT 1", runID).Scan(&prev, &prevN, &prevState)
	if errors.Is(err, pgx.ErrNoRows) {
		prev = nil
	} else if err != nil {
		return uuid.Nil, 0, 0, err
	}
	reason = strings.TrimSpace(reason)
	if prev != nil && prevState != "calculated" && reason == "" {
		return uuid.Nil, 0, 0, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "A correction of an approved run needs a reason: say what changed, so the approver and the books know."}
	}
	if len(reason) > 500 {
		return uuid.Nil, 0, 0, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The reason is up to 500 characters."}
	}
	packs, err := statutoryFor(stat, month)
	if err != nil {
		return uuid.Nil, 0, 0, err
	}
	ids, err := groupMembers(ctx, tx, group, month)
	if err != nil {
		return uuid.Nil, 0, 0, err
	}
	if len(ids) == 0 {
		return uuid.Nil, 0, 0, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "No one in this pay group has pay for the month: record pay records first."}
	}
	overs, err := activeOverrides(ctx, tx, runID)
	if err != nil {
		return uuid.Nil, 0, 0, err
	}
	var results []payroll.Result
	var lines []RunLine
	var builds []MonthBuild
	for _, person := range ids {
		pr, err := PersonOn(ctx, tx, person, month)
		if err != nil {
			return uuid.Nil, 0, 0, err
		}
		mb, err := BuildMonth(ctx, tx, person, runID, group, month, people, hasPeople)
		if err != nil {
			return uuid.Nil, 0, 0, err
		}
		ytd, err := YearToDateFor(ctx, tx, person, month, pr)
		if err != nil {
			return uuid.Nil, 0, 0, err
		}
		mb.Month.YTD = ytd
		res, err := payroll.Calculate(mb.Month, packs)
		if errors.Is(err, payroll.ErrNeedsYearToDate) {
			return uuid.Nil, 0, 0, &payroll.Refusal{Code: payroll.CodeInvalid, Message: pr.LegalName + " is paid additional wages and has no year to date for this employer: import it from 1 January, then calculate."}
		}
		if err != nil {
			return uuid.Nil, 0, 0, fmt.Errorf("%s: %w", pr.LegalName, err)
		}
		computed := map[string]string{}
		for _, o := range overs[person] {
			computed[o.Field] = figureText(res, o.Field)
			res = payroll.Override{Field: o.Field, Value: dec(o.Value), Reason: o.Reason}.Apply(res)
		}
		for _, f := range mb.Flags {
			res.Flags = append(res.Flags, f)
		}
		results = append(results, res)
		builds = append(builds, mb)
		lines = append(lines, lineOf(person, pr.LegalName, res, mb, computed))
	}
	t := payroll.Total(results)
	digest, err := digestOf(month, group, results, lines, overs)
	if err != nil {
		return uuid.Nil, 0, 0, err
	}
	failing, warning := 0, 0
	for _, l := range lines {
		for _, f := range l.Flags {
			if f.Fail {
				failing++
			} else {
				warning++
			}
		}
	}
	id := uuid.Must(uuid.NewV7())
	n := prevN + 1
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_run_version (id, run_id, version, state, prepared_by, prepared_at, supersedes, reason, digest, pack_versions, people, flags_failing, flags_warning, gross, cpf_employee, cpf_employer, sdl_payable, shg,
 deductions, reimbursements, net, disburse, employer_cost) VALUES ($1, $2, $3, 'calculated', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::numeric, $14::numeric, $15::numeric, $16::numeric, $17::numeric, $18::numeric, $19::numeric, $20::numeric, $21::numeric, $22::numeric)`,
		id, runID, n, by, at, prev, orNil(reason), digest[:], results[0].PackVersions, len(results), failing, warning, t.Gross.Text('f'), t.EmployeeCPF.Text('f'), t.EmployerCPF.Text('f'), t.SDLPayable.Text('f'), t.SHG.Text('f'),
		t.Deductions.Text('f'), t.Reimbursements.Text('f'), t.Net.Text('f'), t.Disburse.Text('f'), t.EmployerCost.Text('f')); err != nil {
		return uuid.Nil, 0, 0, runError(err)
	}
	for _, l := range lines {
		items, _ := json.Marshal(l.Items)
		working, _ := json.Marshal(l.Working)
		flags, _ := json.Marshal(l.Flags)
		computed, _ := json.Marshal(l.computed())
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_run_line (version_id, person_id, gross, ow_subject, aw_subject, aw_paid, cpf_total, cpf_employee, cpf_employer, sdl, shg, deductions, reimbursements, held, net, disburse, employer_cost, items, working, flags, computed)
VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11::numeric, $12::numeric, $13::numeric, $14::numeric, $15::numeric, $16::numeric, $17::numeric, $18::jsonb, $19::jsonb, $20::jsonb, $21::jsonb)`,
			id, l.Person, l.Gross, l.OWSubject, l.AWSubject, l.AWPaid, l.CPFTotal, l.CPFEmployee, l.CPFEmployer, l.SDL, l.SHG, l.Deductions, l.Reimbursements, l.Held, l.Net, l.Disburse, l.EmployerCost, items, working, flags, computed); err != nil {
			return uuid.Nil, 0, 0, runError(err)
		}
	}
	if prev != nil && prevState == "calculated" {
		if _, err := tx.Exec(ctx, "UPDATE nemura.pay_run_version SET state = 'superseded' WHERE id = $1", *prev); err != nil {
			return uuid.Nil, 0, 0, runError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.pay_run_version.superseded", audit.Object{Type: "pay_run_version", ID: *prev}, map[string]audit.Change{"state": {"calculated", "superseded"}}, ""); err != nil {
			return uuid.Nil, 0, 0, err
		}
	}
	return id, failing, warning, Audit(ctx, tx, org, at, user(by), "nemura.pay_run_version.calculated", audit.Object{Type: "pay_run_version", ID: id},
		map[string]audit.Change{"run": {nil, runID.String()}, "version": {nil, n}, "digest": {nil, fmt.Sprintf("%x", digest)}, "people": {nil, len(results)}, "failing": {nil, failing}, "warning": {nil, warning}}, reason)
}

func (l RunLine) computed() map[string]string {
	return l.Computed
}

func figureText(r payroll.Result, field string) string {
	switch field {
	case payroll.FieldCPFEmployee:
		return r.CPF.Employee.Text('f')
	case payroll.FieldCPFEmployer:
		return r.CPF.Employer.Text('f')
	case payroll.FieldSDL:
		return r.SDL.Text('f')
	}
	return r.SHG.Text('f')
}

func lineOf(person uuid.UUID, name string, r payroll.Result, mb MonthBuild, computed map[string]string) RunLine {
	cents := func(d *apd.Decimal) string {
		out := new(apd.Decimal)
		c := apd.BaseContext.WithPrecision(60)
		c.Rounding = apd.RoundHalfUp
		if _, err := c.Quantize(out, d, -2); err != nil {
			panic(err)
		}
		return out.Text('f')
	}
	l := RunLine{Person: person, PersonName: name, Gross: cents(r.Gross), OWSubject: cents(r.OWSubject), AWSubject: cents(r.AWSubject), AWPaid: cents(r.AdditionalWages), CPFTotal: cents(r.CPF.Total), CPFEmployee: cents(r.CPF.Employee),
		CPFEmployer: cents(r.CPF.Employer), SDL: cents(r.SDL), SHG: cents(r.SHG), Deductions: cents(r.Deductions), Reimbursements: cents(r.Reimbursements), Held: cents(r.Held), Net: cents(r.Net), Disburse: cents(r.Disburse),
		EmployerCost: cents(r.EmployerCost), Working: r.Working, Flags: r.Flags, Overridden: r.Overridden, Computed: computed}
	for _, it := range r.Items {
		src := mb.Sources[it.Key]
		l.Items = append(l.Items, RunItem{Key: it.Key, Name: it.Name, Class: it.Class, Amount: cents(it.Amount), Why: it.Why, Mismatch: it.Mismatch, SourceType: src.Type, SourceID: src.ID, Nature: it.Nature})
	}
	for _, d := range mb.Month.Deductions {
		src := mb.Sources[d.Key]
		l.Items = append(l.Items, RunItem{Key: d.Key, Name: "Deduction: " + d.Kind, Class: "deduction", Amount: cents(d.Amount), SourceType: "person_pay_item", SourceID: d.Key + "", Why: src.Type})
	}
	return l
}

func digestOf(month time.Time, group uuid.UUID, rs []payroll.Result, lines []RunLine, overs map[uuid.UUID][]OverrideRow) ([32]byte, error) {
	c := canonical{Month: day(month), Group: group.String(), Packs: rs[0].PackVersions}
	sorted := append([]RunLine(nil), lines...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Person.String() < sorted[j].Person.String() })
	for _, l := range sorted {
		cl := canonicalLine{Person: l.Person.String(), Figs: [15]string{l.Gross, l.OWSubject, l.AWSubject, l.AWPaid, l.CPFTotal, l.CPFEmployee, l.CPFEmployer, l.SDL, l.SHG, l.Deductions, l.Reimbursements, l.Held, l.Net, l.Disburse, l.EmployerCost}}
		for _, f := range l.Flags {
			cl.Flags = append(cl.Flags, f.Code)
		}
		c.Lines = append(c.Lines, cl)
	}
	for person, list := range overs {
		for _, o := range list {
			c.Overrides = append(c.Overrides, person.String()+"|"+o.Field+"|"+o.Value+"|"+o.Reason)
		}
	}
	sort.Strings(c.Overrides)
	raw, err := json.Marshal(c)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// statutoryFor is the CPF, SDL and SHG packs in force for a wage month.
func statutoryFor(all []pack.Version, month time.Time) (payroll.Packs, error) {
	var p payroll.Packs
	pick := func(name string) (pack.Version, bool) {
		var best pack.Version
		found := false
		for _, v := range all {
			if v.Pack == name && !v.EffectiveFrom.After(month) && (v.EffectiveTo == nil || !v.EffectiveTo.Before(month)) && (!found || v.ID > best.ID) {
				best, found = v, true
			}
		}
		return best, found
	}
	var ok1, ok2, ok3 bool
	p.CPF, ok1 = pick("sg-cpf")
	p.SDL, ok2 = pick("sg-sdl")
	p.SHG, ok3 = pick("sg-shg")
	if !ok1 || !ok2 || !ok3 {
		return p, &payroll.Refusal{Code: payroll.CodeInvalid, Message: fmt.Sprintf("No statutory pack covers %s: a pack version for the month is needed before pay is calculated.", month.Format("January 2006"))}
	}
	return p, nil
}

func groupMembers(ctx context.Context, tx pgx.Tx, group uuid.UUID, month time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT pp.person_id FROM nemura.person_pay pp JOIN nemura.person p ON p.organisation_id = pp.organisation_id AND p.id = pp.person_id
WHERE pp.pay_group_id = $1 AND pp.effective_from <= $3 AND (pp.effective_to IS NULL OR pp.effective_to > $2) AND p.employment_start <= $3 AND (p.leaving_date IS NULL OR p.leaving_date >= $2) ORDER BY pp.person_id`, group, day(month), day(monthEnd(month)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func activeOverrides(ctx context.Context, tx pgx.Tx, run uuid.UUID) (map[uuid.UUID][]OverrideRow, error) {
	rows, err := tx.Query(ctx, `SELECT o.id, o.person_id, p.legal_name, o.field, o.value::numeric(20,2)::text, o.reason, o.created_at FROM nemura.pay_run_override o
JOIN nemura.person p ON p.organisation_id = o.organisation_id AND p.id = o.person_id WHERE o.run_id = $1 AND o.withdrawn_at IS NULL ORDER BY o.created_at, o.id`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]OverrideRow{}
	for rows.Next() {
		var r OverrideRow
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Field, &r.Value, &r.Reason, &r.CreatedAt); err != nil {
			return nil, err
		}
		out[r.Person] = append(out[r.Person], r)
	}
	return out, rows.Err()
}

// StartRun makes the run of a month for a pay group, once (NM-PAY-09: a correction is a version of the same run).
func StartRun(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, group uuid.UUID, month time.Time) (uuid.UUID, error) {
	if month.Day() != 1 {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The wage month is the first day of a month: write it as 2026-06-01."}
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.pay_group WHERE id = $1)", group).Scan(&exists); err != nil {
		return uuid.Nil, err
	}
	if !exists {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The pay group is not one of this company's: choose one from the list."}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.pay_run (id, month, pay_group_id, created_by, created_at) VALUES ($1, $2, $3, $4, $5)", id, day(month), group, by, at); err != nil {
		return uuid.Nil, runError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.pay_run.started", audit.Object{Type: "pay_run", ID: id}, map[string]audit.Change{"month": {nil, day(month)}, "group": {nil, group.String()}}, "")
}

// AddOverride replaces one calculated figure of a person's month in the run, with a reason (FR-PAY-06); the next calculation applies it and flags it. The audit event names the field and
// the person, never the figure.
func AddOverride(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, run, person uuid.UUID, o payroll.Override) (uuid.UUID, error) {
	if err := o.Check(); err != nil {
		return uuid.Nil, err
	}
	var open bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.pay_run WHERE id = $1)", run).Scan(&open); err != nil || !open {
		if err == nil {
			err = &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such pay run."}
		}
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.pay_run_override (id, run_id, person_id, field, value, reason, created_by, created_at) VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8)",
		id, run, person, o.Field, o.Value.Text('f'), strings.TrimSpace(o.Reason), by, at); err != nil {
		return uuid.Nil, runError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.pay_run_override.added", audit.Object{Type: "pay_run_override", ID: id}, map[string]audit.Change{"run": {nil, run.String()}, "person": {nil, person.String()}, "field": {nil, o.Field}}, "")
}

// WithdrawOverride ends an override from the next calculation.
func WithdrawOverride(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.pay_run_override SET withdrawn_at = $2 WHERE id = $1 AND withdrawn_at IS NULL", id, at)
	if err != nil {
		return runError(err)
	}
	if tag.RowsAffected() != 1 {
		return &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such override that is still in force."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.pay_run_override.withdrawn", audit.Object{Type: "pay_run_override", ID: id}, map[string]audit.Change{"withdrawn": {false, true}}, "")
}

// RunRow is a run as stored.
type RunRow struct {
	ID, Group uuid.UUID
	GroupName string
	Month     time.Time
	PayDate   time.Time
}

// VersionRow is a version as stored.
type VersionRow struct {
	ID, PreparedBy                   uuid.UUID
	ApprovedBy                       *uuid.UUID
	Number, People, Failing, Warning int
	State, Reason                    string
	Digest                           []byte
	Packs                            []string
	PreparedAt                       time.Time
	ApprovedAt                       *time.Time
	Totals                           RunTotals
}

// RunIDs lists a month's runs.
func RunIDs(ctx context.Context, tx pgx.Tx, month time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.pay_run WHERE month = $1 ORDER BY created_at, id", day(month))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// RunOf is a run with its versions, newest first.
func RunOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (RunRow, []VersionRow, error) {
	var r RunRow
	var payDay, cutoff, approval int
	err := tx.QueryRow(ctx, "SELECT r.id, r.month, r.pay_group_id, g.name, g.pay_day, g.cutoff_days, g.approval_days FROM nemura.pay_run r JOIN nemura.pay_group g ON g.organisation_id = r.organisation_id AND g.id = r.pay_group_id WHERE r.id = $1", id).
		Scan(&r.ID, &r.Month, &r.Group, &r.GroupName, &payDay, &cutoff, &approval)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil, &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such pay run."}
	}
	if err != nil {
		return r, nil, err
	}
	r.PayDate = payroll.PayGroup{PayDay: payDay, CutoffDays: cutoff, ApprovalDays: approval}.PayDate(r.Month)
	rows, err := tx.Query(ctx, `SELECT id, version, state, prepared_by, prepared_at, approved_by, approved_at, coalesce(reason, ''), digest, pack_versions, people, flags_failing, flags_warning,
 gross::numeric(20,2)::text, cpf_employee::numeric(20,2)::text, cpf_employer::numeric(20,2)::text, sdl_payable::numeric(20,2)::text, shg::numeric(20,2)::text, deductions::numeric(20,2)::text,
 reimbursements::numeric(20,2)::text, net::numeric(20,2)::text, disburse::numeric(20,2)::text, employer_cost::numeric(20,2)::text FROM nemura.pay_run_version WHERE run_id = $1 ORDER BY version DESC`, id)
	if err != nil {
		return r, nil, err
	}
	defer rows.Close()
	var out []VersionRow
	for rows.Next() {
		var v VersionRow
		if err := rows.Scan(&v.ID, &v.Number, &v.State, &v.PreparedBy, &v.PreparedAt, &v.ApprovedBy, &v.ApprovedAt, &v.Reason, &v.Digest, &v.Packs, &v.People, &v.Failing, &v.Warning,
			&v.Totals.Gross, &v.Totals.CPFEmployee, &v.Totals.CPFEmployer, &v.Totals.SDLPayable, &v.Totals.SHG, &v.Totals.Deductions, &v.Totals.Reimbursements, &v.Totals.Net, &v.Totals.Disburse, &v.Totals.EmployerCost); err != nil {
			return r, nil, err
		}
		out = append(out, v)
	}
	return r, out, rows.Err()
}

// RunLines are a version's lines in the order of the people's names.
func RunLines(ctx context.Context, tx pgx.Tx, version uuid.UUID) ([]RunLine, error) {
	rows, err := tx.Query(ctx, `SELECT l.person_id, p.legal_name, l.gross::numeric(20,2)::text, l.ow_subject::numeric(20,2)::text, l.aw_subject::numeric(20,2)::text, l.aw_paid::numeric(20,2)::text, l.cpf_total::numeric(20,2)::text,
 l.cpf_employee::numeric(20,2)::text, l.cpf_employer::numeric(20,2)::text, l.sdl::numeric(20,2)::text, l.shg::numeric(20,2)::text, l.deductions::numeric(20,2)::text, l.reimbursements::numeric(20,2)::text, l.held::numeric(20,2)::text,
 l.net::numeric(20,2)::text, l.disburse::numeric(20,2)::text, l.employer_cost::numeric(20,2)::text, l.items, l.working, l.flags, l.computed
FROM nemura.pay_run_line l JOIN nemura.person p ON p.organisation_id = l.organisation_id AND p.id = l.person_id WHERE l.version_id = $1 ORDER BY p.legal_name, l.person_id`, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunLine
	for rows.Next() {
		var l RunLine
		var items, working, flags, computed []byte
		if err := rows.Scan(&l.Person, &l.PersonName, &l.Gross, &l.OWSubject, &l.AWSubject, &l.AWPaid, &l.CPFTotal, &l.CPFEmployee, &l.CPFEmployer, &l.SDL, &l.SHG, &l.Deductions, &l.Reimbursements, &l.Held,
			&l.Net, &l.Disburse, &l.EmployerCost, &items, &working, &flags, &computed); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(items, &l.Items)
		_ = json.Unmarshal(working, &l.Working)
		_ = json.Unmarshal(flags, &l.Flags)
		_ = json.Unmarshal(computed, &l.Computed)
		for k := range l.Computed {
			l.Overridden = append(l.Overridden, k)
		}
		sort.Strings(l.Overridden)
		out = append(out, l)
	}
	return out, rows.Err()
}

// ApproveRun approves the newest version of a run (FR-PAY-07, NM-ROLE-04): it is calculated and not superseded, it has no ✖, the approver is not the person who prepared it (the database
// refuses it too), is the Payroll administrator or the Owner (the API checks the role), and confirms the digest they read with a passkey in the last five minutes (stepUpAt). The approval is written once with
// what the passkey confirmed; every older approved version of the run is superseded; the inputs the version used (one-off items, unpaid leave, overtime) are marked used by the run, once. The
// audit event names the run, the version and the digest, never an amount.
func ApproveRun(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, runID uuid.UUID, version int, confirmed [32]byte, stepUpAt time.Time) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "pay-run:"+runID.String()); err != nil {
		return err
	}
	var id, preparer uuid.UUID
	var state string
	var failing int
	var digest []byte
	var latest int
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) FROM nemura.pay_run_version WHERE run_id = $1", runID).Scan(&latest); err != nil {
		return err
	}
	err := tx.QueryRow(ctx, "SELECT id, state, prepared_by, flags_failing, digest FROM nemura.pay_run_version WHERE run_id = $1 AND version = $2", runID, version).Scan(&id, &state, &preparer, &failing, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such version of that pay run."}
	}
	if err != nil {
		return err
	}
	if version != latest || state != "calculated" {
		return &payroll.Refusal{Code: payroll.CodeConflict, Message: "Only the newest version, while it is calculated, is approved: calculate the next version, or it is approved already."}
	}
	if failing > 0 {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: fmt.Sprintf("The version has %d failing checks (✖): resolve each, calculate the next version, then approve it.", failing)}
	}
	if preparer == by {
		return &payroll.Refusal{Code: payroll.CodeRole, Message: "Nobody approves the run they prepared: another Payroll administrator or the Owner approves it."}
	}
	if string(digest) != string(confirmed[:]) {
		return &payroll.Refusal{Code: payroll.CodeConflict, Message: "The digest you confirmed is not this version's: open the run again and confirm the one shown."}
	}
	envelope, _ := json.Marshal(map[string]any{"run": runID.String(), "version": version, "digest": fmt.Sprintf("%x", digest), "approver": by.String(), "step_up_at": stepUpAt.UTC().Format(time.RFC3339), "approved_at": at.UTC().Format(time.RFC3339)})
	if _, err := tx.Exec(ctx, "UPDATE nemura.pay_run_version SET state = 'approved', approved_by = $2, approved_at = $3, approved_digest = $4, envelope = $5::jsonb WHERE id = $1", id, by, at, digest, string(envelope)); err != nil {
		return runError(err)
	}
	older, err := tx.Query(ctx, "SELECT id FROM nemura.pay_run_version WHERE run_id = $1 AND version < $2 AND state = 'approved'", runID, version)
	if err != nil {
		return err
	}
	var superseded []uuid.UUID
	for older.Next() {
		var o uuid.UUID
		if err := older.Scan(&o); err != nil {
			older.Close()
			return err
		}
		superseded = append(superseded, o)
	}
	older.Close()
	for _, o := range superseded {
		if _, err := tx.Exec(ctx, "UPDATE nemura.pay_run_version SET state = 'superseded' WHERE id = $1", o); err != nil {
			return runError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.pay_run_version.superseded", audit.Object{Type: "pay_run_version", ID: o}, map[string]audit.Change{"state": {"approved", "superseded"}}, "corrected by version "+fmt.Sprint(version)); err != nil {
			return err
		}
	}
	// The inputs this version used are used by the run, once.
	rows, err := tx.Query(ctx, `SELECT DISTINCT it->>'SourceType', it->>'SourceID' FROM nemura.pay_run_line l, jsonb_array_elements(l.items) it WHERE l.version_id = $1 AND it->>'SourceType' IN ('pay_one_off', 'pay_input', 'overtime_entry')`, id)
	if err != nil {
		return err
	}
	var used []usedInput
	for rows.Next() {
		var table, src string
		if err := rows.Scan(&table, &src); err != nil {
			rows.Close()
			return err
		}
		u, err := uuid.Parse(src)
		if err != nil {
			rows.Close()
			return err
		}
		used = append(used, usedInput{table, u})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range used {
		tag, err := tx.Exec(ctx, "UPDATE nemura."+u.Table+" SET used_in_run = $2, used_at = $3 WHERE id = $1 AND (used_in_run IS NULL OR used_in_run = $2)", u.ID, runID, at)
		if err != nil {
			return runError(err)
		}
		if tag.RowsAffected() != 1 {
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "An input of this version was used by another run meanwhile: calculate the next version."}
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura."+u.Table+".used", audit.Object{Type: u.Table, ID: u.ID}, map[string]audit.Change{"run": {nil, runID.String()}}, ""); err != nil {
			return err
		}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.pay_run_version.approved", audit.Object{Type: "pay_run_version", ID: id},
		map[string]audit.Change{"run": {nil, runID.String()}, "version": {nil, version}, "digest": {nil, fmt.Sprintf("%x", digest)}}, "")
}

// ChangesFor sets the newest version of a run against the previous month's approved run of the same pay group (NM-PAY-11); with none before, it says so.
func ChangesFor(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (payroll.Comparison, bool, error) {
	r, vs, err := RunOf(ctx, tx, runID)
	if err != nil {
		return payroll.Comparison{}, false, err
	}
	if len(vs) == 0 {
		return payroll.Comparison{}, false, nil
	}
	now, err := lineFacts(ctx, tx, vs[0].ID)
	if err != nil {
		return payroll.Comparison{}, false, err
	}
	var prevVersion uuid.UUID
	err = tx.QueryRow(ctx, `SELECT v.id FROM nemura.pay_run pr JOIN nemura.pay_run_version v ON v.organisation_id = pr.organisation_id AND v.run_id = pr.id
WHERE pr.pay_group_id = $1 AND pr.month = $2 AND v.approved_by IS NOT NULL ORDER BY v.version DESC LIMIT 1`, r.Group, day(r.Month.AddDate(0, -1, 0))).Scan(&prevVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return payroll.Comparison{}, false, nil
	}
	if err != nil {
		return payroll.Comparison{}, false, err
	}
	prev, err := lineFacts(ctx, tx, prevVersion)
	if err != nil {
		return payroll.Comparison{}, false, err
	}
	return payroll.Compare(now, prev), true, nil
}

func lineFacts(ctx context.Context, tx pgx.Tx, version uuid.UUID) ([]payroll.LineFacts, error) {
	lines, err := RunLines(ctx, tx, version)
	if err != nil {
		return nil, err
	}
	out := make([]payroll.LineFacts, len(lines))
	for i, l := range lines {
		f := payroll.LineFacts{Person: l.Person.String(), Name: l.PersonName, Gross: dec(l.Gross), Net: dec(l.Net), CPF: addDec(dec(l.CPFEmployee), dec(l.CPFEmployer)), Cost: dec(l.EmployerCost)}
		for _, it := range l.Items {
			amt := dec(it.Amount)
			if it.Class == "deduction" {
				amt = negDec(amt)
			}
			f.Items = append(f.Items, payroll.ItemFacts{Key: it.Key, SourceType: it.SourceType, Class: it.Class, Amount: amt})
		}
		out[i] = f
	}
	return out, nil
}
