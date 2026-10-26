package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The expense claim's source type and rule version (FR-AP-07, FR-AP-16, FR-AP-17; posting rules "Posting an expense
// claim", "Mileage claim" and "Cash advance to an employee", PRD-001 §10.2; p1-expense-claims design D2). A change to
// the rule is a new version; an old version keeps producing what it produced (ENG-06).
const (
	SourceExpenseClaim = "expense_claim"
	RuleExpenseClaim   = "expense_claim@1"
)

// RoleExpenseClaimsPayable is the account what the company owes its claimants is credited to (B.5: 2150).
const RoleExpenseClaimsPayable = "expense-claims-payable"

// The expense claim rule's validation codes (ENG-22, ENG-23).
const (
	CodeClaimLine = "LEDGER-36" // a claim line's amounts do not fit: not more than zero, GST below zero or above the amount, GST on mileage
	CodeClaimPaid = "LEDGER-37" // what a claim settles of an advance, or the base amount paid for a line, does not fit
)

// ClaimLine is one line of an expense claim as the rule reads it: the account it costs, what the receipt says in its
// currency (the amount including GST, and the GST it states), whether that GST is claimable as input tax (FR-AP-20,
// decided by package claim), the rate into the base currency for a line in another currency (nil in the base), the GST
// the receipt states in the base currency for such a line (nil: its input tax is not claimed, ENG-42), and what the
// claimant paid for it in the base currency, as their card statement shows (nil: the amount at the rate). A mileage
// line carries no GST (FR-AP-16).
type ClaimLine struct {
	AccountID   uuid.UUID
	Description string
	Amount      money.Money
	GST         money.Money
	Claimable   bool
	Rate        *money.Rate
	StatedBase  *money.Money
	Paid        *money.Money
	Mileage     bool
}

// Claim is one version of an expense claim as the rule reads it: its date (the entry's posting date), its memo (the
// claim's number and claimant), its lines, and what of it settles the claimant's cash advance, in the base currency,
// credited to Advances (the setting claims.advances_account) instead of 2150 (nil: it settles none).
type Claim struct {
	ID       uuid.UUID
	Version  int
	Date     time.Time
	Memo     string
	Lines    []ClaimLine
	Settles  *money.Money
	Advances uuid.UUID
}

// ExpenseClaimIdentity is the posting identity of a claim version under a rule version (§11.4).
func ExpenseClaimIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceExpenseClaim, id, version, rule)
}

