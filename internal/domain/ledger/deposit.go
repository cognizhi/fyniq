package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The customer deposit's source type and rule version (FR-AR-09; the posting rule "Customer refunds and deposits",
// PRD-001 §10.2; p1-sales task 3.2), and the role of the account that holds deposits (2410 in the standard chart).
const (
	SourceCustomerDeposit = "customer_deposit"
	RuleCustomerDeposit   = "customer_deposit@1"
	RoleCustomerDeposits  = "customer-deposits"
)

// CustomerDeposit is one version of money a customer paid ahead of an invoice, as the rule reads it: the money account
// it came into, its currency and rate (nil for a deposit in the base currency, or one into a base-currency account,
// which names what it Received instead), its gross amount, and the GST rate it bears as a fraction ("0.09"; "0" for a
// zero-rated or exempt part payment), empty when it bears none: a security deposit, a part payment before the
// organisation is GST-registered, or one out of scope (the domain decides).
type CustomerDeposit struct {
	ID       uuid.UUID
	Version  int
	Date     time.Time
	Memo     string
	Account  uuid.UUID
	Currency money.Currency
	Rate     *money.Rate
	Received *money.Money
	Amount   money.Money
	GSTRate  string
}

// CustomerDepositIdentity is the posting identity of a customer deposit version under a rule version (§11.4).
func CustomerDepositIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceCustomerDeposit, id, version, rule)
}

