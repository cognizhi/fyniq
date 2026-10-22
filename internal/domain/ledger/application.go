package ledger

import (
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The credit application's source type and rule version (FR-AP-03, FR-AR-18; ENG-41).
const (
	SourceCreditApplication = "credit_application"
	RuleCreditApplication   = "credit_application@1"
)

// CreditApplication is one version of a supplier credit applied to one of the supplier's bills, as the rule reads it:
// the amount in their currency, the bill's stored rate and the credit's carrying rate (a vendor credit's stored rate,
// or the rate of the payment that left the amount unapplied), and, when the application settles all that is open of
// either, the carrying amount left of it, which it clears (as a payment's last part does: PaidBill).
type CreditApplication struct {
	ID           uuid.UUID
	Version      int
	Date         time.Time
	Memo         string
	Amount       money.Money
	BillRate     money.Rate
	CreditRate   money.Rate
	BillClears   *money.Money
	CreditClears *money.Money
}

// Applied is what an application cleared: the bill's carrying amount, the credit's, and the realised gain (+) or loss
// (−) the two make in the base currency (ENG-41): what the bill carried less what the credit carried.
type Applied struct {
	Bill, Credit, RealisedFX money.Money
}

// CreditApplicationIdentity is the posting identity of a credit application version under a rule version (§11.4).
func CreditApplicationIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceCreditApplication, id, version, rule)
}

// cleared is the carrying amount a part clears at a rate, and the residual a carrying amount left makes of it.
func cleared(amount money.Money, rate money.Rate, clears *money.Money) (money.Money, money.Money, error) {
	base, err := rounding.FX(amount, rate)
	if err != nil {
		return money.Money{}, money.Money{}, err
	}
	residual := money.Zero(rate.To)
	if clears != nil {
		if residual, err = clears.Sub(base); err != nil {
			return money.Money{}, money.Money{}, err
		}
	}
	return base, residual, nil
}

// PostCreditApplication turns one version of a credit application into its entry (credit_application@1) and what it
// cleared. Applying a credit settles the bill and the credit by the same amount, so 2010's balance in their currency
// does not move; only the two carrying amounts can differ, by the rates they were booked at:
//   - Dr 2010 with the amount at the bill's stored rate, and Cr 2010 with it at the credit's carrying rate, each with a
//     base-only line for the residual when it settles what is left of its document (at most MaxResidual);
//   - and what makes the entry balance in the base currency to 7030: the realised FX (ENG-41).
//
// When the two carrying amounts cleared are equal, as for a bill and a credit in the base currency, the application
// posts nothing and the entry has no lines: it is an allocation alone. Every rule is checked first and every failure
// returned together (ENG-22).
func PostCreditApplication(a CreditApplication, s Settings) (Entry, Applied, error) {
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
	}{{"bill", a.BillRate}, {"credit", a.CreditRate}} {
		if r.rate.From != cur || r.rate.To != s.Base {
			add(CodeWrongRate, r.field, fmt.Sprintf("the %s's rate converts %s into %s, not %s into %s", r.field, r.rate.From, r.rate.To, cur, s.Base),
				"Apply a credit to a bill of its own currency")
		}
	}
	for _, c := range []struct {
		field  string
		clears *money.Money
		rate   money.Rate
	}{{"bill", a.BillClears, a.BillRate}, {"credit", a.CreditClears, a.CreditRate}} {
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
	ap, ok := s.byRole(RoleAccountsPayable)
	if !ok {
		add(CodeSystemRole, "bill", "the chart has no accounts payable account", "Load the standard chart's 2010 Accounts payable, or give a liability account the role")
	}
	if len(probs) > 0 {
		return Entry{}, Applied{}, probs
	}
	billBase, billResidual, err := cleared(a.Amount, a.BillRate, a.BillClears)
	if err != nil {
		return Entry{}, Applied{}, Problems{{CodeBaseRange, "amount", err.Error(), "Check the amount"}}
	}
	creditBase, creditResidual, err := cleared(a.Amount, a.CreditRate, a.CreditClears)
	if err != nil {
		return Entry{}, Applied{}, Problems{{CodeBaseRange, "amount", err.Error(), "Check the amount"}}
	}
	out := Applied{Bill: billBase, Credit: creditBase}
	if out.Bill, err = billBase.Add(billResidual); err != nil {
		return Entry{}, Applied{}, err
	}
	if out.Credit, err = creditBase.Add(creditResidual); err != nil {
		return Entry{}, Applied{}, err
	}
	if out.RealisedFX, err = out.Bill.Sub(out.Credit); err != nil {
		return Entry{}, Applied{}, err
	}
	y, m, d := a.Date.Date()
	e := Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceCreditApplication, ID: a.ID.String(), Version: a.Version},
		RuleVersion:     RuleCreditApplication,
		PostingIdentity: CreditApplicationIdentity(a.ID, a.Version, RuleCreditApplication),
		Memo:            a.Memo,
	}
	if out.RealisedFX.IsZero() {
		return e, out, nil // the two carrying amounts are equal: an allocation alone posts nothing
	}
	fxAcc, ok := s.byRole(RoleFXRounding)
	if !ok {
		return Entry{}, Applied{}, Problems{{CodeSystemRole, "bill", "the chart has no FX and rounding account for the realised FX",
			"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
	}
	e.Lines = append(e.Lines, Line{AccountID: ap.ID, Amount: a.Amount, Rate: a.BillRate, Base: billBase, Description: a.Memo})
	if !billResidual.IsZero() {
		e.Lines = append(e.Lines, Line{AccountID: ap.ID, Amount: money.Zero(cur), Rate: a.BillRate, Base: billResidual, Description: "Settlement residual: RND-FX of the parts"})
	}
	e.Lines = append(e.Lines, Line{AccountID: ap.ID, Amount: a.Amount.Neg(), Rate: a.CreditRate, Base: creditBase.Neg(), Description: a.Memo})
	if !creditResidual.IsZero() {
		e.Lines = append(e.Lines, Line{AccountID: ap.ID, Amount: money.Zero(cur), Rate: a.CreditRate, Base: creditResidual.Neg(), Description: "Settlement residual: RND-FX of the parts"})
	}
	gain := out.RealisedFX.Neg()
	e.Lines = append(e.Lines, Line{AccountID: fxAcc.ID, Amount: gain, Rate: money.Identity(s.Base), Base: gain, Description: "Realised FX on applying a credit"})
	return e, out, nil
}

// CreditApplicationRule turns one version of a credit application into its entry under one rule version.
type CreditApplicationRule func(a CreditApplication, s Settings) (Entry, Applied, error)

// CreditApplicationRules are every credit application rule version the engine has posted with, by name: like Rules, a
// version is never removed or changed while an entry names it.
var CreditApplicationRules = map[string]CreditApplicationRule{
	RuleCreditApplication: PostCreditApplication,
}

// ReplayCreditApplication re-runs the rule version that posted a credit application's entry over the same version
// (ENG-06).
func ReplayCreditApplication(rule string, a CreditApplication, s Settings) (Entry, Applied, error) {
	f, ok := CreditApplicationRules[rule]
	if !ok {
		return Entry{}, Applied{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(a, s)
}
