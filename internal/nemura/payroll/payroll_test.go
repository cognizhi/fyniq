package payroll_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/packs"
	. "github.com/cognizhi/fyniq/internal/nemura/payroll"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func statutory(t *testing.T) Packs {
	t.Helper()
	all, err := pack.LoadAll(packs.StatutoryFiles())
	if err != nil {
		t.Fatal(err)
	}
	var p Packs
	for _, v := range all {
		switch v.ID {
		case "sg-cpf@2026.2":
			p.CPF = v
		case "sg-sdl@2026.1":
			p.SDL = v
		case "sg-shg@2026.1":
			p.SHG = v
		}
	}
	if p.CPF.ID == "" || p.SDL.ID == "" || p.SHG.ID == "" {
		t.Fatalf("the statutory packs are not all there: %q %q %q", p.CPF.ID, p.SDL.ID, p.SHG.ID)
	}
	return p
}

func basic(amount string) Item {
	return Item{Key: "basic", Name: "Basic salary", Nature: "basic", Amount: D(amount), CPFWages: true, Suggested: Ordinary}
}

func citizen() Person {
	return Person{Employee: true, Residency: Citizen, BirthDate: day("1990-05-20")}
}

func text(d *apd.Decimal) string { return d.Text('f') }

// A citizen of 36 on S$5,000 a month: CPF at 17% and 20% is 1,850 in all, 1,000 for the employee and 850 for the employer (RND-CPF); SDL is 0.25% of 5,000, which is 12.50, held at the
// S$11.25 maximum; net pay is 5,000 less the employee's 1,000; the employer's cost is 5,000 + 850 + 11.25 (PRD-001 B.3).
// req: FR-PAY-01, FR-PAY-02, FR-PAY-03, RND-CPF, RND-SDL
func TestAMonthOfOrdinaryWagesIsTheWorkedBoardFigures(t *testing.T) {
	r, err := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("5000.00")}, YTD: YearToDate{Known: true, OW: D("25000"), AWSubject: D("0"), AWPaid: D("0")}}, statutory(t))
	if err != nil {
		t.Fatal(err)
	}
	if text(r.CPF.Total) != "1850" || text(r.CPF.Employee) != "1000" || text(r.CPF.Employer) != "850" || r.CPFGroup != "sc_spr3" {
		t.Errorf("CPF: %s %s %s %s", text(r.CPF.Total), text(r.CPF.Employee), text(r.CPF.Employer), r.CPFGroup)
	}
	if text(r.SDL) != "11.25" || text(r.Net) != "4000.00" || text(r.EmployerCost) != "5861.25" || text(r.Disburse) != "4000.00" || len(r.Working) == 0 {
		t.Errorf("SDL %s net %s cost %s disburse %s", text(r.SDL), text(r.Net), text(r.EmployerCost), text(r.Disburse))
	}
	if len(r.Flags) != 0 || r.PackVersions[0] != "sg-cpf@2026.2" {
		t.Errorf("flags %+v versions %v", r.Flags, r.PackVersions)
	}
}

// The Ordinary Wage ceiling caps what is subject to CPF: S$9,000 is S$8,000 subject, at 37% S$2,960 in all, S$1,600 for the employee and S$1,360 for the employer, and the gross, net pay and SDL
// are on the whole S$9,000 (FR-PAY-02).
// req: FR-PAY-02
func TestTheOrdinaryWageCeilingCapsWhatIsSubjectToCPFButNotWhatIsPaid(t *testing.T) {
	r, err := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("9000.00")}, YTD: YearToDate{Known: true, OW: D("0"), AWSubject: D("0"), AWPaid: D("0")}}, statutory(t))
	if err != nil {
		t.Fatal(err)
	}
	if text(r.OWSubject) != "8000.00" || text(r.CPF.Total) != "2960" || text(r.CPF.Employee) != "1600" || text(r.CPF.Employer) != "1360" || text(r.Gross) != "9000.00" || text(r.Net) != "7400.00" {
		t.Errorf("%s %s %s %s gross %s net %s", text(r.OWSubject), text(r.CPF.Total), text(r.CPF.Employee), text(r.CPF.Employer), text(r.Gross), text(r.Net))
	}
}

