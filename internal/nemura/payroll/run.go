package payroll

import (
	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Totals are a run's totals, kept apart (FR-PAY-18) and ready for B.3's postings: the SDL payable is the employer-month total rounded down to the dollar (RND-SDL), never the sum of the
// employee amounts.
type Totals struct {
	Gross, EmployeeCPF, EmployerCPF, SDLEmployees, SDLPayable, SHG, Deductions, Reimbursements, Held, Net, Disburse, EmployerCost *apd.Decimal
}

// Total adds a run's employee-months. The employer-month SDL is rounded down once, across the run (RND-SDL).
func Total(rs []Result) Totals {
	t := Totals{Gross: zero(), EmployeeCPF: zero(), EmployerCPF: zero(), SDLEmployees: zero(), SHG: zero(), Deductions: zero(), Reimbursements: zero(), Held: zero(), Net: zero(), Disburse: zero(), EmployerCost: zero()}
	var sdl []*apd.Decimal
	for _, r := range rs {
		t.Gross = add(t.Gross, r.Gross)
		t.EmployeeCPF = add(t.EmployeeCPF, r.CPF.Employee)
		t.EmployerCPF = add(t.EmployerCPF, r.CPF.Employer)
		t.SDLEmployees = add(t.SDLEmployees, r.SDL)
		sdl = append(sdl, r.SDL)
		t.SHG = add(t.SHG, r.SHG)
		t.Deductions = add(t.Deductions, r.Deductions)
		t.Reimbursements = add(t.Reimbursements, r.Reimbursements)
		t.Held = add(t.Held, r.Held)
		t.Net = add(t.Net, r.Net)
		t.Disburse = add(t.Disburse, r.Disburse)
	}
	t.SDLPayable = rounding.SDLPayable(sdl)
	t.EmployerCost = add(add(t.Gross, t.EmployerCPF), t.SDLPayable)
	return t
}

// Line is one line of B.3's posting: the account and the amount on the debit or the credit side.
type Line struct {
	Account string
	Debit   *apd.Decimal
	Credit  *apd.Decimal
	Why     string
}

// Postings are B.3's entry for a run's totals: Dr salary expense gross; Dr 6020 employer CPF; Dr 6030 SDL (the payable); Dr 2150 for claims paid with the salary; Cr 1250 for advances
// recovered and the deductions' own accounts; Cr 2100 the amount to disburse plus the monies held (which stay in 2100, flagged, until released); Cr 2110 the employee and employer CPF,
// the SDL payable and the self-help group funds. Fyniq posts it by department (p1-payroll-link); here it is the whole run, so that the entry's balance is proved.
func Postings(t Totals) []Line {
	return []Line{
		{"salary expense", t.Gross, zero(), "Gross pay"},
		{"6020", t.EmployerCPF, zero(), "Employer CPF"},
		{"6030", t.SDLPayable, zero(), "SDL"},
		{"2150", t.Reimbursements, zero(), "Claims paid with the salary"},
		{"deductions", zero(), t.Deductions, "Advances recovered and other authorised deductions"},
		{"2100", zero(), add(t.Disburse, t.Held), "To disburse, and monies held"},
		{"2110", zero(), add(add(add(t.EmployeeCPF, t.EmployerCPF), t.SDLPayable), t.SHG), "CPF, SDL and self-help group funds"},
	}
}

// Balanced reports whether the entry balances: gross + employer CPF + SDL + reimbursements = deductions + 2100 + 2110.
func Balanced(lines []Line) bool {
	debit, credit := zero(), zero()
	for _, l := range lines {
		debit, credit = add(debit, l.Debit), add(credit, l.Credit)
	}
	return debit.Cmp(credit) == 0
}

// Diff is what a later run or correction of the same employee-month changes: the new whole month against what was already posted, per figure, so that only the difference is posted
// (B.3, NM-PAY-09). Positive is more than before.
type Difference struct {
	Gross, EmployeeCPF, EmployerCPF, SDL, SHG, Deductions, Reimbursements, Net, Disburse *apd.Decimal
}

// Diff computes the difference between a month's new result and what was posted (nil: nothing yet).
func Diff(now Result, posted *Result) Difference {
	z := func() *apd.Decimal { return zero() }
	p := Result{Gross: z(), CPF: rounding.Contributions{Total: z(), Employee: z(), Employer: z()}, SDL: z(), SHG: z(), Deductions: z(), Reimbursements: z(), Net: z(), Disburse: z()}
	if posted != nil {
		p = *posted
	}
	return Difference{Gross: sub(now.Gross, p.Gross), EmployeeCPF: sub(now.CPF.Employee, p.CPF.Employee), EmployerCPF: sub(now.CPF.Employer, p.CPF.Employer), SDL: sub(now.SDL, p.SDL),
		SHG: sub(now.SHG, p.SHG), Deductions: sub(now.Deductions, p.Deductions), Reimbursements: sub(now.Reimbursements, p.Reimbursements), Net: sub(now.Net, p.Net), Disburse: sub(now.Disburse, p.Disburse)}
}
