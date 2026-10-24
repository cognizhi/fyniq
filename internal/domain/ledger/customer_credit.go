package ledger

import (
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The customer credit application's source type and rule version (FR-AR-04, FR-AR-18; ENG-41; p1-sales task 3.1).
const (
	SourceCustomerCreditApplication = "customer_credit_application"   // #nosec G101 -- a source type's name, not a credential
	RuleCustomerCreditApplication   = "customer_credit_application@1" // #nosec G101 -- a rule version's name, not a credential
)

// CustomerCreditApplication is one version of a credit with a customer applied to one of the customer's invoices, as
// the rule reads it: the amount in their currency, the invoice's stored rate and the credit's carrying rate (a credit
// note's stored rate, or the rate of the receipt that left the amount unapplied), and, when the application settles
// all that is open of either, the carrying amount left of it, which it clears (as a receipt's last part does).
type CustomerCreditApplication struct {
	ID            uuid.UUID
	Version       int
	Date          time.Time
	Memo          string
	Amount        money.Money
	InvoiceRate   money.Rate
	CreditRate    money.Rate
	InvoiceClears *money.Money
	CreditClears  *money.Money
}

// CustomerApplied is what an application cleared: the invoice's carrying amount, the credit's, and the realised gain
// (+) or loss (−) the two make in the base currency (ENG-41): what the credit carried less what the invoice carried.
type CustomerApplied struct {
	Invoice, Credit, RealisedFX money.Money
}

// CustomerCreditApplicationIdentity is the posting identity of a customer credit application version under a rule
// version (§11.4).
func CustomerCreditApplicationIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceCustomerCreditApplication, id, version, rule)
}

