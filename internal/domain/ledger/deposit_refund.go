package ledger

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// RuleDepositRefund is the rule version of a refund of a customer deposit (FR-AR-09; p1-sales task 3.2), a refund to a
// customer (source customer_refund) whose credit is a deposit.
const RuleDepositRefund = "deposit_refund@1"

// DepositPart is what a refund of a customer deposit takes of it (RND-DEPOSIT): its part of the deposit's base amount,
// and of the output tax the deposit bore, in the base currency.
type DepositPart struct {
	Base money.Money
	Tax  money.Money
}

// PostDepositRefund turns one version of a refund of a customer deposit into its entry (deposit_refund@1) and what it
// settled of the deposit: Dr 2410 with the deposit's base part less its tax part, Dr 2200 with the tax part (the output
// tax accounted for when the deposit came in, adjusted with the refund) [verify], Cr the money account with what left
// it (the amount at the refund's rate, or what a base-currency account paid), and what the deposit's part came to
// against what was paid to 7030 as realised FX: a gain when less went out. Every rule is checked first and every
// failure returned together (ENG-22).
func PostDepositRefund(r CustomerRefund, s Settings) (Entry, Settlement, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if r.Date.IsZero() {
		add(CodeNoDate, "date", "the refund has no date", "Enter the date the money left the account")
	}
	if len(r.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(r.Memo), MaxMemo), "Shorten the reference")
	}
	acc, ok := s.Accounts[r.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "money_account", "the account does not exist in this organisation", "Choose the bank, card or wallet account it left")
	case acc.Group || !acc.Active || !acc.Money:
		add(CodeNotMoneyAccount, "money_account", fmt.Sprintf("account %s is not an active money account", acc.Code), "Choose the bank, card or wallet account it left")
	}
	from := s.Base
	if acc.Currency != "" {
		if c, err := money.Lookup(acc.Currency); err == nil {
			from = c
		}
	}
	foreign := r.Currency != s.Base
	fromBase := ok && foreign && from == s.Base
	rate := money.Identity(s.Base)
	switch {
	case fromBase && r.Rate != nil:
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("account %s is in %s: the refund is at the rate what it paid implies", acc.Code, s.Base),
			"Leave the rate out, and enter what the account paid")
	case fromBase:
	case !foreign && r.Rate != nil:
		add(CodeRateOnBase, "fx_rate", fmt.Sprintf("the deposit is in %s, the base currency, so the refund takes no rate", s.Base), "Leave the rate out")
	case foreign && r.Rate == nil:
		add(CodeMissingRate, "fx_rate", fmt.Sprintf("the deposit is in %s, so the refund needs a rate into %s", r.Currency, s.Base),
			"Enter the rate, or add a rate for the refund's date in Accounting › Currencies")
	case foreign && (r.Rate.From != r.Currency || r.Rate.To != s.Base):
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("the rate converts %s into %s, but the refund needs %s into %s", r.Rate.From, r.Rate.To, r.Currency, s.Base),
			"Use a rate from the deposit's currency into the base currency")
	case foreign:
		rate = *r.Rate
	}
	if ok && from != r.Currency && from != s.Base {
		add(CodeLineCurrency, "money_account", fmt.Sprintf("account %s is in %s; the deposit is in %s", acc.Code, from, r.Currency),
			"Refund from an account in the deposit's currency, or in the base currency")
	}
	switch {
	case fromBase && r.Paid == nil:
		add(CodePaidAmount, "paid", fmt.Sprintf("account %s is in %s: enter what it paid in %s", acc.Code, s.Base, s.Base), "Enter the amount the account statement shows")
	case r.Paid != nil && !fromBase:
		add(CodePaidAmount, "paid", "only a deposit in another currency refunded from a base-currency account names what the account paid", "Leave it out")
	case r.Paid != nil && (r.Paid.Currency() != s.Base || r.Paid.Sign() <= 0):
		add(CodePaidAmount, "paid", fmt.Sprintf("what the account paid is %s %s", r.Paid, r.Paid.Currency()), fmt.Sprintf("Enter a positive amount in %s", s.Base))
	}
	amount := r.Credit.Amount
	if amount.Currency() != r.Currency || amount.Sign() <= 0 {
		add(CodePaymentAmount, "amount", fmt.Sprintf("the amount refunded is %s %s", amount, amount.Currency()), fmt.Sprintf("Enter a positive amount in %s", r.Currency))
	}
	var part DepositPart
	switch {
	case r.Deposit == nil:
		add(CodeDeposit, "credit_id", "the refund names no deposit's part", "Refund a customer deposit")
	case r.Deposit.Base.Currency() != s.Base || r.Deposit.Tax.Currency() != s.Base || r.Deposit.Base.Sign() < 0 || r.Deposit.Tax.Sign() < 0:
		add(CodeDeposit, "credit_id", fmt.Sprintf("the deposit's parts are %s %s and %s %s of tax", r.Deposit.Base, r.Deposit.Base.Currency(), r.Deposit.Tax,
			r.Deposit.Tax.Currency()), fmt.Sprintf("Its parts are amounts in %s, not below zero", s.Base))
	default:
		part = *r.Deposit
		if c, _ := part.Tax.Cmp(part.Base); c > 0 {
			add(CodeDeposit, "credit_id", fmt.Sprintf("the deposit's tax part %s is more than its base part %s", part.Tax, part.Base), "Its tax part is a part of its base amount")
		}
	}
	deposits, hasDeposits := s.byRole(RoleCustomerDeposits)
	if !hasDeposits {
		add(CodeSystemRole, "credit_id", "the chart has no customer deposits account", "Load the standard chart's 2410 Customer deposits & retainers, or give a liability account the role")
	}
	gstControl, hasGST := s.byRole(RoleGSTControl)
	if r.Deposit != nil && !r.Deposit.Tax.IsZero() && !hasGST {
		add(CodeSystemRole, "credit_id", "the chart has no GST control account for the output tax the deposit bore", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	if len(probs) > 0 {
		return Entry{}, Settlement{}, probs
	}
	paid := money.Zero(s.Base)
	if r.Paid != nil {
		paid = *r.Paid
	} else {
		var err error
		if paid, err = rounding.FX(amount, rate); err != nil {
			return Entry{}, Settlement{}, Problems{{CodeBaseRange, "amount", err.Error(), "Check the amount and the rate"}}
		}
	}
	id := money.Identity(s.Base)
	var lines []Line
	if held, _ := part.Base.Sub(part.Tax); !held.IsZero() {
		lines = append(lines, Line{AccountID: deposits.ID, Amount: held, Rate: id, Base: held, Description: r.Memo})
	}
	if !part.Tax.IsZero() {
		lines = append(lines, Line{AccountID: gstControl.ID, Amount: part.Tax, Rate: id, Base: part.Tax, Description: "Output tax on the deposit refunded"})
	}
	if r.Paid != nil {
		lines = append(lines, Line{AccountID: r.Account, Amount: paid.Neg(), Rate: id, Base: paid.Neg(), Description: r.Memo})
	} else {
		lines = append(lines, Line{AccountID: r.Account, Amount: amount.Neg(), Rate: rate, Base: paid.Neg(), Description: r.Memo})
	}
	gain, _ := part.Base.Sub(paid)
	if !gain.IsZero() {
		fxAcc, ok := s.byRole(RoleFXRounding)
		if !ok {
			return Entry{}, Settlement{}, Problems{{CodeSystemRole, "credit_id", "the chart has no FX and rounding account for the realised FX",
				"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
		}
		lines = append(lines, Line{AccountID: fxAcc.ID, Amount: gain.Neg(), Rate: id, Base: gain.Neg(), Description: "Realised FX on the deposit refunded"})
	}
	y, m, d := r.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceCustomerRefund, ID: r.ID.String(), Version: r.Version},
		RuleVersion:     RuleDepositRefund,
		PostingIdentity: CustomerRefundIdentity(r.ID, r.Version, RuleDepositRefund),
		Memo:            r.Memo,
		Lines:           lines,
	}, Settlement{DocumentID: r.Credit.BillID, Amount: amount, Base: part.Base, RealisedFX: gain, Tolerance: money.Zero(s.Base)}, nil
}
