package sharepay

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

func price(t *testing.T, s string) money.UnitPrice {
	t.Helper()
	p, err := money.ParseUnitPrice(s, money.MustLookup("SGD"), "fair_value")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func codeAndField(err error) (string, string) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, e.Field
	}
	return "", ""
}

// Whole calendar months, both ends counted.
// req: FR-EQ-13
func TestMonthsAreWholeCalendarMonthsBothCounted(t *testing.T) {
	for _, c := range []struct {
		start, vest string
		want        int
	}{{"2026-01-01", "2026-12-31", 12}, {"2026-01-01", "2026-01-01", 1}, {"2026-11-01", "2027-02-01", 4}, {"2026-03-01", "2026-02-28", 0}, {"2026-01-01", "2028-01-15", 25}} {
		if got := MonthsBetween(day(c.start), day(c.vest)); got != c.want {
			t.Errorf("%s to %s = %d, want %d", c.start, c.vest, got, c.want)
		}
	}
}

// A grant that cannot be worked is refused in plain words naming the field.
// req: FR-EQ-13
func TestAGrantThatCannotBeWorkedIsRefused(t *testing.T) {
	ok := []Tranche{{Shares: 10, VestOn: day("2026-06-30")}}
	for name, c := range map[string]struct {
		fv       string
		start    string
		tranches []Tranche
		code     string
		field    string
	}{
		"no fair value":           {"0", "2026-01-01", ok, CodeGrant, "fair_value"},
		"a start inside a month":  {"1", "2026-01-02", ok, CodeGrant, "service_start"},
		"no tranche":              {"1", "2026-01-01", nil, CodeGrant, "tranches"},
		"no shares":               {"1", "2026-01-01", []Tranche{{Shares: 10, VestOn: day("2026-03-31")}, {Shares: 0, VestOn: day("2026-03-31")}}, CodeTranche, "tranche 2"},
		"vested before the start": {"1", "2026-03-01", []Tranche{{Shares: 1, VestOn: day("2026-02-28")}}, CodeTranche, "tranche 1"},
	} {
		_, err := Work(price(t, c.fv), day(c.start), c.tranches)
		if code, field := codeAndField(err); code != c.code || field != c.field {
			t.Errorf("%s: %v (%s, %s), want %s, %s", name, err, code, field, c.code, c.field)
		}
	}
	if _, err := Work(price(t, "1"), day("2026-01-01"), []Tranche{{Shares: 1, VestOn: day("2025-06-01")}}); err == nil || !strings.Contains(err.Error(), "before its service starts in January 2026") {
		t.Errorf("a tranche before the service: %v", err)
	}
}

// The months of a grant with tranches of different lengths are in order, and the last month is the longest tranche's.
// req: FR-EQ-13
func TestTheMonthsRunToTheLongestTranche(t *testing.T) {
	e, err := Work(price(t, "1.00"), day("2026-01-01"), []Tranche{{Shares: 600, VestOn: day("2026-03-31")}, {Shares: 1200, VestOn: day("2026-06-30")}})
	if err != nil || len(e.Months) != 6 || e.Total.String() != "1800.00" {
		t.Fatalf("%+v, %v", e, err)
	}
	for i, want := range []string{"2026-01 400.00", "2026-02 400.00", "2026-03 400.00", "2026-04 200.00", "2026-05 200.00", "2026-06 200.00"} {
		if got := e.Months[i].Month.Format("2006-01") + " " + e.Months[i].Expense.String(); got != want {
			t.Errorf("month %d: %s, want %s", i+1, got, want)
		}
	}
}

// A tranche vesting in the month its service starts is one month; the messages name the tranche that is wrong.
// req: FR-EQ-13
func TestATrancheVestingInItsFirstMonthIsOneMonthAndRefusalsNameTheTranche(t *testing.T) {
	e, err := Work(price(t, "2.50"), day("2026-05-01"), []Tranche{{Shares: 4, VestOn: day("2026-05-31")}})
	if err != nil || len(e.Months) != 1 || e.Tranches[0].Months != 1 || e.Months[0].Expense.String() != "10.00" {
		t.Fatalf("%+v, %v", e, err)
	}
	_, err = Work(price(t, "1"), day("2026-01-01"), []Tranche{{Shares: 1, VestOn: day("2026-03-31")}, {Shares: 0, VestOn: day("2026-03-31")}})
	if err == nil || !strings.Contains(err.Error(), "tranche 2 has no shares") {
		t.Errorf("no shares: %v", err)
	}
	_, err = Work(price(t, "1"), day("2026-03-01"), []Tranche{{Shares: 1, VestOn: day("2026-03-31")}, {Shares: 1, VestOn: day("2026-02-28")}})
	if err == nil || !strings.Contains(err.Error(), "tranche 2 vests on 28 Feb 2026, before its service starts in March 2026") {
		t.Errorf("vested before: %v", err)
	}
}
