package rounding

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"
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

// RND-PERCENT: the change as a percentage of the earlier total, to one decimal, half away from zero, with the change's
// sign; the earlier total's sign does not matter, and zero has no percentage.
// req: RND-PERCENT
func TestPercentPinnedCases(t *testing.T) {
	for _, c := range []struct{ part, whole, want string }{
		{"2000.00", "5000.00", "40.0"},
		{"-2000.00", "5000.00", "-40.0"},
		{"2000.00", "-5000.00", "40.0"},
		{"-2000.00", "-5000.00", "-40.0"},
		{"0.01", "0.03", "33.3"},
		{"0.02", "0.03", "66.7"},
		{"-0.02", "0.03", "-66.7"},
		{"1.00", "2000.00", "0.1"},   // 0.05: half away from zero
		{"-1.00", "2000.00", "-0.1"}, // and its mirror
		{"0.99", "2000.00", "0.0"},   // 0.0495
		{"-0.01", "1000.00", "0.0"},  // -0.001: never -0.0
		{"49.00", "400.00", "12.3"},  // 12.25
		{"-49.00", "400.00", "-12.3"},
		{"5000.00", "5000.00", "100.0"},
		{"0.00", "5000.00", "0.0"},
		{"9999999999999999.99", "0.01", "99999999999999999900.0"},
	} {
		got, err := Percent(m(t, c.part, sgd), m(t, c.whole, sgd))
		if err != nil || got != c.want {
			t.Errorf("Percent(%s, %s) = %q, %v; want %q", c.part, c.whole, got, err, c.want)
		}
	}
	if _, err := Percent(m(t, "1.00", sgd), m(t, "0.00", sgd)); err == nil {
		t.Error("a percentage of zero")
	}
	if _, err := Percent(m(t, "1.00", sgd), m(t, "1.00", usd)); err == nil {
		t.Error("a percentage of another currency")
	}
	if got, err := Percent(m(t, "1", jpy), m(t, "3", jpy)); err != nil || got != "33.3" {
		t.Errorf("Percent of yen = %q, %v", got, err)
	}
}

// req: RND-RATE
// The cases are worked by hand (60-digit decimal arithmetic, then half away from zero to 10 places), never by running
// the code under test.
func TestRateTurnsEveryQuoteIntoBasePerUnit(t *testing.T) {
	cases := []struct {
		quote string
		form  money.QuoteForm
		from  money.Currency
		want  string
	}{
		{"1.3472", money.QuoteBasePerUnit, usd, "1.3472"},
		{"90.4512", money.QuoteBasePer100, jpy, "0.904512"},
		{"123.4567890123", money.QuoteBasePer100, jpy, "1.2345678901"},
		{"0.0000000050", money.QuoteBasePer100, jpy, "0.0000000001"}, // a tie, away from zero
		{"0.7432", money.QuoteForeignPerBase, usd, "1.345532831"},
		{"1.5", money.QuoteForeignPerBase, usd, "0.6666666667"},
		{"12000", money.QuoteForeignPerBase, usd, "0.0000833333"},
		{"2048", money.QuoteForeignPerBase, usd, "0.0004882813"}, // 1 ÷ 2048 = 0.00048828125 exactly: a tie
	}
	for _, c := range cases {
		q, err := money.ParseQuote(c.quote, "quote")
		if err != nil {
			t.Fatal(err)
		}
		r, err := Rate(q, c.form, c.from, sgd, "test")
		if err != nil {
			t.Fatalf("%s %s: %v", c.quote, c.form, err)
		}
		if r.String() != c.want || r.From != c.from || r.To != sgd || r.Source != "test" {
			t.Errorf("%s %s: want %s %s→SGD, got %s %s→%s from %q", c.quote, c.form, c.want, c.from, r, r.From, r.To, r.Source)
		}
	}
	q, _ := money.ParseQuote("0.0000000049", "quote")
	if _, err := Rate(q, money.QuoteBasePer100, jpy, sgd, "test"); err == nil || !strings.Contains(err.Error(), "smallest rate") {
		t.Errorf("a quote whose rate rounds to zero: want refused, got %v", err)
	}
	q, _ = money.ParseQuote("1.3", "quote")
	if _, err := Rate(q, "per-pound", usd, sgd, "test"); err == nil {
		t.Error("an unknown quote form: want refused")
	}
	for _, s := range []string{"0", "-1.3", "1.00000000001", "12345678901", "1e3"} {
		if _, err := money.ParseQuote(s, "quote"); err == nil {
			t.Errorf("ParseQuote(%q): want refused", s)
		}
	}
}

// req: RND-RATE
// Properties: the stored rate is within half a unit of the 10th place of the exact one, so a quote per 100 units and
// its inverse agree with the exact rate to open question 30's bound. A rate is refused only when it rounds to zero, or
// when it has more than 10 digits before the point, which NUMERIC(20,10) cannot hold.
func TestRateIsWithinHalfAUnitOfTheTenthPlace(t *testing.T) {
	half, _, _ := apd.NewFromString("0.00000000005")
	tooLarge := apd.New(1, money.RateIntegerDigits)
	rapid.Check(t, func(t *rapid.T) {
		units := rapid.Int64Range(1, 99_999_999_999_999).Draw(t, "units") // a quote of up to 10 places
		q := apd.New(units, -10)
		form := rapid.SampledFrom([]money.QuoteForm{money.QuoteBasePer100, money.QuoteForeignPerBase}).Draw(t, "form")
		r, err := Rate(q, form, usd, sgd, "test")
		var exact apd.Decimal
		if form == money.QuoteBasePer100 {
			work.Quo(&exact, q, apd.New(100, 0))
		} else {
			work.Quo(&exact, apd.New(1, 0), q)
		}
		if err != nil {
			if exact.Cmp(half) >= 0 && exact.Cmp(tooLarge) < 0 {
				t.Fatalf("%s %s: refused a rate of %s: %v", q.Text('f'), form, exact.Text('f'), err)
			}
			return
		}
		var diff apd.Decimal
		work.Sub(&diff, r.Decimal(), &exact)
		diff.Abs(&diff)
		if diff.Cmp(half) > 0 {
			t.Fatalf("%s %s: stored %s, exact %s, off by %s", q.Text('f'), form, r, exact.Text('f'), diff.Text('f'))
		}
	})
}
