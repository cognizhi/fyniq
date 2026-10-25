package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The transfer's source type and rule version (FR-BNK-09; posting rule "Transfer between money accounts", rev 0.140;
// p1-bank-import-reconcile task 4.4, design D6): one record with a leg on each money account, posted once.
const (
	SourceBankTransfer = "bank_transfer"
	RuleBankTransfer   = "bank_transfer@1"
)

// CodeTransfer is a transfer whose legs do not fit: one account twice, an amount that is not more than zero, a fee
// beyond what was paid, or legs in one currency that differ by more than the fee.
const CodeTransfer = "LEDGER-35"

// BankTransfer is one transfer between money accounts as the rule reads it: its date, the paying account's chart
// account, what left it in its currency and at that currency's rate for the day (nil in the base currency), the
// receiving account's and what arrived at its rate, the fee the provider reports in the paying currency (nil for
// none, included in what was paid), and the account that fee goes to (the setting bank.charges_account).
type BankTransfer struct {
	ID       uuid.UUID
	Date     time.Time
	Memo     string
	From     uuid.UUID
	Paid     money.Money
	FromRate *money.Rate
	To       uuid.UUID
	Received money.Money
	ToRate   *money.Rate
	Fee      *money.Money
	Charges  uuid.UUID
}

// BankTransferIdentity is the posting identity of a transfer under a rule version (§11.4).
func BankTransferIdentity(id uuid.UUID, rule string) string {
	return identity(SourceBankTransfer, id, 1, rule)
}

// leg is a money account leg's checks: the account, its amount in the account's currency and the rate into the base.
func (t BankTransfer) leg(s Settings, add func(code, field, msg, fix string), field string, acc uuid.UUID, amount money.Money, rate *money.Rate) (money.Rate, bool) {
	a, ok := s.Accounts[acc]
	switch {
	case !ok:
		add(CodeUnknownAccount, field+"_account", "the account does not exist in this organisation", "Choose one of your money accounts")
		return money.Rate{}, false
	case a.Group || !a.Active || !a.Money:
		add(CodeNotMoneyAccount, field+"_account", fmt.Sprintf("account %s is not an active money account", a.Code), "Choose one of your money accounts")
		return money.Rate{}, false
	}
	cur := s.Base
	if a.Currency != "" {
		if c, err := money.Lookup(a.Currency); err == nil {
			cur = c
		}
	}
	if amount.Currency() != cur || amount.Sign() <= 0 {
		add(CodeTransfer, field+"_amount", fmt.Sprintf("the amount is %s %s; account %s is in %s", amount, amount.Currency(), a.Code, cur),
			fmt.Sprintf("Enter a positive amount in %s", cur))
		return money.Rate{}, false
	}
	switch {
	case cur == s.Base && rate != nil:
		add(CodeRateOnBase, field+"_rate", fmt.Sprintf("account %s is in %s, the base currency, so it takes no rate", a.Code, s.Base), "Leave the rate out")
	case cur != s.Base && rate == nil:
		add(CodeMissingRate, field+"_rate", fmt.Sprintf("account %s is in %s, so its leg needs a rate into %s", a.Code, cur, s.Base),
			"Add a rate for the transfer's date in Accounting › Currencies")
	case cur != s.Base && (rate.From != cur || rate.To != s.Base):
		add(CodeWrongRate, field+"_rate", fmt.Sprintf("the rate converts %s into %s, but the leg needs %s into %s", rate.From, rate.To, cur, s.Base),
			"Use a rate from the leg's currency into the base currency")
	case cur != s.Base:
		return *rate, true
	default:
		return money.Identity(s.Base), true
	}
	return money.Rate{}, false
}

