package closing

import (
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var sgd = money.MustLookup("SGD")

func sgdOf(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, sgd)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func dayOf(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func thresholdsOf(t *testing.T, amount, percent string) Thresholds {
	t.Helper()
	th := Thresholds{Amount: sgdOf(t, amount)}
	if _, _, err := th.Percent.SetString(percent); err != nil {
		t.Fatal(err)
	}
	return th
}

// A variance needs an explanation when it is more than both the amount and the percentage of the previous month's
// total, compared exactly; when the previous month is zero the amount alone decides (the founders' decision of rev
// 0.63; rev 0.65). Income is credit, so negative, and the size of a change is what counts.
// req: FR-GL-14
func TestAVarianceNeedsAnExplanationAboveBothThresholds(t *testing.T) {
	recommended := thresholdsOf(t, "1000.00", "10")
	for _, c := range []struct {
		name              string
		current, previous string
		th                Thresholds
		want              bool
	}{
		{"above both", "15000.00", "12000.00", recommended, true},
		{"above the amount, not the percentage", "51000.00", "50000.00", recommended, false},
		{"above the percentage, not the amount", "1500.00", "800.00", recommended, false},
		{"exactly the amount is not more than it", "11000.00", "10000.00", thresholdsOf(t, "1000.00", "5"), false},
		{"a cent more than the amount", "11000.01", "10000.00", thresholdsOf(t, "1000.00", "5"), true},
		{"exactly the percentage is not more than it", "13200.00", "12000.00", recommended, false},
		{"a cent more than the percentage", "13200.01", "12000.00", recommended, true},
		{"a fall counts as much as a rise", "10799.99", "12000.00", recommended, true},
		{"income is credit: a rise in sales", "-15000.00", "-12000.00", recommended, true},
		{"income is credit: a cent short of the percentage", "-13200.00", "-12000.00", recommended, false},
		{"a total that changes sign", "-500.00", "1000.00", recommended, true},
		{"no previous month: the amount decides", "1000.01", "0.00", recommended, true},
		{"no previous month, not above the amount", "1000.00", "0.00", recommended, false},
		{"nothing this month", "0.00", "2000.00", recommended, true},
		{"no change", "5000.00", "5000.00", recommended, false},
		{"a percentage with decimals", "10251.00", "10000.00", thresholdsOf(t, "100.00", "2.5"), true},
		{"exactly a percentage with decimals", "10250.00", "10000.00", thresholdsOf(t, "100.00", "2.5"), false},
		{"zero thresholds: any change", "0.01", "0.00", thresholdsOf(t, "0.00", "0"), true},
		{"zero thresholds: no change", "7.00", "7.00", thresholdsOf(t, "0.00", "0"), false},
	} {
		got, err := NeedsExplanation(sgdOf(t, c.current), sgdOf(t, c.previous), c.th)
		if err != nil || got != c.want {
			t.Errorf("%s: NeedsExplanation(%s, %s) = %v, %v; want %v", c.name, c.current, c.previous, got, err, c.want)
		}
	}
	if _, err := NeedsExplanation(sgdOf(t, "1.00"), money.Zero(money.MustLookup("USD")), recommended); err == nil {
		t.Error("totals in two currencies were compared")
	}
}

// The close's check of the variances: it passes with no earlier month, fails while a variance above the thresholds has
// no current explanation, and passes once each has one (Appendix C.2 #57).
// req: FR-GL-14
// req: C.2 #57
func TestTheVariancesCheck(t *testing.T) {
	th := thresholdsOf(t, "1000.00", "10")
	oct := storage.Period{StartsOn: dayOf(t, "2026-10-01"), EndsOn: dayOf(t, "2026-10-31")}
	if c := fluxCheck(Flux{Currency: sgd, Thresholds: th}); c.Key != "variances" || c.Severity != OK || c.Title != "No earlier month to compare" {
		t.Errorf("no earlier month: %+v", c)
	}
	f := Flux{Previous: &oct, Currency: sgd, Thresholds: th, Variances: []Variance{
		{Needs: true}, {Needs: true, Explained: true}, {Needs: false},
	}}
	c := fluxCheck(f)
	if c.Severity != Failed || c.Detail != "1 income or expense account(s) moved by more than 1000.00 SGD and 10% against October 2026 "+
		"with no explanation of their totals as they are now: explain them, then run the checks again" {
		t.Errorf("an unexplained variance: %+v", c)
	}
	f.Variances[0].Explained = true
	if c := fluxCheck(f); c.Severity != OK || c.Title != "Every variance above the thresholds is explained" {
		t.Errorf("every variance explained: %+v", c)
	}
	if n := (Flux{Variances: []Variance{{Needs: true}, {Needs: true}, {Explained: true}}}).Unexplained(); n != 2 {
		t.Errorf("Unexplained = %d", n)
	}
	var pct apd.Decimal
	pct.SetString("2.5")
	if c := fluxCheck(Flux{Previous: &oct, Currency: sgd, Thresholds: Thresholds{Amount: sgdOf(t, "50.00"), Percent: pct},
		Variances: []Variance{{Needs: true}}}); !strings.HasPrefix(c.Detail, "1 income or expense account(s) moved by more than 50.00 SGD and 2.5% against") {
		t.Errorf("the thresholds in the words: %q", c.Detail)
	}
}
