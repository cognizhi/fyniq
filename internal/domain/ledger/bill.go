package ledger

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The bill's source type and rule version (FR-AP-02; posting rules "Posting a bill" and "Amounts of a posted bill",
// PRD-001 §10.2 and Appendix B.2). A change to the rule is a new version; an old version keeps producing what it
// produced (ENG-06).
const (
	SourceBill = "bill"
	RuleBill   = "bill@1"
)

// The system roles of the accounts the purchase rules post to (FR-COA-03, B.5).
const (
	RoleAccountsPayable = "accounts-payable"
	RoleGSTControl      = "gst-control"
	RoleFXRounding      = "fx-rounding"
	RolePrepayments     = "prepayments"
	RoleWithholdingTax  = "withholding-tax-payable" // tax withheld from payments to non-residents (FR-AP-19; rev 0.90)
)

// The bill rule's validation codes (ENG-22, ENG-23).
const (
	CodeBillTotal    = "LEDGER-21" // a bill's total is not more than zero: a vendor credit records what a supplier owes
	CodeSystemRole   = "LEDGER-22" // the chart has no account with a system role the rule posts to
	CodeRuleAccount  = "LEDGER-23" // a line on an account the rule posts to itself (AP, GST control)
	CodeLineCurrency = "LEDGER-24" // a line in another currency than its document's
	CodeSpread       = "LEDGER-28" // a spread that is not 1 to MaxSpreadMonths months of an expense line's cost (FR-GL-03)
	CodeClaimShare   = "LEDGER-29" // a claimable share that is not a percentage from 0 to 100 (FR-AP-20)
	CodeWithholding  = "LEDGER-30" // a withholding rate that is not a percentage, or nothing to withhold it from (FR-AP-19)
)

// MaxSpreadMonths is the most months a bill line's cost is spread over (FR-GL-03): ten years.
const MaxSpreadMonths = 120

// Spread is a bill line's cost spread over months (FR-GL-03, Appendix B.2): the bill debits 1210 Prepayments with it,
// and each month from From, the first day of its month, releases its ALLOC-EVEN part to the line's account
// (bill_release@1).
type Spread struct {
	From   time.Time
	Months int
}

// BillLine is one line of a bill as the rules read it: the account it costs, its net and its GST as the supplier
// states them in the bill's currency, and whether the line's input tax is claimable (FR-AP-20: from its facts and the
// organisation's registration at the bill's date). StatedBase is the GST the tax invoice states in the base currency,
// for a bill in another currency (ENG-42); nil when the invoice states none, and then the input tax is not claimed.
// Shares are the shares of a claimable line's GST that are claimed, as percentages (FR-AP-20; rev 0.89): its business
// share and, for an invoice in an agent's name, the share the company reimbursed; none claims the GST whole.
type BillLine struct {
	AccountID   uuid.UUID
	Description string
	Net         money.Money
	GST         money.Money
	Claimable   bool
	StatedBase  *money.Money
	Spread      *Spread // nil: the cost goes to the line's account on the bill's date
	Shares      []string
}

// claim is what a claimable line claims of stated, its GST as the input tax is reported (the line's GST, or the GST
// stated in the base currency for a bill in another currency): the input tax, under RND-GST-SHARE when the line has
// shares, and the GST in the bill's currency that stays in the cost, RND-GST-SHARE's remainder of the line's own GST.
func (l BillLine) claim(stated money.Money) (claim, kept money.Money, err error) {
	if len(l.Shares) == 0 {
		return stated, money.Zero(l.GST.Currency()), nil
	}
	if claim, err = rounding.GSTShare(stated, l.Shares...); err != nil {
		return money.Money{}, money.Money{}, err
	}
	part, err := rounding.GSTShare(l.GST, l.Shares...)
	if err != nil {
		return money.Money{}, money.Money{}, err
	}
	kept, err = l.GST.Sub(part)
	return claim, kept, err
}

// ClaimedCost is a claimable line's cost in the bill's currency: its net, and the GST its shares leave unclaimed.
func (l BillLine) ClaimedCost() (money.Money, error) {
	_, kept, err := l.claim(l.GST)
	if err != nil {
		return money.Money{}, err
	}
	return l.Net.Add(kept)
}

// Bill is one version of a supplier's bill as the rules read it.
type Bill struct {
	ID       uuid.UUID
	Version  int
	Date     time.Time // the bill's date: its tax point
	Memo     string    // the supplier and the supplier's number
	Currency money.Currency
	Rate     *money.Rate // into the base currency; nil for a bill in the base currency
	Lines    []BillLine
	// Withholding is the tax withheld from a non-resident supplier's bill (FR-AP-19; rev 0.90); nil when none is.
	Withholding *Withholding
}

