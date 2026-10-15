package money

import (
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/apd/v3"
)

// RateScale is the most decimal places an exchange rate keeps, and RateIntegerDigits the most digits
// before the point: NUMERIC(20,10) (§11.3, ENG-11).
const (
	RateScale         = 10
	RateIntegerDigits = 10
)

// Rate is an exchange rate: how many units of To one unit of From is worth, and where the rate
// came from (ENG-13, ENG-40). It is always positive.
type Rate struct {
	value  apd.Decimal
	From   Currency
	To     Currency
	Source string
}

// ParseRate reads a rate strictly (ENG-44): a positive plain decimal with at most RateScale
// decimals, from one currency into another, with its source.
func ParseRate(s string, from, to Currency, source string) (Rate, error) {
	d, err := parseDecimal(s, "fx_rate", RateScale, "an exchange rate")
	if err != nil {
		return Rate{}, err
	}
	if digits := int(d.NumDigits()) + int(d.Exponent); digits > RateIntegerDigits {
		return Rate{}, &Error{Code: CodeRange, Field: "fx_rate",
			Message: fmt.Sprintf("the exchange rate %s has %d digits before the decimal point; the most is %d", s, digits, RateIntegerDigits),
			Fix:     "Check the rate and its direction: it is the base currency per unit of the line's currency"}
	}
	if d.Sign() <= 0 {
		return Rate{}, &Error{Code: CodeRate, Field: "fx_rate", Message: fmt.Sprintf("the exchange rate %s is not positive", s),
			Fix: "Enter a rate above zero"}
	}
	if source == "" {
		return Rate{}, &Error{Code: CodeRate, Field: "fx_rate_source", Message: "the exchange rate has no source",
			Fix: "Say where the rate came from, such as the document or the daily feed"}
	}
	if from == to {
		if d.Cmp(apd.New(1, 0)) != 0 {
			return Rate{}, &Error{Code: CodeRate, Field: "fx_rate", Message: fmt.Sprintf("a rate from %s to itself must be 1, not %s", from, s)}
		}
	}
	return Rate{value: d, From: from, To: to, Source: source}, nil
}

// Identity is the rate 1 from a currency to itself, for lines already in the base currency.
func Identity(c Currency) Rate {
	return Rate{value: *apd.New(1, 0), From: c, To: c, Source: "base"}
}

// Decimal returns a copy of the rate's exact value.
func (r Rate) Decimal() *apd.Decimal { return new(apd.Decimal).Set(&r.value) }

// String is the rate as a plain decimal, such as "1.3472".
func (r Rate) String() string { return r.value.Text('f') }

// MarshalJSON writes {"rate":"1.3472","from":"USD","to":"SGD","source":"document"}: the rate as a
// decimal string, never a JSON number (ENG-12).
func (r Rate) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Rate   string `json:"rate"`
		From   string `json:"from"`
		To     string `json:"to"`
		Source string `json:"source"`
	}{r.String(), r.From.Code, r.To.Code, r.Source})
}
