package ledger

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The source type and rule version of a pay run (FR-PAY-07, FR-PPL-04, FR-PPL-11; p1-payroll-link design D7), and the system
// roles it posts to (FR-COA-03: 2100 Salaries payable, 2110 CPF & SDL payable, 6020 Employer CPF, 6030 SDL).
const (
	SourcePayrollRun = "payroll_run"
	RulePayrollRun   = "payroll_run@1"

	RoleSalariesPayable = "salaries-payable"
	RoleCPFSDLPayable   = "cpf-sdl-payable"
	RoleEmployerCPF     = "employer-cpf"
	RoleSDL             = "sdl"
)

// CodePayroll is a pay run that does not fit the rule: an account it may not post to, an amount that is not in the base
// currency or below zero, deductions that leave less than nothing to pay, or a version that is not newer than one posted.
const CodePayroll = "LEDGER-51"

// PayrollDepartment is a department's gross pay in the run's wage month: the salary expense account it posts to
// (FR-EMP-02) and its dimension values (FR-DIM-02). No person appears here: the ledger holds no per-person pay (FR-PAY-13).
type PayrollDepartment struct {
	Account uuid.UUID
	Values  []uuid.UUID
	Gross   money.Money
}

// PayrollDeduction is a deduction authorised under FR-PAY-16 taken from net pay, paid or recovered through its own account:
// an advance recovered clears 1250, never salary expense (FR-PAY-18).
type PayrollDeduction struct {
	Account uuid.UUID
	Amount  money.Money
}

// PayrollRun is one version of a pay run as the rule reads it: the full figures of its wage month (FR-PPL-11), whichever
// earlier version they replace. EmployeeCPF, EmployerCPF, SDL and SHG are the month's totals as the payroll source calculated
// them; Reimbursements are claims already booked (2150) that are paid with salary and are not wages (FR-PAY-18).
type PayrollRun struct {
	Source         string // the source system; part of the run's economic identity with the run and the wage month
	RunID          uuid.UUID
	Version        int
	WageMonth      time.Time // any day of the wage month
	PostingDate    time.Time // the day the difference posts: in the wage month, or, when it is closed, the open month (FR-PPL-11)
	Memo           string
	Departments    []PayrollDepartment
	EmployeeCPF    money.Money
	EmployerCPF    money.Money
	SDL            money.Money
	SHG            money.Money
	Deductions     []PayrollDeduction
	Reimbursements money.Money
}

// PayrollPosted is what the earlier versions of a run posted: the highest version and the signed base total per line key
// (debit positive, credit negative). The zero value is a run nothing was posted for.
type PayrollPosted struct {
	Version int
	Totals  map[PayrollKey]money.Money
}

// PayrollKey is one line of a pay run's entry: an account and its dimension values, in ascending order, joined.
type PayrollKey struct {
	Account uuid.UUID
	Values  string
}

func payrollKey(account uuid.UUID, values []uuid.UUID) PayrollKey {
	v := canonical(values)
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = x.String()
	}
	return PayrollKey{account, strings.Join(parts, ",")}
}

func (k PayrollKey) valueIDs() []uuid.UUID {
	if k.Values == "" {
		return nil
	}
	var out []uuid.UUID
	for _, p := range strings.Split(k.Values, ",") {
		out = append(out, uuid.MustParse(p))
	}
	return out
}

// PayrollIdentity is the posting identity of a run's version under a rule version (§11.4). The run is the economic
// identity: one per organisation, source system, run and wage month, which the caller keeps unique.
func PayrollIdentity(source string, run uuid.UUID, version int, rule string) string {
	return identity(SourcePayrollRun+"/"+source, run, version, rule)
}

