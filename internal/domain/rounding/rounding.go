// Package rounding holds Fyniq's named rounding and allocation policies (PRD-001 §10.4). Every
// rounding in the system goes through one of these functions, called by its name, and "half-up"
// means half away from zero. The policies the P0 posting source needs are here; the statutory ones
// (RND-CPF*, RND-SDL) arrive with their P1 sources, as RND-GST-EXCL did with bills (p1-purchases), RND-GST-INCL
// with customer deposits (p1-sales), RND-MILEAGE with expense claims (p1-expense-claims) and RND-SHARE with
// conversions to share capital (p1-founders-money).
package rounding

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The policy names, recorded with the figures they produce.
const (
	FXName           = "RND-FX"
	AllocEvenName    = "ALLOC-EVEN"
	AllocProRataName = "ALLOC-PRORATA"
	PercentName      = "RND-PERCENT"
	KPIName          = "RND-KPI"
	RateName         = "RND-RATE"
	GSTExclName      = "RND-GST-EXCL"
	LineName         = "RND-LINE"
	GSTShareName     = "RND-GST-SHARE"
	WHTName          = "RND-WHT"
	DiscountName     = "RND-DISCOUNT"
	GSTInclName      = "RND-GST-INCL"
	DepositName      = "RND-DEPOSIT"
	MileageName      = "RND-MILEAGE"
	ShareName        = "RND-SHARE"
	OwnershipName    = "RND-DISPLAY"
	PlanName         = "RND-PLAN"
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

// Plan is RND-PLAN's amount (rev 0.214): a forecast's or a feasibility study's driver line, an average or another amount
// from a division = round(its exact value, cur's minor units), half away from zero, once; -0 shows as 0. The value may
// hold any number of digits: a forecast compounds customers exactly.
func Plan(x *apd.Decimal, cur money.Currency) *apd.Decimal { return plan(x, cur.MinorUnits) }

// PlanRatio is RND-PLAN's ratio or multiple (LTV ÷ CAC, the burn multiple, the profitability index): 2 decimals.
func PlanRatio(x *apd.Decimal) string { return plan(x, 2).Text('f') }

// PlanRate is RND-PLAN's IRR: 4 decimals of a fraction (0.1234 is 12.34%).
func PlanRate(x *apd.Decimal) string { return plan(x, 4).Text('f') }

// PlanYears is RND-PLAN's payback in years, and PlanCount its customers and units: each to 1 decimal.
func PlanYears(x *apd.Decimal) string { return plan(x, 1).Text('f') }

// PlanCount is RND-PLAN's customers and units, shown to 1 decimal; the forecast keeps them exact.
func PlanCount(x *apd.Decimal) string { return plan(x, 1).Text('f') }

// PlanQuotient is num ÷ den under RND-PLAN at places decimals: the exact quotient, rounded once. It is worked to 60
// digits past the units of its magnitude, which no tie of a quotient of two stored decimals falls within: a quotient
// that ends within them is exact, and one that does not end is never half a unit. A zero den has no quotient.
func PlanQuotient(num, den *apd.Decimal, places int) (*apd.Decimal, error) {
	if den.IsZero() {
		return nil, fmt.Errorf("%s: a quotient of zero", PlanName)
	}
	digits := uint32(num.NumDigits()) + uint32(max(0, num.Exponent)) + uint32(den.NumDigits()) + uint32(places) + 60
	c := apd.BaseContext.WithPrecision(digits)
	var q apd.Decimal
	if _, err := c.Quo(&q, num, den); err != nil {
		return nil, err
	}
	return plan(&q, places), nil
}

// plan rounds x to places decimals, half away from zero, at whatever precision x needs.
func plan(x *apd.Decimal, places int) *apd.Decimal {
	digits := uint32(x.NumDigits()) + uint32(max(0, x.Exponent)) + uint32(places) + 2
	c := apd.BaseContext.WithPrecision(max(digits, 60))
	c.Rounding = apd.RoundHalfUp
	out := new(apd.Decimal)
	if _, err := c.Quantize(out, x, -int32(places)); err != nil {
		panic(fmt.Sprintf("rounding: %s %s to %d places: %v", PlanName, x.Text('f'), places, err))
	}
	out.Negative = out.Negative && !out.IsZero()
	return out
}

// KPI is RND-KPI (rev 0.161): a KPI's value = round(its exact value, places), half away from zero, once; a value that
// rounds to zero shows as 0, never -0.
func KPI(x *apd.Decimal, places int) string {
	r := halfAwayFromZero(x, places)
	r.Negative = r.Negative && !r.IsZero()
	return r.Text('f')
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

// Line is RND-LINE (PRD-001 §10.4, rev 0.87; provisional until the accountant's sign-off): a line's amount =
// round(quantity × unit price, the currency's minor units), half away from zero, computed exactly before its one
// rounding.
func Line(q money.Quantity, p money.UnitPrice) (money.Money, error) {
	var product apd.Decimal
	if _, err := work.Mul(&product, q.Decimal(), p.Decimal()); err != nil {
		return money.Money{}, &money.Error{Code: money.CodeRange, Field: "quantity", Message: err.Error()}
	}
	return money.FromRounded(halfAwayFromZero(&product, p.Currency().MinorUnits), p.Currency())
}

// LineDiscounted is RND-LINE for a line with a discount (PRD-001 §10.4, rev 0.116; provisional until the accountant's
// sign-off): round(quantity × unit price × (100 − discount) ÷ 100, the currency's minor units), half away from zero,
// computed exactly before its one rounding, so a discounted line rounds once, never on its gross and again on its
// discount. The discount is a percentage from 0 to 100 with at most two decimals; empty or "0" is Line.
func LineDiscounted(q money.Quantity, p money.UnitPrice, discount string) (money.Money, error) {
	if discount == "" || isZeroText(discount) {
		return Line(q, p)
	}
	if !IsShare(discount) {
		return money.Money{}, fmt.Errorf("%s: the discount %q is not a percentage from 0 to 100 with at most two decimals", LineName, discount)
	}
	d, _, err := apd.NewFromString(discount)
	if err != nil {
		return money.Money{}, err
	}
	var kept, product apd.Decimal
	if _, err := work.Sub(&kept, apd.New(100, 0), d); err != nil {
		return money.Money{}, err
	}
	if _, err := work.Mul(&product, q.Decimal(), p.Decimal()); err != nil {
		return money.Money{}, &money.Error{Code: money.CodeRange, Field: "quantity", Message: err.Error()}
	}
	if _, err := work.Mul(&product, &product, &kept); err != nil {
		return money.Money{}, &money.Error{Code: money.CodeRange, Field: "quantity", Message: err.Error()}
	}
	if _, err := work.Quo(&product, &product, apd.New(100, 0)); err != nil {
		return money.Money{}, err
	}
	return money.FromRounded(halfAwayFromZero(&product, p.Currency().MinorUnits), p.Currency())
}

// LinePart is part of a line under RND-LINE, after the quantity before was taken by earlier parts, by cumulative
// rounding as ALLOC-EVEN: RND-LINE of everything taken so far, this part included, less RND-LINE of what was taken
// before it. The parts of a line then add up exactly to the whole line's amount, and each is within one minor unit of
// its own quantity × price.
func LinePart(before, part money.Quantity, p money.UnitPrice) (money.Money, error) {
	all, err := Line(before.Add(part), p)
	if err != nil {
		return money.Money{}, err
	}
	prev, err := Line(before, p)
	if err != nil {
		return money.Money{}, err
	}
	return all.Sub(prev)
}

// sharePattern reads a share strictly: a percentage from 0 to 100 with at most two decimals ("60", "33.33", "100").
var sharePattern = regexp.MustCompile(`^(100(\.0{1,2})?|[0-9]{1,2}(\.[0-9]{1,2})?)$`)

// IsShare reports whether s is a share GSTShare takes: a percentage from 0 to 100 with at most two decimals.
func IsShare(s string) bool { return sharePattern.MatchString(s) }

// GSTShare is RND-GST-SHARE (PRD-001 §10.4, rev 0.89; provisional until the accountant's sign-off): the input tax
// claimed of a line's GST when only shares of it are claimable, such as the business share and, for an invoice in an
// agent's name, the share the company reimbursed = round(GST × share₁ ÷ 100 × share₂ ÷ 100 …, the currency's minor
// units), half away from zero, computed exactly before its one rounding. The rest of the GST stays in the cost. Shares
// of 100 claim the GST whole, and a share of 0 claims none.
func GSTShare(gst money.Money, shares ...string) (money.Money, error) {
	product := gst.Decimal()
	for i, sh := range shares {
		if !sharePattern.MatchString(sh) {
			return money.Money{}, fmt.Errorf("%s: share %d %q is not a percentage from 0 to 100 with at most two decimals", GSTShareName, i+1, sh)
		}
		d, _, err := apd.NewFromString(sh)
		if err != nil {
			return money.Money{}, fmt.Errorf("%s: share %d %q: %w", GSTShareName, i+1, sh, err)
		}
		if _, err := work.Mul(product, product, d); err != nil {
			return money.Money{}, err
		}
		if _, err := work.Quo(product, product, apd.New(100, 0)); err != nil {
			return money.Money{}, err
		}
	}
	return money.FromRounded(halfAwayFromZero(product, gst.Currency().MinorUnits), gst.Currency())
}

// IsWHTRate reports whether s is a withholding rate WHT takes: a percentage above 0 and up to 100, with at most two
// decimals ("17", "10", "24", "12.5").
func IsWHTRate(s string) bool { return sharePattern.MatchString(s) && !isZeroText(s) }

func isZeroText(s string) bool {
	d, _, err := apd.NewFromString(s)
	return err == nil && d.IsZero()
}

// WHT is RND-WHT (PRD-001 §10.4, rev 0.90; provisional until the accountant's sign-off): the tax withheld from a
// payment to a non-resident supplier (FR-AP-19), in the base currency = round(⌊gross⌋ × rate ÷ 100, the base
// currency's minor units), half away from zero, where gross is the payment before GST in the base currency and ⌊⌋
// rounds it down to a whole unit, as IRAS's withholding-tax form states it ("rounded down to the nearest dollar").
func WHT(gross money.Money, percent string) (money.Money, error) {
	if !IsWHTRate(percent) {
		return money.Money{}, fmt.Errorf("%s: the rate %q is not a percentage above 0 and up to 100 with at most two decimals", WHTName, percent)
	}
	c := work.WithPrecision(60)
	c.Rounding = apd.RoundDown
	whole := new(apd.Decimal)
	if _, err := c.Quantize(whole, gross.Decimal(), 0); err != nil {
		return money.Money{}, err
	}
	base, err := money.FromRounded(whole, gross.Currency())
	if err != nil {
		return money.Money{}, err
	}
	return GSTShare(base, percent)
}

// Withheld is RND-WHT's part of a supplier's own amount, for a bill in another currency than the base: round(gross ×
// rate ÷ 100, the currency's minor units), half away from zero. What it owes the supplier is its total less this.
func Withheld(gross money.Money, percent string) (money.Money, error) {
	if !IsWHTRate(percent) {
		return money.Money{}, fmt.Errorf("%s: the rate %q is not a percentage above 0 and up to 100 with at most two decimals", WHTName, percent)
	}
	return GSTShare(gross, percent)
}

// Discount is RND-DISCOUNT (PRD-001 §10.4, rev 0.93; provisional until the accountant's sign-off): an early-payment
// discount a supplier offers on a bill (FR-AP-13) = round(total × percent ÷ 100, the currency's minor units), half away
// from zero, computed exactly before its one rounding. The percent is above 0 and below 100, with at most two decimals.
func Discount(total money.Money, percent string) (money.Money, error) {
	if !IsWHTRate(percent) || percent == "100" || strings.HasPrefix(percent, "100.") {
		return money.Money{}, fmt.Errorf("%s: %q is not a percentage above 0 and below 100 with at most two decimals", DiscountName, percent)
	}
	return GSTShare(total, percent)
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

// Implied is the rate a conversion implies, under RND-RATE: base ÷ foreign, base currency per one unit of the foreign
// currency, rounded once to 10 places. A bill in a foreign currency paid from a base-currency account settles at the
// rate its payment implies (ENG-41).
func Implied(base, foreign money.Money, source string) (money.Rate, error) {
	if foreign.Sign() <= 0 || base.Sign() <= 0 {
		return money.Rate{}, &money.Error{Code: money.CodeRate, Field: "paid",
			Message: fmt.Sprintf("%s %s for %s %s implies no rate", base, base.Currency(), foreign, foreign.Currency())}
	}
	var q apd.Decimal
	if _, err := work.Quo(&q, base.Decimal(), foreign.Decimal()); err != nil {
		return money.Rate{}, &money.Error{Code: money.CodeRate, Field: "paid", Message: err.Error()}
	}
	return Rate(&q, money.QuoteBasePerUnit, foreign.Currency(), base.Currency(), source)
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

// Ownership is the cap table's percentage (FR-EQ-20, which names RND-DISPLAY for it): part as a percentage of whole =
// round(part ÷ whole × 100, 2 decimals), half-up, once, from the exact quotient computed to 60 digits, as a plain
// decimal string such as "79.87". Shares and votes are never negative. A table's percentages are never forced to sum to
// 100.00 (design D7). A zero whole has no percentage.
func Ownership(part, whole *apd.Decimal) (string, error) {
	if part.Negative || whole.Negative {
		return "", fmt.Errorf("%s: a percentage of %s in %s", OwnershipName, part.Text('f'), whole.Text('f'))
	}
	if whole.IsZero() {
		return "", fmt.Errorf("%s: a percentage of zero", OwnershipName)
	}
	var x apd.Decimal
	if _, err := work.Mul(&x, part, apd.New(100, 0)); err != nil {
		return "", err
	}
	if _, err := work.Quo(&x, &x, whole); err != nil {
		return "", err
	}
	return halfAwayFromZero(&x, 2).Text('f'), nil
}

// Display is RND-DISPLAY for a figure that is shown and never posted or stored, such as a SAFE conversion's working
// (the company's capitalisation once the SAFEs convert, the part of it they take): round(x, places), half away from
// zero, once, as a plain decimal string; a figure that rounds to zero shows as 0.00, never -0.00.
func Display(x *apd.Decimal, places int) string {
	r := halfAwayFromZero(x, places)
	r.Negative = r.Negative && !r.IsZero()
	return r.Text('f')
}

// GSTMethod is how RND-GST-EXCL rounds a document's GST (PRD-001 §10.4): once per GST code's subtotal, the default,
// or once per line, an organisation setting. IRAS accepts either when it is applied consistently.
type GSTMethod string

// The two methods of RND-GST-EXCL.
// GSTIncl is RND-GST-INCL (PRD-001 §10.4): the GST inside a gross amount = round(gross × rate ÷ (1 + rate), the
// currency's minor units), half away from zero, computed exactly before its one rounding; the net is gross − GST. rate
// is the GST code's rate as a fraction, such as "0.09", from the sg-gst pack in force at the tax point.
func GSTIncl(gross money.Money, rate string) (money.Money, error) {
	if !gstRate.MatchString(rate) {
		return money.Money{}, fmt.Errorf("%s: the rate %q is not a fraction such as 0.09", GSTInclName, rate)
	}
	r, _, err := apd.NewFromString(rate)
	if err != nil {
		return money.Money{}, fmt.Errorf("%s: the rate %q: %w", GSTInclName, rate, err)
	}
	var onePlus, product, quotient apd.Decimal
	if _, err := work.Add(&onePlus, r, apd.New(1, 0)); err != nil {
		return money.Money{}, err
	}
	if _, err := work.Mul(&product, gross.Decimal(), r); err != nil {
		return money.Money{}, err
	}
	if _, err := work.Quo(&quotient, &product, &onePlus); err != nil {
		return money.Money{}, err
	}
	return money.FromRounded(halfAwayFromZero(&quotient, gross.Currency().MinorUnits), gross.Currency())
}

// DepositPart is RND-DEPOSIT (PRD-001 §10.4, rev 0.127; provisional until the accountant's sign-off): the part of
// what is left of a customer deposit's base amount, or of the output tax it bore, that a use of amount takes, of the
// remaining amount of the deposit in its currency = round(left × amount ÷ remaining, the base currency's minor units),
// half away from zero, computed exactly before its one rounding. A use of all that remains takes all that is left, so
// the uses of a deposit add up exactly to it, whatever order they are made and voided in.
func DepositPart(left, amount, remaining money.Money) (money.Money, error) {
	switch {
	case amount.Currency() != remaining.Currency():
		return money.Money{}, fmt.Errorf("%s: the use is in %s, the deposit in %s", DepositName, amount.Currency(), remaining.Currency())
	case remaining.Sign() <= 0 || amount.Sign() <= 0:
		return money.Money{}, fmt.Errorf("%s: a use of %s of %s remaining", DepositName, amount, remaining)
	}
	c, err := amount.Cmp(remaining)
	if err != nil {
		return money.Money{}, err
	}
	switch {
	case c > 0:
		return money.Money{}, fmt.Errorf("%s: a use of %s is more than the %s remaining", DepositName, amount, remaining)
	case c == 0:
		return left, nil
	}
	var product, quotient apd.Decimal
	if _, err := work.Mul(&product, left.Decimal(), amount.Decimal()); err != nil {
		return money.Money{}, err
	}
	if _, err := work.Quo(&quotient, &product, remaining.Decimal()); err != nil {
		return money.Money{}, err
	}
	return money.FromRounded(halfAwayFromZero(&quotient, left.Currency().MinorUnits), left.Currency())
}

const (
	GSTPerSubtotal GSTMethod = "subtotal"
	GSTPerLine     GSTMethod = "line"
)

// GSTNet is one line of a document for RND-GST-EXCL: its net amount, its GST code, and the code's rate in force at the
// document's tax point as a decimal fraction, such as "0.09", from the sg-gst pack.
type GSTNet struct {
	Code string
	Rate string
	Net  money.Money
}

// GSTSubtotal is one GST code's net and GST on a document.
type GSTSubtotal struct {
	Code string
	Rate string
	Net  money.Money
	GST  money.Money
}

// gstRate reads a GST rate strictly: a fraction from 0 to under 1, with at most six decimals ("0.09", "0").
var gstRate = regexp.MustCompile(`^0(\.[0-9]{1,6})?$`)

// GSTExcl is RND-GST-EXCL: GST = round(net × rate, the currency's minor units), half away from zero, rounded once
// per GST code's subtotal (GSTPerSubtotal) or once per line and then added (GSTPerLine). It answers each code once,
// in the order the codes first appear. The lines are in one currency, and the lines of one code have one rate.
func GSTExcl(lines []GSTNet, method GSTMethod) ([]GSTSubtotal, error) {
	if method != GSTPerSubtotal && method != GSTPerLine {
		return nil, fmt.Errorf("%s: no method %q; use %q or %q", GSTExclName, method, GSTPerSubtotal, GSTPerLine)
	}
	var out []GSTSubtotal
	rates := []*apd.Decimal{}
	exact := []*apd.Decimal{} // each code's exact GST, rounded once at the end by subtotal
	at := map[string]int{}
	for i, l := range lines {
		cur := l.Net.Currency()
		if cur != lines[0].Net.Currency() {
			return nil, fmt.Errorf("%s: line %d is in %s, line 1 in %s", GSTExclName, i+1, cur, lines[0].Net.Currency())
		}
		if !gstRate.MatchString(l.Rate) {
			return nil, fmt.Errorf("%s: line %d's rate %q is not a fraction such as 0.09", GSTExclName, i+1, l.Rate)
		}
		r, _, err := apd.NewFromString(l.Rate)
		if err != nil {
			return nil, fmt.Errorf("%s: line %d's rate %q: %w", GSTExclName, i+1, l.Rate, err)
		}
		k, seen := at[l.Code]
		if !seen {
			k = len(out)
			at[l.Code] = k
			out = append(out, GSTSubtotal{Code: l.Code, Rate: l.Rate, Net: money.Zero(cur), GST: money.Zero(cur)})
			rates = append(rates, r)
			exact = append(exact, new(apd.Decimal))
		} else if rates[k].Cmp(r) != 0 {
			return nil, fmt.Errorf("%s: line %d gives code %s the rate %s, an earlier line %s", GSTExclName, i+1, l.Code, l.Rate, out[k].Rate)
		}
		if out[k].Net, err = out[k].Net.Add(l.Net); err != nil {
			return nil, err
		}
		var product apd.Decimal
		if _, err := work.Mul(&product, l.Net.Decimal(), r); err != nil {
			return nil, err
		}
		if method == GSTPerLine {
			g, err := money.FromRounded(halfAwayFromZero(&product, cur.MinorUnits), cur)
			if err != nil {
				return nil, err
			}
			if out[k].GST, err = out[k].GST.Add(g); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := work.Add(exact[k], exact[k], &product); err != nil {
			return nil, err
		}
	}
	if method == GSTPerSubtotal {
		for k := range out {
			g, err := money.FromRounded(halfAwayFromZero(exact[k], out[k].Net.Currency().MinorUnits), out[k].Net.Currency())
			if err != nil {
				return nil, err
			}
			out[k].GST = g
		}
	}
	return out, nil
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

// Mileage is RND-MILEAGE (PRD-001 §10.4; FR-AP-16): a trip's amount = round(distance in km, to 0.1 km, half-up) × the
// rate per km, then rounded to the rate currency's minor units, half-up, per trip. It returns the distance it rounded
// to, which the trip is claimed for, and the amount; the distance and the rate are more than zero.
func Mileage(distance money.Quantity, rate money.Money) (money.Quantity, money.Money, error) {
	if rate.Sign() <= 0 {
		return money.Quantity{}, money.Money{}, &money.Error{Code: money.CodeRange, Field: "rate",
			Message: fmt.Sprintf("the rate per km is %s %s", rate, rate.Currency()), Fix: "Set a rate per km above zero"}
	}
	km, err := money.ParseQuantity(halfAwayFromZero(distance.Decimal(), 1).Text('f'), "distance")
	if err != nil {
		return money.Quantity{}, money.Money{}, err
	}
	if km.Sign() <= 0 {
		return money.Quantity{}, money.Money{}, &money.Error{Code: money.CodeRange, Field: "distance",
			Message: fmt.Sprintf("the distance is %s km, which rounds to %s km", distance, km), Fix: "Enter the distance driven, at least 0.05 km"}
	}
	var product apd.Decimal
	if _, err := work.Mul(&product, km.Decimal(), rate.Decimal()); err != nil {
		return money.Quantity{}, money.Money{}, &money.Error{Code: money.CodeRange, Field: "distance", Message: err.Error()}
	}
	amount, err := money.FromRounded(halfAwayFromZero(&product, rate.Currency().MinorUnits), rate.Currency())
	return km, amount, err
}

// Shares are what RND-SHARE allots for an amount at a price per share: a whole number of shares, the amount they
// convert (round(shares × price, minor units)), and the remainder that stays on the source balance.
type Shares struct {
	Count     apd.Decimal
	Converted money.Money
	Remainder money.Money
}

// Share is RND-SHARE (PRD-001 §10.4, rev 0.3; FR-EQ-06): the largest whole number of shares n for which round(n ×
// price, the currency's minor units), half up, is no more than amount; converted = round(n × price), and the
// remainder stays on the source balance. Never fractional shares. Since a positive x rounds half up to at most amount
// exactly when x < amount + half a minor unit, n is the largest whole number below (amount + half a minor unit) ÷
// price. An amount too small for one share allots none and converts nothing.
func Share(amount money.Money, price money.UnitPrice) (Shares, error) {
	cur := amount.Currency()
	switch {
	case price.Currency() != cur:
		return Shares{}, &money.Error{Code: money.CodeCurrency, Field: "price", Message: fmt.Sprintf("the price is in %s, the amount in %s", price.Currency().Code, cur.Code)}
	case amount.Sign() <= 0:
		return Shares{}, &money.Error{Code: money.CodeRange, Field: "amount", Message: "the amount converted is not more than zero"}
	case price.Sign() <= 0:
		return Shares{}, &money.Error{Code: money.CodeRange, Field: "price", Message: "the price per share is not more than zero"}
	}
	// limit = amount + half a minor unit; n = the largest whole number with n × price < limit.
	half := apd.New(5, -int32(cur.MinorUnits)-1)
	var limit, quotient apd.Decimal
	if _, err := work.Add(&limit, amount.Decimal(), half); err != nil {
		return Shares{}, err
	}
	if _, err := work.Quo(&quotient, &limit, price.Decimal()); err != nil {
		return Shares{}, err
	}
	var n apd.Decimal
	floor := work.WithPrecision(60)
	floor.Rounding = apd.RoundFloor
	if _, err := floor.Quantize(&n, &quotient, 0); err != nil {
		return Shares{}, err
	}
	var product apd.Decimal
	if _, err := work.Mul(&product, &n, price.Decimal()); err != nil {
		return Shares{}, err
	}
	if product.Cmp(&limit) >= 0 { // exactly on the limit: it rounds up past the amount
		if _, err := work.Sub(&n, &n, apd.New(1, 0)); err != nil {
			return Shares{}, err
		}
		if _, err := work.Mul(&product, &n, price.Decimal()); err != nil {
			return Shares{}, err
		}
	}
	converted, err := money.FromRounded(halfAwayFromZero(&product, cur.MinorUnits), cur)
	if err != nil {
		return Shares{}, err
	}
	remainder, err := amount.Sub(converted)
	if err != nil {
		return Shares{}, err
	}
	out := Shares{Converted: converted, Remainder: remainder}
	if _, err := work.Quantize(&out.Count, &n, 0); err != nil { // a whole number, with no exponent
		return Shares{}, err
	}
	return out, nil
}