// PostCustomerCreditApplication turns one version of a customer credit application into its entry
// (customer_credit_application@1) and what it cleared. Applying a credit settles the invoice and the credit by the same
// amount, so 1100's balance in their currency does not move; only the two carrying amounts can differ, by the rates
// they were booked at:
//   - Cr 1100 with the amount at the invoice's stored rate, and Dr 1100 with it at the credit's carrying rate, each with
//     a base-only line for the residual when it settles what is left of its document (at most MaxResidual);
//   - and what makes the entry balance in the base currency to 7030: the realised FX (ENG-41).
//
// When the two carrying amounts cleared are equal, as for an invoice and a credit in the base currency, the application
// posts nothing and the entry has no lines: it is an allocation alone. Every rule is checked first and every failure
// returned together (ENG-22).
func PostCustomerCreditApplication(a CustomerCreditApplication, s Settings) (Entry, CustomerApplied, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if a.Date.IsZero() {
		add(CodeNoDate, "date", "the application has no date", "Enter the date the credit is applied")
	}
	if len(a.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(a.Memo), MaxMemo), "Shorten it")
	}
	cur := a.Amount.Currency()
	if a.Amount.Sign() <= 0 {
		add(CodePaymentAmount, "amount", fmt.Sprintf("the amount applied is %s %s", a.Amount, cur), "Enter a positive amount")
	}
	for _, r := range []struct {
		field string
		rate  money.Rate
	}{{"invoice", a.InvoiceRate}, {"credit", a.CreditRate}} {
		if r.rate.From != cur || r.rate.To != s.Base {
			add(CodeWrongRate, r.field, fmt.Sprintf("the %s's rate converts %s into %s, not %s into %s", r.field, r.rate.From, r.rate.To, cur, s.Base),
				"Apply a credit to an invoice of its own currency")
		}
	}
	for _, c := range []struct {
		field  string
		clears *money.Money
		rate   money.Rate
	}{{"invoice", a.InvoiceClears, a.InvoiceRate}, {"credit", a.CreditClears, a.CreditRate}} {
		if c.clears == nil || len(probs) > 0 {
			continue
		}
		base, residual, err := cleared(a.Amount, c.rate, c.clears)
		if err != nil || c.clears.Currency() != s.Base || c.clears.Sign() <= 0 {
			add(CodePaymentAmount, c.field, fmt.Sprintf("the %s's carrying amount left is %s %s", c.field, c.clears, c.clears.Currency()), fmt.Sprintf("It is a positive amount in %s", s.Base))
			continue
		}
		if r := residual.Decimal(); r.Abs(r).Cmp(apd.New(MaxResidual, 0)) > 0 || (!residual.IsZero() && cur == s.Base) {
			add(CodePaymentAmount, c.field, fmt.Sprintf("the %s carries %s %s, but %s %s at its rate is %s", c.field, c.clears, s.Base, a.Amount, cur, base),
				"Apply at most what is open; a larger difference is a write-off")
		}
	}
	ar, ok := s.byRole(RoleAccountsReceivable)
	if !ok {
		add(CodeSystemRole, "invoice", "the chart has no accounts receivable account", "Load the standard chart's 1100 Accounts receivable, or give an asset account the role")
	}
	if len(probs) > 0 {
		return Entry{}, CustomerApplied{}, probs
	}
	invoiceBase, invoiceResidual, err := cleared(a.Amount, a.InvoiceRate, a.InvoiceClears)
	if err != nil {
		return Entry{}, CustomerApplied{}, Problems{{CodeBaseRange, "amount", err.Error(), "Check the amount"}}
	}
	creditBase, creditResidual, err := cleared(a.Amount, a.CreditRate, a.CreditClears)
	if err != nil {
		return Entry{}, CustomerApplied{}, Problems{{CodeBaseRange, "amount", err.Error(), "Check the amount"}}
	}
	var out CustomerApplied
	if out.Invoice, err = invoiceBase.Add(invoiceResidual); err != nil {
		return Entry{}, CustomerApplied{}, err
	}
	if out.Credit, err = creditBase.Add(creditResidual); err != nil {
		return Entry{}, CustomerApplied{}, err
	}
	if out.RealisedFX, err = out.Credit.Sub(out.Invoice); err != nil {
		return Entry{}, CustomerApplied{}, err
	}
	y, m, d := a.Date.Date()
	e := Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceCustomerCreditApplication, ID: a.ID.String(), Version: a.Version},
		RuleVersion:     RuleCustomerCreditApplication,
		PostingIdentity: CustomerCreditApplicationIdentity(a.ID, a.Version, RuleCustomerCreditApplication),
		Memo:            a.Memo,
	}
	if out.RealisedFX.IsZero() {
		return e, out, nil // the two carrying amounts are equal: an allocation alone posts nothing
	}
	fxAcc, ok := s.byRole(RoleFXRounding)
	if !ok {
		return Entry{}, CustomerApplied{}, Problems{{CodeSystemRole, "invoice", "the chart has no FX and rounding account for the realised FX",
			"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
	}
	e.Lines = append(e.Lines, Line{AccountID: ar.ID, Amount: a.Amount.Neg(), Rate: a.InvoiceRate, Base: invoiceBase.Neg(), Description: a.Memo})
	if !invoiceResidual.IsZero() {
		e.Lines = append(e.Lines, Line{AccountID: ar.ID, Amount: money.Zero(cur), Rate: a.InvoiceRate, Base: invoiceResidual.Neg(), Description: "Settlement residual: RND-FX of the parts"})
	}
	e.Lines = append(e.Lines, Line{AccountID: ar.ID, Amount: a.Amount, Rate: a.CreditRate, Base: creditBase, Description: a.Memo})
	if !creditResidual.IsZero() {
		e.Lines = append(e.Lines, Line{AccountID: ar.ID, Amount: money.Zero(cur), Rate: a.CreditRate, Base: creditResidual, Description: "Settlement residual: RND-FX of the parts"})
	}
	gain := out.RealisedFX.Neg()
	e.Lines = append(e.Lines, Line{AccountID: fxAcc.ID, Amount: gain, Rate: money.Identity(s.Base), Base: gain, Description: "Realised FX on applying a credit"})
	return e, out, nil
}

// CustomerCreditApplicationRule turns one version of a customer credit application into its entry under one rule
// version.
type CustomerCreditApplicationRule func(a CustomerCreditApplication, s Settings) (Entry, CustomerApplied, error)

// CustomerCreditApplicationRules are every customer credit application rule version the engine has posted with, by
// name: like Rules, a version is never removed or changed while an entry names it.
var CustomerCreditApplicationRules = map[string]CustomerCreditApplicationRule{
	RuleCustomerCreditApplication: PostCustomerCreditApplication,
}

// ReplayCustomerCreditApplication re-runs the rule version that posted a customer credit application's entry over the
// same version (ENG-06).
func ReplayCustomerCreditApplication(rule string, a CustomerCreditApplication, s Settings) (Entry, CustomerApplied, error) {
	f, ok := CustomerCreditApplicationRules[rule]
	if !ok {
		return Entry{}, CustomerApplied{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(a, s)
}
