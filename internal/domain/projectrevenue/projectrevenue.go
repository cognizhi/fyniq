// Package projectrevenue is revenue over time for a fixed-fee project (PRD-001 FR-PRJ-11; p2-customer-portal-subscriptions task 4.1): the progress of the work
// by cost-to-cost, the revenue it has earned, and the contract asset or liability that earned revenue leaves against what was invoiced. It only works out
// figures from what the ledger and the milestones already hold; the journal that books them is proposed for a person to approve.
//
// A milestone invoice credits revenue as it posts, so the monthly journal is a true-up: it moves the revenue the ledger holds for the project to what the
// progress has earned, and the difference sits on the balance sheet, as a contract asset when more is earned than invoiced and as a contract liability when
// more is invoiced than earned. The method rests on SFRS(I) 15 ¶35 and the input method of B18 to B19 `[accountant to confirm]`.
package projectrevenue

import (
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Position is a project's revenue position after this month's journal.
type Position struct {
	// Progress is cost to date over the estimate as a percentage (RND-PERCENT), at most 100.0.
	Progress string
	// Overrun says the cost to date has reached the estimate: the whole price is earned, and the estimate is revisited.
	Overrun bool
	// Earned is the revenue to recognise to date: RND-RECOGNISE of the price by progress.
	Earned money.Money
	// Adjustment is the revenue this month's journal books: Earned less the revenue the ledger holds for the project. Negative when invoiced ahead of progress.
	Adjustment money.Money
	// Asset and Liability are the contract asset and the contract liability after the journal: at most one is above nothing.
	Asset, Liability money.Money
	// AssetChange and LiabilityChange are what the journal moves them by from what the ledger holds now (a debit to the asset and a credit to the liability when
	// positive). AssetChange less LiabilityChange is Adjustment.
	AssetChange, LiabilityChange money.Money
}

// Work works out the position of a project with a fixed fee of price, an estimated total cost, a cost to date, the amount invoiced to date (net of GST) and the
// revenue the ledger holds for the project to date (the invoiced revenue and the earlier journals' adjustments), all in one currency.
func Work(price, estimate, cost, billed, held money.Money) (Position, error) {
	earned, err := rounding.Recognised(price, cost, estimate)
	if err != nil {
		return Position{}, err
	}
	cur := price.Currency()
	zero := money.Zero(cur)
	overrun := false
	if c, _ := cost.Cmp(estimate); c >= 0 {
		overrun = true
	}
	progress := "100.0"
	if !overrun {
		if progress, err = rounding.Percent(cost, estimate); err != nil {
			return Position{}, err
		}
	}
	// After the journal the revenue held is Earned, so the net contract position is Earned less what was invoiced; before it, what the ledger holds less invoiced.
	after, err := earned.Sub(billed)
	if err != nil {
		return Position{}, err
	}
	before, err := held.Sub(billed)
	if err != nil {
		return Position{}, err
	}
	split := func(net money.Money) (asset, liability money.Money) {
		if net.Sign() > 0 {
			return net, zero
		}
		return zero, net.Neg()
	}
	assetAfter, liabAfter := split(after)
	assetBefore, liabBefore := split(before)
	out := Position{Progress: progress, Overrun: overrun, Earned: earned, Asset: assetAfter, Liability: liabAfter}
	if out.Adjustment, err = earned.Sub(held); err != nil {
		return Position{}, err
	}
	if out.AssetChange, err = assetAfter.Sub(assetBefore); err != nil {
		return Position{}, err
	}
	if out.LiabilityChange, err = liabAfter.Sub(liabBefore); err != nil {
		return Position{}, err
	}
	return out, nil
}