// PostPayrollRun turns one version of a pay run into its entry (payroll_run@1), as the difference from what the run's earlier
// versions posted (FR-PPL-11), so retries and versions arriving in any order end at the latest version's totals:
//
//   - Dr each department's salary account (with its dimension values) with its gross · Dr 6020 with the employer's CPF · Dr 6030
//     with SDL · Dr 2150 with the reimbursements paid with salary;
//   - Cr 2110 with employee CPF, employer CPF, SDL and SHG · Cr each deduction's account · Cr 2100 with what is left to pay:
//     gross less employee CPF, SHG and the deductions, plus the reimbursements (FR-PAY-18).
//
// A version posts only the lines whose total changed, in account-code order, and none when nothing changed (the returned
// entry has no lines). A version no newer than one posted is refused as stale. Every rule is checked first and every
// failure returned together (ENG-22).
func PostPayrollRun(r PayrollRun, prior PayrollPosted, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if r.PostingDate.IsZero() || r.WageMonth.IsZero() {
		add(CodeNoDate, "wage_month", "the run has no wage month or no posting date", "Give the wage month and the day the difference posts")
	} else if wm := time.Date(r.WageMonth.Year(), r.WageMonth.Month(), 1, 0, 0, 0, 0, time.UTC); r.PostingDate.Before(wm) {
		add(CodePayroll, "posting_date", "the posting date is before the wage month", "Post in the wage month, or in the open month after it")
	}
	if len(r.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(r.Memo), MaxMemo), "Shorten it")
	}
	if r.Version < 1 || r.Version <= prior.Version {
		add(CodePayroll, "version", fmt.Sprintf("version %d is not newer than version %d, which is posted", r.Version, prior.Version),
			"A version older than one posted is stale: send the latest version")
	}
	zero := money.Zero(s.Base)
	amount := func(field string, m money.Money, minimum int) {
		if m.Currency() != s.Base || m.Sign() < minimum {
			add(CodePayroll, field, fmt.Sprintf("the amount is %s %s", m, m.Currency()), fmt.Sprintf("Give it in %s, at least zero", s.Base))
		}
	}
	new := map[PayrollKey]money.Money{}
	var order []PayrollKey
	post := func(k PayrollKey, m money.Money) {
		if cur, ok := new[k]; ok {
			sum, err := cur.Add(m)
			if err != nil {
				return
			}
			new[k] = sum
			return
		}
		new[k] = m
		order = append(order, k)
	}
	gross := zero
	for i, d := range r.Departments {
		f := fmt.Sprintf("departments[%d]", i)
		amount(f+".gross", d.Gross, 0)
		acc, ok := s.Accounts[d.Account]
		switch {
		case !ok:
			add(CodeUnknownAccount, f+".account", "the account does not exist in this organisation", "Map the department to an expense account")
		case acc.Group || !acc.Active || acc.Type != "expense" || acc.Role == RoleEmployerCPF || acc.Role == RoleSDL:
			add(CodePayroll, f+".account", fmt.Sprintf("account %s is not an active salary expense account", acc.Code), "Map the department to an active expense account that is not 6020 or 6030")
		default:
			probs = append(probs, documentLine(s, acc, true, d.Values, f+".values")...)
		}
		if g, err := gross.Add(d.Gross); err == nil {
			gross = g
		}
		post(payrollKey(d.Account, d.Values), d.Gross)
	}
	amount("employee_cpf", r.EmployeeCPF, 0)
	amount("employer_cpf", r.EmployerCPF, 0)
	amount("sdl", r.SDL, 0)
	amount("shg", r.SHG, 0)
	amount("reimbursements", r.Reimbursements, 0)
	role := func(role, what string) (Account, bool) {
		a, ok := s.byRole(role)
		if !ok {
			add(CodeSystemRole, "account", fmt.Sprintf("the chart has no %s account", what), "Load the standard chart, or give an account the role "+role)
		}
		return a, ok
	}
	employer, _ := role(RoleEmployerCPF, "employer CPF (6020)")
	sdl, _ := role(RoleSDL, "SDL (6030)")
	cpfPayable, _ := role(RoleCPFSDLPayable, "CPF & SDL payable (2110)")
	salaries, _ := role(RoleSalariesPayable, "salaries payable (2100)")
	claims, hasClaims := s.byRole(RoleExpenseClaimsPayable)
	if r.Reimbursements.Sign() > 0 && !hasClaims {
		add(CodeSystemRole, "reimbursements", "the chart has no expense claims payable account", "Load the standard chart's 2150")
	}
	deducted := zero
	for i, d := range r.Deductions {
		f := fmt.Sprintf("deductions[%d]", i)
		amount(f+".amount", d.Amount, 1)
		acc, ok := s.Accounts[d.Account]
		switch {
		case !ok:
			add(CodeUnknownAccount, f+".account", "the account does not exist in this organisation", "Choose the account the deduction is recovered through")
		case acc.Group || !acc.Active || acc.Money || acc.ShareCapital || acc.DirectorAccount() || acc.Role == RoleGSTControl ||
			acc.Role == RoleSalariesPayable || (acc.Type != "asset" && acc.Type != "liability"):
			add(CodePayroll, f+".account", fmt.Sprintf("account %s cannot take a deduction", acc.Code), "Choose an active asset or liability account, such as 1250 for an advance recovered")
		}
		if t, err := deducted.Add(d.Amount); err == nil {
			deducted = t
		}
		post(PayrollKey{Account: d.Account}, d.Amount.Neg())
	}
	if len(probs) == 0 {
		contributions, e1 := r.EmployeeCPF.Add(r.EmployerCPF)
		contributions, e2 := contributions.Add(r.SDL)
		contributions, e3 := contributions.Add(r.SHG)
		taken, e4 := r.EmployeeCPF.Add(r.SHG)
		taken, e5 := taken.Add(deducted)
		toPay, e6 := gross.Sub(taken)
		toPay, e7 := toPay.Add(r.Reimbursements)
		if err := firstError(e1, e2, e3, e4, e5, e6, e7); err != nil {
			return Entry{}, err
		}
		net, _ := gross.Sub(taken)
		if net.Sign() < 0 {
			add(CodePayroll, "deductions", fmt.Sprintf("employee CPF, SHG and deductions of %s are more than the gross pay of %s", taken, gross),
				"Check the deductions: they cannot take more than the pay (FR-PAY-16)")
		} else {
			post(PayrollKey{Account: employer.ID}, r.EmployerCPF)
			post(PayrollKey{Account: sdl.ID}, r.SDL)
			if r.Reimbursements.Sign() > 0 && hasClaims {
				post(PayrollKey{Account: claims.ID}, r.Reimbursements)
			}
			post(PayrollKey{Account: cpfPayable.ID}, contributions.Neg())
			post(PayrollKey{Account: salaries.ID}, toPay.Neg())
		}
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	keys := slices.Clone(order)
	for k := range prior.Totals {
		if _, ok := new[k]; !ok {
			keys = append(keys, k)
		}
	}
	keys = slices.Compact(slices.SortedFunc(slices.Values(keys), func(a, b PayrollKey) int { return comparePayrollKeys(s, a, b) }))
	var lines []Line
	base := money.Identity(s.Base)
	for _, k := range keys {
		before, ok := prior.Totals[k]
		if !ok {
			before = zero
		}
		now, ok := new[k]
		if !ok {
			now = zero
		}
		d, err := now.Sub(before)
		if err != nil {
			return Entry{}, err
		}
		if d.IsZero() {
			continue
		}
		lines = append(lines, Line{AccountID: k.Account, Amount: d, Rate: base, Base: d, Description: r.Memo, Values: k.valueIDs()})
	}
	y, m, day := r.PostingDate.Date()
	return Entry{
		PostingDate:     time.Date(y, m, day, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourcePayrollRun, ID: r.RunID.String(), Version: r.Version},
		RuleVersion:     RulePayrollRun,
		PostingIdentity: PayrollIdentity(r.Source, r.RunID, r.Version, RulePayrollRun),
		Memo:            r.Memo,
		Lines:           lines,
	}, nil
}

