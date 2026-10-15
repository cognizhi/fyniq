// Package money is Fyniq's money type: an exact decimal (apd v3) with an ISO 4217 currency
// (PRD-001 §10.3). There is no floating-point path in or out (ENG-10); arithmetic across currencies
// needs an explicit conversion with a rate and its source (ENG-13); amounts are parsed strictly
// from decimal strings and never rounded silently (ENG-44); rounding happens only in the named
// policies of package rounding (§10.4).
package money

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/cockroachdb/apd/v3"
)

// Currency is an ISO 4217 currency and its minor units (SGD 2, JPY 0, KWD 3).
type Currency struct {
	Code       string
	MinorUnits int
}

func (c Currency) String() string { return c.Code }

// The currencies Fyniq knows, the same list as the database's currency table
// (db/migrations/000002), from the ISO 4217 list maintained by SIX.
var currencies = func() map[string]Currency {
	out := map[string]Currency{}
	for code, units := range map[string]int{
		"AUD": 2, "BHD": 3, "CAD": 2, "CHF": 2, "CNY": 2, "EUR": 2, "GBP": 2, "HKD": 2, "IDR": 2,
		"INR": 2, "JPY": 0, "KRW": 0, "KWD": 3, "MYR": 2, "NZD": 2, "PHP": 2, "SGD": 2, "THB": 2,
		"TWD": 2, "USD": 2, "VND": 0,
	} {
		out[code] = Currency{Code: code, MinorUnits: units}
	}
	return out
}()

// Lookup returns the currency for an ISO 4217 code.
func Lookup(code string) (Currency, error) {
	c, ok := currencies[code]
	if !ok {
		return Currency{}, &Error{Code: CodeCurrency, Field: "currency",
			Message: fmt.Sprintf("%q is not a currency Fyniq knows", code), Fix: "Use a three-letter ISO 4217 code such as SGD or USD"}
	}
	return c, nil
}

// MustLookup is Lookup for codes known at compile time.
func MustLookup(code string) Currency {
	c, err := Lookup(code)
	if err != nil {
		panic(err)
	}
	return c
}

// Currencies returns every known currency code and its minor units.
func Currencies() map[string]int {
	out := make(map[string]int, len(currencies))
	for code, c := range currencies {
		out[code] = c.MinorUnits
	}
	return out
}

// MaxIntegerDigits is the most digits before the decimal point a stored amount can have:
// NUMERIC(20,4) holds 16 (§11.3).
const MaxIntegerDigits = 16

// Error is a money failure with a stable code, the field, what is wrong and how to fix it (ENG-23).
type Error struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

func (e *Error) Error() string {
	if e.Fix != "" {
		return fmt.Sprintf("%s: %s: %s (%s)", e.Code, e.Field, e.Message, e.Fix)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Field, e.Message)
}

// The stable error codes of package money.
const (
	CodeNotDecimal = "MONEY-01" // not a plain decimal: NaN, Infinity, exponent, spaces, a plus sign…
	CodePrecision  = "MONEY-02" // more decimals than the currency's minor units (or the rate's scale)
	CodeRange      = "MONEY-03" // more digits than the column holds
	CodeCurrency   = "MONEY-04" // an unknown currency, or two currencies mixed without a conversion
	CodeRate       = "MONEY-05" // a rate that is not positive or whose currencies do not fit
)

// plainDecimal is the only accepted text form: an optional minus, digits without leading zeros,
// and optional decimals. No plus sign, exponent, spaces, separators, NaN or Infinity (ENG-44).
var plainDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// ctx is wide enough that adding and subtracting stored amounts is exact. It is never used to round
// to a currency: that is the job of a named policy.
var ctx = apd.BaseContext.WithPrecision(60)

