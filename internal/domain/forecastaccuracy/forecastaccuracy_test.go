package forecastaccuracy

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

// A snapshot taken on Monday 2 November 2026 with the projection of 4 weeks: it expected a customer's 5,000.00 invoice on 12 November, a supplier's 1,200.00 bill on
// 20 November, another 800.00 bill on 10 December (after week 4, which ends on 29 November) and a 300.00 recurring bill, and ended week 4 at 9,100.00 from 5,000.00.
func snapshot() Projection {
	weeks := []Week{{"2026-11-02", "2026-11-08", "0.00", "0.00", "5000.00"}, {"2026-11-09", "2026-11-15", "5000.00", "0.00", "10000.00"}, {"2026-11-16", "2026-11-22", "0.00", "1200.00", "8800.00"},
		{"2026-11-23", "2026-11-29", "0.00", "300.00", "9100.00"}}
	return Projection{Today: "2026-11-02", Opening: "5000.00", Weekly: weeks, Items: []Item{
		{"inv1", "invoice", "inv1", "2026-11-12", "5000.00"}, {"bill1", "bill", "bill1", "2026-11-20", "-1200.00"}, {"bill2", "bill", "bill2", "2026-12-10", "-800.00"},
		{"rb1", "recurring_bill", "rb1", "2026-11-25", "-300.00"}}}
}

// The causes always add to the difference exactly, and each is signed as the cash moved.
// req: FR-PLN-04
func TestTheCausesAddToTheDifferenceExactly(t *testing.T) {
	p := snapshot()
	cases := []struct {
		name                           string
		actual                         string
		settled                        map[string]time.Time
		diff, later, timing, unplanned string
	}{
		{"as projected", "9100.00", map[string]time.Time{"inv1": day("2026-11-12"), "bill1": day("2026-11-20")}, "0.00", "0.00", "0.00", "0.00"},
		{"the customer paid after the window", "4100.00", map[string]time.Time{"bill1": day("2026-11-20")}, "-5000.00", "-5000.00", "0.00", "0.00"},
		{"the supplier was paid late", "10300.00", map[string]time.Time{"inv1": day("2026-11-12")}, "1200.00", "0.00", "1200.00", "0.00"},
		{"a bill expected later was paid early", "8300.00", map[string]time.Time{"inv1": day("2026-11-12"), "bill1": day("2026-11-20"), "bill2": day("2026-11-27")}, "-800.00", "0.00", "-800.00", "0.00"},
		{"unplanned spend", "8000.00", map[string]time.Time{"inv1": day("2026-11-12"), "bill1": day("2026-11-20")}, "-1100.00", "0.00", "0.00", "-1100.00"},
		{"all of them", "5000.00", map[string]time.Time{"bill2": day("2026-11-29")}, "-4100.00", "-5000.00", "400.00", "500.00"},
	}
	for _, c := range cases {
		r, err := Compare(p, 4, c.actual, c.settled)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if r.Projected != "9100.00" || r.WindowEnd != "2026-11-29" || r.Difference != c.diff || r.Causes.ReceiptsLater != c.later || r.Causes.Timing != c.timing || r.Causes.Unplanned != c.unplanned {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
}

// A horizon the snapshot does not reach, a bad date and a bad amount are refused.
// req: FR-PLN-04
func TestAHorizonBeyondTheSnapshotOrABadFigureIsRefused(t *testing.T) {
	p := snapshot()
	if _, err := Compare(p, 13, "1.00", nil); err == nil {
		t.Error("13 weeks of a 4-week snapshot")
	}
	if _, err := Compare(p, 0, "1.00", nil); err == nil {
		t.Error("0 weeks")
	}
	if _, err := Compare(p, 4, "lots", nil); err == nil {
		t.Error("an amount that is not a decimal")
	}
	bad := snapshot()
	bad.Weekly[3].To = "soon"
	if _, err := Compare(bad, 4, "1.00", nil); err == nil {
		t.Error("a week that ends on no date")
	}
}