// A first-year permanent resident on S$700 is in the graduated range: at the graduated rates of both the employer and the employee the exact total is 4% of 700 plus 15% of the 200 over 500, which
// is 28 + 30 = 58, the employee's 30; RND-CPF-GRAD takes the dollar rounding, so the employer's is 28. A joint election reads the other group (CPF Board table 2).
// req: FR-PAY-02, RND-CPF-GRAD
func TestAFirstYearPermanentResidentInTheGraduatedRange(t *testing.T) {
	spr := Person{Employee: true, Residency: PermanentResident, PRStart: ptr(day("2026-02-10")), BirthDate: day("1990-05-20")}
	r, err := Calculate(Month{Month: day("2026-06-01"), Person: spr, Items: []Item{basic("700.00")}}, statutory(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.CPFGroup != "spr1_gg" || text(r.CPF.Total) != "58" || text(r.CPF.Employee) != "30" || text(r.CPF.Employer) != "28" {
		t.Errorf("%s %s %s %s", r.CPFGroup, text(r.CPF.Total), text(r.CPF.Employee), text(r.CPF.Employer))
	}
	spr.JointElection = true
	r, err = Calculate(Month{Month: day("2026-06-01"), Person: spr, Items: []Item{basic("2000.00")}}, statutory(t))
	if err != nil || r.CPFGroup != "spr1_fg" {
		t.Errorf("a joint election: %s %v", r.CPFGroup, err)
	}
	// Under S$50 nothing is contributed; a citizen on S$600 pays the graduated formula at 17% with a 60% slope above 500.
	r, _ = Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("40.00")}}, statutory(t))
	if text(r.CPF.Total) != "0" {
		t.Errorf("under S$50: %s", text(r.CPF.Total))
	}
	r, _ = Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("600.00")}}, statutory(t))
	if text(r.CPF.Total) != "162" || text(r.CPF.Employee) != "60" || text(r.CPF.Employer) != "102" {
		t.Errorf("S$600: %s %s %s", text(r.CPF.Total), text(r.CPF.Employee), text(r.CPF.Employer))
	}
}

func ptr(t time.Time) *time.Time { return &t }

// The age band changes from the first day of the month after the birthday, and the year of permanent residence runs to the end of the month of each anniversary (CPF Board).
// req: FR-PAY-02, FR-PAY-14
func TestTheAgeBandAndThePRYearChangeAtTheStartOfTheNextMonth(t *testing.T) {
	birth := day("1971-03-15") // 55 on 15 March 2026
	for month, want := range map[string]int{"2026-02-01": 55, "2026-03-01": 55, "2026-04-01": 56, "2026-05-01": 56} {
		if got := CPFAge(birth, day(month)); got != want {
			t.Errorf("%s: age %d, want %d", month, got, want)
		}
	}
	start := day("2025-06-10")
	for month, want := range map[string]int{"2025-06-01": 1, "2026-06-01": 1, "2026-07-01": 2, "2027-06-01": 2, "2027-07-01": 3} {
		if got := PRYear(start, day(month)); got != want {
			t.Errorf("%s: PR year %d, want %d", month, got, want)
		}
	}
	// A citizen of 55 on S$5,000 contributes at the 55 to 60 rates from April, not March.
	p := statutory(t)
	before, _ := Calculate(Month{Month: day("2026-03-01"), Person: Person{Employee: true, Residency: Citizen, BirthDate: birth}, Items: []Item{basic("5000.00")}}, p)
	after, _ := Calculate(Month{Month: day("2026-04-01"), Person: Person{Employee: true, Residency: Citizen, BirthDate: birth}, Items: []Item{basic("5000.00")}}, p)
	if text(before.CPF.Total) != "1850" || text(after.CPF.Total) != "1700" || text(after.CPF.Employee) != "900" {
		t.Errorf("before %s after %s employee %s", text(before.CPF.Total), text(after.CPF.Total), text(after.CPF.Employee))
	}
}

// A pay item is ordinary or additional wages by its facts, not its name: a bonus for the month payable by the 14th of the next month is ordinary, and a monthly "allowance" paid for
// another month or after the 14th is additional; a name that disagrees is reported; an item that is not CPF wages is neither (FR-PAY-17).
// req: FR-PAY-17
func TestAPayItemsClassFollowsItsFactsNotItsName(t *testing.T) {
	month, payDay := day("2026-06-01"), day("2026-06-30")
	for _, tc := range []struct {
		name     string
		it       Item
		class    string
		mismatch bool
	}{
		{"commission for June, paid 10 July", Item{Name: "Commission", Amount: D("300"), CPFWages: true, ForMonth: month, PayableOn: day("2026-07-10"), Suggested: Additional}, Ordinary, true},
		{"commission for June, paid 20 July", Item{Name: "Commission", Amount: D("300"), CPFWages: true, ForMonth: month, PayableOn: day("2026-07-20"), Suggested: Additional}, Additional, false},
		{"quarterly commission for April to June", Item{Name: "Commission", Amount: D("900"), CPFWages: true, ForMonth: day("2026-04-01"), PayableOn: day("2026-06-30")}, Additional, false},
		{"annual bonus", Item{Name: "Bonus", Amount: D("5000"), CPFWages: true, ForMonth: day("2025-12-01"), PayableOn: day("2026-06-30"), Suggested: Additional}, Additional, false},
		{"allowance with no dates", Item{Name: "Allowance", Amount: D("200"), CPFWages: true, Suggested: Ordinary}, Ordinary, false},
		{"not CPF wages", Item{Name: "Transport", Amount: D("50"), CPFWages: false}, NotWages, false},
	} {
		c := Classify(tc.it, month, payDay)
		if c.Class != tc.class || c.Mismatch != tc.mismatch {
			t.Errorf("%s: %s mismatch %v (%s)", tc.name, c.Class, c.Mismatch, c.Why)
		}
	}
	// A mismatch is a ⚠ on the month, not a refusal.
	r, err := Calculate(Month{Month: month, PayDay: payDay, Person: citizen(), Items: []Item{basic("5000.00"), {Key: "c", Name: "Commission", Amount: D("300"), CPFWages: true, ForMonth: month, PayableOn: day("2026-07-10"), Suggested: Additional}}}, statutory(t))
	if err != nil || len(r.Flags) != 1 || r.Flags[0].Fail || r.Flags[0].Code != "WAGE_CLASS" || text(r.OrdinaryWages) != "5300.00" {
		t.Errorf("%+v %v ordinary %s", r.Flags, err, text(r.OrdinaryWages))
	}
}

