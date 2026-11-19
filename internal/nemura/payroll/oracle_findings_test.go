package payroll_test

import (
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	nemurapacks "github.com/cognizhi/fyniq/internal/nemura/packs"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// The disagreements the differential run against the independent oracle found (QA-ENG-02; p1-nemura-payroll 1.5): each is a month worked out by hand
// from PRD-001 B.3 and FR-PAY-02, FR-PAY-14 and FR-PAY-18, and failed before the engine was corrected.

func oraclePacks(t *testing.T) payroll.Packs {
	t.Helper()
	var out payroll.Packs
	for dir, dst := range map[string]*pack.Version{"sg-cpf/2026.2": &out.CPF, "sg-sdl/2026.1": &out.SDL, "sg-shg/2026.1": &out.SHG} {
		v, err := pack.Load(nemurapacks.StatutoryFiles(), dir)
		if err != nil {
			t.Fatal(err)
		}
		*dst = v
	}
	return out
}

func fDec(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func fDay(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func fCitizen() payroll.Person {
	return payroll.Person{Employee: true, Residency: payroll.Citizen, BirthDate: fDay(1990, 5, 20)}
}

func fBasic(amount string) payroll.Item {
	return payroll.Item{Key: "basic", Name: "Basic salary", Amount: fDec(amount), CPFWages: true}
}

func fEq(t *testing.T, what string, got *apd.Decimal, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s is not worked out, want %s", what, want)
		return
	}
	if got.Cmp(fDec(want)) != 0 {
		t.Errorf("%s = %s, want %s", what, got.Text('f'), want)
	}
}

// B.3 posts an advance recovered to 1250 and the other deductions to their own accounts, and FR-PAY-18 keeps them apart: the run's totals say how much of
// the deductions is advances.
// req: FR-PAY-18
func TestARunsEntryCreditsAdvancesRecoveredToAccount1250(t *testing.T) {
	m := payroll.Month{Month: fDay(2026, 6, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("4000.00")},
		Deductions: []payroll.Deduction{
			{Key: "adv", Kind: payroll.DedRecovery, Amount: fDec("500.00"), Instalment: 1, IsAdvance: true},
			{Key: "dmg", Kind: payroll.DedDamage, Amount: fDec("100.00")},
		}}
	r, err := payroll.Calculate(m, oraclePacks(t))
	if err != nil {
		t.Fatal(err)
	}
	totals := payroll.Total([]payroll.Result{r})
	fEq(t, "advances", totals.Advances, "500.00")
	fEq(t, "deductions", totals.Deductions, "600.00")
	credits := map[string]*apd.Decimal{}
	for _, l := range payroll.Postings(totals) {
		credits[l.Account] = l.Credit
	}
	fEq(t, "Cr 1250", credits["1250"], "500.00")
	fEq(t, "Cr the other deductions", credits["deductions"], "100.00")
	if !payroll.Balanced(payroll.Postings(totals)) {
		t.Error("the entry does not balance")
	}
}

// FR-PAY-14: the last month's true-up adds a shortfall to the month as additional wages subject to CPF, and the wage band is the month's wages subject to CPF
// (the sg-cpf pack's definition of total wages), so S$6.57 of ordinary wages and S$13,993.43 of shortfall are S$14,000 at 37%: 5,180, the employee 2,800.
// The ordinary wages 88,000 + 6.57 leave 13,993.43 of the S$102,000 ceiling for the 96,995.80 paid and none counted.
// req: FR-PAY-14
// req: FR-PAY-02
func TestATrueUpShortfallIsInTheWagesTheBandAndTheRatesAreReadOn(t *testing.T) {
	m := payroll.Month{Month: fDay(2026, 12, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("6.57")}, LastMonth: true,
		YTD: payroll.YearToDate{Known: true, OW: fDec("88000"), AWSubject: fDec("0"), AWPaid: fDec("96995.80")}}
	r, err := payroll.Calculate(m, oraclePacks(t))
	if err != nil {
		t.Fatal(err)
	}
	fEq(t, "additional wages subject", r.AWSubject, "13993.43")
	fEq(t, "CPF total", r.CPF.Total, "5180")
	fEq(t, "CPF employee", r.CPF.Employee, "2800")
	fEq(t, "CPF employer", r.CPF.Employer, "2380")
}

// FR-PAY-02: additional wages above the annual ceiling are not subject to CPF, so they do not move the contribution by the wage band either: S$245.43 of ordinary
// wages and S$191.72 of additional wages the ceiling leaves no room for are S$245.43 subject to CPF, at the graduated 17% of a citizen aged 36 (41.72 -> 42, nothing by the
// employee), not 17% of the S$437.15 paid.
// The year's ordinary wages are estimated at 96,000 + 245.43 x 5 = 97,227.15, 102,000 less that and the 5,661.54 counted leaves nothing.
// req: FR-PAY-02
// req: FR-PAY-14
func TestAdditionalWagesAboveTheCeilingDoNotMoveTheGraduatedContribution(t *testing.T) {
	m := payroll.Month{Month: fDay(2026, 8, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("245.43"),
		{Key: "c", Name: "Commission", Amount: fDec("191.72"), CPFWages: true, ForMonth: fDay(2026, 8, 1), PayableOn: fDay(2026, 9, 28)}},
		YTD: payroll.YearToDate{Known: true, OW: fDec("96000"), AWSubject: fDec("5661.54"), AWPaid: fDec("5661.54")}}
	r, err := payroll.Calculate(m, oraclePacks(t))
	if err != nil {
		t.Fatal(err)
	}
	fEq(t, "additional wages subject", r.AWSubject, "0")
	fEq(t, "CPF total", r.CPF.Total, "42")
	fEq(t, "CPF employee", r.CPF.Employee, "0")
}

// FR-PAY-14: the refund task and the employee's share are worked out whatever band the month's own wages are in. The ordinary wages 88,000 + 0.10 leave 13,999.90 of the
// ceiling for the 14,961.56 counted: 961.66 is refundable. The contributions on it at 37% are 355.81 -> 356 (RND-CPF's dollar), and the employee's share is the refund
// divided by the total rate times the employee rate, 356 / 0.37 x 0.20 = 192.43, and the CPF Board drops the cents: 192 (its own worked example, $2,260 / 32.5% x 17% =
// $1,182.15 -> $1,182; cpf.gov.sg, checked 20 Nov 2026). The refund's total is an estimate the accountant confirms [accountant to confirm].
// req: FR-PAY-14
func TestARefundIsWorkedOutInTheGraduatedRangeToo(t *testing.T) {
	m := payroll.Month{Month: fDay(2026, 12, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("0.10")}, LastMonth: true,
		YTD: payroll.YearToDate{Known: true, OW: fDec("88000"), AWSubject: fDec("14961.56"), AWPaid: fDec("14961.56")}}
	r, err := payroll.Calculate(m, oraclePacks(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Refund == nil {
		t.Fatal("no refund task")
	}
	fEq(t, "refund wages", r.Refund.Wages, "961.66")
	fEq(t, "refund total", r.Refund.Total, "356")
	fEq(t, "refund employee share", r.Refund.Employee, "192")
}

// FR-PAY-14: the employee's share of a refund is whole dollars, as the CPF Board's example shows ("cents should be dropped": $2,260 / 32.5% x 17% = $1,182.15 is $1,182).
// 8,100.07 refundable at 37% is 2,997.03 -> 2,997 contributions, and 2,997 / 0.37 x 0.20 = 1,620 exactly.
// req: FR-PAY-14
func TestTheEmployeesShareOfARefundIsWholeDollars(t *testing.T) {
	m := payroll.Month{Month: fDay(2026, 12, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("8000.00")}, LastMonth: true,
		YTD: payroll.YearToDate{Known: true, OW: fDec("89999.93"), AWSubject: fDec("12100.14"), AWPaid: fDec("12100.14")}}
	r, err := payroll.Calculate(m, oraclePacks(t))
	if err != nil {
		t.Fatal(err)
	}
	fEq(t, "refund wages", r.Refund.Wages, "8100.07")
	fEq(t, "refund total", r.Refund.Total, "2997")
	fEq(t, "refund employee share", r.Refund.Employee, "1620")
}

// [verify] The band is the month's wages subject to CPF (the sg-cpf pack's definition of total wages), so S$103.77 of ordinary wages with S$20,000 of additional wages the
// annual ceiling leaves no room for stay in the graduated range: 17% of 103.77 = 17.64 -> 18, nothing by the employee, and not the full 37% (38, the employee 20) the
// S$20,103.77 paid would give. The CPF Board's page does not say which wages choose the band; the accountant confirms.
// req: FR-PAY-02
func TestTheBandIsChosenOnTheWagesSubjectToCPFNotThoseAboveTheCeiling(t *testing.T) {
	m := payroll.Month{Month: fDay(2026, 6, 1), Person: fCitizen(), Items: []payroll.Item{fBasic("103.77"),
		{Key: "c", Name: "Commission", Amount: fDec("20000.00"), CPFWages: true, ForMonth: fDay(2026, 6, 1), PayableOn: fDay(2026, 7, 28)}},
		YTD: payroll.YearToDate{Known: true, OW: fDec("101500"), AWSubject: fDec("0"), AWPaid: fDec("0")}}
	r, err := payroll.Calculate(m, oraclePacks(t))
	if err != nil {
		t.Fatal(err)
	}
	fEq(t, "additional wages subject", r.AWSubject, "0")
	fEq(t, "CPF total", r.CPF.Total, "18")
	fEq(t, "CPF employee", r.CPF.Employee, "0")
}
