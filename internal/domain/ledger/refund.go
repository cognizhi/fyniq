package ledger

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The supplier refund's source type and rule version (FR-AP-03: a refund from a vendor; PRD-001 §10.2's supplier
// payment the other way).
const (
	SourceSupplierRefund = "supplier_refund"
	RuleSupplierRefund   = "supplier_refund@1"
)

// SupplierRefund is one version of money a supplier paid back against a credit with it, as the rule reads it: the money
// account it came into, the credit's currency and the refund's rate, what a base-currency account received for a credit
// in another currency, and the credit it settles (Credit's StoredRate the rate the credit carries; its Clears the
// credit's carrying amount left when the refund takes all that is left of it).
type SupplierRefund struct {
	ID       uuid.UUID
	Version  int
	Date     time.Time
	Memo     string
	Account  uuid.UUID
	Currency money.Currency
	Rate     *money.Rate
	Received *money.Money
	Credit   PaidBill
}

// SupplierRefundIdentity is the posting identity of a supplier refund version under a rule version (§11.4).
func SupplierRefundIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceSupplierRefund, id, version, rule)
}

// asRefund words a payment rule's problems for a refund, and names its fields.
var asRefund = strings.NewReplacer(
	"the money left the account", "the money came into the account",
	"Enter what it paid of each bill, or the amount paid", "Enter what the supplier paid back",
	"the payment settles nothing and leaves nothing unapplied", "the refund settles nothing",
	"what it paid implies", "what it received implies",
	"enter what it paid in", "enter what it received in",
	"Enter the amount the account statement shows", "Enter the amount the account statement shows came in",
	"only bills in another currency paid from a base-currency account name what the account paid", "only a credit in another currency refunded into a base-currency account names what the account received",
	"what the account paid", "what the account received",
	"the bills are", "the credit is",
	"the bills'", "the credit's",
	"Pay bills of one currency together", "Refund a credit into an account of its currency, or of the base currency",
	"Pay from an account in the bills' currency, or in the base currency", "Refund into an account in the credit's currency, or in the base currency",
	"the bank, card or wallet account it was paid from", "the bank, card or wallet account it came into",
	"the amount settled", "the amount refunded",
	"the payment's", "the refund's",
	"the payment", "the refund",
	"payment's", "refund's",
)

var refundFields = strings.NewReplacer("bills[1].", "", "paid", "received", "bills", "credit_id")

// PostSupplierRefund turns one version of a supplier refund into its entry (supplier_refund@1) and what it settled of
// the credit: the legs bill_payment@1 posts for a payment of the same amount of a bill carried as the credit is, every
// amount and base amount negated: Cr 2010 with the amount at the credit's rate (and the residual when it takes the
// last of the credit), Dr the money account with what came in, and the realised FX to 7030 the other way. Its
// settlement's realised FX is what came in less what the credit carried: a gain (+) when more came in. Every refusal is
// the payment rule's, worded for a refund (ENG-22).
func PostSupplierRefund(r SupplierRefund, s Settings) (Entry, Settlement, error) {
	p := BillPayment{ID: r.ID, Version: r.Version, Date: r.Date, Memo: r.Memo, Account: r.Account, Currency: r.Currency, Rate: r.Rate, Paid: r.Received,
		Bills: []PaidBill{r.Credit}, Unapplied: money.Zero(r.Currency)}
	e, settled, err := PostBillPayment(p, s)
	if probs, ok := err.(Problems); ok {
		for i := range probs {
			probs[i].Message, probs[i].Fix = asRefund.Replace(probs[i].Message), asRefund.Replace(probs[i].Fix)
			probs[i].Field = refundFields.Replace(probs[i].Field)
		}
		return Entry{}, Settlement{}, probs
	}
	if err != nil {
		return Entry{}, Settlement{}, err
	}
	for i := range e.Lines {
		e.Lines[i].Amount, e.Lines[i].Base = e.Lines[i].Amount.Neg(), e.Lines[i].Base.Neg()
		if e.Lines[i].Description == "Realised FX and rounding on settlement" {
			e.Lines[i].Description = "Realised FX and rounding on a refund"
		}
	}
	e.Source = Source{Type: SourceSupplierRefund, ID: r.ID.String(), Version: r.Version}
	e.RuleVersion = RuleSupplierRefund
	e.PostingIdentity = SupplierRefundIdentity(r.ID, r.Version, RuleSupplierRefund)
	out := settled[0]
	out.RealisedFX = out.RealisedFX.Neg()
	return e, out, nil
}

// SupplierRefundRule turns one version of a supplier refund into its entry under one rule version.
type SupplierRefundRule func(r SupplierRefund, s Settings) (Entry, Settlement, error)

// SupplierRefundRules are every supplier refund rule version the engine has posted with, by name: like Rules, a
// version is never removed or changed while an entry names it.
var SupplierRefundRules = map[string]SupplierRefundRule{
	RuleSupplierRefund: PostSupplierRefund,
}

// ReplaySupplierRefund re-runs the rule version that posted a supplier refund's entry over the same version (ENG-06).
func ReplaySupplierRefund(rule string, r SupplierRefund, s Settings) (Entry, Settlement, error) {
	f, ok := SupplierRefundRules[rule]
	if !ok {
		return Entry{}, Settlement{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(r, s)
}
