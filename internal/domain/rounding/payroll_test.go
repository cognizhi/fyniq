package rounding_test

import (
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

func dec(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// RND-CPF: the total to the nearest dollar with 50 cents up, the employee's share down, the employer's the difference; applied once, to the month's wages (PRD-001 B.3).
// Hand-worked: 5,000 at 17% and 20% is 1,850 in all and 1,000 for the employee, 850 for the employer; 3,336.50 at 37% is 1,234.505, so 1,235 in all, and 20% of it is 667.30, so 667
// for the employee; 3,333.33 at 37% is 1,233.3321, so 1,233, and 20% of it is 666.666, so 666 for the employee; a half exactly rounds up.
// req: RND-CPF
func TestRNDCPFRoundsTheTotalToTheNearestDollarAndTheEmployeesShareDown(t *testing.T) {
	for _, tc := range []struct {
		wages, er, ee, total, employee, employer string
	}{
		{"5000.00", "0.17", "0.20", "1850", "1000", "850"},
		{"3336.50", "0.17", "0.20", "1235", "667", "568"},
		{"3333.33", "0.17", "0.20", "1233", "666", "567"},
		{"1000.00", "0.17", "0.20", "370", "200", "170"},
		{"2500.00", "0.085", "0.04", "313", "100", "213"}, // 312.5 rounds up; the employee's 100.00 is exact
		{"2501.00", "0.085", "0.04", "313", "100", "213"}, // 312.625 up; the employee's 100.04 down
		{"0.00", "0.17", "0.20", "0", "0", "0"},
	} {
		got := rounding.CPF(dec(tc.wages), dec(tc.er), dec(tc.ee))
		if got.Total.Text('f') != tc.total || got.Employee.Text('f') != tc.employee || got.Employer.Text('f') != tc.employer {
			t.Errorf("%s at %s and %s: total %s employee %s employer %s, want %s %s %s", tc.wages, tc.er, tc.ee, got.Total.Text('f'), got.Employee.Text('f'), got.Employer.Text('f'), tc.total, tc.employee, tc.employer)
		}
		if rounding.CPFName != "RND-CPF" {
			t.Error("the policy's name")
		}
	}
}

// RND-CPF-GRAD: in the graduated range the exact total and employee amounts follow the Board's formulas and then take RND-CPF's dollar rounding. A total of 277.50 (wages 750 at 17%
// plus 60% of 250) is 278 and the employee's 150.00 is 150, so the employer's is 128; 162.00 (600) is 162 and 60.00 is 60; a total of 100.49 is 100 and 100.50 is 101.
// req: RND-CPF-GRAD
func TestRNDCPFGradRoundsTheBandsExactAmountsToTheDollar(t *testing.T) {
	for _, tc := range []struct{ total, employee, wantTotal, wantEmployee, wantEmployer string }{
		{"277.50", "150.00", "278", "150", "128"},
		{"162.00", "60.00", "162", "60", "102"},
		{"100.49", "0", "100", "0", "100"},
		{"100.50", "0", "101", "0", "101"},
		{"58.50", "29.999", "59", "29", "30"},
	} {
		got := rounding.CPFGraduated(dec(tc.total), dec(tc.employee))
		if got.Total.Text('f') != tc.wantTotal || got.Employee.Text('f') != tc.wantEmployee || got.Employer.Text('f') != tc.wantEmployer {
			t.Errorf("%s %s: %s %s %s, want %s %s %s", tc.total, tc.employee, got.Total.Text('f'), got.Employee.Text('f'), got.Employer.Text('f'), tc.wantTotal, tc.wantEmployee, tc.wantEmployer)
		}
	}
	if rounding.CPFGradName != "RND-CPF-GRAD" {
		t.Error("the policy's name")
	}
}

// RND-SDL: each employee's levy to the cent, half up; the employer-month payable is the sum rounded down to the dollar, so 7.50 + 2.00 + 11.25 = 20.75 pays 20 and the 0.75 is not payable;
// 7.505 is 7.51 for the employee; nothing to add pays nothing.
// req: RND-SDL
func TestRNDSDLRoundsEachEmployeeToTheCentAndTheEmployerMonthDownToTheDollar(t *testing.T) {
	if got := rounding.SDLEmployee(dec("7.505")).Text('f'); got != "7.51" {
		t.Errorf("a half cent: %s", got)
	}
	if got := rounding.SDLEmployee(dec("7.5049")).Text('f'); got != "7.50" {
		t.Errorf("under a half cent: %s", got)
	}
	if got := rounding.SDLPayable([]*apd.Decimal{dec("7.50"), dec("2.00"), dec("11.25")}).Text('f'); got != "20" {
		t.Errorf("payable %s", got)
	}
	if got := rounding.SDLPayable([]*apd.Decimal{dec("0.99")}).Text('f'); got != "0" {
		t.Errorf("under a dollar: %s", got)
	}
	if got := rounding.SDLPayable(nil).Text('f'); got != "0" {
		t.Errorf("none: %s", got)
	}
	if rounding.SDLName != "RND-SDL" {
		t.Error("the policy's name")
	}
}
