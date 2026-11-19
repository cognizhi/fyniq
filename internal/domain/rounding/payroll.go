package rounding

import (
	"github.com/cockroachdb/apd/v3"
)

// The statutory payroll policies (PRD-001 §10.4, Appendix B.3; p1-nemura-payroll slice 1). Every figure is exact until its one named rounding step, and the policies are pure:
// the wages, the rates and the limits come from the caller (the packs and the run), never from here.
const (
	CPFName     = "RND-CPF"
	CPFGradName = "RND-CPF-GRAD"
	SDLName     = "RND-SDL"
)

// dollars is a whole-dollar amount.
func floorDollar(d *apd.Decimal) *apd.Decimal {
	c := work.WithPrecision(60)
	c.Rounding = apd.RoundFloor
	out := new(apd.Decimal)
	if _, err := c.Quantize(out, d, 0); err != nil {
		panic("rounding: floor " + d.String() + ": " + err.Error())
	}
	return out
}

func mul(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Mul(out, a, b); err != nil {
		panic("rounding: multiply " + a.String() + " by " + b.String() + ": " + err.Error())
	}
	return out
}

func sub(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Sub(out, a, b); err != nil {
		panic("rounding: subtract " + b.String() + " from " + a.String() + ": " + err.Error())
	}
	return out
}

func add(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Add(out, a, b); err != nil {
		panic("rounding: add " + a.String() + " and " + b.String() + ": " + err.Error())
	}
	return out
}

// Contributions are CPF in whole dollars: the total, the employee's share and the employer's (the total less the employee's).
type Contributions struct {
	Total, Employee, Employer *apd.Decimal
}

// CPF is RND-CPF: the total contribution is the wages subject to CPF (the capped Ordinary Wages and the Additional Wages together) times the employer's and the employee's rates,
// rounded to the nearest dollar with 50 cents rounding up; the employee's share is the wages times the employee's rate rounded down to the dollar; the employer's is the total
// less the employee's. It applies to the employee-month's total, never to a run (CPF Board; PRD-001 B.3).
func CPF(subject, employer, employee *apd.Decimal) Contributions {
	total := halfAwayFromZero(mul(subject, add(employer, employee)), 0)
	ee := floorDollar(mul(subject, employee))
	return Contributions{Total: total, Employee: ee, Employer: sub(total, ee)}
}

// CPFGraduated is RND-CPF-GRAD: for total wages from S$50 to S$750 the total and the employee's contribution follow the CPF Board's formulas for the wage band, which the pack
// gives exactly; then the dollar rounding of RND-CPF (the total to the nearest dollar, 50 cents up; the employee's share down; the employer's the difference).
func CPFGraduated(total, employee *apd.Decimal) Contributions {
	t := halfAwayFromZero(total, 0)
	ee := floorDollar(employee)
	return Contributions{Total: t, Employee: ee, Employer: sub(t, ee)}
}

// SDLEmployee is RND-SDL's per-employee amount: the levy on the month's total remuneration rounded to the cent, half up. The minimum and maximum are applied by the pack's levy
// on the exact amount, which is the same as applying them after this rounding because they are whole cents.
func SDLEmployee(levy *apd.Decimal) *apd.Decimal { return halfAwayFromZero(levy, 2) }

// SDLPayable is RND-SDL's employer-month amount: the employee amounts added up and the total rounded down to the dollar (CPF Board). The difference, under a dollar, is not
// payable and is not an expense of the month (PRD-001 question 27, decided: the recommended default; the accountant confirms it).
func SDLPayable(amounts []*apd.Decimal) *apd.Decimal {
	sum := apd.New(0, 0)
	for _, a := range amounts {
		sum = add(sum, a)
	}
	return floorDollar(sum)
}
