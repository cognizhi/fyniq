package equity

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// CodeRound is the code of a round model that does not fit: its price, investors, top-up or instruments.
const CodeRound = "EQUITY-19"

// RoundInvestor is new money in a modelled round: the contact who puts it in, and how much, in the base currency.
type RoundInvestor struct {
	HolderID uuid.UUID
	Amount   money.Money
}

// TopUp is shares added to the option pool with the round: before the money, counted in the pre-money price, or after.
type TopUp struct {
	Shares apd.Decimal
	Before bool
}

// RoundModel is a priced round as modelled (FR-EQ-21, design D8): the day it takes effect, its price per share or a
// pre-money valuation (divided by the shares fully diluted before it, with a top-up before the money, and cut down to
// PriceDecimals), the investors' money, a pool top-up, and the SAFEs and notes converting. It never posts.
type RoundModel struct {
	On            time.Time
	Price         *money.UnitPrice
	PreMoney      *money.Money
	PriceDecimals int
	Investors     []RoundInvestor
	TopUp         *TopUp
}

// RoundIssue is an investor's allotment in the round: RND-SHARE of their money at the price, and what is left of it.
type RoundIssue struct {
	HolderID uuid.UUID
	Shares   apd.Decimal
	Amount   money.Money
	Left     money.Money
}

// RoundHolder is a holder before and after the round: shares issued and fully diluted, each with its percentage of the
// day's totals (RND-DISPLAY), and the dilution in percentage points fully diluted (before less after, exact, shown to 2
// decimals). ProRata is what their pro-rata right entitles them to of the round's new shares [accountant to confirm
// against the side letter]: the new shares × their part fully diluted before, rounded down.
type RoundHolder struct {
	HolderID                                    uuid.UUID
	IssuedBefore, IssuedAfter                   apd.Decimal
	DilutedBefore, DilutedAfter                 apd.Decimal
	OwnershipBefore, OwnershipAfter             string
	DilutedShareBefore, DilutedShareAfter, Drop string
	ProRata                                     apd.Decimal
}

// RoundOutcome is what a modelled round gives (FR-EQ-21): its price, the pre-money and post-money valuations, each
// investor's allotment, each instrument's conversion, each holder before and after, and the day's totals.
type RoundOutcome struct {
	Price                                          money.UnitPrice
	PreMoney, PostMoney                            money.Money
	Issues                                         []RoundIssue
	Conversions                                    []Conversion
	Holders                                        []RoundHolder
	IssuedBefore, IssuedAfter                      apd.Decimal
	DilutedBefore, DilutedAfter                    apd.Decimal
	UnallocatedBefore, UnallocatedAfter, NewShares apd.Decimal
}

func roundProblem(msg, fix string) error { return &Error{CodeRound, msg, fix} }

