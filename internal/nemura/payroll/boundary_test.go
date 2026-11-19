package payroll_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	. "github.com/cognizhi/fyniq/internal/nemura/payroll"
)

func ok(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: refused: %v", name, err)
	}
}

func refused(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted", name)
	}
}

// Each limit of a record is checked at its edge, on both sides, so that a limit moved by one is caught (QA-ENG-04): a pay group's name, pay day, cut-off and approval days; an item's key and name; a one-off's
// key, name and note; the 30 items of a pay record; the years of a year-to-date import; an override's reason and decimals.
// req: NM-PAY-03, FR-PAY-14, FR-PAY-06, FR-PAY-17
func TestEachLimitOfARecordIsCheckedAtItsEdge(t *testing.T) {
	g := func(name string, day, cutoff, approval int) PayGroup {
		return PayGroup{Name: name, PayDay: day, CutoffDays: cutoff, ApprovalDays: approval}
	}
	ok(t, "a group at every upper edge", g(strings.Repeat("n", 80), 28, 20, 20).Check())
	ok(t, "a group at every lower edge", g("M", 0, 0, 0).Check())
	refused(t, "an empty name", g("", 25, 7, 3).Check())
	refused(t, "a name of 81", g(strings.Repeat("n", 81), 25, 7, 3).Check())
	refused(t, "pay day 29", g("M", 29, 7, 3).Check())
	refused(t, "pay day -1", g("M", -1, 7, 3).Check())
	refused(t, "cut-off 21", g("M", 25, 21, 3).Check())
	refused(t, "cut-off -1", g("M", 25, -1, 0).Check())
	refused(t, "approval 21", g("M", 25, 20, 21).Check())
	refused(t, "approval -1", g("M", 25, 3, -1).Check())
	refused(t, "approval later than the cut-off", g("M", 25, 3, 4).Check())
	ok(t, "approval on the cut-off", g("M", 25, 3, 3).Check())

	item := func(key, name string) RecurringItem {
		return RecurringItem{Key: key, Name: name, Kind: KindAllowance, Nature: "allowance", Amount: "100.00", CPFWages: true}
	}
	ok(t, "an item at 40 and 80", item(strings.Repeat("k", 40), strings.Repeat("n", 80)).Check())
	refused(t, "a key of 41", item(strings.Repeat("k", 41), "Transport").Check())
	refused(t, "a name of 81", item("transport", strings.Repeat("n", 81)).Check())
	refused(t, "an empty key", item("", "Transport").Check())
	refused(t, "an empty name", item("transport", "").Check())

	one := func(key, name, note string) OneOff {
		return OneOff{Key: key, Name: name, Nature: "bonus", Amount: "500.00", Note: note}
	}
	ok(t, "a one-off at 40, 80 and 300", one(strings.Repeat("k", 40), strings.Repeat("n", 80), strings.Repeat("x", 300)).Check())
	refused(t, "a one-off key of 41", one(strings.Repeat("k", 41), "Bonus", "").Check())
	refused(t, "a one-off name of 81", one("bonus", strings.Repeat("n", 81), "").Check())
	refused(t, "a note of 301", one("bonus", "Bonus", strings.Repeat("x", 301)).Check())
	refused(t, "an empty one-off key", one("", "Bonus", "").Check())
	refused(t, "an empty one-off name", one("bonus", "", "").Check())

	items := func(n int) PayRecord {
		r := PayRecord{Group: "g", Basic: "4500.00"}
		for i := 0; i < n; i++ {
			r.Items = append(r.Items, item(strings.Repeat("k", 1)+string(rune('a'+i%26))+strings.Repeat("x", i/26), "Item"))
		}
		return r
	}
	ok(t, "30 items", items(30).Check())
	refused(t, "31 items", items(31).Check())

	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ytd := func(year int) YTDImport {
		return YTDImport{Year: year, OW: "1000.00", AWSubject: "0.00", AWPaid: "0.00", Through: time.Date(year, 5, 1, 0, 0, 0, 0, time.UTC)}
	}
	ok(t, "year 2020", ytd(2020).Check())
	ok(t, "year 2100", ytd(2100).Check())
	refused(t, "year 2019", ytd(2019).Check())
	refused(t, "year 2101", ytd(2101).Check())
	_ = june

	o := func(reason, value string) Override {
		v, _, _ := apd.NewFromString(value)
		return Override{Field: FieldCPFEmployee, Value: v, Reason: reason}
	}
	ok(t, "a reason of 10", o(strings.Repeat("r", 10), "999.00").Check())
	ok(t, "a reason of 500", o(strings.Repeat("r", 500), "999.00").Check())
	refused(t, "a reason of 9", o(strings.Repeat("r", 9), "999.00").Check())
	refused(t, "a reason of 501", o(strings.Repeat("r", 501), "999.00").Check())
	ok(t, "two decimals", o(strings.Repeat("r", 10), "999.12").Check())
	ok(t, "a zero", o(strings.Repeat("r", 10), "0").Check())
	refused(t, "three decimals", o(strings.Repeat("r", 10), "999.123").Check())
	refused(t, "a negative", o(strings.Repeat("r", 10), "-1.00").Check())
}

