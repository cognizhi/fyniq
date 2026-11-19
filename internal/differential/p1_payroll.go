package differential

import (
	"encoding/json"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// stagedPayroll is a pay run version being posted, and the totals it leaves its run once it is accepted.
type stagedPayroll struct {
	run    string
	posted ledger.PayrollPosted
}

// payrollRun posts one version of a pay run from the people platform (payroll_run@1, FR-PAY-07, FR-PPL-11): the difference
// between the version's full figures and what the run's earlier versions posted, on the day the document is dated.
func (r *p1) payrollRun(d Document, date time.Time) (ledger.Entry, bool) {
	var x struct {
		Run         string `json:"run"`
		WageMonth   string `json:"wage_month"`
		Departments []struct {
			Account string `json:"account"`
			Gross   string `json:"gross"`
		} `json:"departments"`
		EmployeeCPF string `json:"employee_cpf"`
		EmployerCPF string `json:"employer_cpf"`
		SDL         string `json:"sdl"`
		SHG         string `json:"shg"`
		Deductions  []struct {
			Account string `json:"account"`
			Amount  string `json:"amount"`
		} `json:"deductions"`
		Reimbursements string `json:"reimbursements"`
	}
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	wage, err := time.Parse("2006-01", x.WageMonth)
	if err != nil {
		return ledger.Entry{}, false
	}
	run := ledger.PayrollRun{Source: "differential", RunID: stableID("payroll", x.Run), Version: d.Version, WageMonth: wage, PostingDate: date}
	amt := func(s string, dst *money.Money) bool {
		if s == "" {
			s = "0"
		}
		m, ok := r.amount(s, r.base.Code)
		*dst = m
		return ok
	}
	var ok1, ok2, ok3, ok4, ok5 bool
	ok1 = amt(x.EmployeeCPF, &run.EmployeeCPF)
	ok2 = amt(x.EmployerCPF, &run.EmployerCPF)
	ok3 = amt(x.SDL, &run.SDL)
	ok4 = amt(x.SHG, &run.SHG)
	ok5 = amt(x.Reimbursements, &run.Reimbursements)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return ledger.Entry{}, false
	}
	for _, dept := range x.Departments {
		var g money.Money
		if !amt(dept.Gross, &g) {
			return ledger.Entry{}, false
		}
		run.Departments = append(run.Departments, ledger.PayrollDepartment{Account: account(dept.Account), Gross: g})
	}
	for _, ded := range x.Deductions {
		var a money.Money
		if !amt(ded.Amount, &a) {
			return ledger.Entry{}, false
		}
		run.Deductions = append(run.Deductions, ledger.PayrollDeduction{Account: account(ded.Account), Amount: a})
	}
	prior := r.payrolls[x.Run]
	e, err := ledger.PostPayrollRun(run, prior, r.settings)
	if err != nil {
		return ledger.Entry{}, false
	}
	r.stagedPayroll = &stagedPayroll{run: x.Run, posted: ledger.PayrollTotals(prior, e)}
	return e, true
}
