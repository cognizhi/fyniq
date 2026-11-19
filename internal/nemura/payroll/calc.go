package payroll

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Residency values, as the people record holds them.
const (
	Citizen           = "citizen"
	PermanentResident = "permanent_resident"
	Foreigner         = "foreigner"
)

// Person is what the calculation needs to know about the employee for the month.
type Person struct {
	Employee      bool       // a contract of service: SDL applies to every employee, CPF to citizens and permanent residents; a contractor has neither
	Residency     string     // citizen, permanent_resident or foreigner
	PRStart       *time.Time // the day permanent resident status was obtained (the first-year and second-year rates run from it)
	BirthDate     time.Time  // for the age band
	JointElection bool       // a permanent resident in the first two years with the CPF Board's approval of a joint application: full employer and graduated employee rates
	SHGFund       string     // CDAC, ECF, MBMF or SINDA; empty when none
	SHGOptOut     bool       // the employee opted out (recorded)
}

// YearToDate is what this employer has already counted for the person in the calendar year, before the month (FR-PAY-14). Known is false when it was never imported or built up,
// which is an error for a month that pays additional wages.
type YearToDate struct {
	Known     bool
	OW        *apd.Decimal // ordinary wages subject to CPF
	AWSubject *apd.Decimal // additional wages subject to CPF
	AWPaid    *apd.Decimal // additional wages paid, whether or not subject
}

// Month is one employee-month: every item of the calendar month across the main run, any off-cycle run and any correction (B.3).
type Month struct {
	Month          time.Time // the first day of the wage month
	PayDay         time.Time // the pay day of the month, for an item that names no date
	Person         Person
	Items          []Item
	YTD            YearToDate
	LastMonth      bool // December, or the person's last month of employment, whichever is earlier: the true-up of the additional wage ceiling
	Deductions     []Deduction
	Final          bool           // final pay on termination: the 50% limit does not apply and an advance is recovered only as far as consented (FR-PAY-16)
	Reimbursements []*apd.Decimal // non-wage reimbursements paid with the salary (FR-AP-17), never CPF wages
	Held           *apd.Decimal   // monies held under an IR21 clearance hold (FR-PAY-10)
	HoldAll        bool           // a clearance hold is in force: every dollar due to the person is held (net pay and reimbursements), and nothing is disbursed until the hold is released
}

// Packs are the statutory packs the month reads.
type Packs struct {
	CPF, SDL, SHG pack.Version
}

// Step is one line of the working: the rule and what it did.
type Step struct{ Rule, Text string }

// Flag is something the preparer must resolve or know: a name that disagrees with the facts, a missing year to date, a refund task.
type Flag struct {
	Code    string
	Fail    bool // ✖: the run cannot be approved with it; otherwise ⚠
	Message string
}

// Refund is the refund task of a month whose true-up finds additional wages counted beyond the annual ceiling: the employer applies to the CPF Board, and the employee's share is the
// refund divided by the total rate times the employee rate, cents dropped: whole dollars (FR-PAY-14).
type Refund struct {
	Wages    *apd.Decimal // the excess of additional wages subject to CPF
	Total    *apd.Decimal // the contributions on it, at the month's total rate, in whole dollars (RND-CPF)
	Employee *apd.Decimal // the employee's share, whole dollars
}

// Result is the whole employee-month.
type Result struct {
	Month               time.Time
	Items               []Classed
	Gross               *apd.Decimal
	OrdinaryWages       *apd.Decimal // the ordinary wages of the month that are CPF wages, before the ceiling
	AdditionalWages     *apd.Decimal // the additional wages of the month that are CPF wages, before the annual ceiling
	OWSubject           *apd.Decimal // the ordinary wages subject to CPF: capped at the monthly ceiling
	AWSubject           *apd.Decimal // the additional wages subject to CPF this month, after the annual ceiling and any true-up
	CPF                 rounding.Contributions
	CPFGroup            string // the rate group read from the pack, or "none"
	SDL                 *apd.Decimal
	SHG                 *apd.Decimal
	Deductions          *apd.Decimal
	Advances            *apd.Decimal // of the deductions, the advances recovered, which B.3 posts to 1250 and not to a deduction's own account (FR-PAY-18)
	Reimbursements      *apd.Decimal
	Held                *apd.Decimal
	Net                 *apd.Decimal // gross − employee CPF − SHG − deductions
	Disburse            *apd.Decimal // net + reimbursements − held
	EmployerCost        *apd.Decimal // gross + employer CPF + SDL
	Refund              *Refund
	Flags               []Flag
	Working             []Step
	PackVersions        []string
	DeductionViolations []Violation
	Overridden          []string // the figures an override replaced
}