// The ratio refuses a denominator of zero and works for one: amount × num ÷ den, rounded once (RND-PAY-RATIO).
// req: RND-PAY-RATIO
func TestARatioNeedsADenominatorAboveZero(t *testing.T) {
	for name, f := range map[string]func(){
		"Ratio with 0":     func() { Ratio(D("100"), 1, 0) },
		"Ratio with -1":    func() { Ratio(D("100"), 1, -1) },
		"RatioDays with 0": func() { RatioDays(D("100"), D("1"), 0) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			f()
		}()
	}
	if got := Ratio(D("100"), 1, 1).Text('f'); got != "100.00" {
		t.Errorf("a ratio of 1 over 1: %s", got)
	}
	if got := RatioDays(D("4500"), D("0.5"), 22).Text('f'); got != "102.27" {
		t.Errorf("half a day of 22: %s", got)
	}
}

// The age the CPF band reads is the year of life the person is in at the end of the month before: the years completed then, plus one; checked each side of a birthday, of a month and of a year.
// req: FR-PAY-02
func TestTheCPFAgeIsReadAtTheEndOfThePreviousMonth(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	for _, tc := range []struct {
		birth, month string
		want         int
	}{
		{"1990-05-20", "2026-06-01", 37}, // 36 completed at 31 May: a birthday on the 20th has passed
		{"1990-05-31", "2026-06-01", 37}, // the birthday is the last day of the month before: completed
		{"1990-06-01", "2026-06-01", 36}, // the birthday is the first of this month: it counts from the next month
		{"1990-06-01", "2026-07-01", 37},
		{"1990-12-31", "2026-01-01", 36}, // 35 completed at 31 Dec 2025
		{"1990-12-31", "2027-01-01", 37},
		{"1990-12-30", "2026-01-01", 36},
		{"1990-01-01", "2026-01-01", 36}, // 35 completed at 31 Dec 2025 (birthday 1 Jan is not yet reached)
		{"1990-01-01", "2026-02-01", 37},
		{"1990-03-15", "2026-03-01", 36}, // end 28 Feb 2026, birthday in March: 35 completed
		{"1990-02-28", "2026-03-01", 37}, // end 28 Feb 2026, birthday that day: 36 completed
		{"1990-02-27", "2026-03-01", 37},
	} {
		if got := CPFAge(d(tc.birth), d(tc.month)); got != tc.want {
			t.Errorf("born %s, month %s: %d, want %d", tc.birth, tc.month, got, tc.want)
		}
	}
}