// Withholding is withholding tax on a bill, at the rate the tax agent advised, as a percentage ("17").
type Withholding struct {
	RatePercent string
}

// BillIdentity is the posting identity of a bill version under a rule version (§11.4).
func BillIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceBill, id, version, rule)
}

// byRole is the account of s with a system role, if the chart has one.
func (s Settings) byRole(role string) (Account, bool) {
	for _, a := range s.Accounts {
		if a.Role == role {
			return a, true
		}
	}
	return Account{}, false
}

// PostBill turns one version of a bill into its entry (bill@1):
//   - Dr each line's account with its net, at the bill's rate under RND-FX; a line whose input tax is not claimable
//     keeps its GST in the cost, net plus GST (code BL, a private purchase, no valid tax invoice, or an organisation
//     not GST-registered at the bill's date: FR-AP-20), and a line claimable in part keeps the GST its shares leave
//     (RND-GST-SHARE; rev 0.89: no entry before it had a share, so every earlier entry replays as it posted);
//   - but Dr 1210 Prepayments, in the base currency, with the base amount of the cost of a line spread over months
//     (FR-GL-03, Appendix B.2; rev 0.83): a prepayment is not monetary, so it keeps its base amount and is never
//     revalued, and its releases (bill_release@1) move that amount to the line's account month by month. Only an
//     expense line's cost is spread (B.1: P&L categories only);
//   - Dr 2200 GST control with the claimable GST: as stated in the base currency for a bill in another currency
//     (ENG-42), else the line's GST;
//   - Cr 2010 Accounts payable with the total, net plus GST, at the bill's rate; but for a bill that withholds tax
//     (FR-AP-19; rev 0.90), Cr 2330 Withholding tax payable with RND-WHT's tax on the lines' net in the base currency,
//     and 2010 the total less what is withheld of the supplier's own amount: the same tax for a bill in the base
//     currency, RND-WHT's part of the net in the bill's currency for one in another;
//   - and, for a bill in another currency, what makes the entry balance in the base currency to 7030: the stated GST
//     against the GST at the book rate, and any cent RND-FX leaves between the lines and the total.
//
// Every rule is checked first and every failure returned together (ENG-22): the bill is rejected whole.
func PostBill(b Bill, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if b.Date.IsZero() {
		add(CodeNoDate, "date", "the bill has no date", "Enter the date on the supplier's invoice")
	}
	if len(b.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(b.Memo), MaxMemo), "Shorten the memo")
	}
	if len(b.Lines) == 0 {
		add(CodeTooFewLines, "lines", "the bill has no lines", "Add a line for what the bill is for")
	}
	rate := money.Identity(s.Base)
	foreign := b.Currency != s.Base
	rated := true // the bill's rate can convert its amounts: without it no line has a base amount to check
	switch {
	case !foreign && b.Rate != nil:
		add(CodeRateOnBase, "fx_rate", fmt.Sprintf("the bill is in the base currency %s, so it takes no exchange rate", s.Base),
			"Remove the rate: Fyniq records rate 1 for base-currency amounts")
	case foreign && b.Rate == nil:
		add(CodeMissingRate, "fx_rate", fmt.Sprintf("the bill is in %s, so it needs a rate into %s", b.Currency, s.Base),
			"Enter the exchange rate, or add a rate for the bill's date in Accounting › Currencies")
		rated = false
	case foreign && (b.Rate.From != b.Currency || b.Rate.To != s.Base):
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("the rate converts %s into %s, but the bill needs %s into %s", b.Rate.From, b.Rate.To, b.Currency, s.Base),
			"Use a rate from the bill's currency into the base currency")
		rated = false
	case foreign:
		rate = *b.Rate
	}
	ap, hasAP := s.byRole(RoleAccountsPayable)
	if !hasAP {
		add(CodeSystemRole, "lines", "the chart has no accounts payable account", "Load the standard chart's 2010 Accounts payable, or give a liability account the role")
	}
	gstControl, hasGST := s.byRole(RoleGSTControl)
	prepay, hasPrepay := s.byRole(RolePrepayments)
	spread := false
	spreads := slices.ContainsFunc(b.Lines, func(l BillLine) bool { return l.Spread != nil })

	var lines []Line
	total := money.Zero(b.Currency)
	gstBase := money.Zero(s.Base)
	baseTotal := money.Zero(s.Base)
	claimed := false
	for i, l := range b.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		acc, ok := s.Accounts[l.AccountID]
		switch {
		case !ok:
			add(CodeUnknownAccount, f("account"), "the account does not exist in this organisation", "Choose an expense or asset account from the chart")
		case acc.Group:
			add(CodeGroupAccount, f("account"), fmt.Sprintf("account %s is a group heading", acc.Code), "Choose an account under it")
		case !acc.Active:
			add(CodeInactiveAccount, f("account"), fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it or choose another account")
		case acc.ShareCapital:
			add(CodeShareCapital, f("account"), fmt.Sprintf("account %s is share capital, which changes only through the register of share issues", acc.Code),
				"Choose the account the bill is for")
		case acc.Role == RoleAccountsPayable || acc.Role == RoleGSTControl:
			add(CodeRuleAccount, f("account"), fmt.Sprintf("account %s is the one the bill posts its total or its GST to", acc.Code),
				"Choose the account the bill is for: the rule posts to it by itself")
		case acc.Role == RolePrepayments && hasPrepay && spreads && l.Spread == nil:
			// Its line on 1210 and the spread's would be one account's, and the releases could not tell them apart.
			add(CodeSpread, f("account"), fmt.Sprintf("account %s holds the bill's spread costs", acc.Code),
				"Choose the account the cost is for, and spread that line")
		}
		if len(l.Description) > MaxDescription {
			add(CodeTooLong, f("description"), fmt.Sprintf("the description has %d characters; the most is %d", len(l.Description), MaxDescription), "Shorten it")
		}
		if x := l.Spread; x != nil {
			spread = true
			switch {
			case x.Months < 1 || x.Months > MaxSpreadMonths:
				add(CodeSpread, f("spread_months"), fmt.Sprintf("the line is spread over %d months", x.Months), fmt.Sprintf("Spread it over 1 to %d months", MaxSpreadMonths))
			case x.From.IsZero():
				add(CodeSpread, f("spread_from"), "the spread has no first month", "Enter the month the service starts")
			case ok && acc.Type != "expense":
				add(CodeSpread, f("spread_months"), fmt.Sprintf("account %s is not an expense account, so its cost is not spread", acc.Code),
					"Spread only a cost that is used up over months, on an expense account; leave an asset's cost unspread")
			}
		}
		if l.Net.Currency() != b.Currency || l.GST.Currency() != b.Currency {
			add(CodeLineCurrency, f("net"), fmt.Sprintf("the line is in %s and its GST in %s; the bill is in %s", l.Net.Currency(), l.GST.Currency(), b.Currency),
				"Enter every amount of the bill in its currency")
			continue
		}
		if l.Net.IsZero() && l.GST.IsZero() {
			add(CodeZeroLine, f("net"), "the line's amount is zero", "Enter its amount, or remove the line")
			continue
		}
		gross, err := l.Net.Add(l.GST)
		if err != nil {
			add(CodeBaseRange, f("net"), err.Error(), "Check the amount")
			continue
		}
		if total, err = total.Add(gross); err != nil {
			add(CodeBaseRange, f("net"), err.Error(), "Check the amount")
			continue
		}
		cost := gross
		if l.Claimable && !l.GST.IsZero() && (!foreign || l.StatedBase != nil) {
			stated := l.GST
			if foreign {
				stated = *l.StatedBase
				if stated.Currency() != s.Base {
					add(CodeLineCurrency, f("stated_gst"), fmt.Sprintf("the stated GST is in %s, not %s", stated.Currency(), s.Base),
						fmt.Sprintf("Enter the GST in %s as the tax invoice states it", s.Base))
					continue
				}
			}
			claim, kept, err := l.claim(stated)
			if err != nil {
				add(CodeClaimShare, f("business_share"), err.Error(), "Enter each share as a percentage from 0 to 100, with at most two decimals")
				continue
			}
			if cost, err = l.Net.Add(kept); err != nil {
				add(CodeBaseRange, f("gst"), err.Error(), "Check the GST")
				continue
			}
			if gstBase, err = gstBase.Add(claim); err != nil {
				add(CodeBaseRange, f("gst"), err.Error(), "Check the GST")
				continue
			}
			claimed = claimed || !claim.IsZero()
		}
		if cost.Sign() <= 0 && l.Spread != nil {
			add(CodeSpread, f("spread_months"), fmt.Sprintf("the line's cost is %s %s, so there is nothing to spread", cost, b.Currency), "Spread a line with a cost, or remove its spread")
		}
		if cost.IsZero() || !rated {
			continue // a line of claimable GST alone posts its GST to 2200; without a rate nothing converts
		}
		base, err := rounding.FX(cost, rate)
		if err != nil {
			add(CodeBaseRange, f("net"), fmt.Sprintf("at this rate the base amount does not fit: %v", err), "Check the amount and the rate")
			continue
		}
		baseTotal, _ = baseTotal.Add(base)
		if l.Spread != nil {
			lines = append(lines, Line{AccountID: prepay.ID, Amount: base, Rate: money.Identity(s.Base), Base: base, Description: l.Description})
			continue
		}
		lines = append(lines, Line{AccountID: l.AccountID, Amount: cost, Rate: rate, Base: base, Description: l.Description})
	}
	if spread && !hasPrepay {
		add(CodeSystemRole, "lines", "the chart has no prepayments account for the spread cost", "Load the standard chart's 1210 Prepayments, or give an asset account the role")
	}
	if claimed && !hasGST {
		add(CodeSystemRole, "lines", "the chart has no GST control account for the input tax", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	if len(b.Lines) > 0 && total.Sign() <= 0 && total.Currency() == b.Currency {
		add(CodeBillTotal, "lines", fmt.Sprintf("the bill's total is %s %s", total, b.Currency),
			"A bill's total is more than zero; record what a supplier owes the company as a vendor credit")
	}
	// Withholding tax (FR-AP-19): on the lines' net, before GST.
	var whtBase, withheld money.Money
	wht, hasWHT := s.byRole(RoleWithholdingTax)
	if w := b.Withholding; w != nil && len(probs) == 0 {
		gross := money.Zero(b.Currency)
		for _, l := range b.Lines {
			gross, _ = gross.Add(l.Net)
		}
		grossBase, err := rounding.FX(gross, rate)
		switch {
		case !hasWHT:
			add(CodeSystemRole, "lines", "the chart has no withholding tax payable account for the tax withheld",
				"Load the standard chart's 2330 Withholding tax payable, or give a liability account the role")
		case gross.Sign() <= 0:
			add(CodeWithholding, "lines", fmt.Sprintf("the bill's net is %s %s, so there is nothing to withhold tax from", gross, b.Currency),
				"Withhold tax only from a bill with a net above zero")
		case err != nil:
			add(CodeBaseRange, "lines", fmt.Sprintf("at this rate the net does not fit: %v", err), "Check the amounts and the rate")
		default:
			if whtBase, err = rounding.WHT(grossBase, w.RatePercent); err != nil {
				add(CodeWithholding, "withholding.rate_percent", err.Error(), "Record the rate the tax agent advised as a percentage, such as 17")
				break
			}
			withheld = whtBase
			if foreign {
				if withheld, err = rounding.Withheld(gross, w.RatePercent); err != nil {
					add(CodeWithholding, "withholding.rate_percent", err.Error(), "Record the rate the tax agent advised as a percentage, such as 17")
				}
			}
		}
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	if !gstBase.IsZero() {
		lines = append(lines, Line{AccountID: gstControl.ID, Amount: gstBase, Rate: money.Identity(s.Base), Base: gstBase, Description: "Input tax"})
		baseTotal, _ = baseTotal.Add(gstBase)
	}
	owed := total
	if !whtBase.IsZero() {
		lines = append(lines, Line{AccountID: wht.ID, Amount: whtBase.Neg(), Rate: money.Identity(s.Base), Base: whtBase.Neg(),
			Description: fmt.Sprintf("Withholding tax at %s%%", b.Withholding.RatePercent)})
		baseTotal, _ = baseTotal.Add(whtBase.Neg())
	}
	if !withheld.IsZero() {
		owed, _ = total.Sub(withheld)
	}
	apBase, err := rounding.FX(owed.Neg(), rate)
	if err != nil {
		return Entry{}, Problems{{CodeBaseRange, "lines", fmt.Sprintf("at this rate the total does not fit: %v", err), "Check the amounts and the rate"}}
	}
	lines = append(lines, Line{AccountID: ap.ID, Amount: owed.Neg(), Rate: rate, Base: apBase, Description: b.Memo})
	baseTotal, _ = baseTotal.Add(apBase)
	if !baseTotal.IsZero() { // only for a bill in another currency: an SGD bill's lines are their own base amounts
		fxAcc, ok := s.byRole(RoleFXRounding)
		if !ok {
			return Entry{}, Problems{{CodeSystemRole, "lines", "the chart has no FX and rounding account for the difference at the book rate",
				"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
		}
		diff := baseTotal.Neg()
		lines = append(lines, Line{AccountID: fxAcc.ID, Amount: diff, Rate: money.Identity(s.Base), Base: diff, Description: "GST as stated against the book rate"})
	}
	y, m, d := b.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceBill, ID: b.ID.String(), Version: b.Version},
		RuleVersion:     RuleBill,
		PostingIdentity: BillIdentity(b.ID, b.Version, RuleBill),
		Memo:            b.Memo,
		Lines:           lines,
	}, nil
}

// BillRule turns one version of a bill into its entry under one rule version.
type BillRule func(b Bill, s Settings) (Entry, error)

// BillRules are every bill rule version the engine has posted with, by name: like Rules, a version is never removed or
// changed while an entry names it.
var BillRules = map[string]BillRule{
	RuleBill: PostBill,
}

// ReplayBill re-runs the rule version that posted a bill's entry over the same version (ENG-06).
func ReplayBill(rule string, b Bill, s Settings) (Entry, error) {
	f, ok := BillRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(b, s)
}
