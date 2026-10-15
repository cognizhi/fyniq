package money

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

var (
	sgd = MustLookup("SGD")
	usd = MustLookup("USD")
	jpy = MustLookup("JPY")
	kwd = MustLookup("KWD")
)

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// req: ENG-11
func TestAmountsKeepTheirCurrencysMinorUnits(t *testing.T) {
	for _, c := range []struct {
		cur  Currency
		in   string
		want string
	}{
		{sgd, "1.5", "1.50"}, {sgd, "1234.50", "1234.50"}, {sgd, "0", "0.00"}, {sgd, "-0.75", "-0.75"},
		{jpy, "1234", "1234"}, {kwd, "1.005", "1.005"}, {kwd, "2", "2.000"}, {sgd, "-0.00", "0.00"},
	} {
		m, err := Parse(c.in, c.cur)
		if err != nil || m.String() != c.want {
			t.Errorf("Parse(%q, %s) = %q, %v; want %q", c.in, c.cur, m.String(), err, c.want)
		}
	}
	if got := FromMinor(12345, sgd).String(); got != "123.45" {
		t.Errorf("FromMinor(12345, SGD) = %s", got)
	}
	if got := FromMinor(-1, kwd).String(); got != "-0.001" {
		t.Errorf("FromMinor(-1, KWD) = %s", got)
	}
	if n, ok := MustParse(t, "-1234.56", sgd).Minor(); !ok || n != -123456 {
		t.Errorf("Minor = %d, %v", n, ok)
	}
}

func MustParse(t *testing.T, s string, c Currency) Money {
	t.Helper()
	m, err := Parse(s, c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// req: ENG-44
func TestEnteredValuesAreParsedExactlyAndNeverRounded(t *testing.T) {
	for _, s := range []string{"NaN", "Infinity", "-Infinity", "1e3", "1E-2", "+1", " 1", "1 ", "1,000.00",
		"01", "1.", ".5", "", "-", "0x10", "١٢٣"} {
		if _, err := Parse(s, sgd); code(err) != CodeNotDecimal {
			t.Errorf("Parse(%q): want %s, got %v", s, CodeNotDecimal, err)
		}
	}
	// G4-012: an over-precise SGD amount is refused with the field and the 2-decimal limit.
	_, err := ParseField("1.230001", sgd, "lines[1].amount")
	var e *Error
	if !errors.As(err, &e) || e.Code != CodePrecision || e.Field != "lines[1].amount" || !strings.Contains(e.Message, "allows 2") {
		t.Errorf("1.230001 SGD: got %v; want %s on lines[1].amount naming the 2-decimal limit", err, CodePrecision)
	}
	for cur, s := range map[Currency]string{jpy: "1.5", kwd: "1.0005", usd: "0.001"} {
		if _, err := Parse(s, cur); code(err) != CodePrecision {
			t.Errorf("Parse(%q, %s): want %s, got %v", s, cur, CodePrecision, err)
		}
	}
	if _, err := Parse("99999999999999999.00", sgd); code(err) != CodeRange {
		t.Errorf("17 integer digits: want %s, got %v", CodeRange, err)
	}
	if m, err := Parse("9999999999999999.99", sgd); err != nil || m.String() != "9999999999999999.99" {
		t.Errorf("16 integer digits: %v, %v", m, err)
	}
	if _, err := Parse("1.00", Currency{}); code(err) != CodeCurrency {
		t.Errorf("no currency: %v", err)
	}
	if _, err := Lookup("XYZ"); code(err) != CodeCurrency {
		t.Errorf("unknown currency: %v", err)
	}
	// Rates: positive, at most 10 decimals, with a source.
	for _, c := range []struct{ s, src, want string }{
		{"1.34720000001", "feed", CodePrecision}, {"0", "feed", CodeRate}, {"-1.3", "feed", CodeRate},
		{"1.3472", "", CodeRate}, {"1e2", "feed", CodeNotDecimal},
	} {
		if _, err := ParseRate(c.s, usd, sgd, c.src); code(err) != c.want {
			t.Errorf("ParseRate(%q, %q): want %s, got %v", c.s, c.src, c.want, err)
		}
	}
	if _, err := ParseRate("2", sgd, sgd, "base"); code(err) != CodeRate {
		t.Errorf("a base-currency rate of 2: %v", err)
	}
	if r, err := ParseRate("0.0000000001", jpy, sgd, "feed"); err != nil || r.String() != "0.0000000001" {
		t.Errorf("a 10-decimal rate: %v, %v", r, err)
	}
}

// req: ENG-12
func TestMoneyCrossesJSONAsADecimalString(t *testing.T) {
	b, err := json.Marshal(MustParse(t, "1234.5", sgd))
	if err != nil || string(b) != `{"amount":"1234.50","currency":"SGD"}` {
		t.Fatalf("Marshal = %s, %v", b, err)
	}
	var m Money
	if err := json.Unmarshal([]byte(`{"amount":"-0.75","currency":"USD"}`), &m); err != nil || m.String() != "-0.75" || m.Currency() != usd {
		t.Fatalf("Unmarshal = %v, %v", m, err)
	}
	for _, in := range []string{`{"amount":1234.5,"currency":"SGD"}`, `{"amount":"1.234","currency":"SGD"}`,
		`{"amount":"1e3","currency":"SGD"}`, `{"amount":"1.00","currency":"ZZZ"}`, `"1.00"`} {
		if err := json.Unmarshal([]byte(in), &m); err == nil {
			t.Errorf("Unmarshal(%s) succeeded; want an error", in)
		}
	}
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]Currency{sgd, jpy, kwd, usd}).Draw(t, "currency")
		units := rapid.Int64Range(-999_999_999_999_999, 999_999_999_999_999).Draw(t, "units")
		in := FromMinor(units, cur)
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var out Money
		if err := json.Unmarshal(b, &out); err != nil || !out.Equal(in) {
			t.Fatalf("%s → %s → %v, %v", in, b, out, err)
		}
	})
}

