package equity

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RegisterWaterfall is the exit waterfall's input as the register gives it on a day (FR-EQ-29; p2-reporting-planning design D14): each share class with the shares and money
// invested of its holders, and what the waterfall leaves out and says so.
type RegisterWaterfall struct {
	Classes []WaterfallClass
	// Notes are what the register records that the waterfall does not model, each named: an anti-dilution right (no adjustment is recorded, so none is applied) or a preference
	// class with no recorded multiple (modelled as ordinary shares).
	Notes []string
	// HasPreference is whether any class has a liquidation preference: the page is unavailable until one does.
	HasPreference bool
}

// splitPro is total divided among the weights by the largest-remainder method, ties to the earlier (ALLOC-PRORATA), in whole minor units.
func splitPro(total int64, weights []int64) []int64 {
	var sum int64
	for _, w := range weights {
		sum += w
	}
	out := make([]int64, len(weights))
	if sum == 0 {
		return out
	}
	rem := make([]*big.Int, len(weights))
	var given int64
	for i, w := range weights {
		num := new(big.Int).Mul(big.NewInt(total), big.NewInt(w))
		q, r := new(big.Int).QuoRem(num, big.NewInt(sum), new(big.Int))
		out[i], rem[i] = q.Int64(), r
		given += out[i]
	}
	idx := make([]int, len(weights))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return rem[b].Cmp(rem[a]) })
	for k := int64(0); k < total-given; k++ {
		out[idx[k]]++
	}
	return out
}

func wholeCount(d apd.Decimal, what string) (int64, error) {
	var x apd.Decimal
	if _, err := apd.BaseContext.WithPrecision(40).Quantize(&x, &d, 0); err != nil || x.Cmp(&d) != 0 {
		return 0, fmt.Errorf("%s: %s is not a whole number of shares", CodeWaterfall, what)
	}
	n, err := x.Int64()
	if err != nil {
		return 0, fmt.Errorf("%s: %s is too many shares", CodeWaterfall, what)
	}
	return n, nil
}

// WaterfallFromRegister reads the exit waterfall's classes from the register at the end of day: the cap table's holdings by class, each class's rights in force (a recorded
// class's liquidation multiple, participation, seniority and conversion ratio), and the money invested in it, which is the class's issues up to the day, shared among its holders
// by shares (ALLOC-PRORATA) [accountant to confirm: a holding bought from another holder has the class's average price]. A capped participating preference and a cumulative
// dividend are refused by name, never approximated, until a lawyer or accountant confirms them [lawyer to confirm].
func WaterfallFromRegister(ctx context.Context, u storage.UnitOfWork, day time.Time, base money.Currency) (RegisterWaterfall, error) {
	var out RegisterWaterfall
	r, err := ReaderIn(ctx, u)
	if err != nil {
		return out, err
	}
	table, err := r.At(day)
	if err != nil {
		return out, err
	}
	classes, err := ShareClasses(ctx, u)
	if err != nil {
		return out, err
	}
	issues, err := u.ShareIssues().List(ctx)
	if err != nil {
		return out, err
	}
	type group struct {
		key    string
		class  uuid.UUID
		acc    uuid.UUID
		lines  []CapLine
		name   string
		rights *ShareClass
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, l := range table.Lines {
		if l.Issued.Sign() <= 0 {
			continue
		}
		k := l.Key.Class.String()
		if l.Key.Class == uuid.Nil {
			k = "account:" + l.Key.Account.String()
		}
		g := byKey[k]
		if g == nil {
			g = &group{key: k, class: l.Key.Class, acc: l.Key.Account, name: "Ordinary shares"}
			if cur := InForce(classes[l.Key.Class]); l.Key.Class != uuid.Nil && cur != nil {
				g.rights, g.name = &cur.Value, cur.Value.Name
			}
			byKey[k] = g
			groups = append(groups, g)
		}
		g.lines = append(g.lines, l)
	}
	slices.SortStableFunc(groups, func(a, b *group) int { return strings.Compare(a.key, b.key) })
	for _, g := range groups {
		wc := WaterfallClass{ID: g.key, Name: g.name, Multiple: "0"}
		if g.rights != nil {
			rt := g.rights
			if rt.Liquidation.CapMultiple != "" {
				return out, fmt.Errorf("%s: the class %s has a capped participating preference, which the waterfall does not model until a lawyer or accountant confirms it [lawyer to confirm]", CodeWaterfall, g.name)
			}
			if rt.Dividend.Kind == "cumulative" {
				return out, fmt.Errorf("%s: the class %s has a cumulative dividend, which the waterfall does not model until a lawyer or accountant confirms it [lawyer to confirm]", CodeWaterfall, g.name)
			}
			if rt.AntiDilution != "" && rt.AntiDilution != "none" {
				out.Notes = append(out.Notes, fmt.Sprintf("%s has anti-dilution protection (%s), and no adjustment of its conversion is recorded, so none is applied [lawyer to confirm].", g.name, strings.ReplaceAll(rt.AntiDilution, "_", " ")))
			}
			if rt.Kind == "preference" {
				if rt.Liquidation.Multiple == "" {
					out.Notes = append(out.Notes, fmt.Sprintf("%s records no liquidation multiple, so it is modelled as ordinary shares.", g.name))
				} else {
					wc.Preference, wc.Multiple, wc.Participating = true, rt.Liquidation.Multiple, rt.Liquidation.Participating
					wc.Seniority = max(1, rt.Liquidation.Seniority)
					out.HasPreference = true
				}
			}
		}
		ratio := apd.New(1, 0)
		if g.rights != nil && g.rights.ConversionRatio != "" {
			d, _, err := apd.NewFromString(g.rights.ConversionRatio)
			if err != nil || d.Sign() <= 0 {
				return out, fmt.Errorf("%s: the class %s has a conversion ratio that is not a number above zero", CodeWaterfall, g.name)
			}
			ratio = d
		}
		var invested int64
		for _, is := range issues {
			if is.EffectiveOn.After(day) {
				continue
			}
			same := (g.class != uuid.Nil && is.ClassID != nil && *is.ClassID == g.class) || (is.ClassID == nil && is.ClassAccountID == g.acc) // an issue made before its account had a class is of the account's class
			if !same {
				continue
			}
			if is.Currency != base.Code {
				return out, fmt.Errorf("%s: an issue of the class %s is in %s, not the base currency %s", CodeWaterfall, g.name, is.Currency, base.Code)
			}
			m, err := money.ParseStored(string(is.Amount), base)
			if err != nil {
				return out, err
			}
			n, ok := m.Minor()
			if !ok {
				return out, fmt.Errorf("%s: an issue of %s is not an amount the waterfall can sum", CodeWaterfall, g.name)
			}
			invested += n
		}
		weights := make([]int64, len(g.lines))
		for i, l := range g.lines {
			var conv apd.Decimal
			if _, err := apd.BaseContext.WithPrecision(40).Mul(&conv, &l.Issued, ratio); err != nil {
				return out, err
			}
			if weights[i], err = wholeCount(conv, "the shares of "+g.name+" as converted"); err != nil {
				return out, err
			}
		}
		parts := splitPro(invested, weights)
		for i, l := range g.lines {
			wc.Holders = append(wc.Holders, WaterfallHolder{ID: l.Key.Holder.String(), Shares: weights[i], Invested: money.FromMinor(parts[i], base)})
		}
		out.Classes = append(out.Classes, wc)
	}
	return out, nil
}