// Additional wages are limited by the annual ceiling less the year's ordinary wages. A bonus of S$20,000 in June on S$5,000 a month: the year's ordinary wages are estimated at 5,000 a month for
// twelve months, 60,000, so the ceiling 102,000 leaves 42,000 and all 20,000 is subject; at 37% that is 7,400 on the bonus. A bonus of S$50,000 would be limited to the 42,000 left less the 20,000 already
// counted: 22,000. With no year to date the month cannot be calculated, and ✖ says why (FR-PAY-14).
// req: FR-PAY-02, FR-PAY-14
func TestAdditionalWagesAreLimitedByTheAnnualCeilingAndNeedAYearToDate(t *testing.T) {
	p := statutory(t)
	bonus := func(a string) Item {
		return Item{Key: "bonus", Name: "Bonus", Amount: D(a), CPFWages: true, ForMonth: day("2025-12-01"), PayableOn: day("2026-06-30"), Suggested: Additional}
	}
	ytd := YearToDate{Known: true, OW: D("25000"), AWSubject: D("0"), AWPaid: D("0")} // January to May at 5,000
	r, err := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("5000.00"), bonus("20000.00")}, YTD: ytd}, p)
	if err != nil {
		t.Fatal(err)
	}
	if text(r.AWSubject) != "20000.00" || text(r.CPF.Total) != "9250" || text(r.CPF.Employee) != "5000" {
		t.Errorf("subject %s total %s employee %s", text(r.AWSubject), text(r.CPF.Total), text(r.CPF.Employee))
	}
	ytd2 := YearToDate{Known: true, OW: D("30000"), AWSubject: D("20000"), AWPaid: D("20000")}
	r, err = Calculate(Month{Month: day("2026-07-01"), Person: citizen(), Items: []Item{basic("5000.00"), bonus("50000.00")}, YTD: ytd2}, p)
	if err != nil || text(r.AWSubject) != "22000.00" {
		t.Errorf("a second bonus: %s %v", text(r.AWSubject), err)
	}
	if _, err := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("5000.00"), bonus("20000.00")}}, p); !errors.Is(err, ErrNeedsYearToDate) {
		t.Errorf("no year to date: %v", err)
	}
}

