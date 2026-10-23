package money

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"
)

// QuantityScale is the most decimal places a quantity keeps, and QuantityIntegerDigits the most digits before the
// point: NUMERIC(20,6), §11.3's column for quantities and unit prices (ENG-11).
const (
	QuantityScale         = 6
	QuantityIntegerDigits = 14
)

// Quantity is an exact number of units, with up to QuantityScale decimals: 3 laptops, 2.5 hours, 0.125 tonnes. It
// carries no currency. The zero value is zero.
type Quantity struct{ value apd.Decimal }

// ParseQuantity reads a quantity strictly (ENG-44): a plain decimal with at most QuantityScale decimals and
// QuantityIntegerDigits digits before the point. It never rounds, and reads a quantity as the database returns it
// ("3.000000").
func ParseQuantity(s, field string) (Quantity, error) {
	d, err := parseDecimal(s, field, QuantityScale, "a quantity")
	if err != nil {
		return Quantity{}, err
	}
	if digits := int(d.NumDigits()) + int(d.Exponent); digits > QuantityIntegerDigits {
		return Quantity{}, &Error{Code: CodeRange, Field: field,
			Message: fmt.Sprintf("the quantity %s has %d digits before the decimal point; the most is %d", s, digits, QuantityIntegerDigits),
			Fix:     "Check the quantity: no order is that large"}
	}
	return quantityOf(&d), nil
}

// quantityOf keeps d's significant decimals only: 3.000000 is 3, 2.50 is 2.5.
func quantityOf(d *apd.Decimal) Quantity {
	var q Quantity
	if _, _, err := ctx.Reduce(&q.value, d); err != nil {
		panic(fmt.Sprintf("money: reduce %s: %v", d, err))
	}
	if q.value.IsZero() {
		q.value = apd.Decimal{} // 0, with no exponent or sign
	}
	return q
}

// Sign is -1, 0 or 1 as the quantity is below, at or above zero.
func (q Quantity) Sign() int { return q.value.Sign() }

// IsZero says whether the quantity is zero.
func (q Quantity) IsZero() bool { return q.value.IsZero() }

// Decimal returns a copy of the quantity's exact value.
func (q Quantity) Decimal() *apd.Decimal { return new(apd.Decimal).Set(&q.value) }

// Add is q + r, exactly.
func (q Quantity) Add(r Quantity) Quantity {
	var out apd.Decimal
	if _, err := ctx.Add(&out, &q.value, &r.value); err != nil {
		panic(fmt.Sprintf("money: %s + %s: %v", q, r, err))
	}
	return quantityOf(&out)
}

// Sub is q − r, exactly.
func (q Quantity) Sub(r Quantity) Quantity {
	var out apd.Decimal
	if _, err := ctx.Sub(&out, &q.value, &r.value); err != nil {
		panic(fmt.Sprintf("money: %s - %s: %v", q, r, err))
	}
	return quantityOf(&out)
}

// Cmp is -1, 0 or 1 as q is below, equal to or above r.
func (q Quantity) Cmp(r Quantity) int { return q.value.Cmp(&r.value) }

// String is the quantity as plain decimal text with its significant decimals only, such as "3" or "2.5".
func (q Quantity) String() string { return q.value.Text('f') }
