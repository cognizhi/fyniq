package money

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"
)

// UnitPriceScale is the most decimal places a unit price keeps, and UnitPriceIntegerDigits the most digits
// before the point: NUMERIC(20,6), §11.3's column for quantities and unit prices. A line's amount reaches its
// currency's minor units through a named rounding policy, never by storing the price rounded (ENG-11).
const (
	UnitPriceScale         = 6
	UnitPriceIntegerDigits = 14
)

// UnitPrice is an exact price per unit in one currency, with up to UnitPriceScale decimals. The zero value is
// not valid; build one with ParseUnitPrice.
type UnitPrice struct {
	value apd.Decimal
	cur   Currency
}

// ParseUnitPrice reads a unit price strictly (ENG-44): a plain decimal with at most UnitPriceScale decimals
// and UnitPriceIntegerDigits digits before the point, in cur. It never rounds. It also reads a price as the
// database returns it ("12.500000").
func ParseUnitPrice(s string, cur Currency, field string) (UnitPrice, error) {
	if cur.Code == "" {
		return UnitPrice{}, &Error{Code: CodeCurrency, Field: field, Message: "no currency was given for the unit price"}
	}
	d, err := parseDecimal(s, field, UnitPriceScale, "a unit price")
	if err != nil {
		return UnitPrice{}, err
	}
	if digits := int(d.NumDigits()) + int(d.Exponent); digits > UnitPriceIntegerDigits {
		return UnitPrice{}, &Error{Code: CodeRange, Field: field,
			Message: fmt.Sprintf("the unit price %s has %d digits before the decimal point; the most is %d", s, digits, UnitPriceIntegerDigits),
			Fix:     "Check the price: no unit price is that large"}
	}
	return unitPriceOf(&d, cur), nil
}

// unitPriceOf keeps d's significant decimals, and at least cur's minor units: 12.5 SGD is 12.50, 0.1234
// SGD stays 0.1234 and 100 JPY stays 100.
func unitPriceOf(d *apd.Decimal, cur Currency) UnitPrice {
	p := UnitPrice{cur: cur}
	if _, _, err := ctx.Reduce(&p.value, d); err != nil {
		panic(fmt.Sprintf("money: reduce %s: %v", d, err))
	}
	// Zero's exponent is the currency's minor units as apd holds them: -2 for SGD.
	exp := min(p.value.Exponent, Zero(cur).amount.Exponent)
	if _, err := ctx.Quantize(&p.value, &p.value, exp); err != nil {
		panic(fmt.Sprintf("money: quantize %s to %d places: %v", d, -exp, err))
	}
	if p.value.IsZero() {
		p.value.Negative = false // no negative zero
	}
	return p
}

// Currency is the price's currency.
func (p UnitPrice) Currency() Currency { return p.cur }

// Sign is -1, 0 or 1 as the price is below, at or above zero.
func (p UnitPrice) Sign() int { return p.value.Sign() }

// Decimal returns a copy of the price's exact value.
func (p UnitPrice) Decimal() *apd.Decimal { return new(apd.Decimal).Set(&p.value) }

// String is the price as plain decimal text, with its significant decimals and at least its currency's minor
// units, such as "12.50" or "0.1234".
func (p UnitPrice) String() string { return p.value.Text('f') }