// The last month trues the year up. A person on S$8,000 a month paid a S$30,000 bonus in March has a year of ordinary wages of 96,000, so the ceiling 102,000 leaves 6,000 of additional wages:
// the March estimate counted what the estimate allowed, and December adds a shortfall or raises a refund for the excess with the employee's share (refund ÷ total rate × employee rate, cents dropped).
// req: FR-PAY-14
func TestTheLastMonthTruesTheYearUpAndRaisesARefundForAnExcess(t *testing.T) {
	p := statutory(t)
	bonus := Item{Key: "b", Name: "Bonus", Amount: D("0"), CPFWages: true, ForMonth: day("2025-12-01"), PayableOn: day("2026-12-31"), Suggested: Additional}
	// The year's ordinary wages came to 96,000 and 30,000 of additional wages was counted in March: the ceiling allows 6,000, so 24,000 was counted beyond it.
	ytd := YearToDate{Known: true, OW: D("88000"), AWSubject: D("30000"), AWPaid: D("30000")}
	r, err := Calculate(Month{Month: day("2026-12-01"), Person: citizen(), Items: []Item{basic("8000.00"), bonus}, YTD: ytd, LastMonth: true}, p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Refund == nil || text(r.Refund.Wages) != "24000.00" || text(r.Refund.Total) != "8880.0000" && !strings.HasPrefix(text(r.Refund.Total), "8880") || text(r.AWSubject) != "0.00" && text(r.AWSubject) != "0" {
		t.Fatalf("refund %+v subject %s", r.Refund, text(r.AWSubject))
	}
	if !strings.HasPrefix(text(r.Refund.Employee), "4800") {
		t.Errorf("the employee's share of the refund: %s", text(r.Refund.Employee))
	}
	// A shortfall is added to the last month: ordinary wages of 60,000, additional wages of 20,000 counted of 30,000 paid, the ceiling allows 30,000, so 10,000 more is subject.
	ytd = YearToDate{Known: true, OW: D("52000"), AWSubject: D("20000"), AWPaid: D("30000")}
	r, err = Calculate(Month{Month: day("2026-12-01"), Person: citizen(), Items: []Item{basic("8000.00"), bonus}, YTD: ytd, LastMonth: true}, p)
	if err != nil || r.Refund != nil || !strings.HasPrefix(text(r.AWSubject), "10000") {
		t.Errorf("a shortfall: %+v %s %v", r.Refund, text(r.AWSubject), err)
	}
}

// A foreign employee contributes no CPF but pays SDL; a contractor has none of CPF, SDL or the fund; a self-help group fund comes from the table unless the employee opted out (FR-PAY-03, FR-PAY-04).
// req: FR-PAY-03, FR-PAY-04
func TestForeignersPayNoCPFButSDLAndSelfHelpFundsFollowTheTableUnlessOptedOut(t *testing.T) {
	p := statutory(t)
	r, _ := Calculate(Month{Month: day("2026-06-01"), Person: Person{Employee: true, Residency: Foreigner, BirthDate: day("1990-01-01")}, Items: []Item{basic("3000.00")}}, p)
	if text(r.CPF.Total) != "0" || r.CPFGroup != "none" || text(r.SDL) != "7.50" || text(r.Net) != "3000.00" {
		t.Errorf("a foreigner: cpf %s group %s sdl %s net %s", text(r.CPF.Total), r.CPFGroup, text(r.SDL), text(r.Net))
	}
	r, _ = Calculate(Month{Month: day("2026-06-01"), Person: Person{Residency: Foreigner}, Items: []Item{basic("3000.00")}}, p)
	if text(r.SDL) != "0" || text(r.SHG) != "0" || text(r.Net) != "3000.00" {
		t.Errorf("a contractor: %s %s %s", text(r.SDL), text(r.SHG), text(r.Net))
	}
	with := citizen()
	with.SHGFund = "CDAC"
	r, _ = Calculate(Month{Month: day("2026-06-01"), Person: with, Items: []Item{basic("3000.00")}}, p) // 2,000 to 3,500 is 1.00
	if text(r.SHG) != "1.00" || text(r.Net) != "3000.00"[:0]+"2399.00" {
		t.Errorf("CDAC on 3,000: %s net %s", text(r.SHG), text(r.Net))
	}
	with.SHGOptOut = true
	r, _ = Calculate(Month{Month: day("2026-06-01"), Person: with, Items: []Item{basic("3000.00")}}, p)
	if text(r.SHG) != "0" {
		t.Errorf("opted out: %s", text(r.SHG))
	}
}

// The employer-month SDL payable is the employee amounts added up and rounded down to the dollar, never the sum of the roundings; the run's postings balance whatever the figures: gross + employer
// CPF + SDL + reimbursements = deductions + 2100 + 2110 (B.3, RND-SDL).
// req: FR-PAY-03, FR-PAY-18, RND-SDL
func TestARunsSDLPayableIsRoundedDownOnceAndItsPostingsBalance(t *testing.T) {
	p := statutory(t)
	people := []string{"3000.00", "700.00", "6000.00", "1234.56"} // levies 7.50, 2.00 (minimum), 11.25 (maximum), 3.09 (3.0864)
	var results []Result
	for i, pay := range people {
		m := Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic(pay)}}
		if i == 3 {
			m.Reimbursements = []*apd.Decimal{D("45.00")}
			m.Deductions = []Deduction{{Key: "adv", Kind: DedRecovery, Amount: D("100.00"), IsAdvance: true, Instalment: 2}}
			m.Held = D("0")
		}
		r, err := Calculate(m, p)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	tot := Total(results)
	if text(tot.SDLEmployees) != "23.84" || text(tot.SDLPayable) != "23" {
		t.Errorf("SDL employees %s payable %s", text(tot.SDLEmployees), text(tot.SDLPayable))
	}
	if !Balanced(Postings(tot)) {
		t.Errorf("the postings do not balance: %+v", Postings(tot))
	}
	if text(tot.Reimbursements) != "45.00" || text(tot.Deductions) != "100.00" || text(tot.Disburse) == text(tot.Net) {
		t.Errorf("reimbursements %s deductions %s disburse %s net %s", text(tot.Reimbursements), text(tot.Deductions), text(tot.Disburse), text(tot.Net))
	}
}

// A later run or correction recomputes the whole employee-month and only the difference is posted: a second run adds a S$500 allowance to S$5,000, and the difference is the S$500, the CPF on it
// and the SDL change, with the month's CPF computed once on S$5,500 (B.3, NM-PAY-09).
// req: FR-PAY-01, NM-PAY-09
func TestALaterRunOfTheSameMonthPostsOnlyTheDifference(t *testing.T) {
	p := statutory(t)
	first, _ := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("5000.00")}}, p)
	both, err := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("5000.00"), {Key: "a", Name: "Allowance", Amount: D("500.00"), CPFWages: true, Suggested: Ordinary}}}, p)
	if err != nil {
		t.Fatal(err)
	}
	d := Diff(both, &first)
	if text(d.Gross) != "500.00" || text(d.EmployeeCPF) != "100" || text(d.EmployerCPF) != "85" || text(d.SDL) != "0.00" && text(d.SDL) != "0" {
		t.Errorf("difference: gross %s employee %s employer %s sdl %s", text(d.Gross), text(d.EmployeeCPF), text(d.EmployerCPF), text(d.SDL))
	}
	if all := Diff(first, nil); text(all.Gross) != "5000.00" || text(all.EmployerCPF) != "850" {
		t.Errorf("nothing posted yet: %s %s", text(all.Gross), text(all.EmployerCPF))
	}
	if same := Diff(first, &first); text(same.Gross) != "0.00" && text(same.Gross) != "0" {
		t.Errorf("the same month again: %s", text(same.Gross))
	}
}