// parseDecimal parses s exactly, refusing anything but a plain decimal with at most maxScale decimals
// and MaxIntegerDigits integer digits.
func parseDecimal(s, field string, maxScale int, what string) (apd.Decimal, error) {
	var d apd.Decimal
	if !plainDecimal.MatchString(s) {
		return d, &Error{Code: CodeNotDecimal, Field: field, Message: fmt.Sprintf("%q is not a plain decimal number", s),
			Fix: "Write digits with an optional minus sign and decimal point, such as 1234.50 or -0.75"}
	}
	if _, _, err := d.SetString(s); err != nil {
		return d, &Error{Code: CodeNotDecimal, Field: field, Message: fmt.Sprintf("%q is not a decimal number", s)}
	}
	if scale := -int(d.Exponent); scale > maxScale {
		return d, &Error{Code: CodePrecision, Field: field,
			Message: fmt.Sprintf("%s has %d decimal places, but %s allows %d", s, scale, what, maxScale),
			Fix:     fmt.Sprintf("Enter the amount with at most %d decimal places; Fyniq never rounds what you enter", maxScale)}
	}
	if digits := int(d.NumDigits()) + int(d.Exponent); digits > MaxIntegerDigits {
		return d, &Error{Code: CodeRange, Field: field,
			Message: fmt.Sprintf("%s has %d digits before the decimal point; the most is %d", s, digits, MaxIntegerDigits),
			Fix:     "Check the amount: no ledger amount is that large"}
	}
	return d, nil
}

// Money is an exact amount in one currency, always at that currency's minor units. The zero value
// is not valid; build one with Parse, FromMinor or Zero.
type Money struct {
	amount apd.Decimal
	cur    Currency
}

// Parse reads an entered amount (ENG-44): a plain decimal string with at most the currency's minor
// units of decimals. It never rounds.
func Parse(s string, cur Currency) (Money, error) {
	return ParseField(s, cur, "amount")
}

// ParseField is Parse with the name of the field the amount came from, for the error.
func ParseField(s string, cur Currency, field string) (Money, error) {
	if cur.Code == "" {
		return Money{}, &Error{Code: CodeCurrency, Field: field, Message: "no currency was given for the amount"}
	}
	d, err := parseDecimal(s, field, cur.MinorUnits, cur.Code)
	if err != nil {
		return Money{}, err
	}
	return fromDecimal(&d, cur), nil
}

// StoredScale is the scale of the database's amount columns, NUMERIC(20,4) (§11.3).
const StoredScale = 4

// ParseStored reads an amount as the database returns it, at NUMERIC(20,4)'s scale ("100.5000"
// for SGD 100.50). Zeros beyond the currency's minor units are dropped; any other digit there is
// refused, because the database never stores one (INV-05).
func ParseStored(s string, cur Currency) (Money, error) {
	d, err := parseDecimal(s, "amount", StoredScale, "a stored amount")
	if err != nil {
		return Money{}, err
	}
	return FromRounded(&d, cur)
}

// FromMinor builds an amount from integer minor units (cents for SGD).
func FromMinor(units int64, cur Currency) Money {
	var d apd.Decimal
	d.SetFinite(units, -int32(cur.MinorUnits))
	return fromDecimal(&d, cur)
}

// Zero is zero in cur.
func Zero(cur Currency) Money { return FromMinor(0, cur) }

// fromDecimal stores d at exactly cur's minor units. d must already have no more decimals.
func fromDecimal(d *apd.Decimal, cur Currency) Money {
	m := Money{cur: cur}
	if _, err := ctx.Quantize(&m.amount, d, -int32(cur.MinorUnits)); err != nil {
		panic(fmt.Sprintf("money: quantize %s to %s: %v", d, cur, err))
	}
	if m.amount.IsZero() {
		m.amount.Negative = false // no negative zero
	}
	return m
}

// FromRounded stores a value a named rounding policy has already rounded to cur's minor units.
// It refuses anything with more decimals, so it cannot be used to round.
func FromRounded(d *apd.Decimal, cur Currency) (Money, error) {
	if -int(d.Exponent) > cur.MinorUnits {
		var trimmed apd.Decimal
		if _, _, err := ctx.Reduce(&trimmed, d); err != nil || -int(trimmed.Exponent) > cur.MinorUnits {
			return Money{}, &Error{Code: CodePrecision, Field: "amount",
				Message: fmt.Sprintf("%s is not rounded to %s's %d decimal places", d, cur, cur.MinorUnits),
				Fix:     "Round it with a named rounding policy first"}
		}
		return fromDecimal(&trimmed, cur), nil
	}
	return fromDecimal(d, cur), nil
}