// ErrNeedsYearToDate is the refusal for a month that pays additional wages with no year to date for the person: it is ✖ until the year to date is imported or built up (FR-PAY-14).
var ErrNeedsYearToDate = errors.New("the month pays additional wages and the person has no year to date: import it from 1 January for this employer")

// CPFAge is the age the sg-cpf tables read for a month. The age band changes from the first day of the month after the 55th, 60th, 65th or 70th birthday, so the person is in the
// next band for a month when they had completed that many years by the end of the month before; the tables say "above 55" for a person who is past 55, so the age they read is the
// year of life the person is in at the end of the month before: the years completed then, plus one. (A birthday on the first of a month is read the same way, so the band changes
// the month after; whether the CPF Board treats a first-of-month birthday differently `[verify]`.)
func CPFAge(birth, month time.Time) int {
	end := firstOf(month).AddDate(0, 0, -1)
	completed := end.Year() - birth.Year()
	if end.Month() < birth.Month() || (end.Month() == birth.Month() && end.Day() < birth.Day()) {
		completed--
	}
	return completed + 1
}

// PRYear is the year of permanent residence a month falls in: 1 until the end of the month of the first anniversary of obtaining status, 2 until the end of the month of the second,
// 3 after (CPF Board, checked 14 Oct 2026).
func PRYear(start, month time.Time) int {
	end1 := time.Date(start.Year()+1, start.Month()+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	end2 := time.Date(start.Year()+2, start.Month()+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	switch {
	case !month.After(end1):
		return 1
	case !month.After(end2):
		return 2
	}
	return 3
}

// cpfGroup is the pack's rate group for the person in the month, or "" when CPF does not apply.
func cpfGroup(p Person, month time.Time) string {
	switch p.Residency {
	case Citizen:
		return pack.GroupSCSPR3
	case PermanentResident:
		if p.PRStart == nil {
			return pack.GroupSCSPR3
		}
		switch PRYear(*p.PRStart, month) {
		case 1:
			if p.JointElection {
				return pack.GroupSPR1FG
			}
			return pack.GroupSPR1GG
		case 2:
			if p.JointElection {
				return pack.GroupSPR2FG
			}
			return pack.GroupSPR2GG
		}
		return pack.GroupSCSPR3
	}
	return ""
}

func step(r *Result, rule, format string, a ...any) {
	r.Working = append(r.Working, Step{rule, fmt.Sprintf(format, a...)})
}

// Calculate is Appendix B.3 for one employee-month, exact: the items are classed by the facts; the ordinary wages are capped at the monthly ceiling; the additional wages are limited
// by the annual ceiling, tracked year to date, with the true-up in the last month; CPF takes the rates of the person's group and age (the graduated formulas up to S$750 of total
// wages) and RND-CPF or RND-CPF-GRAD; SDL is RND-SDL's per-employee amount; the self-help group fund comes from its table unless the employee opted out; net pay and the amount to
// disburse follow. The month's whole figures are returned, so a later run or correction of the same month recomputes and the caller posts the difference (Diff).
func Calculate(m Month, p Packs) (Result, error) {
	r := Result{Month: firstOf(m.Month), Gross: zero(), OrdinaryWages: zero(), AdditionalWages: zero(), OWSubject: zero(), AWSubject: zero(), SDL: zero(), SHG: zero(),
		Deductions: zero(), Advances: zero(), Reimbursements: zero(), Held: zero(), CPFGroup: "none"}
	r.CPF = rounding.Contributions{Total: zero(), Employee: zero(), Employer: zero()}
	r.PackVersions = []string{p.CPF.ID, p.SDL.ID, p.SHG.ID}
	month := r.Month
	payDay := m.PayDay
	if payDay.IsZero() {
		payDay = month.AddDate(0, 1, -1)
	}
	r.Items = ByFacts(m.Items, month, payDay)
	for _, it := range r.Items {
		r.Gross = add(r.Gross, it.Amount)
		switch it.Class {
		case Ordinary:
			r.OrdinaryWages = add(r.OrdinaryWages, it.Amount)
		case Additional:
			r.AdditionalWages = add(r.AdditionalWages, it.Amount)
		}
		if it.Nature == NatureLeavePay || it.Nature == NatureNoticePay {
			r.Flags = append(r.Flags, Flag{"FINAL_PAY_CPF", false, fmt.Sprintf("%s: whether this is CPF wages is not settled by a source this system has read: confirm 'CPF wages' for it against the CPF Board's table of payments (accountant to confirm).", it.Name)})
		}
		if it.Mismatch {
			r.Flags = append(r.Flags, Flag{"WAGE_CLASS", false, fmt.Sprintf("%s: %s", it.Name, it.Why)})
		}
	}
	step(&r, "B.3 Gross", "Gross pay %s: basic, allowances, overtime and bonus less unpaid leave.", cents(r.Gross))
	if m.Person.Employee {
		if err := cpf(&r, m, p); err != nil {
			return r, err
		}
		sdl(&r, p)
		shg(&r, m, p)
	} else {
		step(&r, "B.3", "A contract for service: no CPF, no SDL and no self-help group fund.")
	}
	for _, d := range m.Deductions {
		r.Deductions = add(r.Deductions, d.Amount)
		if d.IsAdvance {
			r.Advances = add(r.Advances, d.Amount)
		}
	}
	r.DeductionViolations = CheckDeductions(r.Gross, m.Deductions, m.Final)
	for _, v := range r.DeductionViolations {
		r.Flags = append(r.Flags, Flag{"DEDUCTION", true, v.Text})
	}
	for _, x := range m.Reimbursements {
		r.Reimbursements = add(r.Reimbursements, x)
	}
	if m.Held != nil {
		r.Held = m.Held
	}
	r.Net = sub(sub(sub(r.Gross, r.CPF.Employee), r.SHG), r.Deductions)
	if m.HoldAll {
		r.Held = add(r.Net, r.Reimbursements)
		r.Flags = append(r.Flags, Flag{"CLEARANCE_HOLD", false, "A tax-clearance hold is in force: all monies due are held (nothing is disbursed) until IRAS gives clearance and the hold is released."})
	}
	r.Disburse = sub(add(r.Net, r.Reimbursements), r.Held)
	r.EmployerCost = add(add(r.Gross, r.CPF.Employer), r.SDL)
	step(&r, "B.3 Net pay", "Net pay %s = gross %s − employee CPF %s − self-help group fund %s − deductions %s. To disburse %s = net pay + reimbursements %s − held %s.",
		cents(r.Net), cents(r.Gross), r.CPF.Employee.Text('f'), cents(r.SHG), cents(r.Deductions), cents(r.Disburse), cents(r.Reimbursements), cents(r.Held))
	return r, nil
}

func cpf(r *Result, m Month, p Packs) error {
	group := cpfGroup(m.Person, r.Month)
	if group == "" {
		step(r, "FR-PAY-02", "A foreign employee does not contribute to CPF.")
		return nil
	}
	r.CPFGroup = group
	age := CPFAge(m.Person.BirthDate, r.Month)
	ceilOW, err := pack.CPFCeiling(p.CPF, pack.CeilingOWMonthly, r.Month)
	if err != nil {
		return fmt.Errorf("sg-cpf: %w", err)
	}
	owWages := nonNeg(r.OrdinaryWages)
	r.OWSubject = minD(owWages, ceilOW)
	step(r, "FR-PAY-02", "Ordinary wages %s, capped at the monthly ceiling %s: %s subject to CPF.", cents(owWages), cents(ceilOW), cents(r.OWSubject))
	aw, err := additionalSubject(r, m, p, ceilOW)
	if err != nil {
		return err
	}
	r.AWSubject = aw
	subject := add(r.OWSubject, r.AWSubject)
	tw := subject // [verify] the month's total wages, for the wage band (the CPF Board's page says only "Total Wages payable in a calendar month"; the choice matters only when wages paid pass S$750 and wages subject to CPF do not): the wages subject to CPF, as the sg-cpf pack defines them, so a true-up's shortfall is in them and additional wages above the ceiling are not
	step(r, "FR-PAY-02", "Wages subject to CPF %s = ordinary %s + additional %s; the group is %s and the age %d.", cents(subject), cents(r.OWSubject), cents(r.AWSubject), group, age)
	if tw.Cmp(D("750")) <= 0 {
		a, err := pack.CPFLowWagesOn(p.CPF, r.Month, group, age, tw)
		if err != nil {
			return fmt.Errorf("sg-cpf graduated: %w", err)
		}
		r.CPF = rounding.CPFGraduated(a.Total, a.Employee)
		step(r, "RND-CPF-GRAD", "Total wages %s are in the graduated range: total %s, employee %s, employer %s.", cents(tw), r.CPF.Total.Text('f'), r.CPF.Employee.Text('f'), r.CPF.Employer.Text('f'))
		return refundTask(r, p, group, age, D("750.01")) // the refund is of additional wages counted at the rates above the graduated range
	}
	rates, err := pack.CPFRatesOn(p.CPF, r.Month, group, age, tw)
	if err != nil {
		return fmt.Errorf("sg-cpf rates: %w", err)
	}
	r.CPF = rounding.CPF(subject, rates.Employer, rates.Employee)
	step(r, "RND-CPF", "Rates employer %s and employee %s: total %s, employee %s, employer %s.", rates.Employer.Text('f'), rates.Employee.Text('f'), r.CPF.Total.Text('f'), r.CPF.Employee.Text('f'), r.CPF.Employer.Text('f'))
	return refundTask(r, p, group, age, tw)
}

// refundTask works out the refund task of a month whose true-up found additional wages counted beyond the annual ceiling (FR-PAY-14), at the rates of the person's group and age
// for wages of wage, whatever band the month's own wages are in.
func refundTask(r *Result, p Packs, group string, age int, wage *apd.Decimal) error {
	if r.Refund == nil || r.Refund.Wages.Sign() <= 0 {
		return nil
	}
	rates, err := pack.CPFRatesOn(p.CPF, r.Month, group, age, wage)
	if err != nil {
		return fmt.Errorf("sg-cpf rates: %w", err)
	}
	total := add(rates.Employer, rates.Employee)
	// The refund is contributions, which the CPF Board takes in whole dollars: the total rounds as RND-CPF's does (nearest dollar, 50 cents up) [accountant to confirm: the
	// calculator rounds monthly and the automatic refund over the year]. The employee's share is the CPF Board's formula, the refund over the total rate times the employee
	// rate, with the cents dropped: whole dollars (its worked example: $2,260 / 32.5% x 17% = $1,182.15 is $1,182; cpf.gov.sg, checked 20 Nov 2026).
	r.Refund.Total = rounding.CPF(r.Refund.Wages, rates.Employer, rates.Employee).Total
	r.Refund.Employee = floorDollars(quo(mul(r.Refund.Total, rates.Employee), total))
	r.Flags = append(r.Flags, Flag{"CPF_REFUND", false, fmt.Sprintf("Additional wages counted beyond the annual ceiling: %s of wages. Apply to the CPF Board for a refund; the employee's share, %s, is repaid to the employee.",
		cents(r.Refund.Wages), r.Refund.Employee.Text('f'))})
	return nil
}

// floorDollars drops the cents.
func floorDollars(d *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	c := work
	c.Rounding = apd.RoundFloor
	if _, err := c.Quantize(out, d, 0); err != nil {
		panic("payroll: floor: " + err.Error())
	}
	return out
}

// additionalSubject is the additional wages of the month that are subject to CPF (FR-PAY-14): limited by the annual ceiling less the year's ordinary wages subject to CPF. Before the
// last month the year's ordinary wages are estimated (the year to date plus this month's, over the months to come); in the last month they are the actual and the year is trued up,
// adding a shortfall to the month and raising a refund for an excess.
func additionalSubject(r *Result, m Month, p Packs, ceilOW *apd.Decimal) (*apd.Decimal, error) {
	aw := nonNeg(r.AdditionalWages)
	if aw.Sign() == 0 && !m.LastMonth {
		return zero(), nil
	}
	if !m.YTD.Known {
		if aw.Sign() > 0 {
			r.Flags = append(r.Flags, Flag{"YTD_MISSING", true, "The month pays additional wages and the person has no year to date for this employer."})
			return zero(), ErrNeedsYearToDate
		}
		return zero(), nil // the last month with nothing to true up and no year to date: nothing was counted
	}
	annual, err := pack.CPFCeiling(p.CPF, pack.CeilingAnnualSalary, r.Month)
	if err != nil {
		return nil, fmt.Errorf("sg-cpf: %w", err)
	}
	owYear := add(m.YTD.OW, r.OWSubject)
	if !m.LastMonth {
		monthsLeft := apd.New(int64(12-int(r.Month.Month())), 0)
		est := add(owYear, mul(r.OWSubject, monthsLeft))
		room := nonNeg(sub(sub(annual, est), m.YTD.AWSubject))
		got := minD(aw, room)
		step(r, "FR-PAY-14", "Additional wages %s; the year's ordinary wages are estimated at %s, so the annual ceiling %s leaves %s: %s subject to CPF.", cents(aw), cents(est), cents(annual), cents(room), cents(got))
		return got, nil
	}
	paid := add(m.YTD.AWPaid, aw)
	allowed := minD(paid, nonNeg(sub(annual, owYear)))
	now := sub(allowed, m.YTD.AWSubject)
	step(r, "FR-PAY-14", "The last month trues up the year: ordinary wages subject to CPF %s, additional wages paid %s, the annual ceiling %s allows %s, %s was counted before.", cents(owYear), cents(paid), cents(annual), cents(allowed), cents(m.YTD.AWSubject))
	if now.Sign() < 0 {
		r.Refund = &Refund{Wages: sub(zero(), now), Total: zero(), Employee: zero()}
		return zero(), nil
	}
	return now, nil
}

func sdl(r *Result, p Packs) {
	base := nonNeg(r.Gross)
	levy, err := pack.SDLOn(p.SDL, r.Month, base)
	if err != nil {
		r.Flags = append(r.Flags, Flag{"SDL_NOT_HELD", true, "The sg-sdl pack holds no levy for the month: " + err.Error()})
		return
	}
	r.SDL = rounding.SDLEmployee(levy)
	step(r, "RND-SDL", "SDL on total remuneration %s: %s (the employer-month total is rounded down to the dollar across the run).", cents(base), r.SDL.Text('f'))
}

func shg(r *Result, m Month, p Packs) {
	if m.Person.SHGFund == "" || m.Person.SHGOptOut {
		return
	}
	amt, err := pack.SHGOn(p.SHG, r.Month, m.Person.SHGFund, nonNeg(r.Gross))
	if errors.Is(err, pack.ErrNotHeld) {
		return
	}
	if err != nil {
		r.Flags = append(r.Flags, Flag{"SHG_NOT_HELD", true, "The sg-shg pack could not be read: " + err.Error()})
		return
	}
	r.SHG = amt
	step(r, "FR-PAY-04", "%s fund contribution %s on total wages %s.", m.Person.SHGFund, r.SHG.Text('f'), cents(nonNeg(r.Gross)))
}