// PostBankTransfer turns a transfer into its entry (bank_transfer@1): Cr the paying account with what left it and Dr
// the receiving account with what arrived, each at its own currency's rate for the day (ENG-40, RND-FX); Dr the bank
// charges account with the fee the provider reports, at the paying currency's rate; and what is left of the base
// amounts' difference to 7030, a cost of the conversion (the owner's decision of 25 Oct 2026). In one currency the
// legs differ by the fee only, and a cent RND-FX leaves goes to 7030. Every rule is checked first and every failure
// returned together (ENG-22).
func PostBankTransfer(t BankTransfer, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if t.Date.IsZero() {
		add(CodeNoDate, "date", "the transfer has no date", "Enter the date the money left the paying account")
	}
	if len(t.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(t.Memo), MaxMemo), "Shorten it")
	}
	if t.From == t.To {
		add(CodeTransfer, "to_account", "the transfer pays and receives on one account", "Choose two different money accounts")
	}
	fromRate, fromOK := t.leg(s, add, "from", t.From, t.Paid, t.FromRate)
	toRate, toOK := t.leg(s, add, "to", t.To, t.Received, t.ToRate)
	fee := money.Zero(t.Paid.Currency())
	if t.Fee != nil {
		fee = *t.Fee
		if fee.Currency() != t.Paid.Currency() || fee.Sign() < 0 {
			add(CodeTransfer, "fee", fmt.Sprintf("the fee is %s %s; it is reported in the paying currency, %s", fee, fee.Currency(), t.Paid.Currency()),
				fmt.Sprintf("Enter the fee in %s, zero or more", t.Paid.Currency()))
		} else if c, _ := fee.Cmp(t.Paid); c >= 0 {
			add(CodeTransfer, "fee", fmt.Sprintf("the fee %s is not less than what was paid, %s", fee, t.Paid), "Enter the fee the provider reports, which is part of what was paid")
		}
	}
	if fromOK && toOK && t.Paid.Currency() == t.Received.Currency() && fee.Currency() == t.Paid.Currency() {
		if sent, _ := t.Paid.Sub(fee); !sent.Equal(t.Received) {
			add(CodeTransfer, "to_amount", fmt.Sprintf("%s %s left and %s %s arrived, with a fee of %s", t.Paid, t.Paid.Currency(), t.Received, t.Received.Currency(), fee),
				"In one currency what arrived is what left less the fee: record the difference as the fee")
		}
	}
	charges, hasCharges := s.Accounts[t.Charges]
	if !fee.IsZero() && (!hasCharges || charges.Group || !charges.Active) {
		add(CodeSystemRole, "fee", "the bank charges account is not an active account in the chart", "Set Settings › Organisation › Bank charges account to an account of your chart, 6460 by default")
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	id := money.Identity(s.Base)
	paidBase, err := rounding.FX(t.Paid, fromRate)
	if err != nil {
		return Entry{}, Problems{{CodeBaseRange, "from_amount", err.Error(), "Check the amount and the rate"}}
	}
	receivedBase, err := rounding.FX(t.Received, toRate)
	if err != nil {
		return Entry{}, Problems{{CodeBaseRange, "to_amount", err.Error(), "Check the amount and the rate"}}
	}
	feeBase, err := rounding.FX(fee, fromRate)
	if err != nil {
		return Entry{}, Problems{{CodeBaseRange, "fee", err.Error(), "Check the fee and the rate"}}
	}
	lines := []Line{
		{AccountID: t.To, Amount: t.Received, Rate: toRate, Base: receivedBase, Description: t.Memo},
		{AccountID: t.From, Amount: t.Paid.Neg(), Rate: fromRate, Base: paidBase.Neg(), Description: t.Memo},
	}
	if !feeBase.IsZero() {
		lines = append(lines, Line{AccountID: t.Charges, Amount: feeBase, Rate: id, Base: feeBase, Description: "Fee on the transfer"})
	}
	rest, _ := paidBase.Sub(receivedBase)
	rest, _ = rest.Sub(feeBase)
	if !rest.IsZero() {
		fxr, ok := s.byRole(RoleFXRounding)
		if !ok {
			return Entry{}, Problems{{CodeSystemRole, "to_amount", "the chart has no FX and rounding account for the conversion's difference",
				"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
		}
		lines = append(lines, Line{AccountID: fxr.ID, Amount: rest, Rate: id, Base: rest, Description: "Difference on the conversion"})
	}
	y, m, d := t.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceBankTransfer, ID: t.ID.String(), Version: 1},
		RuleVersion:     RuleBankTransfer,
		PostingIdentity: BankTransferIdentity(t.ID, RuleBankTransfer),
		Memo:            t.Memo,
		Lines:           lines,
	}, nil
}

// BankTransferRule turns a transfer into its entry under one rule version.
type BankTransferRule func(t BankTransfer, s Settings) (Entry, error)

// BankTransferRules are every transfer rule version the engine has posted with, by name: like Rules, a version is
// never removed or changed while an entry names it.
var BankTransferRules = map[string]BankTransferRule{RuleBankTransfer: PostBankTransfer}

// ReplayBankTransfer re-runs the rule version that posted a transfer's entry over the same transfer (ENG-06).
func ReplayBankTransfer(rule string, t BankTransfer, s Settings) (Entry, error) {
	f, ok := BankTransferRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(t, s)
}