// Currency is the amount's currency.
func (m Money) Currency() Currency { return m.cur }

// Decimal returns a copy of the exact amount.
func (m Money) Decimal() *apd.Decimal { return new(apd.Decimal).Set(&m.amount) }

// String is the amount as a plain decimal string at the currency's minor units, such as "1234.50"
// or "-0.75" (ENG-12). It never uses exponent notation.
func (m Money) String() string { return m.amount.Text('f') }

// Minor returns the amount in integer minor units, and whether it fits in an int64.
func (m Money) Minor() (int64, bool) {
	if !m.amount.Coeff.IsInt64() {
		return 0, false
	}
	n := m.amount.Coeff.Int64() // the amount is stored at exactly the minor units
	if m.amount.Negative {
		n = -n
	}
	return n, true
}

// Sign is -1, 0 or +1.
func (m Money) Sign() int { return m.amount.Sign() }

// IsZero reports whether the amount is zero.
func (m Money) IsZero() bool { return m.amount.IsZero() }

// Neg is the amount with the opposite sign.
func (m Money) Neg() Money {
	out := Money{cur: m.cur}
	out.amount.Neg(&m.amount)
	if out.amount.IsZero() {
		out.amount.Negative = false
	}
	return out
}

func (m Money) same(o Money, op string) error {
	if m.cur != o.cur {
		return &Error{Code: CodeCurrency, Field: "currency",
			Message: fmt.Sprintf("cannot %s %s and %s", op, m.cur, o.cur),
			Fix:     "Convert one amount first with a rate and its source (money.Convert)"}
	}
	return nil
}

// Add returns m + o. Amounts in different currencies are refused (ENG-13).
func (m Money) Add(o Money) (Money, error) {
	if err := m.same(o, "add"); err != nil {
		return Money{}, err
	}
	out := Money{cur: m.cur}
	if _, err := ctx.Add(&out.amount, &m.amount, &o.amount); err != nil {
		return Money{}, &Error{Code: CodeRange, Field: "amount", Message: err.Error()}
	}
	return fromDecimal(&out.amount, m.cur), nil
}

// Sub returns m − o. Amounts in different currencies are refused (ENG-13).
func (m Money) Sub(o Money) (Money, error) { return m.Add(o.Neg()) }

// Cmp compares two amounts of the same currency: -1, 0 or +1.
func (m Money) Cmp(o Money) (int, error) {
	if err := m.same(o, "compare"); err != nil {
		return 0, err
	}
	return m.amount.Cmp(&o.amount), nil
}

// Equal reports whether two amounts have the same currency and value.
func (m Money) Equal(o Money) bool { return m.cur == o.cur && m.amount.Cmp(&o.amount) == 0 }

// Sum adds amounts of one currency; an empty list is zero in cur.
func Sum(cur Currency, amounts ...Money) (Money, error) {
	total := Zero(cur)
	for _, a := range amounts {
		var err error
		if total, err = total.Add(a); err != nil {
			return Money{}, err
		}
	}
	return total, nil
}

// wire is Money on the wire: a decimal string and its currency, never a JSON number (ENG-12).
type wire struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON writes {"amount":"1234.50","currency":"SGD"}.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(wire{Amount: m.String(), Currency: m.cur.Code})
}

// UnmarshalJSON reads {"amount":"1234.50","currency":"SGD"} with the rules of Parse; a JSON number
// for the amount is refused.
func (m *Money) UnmarshalJSON(b []byte) error {
	var raw struct {
		Amount   json.RawMessage `json:"amount"`
		Currency string          `json:"currency"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return &Error{Code: CodeNotDecimal, Field: "amount", Message: "money must be an object with amount and currency"}
	}
	var s string
	if err := json.Unmarshal(raw.Amount, &s); err != nil {
		return &Error{Code: CodeNotDecimal, Field: "amount", Message: "the amount must be a decimal string such as \"1234.50\", not a JSON number",
			Fix: "Quote the amount"}
	}
	cur, err := Lookup(raw.Currency)
	if err != nil {
		return err
	}
	parsed, err := Parse(s, cur)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
