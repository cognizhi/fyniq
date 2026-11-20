package equity

import (
	"fmt"
	"math/big"
	"slices"
	"sort"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The exit waterfall (PRD-001 FR-EQ-29; p2-reporting-planning design D14): for an exit value, what each share class and each holder receives under the
// preferences the classes record, next to the as-converted split, with each class's better choice. It is a sandbox: it reads and posts nothing. It models
// only the structures the company's lawyer or accountant has confirmed: a non-participating preference of a multiple of the money invested, an uncapped
// participating preference, and seniority between classes. A capped participating preference, a cumulative dividend or a conversion adjustment is
// refused by name, never approximated.

// The codes of the waterfall's rules.
const (
	CodeWaterfall = "EQ-WF-01" // a class or an exit value the waterfall cannot model
)

// WaterfallHolder is a holder's stake in a class.
type WaterfallHolder struct {
	ID       string
	Shares   int64 // as converted: the shares times the class's conversion ratio
	Invested money.Money
}

// WaterfallClass is a share class and its recorded preference.
type WaterfallClass struct {
	ID            string
	Name          string
	Preference    bool   // a preference class; false for ordinary shares
	Multiple      string // the multiple of the money invested a non-participating or participating preference pays first, such as "1" or "1.5"
	Participating bool   // after its preference it shares the rest as converted, with no cap
	Capped        bool   // a participating preference with a cap: not modelled
	Seniority     int    // 1 is paid first; classes of the same seniority share pari passu, in proportion to their preferences
	Holders       []WaterfallHolder
}

// WaterfallResult is what the exit pays.
type WaterfallResult struct {
	Classes     []WaterfallPaid
	AsConverted []WaterfallPaid // every class converted, no preference: each share the same
	Total       money.Money
}

// WaterfallPaid is a class's proceeds and its holders'.
type WaterfallPaid struct {
	ID         string
	Name       string
	Preference money.Money // what it takes as its preference (zero for a class that converts or has none)
	Residual   money.Money // what it takes of the rest as converted
	Total      money.Money
	Converts   bool // a non-participating preference class that does better as converted
	Holders    []WaterfallHolderPaid
}

// WaterfallHolderPaid is a holder's part of a class's proceeds.
type WaterfallHolderPaid struct {
	ID    string
	Total money.Money
}

func (c WaterfallClass) shares() int64 {
	var n int64
	for _, h := range c.Holders {
		n += h.Shares
	}
	return n
}

func (c WaterfallClass) invested(cur money.Currency) (money.Money, error) {
	sum := money.Zero(cur)
	var err error
	for _, h := range c.Holders {
		if sum, err = sum.Add(h.Invested); err != nil {
			return sum, err
		}
	}
	return sum, nil
}

// preferenceOf is a class's preference: its multiple of the money invested, rounded half away from zero to the currency's minor units (RND-PLAN).
func preferenceOf(c WaterfallClass, cur money.Currency) (money.Money, error) {
	inv, err := c.invested(cur)
	if err != nil {
		return inv, err
	}
	mult, _, err := apd.NewFromString(c.Multiple)
	if err != nil || mult.Sign() < 0 {
		return money.Money{}, fmt.Errorf("%s: the class %s has no multiple that is a number from zero", CodeWaterfall, c.Name)
	}
	var x apd.Decimal
	if _, err := apd.BaseContext.WithPrecision(60).Mul(&x, inv.Decimal(), mult); err != nil {
		return money.Money{}, err
	}
	return money.FromRounded(rounding.Plan(&x, cur), cur)
}

// ModelWaterfall pays an exit value out under the classes' preferences.
func ModelWaterfall(exit money.Money, classes []WaterfallClass) (WaterfallResult, error) {
	cur := exit.Currency()
	if exit.Sign() < 0 {
		return WaterfallResult{}, fmt.Errorf("%s: the exit value is below zero", CodeWaterfall)
	}
	prefs := make([]money.Money, len(classes))
	for i, c := range classes {
		if c.Capped {
			return WaterfallResult{}, fmt.Errorf("%s: the class %s has a capped participating preference, which the waterfall does not model until a lawyer or accountant confirms it", CodeWaterfall, c.Name)
		}
		if c.Participating && !c.Preference {
			return WaterfallResult{}, fmt.Errorf("%s: the class %s is participating without a preference", CodeWaterfall, c.Name)
		}
		if c.shares() == 0 {
			return WaterfallResult{}, fmt.Errorf("%s: the class %s has no shares", CodeWaterfall, c.Name)
		}
		if c.Preference {
			var err error
			if prefs[i], err = preferenceOf(c, cur); err != nil {
				return WaterfallResult{}, err
			}
		} else {
			prefs[i] = money.Zero(cur)
		}
	}
	total := money.Zero(cur)
	converted := make([]bool, len(classes))
	pay := func(conv []bool) ([]money.Money, []money.Money, error) { return payOut(exit, classes, prefs, conv) }
	// The non-participating preference classes, the lowest preference per share first, convert while converting pays them more.
	var order []int
	for i, c := range classes {
		if c.Preference && !c.Participating {
			order = append(order, i)
		}
	}
	// The lowest preference per share first: pref_a / shares_a < pref_b / shares_b as pref_a * shares_b < pref_b * shares_a, in integers.
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		pa, _ := prefs[ia].Minor()
		pb, _ := prefs[ib].Minor()
		left := new(big.Int).Mul(big.NewInt(pa), big.NewInt(classes[ib].shares()))
		right := new(big.Int).Mul(big.NewInt(pb), big.NewInt(classes[ia].shares()))
		return left.Cmp(right) < 0
	})
	class := func(i int, conv []bool) (money.Money, error) { // what a class takes in all, given who converts
		taken, rest, err := pay(conv)
		if err != nil {
			return money.Money{}, err
		}
		return taken[i].Add(rest[i])
	}
	for _, i := range order {
		stay, err := class(i, converted)
		if err != nil {
			return WaterfallResult{}, err
		}
		converted[i] = true
		goes, err := class(i, converted)
		if err != nil {
			return WaterfallResult{}, err
		}
		if c, err := goes.Cmp(stay); err != nil {
			return WaterfallResult{}, err
		} else if c <= 0 {
			converted[i] = false
			break
		}
	}
	prefPaid, residual, err := pay(converted)
	if err != nil {
		return WaterfallResult{}, err
	}
	every := make([]bool, len(classes))
	for i := range every {
		every[i] = true
	}
	nothing := make([]money.Money, len(classes))
	for i := range nothing {
		nothing[i] = money.Zero(cur)
	}
	asConvertedPref, asConvertedRes, err := payOut(exit, classes, nothing, every)
	if err != nil {
		return WaterfallResult{}, err
	}
	out := WaterfallResult{Total: exit}
	for i, c := range classes {
		p, err := paidOf(c, prefPaid[i], residual[i], converted[i], cur)
		if err != nil {
			return WaterfallResult{}, err
		}
		a, err := paidOf(c, asConvertedPref[i], asConvertedRes[i], false, cur)
		if err != nil {
			return WaterfallResult{}, err
		}
		out.Classes, out.AsConverted = append(out.Classes, p), append(out.AsConverted, a)
		if total, err = total.Add(p.Total); err != nil {
			return WaterfallResult{}, err
		}
	}
	if !total.Equal(exit) {
		return WaterfallResult{}, fmt.Errorf("%s: the classes' proceeds %s are not the exit value %s", CodeWaterfall, total, exit)
	}
	return out, nil
}

