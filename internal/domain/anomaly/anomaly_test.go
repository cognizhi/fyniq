package anomaly

import (
	"testing"

	"github.com/cockroachdb/apd/v3"
)

func d(s string) *apd.Decimal {
	x, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return x
}

func series(xs ...string) []*apd.Decimal {
	var out []*apd.Decimal
	for _, x := range xs {
		out = append(out, d(x))
	}
	return out
}

// Worked by hand. History 100, 110, 90, 100, 105, 95: n=6, S=600, mean 100, Q = 10000+12100+8100+10000+11025+9025 = 60250, so n*Q - S^2 =
// 361500 - 360000 = 1500 and the sample variance is 1500/30 = 50 (sigma 7.07). Three sigma above the mean is 121.2. The test is
// (6s - 600)^2 * 5 > 9 * 6 * 1500 = 81000, that is (6s - 600)^2 > 16200, 6s - 600 > 127.279, s > 121.2132: 121.21 is not an anomaly, 121.22 is.
// req: FR-KPI-06
func TestThreeStandardDeviationsAboveTheTrendIsAnAnomalyAndJustBelowIsNot(t *testing.T) {
	h := series("100", "110", "90", "100", "105", "95")
	for s, want := range map[string]bool{"121.22": true, "121.21": false, "150": true, "100": false, "90": false, "121.2": false} {
		got, ok, err := Detect(h, d(s))
		if err != nil || ok != want {
			t.Errorf("spend %s: anomaly %v (want %v), %v", s, ok, want, err)
		}
		if ok && got.Mean.Text('f') != "100" {
			t.Errorf("the mean: %s", got.Mean)
		}
	}
	a, ok, _ := Detect(h, d("150"))
	if !ok || a.Excess.Text('f') != "50" {
		t.Errorf("the excess of 150 over the mean: %+v", a)
	}
}

// req: FR-KPI-06
func TestTooLittleHistoryAndIdenticalMonthsRaiseNothingForACent(t *testing.T) {
	// Five months of history are too few.
	if _, ok, _ := Detect(series("100", "100", "100", "100", "100"), d("1000")); ok {
		t.Error("an anomaly from five months")
	}
	// Six identical months have no deviation; a cent more is no anomaly, a tenth more is.
	same := series("100", "100", "100", "100", "100", "100")
	if _, ok, _ := Detect(same, d("100.01")); ok {
		t.Error("a cent above identical months is an anomaly")
	}
	if _, ok, _ := Detect(same, d("110")); !ok {
		t.Error("a tenth above identical months is not an anomaly")
	}
	// The same arithmetic with decimals: exact, no float.
	dec := series("0.10", "0.20", "0.10", "0.20", "0.10", "0.20")
	if _, ok, err := Detect(dec, d("0.50")); err != nil || !ok {
		t.Errorf("exact decimals: %v %v", ok, err)
	}
}
