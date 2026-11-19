package leave_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/packs"
)

func version(t *testing.T) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.Files(), "sg-people/2026.2")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func d(s string) time.Time {
	x, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return x
}

func day(s string) *time.Time { x := d(s); return &x }

// Days are exact hundredths: read and written as decimal strings with an ASCII minus, never a float.
// req: NM-LV-02
func TestDaysAreExactHundredths(t *testing.T) {
	for in, want := range map[string]leave.Days{"7": 700, "7.5": 750, "0.25": 25, "-0.5": -50, "0": 0} {
		got, err := leave.ParseDays(in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "1.234", "x", "--1", "1.-2", "+1"} {
		if _, err := leave.ParseDays(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if s := leave.Days(-50).String(); s != "-0.50" {
		t.Errorf("minus: %s", s)
	}
	if leave.Whole(14).String() != "14.00" {
		t.Error("whole")
	}
}

// The balance is a sum of ledger entries: entitled, taken, booked, expired and encashed are parts of it, and what is left is their difference.
// req: NM-LV-02, NM-LV-13
func TestTheBalanceIsASumOfEntries(t *testing.T) {
	b := leave.Sum([]leave.Entry{
		{Kind: leave.Grant, Amount: leave.Whole(14)},
		{Kind: leave.Use, Amount: -leave.Whole(3)},
		{Kind: leave.Reservation, Amount: -leave.Whole(2)},
		{Kind: leave.Release, Amount: leave.Whole(1)},
		{Kind: leave.Expiry, Amount: -50},
		{Kind: leave.Correction, Amount: 50},
	})
	if b.Entitled != 1450 || b.Taken != 300 || b.Booked != 100 || b.Expired != 50 || b.Left != 1000 {
		t.Errorf("%+v", b)
	}
	if b.Left != b.Entitled-b.Taken-b.Booked-b.Expired-b.Encashed {
		t.Error("left is not the difference")
	}
	for kind, amount := range map[string]leave.Days{leave.Grant: -1, leave.Use: 1, leave.Reservation: 5, leave.Correction: 0, "nonsense": 1} {
		if leave.SignOK(kind, amount) {
			t.Errorf("%s %d passed", kind, amount)
		}
	}
}

// A working pattern says which weekdays count; Sunday is 7.
// req: NM-TIM-01
func TestAPatternCountsWorkingWeekdays(t *testing.T) {
	p := leave.Pattern{Weekdays: []int{1, 2, 3, 4, 7}, HoursPerWeek: 400}
	if err := p.Check(); err != nil || !p.Works(time.Sunday) || p.Works(time.Saturday) || !p.Works(time.Monday) || p.DaysPerWeek() != 5 {
		t.Errorf("pattern: %v", err)
	}
	for _, bad := range []leave.Pattern{{}, {Weekdays: []int{1, 1}, HoursPerWeek: 10}, {Weekdays: []int{0}, HoursPerWeek: 10}, {Weekdays: []int{1}, HoursPerWeek: 0}, {Weekdays: []int{1}, HoursPerWeek: 1681}} {
		if bad.Check() == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}

// Annual leave for a calendar year follows MOM's pro-rating by completed months of service, rounded once to a whole day (under one half down, a half or more
// up), the statute's days from the pack and the company's policy where it gives more; a person with under three months of service, or a contractor, has none.
// req: NM-LV-02, NM-LV-01
func TestAnnualLeaveIsProratedByCompletedMonths(t *testing.T) {
	v := version(t)
	emp := func(start string, leaving *time.Time) leave.Service {
		return leave.Service{Start: d(start), Leaving: leaving, Employee: true, EACovered: true}
	}
	cases := []struct {
		name   string
		s      leave.Service
		policy []int
		year   int
		want   leave.Days
	}{
		{"a full first calendar year", emp("2026-01-01", nil), nil, 2026, leave.Whole(7)},
		{"joined 1 July: six months of 7 days is 3.5, rounded up", emp("2026-07-01", nil), nil, 2026, leave.Whole(4)},
		{"joined 1 October: three months of 7 is 1.75, rounded up", emp("2026-10-01", nil), nil, 2026, leave.Whole(2)},
		{"joined 1 November: two months, no entitlement yet", emp("2026-11-01", nil), nil, 2026, 0},
		{"joined 14 March, left 31 July: four completed months (MOM's example) of 7 days is 2.33, rounded down", emp("2026-03-14", day("2026-07-31")), nil, 2026, leave.Whole(2)},
		{"second calendar year of someone who joined 1 January", emp("2026-01-01", nil), nil, 2027, leave.Whole(8)},
		{"the second year of service starts mid-year: 7 for six months, 8 for six", emp("2026-07-01", nil), nil, 2027, leave.Whole(8)},
		{"the twelfth year stays at the cap of 14", emp("2015-01-01", nil), nil, 2026, leave.Whole(14)},
		{"the policy gives more", emp("2026-01-01", nil), []int{10}, 2026, leave.Whole(10)},
		{"the policy gives less: the statute holds", emp("2026-01-01", nil), []int{3}, 2026, leave.Whole(7)},
		{"the month worked before three completed counts later: year 1 for twelve months and one of year 2 is 7.67, rounded up", emp("2026-12-01", nil), nil, 2027, leave.Whole(8)},
		{"not yet employed", emp("2027-02-01", nil), nil, 2026, 0},
		{"already left", emp("2020-01-01", day("2025-12-31")), nil, 2026, 0},
		{"a contractor", leave.Service{Start: d("2026-01-01"), Employee: false}, []int{14}, 2026, 0},
	}
	for _, c := range cases {
		g, err := leave.AnnualGrant(v, c.s, c.policy, c.year)
		if err != nil || g.Days != c.want || len(g.Basis) == 0 {
			t.Errorf("%s: %s (%v), want %s", c.name, g.Days, err, c.want)
		}
	}
	// A person the Act does not cover gets the company's days alone.
	g, err := leave.AnnualGrant(v, leave.Service{Start: d("2026-01-01"), Employee: true}, []int{12}, 2026)
	if err != nil || g.Days != leave.Whole(12) {
		t.Errorf("not covered: %s %v", g.Days, err)
	}
	if g, _ := leave.AnnualGrant(v, leave.Service{Start: d("2026-01-01"), Employee: true}, nil, 2026); g.Days != 0 {
		t.Errorf("not covered, no policy: %s", g.Days)
	}
}

// The year's unused days carry into the next year for the policy's months and then lapse; days used in the new year use the carried days first.
// req: NM-LV-02
func TestUnusedLeaveCarriesForwardAndLapses(t *testing.T) {
	carry, last := leave.CarryForward(leave.Whole(5), 2026, leave.DefaultCarryMonths)
	if carry != 500 || last.Format(time.DateOnly) != "2027-12-31" {
		t.Errorf("carry %s until %s", carry, last.Format(time.DateOnly))
	}
	if c, _ := leave.CarryForward(0, 2026, 12); c != 0 {
		t.Error("carried nothing")
	}
	if c, _ := leave.CarryForward(500, 2026, 0); c != 0 {
		t.Error("carried with no months")
	}
	if _, last := leave.CarryForward(100, 2026, 3); last.Format(time.DateOnly) != "2027-03-31" {
		t.Errorf("three months: %s", last.Format(time.DateOnly))
	}
	// 5 carried; 2 taken and 1 booked in the new year: 2 lapse; the new year's own days are untouched.
	b := leave.Balance{Entitled: leave.Whole(5 + 8), Taken: leave.Whole(2), Booked: leave.Whole(1), Left: leave.Whole(5 + 8 - 3)}
	if got := leave.ExpiredCarry(leave.Whole(5), b); got != leave.Whole(2) {
		t.Errorf("lapse %s", got)
	}
	if got := leave.ExpiredCarry(leave.Whole(5), leave.Balance{Taken: leave.Whole(6), Left: leave.Whole(2)}); got != 0 {
		t.Errorf("all carried days were used: %s", got)
	}
}