// payOut pays the preferences by seniority and then the rest as converted, given which classes convert: each class's preference taken, and what it takes of the rest.
func payOut(exit money.Money, classes []WaterfallClass, prefs []money.Money, converted []bool) (taken, residual []money.Money, err error) {
	cur := exit.Currency()
	taken, residual = make([]money.Money, len(classes)), make([]money.Money, len(classes))
	for i := range classes {
		taken[i], residual[i] = money.Zero(cur), money.Zero(cur)
	}
	remaining := exit
	var seniorities []int
	for i, c := range classes {
		if c.Preference && !converted[i] && !prefs[i].IsZero() && !slices.Contains(seniorities, c.Seniority) {
			seniorities = append(seniorities, c.Seniority)
		}
	}
	sort.Ints(seniorities)
	for _, s := range seniorities {
		var idx []int
		var weights []int64
		want := money.Zero(cur)
		for i, c := range classes {
			if c.Preference && !converted[i] && c.Seniority == s && !prefs[i].IsZero() {
				idx = append(idx, i)
				m, _ := prefs[i].Minor()
				weights = append(weights, m)
				if want, err = want.Add(prefs[i]); err != nil {
					return nil, nil, err
				}
			}
		}
		paid := want
		if c, err := want.Cmp(remaining); err != nil {
			return nil, nil, err
		} else if c > 0 {
			paid = remaining // not enough to pay the group in full: pari passu, in proportion to the preferences
		}
		parts, err := rounding.AllocProRata(paid, weights)
		if err != nil {
			return nil, nil, err
		}
		for k, i := range idx {
			taken[i] = parts[k]
		}
		if remaining, err = remaining.Sub(paid); err != nil {
			return nil, nil, err
		}
	}
	// The rest is shared by the shares as converted: ordinary shares, converted classes and participating classes.
	var idx []int
	var weights []int64
	for i, c := range classes {
		if !c.Preference || converted[i] || c.Participating {
			idx = append(idx, i)
			weights = append(weights, c.shares())
		}
	}
	if len(idx) == 0 || remaining.IsZero() {
		return taken, residual, nil
	}
	parts, err := rounding.AllocProRata(remaining, weights)
	if err != nil {
		return nil, nil, err
	}
	for k, i := range idx {
		residual[i] = parts[k]
	}
	return taken, residual, nil
}

// paidOf splits a class's proceeds among its holders: the preference by the money each invested and the rest by shares (ALLOC-PRORATA).
func paidOf(c WaterfallClass, pref, residual money.Money, converts bool, cur money.Currency) (WaterfallPaid, error) {
	out := WaterfallPaid{ID: c.ID, Name: c.Name, Preference: pref, Residual: residual, Converts: converts}
	var err error
	if out.Total, err = pref.Add(residual); err != nil {
		return out, err
	}
	byMoney, byShares := make([]int64, len(c.Holders)), make([]int64, len(c.Holders))
	for i, h := range c.Holders {
		byMoney[i], _ = h.Invested.Minor()
		byShares[i] = h.Shares
	}
	holders := make([]money.Money, len(c.Holders))
	for i := range holders {
		holders[i] = money.Zero(cur)
	}
	split := func(amount money.Money, weights []int64) error {
		if amount.IsZero() {
			return nil
		}
		parts, err := rounding.AllocProRata(amount, weights)
		if err != nil {
			return err
		}
		for i := range parts {
			if holders[i], err = holders[i].Add(parts[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if err := split(pref, byMoney); err != nil {
		return out, err
	}
	if err := split(residual, byShares); err != nil {
		return out, err
	}
	for i, h := range c.Holders {
		out.Holders = append(out.Holders, WaterfallHolderPaid{ID: h.ID, Total: holders[i]})
	}
	return out, nil
}
