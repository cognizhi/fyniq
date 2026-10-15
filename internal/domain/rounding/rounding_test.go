package rounding

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var (
	sgd = money.MustLookup("SGD")
	usd = money.MustLookup("USD")
	jpy = money.MustLookup("JPY")
)

// tb is what the helpers need from *testing.T and *rapid.T.
type tb interface {
	Helper()
	Fatal(args ...any)
}

func m(t tb, s string, c money.Currency) money.Money {
	t.Helper()
	x, err := money.Parse(s, c)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func rate(t tb, s string, from, to money.Currency) money.Rate {
	t.Helper()
	r, err := money.ParseRate(s, from, to, "test")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func strs(ms []money.Money) string { return fmt.Sprint(ms) }

// req: RND-FX
// req: ENG-13
// req: ENG-14
func TestFXRoundsHalfAwayFromZeroToTheBaseCurrency(t *testing.T) {
	for _, c := range []struct {
		amount string
		from   money.Currency
		rate   string
		want   string
	}{
		{"100.00", usd, "1.3472", "134.72"},
		{"0.05", usd, "2.5", "0.13"},   // 0.125 → 0.13
		{"-0.05", usd, "2.5", "-0.13"}, // −0.125 → −0.13, half away from zero
		{"0.01", usd, "0.5", "0.01"},   // 0.005 → 0.01
		{"-0.01", usd, "0.5", "-0.01"},
		{"0.01", usd, "0.4999999999", "0.00"},
		{"1000", jpy, "0.0091", "9.10"},
		{"1", jpy, "0.0049", "0.00"},
		{"55.55", sgd, "1", "55.55"},
	} {
		got, err := FX(m(t, c.amount, c.from), rate(t, c.rate, c.from, sgd))
		if err != nil || got.String() != c.want || got.Currency() != sgd {
			t.Errorf("FX(%s %s × %s) = %s %s, %v; want %s SGD", c.amount, c.from, c.rate, got, got.Currency(), err, c.want)
		}
	}
	if _, err := FX(m(t, "1.00", sgd), rate(t, "1.35", usd, sgd)); err == nil {
		t.Error("FX with a rate for another currency succeeded")
	}
	// After the explicit conversion, the amounts share a currency and add (ENG-13).
	base, _ := FX(m(t, "10.00", usd), rate(t, "1.35", usd, sgd))
	if total, err := base.Add(m(t, "1.00", sgd)); err != nil || total.String() != "14.50" {
		t.Errorf("converted + SGD = %s, %v", total, err)
	}
}

// req: ALLOC-EVEN
func TestAllocEvenPinnedCases(t *testing.T) {
	// G4-015: S$1.00 over six months, and its mirror image.
	got, err := AllocEven(m(t, "1.00", sgd), 6)
	if err != nil || strs(got) != "[0.17 0.16 0.17 0.17 0.16 0.17]" {
		t.Fatalf("1.00 over 6 = %v, %v", got, err)
	}
	got, _ = AllocEven(m(t, "-1.00", sgd), 6)
	if strs(got) != "[-0.17 -0.16 -0.17 -0.17 -0.16 -0.17]" {
		t.Fatalf("−1.00 over 6 = %v", got)
	}
	got, _ = AllocEven(m(t, "100", jpy), 3)
	if strs(got) != "[33 34 33]" {
		t.Fatalf("¥100 over 3 = %v", got)
	}
	if _, err := AllocEven(m(t, "1.00", sgd), 0); err == nil {
		t.Fatal("splitting into 0 periods succeeded")
	}
}

// req: ALLOC-PRORATA
func TestAllocProRataPinnedCases(t *testing.T) {
	got, err := AllocProRata(m(t, "100.00", sgd), []int64{1, 1, 1})
	if err != nil || strs(got) != "[33.34 33.33 33.33]" {
		t.Fatalf("100.00 by 1:1:1 = %v, %v (ties go to the earlier part)", got, err)
	}
	got, _ = AllocProRata(m(t, "-100.00", sgd), []int64{1, 1, 1})
	if strs(got) != "[-33.34 -33.33 -33.33]" {
		t.Fatalf("−100.00 by 1:1:1 = %v", got)
	}
	got, _ = AllocProRata(m(t, "10.00", sgd), []int64{1, 2, 0, 7})
	if strs(got) != "[1.00 2.00 0.00 7.00]" {
		t.Fatalf("10.00 by 1:2:0:7 = %v", got)
	}
	for _, w := range [][]int64{{}, {0, 0}, {1, -1}} {
		if _, err := AllocProRata(m(t, "1.00", sgd), w); err == nil {
			t.Errorf("weights %v accepted", w)
		}
	}
	if _, err := AllocProRata(m(t, "1.00", sgd), []int64{3, 1, -1}); err == nil || !strings.Contains(err.Error(), "weight 3") {
		t.Errorf("a negative third weight: %v; want the error to name weight 3", err)
	}
}

// minor reads an amount in minor units.
func minor(t *rapid.T, x money.Money) int64 {
	n, ok := x.Minor()
	if !ok {
		t.Fatalf("%s does not fit in int64", x)
	}
	return n
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// req: QA-ENG-03
// req: ALLOC-EVEN
func TestAllocEvenProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		units := rapid.Int64Range(-10_000_000_000, 10_000_000_000).Draw(t, "units")
		n := rapid.IntRange(1, 120).Draw(t, "n")
		parts, err := AllocEven(money.FromMinor(units, sgd), n)
		if err != nil {
			t.Fatal(err)
		}
		var sum, cum int64
		for k, p := range parts {
			v := minor(t, p)
			sum += v
			cum += v
			// each part within one minor unit of amount ÷ n: |v·n − amount| ≤ n
			if abs(v*int64(n)-units) > int64(n) {
				t.Fatalf("part %d of %d over %d = %d, more than one unit from the even share", k+1, units, n, v)
			}
			// each cumulative total within half a unit of amount·k ÷ n: |2·cum·n − 2·amount·k| ≤ n
			if abs(2*cum*int64(n)-2*units*int64(k+1)) > int64(n) {
				t.Fatalf("cumulative %d after %d of %d parts of %d is more than half a unit off", cum, k+1, n, units)
			}
		}
		if sum != units {
			t.Fatalf("parts of %d over %d sum to %d", units, n, sum)
		}
		mirror, _ := AllocEven(money.FromMinor(-units, sgd), n)
		for k := range parts {
			if minor(t, mirror[k]) != -minor(t, parts[k]) {
				t.Fatalf("−%d is not the mirror image of %d at part %d", units, units, k+1)
			}
		}
	})
}

