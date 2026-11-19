package payroll

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

// The figures an override may replace (FR-PAY-06): the CPF shares, the SDL and the self-help group fund of one person's month.
const (
	FieldCPFEmployee = "cpf_employee"
	FieldCPFEmployer = "cpf_employer"
	FieldSDL         = "sdl"
	FieldSHG         = "shg"
)

// Fields are the figures an override may replace.
var Fields = []string{FieldCPFEmployee, FieldCPFEmployer, FieldSDL, FieldSHG}

// Override replaces one calculated figure of a person's month with a reason; it is flagged ⚠ on the month and the approval lists it (FR-PAY-06).
type Override struct {
	Field  string
	Value  *apd.Decimal
	Reason string
}

// Check refuses an override that names no figure the engine owns, has no reason of at least ten characters, or an amount that is not dollars with two decimals.
func (o Override) Check() error {
	known := false
	for _, f := range Fields {
		known = known || f == o.Field
	}
	if !known {
		return invalid("An override replaces %s.", strings.Join(Fields, ", "))
	}
	if len(strings.TrimSpace(o.Reason)) < 10 || len(o.Reason) > 500 {
		return invalid("An override needs a reason of 10 to 500 characters: say why the calculated figure is wrong.")
	}
	if o.Value == nil || o.Value.Form != 0 || o.Value.Sign() < 0 || o.Value.Exponent < -2 {
		return invalid("The overriding figure is in dollars with at most two decimal places: write it as 120.00.")
	}
	return nil
}

// Apply returns the month with the override applied: the figure is replaced, the CPF total follows its two shares, and net pay, the amount to disburse and the employer's cost follow;
// the figure the engine worked out is kept in the working, and a ⚠ names the override and its reason. RND-CPF's dollar rounding is not applied to an override: it is the figure the person
// gave, as the CPF Board or the bank showed it.
func (o Override) Apply(r Result) Result {
	step(&r, "FR-PAY-06", "Override of %s: %s replaces the calculated %s. Reason: %s", o.Field, o.Value.Text('f'), r.figure(o.Field).Text('f'), strings.TrimSpace(o.Reason))
	switch o.Field {
	case FieldCPFEmployee:
		r.CPF.Employee = o.Value
	case FieldCPFEmployer:
		r.CPF.Employer = o.Value
	case FieldSDL:
		r.SDL = o.Value
	case FieldSHG:
		r.SHG = o.Value
	}
	r.CPF.Total = add(r.CPF.Employee, r.CPF.Employer)
	r.Net = sub(sub(sub(r.Gross, r.CPF.Employee), r.SHG), r.Deductions)
	r.Disburse = sub(add(r.Net, r.Reimbursements), r.Held)
	r.EmployerCost = add(add(r.Gross, r.CPF.Employer), r.SDL)
	r.Flags = append(r.Flags, Flag{"OVERRIDE", false, fmt.Sprintf("%s is overridden: %s", o.Field, strings.TrimSpace(o.Reason))})
	r.Overridden = append(r.Overridden, o.Field)
	return r
}

func (r Result) figure(field string) *apd.Decimal {
	switch field {
	case FieldCPFEmployee:
		return r.CPF.Employee
	case FieldCPFEmployer:
		return r.CPF.Employer
	case FieldSDL:
		return r.SDL
	}
	return r.SHG
}
