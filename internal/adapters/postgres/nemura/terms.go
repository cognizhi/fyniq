package nemura

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
)

// Terms are a person's key employment terms that the people record does not hold (NM-LTR-06): the base terms HR records and the pay terms the Payroll administrator or the
// Owner records (owner decision O3, 20 Nov 2026). A pay field that is empty is not recorded.
type Terms struct {
	From                                          time.Time
	To                                            *time.Time
	MainDuties, NoticePeriod, MedicalBenefits     string
	ContractEnd                                   *time.Time
	SalaryPeriod, BasicSalary                     string // the period and the SGD per period as a decimal string
	FixedAllowances, FixedDeductions, OvertimePay string
	OtherPay                                      string
}

func termsError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM163":
			return &letters.Refusal{Code: letters.CodeConflict, Message: "A record of terms is history: change it by starting a new one from a later day."}
		case "NM164":
			return &letters.Refusal{Code: letters.CodeConflict, Message: "That day overlaps terms the person already has: start the new ones on or after the day the last end."}
		}
	}
	return err
}

// TermsOn is a person's terms on a day, or false.
func TermsOn(ctx context.Context, tx pgx.Tx, person uuid.UUID, on time.Time) (Terms, bool, error) {
	var t Terms
	var salary *string
	var period, allow, ded, ot, other, med *string
	err := tx.QueryRow(ctx, `SELECT effective_from, effective_to, main_duties, contract_end, salary_period, basic_salary::numeric(20,2)::text, fixed_allowances, fixed_deductions, overtime_period, other_pay, medical_benefits, notice_period
FROM nemura.person_terms WHERE person_id = $1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, person, day(on)).
		Scan(&t.From, &t.To, &t.MainDuties, &t.ContractEnd, &period, &salary, &allow, &ded, &ot, &other, &med, &t.NoticePeriod)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	t.SalaryPeriod, t.BasicSalary, t.FixedAllowances, t.FixedDeductions, t.OvertimePay, t.OtherPay, t.MedicalBenefits = deref(period), deref(salary), deref(allow), deref(ded), deref(ot), deref(other), deref(med)
	return t, true, nil
}

// SetTerms starts a person's terms on a day, ending the current ones the day before (NM-LTR-06). writeBase and writePay say which parts the caller may write: the base terms
// are HR's, the pay terms the Payroll administrator's and the Owner's; a part the caller may not write is carried forward from the current terms, and terms cannot be started
// with no base terms recorded.
func SetTerms(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, t Terms, writeBase, writePay bool) error {
	cur, ok, err := TermsOn(ctx, tx, person, t.From)
	if err != nil {
		return err
	}
	if !writeBase {
		if !ok {
			return &letters.Refusal{Code: letters.CodeConflict, Message: "The base terms (the main duties and the notice period) are recorded first, by the people team: then the pay terms follow."}
		}
		t.MainDuties, t.NoticePeriod, t.MedicalBenefits, t.ContractEnd = cur.MainDuties, cur.NoticePeriod, cur.MedicalBenefits, cur.ContractEnd
	}
	if !writePay && ok {
		t.SalaryPeriod, t.BasicSalary, t.FixedAllowances, t.FixedDeductions, t.OvertimePay, t.OtherPay = cur.SalaryPeriod, cur.BasicSalary, cur.FixedAllowances, cur.FixedDeductions, cur.OvertimePay, cur.OtherPay
	}
	if !writePay && !ok {
		t.SalaryPeriod, t.BasicSalary, t.FixedAllowances, t.FixedDeductions, t.OvertimePay, t.OtherPay = "", "", "", "", "", ""
	}
	if strings.TrimSpace(t.MainDuties) == "" || strings.TrimSpace(t.NoticePeriod) == "" || len(t.MainDuties) > 1000 || len(t.NoticePeriod) > 300 {
		return &letters.Refusal{Code: letters.CodeInvalid, Message: "The main duties (up to 1,000 characters) and the notice period (up to 300) are required: write them as the contract does."}
	}
	if (t.SalaryPeriod == "") != (t.BasicSalary == "") {
		return &letters.Refusal{Code: letters.CodeInvalid, Message: "The salary period and the basic salary go together: give both, or neither."}
	}
	var salary any
	if t.BasicSalary != "" {
		c, err := leave.ParseCents(t.BasicSalary)
		if err != nil || c <= 0 {
			return &letters.Refusal{Code: letters.CodeInvalid, Message: "The basic salary is more than zero, in dollars with at most two decimal places: write it as 4500.00."}
		}
		salary = c.String()
	}
	if ok {
		if !t.From.After(cur.From) {
			return &letters.Refusal{Code: letters.CodeConflict, Message: "The new terms start after the current ones began: choose a later day."}
		}
		var open uuid.UUID
		if err := tx.QueryRow(ctx, "UPDATE nemura.person_terms SET effective_to = $2 WHERE person_id = $1 AND effective_to IS NULL AND effective_from < $2 RETURNING id", person, day(t.From)).Scan(&open); err != nil {
			return termsError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.person_terms.ended", audit.Object{Type: "person_terms", ID: open}, map[string]audit.Change{"effective_to": {nil, day(t.From)}}, ""); err != nil {
			return err
		}
	}
	id := uuid.Must(uuid.NewV7())
	var end any
	if t.ContractEnd != nil {
		end = day(*t.ContractEnd)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_terms (id, person_id, effective_from, main_duties, contract_end, salary_period, basic_salary, fixed_allowances, fixed_deductions, overtime_period, other_pay, medical_benefits, notice_period, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8, $9, $10, $11, $12, $13, $14, $15)`, id, person, day(t.From), t.MainDuties, end, orNil(t.SalaryPeriod), salary, orNil(t.FixedAllowances), orNil(t.FixedDeductions),
		orNil(t.OvertimePay), orNil(t.OtherPay), orNil(t.MedicalBenefits), t.NoticePeriod, by, at); err != nil {
		return termsError(err)
	}
	// The audit event names what changed and never the pay: it says only that pay terms were set.
	return Audit(ctx, tx, org, at, user(by), "nemura.person_terms.set", audit.Object{Type: "person_terms", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "effective_from": {nil, day(t.From)},
		"base": {nil, writeBase}, "pay": {nil, writePay && t.BasicSalary != ""}}, "")
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func money(s string) string {
	if s == "" {
		return ""
	}
	c, err := leave.ParseCents(s)
	if err != nil {
		return s
	}
	whole, frac := int64(c)/100, int64(c)%100
	digits := fmt.Sprintf("%d", whole)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return fmt.Sprintf("S$%s.%02d", digits, frac)
}

// ketValues are the merge fields of the key employment terms for a person on a day: from their terms, working pattern, classification, probation and the country pack. A fact
// that is not recorded is left out so filling names it, and an item that does not apply says so in words.
func ketValues(ctx context.Context, tx pgx.Tx, v pack.Version, person uuid.UUID, at time.Time) (letters.Values, error) {
	out := letters.Values{}
	t, ok, err := TermsOn(ctx, tx, person, at)
	if err != nil {
		return nil, err
	}
	if !ok {
		return out, nil
	}
	out["terms.main_duties"] = t.MainDuties
	out["terms.notice_period"] = t.NoticePeriod
	out["terms.medical_benefits"] = orDefault(t.MedicalBenefits, "None")
	if t.ContractEnd != nil {
		out["terms.contract_duration"] = "A fixed-term contract until " + day(*t.ContractEnd) + "."
	} else {
		out["terms.contract_duration"] = "Not applicable: the contract is not for a fixed term."
	}
	if t.SalaryPeriod != "" {
		out["terms.salary_period"] = t.SalaryPeriod
		out["terms.basic_salary"] = money(t.BasicSalary) + " a " + strings.TrimSuffix(t.SalaryPeriod, "ly") + periodWord(t.SalaryPeriod)
		out["terms.fixed_allowances"] = orDefault(t.FixedAllowances, "None")
		out["terms.fixed_deductions"] = orDefault(t.FixedDeductions, "None")
		out["terms.other_pay"] = orDefault(t.OtherPay, "None")
		out["terms.overtime_period"] = orDefault(t.OvertimePay, "The same as the salary period.")
	}
	c, cok, err := ClassificationOn(ctx, tx, person, at)
	if err != nil {
		return nil, err
	}
	if cok {
		out["terms.overtime_rate"] = overtimeRate(v, c.EACovered, c.PartIVClass, t, at)
	}
	if pat, found, err := WorkingPatternOn(ctx, tx, person, at); err != nil {
		return nil, err
	} else if found {
		names := map[int]string{1: "Monday", 2: "Tuesday", 3: "Wednesday", 4: "Thursday", 5: "Friday", 6: "Saturday", 7: "Sunday"}
		var work, rest []string
		for d := 1; d <= 7; d++ {
			if pat.Works(time.Weekday(d % 7)) {
				work = append(work, names[d])
			} else {
				rest = append(rest, names[d])
			}
		}
		perDay := pat.HoursPerWeek * 10 / pat.DaysPerWeek()
		out["terms.working_arrangement"] = fmt.Sprintf("%d.%d hours a week over %d days (%s), about %s hours a day; rest days: %s.", pat.HoursPerWeek/10, pat.HoursPerWeek%10, pat.DaysPerWeek(), strings.Join(work, ", "),
			leave.Days(perDay).String(), strings.Join(rest, ", "))
	}
	p, err := PersonOn(ctx, tx, person, at)
	if err != nil {
		return nil, err
	}
	if p.ProbationEnd != nil {
		out["terms.probation"] = "The probation period ends on " + day(*p.ProbationEnd) + "."
	} else {
		out["terms.probation"] = "There is no probation period."
	}
	if a1, ok1 := pack.AnnualLeaveDays(v, 1, at); ok1 {
		a8, _ := pack.AnnualLeaveDays(v, 8, at)
		sick := ""
		if o, h, okS := pack.SickLeave(v, 6, at); okS {
			sick = fmt.Sprintf(" Paid sick leave starts after 3 months' service and is %d days outpatient and %d days in all (outpatient included) from 6 months.", o, h)
		}
		out["terms.leave"] = fmt.Sprintf("Annual leave is %d days in the first year of service, one more day each year, to %d days from the eighth year.%s Other leave follows the Employment Act and the company's policy.", a1, a8, sick)
	}
	return out, nil
}

func periodWord(p string) string {
	switch p {
	case "monthly":
		return "month"
	case "weekly":
		return "week"
	case "fortnightly":
		return "fortnight"
	case "daily":
		return "day"
	case "hourly":
		return "hour"
	}
	return p
}

// KETFacts are the people who started 14 or more days ago and have no issued key employment terms, for N.1 #1: employees covered by the Employment Act on a contract of service
// whose employment lasts 14 days or more.
func KETFacts(ctx context.Context, tx pgx.Tx) ([]checks.KETDue, error) {
	rows, err := tx.Query(ctx, `SELECT p.id::text, p.employment_start FROM nemura.person p
JOIN nemura.person_classification c ON c.organisation_id = p.organisation_id AND c.person_id = p.id AND c.effective_from <= p.employment_start AND (c.effective_to IS NULL OR c.effective_to > p.employment_start)
WHERE c.ea_covered AND c.contract_type = 'service' AND (p.leaving_date IS NULL OR p.leaving_date >= p.employment_start + 13)
AND NOT EXISTS (SELECT 1 FROM nemura.letter_issued i WHERE i.organisation_id = p.organisation_id AND i.person_id = p.id AND i.type_key = 'kets')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checks.KETDue
	for rows.Next() {
		var d checks.KETDue
		if err := rows.Scan(&d.Person, &d.Start); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// overtimeRate says what the employment is owed for overtime (Part IV of the Employment Act): a workman earning a basic salary up to the pack's cap, or a non-workman up to
// theirs, is paid at least the pack's multiple of the hourly basic rate; anyone else has no overtime entitlement under the Act. A monthly basic salary is compared with the cap;
// another period is not, so the cap is stated.
func overtimeRate(v pack.Version, covered bool, class string, t Terms, at time.Time) string {
	none := "Not applicable: overtime pay does not apply to this employment."
	if !covered || (class != "workman" && class != "non_workman") {
		return none
	}
	rate, ok := pack.PartIV(v, "overtime_rate_hundredths", at)
	capKey := "non_workman_basic_cap"
	if class == "workman" {
		capKey = "workman_basic_cap"
	}
	limit, ok2 := pack.PartIV(v, capKey, at)
	if !ok || !ok2 {
		return ""
	}
	if t.SalaryPeriod == "monthly" && t.BasicSalary != "" {
		if c, err := leave.ParseCents(t.BasicSalary); err == nil && int64(c) > int64(limit)*100 {
			return fmt.Sprintf("Not applicable: the basic monthly salary is above S$%d, the most for which the Employment Act sets overtime pay.", limit)
		}
	}
	return fmt.Sprintf("At least %d.%02d times the hourly basic rate of pay, for a basic monthly salary up to S$%d.", rate/100, rate%100, limit)
}
