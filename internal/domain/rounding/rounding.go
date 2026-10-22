// Package rounding holds Fyniq's named rounding and allocation policies (PRD-001 §10.4). Every
// rounding in the system goes through one of these functions, called by its name, and "half-up"
// means half away from zero. The policies the P0 posting source needs are here; the statutory ones
// (RND-GST-*, RND-CPF*, RND-SDL) and RND-SHARE and RND-MILEAGE arrive with their P1 sources.
package rounding

import (
	"fmt"
	"sort"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The policy names, recorded with the figures they produce.
const (
	FXName           = "RND-FX"
	AllocEvenName    = "ALLOC-EVEN"
	AllocProRataName = "ALLOC-PRORATA"
	PercentName      = "RND-PERCENT"
	RateName         = "RND-RATE"
)

// work is the exact context the policies compute in before their one rounding step.
var work = apd.BaseContext.WithPrecision(60)

// halfAwayFromZero rounds d to places decimals, half away from zero. apd's RoundHalfUp rounds the
// magnitude, so it is half away from zero for negative values too.
func halfAwayFromZero(d *apd.Decimal, places int) *apd.Decimal {
	c := work.WithPrecision(60)
	c.Rounding = apd.RoundHalfUp
	out := new(apd.Decimal)
	if _, err := c.Quantize(out, d, -int32(places)); err != nil {
		panic(fmt.Sprintf("rounding: quantize %s to %d places: %v", d, places, err))
	}
	return out
}

// FX is RND-FX: base amount = round(foreign amount × rate, base minor units), half away from zero.
// It is the only way to turn an amount into another currency (ENG-13), and the result is the base
// amount stored once at posting (ENG-14).
func FX(amount money.Money, rate money.Rate) (money.Money, error) {
	if amount.Currency() != rate.From {
		return money.Money{}, &money.Error{Code: money.CodeRate, Field: "fx_rate",
			Message: fmt.Sprintf("the rate converts %s, but the amount is in %s", rate.From, amount.Currency())}
	}
	var product apd.Decimal
	if _, err := work.Mul(&product, amount.Decimal(), rate.Decimal()); err != nil {
		return money.Money{}, &money.Error{Code: money.CodeRange, Field: "amount", Message: err.Error()}
	}
	return money.FromRounded(halfAwayFromZero(&product, rate.To.MinorUnits), rate.To)
}

// Rate is RND-RATE (PRD-001 §10.4, rev 0.24): a quote turned into the one form Fyniq stores, base currency per one
// unit of the foreign currency. A quote per 100 units is round(quote ÷ 100, 10); a quote of foreign currency per one
// unit of the base is round(1 ÷ quote, 10); half away from zero, computed to 60 digits before its one rounding. The
// error of 10 places is open question 30's bound. A quote so large that its inverse rounds to zero has no rate.
func Rate(quote *apd.Decimal, form money.QuoteForm, from, to money.Currency, source string) (money.Rate, error) {
	var r apd.Decimal
	switch form {
	case money.QuoteBasePerUnit:
		r.Set(quote)
	case money.QuoteBasePer100:
		if _, err := work.Quo(&r, quote, apd.New(100, 0)); err != nil {
			return money.Rate{}, &money.Error{Code: money.CodeRate, Field: "quote", Message: err.Error()}
		}
	case money.QuoteForeignPerBase:
		if quote.Sign() <= 0 {
			return money.Rate{}, &money.Error{Code: money.CodeRate, Field: "quote", Message: fmt.Sprintf("the quote %s is not positive", quote.Text('f'))}
		}
		if _, err := work.Quo(&r, apd.New(1, 0), quote); err != nil {
			return money.Rate{}, &money.Error{Code: money.CodeRate, Field: "quote", Message: err.Error()}
		}
	default:
		return money.Rate{}, &money.Error{Code: money.CodeRate, Field: "quote_form", Message: fmt.Sprintf("%q is not a way to quote a rate", form),
			Fix: "Use base-per-unit, base-per-100 or foreign-per-base"}
	}
	rounded := halfAwayFromZero(&r, money.RateScale)
	if rounded.Sign() <= 0 {
		return money.Rate{}, &money.Error{Code: money.CodeRate, Field: "quote",
			Message: fmt.Sprintf("the quote %s gives a rate below 0.0000000001 %s per %s, the smallest rate Fyniq holds", quote.Text('f'), to, from),
			Fix:     "Check the quote and its form"}
	}
	rounded.Reduce(rounded)
	return money.ParseRate(rounded.Text('f'), from, to, source)
}

// Percent is RND-PERCENT (PRD-001 rev 0.65): part as a percentage of whole = round(part ÷ |whole| × 100, 1 decimal), half
// away from zero, with part's sign, as a plain decimal string such as "40.0" or "-12.5". It is computed to 60 digits
// before its one rounding, which no tie can fall within for amounts a stored column holds. A percentage is for showing
// only: a rule compares the exact amounts, never a rounded percentage. A zero whole has no percentage.
func Percent(part, whole money.Money) (string, error) {
	if part.Currency() != whole.Currency() {
		return "", fmt.Errorf("%s: %s of %s", PercentName, part.Currency(), whole.Currency())
	}
	if whole.IsZero() {
		return "", fmt.Errorf("%s: a percentage of zero", PercentName)
	}
	var x apd.Decimal
	if _, err := work.Mul(&x, part.Decimal(), apd.New(100, 0)); err != nil {
		return "", err
	}
	w := whole.Decimal()
	w.Abs(w)
	if _, err := work.Quo(&x, &x, w); err != nil {
		return "", err
	}
	r := halfAwayFromZero(&x, 1)
	r.Negative = r.Negative && !r.IsZero() // -0.04 shows as 0.0, never -0.0
	return r.Text('f'), nil
}

// AllocEven is ALLOC-EVEN: amount split into n periods by cumulative rounding. Period k is
// round(amount × k ÷ n) − round(amount × (k − 1) ÷ n), each rounded half away from zero to the
// minor unit, so the parts sum exactly to the whole, each part is within one minor unit of
// amount ÷ n, and a negative amount is the mirror image of the positive one.
func AllocEven(amount money.Money, n int) ([]money.Money, error) {
	if n < 1 {
		return nil, fmt.Errorf("%s: cannot split into %d periods", AllocEvenName, n)
	}
	units := amount.Currency().MinorUnits
	cumulative := func(k int) *apd.Decimal {
		var x apd.Decimal
		if _, err := work.Mul(&x, amount.Decimal(), apd.New(int64(k), 0)); err != nil {
			panic(err)
		}
		if _, err := work.Quo(&x, &x, apd.New(int64(n), 0)); err != nil {
			panic(err)
		}
		return halfAwayFromZero(&x, units)
	}
	parts := make([]money.Money, 0, n)
	prev := cumulative(0)
	for k := 1; k <= n; k++ {
		cur := cumulative(k)
		var part apd.Decimal
		if _, err := work.Sub(&part, cur, prev); err != nil {
			return nil, err
		}
		m, err := money.FromRounded(&part, amount.Currency())
		if err != nil {
			return nil, err
		}
		parts = append(parts, m)
		prev = cur
	}
	return parts, nil
}

// AllocProRata is ALLOC-PRORATA: amount split by non-negative integer weights with the
// largest-remainder method. Each part first gets its share rounded toward zero in minor units; the
// units left over go one each to the parts with the largest remainders, ties broken by their order.
// The parts sum exactly to the whole.
func AllocProRata(amount money.Money, weights []int64) ([]money.Money, error) {
	var total int64
	for i, w := range weights {
		if w < 0 || total+w < total {
			return nil, fmt.Errorf("%s: weight %d is negative or the weights overflow", AllocProRataName, i+1)
		}
		total += w
	}
	if total == 0 {
		return nil, fmt.Errorf("%s: the weights add up to zero", AllocProRataName)
	}
	minor, ok := amount.Minor()
	if !ok {
		return nil, &money.Error{Code: money.CodeRange, Field: "amount", Message: "the amount is too large to allocate"}
	}
	sign := int64(1)
	if minor < 0 {
		sign, minor = -1, -minor
	}
	type share struct {
		i         int
		base, rem *apd.Decimal
	}
	shares := make([]share, len(weights))
	var given int64
	for i, w := range weights {
		var exact apd.Decimal
		if _, err := work.Mul(&exact, apd.New(minor, 0), apd.New(w, 0)); err != nil {
			return nil, err
		}
		if _, err := work.Quo(&exact, &exact, apd.New(total, 0)); err != nil {
			return nil, err
		}
		var floor apd.Decimal
		if _, err := work.Floor(&floor, &exact); err != nil {
			return nil, err
		}
		var rem apd.Decimal
		if _, err := work.Sub(&rem, &exact, &floor); err != nil {
			return nil, err
		}
		f, err := floor.Int64()
		if err != nil {
			return nil, err
		}
		given += f
		shares[i] = share{i: i, base: &floor, rem: &rem}
	}
	order := make([]int, len(shares))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return shares[order[a]].rem.Cmp(shares[order[b]].rem) > 0 })
	parts := make([]int64, len(shares))
	for i, s := range shares {
		parts[i], _ = s.base.Int64()
	}
	for left, k := minor-given, 0; left > 0; left, k = left-1, k+1 {
		parts[order[k]]++
	}
	out := make([]money.Money, len(parts))
	for i, p := range parts {
		out[i] = money.FromMinor(sign*p, amount.Currency())
	}
	return out, nil
}