// An item with no date of its own is classed from the month's pay day: a pay day inside the month is payable by the 14th of the next, one on 20 July is not, and no pay day at all means the last day of the
// month (FR-PAY-17).
// req: FR-PAY-17
func TestAnItemWithNoDateIsClassedFromTheMonthsPayDay(t *testing.T) {
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	item := Item{Key: "commission", Name: "Commission", Nature: "commission", Amount: D("500.00"), CPFWages: true}
	for name, tc := range map[string]struct {
		payDay time.Time
		class  string
	}{
		"no pay day set":        {time.Time{}, Ordinary},
		"a pay day on the 25th": {time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC), Ordinary},
		"a pay day on 14 July":  {time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC), Ordinary},
		"a pay day on 15 July":  {time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), Additional},
		"a pay day on 20 July":  {time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), Additional},
	} {
		r, err := Calculate(Month{Month: june, PayDay: tc.payDay, Person: Person{Employee: true, Residency: Citizen, BirthDate: time.Date(1990, 5, 20, 0, 0, 0, 0, time.UTC)}, Items: []Item{item},
			YTD: YearToDate{Known: true, OW: D("0"), AWSubject: D("0"), AWPaid: D("0")}}, statutoryFor(t))
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Items[0].Class; got != tc.class {
			t.Errorf("%s: %s, want %s", name, got, tc.class)
		}
	}
}

func statutoryFor(t *testing.T) Packs { return statutory(t) }

// The comparison with last month names a joiner, a leaver, an item that changed and one that went, each with its cause, lists the CPF that moved with no change in wages as unexplained, and orders the changes by
// name and then by item, keeping two people of one name in the order they came (NM-PAY-11).
// req: NM-PAY-11
func TestTheComparisonNamesEachChangeAndKeepsItsOrder(t *testing.T) {
	line := func(person, name, gross, cpf string, items ...ItemFacts) LineFacts {
		return LineFacts{Person: person, Name: name, Gross: D(gross), Net: D(gross), CPF: D(cpf), Cost: D(gross), Items: items}
	}
	it := func(key, src, class, amount string) ItemFacts {
		return ItemFacts{Key: key, SourceType: src, Class: class, Amount: D(amount)}
	}
	prev := []LineFacts{
		line("b", "Ben", "5000.00", "1850.00", it("basic", "", "ordinary", "5000.00"), it("transport", "", "ordinary", "0.00"), it("meal", "", "ordinary", "50.00")),
		line("c", "Cy", "4000.00", "1480.00", it("basic", "", "ordinary", "4000.00")),
		line("l", "Leo", "3000.00", "1110.00", it("basic", "", "ordinary", "3000.00")),
		line("e", "Eve", "3500.00", "1295.00", it("basic", "", "ordinary", "3500.00")),
	}
	now := []LineFacts{
		line("b", "Ben", "5400.00", "1998.00", it("basic", "", "ordinary", "5200.00"), it("unpaid-1", "pay_input", "ordinary", "-100.00"), it("ot-1", "overtime_entry", "ordinary", "300.00"),
			it("bonus", "pay_one_off", "additional", "0.00"), it("deduct", "", "deduction", "0.00"), it("transport", "", "ordinary", "0.00")),
		line("c", "Cy", "4000.00", "1500.00", it("basic", "", "ordinary", "4000.00")), // CPF moved with the same wages
		line("a1", "Ana", "2000.00", "740.00", it("basic", "", "ordinary", "2000.00")),
		line("a2", "Ana", "2100.00", "777.00", it("basic", "", "ordinary", "2100.00")),
		line("e", "Eve", "3500.00", "1295.00", it("basic", "", "ordinary", "3500.00")),
	}
	c := Compare(now, prev)
	var got []string
	for _, ch := range c.Changes {
		got = append(got, ch.Name+":"+ch.Key+":"+ch.Cause+":"+ch.Amount.Text('f'))
	}
	want := []string{"Ana::joiner:2000.00", "Ana::joiner:2100.00", "Ben:basic:pay_change:200.00", "Ben:meal:allowance:-50.00", "Ben:ot-1:overtime:300.00", "Ben:unpaid-1:unpaid_leave:-100.00", "Cy::cpf:20.00", "Leo::leaver:-3000.00"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("changes:\n got %v\nwant %v", got, want)
	}
	if len(c.Unexplained) != 1 || c.Unexplained[0].Name != "Cy" {
		t.Errorf("unexplained: %+v", c.Unexplained)
	}
	if c.GrossNow.Text('f') != "17000.00" || c.GrossPrev.Text('f') != "15500.00" {
		t.Errorf("totals: %s %s", c.GrossNow.Text('f'), c.GrossPrev.Text('f'))
	}
	// Each cause, by the item's key, class and source.
	for _, tc := range []struct {
		it   ItemFacts
		want string
	}{
		{it("basic", "", "ordinary", "1"), CausePayChange}, {it("x", "", "deduction", "1"), CauseDeduction}, {it("unpaid-9", "", "ordinary", "1"), CauseUnpaidLeave},
		{it("ot-9", "", "ordinary", "1"), CauseOvertime}, {it("bonus", "pay_one_off", "additional", "1"), CauseOneOff}, {it("transport", "person_pay_item", "ordinary", "1"), CauseAllowance},
	} {
		one := Compare([]LineFacts{line("p", "P", "1", "0", tc.it)}, []LineFacts{line("p", "P", "0", "0")})
		if len(one.Changes) != 1 || one.Changes[0].Cause != tc.want {
			t.Errorf("%+v: %+v", tc.it, one.Changes)
		}
	}
}

