package gstmonitor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/gstmonitor"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func sgd(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, gstmonitor.SGD)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func day(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s)
	return d
}

// req: FR-TAX-05
// The warning compares exact amounts: 800,000.00 is 80% of 1,000,000.00; a cent less is not. The percentage shown is
// rounded, the comparison never is.
func TestTheWarningComparesExactAmounts(t *testing.T) {
	threshold := sgd(t, "1000000.00")
	for _, c := range []struct {
		supplies, percent string
		want            bool
	}{
		{"800000.00", "80", true},
		{"799999.99", "80", false},
		{"0.00", "80", false},
		{"1000000.01", "99", true},
		{"505000.00", "50.5", true},
		{"504999.99", "50.5", false},
	} {
		got, err := gstmonitor.Reaches(sgd(t, c.supplies), threshold, c.percent)
		if err != nil || got != c.want {
			t.Errorf("%s at %s%%: %v, %v; want %v", c.supplies, c.percent, got, err, c.want)
		}
	}
	if _, err := gstmonitor.Reaches(threshold, threshold, "eighty"); err == nil {
		t.Error("a percentage that is not a number was read")
	}
}

// req: FR-TAX-05
// A forecast is dated today or earlier, in Singapore dollars above zero, with a basis; every problem comes back at once.
func TestAForecastKeepsItsRules(t *testing.T) {
	today := day("2026-11-20")
	ok := gstmonitor.Forecast{ForecastOn: today, Value: sgd(t, "1200000.00"), Basis: "Signed contracts"}
	if p := gstmonitor.Check(ok, today); p != nil {
		t.Errorf("a good forecast: %v", p)
	}
	usd := money.MustLookup("USD")
	bad := gstmonitor.Forecast{ForecastOn: day("2026-11-21"), Value: money.Zero(usd), Basis: strings.Repeat("x", gstmonitor.MaxBasis+1)}
	p := gstmonitor.Check(bad, today)
	fields := map[string]bool{}
	for _, x := range p {
		fields[x.Field] = x.Code == gstmonitor.CodeForecast
	}
	if len(p) != 3 || !fields["forecast_on"] || !fields["value"] || !fields["basis"] {
		t.Errorf("a bad forecast: %v", p)
	}
	if p := gstmonitor.Check(gstmonitor.Forecast{Value: sgd(t, "-1.00")}, today); len(p) != 3 {
		t.Errorf("no day, below zero, no basis: %v", p)
	}
	if got := ok.Until().Format(time.DateOnly); got != "2027-11-19" {
		t.Errorf("12 months from 20 Nov 2026 end on %s; want 2027-11-19", got)
	}
}

// req: FR-TAX-05
// The forecast in force is the latest recorded whose 12 months hold the day; a later one supersedes an earlier.
func TestTheForecastInForceIsTheLatestRecorded(t *testing.T) {
	at := func(s string) *time.Time { d := day(s); return &d }
	older := gstmonitor.Recorded{Doc: storage.Document{PostedAt: at("2026-06-01")}, Forecast: gstmonitor.Forecast{ForecastOn: day("2026-06-01"), Basis: "older"}}
	newer := gstmonitor.Recorded{Doc: storage.Document{PostedAt: at("2026-09-01")}, Forecast: gstmonitor.Forecast{ForecastOn: day("2026-09-01"), Basis: "newer"}}
	rs := []gstmonitor.Recorded{newer, older}
	for _, c := range []struct{ on, want string }{
		{"2026-07-01", "older"}, {"2026-10-01", "newer"}, {"2027-08-31", "newer"}, {"2027-09-01", ""}, {"2026-05-31", ""},
	} {
		f, ok := gstmonitor.InForce(rs, day(c.on))
		if (c.want == "" && ok) || (c.want != "" && f.Basis != c.want) {
			t.Errorf("on %s: %q %v; want %q", c.on, f.Basis, ok, c.want)
		}
	}
}