// Deductions are checked apart from the journal: only the kinds MOM allows, consent for any other purpose, at most 50% of the salary not counting the exempt kinds, an advance in at most 12
// instalments each at most 25% of the salary, and on termination an advance only as far as consented (FR-PAY-16).
// req: FR-PAY-16
func TestSalaryDeductionsAreCheckedAgainstMOMsLimits(t *testing.T) {
	salary := D("4000.00")
	rules := func(ds []Deduction, final bool) string {
		var out []string
		for _, v := range CheckDeductions(salary, ds, final) {
			out = append(out, v.Key+":"+v.Text[:12])
		}
		return strings.Join(out, "|")
	}
	if got := rules([]Deduction{{Key: "a", Kind: DedAbsence, Amount: D("3000")}, {Key: "b", Kind: DedDamage, Amount: D("1900")}}, false); got != "" {
		t.Errorf("absence is exempt and 1,900 is under half: %q", got)
	}
	if got := rules([]Deduction{{Key: "a", Kind: DedDamage, Amount: D("1500")}, {Key: "b", Kind: DedAccommodation, Amount: D("600")}}, false); !strings.Contains(got, ":Deductions, ") {
		t.Errorf("over 50%%: %q", got)
	}
	if got := rules([]Deduction{{Key: "a", Kind: DedDamage, Amount: D("1500")}, {Key: "b", Kind: DedAccommodation, Amount: D("600")}}, true); got != "" {
		t.Errorf("no 50%% limit on termination: %q", got)
	}
	if got := rules([]Deduction{{Key: "o", Kind: DedOther, Amount: D("50")}}, false); !strings.Contains(got, "o:A deduction ") {
		t.Errorf("no consent: %q", got)
	}
	if got := rules([]Deduction{{Key: "o", Kind: DedOther, Amount: D("50"), Consent: true}}, false); got != "" {
		t.Errorf("with consent: %q", got)
	}
	if got := rules([]Deduction{{Key: "x", Kind: "fine", Amount: D("50")}}, false); !strings.Contains(got, "x:That kind of") {
		t.Errorf("a kind MOM does not allow: %q", got)
	}
	if got := rules([]Deduction{{Key: "adv", Kind: DedRecovery, Amount: D("1000"), IsAdvance: true, Instalment: 3}}, false); got != "" {
		t.Errorf("25%% of 4,000 is exactly 1,000: %q", got)
	}
	if got := rules([]Deduction{{Key: "adv", Kind: DedRecovery, Amount: D("1000.01"), IsAdvance: true, Instalment: 3}}, false); !strings.Contains(got, "adv:An advance i") {
		t.Errorf("over 25%%: %q", got)
	}
	if got := rules([]Deduction{{Key: "adv", Kind: DedRecovery, Amount: D("200"), IsAdvance: true, Instalment: 13}}, false); !strings.Contains(got, "adv:An advance i") {
		t.Errorf("a thirteenth instalment: %q", got)
	}
	if got := rules([]Deduction{{Key: "adv", Kind: DedRecovery, Amount: D("2500"), IsAdvance: true, Instalment: 5}}, true); !strings.Contains(got, "adv:On terminati") {
		t.Errorf("an advance from final pay without consent: %q", got)
	}
	if got := rules([]Deduction{{Key: "adv", Kind: DedRecovery, Amount: D("2500"), IsAdvance: true, Instalment: 5, Consent: true}}, true); got != "" {
		t.Errorf("an advance from final pay with consent: %q", got)
	}
	// A violation is ✖ on the month.
	r, _ := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("4000.00")}, Deductions: []Deduction{{Key: "x", Kind: "fine", Amount: D("50")}}}, statutory(t))
	if len(r.DeductionViolations) != 1 || len(r.Flags) != 1 || !r.Flags[0].Fail {
		t.Errorf("flags %+v", r.Flags)
	}
}

