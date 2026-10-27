package ledger

import (
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The fixed assets' source types and rule versions (FR-FA-02, FR-FA-03; posting rules "Depreciation" and "Asset
// disposal", Appendix B.4): a month's depreciation of a registered asset at a version, and a disposal.
const (
	SourceAssetDepreciation = "asset_depreciation"
	RuleAssetDepreciation   = "asset_depreciation@1"
)

// BillLineCost is the line of a bill's entry e (bill@1) that took line i's cost (from 1) on the line's own account: what
// an asset registered from the line costs, in the base currency (FR-FA-01; p1-fixed-assets design D1). bill@1 writes one
// line for each bill line with a cost, in the bill's order, before its GST and payable lines: the line's net and the GST
// it does not claim, at the bill's rate (RND-FX). ok is false for a line that put no cost of its own on its account: one
// of claimable GST alone, or a spread line, whose cost is on 1210 Prepayments.
func BillLineCost(b Bill, e Entry, s Settings, line int) (Line, bool) {
	foreign := b.Currency != s.Base
	k := 0
	for i, l := range b.Lines {
		cost, err := l.Net.Add(l.GST)
		if err != nil {
			return Line{}, false
		}
		if l.Claimable && !l.GST.IsZero() && (!foreign || l.StatedBase != nil) {
			if cost, err = l.ClaimedCost(); err != nil {
				return Line{}, false
			}
		}
		if cost.IsZero() {
			if i == line-1 {
				return Line{}, false
			}
			continue
		}
		if i == line-1 {
			if l.Spread != nil || k >= len(e.Lines) || e.Lines[k].AccountID != l.AccountID {
				return Line{}, false
			}
			return e.Lines[k], true
		}
		k++
	}
	return Line{}, false
}

// BankPartCost is the line of a categorised bank line's entry e (bank_transaction@1) that took part i (from 1) on the
// part's own account: what an asset registered from the part costs, in the base currency (FR-FA-01). bank_transaction@1
// writes the money account's line first, then one line for each part whose amount net of its GST is not zero, in the
// parts' order. ok is false for a part that put nothing on its own account: a spread part, whose amount is on 1210 or
// 2400.
func BankPartCost(t BankTransaction, e Entry, s Settings, part int) (Line, bool) {
	rate := money.Identity(s.Base)
	if t.Rate != nil {
		rate = *t.Rate
	}
	k := 1
	for i, p := range t.Parts {
		base, err := rounding.FX(p.Amount, rate)
		if err != nil {
			return Line{}, false
		}
		net := base
		if p.GSTRate != "" {
			gst, err := rounding.GSTIncl(base, p.GSTRate)
			if err != nil {
				return Line{}, false
			}
			net, _ = base.Sub(gst)
		}
		if net.IsZero() {
			if i == part-1 {
				return Line{}, false
			}
			continue
		}
		if i == part-1 {
			if p.Months != 0 || k >= len(e.Lines) || e.Lines[k].AccountID != p.AccountID {
				return Line{}, false
			}
			return e.Lines[k], true
		}
		k++
	}
	return Line{}, false
}