// PostExpenseClaim turns one version of an approved expense claim into its entry (expense_claim@1):
//   - Dr each line's account with its cost: the amount, less the GST when it is claimable (a tax invoice or a simplified
//     one, the organisation GST-registered on the date: FR-AP-20), else the amount with its GST in it; a line in another
//     currency at its own rate under RND-FX;
//   - Dr 2200 GST control with the claimable GST: the line's GST, or for a line in another currency the GST the
//     receipt states in the base currency (ENG-42; none stated, none claimed); a mileage line has none;
//   - Cr 2150 Expense claims payable, in the base currency, with what the company owes the claimant: each line's amount,
//     at its rate for a line in another currency, or what the claimant paid for it when they state it (FR-AP-17);
//   - but Cr the advances account (1250) with what of that settles the claimant's cash advance, and 2150 the rest;
//   - and what makes the entry balance in the base currency to 7030 FX & rounding: the base paid against the amount at
//     the rate, the stated GST against the GST at the rate, and any cent RND-FX leaves between a line's parts.
//
// Every rule is checked first and every failure returned together (ENG-22): the claim is rejected whole.
func PostExpenseClaim(c Claim, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if c.Date.IsZero() {
		add(CodeNoDate, "date", "the claim has no date", "Enter the claim's date")
	}
	if len(c.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(c.Memo), MaxMemo), "Shorten the memo")
	}
	if len(c.Lines) == 0 {
		add(CodeTooFewLines, "lines", "the claim has no lines", "Add a line for each receipt or trip")
	}
	payable, hasPayable := s.byRole(RoleExpenseClaimsPayable)
	if !hasPayable {
		add(CodeSystemRole, "lines", "the chart has no expense claims payable account",
			"Load the standard chart's 2150 Expense claims payable, or give a liability account the role")
	}
	gstControl, hasGST := s.byRole(RoleGSTControl)

	var lines []Line
	owed := money.Zero(s.Base)      // what the company owes, in the base currency
	baseTotal := money.Zero(s.Base) // the debits so far, in the base currency
	gstBase := money.Zero(s.Base)
	for i, l := range c.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		acc, ok := s.Accounts[l.AccountID]
		switch {
		case !ok:
			add(CodeUnknownAccount, f("category_id"), "the line's account does not exist in this organisation", "Choose the claim's category again")
		case acc.Group:
			add(CodeGroupAccount, f("category_id"), fmt.Sprintf("account %s is a group heading", acc.Code), "Give the category an account under it")
		case !acc.Active:
			add(CodeInactiveAccount, f("category_id"), fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it, or give the category another account")
		case acc.ShareCapital:
			add(CodeShareCapital, f("category_id"), fmt.Sprintf("account %s is share capital, which changes only through the register of share issues", acc.Code),
				"Give the category the account the expense is for")
		case acc.Role == RoleExpenseClaimsPayable || acc.Role == RoleGSTControl || l.AccountID == c.Advances:
			add(CodeRuleAccount, f("category_id"), fmt.Sprintf("account %s is one the claim posts what is owed, its GST or an advance to", acc.Code),
				"Give the category the account the expense is for: the rule posts to it by itself")
		}
		if len(l.Description) > MaxDescription {
			add(CodeTooLong, f("description"), fmt.Sprintf("the description has %d characters; the most is %d", len(l.Description), MaxDescription), "Shorten it")
		}
		cur := l.Amount.Currency()
		rate := money.Identity(s.Base)
		rated := true
		switch {
		case cur == s.Base && l.Rate != nil:
			add(CodeRateOnBase, f("fx_rate"), fmt.Sprintf("the line is in the base currency %s, so it takes no exchange rate", s.Base),
				"Remove the rate: Fyniq records rate 1 for base-currency amounts")
		case cur != s.Base && l.Rate == nil:
			add(CodeMissingRate, f("fx_rate"), fmt.Sprintf("the line is in %s, so it needs a rate into %s", cur, s.Base),
				"Enter the exchange rate, or add a rate for the receipt's date in Accounting › Currencies")
			rated = false
		case cur != s.Base && (l.Rate.From != cur || l.Rate.To != s.Base):
			add(CodeWrongRate, f("fx_rate"), fmt.Sprintf("the rate converts %s into %s, but the line needs %s into %s", l.Rate.From, l.Rate.To, cur, s.Base),
				"Use a rate from the receipt's currency into the base currency")
			rated = false
		case cur != s.Base:
			rate = *l.Rate
		}
		if l.Paid != nil {
			switch {
			case cur == s.Base:
				add(CodeClaimPaid, f("paid"), fmt.Sprintf("the line is in %s already, so what was paid is its amount", s.Base), "Leave out what was paid")
			case l.Paid.Currency() != s.Base || l.Paid.Sign() <= 0:
				add(CodeClaimPaid, f("paid"), fmt.Sprintf("what was paid is %s %s", l.Paid, l.Paid.Currency()),
					fmt.Sprintf("Enter what left your account in %s, more than zero, as your card statement shows it", s.Base))
			}
		}
		if l.StatedBase != nil && (cur == s.Base || l.StatedBase.Currency() != s.Base || l.StatedBase.Sign() < 0) {
			add(CodeLineCurrency, f("stated_gst"), fmt.Sprintf("the GST stated in the base currency is %s %s for a line in %s", l.StatedBase, l.StatedBase.Currency(), cur),
				fmt.Sprintf("Enter the GST in %s as the tax invoice states it, only for a receipt in another currency", s.Base))
		}
		if l.GST.Currency() != cur {
			add(CodeLineCurrency, f("gst"), fmt.Sprintf("the line is in %s and its GST in %s", cur, l.GST.Currency()), "Enter the GST in the receipt's currency")
			continue
		}
		switch over, _ := l.GST.Cmp(l.Amount); {
		case l.Amount.IsZero():
			add(CodeZeroLine, f("amount"), "the line's amount is zero", "Enter the amount on the receipt, or remove the line")
			continue
		case l.Amount.Sign() < 0:
			add(CodeClaimLine, f("amount"), fmt.Sprintf("the line's amount is %s %s", l.Amount, cur), "Enter what you spent, more than zero")
			continue
		case l.GST.Sign() < 0 || over > 0:
			add(CodeClaimLine, f("gst"), fmt.Sprintf("the GST is %s %s on an amount of %s %s", l.GST, cur, l.Amount, cur),
				"Enter the GST the receipt shows, from zero up to its amount")
			continue
		case l.Mileage && !l.GST.IsZero():
			add(CodeClaimLine, f("gst"), "a mileage line carries no GST", "Leave the GST out: mileage claims no input tax")
			continue
		}
		cost := l.Amount
		if l.Claimable && !l.GST.IsZero() && (cur == s.Base || l.StatedBase != nil) {
			claim := l.GST
			if cur != s.Base {
				claim = *l.StatedBase
			}
			cost, _ = l.Amount.Sub(l.GST)
			if claim.Currency() == s.Base {
				gstBase, _ = gstBase.Add(claim)
			}
		}
		if !rated {
			continue
		}
		due := l.Amount
		if cur != s.Base {
			var err error
			if due, err = rounding.FX(l.Amount, rate); err != nil {
				add(CodeBaseRange, f("amount"), fmt.Sprintf("at this rate the base amount does not fit: %v", err), "Check the amount and the rate")
				continue
			}
			if l.Paid != nil && l.Paid.Currency() == s.Base {
				due = *l.Paid
			}
		}
		owed, _ = owed.Add(due)
		if cost.IsZero() {
			continue // a line of claimable GST alone posts its GST to 2200
		}
		base, err := rounding.FX(cost, rate)
		if err != nil {
			add(CodeBaseRange, f("amount"), fmt.Sprintf("at this rate the base amount does not fit: %v", err), "Check the amount and the rate")
			continue
		}
		baseTotal, _ = baseTotal.Add(base)
		lines = append(lines, Line{AccountID: l.AccountID, Amount: cost, Rate: rate, Base: base, Description: l.Description})
	}
	if !gstBase.IsZero() && !hasGST {
		add(CodeSystemRole, "lines", "the chart has no GST control account for the input tax", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	settles := money.Zero(s.Base)
	if x := c.Settles; x != nil && !x.IsZero() {
		adv, ok := s.Accounts[c.Advances]
		cmp, err := x.Cmp(owed)
		switch {
		case x.Currency() != s.Base || x.Sign() < 0:
			add(CodeClaimPaid, "settles", fmt.Sprintf("the claim settles %s %s of an advance", x, x.Currency()),
				fmt.Sprintf("Settle an amount in %s from zero up to the claim's total", s.Base))
		case err == nil && cmp > 0 && len(probs) == 0:
			add(CodeClaimPaid, "settles", fmt.Sprintf("the claim settles %s %s of an advance but totals %s %s", x, s.Base, owed, s.Base),
				"Settle at most the claim's total: the rest of the advance stays open")
		case !ok || adv.Group || !adv.Active:
			add(CodeClaimPaid, "advance", "the organisation has no active account for staff and director advances",
				"Set the advances account in Settings › Expense claims, such as the standard chart's 1250 Staff & director advances")
		default:
			settles = *x
		}
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	if !gstBase.IsZero() {
		lines = append(lines, Line{AccountID: gstControl.ID, Amount: gstBase, Rate: money.Identity(s.Base), Base: gstBase, Description: "Input tax"})
		baseTotal, _ = baseTotal.Add(gstBase)
	}
	toPayable, _ := owed.Sub(settles)
	if !toPayable.IsZero() {
		lines = append(lines, Line{AccountID: payable.ID, Amount: toPayable.Neg(), Rate: money.Identity(s.Base), Base: toPayable.Neg(), Description: c.Memo})
	}
	if !settles.IsZero() {
		lines = append(lines, Line{AccountID: c.Advances, Amount: settles.Neg(), Rate: money.Identity(s.Base), Base: settles.Neg(),
			Description: "Settles the cash advance: " + c.Memo})
	}
	if diff, _ := owed.Sub(baseTotal); !diff.IsZero() { // only for lines in another currency
		fxAcc, ok := s.byRole(RoleFXRounding)
		if !ok {
			return Entry{}, Problems{{CodeSystemRole, "lines", "the chart has no FX and rounding account for the difference at the receipts' rates",
				"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
		}
		lines = append(lines, Line{AccountID: fxAcc.ID, Amount: diff, Rate: money.Identity(s.Base), Base: diff,
			Description: "What was paid and the GST as stated against the receipts' rates"})
	}
	y, m, d := c.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceExpenseClaim, ID: c.ID.String(), Version: c.Version},
		RuleVersion:     RuleExpenseClaim,
		PostingIdentity: ExpenseClaimIdentity(c.ID, c.Version, RuleExpenseClaim),
		Memo:            c.Memo,
		Lines:           lines,
	}, nil
}

// ExpenseClaimRule turns one version of an expense claim into its entry under one rule version.
type ExpenseClaimRule func(c Claim, s Settings) (Entry, error)

// ExpenseClaimRules are every expense claim rule version the engine has posted with, by name: like Rules, a version is
// never removed or changed while an entry names it.
var ExpenseClaimRules = map[string]ExpenseClaimRule{
	RuleExpenseClaim: PostExpenseClaim,
}

// ReplayExpenseClaim re-runs the rule version that posted a claim's entry over the same version (ENG-06).
func ReplayExpenseClaim(rule string, c Claim, s Settings) (Entry, error) {
	f, ok := ExpenseClaimRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(c, s)
}

// The reimbursement's source type and rule version (FR-AP-07; the owner's decisions of 26 Oct 2026, rev 0.147: recorded
// by hand until payment batches; p1-expense-claims design D7).
const (
	SourceClaimReimbursement = "claim_reimbursement"
	RuleClaimReimbursement   = "claim_reimbursement@1"
)

// PaidClaim is what a reimbursement settles of one claim, in the base currency: what the company owes is in it.
type PaidClaim struct {
	ClaimID uuid.UUID
	Amount  money.Money
}

// ClaimReimbursement is one version of a reimbursement of a claimant's claims as the rule reads it: the money account it
// left, in the base currency, and what it settles of each claim.
type ClaimReimbursement struct {
	ID      uuid.UUID
	Version int
	Date    time.Time
	Memo    string    // the claimant and the bank's reference
	Account uuid.UUID // the money account it was paid from
	Claims  []PaidClaim
}

// ClaimReimbursementIdentity is the posting identity of a reimbursement version under a rule version (§11.4).
func ClaimReimbursementIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceClaimReimbursement, id, version, rule)
}