// ModelRound works out a priced round over the cap table the day before it (before, fully diluted: DilutedAt), with
// the instruments safes converting at its price over the shares issued then (Convert, design D6). It is pure, so the
// product and the differential harness share it.
func ModelRound(m RoundModel, before CapTable, safes []Converting, enabled func(string) bool) (RoundOutcome, error) {
	out := RoundOutcome{IssuedBefore: before.Issued, DilutedBefore: before.FullyDiluted, UnallocatedBefore: before.Unallocated}
	var topUp apd.Decimal
	if m.TopUp != nil {
		topUp.Set(&m.TopUp.Shares)
	}
	var priced apd.Decimal // the shares the pre-money valuation is of
	priced.Set(&before.FullyDiluted)
	if m.TopUp != nil && m.TopUp.Before {
		if _, err := exact.Add(&priced, &priced, &topUp); err != nil {
			return out, err
		}
	}
	switch {
	case m.Price != nil:
		out.Price = *m.Price
	case m.PreMoney != nil:
		if priced.Sign() <= 0 {
			return out, roundProblem("no share is issued or set aside before the round, so a pre-money valuation prices nothing", "Enter the price per share instead")
		}
		if m.PriceDecimals < 0 || m.PriceDecimals >= len(priceExponents) {
			return out, roundProblem(fmt.Sprintf("the price's precision of %d decimals is outside 0 to %d", m.PriceDecimals, MaxPriceDecimals), "Enter 0 to 6 decimals")
		}
		var p apd.Decimal
		if _, err := exact.Quo(&p, m.PreMoney.Decimal(), &priced); err != nil {
			return out, err
		}
		down := exact.WithPrecision(60)
		down.Rounding = apd.RoundDown
		if _, err := down.Quantize(&p, &p, priceExponents[m.PriceDecimals]); err != nil {
			return out, err
		}
		price, err := money.ParseUnitPrice(text(&p), m.PreMoney.Currency(), "price")
		if err != nil || price.Sign() <= 0 {
			return out, roundProblem(fmt.Sprintf("the pre-money valuation gives a price of %s, not a price above zero", text(&p)),
				"Enter a higher valuation, or more decimals")
		}
		out.Price = price
	default:
		return out, roundProblem("the round has no price per share and no pre-money valuation", "Enter one of them")
	}
	shares := map[uuid.UUID]*apd.Decimal{}
	add := func(holder uuid.UUID, n *apd.Decimal) error {
		x, ok := shares[holder]
		if !ok {
			x = new(apd.Decimal)
			shares[holder] = x
		}
		if _, err := exact.Add(x, x, n); err != nil {
			return err
		}
		_, err := exact.Add(&out.NewShares, &out.NewShares, n)
		return err
	}
	for _, inv := range m.Investors {
		allotted, err := rounding.Share(inv.Amount, out.Price)
		if err != nil {
			return out, err
		}
		if allotted.Count.Sign() <= 0 {
			return out, roundProblem(fmt.Sprintf("%s buys no whole share at %s", inv.Amount, out.Price), "Put in at least the price of one share")
		}
		out.Issues = append(out.Issues, RoundIssue{HolderID: inv.HolderID, Shares: allotted.Count, Amount: allotted.Converted, Left: allotted.Remainder})
		if err := add(inv.HolderID, &allotted.Count); err != nil {
			return out, err
		}
	}
	if len(safes) > 0 {
		var err error
		if out.Conversions, err = Convert(Round{Price: out.Price, Before: before.Issued}, safes, enabled); err != nil {
			return out, err
		}
		for i, c := range out.Conversions {
			if err := add(safes[i].Instrument.HolderID, &c.Shares); err != nil {
				return out, err
			}
		}
	}
	for _, sum := range [][3]*apd.Decimal{{&out.IssuedAfter, &before.Issued, &out.NewShares}, {&out.DilutedAfter, &before.FullyDiluted, &out.NewShares},
		{&out.UnallocatedAfter, &before.Unallocated, &topUp}} {
		if _, err := exact.Add(sum[0], sum[1], sum[2]); err != nil {
			return out, err
		}
	}
	if _, err := exact.Add(&out.DilutedAfter, &out.DilutedAfter, &topUp); err != nil {
		return out, err
	}
	var err error
	if out.PreMoney, err = valued(&priced, out.Price); err != nil {
		return out, err
	}
	if out.PostMoney, err = valued(&out.DilutedAfter, out.Price); err != nil {
		return out, err
	}
	// Each holder before and after.
	ids := []uuid.UUID{}
	for _, h := range before.Holders {
		ids = append(ids, h.Holder)
	}
	for id := range shares {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range ids {
		h := RoundHolder{HolderID: id}
		for _, x := range before.Holders {
			if x.Holder == id {
				h.IssuedBefore.Set(&x.Issued)
				h.DilutedBefore.Set(&x.FullyDiluted)
			}
		}
		var gained apd.Decimal
		if n, ok := shares[id]; ok {
			gained.Set(n)
		}
		if _, err := exact.Add(&h.IssuedAfter, &h.IssuedBefore, &gained); err != nil {
			return out, err
		}
		if _, err := exact.Add(&h.DilutedAfter, &h.DilutedBefore, &gained); err != nil {
			return out, err
		}
		for _, pc := range []struct {
			to          *string
			part, whole *apd.Decimal
		}{{&h.OwnershipBefore, &h.IssuedBefore, &before.Issued}, {&h.OwnershipAfter, &h.IssuedAfter, &out.IssuedAfter},
			{&h.DilutedShareBefore, &h.DilutedBefore, &before.FullyDiluted}, {&h.DilutedShareAfter, &h.DilutedAfter, &out.DilutedAfter}} {
			if pc.whole.Sign() > 0 {
				if *pc.to, err = rounding.Ownership(pc.part, pc.whole); err != nil {
					return out, err
				}
			}
		}
		if before.FullyDiluted.Sign() > 0 && out.DilutedAfter.Sign() > 0 {
			var b, a, drop apd.Decimal
			if _, err := exact.Quo(&b, &h.DilutedBefore, &before.FullyDiluted); err != nil {
				return out, err
			}
			if _, err := exact.Quo(&a, &h.DilutedAfter, &out.DilutedAfter); err != nil {
				return out, err
			}
			if _, err := exact.Sub(&drop, &b, &a); err != nil {
				return out, err
			}
			if _, err := exact.Mul(&drop, &drop, apd.New(100, 0)); err != nil {
				return out, err
			}
			h.Drop = rounding.Display(&drop, 2)
			var entitled apd.Decimal
			if _, err := exact.Mul(&entitled, &out.NewShares, &b); err != nil {
				return out, err
			}
			floor := exact.WithPrecision(60)
			floor.Rounding = apd.RoundFloor
			if _, err := floor.Quantize(&h.ProRata, &entitled, 0); err != nil {
				return out, err
			}
		}
		out.Holders = append(out.Holders, h)
	}
	return out, nil
}

// valued is shares × price, a valuation, under RND-LINE.
func valued(shares *apd.Decimal, price money.UnitPrice) (money.Money, error) {
	if shares.Sign() <= 0 {
		return money.Zero(price.Currency()), nil
	}
	q, err := money.ParseQuantity(text(shares), "shares")
	if err != nil {
		return money.Money{}, err
	}
	return rounding.Line(q, price)
}
