package payroll

import (
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// PayItem is an item of a payslip: what it is called and how much.
type PayItem struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

// Payslip holds the twelve items MOM requires of an itemised pay slip (sg-people@2026.7 `payslip_items`; NM-PAY-04), from the employee-month. An item that does not apply is empty.
type Payslip struct {
	Employer       string    `json:"employer"`                 // 1
	Employee       string    `json:"employee"`                 // 2
	PaymentDate    string    `json:"payment_date"`             // 3
	Basic          string    `json:"basic_salary"`             // 4
	PeriodStart    string    `json:"period_start"`             // 5
	PeriodEnd      string    `json:"period_end"`               // 5
	Allowances     []PayItem `json:"allowances"`               // 6: fixed and ad-hoc
	Additional     []PayItem `json:"additional"`               // 7: bonuses, rest day pay, public holiday pay and other additional payments
	Deductions     []PayItem `json:"deductions"`               // 8: fixed (the employee's CPF) and ad-hoc (no-pay leave, absence)
	OvertimeHours  string    `json:"overtime_hours"`           // 9
	OvertimePay    string    `json:"overtime_pay"`             // 10
	OvertimePeriod string    `json:"overtime_period"`          // 11: only if different from the salary period
	Net            string    `json:"net_salary"`               // 12
	Reimbursements string    `json:"reimbursements,omitempty"` // paid with the salary, not part of it
}

// SlipLine is an item of a line of a run as the payslip reads it.
type SlipLine struct {
	Key, Name, Class, Amount, SourceType, Nature string
}

// PayslipInput is what a payslip is made from: the person's month in an approved run.
type PayslipInput struct {
	Employer, Employee string
	Month, PayDate     time.Time
	Items              []SlipLine
	EmployeeCPF, SHG   *apd.Decimal
	OvertimeHours      *apd.Decimal // the hours behind the overtime pay
	Net                *apd.Decimal
	Reimbursements     *apd.Decimal
}

func neg(d *apd.Decimal) *apd.Decimal { return sub(zero(), d) }

// BuildPayslip puts the month's items under MOM's twelve: the basic salary; the fixed and ad-hoc allowances; the other additional payments (bonuses, commissions and the rest); the overtime hours
// and pay; the deductions, fixed (the employee's CPF, the self-help group fund, fixed deductions) and ad-hoc (no-pay leave); and the net salary. It refuses a payslip whose items do not add up
// to the net salary, which would be a defect, never a rounding.
func BuildPayslip(in PayslipInput) (Payslip, error) {
	p := Payslip{Employer: in.Employer, Employee: in.Employee, PaymentDate: in.PayDate.Format(time.DateOnly), PeriodStart: in.Month.Format(time.DateOnly),
		PeriodEnd: in.Month.AddDate(0, 1, -1).Format(time.DateOnly), Net: cents(in.Net), Allowances: []PayItem{}, Additional: []PayItem{}, Deductions: []PayItem{}}
	sum := zero()
	basic, overtime := zero(), zero()
	for _, it := range in.Items {
		amt := D(it.Amount)
		switch {
		case it.Key == "basic":
			basic = add(basic, amt)
			sum = add(sum, amt)
		case strings.HasPrefix(it.Key, "ot-"):
			overtime = add(overtime, amt)
			sum = add(sum, amt)
		case strings.HasPrefix(it.Key, "unpaid-"):
			p.Deductions = append(p.Deductions, PayItem{Name: it.Name, Amount: cents(neg(amt))})
			sum = add(sum, amt)
		case it.Class == "deduction":
			p.Deductions = append(p.Deductions, PayItem{Name: strings.TrimPrefix(it.Name, "Deduction: "), Amount: cents(amt)})
			sum = sub(sum, amt)
		case it.SourceType == "pay_one_off" && it.Nature != "allowance":
			p.Additional = append(p.Additional, PayItem{Name: it.Name, Amount: cents(amt)})
			sum = add(sum, amt)
		default:
			p.Allowances = append(p.Allowances, PayItem{Name: it.Name, Amount: cents(amt)})
			sum = add(sum, amt)
		}
	}
	p.Basic = cents(basic)
	if overtime.Sign() != 0 {
		p.OvertimePay = cents(overtime)
		if in.OvertimeHours != nil {
			p.OvertimeHours = in.OvertimeHours.Text('f')
		}
	}
	if in.EmployeeCPF != nil && in.EmployeeCPF.Sign() != 0 {
		p.Deductions = append(p.Deductions, PayItem{Name: "Employee's CPF contribution", Amount: cents(in.EmployeeCPF)})
		sum = sub(sum, in.EmployeeCPF)
	}
	if in.SHG != nil && in.SHG.Sign() != 0 {
		p.Deductions = append(p.Deductions, PayItem{Name: "Self-help group fund", Amount: cents(in.SHG)})
		sum = sub(sum, in.SHG)
	}
	if in.Reimbursements != nil && in.Reimbursements.Sign() != 0 {
		p.Reimbursements = cents(in.Reimbursements)
	}
	if cents(sum) != cents(in.Net) {
		return p, fmt.Errorf("the payslip of %s does not add up: its items come to %s and the net salary is %s", in.Employee, cents(sum), cents(in.Net))
	}
	return p, nil
}

// SlipRow is a label and its value, for a printed payslip.
type SlipRow struct{ Label, Value string }

// Lines are the payslip's items in MOM's order, each with its label and value, leaving out what does not apply.
func (p Payslip) Lines() []SlipRow {
	out := []SlipRow{{"Employer", p.Employer}, {"Employee", p.Employee}, {"Date of payment", p.PaymentDate}, {"Salary period", p.PeriodStart + " to " + p.PeriodEnd}, {"Basic salary", p.Basic}}
	for _, a := range p.Allowances {
		out = append(out, SlipRow{"Allowance: " + a.Name, a.Amount})
	}
	for _, a := range p.Additional {
		out = append(out, SlipRow{"Additional payment: " + a.Name, a.Amount})
	}
	if p.OvertimePay != "" {
		if p.OvertimeHours != "" {
			out = append(out, SlipRow{"Overtime hours worked", p.OvertimeHours})
		}
		out = append(out, SlipRow{"Overtime pay", p.OvertimePay})
		if p.OvertimePeriod != "" {
			out = append(out, SlipRow{"Overtime payment period", p.OvertimePeriod})
		}
	}
	for _, d := range p.Deductions {
		out = append(out, SlipRow{"Deduction: " + d.Name, d.Amount})
	}
	out = append(out, SlipRow{"Net salary paid", p.Net})
	if p.Reimbursements != "" {
		out = append(out, SlipRow{"Reimbursements paid with the salary (not part of the salary)", p.Reimbursements})
	}
	return out
}

// SlipFields maps each key of the pack's `payslip_items` (sg-people@2026.7) to the field of Payslip that holds it, so that a payslip is checked against the pack's list of what MOM requires
// and a new item in a later version cannot be left out unnoticed (a test reads every item of the newest version).
var SlipFields = map[string]string{
	"employer_name": "Employer", "employee_name": "Employee", "payment_date": "PaymentDate", "basic_salary": "Basic", "salary_period": "PeriodStart", "allowances": "Allowances",
	"additional_payments": "Additional", "deductions": "Deductions", "overtime_hours": "OvertimeHours", "overtime_pay": "OvertimePay", "overtime_period": "OvertimePeriod", "net_salary": "Net",
}