// PostClaimReimbursement turns one version of a reimbursement into its entry (claim_reimbursement@1) and the
// settlements it records: Dr 2150 Expense claims payable with what it settles, Cr the money account with the same, both
// in the base currency. Every rule is checked first and every failure returned together (ENG-22).
func PostClaimReimbursement(r ClaimReimbursement, s Settings) (Entry, []Settlement, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if r.Date.IsZero() {
		add(CodeNoDate, "date", "the reimbursement has no date", "Enter the date the money left the account")
	}
	if len(r.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(r.Memo), MaxMemo), "Shorten the reference")
	}
	if len(r.Claims) == 0 {
		add(CodeTooFewLines, "claims", "the reimbursement settles no claim", "Choose the claims it pays back")
	}
	acc, ok := s.Accounts[r.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "money_account", "the account does not exist in this organisation", "Choose the bank, card or wallet account it was paid from")
	case acc.Group || !acc.Active || !acc.Money:
		add(CodeNotMoneyAccount, "money_account", fmt.Sprintf("account %s is not an active money account", acc.Code), "Choose the bank, card or wallet account it was paid from")
	case acc.Currency != "" && acc.Currency != s.Base.Code:
		add(CodePaymentAmount, "money_account", fmt.Sprintf("account %s holds %s, but what the company owes its claimants is in %s", acc.Code, acc.Currency, s.Base),
			fmt.Sprintf("Pay it back from an account in %s", s.Base))
	}
	payable, hasPayable := s.byRole(RoleExpenseClaimsPayable)
	if !hasPayable {
		add(CodeSystemRole, "claims", "the chart has no expense claims payable account",
			"Load the standard chart's 2150 Expense claims payable, or give a liability account the role")
	}
	total := money.Zero(s.Base)
	var out []Settlement
	for i, c := range r.Claims {
		if c.Amount.Currency() != s.Base || c.Amount.Sign() <= 0 {
			add(CodePaymentAmount, fmt.Sprintf("claims[%d].amount", i+1), fmt.Sprintf("it settles %s %s of the claim", c.Amount, c.Amount.Currency()),
				fmt.Sprintf("Enter what it pays back of the claim in %s, more than zero", s.Base))
			continue
		}
		total, _ = total.Add(c.Amount)
		out = append(out, Settlement{DocumentID: c.ClaimID, Amount: c.Amount, Base: c.Amount, RealisedFX: money.Zero(s.Base), Tolerance: money.Zero(s.Base)})
	}
	if len(probs) > 0 {
		return Entry{}, nil, probs
	}
	y, m, d := r.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceClaimReimbursement, ID: r.ID.String(), Version: r.Version},
		RuleVersion:     RuleClaimReimbursement,
		PostingIdentity: ClaimReimbursementIdentity(r.ID, r.Version, RuleClaimReimbursement),
		Memo:            r.Memo,
		Lines: []Line{
			{AccountID: payable.ID, Amount: total, Rate: money.Identity(s.Base), Base: total, Description: r.Memo},
			{AccountID: r.Account, Amount: total.Neg(), Rate: money.Identity(s.Base), Base: total.Neg(), Description: r.Memo},
		},
	}, out, nil
}

// ClaimReimbursementRule turns one version of a reimbursement into its entry under one rule version.
type ClaimReimbursementRule func(r ClaimReimbursement, s Settings) (Entry, []Settlement, error)

// ClaimReimbursementRules are every reimbursement rule version the engine has posted with, by name.
var ClaimReimbursementRules = map[string]ClaimReimbursementRule{
	RuleClaimReimbursement: PostClaimReimbursement,
}

// ReplayClaimReimbursement re-runs the rule version that posted a reimbursement's entry over the same version (ENG-06).
func ReplayClaimReimbursement(rule string, r ClaimReimbursement, s Settings) (Entry, error) {
	f, ok := ClaimReimbursementRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	e, _, err := f(r, s)
	return e, err
}