// PostCustomerDeposit turns one version of a customer deposit into its entry (customer_deposit@1): Dr the money account
// with what came in (its gross at the rate, or what a base-currency account received), Cr the customer deposits
// account (2410) in the base currency with that less its GST, and Cr 2200 with the GST a part payment bears when
// received (ENG-45) under RND-GST-INCL on the gross in the base currency. 2410 holds the deposit at the rate it came in
// and is never revalued. Every rule is checked first and every failure returned together (ENG-22).
func PostCustomerDeposit(d CustomerDeposit, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if d.Date.IsZero() {
		add(CodeNoDate, "date", "the deposit has no date", "Enter the date the money came into the account")
	}
	if len(d.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(d.Memo), MaxMemo), "Shorten the reference")
	}
	acc, ok := s.Accounts[d.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "money_account", "the account does not exist in this organisation", "Choose the bank, card or wallet account it came into")
	case acc.Group || !acc.Active || !acc.Money:
		add(CodeNotMoneyAccount, "money_account", fmt.Sprintf("account %s is not an active money account", acc.Code), "Choose the bank, card or wallet account it came into")
	}
	into := s.Base
	if acc.Currency != "" {
		if c, err := money.Lookup(acc.Currency); err == nil {
			into = c
		}
	}
	foreign := d.Currency != s.Base
	intoBase := ok && foreign && into == s.Base
	rate := money.Identity(s.Base)
	switch {
	case intoBase && d.Rate != nil:
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("account %s is in %s: the deposit is at the rate what it received implies", acc.Code, s.Base),
			"Leave the rate out, and enter what the account received")
	case intoBase:
	case !foreign && d.Rate != nil:
		add(CodeRateOnBase, "fx_rate", fmt.Sprintf("the deposit is in %s, the base currency, so it takes no rate", s.Base), "Leave the rate out")
	case foreign && d.Rate == nil:
		add(CodeMissingRate, "fx_rate", fmt.Sprintf("the deposit is in %s, so it needs a rate into %s", d.Currency, s.Base),
			"Enter the rate, or add a rate for the deposit's date in Accounting › Currencies")
	case foreign && (d.Rate.From != d.Currency || d.Rate.To != s.Base):
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("the rate converts %s into %s, but the deposit needs %s into %s", d.Rate.From, d.Rate.To, d.Currency, s.Base),
			"Use a rate from the deposit's currency into the base currency")
	case foreign:
		rate = *d.Rate
	}
	if ok && into != d.Currency && into != s.Base {
		add(CodeLineCurrency, "money_account", fmt.Sprintf("account %s is in %s; the deposit is in %s", acc.Code, into, d.Currency),
			"Record it into an account in the deposit's currency, or in the base currency")
	}
	switch {
	case intoBase && d.Received == nil:
		add(CodePaidAmount, "received", fmt.Sprintf("account %s is in %s: enter what it received in %s", acc.Code, s.Base, s.Base), "Enter the amount the account statement shows")
	case d.Received != nil && !intoBase:
		add(CodePaidAmount, "received", "only a deposit in another currency into a base-currency account names what the account received", "Leave it out")
	case d.Received != nil && (d.Received.Currency() != s.Base || d.Received.Sign() <= 0):
		add(CodePaidAmount, "received", fmt.Sprintf("what the account received is %s %s", d.Received, d.Received.Currency()), fmt.Sprintf("Enter a positive amount in %s", s.Base))
	}
	if d.Amount.Currency() != d.Currency || d.Amount.Sign() <= 0 {
		add(CodePaymentAmount, "amount", fmt.Sprintf("the deposit is %s %s", d.Amount, d.Amount.Currency()), fmt.Sprintf("Enter a positive amount in %s", d.Currency))
	}
	deposits, hasDeposits := s.byRole(RoleCustomerDeposits)
	if !hasDeposits {
		add(CodeSystemRole, "amount", "the chart has no customer deposits account", "Load the standard chart's 2410 Customer deposits & retainers, or give a liability account the role")
	}
	gstControl, hasGST := s.byRole(RoleGSTControl)
	if d.GSTRate != "" && !hasGST {
		add(CodeSystemRole, "gst_code", "the chart has no GST control account for the output tax", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	gross := money.Zero(s.Base)
	if d.Received != nil {
		gross = *d.Received
	} else {
		var err error
		if gross, err = rounding.FX(d.Amount, rate); err != nil {
			return Entry{}, Problems{{CodeBaseRange, "amount", err.Error(), "Check the amount and the rate"}}
		}
	}
	gst := money.Zero(s.Base)
	if d.GSTRate != "" {
		var err error
		if gst, err = rounding.GSTIncl(gross, d.GSTRate); err != nil {
			return Entry{}, Problems{{CodeGSTRate, "gst_code", err.Error(), "Choose the deposit's GST code again: its rate comes from the sg-gst pack"}}
		}
	}
	net, _ := gross.Sub(gst)
	id := money.Identity(s.Base)
	var lines []Line
	if d.Received != nil {
		lines = append(lines, Line{AccountID: d.Account, Amount: gross, Rate: id, Base: gross, Description: d.Memo})
	} else {
		lines = append(lines, Line{AccountID: d.Account, Amount: d.Amount, Rate: rate, Base: gross, Description: d.Memo})
	}
	lines = append(lines, Line{AccountID: deposits.ID, Amount: net.Neg(), Rate: id, Base: net.Neg(), Description: d.Memo})
	if !gst.IsZero() {
		lines = append(lines, Line{AccountID: gstControl.ID, Amount: gst.Neg(), Rate: id, Base: gst.Neg(), Description: "Output tax on the deposit"})
	}
	y, m, dd := d.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, dd, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceCustomerDeposit, ID: d.ID.String(), Version: d.Version},
		RuleVersion:     RuleCustomerDeposit,
		PostingIdentity: CustomerDepositIdentity(d.ID, d.Version, RuleCustomerDeposit),
		Memo:            d.Memo,
		Lines:           lines,
	}, nil
}

// CustomerDepositRule turns one version of a customer deposit into its entry under one rule version.
type CustomerDepositRule func(d CustomerDeposit, s Settings) (Entry, error)

// CustomerDepositRules are every customer deposit rule version the engine has posted with, by name: like Rules, a
// version is never removed or changed while an entry names it.
var CustomerDepositRules = map[string]CustomerDepositRule{
	RuleCustomerDeposit: PostCustomerDeposit,
}

// ReplayCustomerDeposit re-runs the rule version that posted a customer deposit's entry over the same version (ENG-06).
func ReplayCustomerDeposit(rule string, d CustomerDeposit, s Settings) (Entry, error) {
	f, ok := CustomerDepositRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(d, s)
}
