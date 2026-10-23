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

func gstOf(t tb, method GSTMethod, lines ...GSTNet) string {
	t.Helper()
	got, err := GSTExcl(lines, method)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, s := range got {
		fmt.Fprintf(&b, "%s %s %s %s; ", s.Code, s.Rate, s.Net, s.GST)
	}
	return b.String()
}

// Worked by hand. Three lines of S$10.06 at 9%: 0.9054 each, so 0.91 a line and S$2.73 by line, against round(30.18 ×
// 0.09) = round(2.7162) = S$2.72 by subtotal: G4-024's supplier who rounded per line, one cent above. Three of S$10.05
// go the other way (0.90 a line, S$2.70, against round(2.7135) = S$2.71). Each code is its own subtotal, in the order
// codes first appear; a credit line rounds half away from zero; JPY rounds to the yen.
// req: RND-GST-EXCL
func TestGSTExclRoundsPerSubtotalOrPerLine(t *testing.T) {
	tx := func(net string) GSTNet { return GSTNet{Code: "TX", Rate: "0.09", Net: m(t, net, sgd)} }
	for _, c := range []struct {
		name   string
		method GSTMethod
		lines  []GSTNet
		want   string
	}{
		{"G4-024 by subtotal", GSTPerSubtotal, []GSTNet{tx("10.06"), tx("10.06"), tx("10.06")}, "TX 0.09 30.18 2.72; "},
		{"G4-024 by line", GSTPerLine, []GSTNet{tx("10.06"), tx("10.06"), tx("10.06")}, "TX 0.09 30.18 2.73; "},
		{"down by line", GSTPerLine, []GSTNet{tx("10.05"), tx("10.05"), tx("10.05")}, "TX 0.09 30.15 2.70; "},
		{"down by subtotal", GSTPerSubtotal, []GSTNet{tx("10.05"), tx("10.05"), tx("10.05")}, "TX 0.09 30.15 2.71; "},
		{"codes apart", GSTPerSubtotal, []GSTNet{tx("100.00"), {Code: "ZP", Rate: "0", Net: m(t, "50.00", sgd)}, tx("0.50"),
			{Code: "BL", Rate: "0.090", Net: m(t, "20.00", sgd)}}, "TX 0.09 100.50 9.05; ZP 0 50.00 0.00; BL 0.090 20.00 1.80; "},
		{"a credit line", GSTPerSubtotal, []GSTNet{tx("-5.50")}, "TX 0.09 -5.50 -0.50; "}, // −0.495 → −0.50
		{"a credit line by line", GSTPerLine, []GSTNet{tx("5.50"), tx("-5.50")}, "TX 0.09 0.00 0.00; "},
		{"the yen", GSTPerSubtotal, []GSTNet{{Code: "TX", Rate: "0.09", Net: m(t, "1005", jpy)}}, "TX 0.09 1005 90; "}, // 90.45
		{"half a cent up", GSTPerSubtotal, []GSTNet{{Code: "TX", Rate: "0.05", Net: m(t, "0.10", sgd)}}, "TX 0.05 0.10 0.01; "},
		{"no lines", GSTPerSubtotal, nil, ""},
	} {
		if got := gstOf(t, c.method, c.lines...); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	for name, c := range map[string]struct {
		lines  []GSTNet
		method GSTMethod
		want   string
	}{
		"no such method":        {[]GSTNet{tx("1.00")}, "invoice", `no method "invoice"`},
		"two currencies":        {[]GSTNet{tx("1.00"), {Code: "TX", Rate: "0.09", Net: m(t, "1.00", usd)}}, GSTPerSubtotal, "line 2 is in USD"},
		"two rates of a code":   {[]GSTNet{tx("1.00"), {Code: "TX", Rate: "0.08", Net: m(t, "1.00", sgd)}}, GSTPerLine, "line 2 gives code TX the rate 0.08"},
		"a percentage":          {[]GSTNet{{Code: "TX", Rate: "9", Net: m(t, "1.00", sgd)}}, GSTPerSubtotal, "line 1's rate"},
		"a negative rate":       {[]GSTNet{{Code: "TX", Rate: "-0.09", Net: m(t, "1.00", sgd)}}, GSTPerSubtotal, "line 1's rate"},
		"a rate of one or more": {[]GSTNet{{Code: "TX", Rate: "1.0", Net: m(t, "1.00", sgd)}}, GSTPerSubtotal, "line 1's rate"},
		"seven decimals":        {[]GSTNet{{Code: "TX", Rate: "0.0900001", Net: m(t, "1.00", sgd)}}, GSTPerSubtotal, "line 1's rate"},
	} {
		if _, err := GSTExcl(c.lines, c.method); err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), GSTExclName) {
			t.Errorf("%s: got %v, want an error with %q", name, err, c.want)
		}
	}
}

