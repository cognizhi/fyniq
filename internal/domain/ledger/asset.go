package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The fixed assets' source types and rule versions (FR-FA-02, FR-FA-03; posting rules "Depreciation" and "Asset
// disposal", Appendix B.4): a month's depreciation of a registered asset at a version, and a disposal.
const (
	SourceAssetDepreciation = "asset_depreciation"
	RuleAssetDepreciation   = "asset_depreciation@1"
)

// The accounts' system roles depreciation posts between (B.5): 6610 Depreciation and 1590 Accumulated depreciation.
const (
	RoleDepreciation            = "depreciation"
	RoleAccumulatedDepreciation = "accumulated-depreciation"
)

// CodeDepreciation is a depreciation the rule refuses: one not in the base currency, of nothing, or less than nothing.
const CodeDepreciation = "LEDGER-39"

// DepreciableAsset is what an asset version's schedule reads (p1-fixed-assets design D2): the asset and its version, the
// words its entries' memos name it by, its cost, residual value and the accumulated depreciation it carries up to From,
// the first month it posts, and End, the last month of its life (start + life − 1, B.4). All amounts are in the base
// currency, months on their first day.
type DepreciableAsset struct {
	ID       uuid.UUID
	Version  int
	Memo     string
	Cost     money.Money
	Residual money.Money
	Carried  money.Money
	From     time.Time
	End      time.Time
}

// Depreciation is one month's depreciation of an asset version: the asset and its version, the month (its first day),
// which part of how many it is, the date it posts on (the month's last day), the memo, and the amount in the base
// currency.
type Depreciation struct {
	AssetID  uuid.UUID
	Version  int
	Month    time.Time
	Part, Of int
	Date     time.Time
	Memo     string
	Amount   money.Money
}

// DepreciationIdentity is the posting identity of an asset version's depreciation for a month under a rule version
// (§11.4): one entry per asset, version and month, ever.
func DepreciationIdentity(id uuid.UUID, version int, month time.Time, rule string) string {
	return fmt.Sprintf("%s:%s:v%d:%s:%s", SourceAssetDepreciation, id, version, month.Format("2006-01"), rule)
}

// Identity is the depreciation's posting identity under rule.
func (d Depreciation) Identity(rule string) string { return DepreciationIdentity(d.AssetID, d.Version, d.Month, rule) }

// DepreciationSchedule is an asset version's months of depreciation (FR-FA-02; B.4): what is left to depreciate, its
// cost less its residual value less what it carries, split under ALLOC-EVEN over the months from From to End, each
// dated on its month's last day. A version with nothing left, or no month left, has none; one carrying more than its
// cost less residual value is an error, which a version never saves.
func DepreciationSchedule(a DepreciableAsset) ([]Depreciation, error) {
	left, err := a.Cost.Sub(a.Residual)
	if err == nil {
		left, err = left.Sub(a.Carried)
	}
	if err != nil {
		return nil, err
	}
	from, end := firstOfMonth(a.From), firstOfMonth(a.End)
	if left.Sign() < 0 {
		return nil, fmt.Errorf("asset %s version %d carries %s, more than its cost less its residual value", a.ID, a.Version, a.Carried)
	}
	if left.IsZero() || end.Before(from) {
		return nil, nil
	}
	n := 0
	for m := from; !m.After(end); m = m.AddDate(0, 1, 0) {
		n++
	}
	parts, err := rounding.AllocEven(left, n)
	if err != nil {
		return nil, err
	}
	out := make([]Depreciation, n)
	for i, part := range parts {
		m := from.AddDate(0, i, 0)
		out[i] = Depreciation{AssetID: a.ID, Version: a.Version, Month: m, Part: i + 1, Of: n, Date: m.AddDate(0, 1, -1), Memo: a.Memo, Amount: part}
	}
	return out, nil
}

// PostDepreciation turns one month's depreciation of an asset into its entry (asset_depreciation@1; posting rule
// "Depreciation", B.4), in the base currency on the month's last day: Dr 6610 Depreciation and Cr 1590 Accumulated
// depreciation with the month's part (ENG-05: posted by the scheduler as a real entry, up to the current period only).
// Every rule is checked first and every failure returned together (ENG-22).
func PostDepreciation(d Depreciation, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if d.Date.IsZero() || d.Month.IsZero() {
		add(CodeNoDate, "date", "the depreciation has no date", "Depreciate a month of a registered asset's schedule")
	}
	switch {
	case d.Amount.Currency() != s.Base:
		add(CodeDepreciation, "amount", fmt.Sprintf("the depreciation is in %s, not the base currency %s", d.Amount.Currency(), s.Base),
			"Depreciate the asset's cost in the base currency")
	case d.Amount.Sign() <= 0:
		add(CodeDepreciation, "amount", fmt.Sprintf("this month's depreciation is %s %s", d.Amount, d.Amount.Currency()),
			"Nothing is depreciated in a month whose part is nothing")
	}
	memo := fmt.Sprintf("%s: depreciation for %s (%d of %d)", d.Memo, d.Month.Format("January 2006"), d.Part, d.Of)
	if len(memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(memo), MaxMemo), "Shorten the asset's name")
	}
	expense, ok := s.byRole(RoleDepreciation)
	if !ok {
		add(CodeSystemRole, "lines", "the chart has no depreciation account", "Load the standard chart's 6610 Depreciation, or give an expense account the role")
	}
	accumulated, ok := s.byRole(RoleAccumulatedDepreciation)
	if !ok {
		add(CodeSystemRole, "lines", "the chart has no accumulated depreciation account",
			"Load the standard chart's 1590 Accumulated depreciation, or give an asset account the role")
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	id := money.Identity(s.Base)
	y, m, day := d.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, day, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceAssetDepreciation, ID: d.AssetID.String(), Version: d.Version},
		RuleVersion:     RuleAssetDepreciation,
		PostingIdentity: d.Identity(RuleAssetDepreciation),
		Memo:            memo,
		Lines: []Line{
			{AccountID: expense.ID, Amount: d.Amount, Rate: id, Base: d.Amount, Description: d.Memo},
			{AccountID: accumulated.ID, Amount: d.Amount.Neg(), Rate: id, Base: d.Amount.Neg(), Description: d.Memo},
		},
	}, nil
}

