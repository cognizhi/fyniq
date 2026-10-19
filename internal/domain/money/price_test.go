package money

import "testing"

// A unit price keeps up to six decimals exactly (NUMERIC(20,6)), is shown with at least its currency's minor
// units, and is refused when it would not fit its column.
// req: FR-ITM-01
// req: ENG-11
func TestUnitPricesKeepSixDecimalsExactly(t *testing.T) {
	for _, c := range []struct {
		in   string
		cur  Currency
		want string
		sign int
	}{
		{"12.5", sgd, "12.50", 1},
		{"12.500000", sgd, "12.50", 1}, // as the database returns it
		{"0.1234", sgd, "0.1234", 1},
		{"0.000001", sgd, "0.000001", 1},
		{"100", jpy, "100", 1},
		{"100.000000", jpy, "100", 1},
		{"0.5", jpy, "0.5", 1},
		{"1.2", kwd, "1.200", 1},
		{"0", sgd, "0.00", 0},
		{"-0", sgd, "0.00", 0},
		{"-0.000000", usd, "0.00", 0},
		{"-3.25", usd, "-3.25", -1},
		{"99999999999999.999999", sgd, "99999999999999.999999", 1},
	} {
		p, err := ParseUnitPrice(c.in, c.cur, "unit_price")
		if err != nil {
			t.Errorf("%s %s: %v", c.in, c.cur, err)
			continue
		}
		if p.String() != c.want || p.Sign() != c.sign || p.Currency() != c.cur || p.Decimal().Text('f') != c.want {
			t.Errorf("%s %s: %s (sign %d, %s); want %s (sign %d)", c.in, c.cur, p, p.Sign(), p.Currency(), c.want, c.sign)
		}
	}
	for _, c := range []struct {
		in, code string
		cur      Currency
	}{
		{"1.0000001", CodePrecision, sgd},
		{"100000000000000", CodeRange, sgd}, // 15 integer digits
		{"1e3", CodeNotDecimal, sgd},
		{"NaN", CodeNotDecimal, sgd},
		{"12.50", CodeCurrency, Currency{}},
	} {
		if _, err := ParseUnitPrice(c.in, c.cur, "unit_price"); code(err) != c.code {
			t.Errorf("%q: want %s, got %v", c.in, c.code, err)
		}
	}
	p, _ := ParseUnitPrice("1.5", sgd, "unit_price")
	d := p.Decimal()
	d.Negative = true
	if p.String() != "1.50" {
		t.Errorf("changing the copy changed the price: %s", p)
	}
}