// By subtotal each code's GST is within half a cent of its exact net × rate; by line within half a cent a line; a
// document's mirror image has the mirror GST; one line is the same either way; and the nets add up.
// req: QA-ENG-03
// req: RND-GST-EXCL
func TestGSTExclProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rate := rapid.SampledFrom([]string{"0.09", "0.08", "0.07", "0.03", "0", "0.075"}).Draw(t, "rate")
		n := rapid.IntRange(1, 12).Draw(t, "lines")
		var lines, mirror []GSTNet
		var units int64
		for i := range n {
			u := rapid.Int64Range(-100_000_000, 100_000_000).Draw(t, fmt.Sprintf("net %d", i))
			units += u
			lines = append(lines, GSTNet{Code: "TX", Rate: rate, Net: money.FromMinor(u, sgd)})
			mirror = append(mirror, GSTNet{Code: "TX", Rate: rate, Net: money.FromMinor(-u, sgd)})
		}
		exact := new(apd.Decimal)
		r, _, _ := apd.NewFromString(rate)
		if _, err := work.Mul(exact, money.FromMinor(units, sgd).Decimal(), r); err != nil {
			t.Fatal(err)
		}
		for _, method := range []GSTMethod{GSTPerSubtotal, GSTPerLine} {
			got, err := GSTExcl(lines, method)
			if err != nil || len(got) != 1 {
				t.Fatalf("%s: %v %v", method, got, err)
			}
			if minor(t, got[0].Net) != units {
				t.Fatalf("%s: the nets add to %s, want %d cents", method, got[0].Net, units)
			}
			// |GST − exact| ≤ ½ cent (by subtotal) or n × ½ cent (by line)
			var diff apd.Decimal
			if _, err := work.Sub(&diff, got[0].GST.Decimal(), exact); err != nil {
				t.Fatal(err)
			}
			diff.Abs(&diff)
			limit := apd.New(5, -3)
			if method == GSTPerLine {
				limit = apd.New(int64(5*n), -3)
			}
			if diff.Cmp(limit) > 0 {
				t.Fatalf("%s: GST %s is %s from the exact %s, over %s", method, got[0].GST, diff.Text('f'), exact.Text('f'), limit.Text('f'))
			}
			back, _ := GSTExcl(mirror, method)
			if minor(t, back[0].GST) != -minor(t, got[0].GST) {
				t.Fatalf("%s: the mirror's GST %s is not −%s", method, back[0].GST, got[0].GST)
			}
		}
		one, _ := GSTExcl(lines[:1], GSTPerSubtotal)
		same, _ := GSTExcl(lines[:1], GSTPerLine)
		if !one[0].GST.Equal(same[0].GST) {
			t.Fatalf("one line: %s by subtotal, %s by line", one[0].GST, same[0].GST)
		}
	})
}