// A payslip's lines run in MOM's order and an item that does not apply is left out: overtime hours and the overtime period only beside overtime pay.
// req: NM-PAY-04
func TestAPayslipsLinesLeaveOutWhatDoesNotApply(t *testing.T) {
	has := func(p Payslip, label string) bool {
		for _, r := range p.Lines() {
			if r.Label == label {
				return true
			}
		}
		return false
	}
	base := Payslip{Employer: "A", Employee: "B", PaymentDate: "2026-06-25", Basic: "1.00", PeriodStart: "2026-06-01", PeriodEnd: "2026-06-30", Net: "1.00"}
	if p := base; has(p, "Overtime pay") || has(p, "Overtime hours worked") || has(p, "Overtime payment period") {
		t.Errorf("overtime with no overtime pay: %+v", p.Lines())
	}
	hoursOnly := base
	hoursOnly.OvertimeHours, hoursOnly.OvertimePeriod = "3", "2026-05-20 to 2026-06-30"
	if has(hoursOnly, "Overtime hours worked") || has(hoursOnly, "Overtime payment period") {
		t.Error("overtime hours or period with no overtime pay")
	}
	pay := base
	pay.OvertimePay = "10.00"
	if !has(pay, "Overtime pay") || has(pay, "Overtime hours worked") || has(pay, "Overtime payment period") {
		t.Errorf("overtime pay alone: %+v", pay.Lines())
	}
	pay.OvertimeHours = "3"
	if !has(pay, "Overtime hours worked") || has(pay, "Overtime payment period") {
		t.Errorf("overtime pay and hours: %+v", pay.Lines())
	}
	pay.OvertimePeriod = "2026-05-20 to 2026-06-30"
	if !has(pay, "Overtime payment period") {
		t.Errorf("overtime pay, hours and period: %+v", pay.Lines())
	}
}

