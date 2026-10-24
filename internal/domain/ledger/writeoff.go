package ledger

import (
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The write-off's source type and rule version (FR-AR-17; FR-BNK-08's reason-coded adjustment; the posting rule
// "Bad-debt write-off", §10.2; p1-sales task 2.3).
const (
	SourceWriteOff = "write_off"
	RuleWriteOff   = "write_off@1"
)

// WriteOff is one version of the write-off of what is left of an invoice, as the rule reads it: the invoice and the
// amount written off in its currency, the invoice's stored rate (which its accounts receivable line carries), Clears
// (the invoice's carrying amount left when the write-off takes all that is open of it; nil otherwise), and the account
// its reason names, which takes the carrying amount in the base currency.
type WriteOff struct {
	ID         uuid.UUID
	Version    int
	Date       time.Time
	Memo       string // the invoice, the customer and the reason
	InvoiceID  uuid.UUID
	Amount     money.Money
	StoredRate money.Rate
	Clears     *money.Money
	Account    uuid.UUID
}

// WriteOffIdentity is the posting identity of a write-off version under a rule version (§11.4).
func WriteOffIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceWriteOff, id, version, rule)
}

// PostWriteOff turns one version of a write-off into its entry (write_off@1) and what it settled of the invoice:
//   - Cr 1100 with the amount at the invoice's stored rate (its carrying amount: ENG-14), and when it takes all that is
//     open, a base-only line on 1100 with the residual RND-FX left (at most MaxResidual), so no cent stays;
//   - Dr the reason's account, in the base currency, with the carrying amount cleared.
//
// No realised FX: nothing came in. Every rule is checked first and every failure returned together (ENG-22).
func PostWriteOff(w WriteOff, s Settings) (Entry, Settlement, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if w.Date.IsZero() {
		add(CodeNoDate, "date", "the write-off has no date", "Enter the date it is written off")
	}
	if len(w.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(w.Memo), MaxMemo), "Shorten the reason")
	}
	ar, hasAR := s.byRole(RoleAccountsReceivable)
	if !hasAR {
		add(CodeSystemRole, "invoice_id", "the chart has no accounts receivable account", "Load the standard chart's 1100 Accounts receivable, or give an asset account the role")
	}
	acc, known := s.Accounts[w.Account]
	switch {
	case !known:
		add(CodeUnknownAccount, "account_id", "the account does not exist in this organisation", "Choose the account the reason writes off to")
	case acc.Group:
		add(CodeGroupAccount, "account_id", fmt.Sprintf("account %s is a group heading", acc.Code), "Choose an account under it")
	case !acc.Active:
		add(CodeInactiveAccount, "account_id", fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it, or choose another")
	case acc.Role == RoleAccountsReceivable || acc.Role == RoleGSTControl || acc.Role == RoleDeferredRevenue || acc.ShareCapital:
		add(CodeRuleAccount, "account_id", fmt.Sprintf("account %s is not one a write-off takes", acc.Code),
			"Choose the expense or revenue account the reason writes off to")
	}
	var base money.Money
	switch {
	case w.Amount.Sign() <= 0:
		add(CodePaymentAmount, "amount", fmt.Sprintf("the amount written off is %s %s", w.Amount, w.Amount.Currency()), "Enter a positive amount in the invoice's currency")
	case w.StoredRate.From != w.Amount.Currency() || w.StoredRate.To != s.Base:
		add(CodeWrongRate, "invoice_id", "the invoice's stored rate is not of the amount's currency", "Write off in the invoice's currency")
	default:
		var err error
		if base, err = rounding.FX(w.Amount, w.StoredRate); err != nil {
			add(CodeBaseRange, "amount", err.Error(), "Check the amount")
		} else if w.Clears != nil {
			residual, _ := w.Clears.Sub(base)
			if w.Clears.Currency() != s.Base || w.Clears.Sign() <= 0 {
				add(CodePaymentAmount, "invoice_id", fmt.Sprintf("the carrying amount left is %s %s", w.Clears, w.Clears.Currency()), fmt.Sprintf("It is a positive amount in %s", s.Base))
			} else if r := residual.Decimal(); r.Abs(r).Cmp(apd.New(MaxResidual, 0)) > 0 || (!residual.IsZero() && w.Amount.Currency() == s.Base) {
				add(CodePaymentAmount, "amount", fmt.Sprintf("the invoice carries %s %s, but %s %s at its rate is %s", w.Clears, s.Base, w.Amount, w.Amount.Currency(), base),
					"Write off what is open of the invoice")
			}
		}
	}
	if len(probs) > 0 {
		return Entry{}, Settlement{}, probs
	}
	cleared := base
	lines := []Line{{AccountID: ar.ID, Amount: w.Amount.Neg(), Rate: w.StoredRate, Base: base.Neg(), Description: w.Memo}}
	if w.Clears != nil && !w.Clears.Equal(base) {
		residual, _ := w.Clears.Sub(base)
		lines = append(lines, Line{AccountID: ar.ID, Amount: money.Zero(w.Amount.Currency()), Rate: w.StoredRate, Base: residual.Neg(),
			Description: "Settlement residual: RND-FX of the parts"})
		cleared = *w.Clears
	}
	id := money.Identity(s.Base)
	lines = append(lines, Line{AccountID: w.Account, Amount: cleared, Rate: id, Base: cleared, Description: w.Memo})
	y, m, d := w.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceWriteOff, ID: w.ID.String(), Version: w.Version},
		RuleVersion:     RuleWriteOff,
		PostingIdentity: WriteOffIdentity(w.ID, w.Version, RuleWriteOff),
		Memo:            w.Memo,
		Lines:           lines,
	}, Settlement{DocumentID: w.InvoiceID, Amount: w.Amount, Base: cleared, RealisedFX: money.Zero(s.Base), Tolerance: money.Zero(s.Base)}, nil
}

// WriteOffRule turns one version of a write-off into its entry and settlement under one rule version.
type WriteOffRule func(w WriteOff, s Settings) (Entry, Settlement, error)

// WriteOffRules are every write-off rule version the engine has posted with, by name: like Rules, a version is never
// removed or changed while an entry names it.
var WriteOffRules = map[string]WriteOffRule{
	RuleWriteOff: PostWriteOff,
}

// ReplayWriteOff re-runs the rule version that posted a write-off's entry over the same version (ENG-06).
func ReplayWriteOff(rule string, w WriteOff, s Settings) (Entry, Settlement, error) {
	f, ok := WriteOffRules[rule]
	if !ok {
		return Entry{}, Settlement{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(w, s)
}