func qty(t tb, s string) money.Quantity {
	t.Helper()
	q, err := money.ParseQuantity(s, "quantity")
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func price(t tb, s string, c money.Currency) money.UnitPrice {
	t.Helper()
	p, err := money.ParseUnitPrice(s, c, "unit_price")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Worked by hand: 3 × 33.333333 is 99.999999, so 100.00; 2.5 × 1.25 is 3.125, half away from zero 3.13; 0.125 × 10.01
// is 1.25125, so 1.25; −1 × 0.005 is −0.005, so −0.01; ¥33.5 × 3 is ¥100.5, so ¥101. Three parts of one each of the
// first line are 33.33, 33.34 and 33.33: RND-LINE of 1, of 2 less of 1, of 3 less of 2, which add up to 100.00.
// req: RND-LINE
// req: ENG-11
func TestLinePinnedCases(t *testing.T) {
	for _, c := range []struct {
		q, p string
		cur  money.Currency
		want string
	}{{"3", "33.333333", sgd, "100.00"}, {"2.5", "1.25", sgd, "3.13"}, {"0.125", "10.01", sgd, "1.25"}, {"-1", "0.005", sgd, "-0.01"},
		{"3", "33.5", jpy, "101"}, {"0", "12.50", sgd, "0.00"}} {
		got, err := Line(qty(t, c.q), price(t, c.p, c.cur))
		if err != nil || got.String() != c.want || got.Currency() != c.cur {
			t.Errorf("%s × %s %s = %v, %v; want %s", c.q, c.p, c.cur, got, err, c.want)
		}
	}
	p := price(t, "33.333333", sgd)
	var parts []string
	before := money.Quantity{}
	for range 3 {
		part, err := LinePart(before, qty(t, "1"), p)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, part.String())
		before = before.Add(qty(t, "1"))
	}
	if fmt.Sprint(parts) != "[33.33 33.34 33.33]" {
		t.Errorf("three parts of one: %v", parts)
	}
}

// Any line billed in any parts: the parts add up exactly to RND-LINE of the whole line, and each part is within one
// minor unit of its own quantity × price.
// req: RND-LINE
// req: QA-ENG-03
func TestLinePartsProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, jpy}).Draw(t, "currency")
		p := price(t, fmt.Sprintf("%d.%06d", rapid.IntRange(0, 99_999).Draw(t, "price units"), rapid.IntRange(0, 999_999).Draw(t, "price decimals")), cur)
		var parts []money.Quantity
		total := money.Quantity{}
		for i := range rapid.IntRange(1, 8).Draw(t, "parts") {
			q := qty(t, fmt.Sprintf("%d.%03d", rapid.IntRange(0, 1_000).Draw(t, fmt.Sprintf("units %d", i)), rapid.IntRange(0, 999).Draw(t, fmt.Sprintf("decimals %d", i))))
			parts = append(parts, q)
			total = total.Add(q)
		}
		whole, err := Line(total, p)
		if err != nil {
			t.Fatal(err)
		}
		sum := money.Zero(cur)
		before := money.Quantity{}
		one := apd.New(1, -int32(cur.MinorUnits))
		for _, q := range parts {
			part, err := LinePart(before, q, p)
			if err != nil {
				t.Fatal(err)
			}
			sum, _ = sum.Add(part)
			before = before.Add(q)
			var exact, diff apd.Decimal
			if _, err := work.Mul(&exact, q.Decimal(), p.Decimal()); err != nil {
				t.Fatal(err)
			}
			if _, err := work.Sub(&diff, part.Decimal(), &exact); err != nil {
				t.Fatal(err)
			}
			if diff.Abs(&diff).Cmp(one) > 0 {
				t.Fatalf("a part of %s × %s is %s, more than a minor unit from %s", q, p, part, exact.Text('f'))
			}
		}
		if !sum.Equal(whole) {
			t.Fatalf("the parts add up to %s; the whole line is %s", sum, whole)
		}
	})
}