// DepreciationRule turns one month's depreciation into its entry under one rule version.
type DepreciationRule func(d Depreciation, s Settings) (Entry, error)

// DepreciationRules are every depreciation rule version the engine has posted with, by name: like Rules, a version is
// never removed or changed while an entry names it.
var DepreciationRules = map[string]DepreciationRule{RuleAssetDepreciation: PostDepreciation}

// ReplayDepreciation re-runs the rule version that posted a depreciation's entry over the same month (ENG-06).
func ReplayDepreciation(rule string, d Depreciation, s Settings) (Entry, error) {
	f, ok := DepreciationRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(d, s)
}

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

// The disposal's source type and rule version (FR-FA-03; posting rule "Asset disposal", §10.2; rev 0.149).
const (
	SourceAssetDisposal = "asset_disposal"
	RuleAssetDisposal   = "asset_disposal@1"
)

// CodeDisposal is a disposal the rule refuses: amounts not in the base currency or out of range, or accounts it cannot
// post to.
const CodeDisposal = "LEDGER-40"

// AssetDisposalIdentity is the posting identity of a disposal version under a rule version (§11.4).
func AssetDisposalIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceAssetDisposal, id, version, rule)
}

// AssetDisposal is one disposal as the rule reads it (design D4): the disposal and its version, its date and memo, the
// asset account and the share of the asset's cost it takes off it, that share's accumulated depreciation through the
// month before, and its proceeds: money received into a money account (Gross, the GST in it at GSTRate, "" when it
// bears none), or an issued invoice's line on the clearing account (InvoiceNet), or none for a write-off; and the
// account the gain or loss goes to. All amounts are in the base currency.
type AssetDisposal struct {
	ID                uuid.UUID
	Version           int
	Date              time.Time
	Memo              string
	AssetAccountID    uuid.UUID
	Cost              money.Money
	Accumulated       money.Money
	MoneyAccountID    *uuid.UUID
	Gross             money.Money
	GSTRate           string
	ClearingAccountID *uuid.UUID
	InvoiceNet        money.Money
	DisposalAccountID uuid.UUID
}