// For a thousand generated employee-months the identities of Appendix B.3 hold exactly, whatever the figures: CPF is the employer's and the employee's, net pay is gross less the employee's CPF, the
// fund and the deductions, the amount to disburse adds reimbursements and takes the monies held, and a run's postings balance; no figure has a fraction of a cent, and the whole-dollar CPF has none of a dollar
// (nothing rounds outside a named policy).
// req: FR-PAY-18, QA-ENG-02
func TestGeneratedEmployeeMonthsKeepTheIdentitiesOfB3(t *testing.T) {
	p := statutory(t)
	seed := uint64(20261120)
	next := func(n uint64) uint64 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return (seed >> 33) % n
	}
	money := func(max uint64) *apd.Decimal { return apd.New(int64(next(max*100)), -2) }
	var all []Result
	for i := 0; i < 1000; i++ {
		person := Person{Employee: next(10) > 0, Residency: []string{Citizen, PermanentResident, Foreigner}[next(3)], BirthDate: day("1960-01-01").AddDate(int(next(45)), int(next(12)), int(next(28))), SHGFund: []string{"", "CDAC", "ECF", "MBMF", "SINDA"}[next(5)]}
		if person.Residency == PermanentResident {
			start := day("2024-01-01").AddDate(0, int(next(36)), 0)
			person.PRStart = &start
			person.JointElection = next(2) == 0
		}
		items := []Item{basic(money(12000).Text('f'))}
		if next(3) == 0 {
			items = append(items, Item{Key: "a", Name: "Allowance", Amount: money(800), CPFWages: next(4) > 0, Suggested: Ordinary})
		}
		if next(5) == 0 {
			items = append(items, Item{Key: "u", Name: "Unpaid leave", Nature: "unpaid_leave", Amount: sub(apd.New(0, 0), money(300)), CPFWages: true, Suggested: Ordinary})
		}
		m := Month{Month: day("2026-06-01"), Person: person, Items: items}
		if next(4) == 0 {
			m.Reimbursements = []*apd.Decimal{money(200)}
		}
		if next(6) == 0 {
			m.Deductions = []Deduction{{Key: "d", Kind: DedAbsence, Amount: money(150)}}
		}
		if next(8) == 0 {
			m.Held = money(500)
		}
		r, err := Calculate(m, p)
		if err != nil {
			continue // a month the pack does not hold a figure for (an age with no band, a wage with no band) is refused, not guessed
		}
		all = append(all, r)
		if r.CPF.Total.Cmp(add2(r.CPF.Employee, r.CPF.Employer)) != 0 {
			t.Fatalf("CPF total %s is not employee %s + employer %s", text(r.CPF.Total), text(r.CPF.Employee), text(r.CPF.Employer))
		}
		if r.CPF.Total.Exponent < 0 && !r.CPF.Total.IsZero() || r.CPF.Employee.Exponent < 0 && !r.CPF.Employee.IsZero() {
			t.Fatalf("CPF has cents: %s %s", text(r.CPF.Total), text(r.CPF.Employee))
		}
		if r.Net.Cmp(sub2(sub2(sub2(r.Gross, r.CPF.Employee), r.SHG), r.Deductions)) != 0 || r.Disburse.Cmp(sub2(add2(r.Net, r.Reimbursements), r.Held)) != 0 {
			t.Fatalf("net %s or disburse %s breaks its identity", text(r.Net), text(r.Disburse))
		}
		if r.OWSubject.Cmp(D("8000")) > 0 {
			t.Fatalf("ordinary wages subject %s are over the ceiling", text(r.OWSubject))
		}
	}
	if len(all) < 800 {
		t.Fatalf("only %d of 1000 months could be calculated", len(all))
	}
	for from := 0; from+40 <= len(all); from += 40 {
		if !Balanced(Postings(Total(all[from : from+40]))) {
			t.Fatalf("the postings of run %d do not balance", from/40)
		}
	}
}

func add2(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(60).Add(out, a, b); err != nil {
		panic(err)
	}
	return out
}

func sub2(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(60).Sub(out, a, b); err != nil {
		panic(err)
	}
	return out
}

func sub(a, b *apd.Decimal) *apd.Decimal { return sub2(a, b) }
func add(a, b *apd.Decimal) *apd.Decimal { return add2(a, b) }

