package projectreport

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

func sgd() money.Currency { c, _ := money.Lookup("SGD"); return c }

func amt(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, sgd())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// req: FR-PRJ-12
func TestAvailableHoursAreWeekdaysLessHolidays(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC) }
	// 1 to 31 October 2026: 22 weekdays; 8 October 2026 is not a holiday in the table, so one is added.
	if got := Available(d(1), d(31), nil); got != 22*StandardDayMinutes {
		t.Errorf("October = %d", got)
	}
	if got := Available(d(1), d(31), map[time.Time]string{d(8): "Holiday"}); got != 21*StandardDayMinutes {
		t.Errorf("with a holiday = %d", got)
	}
}

// req: FR-PRJ-12
func TestMarginIsRevenueLessTimeAtCostLessDirectCostsAndSalaryIsNotCountedTwice(t *testing.T) {
	zero := money.Zero(sgd())
	l := Ledger{Revenue: amt(t, "1000.00"), DirectCosts: amt(t, "300.00"), PayrollTagged: amt(t, "5000.00")}
	cost := amt(t, "210.00")
	m, err := Margin(l, Time{Minutes: 180, Cost: &cost})
	if err != nil || m == nil || m.String() != "490.00" {
		t.Fatalf("margin = %v, %v", m, err)
	}
	// Time with no rate to use has no margin; no time needs no rate.
	if m, _ := Margin(l, Time{Minutes: 60}); m != nil {
		t.Errorf("margin without a rate = %v", m)
	}
	if m, _ := Margin(Ledger{Revenue: amt(t, "10.00"), DirectCosts: zero, PayrollTagged: zero}, Time{}); m == nil || m.String() != "10.00" {
		t.Errorf("margin of no time = %v", m)
	}
}

// req: FR-PRJ-12
func TestUtilisationAndRealisationArePercentagesToOneDecimal(t *testing.T) {
	if got, _ := Utilisation(7440, 9360, sgd()); got != "79.5" {
		t.Errorf("utilisation = %q", got)
	}
	if got, _ := Utilisation(100, 0, sgd()); got != "" {
		t.Errorf("no available hours = %q", got)
	}
	std := amt(t, "450.00")
	if got, _ := Realisation(amt(t, "1000.00"), Time{Standard: &std}); got != "222.2" {
		t.Errorf("realisation = %q", got)
	}
	if got, _ := Realisation(amt(t, "1000.00"), Time{}); got != "" {
		t.Errorf("realisation without a standard value = %q", got)
	}
}
