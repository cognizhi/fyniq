package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The GST bad debt relief claim's source type and rule version (FR-AR-11; the posting rule "GST bad debt relief", §10.2; rev 0.608;
// p2-tax-filings task 3.2). The write-off of a debt (write_off@1) took the whole of it, the GST the customer never paid
// included, to an expense account and left the output tax on the invoice in the GST control account. When the claim is made
// (IRAS's conditions: the sg-tax-filings pack's dates), the output tax in the part written off comes back out of it: Dr the GST
// control account, Cr the account the write-off took the debt to, both in the base currency, for the amount RND-BDR gives.
const (
	SourceBadDebtRelief = "bad_debt_relief"
	RuleBadDebtRelief   = "bad_debt_relief@1"
)

// CodeNoRelief is the code of a claim that has no GST to claim (nothing of the invoice's output tax is left, or the part's share
// of it rounds to nothing).
const CodeNoRelief = "LEDGER-52"

// BadDebtRelief is one version of a claim, as the rule reads it: the write-off it is for, what was written off and what the invoice
// made the customer owe (the invoice's currency), what the invoice's earlier claims were for (WrittenBefore) and claimed
// (ClaimedBefore), the output tax the invoice posted to the GST control account (Output, in the base currency) and the account the
// write-off took the debt to.
type BadDebtRelief struct {
	ID            uuid.UUID
	Version       int
	Date          time.Time
	Memo          string // the invoice, the customer and the write-off
	WriteOffID    uuid.UUID
	InvoiceID     uuid.UUID
	Written       money.Money
	Owed          money.Money
	WrittenBefore money.Money
	Output        money.Money
	ClaimedBefore money.Money
	Account       uuid.UUID
}

// BadDebtReliefIdentity is the posting identity of a claim version under a rule version (§11.4).
func BadDebtReliefIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceBadDebtRelief, id, version, rule)
}

// ReliefOf is the GST a claim brings in under RND-BDR, and what is wrong with the claim when it cannot be worked.
func ReliefOf(c BadDebtRelief, base money.Currency) (money.Money, error) {
	if c.Output.Currency() != base || c.ClaimedBefore.Currency() != base {
		return money.Money{}, fmt.Errorf("the output tax is %s and what was claimed %s, but GST is accounted for in %s", c.Output.Currency(), c.ClaimedBefore.Currency(), base)
	}
	return rounding.BadDebtGST(c.Output, c.Owed, c.WrittenBefore, c.Written, c.ClaimedBefore)
}

// PostBadDebtRelief turns one version of a claim into its entry (bad_debt_relief@1): Dr the GST control account and Cr the
// write-off's account, in the base currency, for the share of the invoice's output tax in the part written off (RND-BDR). Every
// rule is checked first and every failure returned together (ENG-22).
func PostBadDebtRelief(c BadDebtRelief, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if c.Date.IsZero() {
		add(CodeNoDate, "date", "the claim has no date", "Enter the date the relief is claimed")
	}
	if len(c.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(c.Memo), MaxMemo), "Shorten the memo")
	}
	gstControl, hasGST := s.byRole(RoleGSTControl)
	if !hasGST {
		add(CodeSystemRole, "write_off_id", "the chart has no GST control account", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	acc, known := s.Accounts[c.Account]
	switch {
	case !known:
		add(CodeUnknownAccount, "account_id", "the account the debt was written off to does not exist in this organisation", "Choose the account the write-off took the debt to")
	case acc.Group:
		add(CodeGroupAccount, "account_id", fmt.Sprintf("account %s is a group heading", acc.Code), "Choose an account under it")
	case !acc.Active:
		add(CodeInactiveAccount, "account_id", fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it, or write the debt off to another")
	case acc.Role == RoleAccountsReceivable || acc.Role == RoleGSTControl || acc.Role == RoleDeferredRevenue || acc.ShareCapital:
		add(CodeRuleAccount, "account_id", fmt.Sprintf("account %s is not one a relief claim takes", acc.Code), "The claim is credited to the expense or revenue account the write-off took the debt to")
	}
	var gst money.Money
	if share, err := ReliefOf(c, s.Base); err != nil {
		add(CodePaymentAmount, "write_off_id", err.Error(), "Claim for a write-off of what the invoice owed, once")
	} else if share.IsZero() {
		add(CodeNoRelief, "write_off_id", fmt.Sprintf("no GST is left to claim: the share of %s of output tax in %s written off of %s owed is nothing", c.Output, c.Written, c.Owed),
			"Only a write-off of an invoice that charged GST can be claimed, and each only once")
	} else {
		gst = share
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	id := money.Identity(s.Base)
	y, m, d := c.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceBadDebtRelief, ID: c.ID.String(), Version: c.Version},
		RuleVersion:     RuleBadDebtRelief,
		PostingIdentity: BadDebtReliefIdentity(c.ID, c.Version, RuleBadDebtRelief),
		Memo:            c.Memo,
		Lines: []Line{
			{AccountID: gstControl.ID, Amount: gst, Rate: id, Base: gst, Description: c.Memo},
			{AccountID: c.Account, Amount: gst.Neg(), Rate: id, Base: gst.Neg(), Description: c.Memo},
		},
	}, nil
}

// BadDebtReliefRule turns one version of a claim into its entry under one rule version.
type BadDebtReliefRule func(c BadDebtRelief, s Settings) (Entry, error)

// BadDebtReliefRules are every claim rule version the engine has posted with, by name: like Rules, a version is never removed or
// changed while an entry names it.
var BadDebtReliefRules = map[string]BadDebtReliefRule{
	RuleBadDebtRelief: PostBadDebtRelief,
}

// ReplayBadDebtRelief re-runs the rule version that posted a claim's entry over the same version (ENG-06).
func ReplayBadDebtRelief(rule string, c BadDebtRelief, s Settings) (Entry, error) {
	f, ok := BadDebtReliefRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(c, s)
}