// A pay group's calendar: the pay day, the last day inputs are accepted and the latest approval are worked from the day of the month (0 is the last day); a group must be whole (NM-PAY-03).
// req: NM-PAY-03
func TestAPayGroupsCalendarIsWorkedFromItsPayDay(t *testing.T) {
	g := PayGroup{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3}
	if err := g.Check(); err != nil {
		t.Fatal(err)
	}
	month := day("2026-06-01")
	if got := g.PayDate(month); !got.Equal(day("2026-06-25")) || !g.CutoffDate(month).Equal(day("2026-06-18")) || !g.ApprovalDate(month).Equal(day("2026-06-22")) {
		t.Errorf("%v %v %v", got, g.CutoffDate(month), g.ApprovalDate(month))
	}
	last := PayGroup{Name: "Month end", PayDay: 0, CutoffDays: 5, ApprovalDays: 2}
	if got := last.PayDate(day("2026-02-01")); !got.Equal(day("2026-02-28")) {
		t.Errorf("the last day of February: %v", got)
	}
	for _, bad := range []PayGroup{{Name: " ", PayDay: 25}, {Name: "x", PayDay: 29}, {Name: "x", PayDay: 25, CutoffDays: 21}, {Name: "x", PayDay: 25, CutoffDays: 2, ApprovalDays: 3}} {
		if err := bad.Check(); err == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}

// A pay record, its items, a year to date and a one-off are whole or refused with the field named: an amount is dollars with two decimals; a deduction is never CPF wages; a key is used once;
// additional wages subject to CPF are no more than were paid; a month is the first day (FR-EMP-01, FR-PAY-14, FR-PAY-17).
// req: FR-EMP-01, FR-PAY-14
func TestPayRecordsAreWholeOrRefusedWithTheFieldNamed(t *testing.T) {
	ok := PayRecord{Group: "g", Basic: "4500.00", SHGFund: "CDAC", Items: []RecurringItem{{Key: "transport", Name: "Transport allowance", Kind: KindAllowance, Nature: "allowance", Amount: "100.00", CPFWages: true},
		{Key: "dorm", Name: "Dormitory", Kind: KindFixedDeduction, Nature: "accommodation", Amount: "200.00"}}}
	if err := ok.Check(); err != nil {
		t.Fatal(err)
	}
	mutate := func(f func(p *PayRecord)) error {
		p := ok
		p.Items = append([]RecurringItem(nil), ok.Items...)
		f(&p)
		return p.Check()
	}
	for name, f := range map[string]func(p *PayRecord){
		"no group":                   func(p *PayRecord) { p.Group = "" },
		"basic with cents":           func(p *PayRecord) { p.Basic = "4500.005" },
		"basic negative":             func(p *PayRecord) { p.Basic = "-1" },
		"a fund":                     func(p *PayRecord) { p.SHGFund = "UNICEF" },
		"a deduction as wages":       func(p *PayRecord) { p.Items[1].CPFWages = true },
		"a nature of the other kind": func(p *PayRecord) { p.Items[0].Nature = "accommodation" },
		"a zero amount":              func(p *PayRecord) { p.Items[0].Amount = "0" },
		"a repeated key":             func(p *PayRecord) { p.Items[1].Key = "transport" },
	} {
		if err := mutate(f); err == nil {
			t.Errorf("%s passed", name)
		} else if r, isR := err.(*Refusal); !isR || r.Code != CodeInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	y := YTDImport{Year: 2026, OW: "25000.00", AWSubject: "0", AWPaid: "0", Through: day("2026-05-01")}
	if err := y.Check(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]YTDImport{"subject over paid": {Year: 2026, OW: "1", AWSubject: "5", AWPaid: "4", Through: day("2026-05-01")}, "a month of another year": {Year: 2026, OW: "1", AWSubject: "0", AWPaid: "0", Through: day("2025-05-01")},
		"not the first": {Year: 2026, OW: "1", AWSubject: "0", AWPaid: "0", Through: day("2026-05-02")}, "a negative": {Year: 2026, OW: "-1", AWSubject: "0", AWPaid: "0", Through: day("2026-05-01")}} {
		if bad.Check() == nil {
			t.Errorf("%s passed", name)
		}
	}
	if (OneOff{Key: "b", Name: "Bonus", Nature: "bonus", Amount: "5000.00"}).Check() != nil || (OneOff{Key: "b", Name: "Bonus", Nature: "gift", Amount: "5000.00"}).Check() == nil {
		t.Error("a one-off's nature")
	}
	fm := day("2026-06-02")
	if (OneOff{Key: "b", Name: "Bonus", Nature: "bonus", Amount: "5000.00", ForMonth: &fm}).Check() == nil {
		t.Error("a one-off for a month that is not the first")
	}
}

// Amounts worked out by a ratio are exact and rounded once to the cent, half away from zero (NM-PAY-12): a joiner who starts on 11 June earns 20 of 30 days of S$4,500; 7 of 31 days of S$4,000
// is 903.2258…, so 903.23; an unpaid day of 22 working days of S$4,500 is 204.5454…, so 204.55; three hours of overtime at 1.5 times on S$4,500 over 44 hours is 243,000 ÷ 2,288 = 106.2062…, so 106.21.
// req: NM-PAY-12
func TestAmountsWorkedOutByARatioAreRoundedOnceToTheCent(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		got        *apd.Decimal
	}{
		{"a part month of 20 of 30 days", "3000.00", Ratio(D("4500.00"), 20, 30)},
		{"7 of 31 days", "903.23", Ratio(D("4000.00"), 7, 31)},
		{"a full month", "4500.00", Ratio(D("4500.00"), 31, 31)},
		{"an unpaid day of 22 working days", "204.55", Ratio(D("4500.00"), 1, 22)},
		{"a half cent rounds up", "0.01", Ratio(D("0.01"), 1, 2)},
		{"three hours of overtime", "106.21", OvertimePay(D("4500.00"), D("44"), D("1.5"), D("3"))},
		{"no overtime hours", "0.00", OvertimePay(D("4500.00"), D("44"), D("1.5"), D("0"))},
	} {
		if text(tc.got) != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, text(tc.got), tc.want)
		}
	}
	if RatioName != "RND-PAY-RATIO" {
		t.Error("the policy's name")
	}
}