// req: RND-GST-SHARE
// req: FR-AP-20
func TestGSTSharePinnedCases(t *testing.T) {
	for _, c := range []struct {
		gst    string
		cur    money.Currency
		shares []string
		want   string
	}{
		{"4.50", sgd, nil, "4.50"},
		{"4.50", sgd, []string{"100"}, "4.50"},
		{"4.50", sgd, []string{"100.00", "100.0"}, "4.50"},
		{"9.00", sgd, []string{"60"}, "5.40"},
		{"9.00", sgd, []string{"60", "50"}, "2.70"},
		{"7.65", sgd, []string{"50"}, "3.83"},     // 3.825: half away from zero
		{"-7.65", sgd, []string{"50"}, "-3.83"},   // the mirror image
		{"10.00", sgd, []string{"33.33"}, "3.33"}, // 3.333
		{"0.01", sgd, []string{"40"}, "0.00"},     // 0.004
		{"0.01", sgd, []string{"50"}, "0.01"},     // 0.005
		{"123", jpy, []string{"50"}, "62"},        // 61.5
		{"9.00", sgd, []string{"0"}, "0.00"},
		{"9.00", sgd, []string{"60", "0.01"}, "0.00"}, // 0.00054
	} {
		g, err := money.Parse(c.gst, c.cur)
		if err != nil {
			t.Fatal(err)
		}
		got, err := GSTShare(g, c.shares...)
		if err != nil || got.String() != c.want || got.Currency() != c.cur {
			t.Errorf("GSTShare(%s %s, %v) = %v, %v; want %s", c.gst, c.cur, c.shares, got, err, c.want)
		}
	}
	g, _ := money.Parse("9.00", sgd)
	for _, bad := range []string{"", "101", "100.01", "-1", "60%", "0.6.0", "1e2", "33.333", " 60"} {
		if _, err := GSTShare(g, bad); err == nil || IsShare(bad) {
			t.Errorf("share %q was taken", bad)
		}
	}
}

// req: RND-GST-SHARE
// req: QA-ENG-03
// The input tax claimed is never more than the GST nor less than zero, is within half a minor unit of the exact share,
// claims the GST whole at 100%, and never falls as a share grows.
func TestGSTShareProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, jpy}).Draw(t, "currency")
		units := rapid.Int64Range(0, 10_000_000).Draw(t, "gst minor units")
		g := money.Zero(cur)
		if cur.MinorUnits == 2 {
			g, _ = money.Parse(fmt.Sprintf("%d.%02d", units/100, units%100), cur)
		} else {
			g, _ = money.Parse(fmt.Sprint(units), cur)
		}
		share := func(label string) string {
			n := rapid.IntRange(0, 10_000).Draw(t, label)
			return fmt.Sprintf("%d.%02d", n/100, n%100)
		}
		a, b := share("business"), share("reimbursed")
		got, err := GSTShare(g, a, b)
		if err != nil {
			t.Fatal(err)
		}
		if got.Sign() < 0 || mustCmp(t, got, g) > 0 {
			t.Fatalf("claimed %s of %s", got, g)
		}
		var exact, diff apd.Decimal
		da, _, _ := apd.NewFromString(a)
		db, _, _ := apd.NewFromString(b)
		_, _ = work.Mul(&exact, g.Decimal(), da)
		_, _ = work.Mul(&exact, &exact, db)
		_, _ = work.Quo(&exact, &exact, apd.New(10_000, 0))
		_, _ = work.Sub(&diff, got.Decimal(), &exact)
		half := apd.New(5, -int32(cur.MinorUnits)-1)
		if diff.Abs(&diff).Cmp(half) > 0 {
			t.Fatalf("claimed %s; exact %s", got, &exact)
		}
		if whole, _ := GSTShare(g, "100", "100"); !whole.Equal(g) {
			t.Fatalf("100%% of %s claimed %s", g, whole)
		}
		more, err := GSTShare(g, "100", b)
		if err != nil || mustCmp(t, more, got) < 0 {
			t.Fatalf("a larger share claimed less: %s < %s", more, got)
		}
	})
}