func comparePayrollKeys(s Settings, a, b PayrollKey) int {
	if c := strings.Compare(s.Accounts[a.Account].Code, s.Accounts[b.Account].Code); c != 0 {
		return c
	}
	if c := strings.Compare(a.Values, b.Values); c != 0 {
		return c
	}
	return slices.Compare(a.Account[:], b.Account[:])
}

func firstError(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// PayrollTotals are what an entry of a pay run adds to a run's posted totals: the entry's signed base amounts by line key.
// The caller keeps them with the version, and hands the sum of every earlier version's to PostPayrollRun as PayrollPosted.
func PayrollTotals(prior PayrollPosted, e Entry) PayrollPosted {
	out := PayrollPosted{Version: max(prior.Version, e.Source.Version), Totals: map[PayrollKey]money.Money{}}
	for k, v := range prior.Totals {
		out.Totals[k] = v
	}
	for _, l := range e.Lines {
		k := payrollKey(l.AccountID, l.Values)
		if cur, ok := out.Totals[k]; ok {
			if sum, err := cur.Add(l.Base); err == nil {
				out.Totals[k] = sum
			}
		} else {
			out.Totals[k] = l.Base
		}
	}
	return out
}

// PayrollRunRule turns one version of a run into its entry under one rule version.
type PayrollRunRule func(r PayrollRun, prior PayrollPosted, s Settings) (Entry, error)

// PayrollRunRules are every rule version the engine has posted pay runs with, by name.
var PayrollRunRules = map[string]PayrollRunRule{RulePayrollRun: PostPayrollRun}

// ReplayPayrollRun re-runs the rule version that posted a run's entry over the same version and the totals posted before it (ENG-06).
func ReplayPayrollRun(rule string, r PayrollRun, prior PayrollPosted, s Settings) (Entry, error) {
	f, ok := PayrollRunRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(r, prior, s)
}