// req: QA-ENG-03
// req: ALLOC-PRORATA
func TestAllocProRataProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		units := rapid.Int64Range(-10_000_000_000, 10_000_000_000).Draw(t, "units")
		weights := rapid.SliceOfN(rapid.Int64Range(0, 1_000_000), 1, 20).Filter(func(w []int64) bool {
			for _, x := range w {
				if x > 0 {
					return true
				}
			}
			return false
		}).Draw(t, "weights")
		parts, err := AllocProRata(money.FromMinor(units, sgd), weights)
		if err != nil {
			t.Fatal(err)
		}
		var total, sum int64
		for _, w := range weights {
			total += w
		}
		for i, p := range parts {
			v := minor(t, p)
			sum += v
			// within one unit of the exact share units·w ÷ total, with the amount's sign
			exactTimesTotal := units * weights[i] // fits: |units| ≤ 1e10, w ≤ 1e6
			if abs(v*total-exactTimesTotal) >= total {
				t.Fatalf("part %d = %d is a unit or more from %d·%d÷%d", i+1, v, units, weights[i], total)
			}
			if weights[i] == 0 && v != 0 {
				t.Fatalf("a zero weight got %d", v)
			}
		}
		if sum != units {
			t.Fatalf("parts sum to %d, not %d", sum, units)
		}
	})
}

// req: QA-ENG-03
// req: RND-FX
func TestFXIsMonotonicAndWithinHalfAUnit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.Int64Range(-1_000_000_000_00, 1_000_000_000_00).Draw(t, "a")
		b := rapid.Int64Range(-1_000_000_000_00, 1_000_000_000_00).Draw(t, "b")
		// a rate with four decimals, as an integer ×10⁴
		r4 := rapid.Int64Range(1, 50_000).Draw(t, "rate")
		r := rate(t, fmt.Sprintf("%d.%04d", r4/10_000, r4%10_000), usd, sgd)
		fa, err := FX(money.FromMinor(a, usd), r)
		if err != nil {
			t.Fatal(err)
		}
		fb, _ := FX(money.FromMinor(b, usd), r)
		// exact: base cents = a·r4 ÷ 10⁴; the rounded value is within half a cent: |2·10⁴·x − 2·a·r4| ≤ 10⁴
		if abs(2*10_000*minor(t, fa)-2*a*r4) > 10_000 {
			t.Fatalf("FX(%d cents × %d/10⁴) = %s, more than half a cent off", a, r4, fa)
		}
		if a <= b && minor(t, fa) > minor(t, fb) {
			t.Fatalf("not monotonic: FX(%d) = %s > FX(%d) = %s", a, fa, b, fb)
		}
	})
}