// req: ENG-13
func TestArithmeticAcrossCurrenciesIsRefused(t *testing.T) {
	a, b := MustParse(t, "10.00", sgd), MustParse(t, "10.00", usd)
	if _, err := a.Add(b); code(err) != CodeCurrency {
		t.Errorf("SGD + USD: want %s, got %v", CodeCurrency, err)
	}
	if _, err := a.Sub(b); code(err) != CodeCurrency {
		t.Errorf("SGD − USD: want %s, got %v", CodeCurrency, err)
	}
	if _, err := a.Cmp(b); code(err) != CodeCurrency {
		t.Errorf("compare SGD with USD: want %s, got %v", CodeCurrency, err)
	}
	if _, err := Sum(sgd, a, b); code(err) != CodeCurrency {
		t.Errorf("Sum of SGD and USD: want %s, got %v", CodeCurrency, err)
	}
	// After an explicit conversion (rounding.FX, tested there) the currencies match and Add works.
}

// req: ENG-10
// req: QA-ENG-03
func TestAdditionIsExact(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]Currency{sgd, jpy, kwd}).Draw(t, "currency")
		xs := rapid.SliceOfN(rapid.Int64Range(-1_000_000_000_000, 1_000_000_000_000), 1, 50).Draw(t, "units")
		var want int64
		ms := make([]Money, len(xs))
		for i, x := range xs {
			want += x
			ms[i] = FromMinor(x, cur)
		}
		got, err := Sum(cur, ms...)
		if err != nil {
			t.Fatal(err)
		}
		if n, ok := got.Minor(); !ok || n != want {
			t.Fatalf("sum of %v = %s; want %d minor units", xs, got, want)
		}
		back, err := Parse(got.String(), cur)
		if err != nil || !back.Equal(got) {
			t.Fatalf("%s does not parse back: %v", got, err)
		}
		if neg := got.Neg(); neg.Sign() != -got.Sign() {
			t.Fatalf("Neg(%s) = %s", got, neg)
		}
	})
}

func ExampleMoney_String() {
	m, _ := Parse("-1234.5", MustLookup("SGD"))
	fmt.Println(m)
	// Output: -1234.50
}