func mustCmp(t interface{ Fatal(...any) }, a, b money.Money) int {
	c, err := a.Cmp(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// req: RND-WHT
// req: FR-AP-19
func TestWHTPinnedCases(t *testing.T) {
	for _, c := range []struct {
		gross, rate string
		cur         money.Currency
		tax, part   string
	}{
		{"5000.00", "15", sgd, "750.00", "750.00"},
		{"1234.56", "17", sgd, "209.78", "209.88"}, // the tax on 1,234 (rounded down); the part of 1,234.56
		{"1346.10", "17", sgd, "228.82", "228.84"},
		{"999.99", "10", sgd, "99.90", "100.00"},
		{"0.99", "17", sgd, "0.00", "0.17"},
		{"12345", "15", jpy, "1852", "1852"}, // 1,851.75
		{"100.00", "12.5", sgd, "12.50", "12.50"},
	} {
		g, err := money.Parse(c.gross, c.cur)
		if err != nil {
			t.Fatal(err)
		}
		tax, err := WHT(g, c.rate)
		if err != nil || tax.String() != c.tax {
			t.Errorf("WHT(%s, %s%%) = %v, %v; want %s", c.gross, c.rate, tax, err, c.tax)
		}
		part, err := Withheld(g, c.rate)
		if err != nil || part.String() != c.part {
			t.Errorf("Withheld(%s, %s%%) = %v, %v; want %s", c.gross, c.rate, part, err, c.part)
		}
	}
	g, _ := money.Parse("100.00", sgd)
	for _, bad := range []string{"", "0", "0.00", "100.01", "-5", "17%", "1e1"} {
		if _, err := WHT(g, bad); err == nil || IsWHTRate(bad) {
			t.Errorf("WHT took the rate %q", bad)
		}
		if _, err := Withheld(g, bad); err == nil {
			t.Errorf("Withheld took the rate %q", bad)
		}
	}
}

// req: RND-WHT
// req: QA-ENG-03
// The tax is never more than the rate of the gross nor less than the rate of its whole units, and never falls as the
// gross grows.
func TestWHTProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cents := rapid.Int64Range(0, 100_000_000).Draw(t, "gross cents")
		g, _ := money.Parse(fmt.Sprintf("%d.%02d", cents/100, cents%100), sgd)
		n := rapid.IntRange(1, 10_000).Draw(t, "rate hundredths")
		rate := fmt.Sprintf("%d.%02d", n/100, n%100)
		tax, err := WHT(g, rate)
		if err != nil {
			t.Fatal(err)
		}
		part, err := Withheld(g, rate)
		if err != nil {
			t.Fatal(err)
		}
		if tax.Sign() < 0 || mustCmp(t, tax, part) > 0 {
			t.Fatalf("tax %s above the part %s of %s at %s%%", tax, part, g, rate)
		}
		more, _ := money.Parse(fmt.Sprintf("%d.%02d", (cents+100)/100, (cents+100)%100), sgd)
		if next, _ := WHT(more, rate); mustCmp(t, next, tax) < 0 {
			t.Fatalf("a larger gross withheld less: %s < %s", next, tax)
		}
	})
}

// req: RND-DISCOUNT
// req: FR-AP-13
func TestDiscountPinnedCases(t *testing.T) {
	for _, c := range []struct {
		total, pct string
		cur        money.Currency
		want       string
	}{
		{"1090.00", "2", sgd, "21.80"},
		{"1234.56", "2.5", sgd, "30.86"}, // 30.864
		{"99.99", "1.5", sgd, "1.50"},    // 1.49985
		{"12345", "2", jpy, "247"},       // 246.9
		{"0.01", "0.01", sgd, "0.00"},
	} {
		got, err := Discount(amount(t, c.total, c.cur), c.pct)
		if err != nil || got.String() != c.want {
			t.Errorf("Discount(%s, %s%%) = %v, %v; want %s", c.total, c.pct, got, err, c.want)
		}
	}
	for _, bad := range []string{"0", "100", "100.00", "101", "-2", "2%", ""} {
		if _, err := Discount(amount(t, "100.00", sgd), bad); err == nil {
			t.Errorf("Discount took %q", bad)
		}
	}
}

func amount(t *testing.T, s string, cur money.Currency) money.Money {
	t.Helper()
	m, err := money.Parse(s, cur)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