// PostAssetDisposal turns a disposal into its entry (asset_disposal@1; §10.2 "Asset disposal"), in the base currency on
// its date:
//   - Dr 1590 Accumulated depreciation with the share's accumulated depreciation;
//   - Dr the money account with the money received, and Cr 2200 GST control with its output GST (RND-GST-INCL); or Dr
//     the clearing account with the invoice line's net, the invoice having charged its GST;
//   - Cr the asset account with the share of the cost;
//   - the difference to the disposal account: Dr a loss, Cr a gain.
//
// Every rule is checked first and every failure returned together (ENG-22).
func PostAssetDisposal(d AssetDisposal, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if d.Date.IsZero() {
		add(CodeNoDate, "date", "the disposal has no date", "Enter the day the asset was sold or written off")
	}
	if len(d.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(d.Memo), MaxMemo), "Shorten the asset's name")
	}
	account := func(field string, id uuid.UUID, money bool, what, fix string) {
		a, ok := s.Accounts[id]
		switch {
		case !ok:
			add(CodeUnknownAccount, field, "the "+what+" does not exist in this organisation", fix)
		case a.Group:
			add(CodeGroupAccount, field, fmt.Sprintf("account %s is a group heading", a.Code), "Choose an account under it")
		case !a.Active:
			add(CodeInactiveAccount, field, fmt.Sprintf("account %s is inactive", a.Code), "Reactivate it, or choose another account")
		case money && (!a.Money || a.Currency != "" && a.Currency != s.Base.Code):
			add(CodeNotMoneyAccount, field, fmt.Sprintf("account %s is not a money account in %s", a.Code, s.Base), "Choose the bank, cash or wallet account the money came into")
		}
	}
	account("asset_account", d.AssetAccountID, false, "asset's account", "Register the asset on an asset account")
	account("disposal_account", d.DisposalAccountID, false, "disposal account", "Set the disposal account (assets.disposal_account) to an account of the chart, such as 7050")
	amount := func(field string, m money.Money, positive bool) bool {
		switch {
		case m.Currency() != s.Base:
			add(CodeDisposal, field, fmt.Sprintf("the %s is in %s, not the base currency %s", field, m.Currency(), s.Base), "Enter it in the base currency")
		case positive && m.Sign() <= 0, !positive && m.Sign() < 0:
			add(CodeDisposal, field, fmt.Sprintf("the %s is %s %s", field, m, m.Currency()), "Enter an amount above zero")
		default:
			return true
		}
		return false
	}
	costOK := amount("cost", d.Cost, true)
	if amount("accumulated", d.Accumulated, false) && costOK {
		if over, _ := d.Accumulated.Cmp(d.Cost); over > 0 {
			add(CodeDisposal, "accumulated", fmt.Sprintf("the accumulated depreciation %s is more than the cost %s", d.Accumulated, d.Cost),
				"Dispose of an asset with its own accumulated depreciation")
		}
	}
	accumulated, hasAccumulated := s.byRole(RoleAccumulatedDepreciation)
	if !hasAccumulated && d.Accumulated.Sign() > 0 {
		add(CodeSystemRole, "lines", "the chart has no accumulated depreciation account", "Load the standard chart's 1590 Accumulated depreciation, or give an asset account the role")
	}
	gst := money.Zero(s.Base)
	gstControl, hasGST := s.byRole(RoleGSTControl)
	switch {
	case d.MoneyAccountID != nil && d.ClearingAccountID != nil:
		add(CodeDisposal, "proceeds", "the disposal has both money received and an invoice", "Record the proceeds once: as money received, or on the invoice")
	case d.MoneyAccountID != nil:
		account("money_account_id", *d.MoneyAccountID, true, "money account", "Choose the bank, cash or wallet account the money came into")
		if amount("proceeds", d.Gross, true) && d.GSTRate != "" {
			var err error
			if gst, err = rounding.GSTIncl(d.Gross, d.GSTRate); err != nil {
				add(CodeGSTRate, "gst_code", err.Error(), "Choose the GST code again: its rate comes from the sg-gst pack")
			}
			if !gst.IsZero() && !hasGST {
				add(CodeSystemRole, "lines", "the chart has no GST control account for the output tax", "Load the standard chart's 2200 GST control, or give a liability account the role")
			}
		}
	case d.ClearingAccountID != nil:
		account("clearing_account", *d.ClearingAccountID, false, "disposal clearing account", "Set the disposal clearing account (assets.disposal_clearing_account) to an account of the chart")
		amount("proceeds", d.InvoiceNet, true)
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	id := money.Identity(s.Base)
	line := func(acc uuid.UUID, m money.Money, desc string) Line {
		return Line{AccountID: acc, Amount: m, Rate: id, Base: m, Description: desc}
	}
	var lines []Line
	diff := d.Cost.Neg() // what the disposal account takes: debits positive
	if d.Accumulated.Sign() > 0 {
		lines = append(lines, line(accumulated.ID, d.Accumulated, "Accumulated depreciation"))
		diff, _ = diff.Add(d.Accumulated)
	}
	switch {
	case d.MoneyAccountID != nil:
		lines = append(lines, line(*d.MoneyAccountID, d.Gross, "Proceeds"))
		net, _ := d.Gross.Sub(gst)
		diff, _ = diff.Add(net)
		if !gst.IsZero() {
			lines = append(lines, line(gstControl.ID, gst.Neg(), "Output tax"))
		}
	case d.ClearingAccountID != nil:
		lines = append(lines, line(*d.ClearingAccountID, d.InvoiceNet, "Proceeds invoiced"))
		diff, _ = diff.Add(d.InvoiceNet)
	}
	lines = append(lines, line(d.AssetAccountID, d.Cost.Neg(), "Cost"))
	switch diff.Sign() {
	case 1:
		lines = append(lines, line(d.DisposalAccountID, diff.Neg(), "Gain on disposal"))
	case -1:
		lines = append(lines, line(d.DisposalAccountID, diff.Neg(), "Loss on disposal"))
	}
	y, m, day := d.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, day, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceAssetDisposal, ID: d.ID.String(), Version: d.Version},
		RuleVersion:     RuleAssetDisposal,
		PostingIdentity: AssetDisposalIdentity(d.ID, d.Version, RuleAssetDisposal),
		Memo:            d.Memo,
		Lines:           lines,
	}, nil
}

// AssetDisposalRules are every disposal rule version the engine has posted with, by name.
var AssetDisposalRules = map[string]func(d AssetDisposal, s Settings) (Entry, error){RuleAssetDisposal: PostAssetDisposal}

// ReplayAssetDisposal re-runs the rule version that posted a disposal's entry over the same disposal (ENG-06).
func ReplayAssetDisposal(rule string, d AssetDisposal, s Settings) (Entry, error) {
	f, ok := AssetDisposalRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(d, s)
}