// An override replaces one calculated figure with a reason and is flagged: the CPF total follows its shares, net pay and the amount to disburse follow, the original is in the working, and a
// reason under ten characters or a figure with a fraction of a cent is refused (FR-PAY-06).
// req: FR-PAY-06
func TestAnOverrideReplacesOneFigureWithAReasonAndIsFlagged(t *testing.T) {
	r, err := Calculate(Month{Month: day("2026-06-01"), Person: citizen(), Items: []Item{basic("5000.00")}, YTD: YearToDate{Known: true, OW: D("25000"), AWSubject: D("0"), AWPaid: D("0")}}, statutory(t))
	if err != nil {
		t.Fatal(err)
	}
	o := Override{Field: FieldCPFEmployee, Value: D("999.00"), Reason: "The CPF Board's own figure for the month."}
	if err := o.Check(); err != nil {
		t.Fatal(err)
	}
	got := o.Apply(r)
	if text(got.CPF.Employee) != "999.00" || text(got.CPF.Employer) != "850" || text(got.CPF.Total) != "1849.00" || text(got.Net) != "4001.00" || text(got.Disburse) != "4001.00" || text(got.EmployerCost) != "5861.25" {
		t.Errorf("after the override: employee %s employer %s total %s net %s disburse %s", text(got.CPF.Employee), text(got.CPF.Employer), text(got.CPF.Total), text(got.Net), text(got.Disburse))
	}
	flagged := false
	for _, f := range got.Flags {
		flagged = flagged || f.Code == "OVERRIDE" && strings.Contains(f.Message, "The CPF Board's own figure")
	}
	if !flagged || len(got.Overridden) != 1 || got.Overridden[0] != FieldCPFEmployee || !strings.Contains(got.Working[len(got.Working)-1].Text, "replaces the calculated 1000") {
		t.Errorf("flags %+v overridden %v working %+v", got.Flags, got.Overridden, got.Working[len(got.Working)-1])
	}
	if text(r.CPF.Employee) != "1000" {
		t.Error("the original result changed")
	}
	for _, bad := range []Override{{Field: "gross", Value: D("1.00"), Reason: "A long enough reason."}, {Field: FieldSDL, Value: D("1.00"), Reason: "short"}, {Field: FieldSDL, Value: D("1.005"), Reason: "A long enough reason."}, {Field: FieldSDL, Value: D("-1.00"), Reason: "A long enough reason."}} {
		if bad.Check() == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}

// The run is set against last month's with every difference explained by its cause and the item it comes from: a joiner and a leaver, a pay change, a one-off item, unpaid leave, overtime; and
// a person whose CPF changed with no change in their wages is listed for the approver (NM-PAY-11).
// req: NM-PAY-11
func TestTheRunIsComparedWithLastMonthWithEveryDifferenceExplained(t *testing.T) {
	line := func(id, name, gross, cpf string, items ...ItemFacts) LineFacts {
		return LineFacts{Person: id, Name: name, Gross: D(gross), Net: D(gross), CPF: D(cpf), Cost: D(gross), Items: items}
	}
	item := func(key, src, amount string) ItemFacts {
		return ItemFacts{Key: key, SourceType: src, Class: "ordinary", Amount: D(amount)}
	}
	prev := []LineFacts{
		line("ana", "Ana Tan", "5000.00", "1850", item("basic", "person_pay", "5000.00")),
		line("ben", "Ben Lim", "4000.00", "1480", item("basic", "person_pay", "4000.00")),
		line("cy", "Cy Wong", "3000.00", "1110", item("basic", "person_pay", "3000.00")),
		line("eve", "Eve Ong", "3500.00", "1295", item("basic", "person_pay", "3500.00")),
	}
	now := []LineFacts{
		line("ana", "Ana Tan", "10000.00", "3700", item("basic", "person_pay", "5000.00"), ItemFacts{Key: "bonus", SourceType: "pay_one_off", Class: "additional", Amount: D("5000.00")}),
		line("ben", "Ben Lim", "4500.00", "1665", item("basic", "person_pay", "4500.00")),
		line("cy", "Cy Wong", "2795.45", "1034", item("basic", "person_pay", "3000.00"), item("unpaid-1a2b3c4d", "pay_input", "-204.55")),
		line("dee", "Dee Foo", "2000.00", "740", item("basic", "person_pay", "2000.00")),
		line("eve", "Eve Ong", "3500.00", "1190", item("basic", "person_pay", "3500.00")),
	}
	got := Compare(now, prev)
	if text(got.GrossNow) != "22795.45" || text(got.GrossPrev) != "15500.00" {
		t.Errorf("gross %s against %s", text(got.GrossNow), text(got.GrossPrev))
	}
	causes := map[string]string{}
	for _, c := range got.Changes {
		causes[c.Name+"/"+c.Cause+"/"+c.Key] = text(c.Amount)
	}
	for k, want := range map[string]string{"Ana Tan/one_off/bonus": "5000.00", "Ben Lim/pay_change/basic": "500.00", "Cy Wong/unpaid_leave/unpaid-1a2b3c4d": "-204.55", "Dee Foo/joiner/": "2000.00", "Eve Ong/cpf/": "-105"} {
		if causes[k] != want {
			t.Errorf("%s: %q, want %s (all: %v)", k, causes[k], want, causes)
		}
	}
	if len(got.Unexplained) != 1 || got.Unexplained[0].Name != "Eve Ong" || !strings.Contains(got.Unexplained[0].Why, "no change in wages") {
		t.Errorf("unexplained: %+v", got.Unexplained)
	}
	// A person who left is a leaver with the whole of last month's gross pay.
	left := Compare(now[:1], prev[:2])
	found := false
	for _, c := range left.Changes {
		found = found || c.Cause == CauseLeaver && c.Name == "Ben Lim" && text(c.Amount) == "-4000.00"
	}
	if !found {
		t.Errorf("a leaver: %+v", left.Changes)
	}
}
