package payroll_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// The edges the mutation run (QA-ENG-04) found no test on, each worked out by hand from the rules it names.

// FR-PAY-02: the age band changes from the first day of the month after the birthday, so the age the tables read is the years completed by the end of the month
// before, plus one: a birthday on the 1st or the last day of a month is completed by the end of that month, one in a later month is not.
// req: FR-PAY-02
func TestTheAgeTheTablesReadIsTheYearsCompletedByTheEndOfTheMonthBeforePlusOne(t *testing.T) {
	for _, c := range []struct {
		birth, month string
		want         int
	}{
		{"1971-03-01", "2026-03-01", 55}, // 54 completed by 28 February
		{"1971-03-01", "2026-04-01", 56}, // 55 completed by 31 March
		{"1971-03-31", "2026-03-01", 55},
		{"1971-03-31", "2026-04-01", 56}, // the 31st is completed on 31 March itself
		{"1971-01-15", "2026-03-01", 56}, // the birthday was in January: 55 completed by 28 February
		{"1971-11-15", "2026-03-01", 55}, // the birthday is in November: 54 completed
		{"1971-03-15", "2026-03-01", 55}, // the birthday is in March: not yet
	} {
		b, _ := time.Parse("2006-01-02", c.birth)
		m, _ := time.Parse("2006-01-02", c.month)
		if got := payroll.CPFAge(b, m); got != c.want {
			t.Errorf("CPFAge(%s, %s) = %d, want %d", c.birth, c.month, got, c.want)
		}
	}
}

// FR-PAY-14: a last month with no year to date and no additional wages has nothing to true up, and is not refused; one whose true-up comes out at nothing has no refund task.
// req: FR-PAY-14
func TestTheLastMonthNeedsNoYearToDateWhenNothingIsPaidAndRefundsNothingAtExactlyTheCeiling(t *testing.T) {
	packs := oraclePacks(t)
	r, err := payroll.Calculate(payroll.Month{Month: fDay(2026, 12, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("5000.00")}, LastMonth: true}, packs)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	fEq(t, "additional wages subject", r.AWSubject, "0")
	if r.Refund != nil {
		t.Error("a refund task with nothing to refund")
	}
	// Ordinary wages 88,000 + 8,000 = 96,000 leave exactly 6,000 of the ceiling, and 6,000 was counted: nothing to add and nothing to refund.
	exact := payroll.Month{Month: fDay(2026, 12, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("8000.00")}, LastMonth: true,
		YTD: payroll.YearToDate{Known: true, OW: fDec("88000"), AWSubject: fDec("6000"), AWPaid: fDec("6000")}}
	r, err = payroll.Calculate(exact, packs)
	if err != nil {
		t.Fatal(err)
	}
	fEq(t, "additional wages subject", r.AWSubject, "0")
	if r.Refund != nil {
		t.Errorf("a refund task of %s with nothing to refund", r.Refund.Wages.Text('f'))
	}
	// An additional wage with no year to date is refused (the sentinel the run shows as a blocking error).
	_, err = payroll.Calculate(payroll.Month{Month: fDay(2026, 6, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("5000.00"),
		{Key: "b", Name: "Bonus", Amount: fDec("1.00"), CPFWages: true, ForMonth: fDay(2025, 12, 1), PayableOn: fDay(2026, 6, 30)}}}, packs)
	if !errors.Is(err, payroll.ErrNeedsYearToDate) {
		t.Errorf("err = %v, want ErrNeedsYearToDate", err)
	}
}

// FR-PAY-16: the limits are inclusive: exactly 50% of the salary, exactly 25% for an advance and exactly 12 instalments pass, and a deduction of nothing is itself refused.
// req: FR-PAY-16
func TestTheDeductionLimitsAreInclusive(t *testing.T) {
	salary := fDec("4000.00")
	keys := func(ds []payroll.Deduction, final bool) []string {
		var out []string
		for _, v := range payroll.CheckDeductions(salary, ds, final) {
			out = append(out, v.Key)
		}
		return out
	}
	adv := func(amount string, instalment int) payroll.Deduction {
		return payroll.Deduction{Key: "a", Kind: payroll.DedRecovery, Amount: fDec(amount), Instalment: instalment, IsAdvance: true}
	}
	damage := func(amount string) payroll.Deduction {
		return payroll.Deduction{Key: "d", Kind: payroll.DedDamage, Amount: fDec(amount)}
	}
	if got := keys([]payroll.Deduction{damage("2000.00")}, false); len(got) != 0 {
		t.Errorf("exactly 50%% refused: %v", got)
	}
	if got := keys([]payroll.Deduction{damage("2000.01")}, false); len(got) != 1 || got[0] != "" {
		t.Errorf("over 50%%: %v", got)
	}
	if got := keys([]payroll.Deduction{adv("1000.00", 12)}, false); len(got) != 0 {
		t.Errorf("exactly 25%% and 12 instalments refused: %v", got)
	}
	if got := keys([]payroll.Deduction{adv("1000.01", 1)}, false); len(got) != 1 {
		t.Errorf("over 25%%: %v", got)
	}
	if got := keys([]payroll.Deduction{adv("100.00", 13)}, false); len(got) != 1 {
		t.Errorf("a 13th instalment: %v", got)
	}
	if got := keys([]payroll.Deduction{damage("0.00")}, false); len(got) != 1 || got[0] != "d" {
		t.Errorf("a deduction of nothing: %v", got)
	}
	if got := keys([]payroll.Deduction{damage("-1.00")}, false); len(got) != 1 {
		t.Errorf("a negative deduction: %v", got)
	}
}

// The working a payslip shows carries amounts to the cent.
// req: FR-PAY-01
func TestTheWorkingShowsAmountsToTheCent(t *testing.T) {
	r, err := payroll.Calculate(payroll.Month{Month: fDay(2026, 6, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("5000")}}, oraclePacks(t))
	if err != nil {
		t.Fatal(err)
	}
	var text []string
	for _, s := range r.Working {
		text = append(text, s.Text)
	}
	if all := strings.Join(text, "\n"); !strings.Contains(all, "Gross pay 5000.00") {
		t.Errorf("the working does not show the gross to the cent:\n%s", all)
	}
}