// Each kind of item goes under its MOM heading: basic salary, overtime, no-pay leave and a fixed deduction as deductions, a bonus as an additional payment, an allowance (a pay item or a one-off whose nature is
// allowance) as an allowance; a fund, the employee's CPF and reimbursements only when they are more than zero (NM-PAY-04).
// req: NM-PAY-04
func TestEachItemOfAPayslipGoesUnderItsHeading(t *testing.T) {
	d := func(s string) *apd.Decimal { return D(s) }
	p, err := BuildPayslip(PayslipInput{Employer: "A", Employee: "B", Month: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), PayDate: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		Items: []SlipLine{{Key: "basic", Name: "Basic", Amount: "1000.00"}, {Key: "ot-1", Name: "Overtime", Amount: "50.00"}, {Key: "unpaid-1", Name: "Unpaid leave", Amount: "-20.00"},
			{Key: "dorm", Name: "Deduction: Dorm", Class: "deduction", Amount: "30.00"}, {Key: "bonus", Name: "Bonus", Class: "additional", Amount: "100.00", SourceType: "pay_one_off", Nature: "bonus"},
			{Key: "uniform", Name: "Uniform", Amount: "10.00", SourceType: "pay_one_off", Nature: "allowance"}, {Key: "transport", Name: "Transport", Amount: "40.00", SourceType: "person_pay_item"}},
		EmployeeCPF: d("200"), SHG: d("0.00"), OvertimeHours: d("2"), Reimbursements: d("0.00"), Net: d("950.00")})
	if err != nil {
		t.Fatal(err)
	}
	// 1000 + 50 − 20 − 30 + 100 + 10 + 40 − 200 = 950
	if p.Basic != "1000.00" || p.OvertimePay != "50.00" || p.OvertimeHours != "2" || len(p.Allowances) != 2 || len(p.Additional) != 1 || len(p.Deductions) != 3 || p.Reimbursements != "" {
		t.Errorf("payslip: %+v", p)
	}
	for _, x := range p.Deductions {
		if x.Name == "Self-help group fund" {
			t.Error("a fund of nothing is listed")
		}
	}
	none, err := BuildPayslip(PayslipInput{Employer: "A", Employee: "B", Month: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), PayDate: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		Items: []SlipLine{{Key: "basic", Name: "Basic", Amount: "1000.00"}}, EmployeeCPF: nil, SHG: nil, Reimbursements: nil, Net: d("1000.00")})
	if err != nil || len(none.Deductions) != 0 || none.OvertimePay != "" || none.OvertimeHours != "" {
		t.Errorf("a plain month: %+v %v", none, err)
	}
	withShg, err := BuildPayslip(PayslipInput{Employer: "A", Employee: "B", Month: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), PayDate: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		Items: []SlipLine{{Key: "basic", Name: "Basic", Amount: "1000.00"}}, SHG: d("2.00"), Reimbursements: d("45.00"), Net: d("998.00")})
	if err != nil || len(withShg.Deductions) != 1 || withShg.Deductions[0].Name != "Self-help group fund" || withShg.Reimbursements != "45.00" {
		t.Errorf("a fund and reimbursements: %+v %v", withShg, err)
	}
}

// A payment's moves are the PRD's: the table of legal moves, state by state, and the three small answers around it (NM-PAY-10).
// req: NM-PAY-10
func TestEachLegalMoveOfAPaymentIsTheTableOfThePRD(t *testing.T) {
	legal := map[string][]string{
		"":          {"scheduled", "released", "sent", "accepted", "paid", "rejected", "returned", "unknown"},
		"scheduled": {"released", "sent", "accepted", "paid", "rejected", "unknown"},
		"released":  {"sent", "accepted", "paid", "rejected", "unknown"},
		"sent":      {"accepted", "paid", "rejected", "returned", "unknown"},
		"accepted":  {"paid", "rejected", "returned", "unknown"},
		"paid":      {"returned"},
		"rejected":  {"scheduled"},
		"returned":  {"scheduled"},
		"unknown":   {"paid", "rejected", "returned"},
	}
	for from, tos := range legal {
		allowed := map[string]bool{}
		for _, to := range tos {
			allowed[to] = true
		}
		for _, to := range PaymentStates {
			if got := LegalMove(from, to); got != allowed[to] {
				t.Errorf("%q to %s: %v", from, to, got)
			}
		}
	}
	if LegalMove("", "teleported") || LegalMove("paid", "teleported") || ValidState("") || ValidState("teleported") || !ValidState("unknown") {
		t.Error("an invented state")
	}
	for _, s := range PaymentStates {
		if want := s == "rejected" || s == "returned" || s == "unknown"; NeedsAttention(s) != want {
			t.Errorf("%s needs attention: %v", s, NeedsAttention(s))
		}
	}
	for _, tc := range []struct {
		owed, paid int
		want       string
	}{{0, 0, "approved"}, {2, 0, "approved"}, {2, 1, "paid in part"}, {2, 2, "paid"}, {1, 1, "paid"}, {0, 1, "paid in part"}} {
		if got := RunPayState(tc.owed, tc.paid); got != tc.want {
			t.Errorf("%d owed, %d paid: %s", tc.owed, tc.paid, got)
		}
	}
}
