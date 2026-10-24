package ledger

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The customer refund's source type and rule version (FR-AR-04: refunds; FR-AR-18: an unapplied receipt refunded, "Dr
// 1100, Cr Bank"; p1-sales task 3.1).
const (
	SourceCustomerRefund = "customer_refund"
	RuleCustomerRefund   = "customer_refund@1"
)

// CustomerRefund is one version of money paid back to a customer against a credit with it, as the rule reads it: the
// money account it left, the credit's currency and the refund's rate, what a base-currency account paid for a credit in
// another currency, and the credit it settles (Credit's StoredRate the rate the credit carries; its Clears the credit's
// carrying amount left when the refund takes all that is left of it).
type CustomerRefund struct {
	ID       uuid.UUID
	Version  int
	Date     time.Time
	Memo     string
	Account  uuid.UUID
	Currency money.Currency
	Rate     *money.Rate
	Paid     *money.Money
	Credit   PaidBill
}

// CustomerRefundIdentity is the posting identity of a customer refund version under a rule version (§11.4).
func CustomerRefundIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceCustomerRefund, id, version, rule)
}

// asCustomerRefund words a payment rule's problems for a refund to a customer, and customerRefundFields names its fields.
var (
	asCustomerRefund = strings.NewReplacer(
		"Enter what it paid of each bill, or the amount paid", "Enter what the customer was paid back",
		"the payment settles nothing and leaves nothing unapplied", "the refund settles nothing",
		"only bills in another currency paid from a base-currency account name what the account paid",
		"only a credit in another currency refunded from a base-currency account names what the account paid",
		"the bills are", "the credit is",
		"the bills'", "the credit's",
		"the bill's", "the credit's",
		"Pay bills of one currency together", "Refund a credit in its own currency",
		"Pay from", "Refund from",
		"the amount settled", "the amount refunded",
		"the payment's", "the refund's",
		"the payment", "the refund",
		"payment's", "refund's",
	)
	customerRefundFields = strings.NewReplacer("bills[1].", "", "bills", "credit_id")
)

// PostCustomerRefund turns one version of a customer refund into its entry (customer_refund@1) and what it settled of
// the credit: the legs a payment of bills posts for the same amount of a document carried as the credit is, on accounts
// receivable: Dr 1100 with the amount at the credit's rate (and the residual when it takes the last of the credit), Cr
// the money account with what left it, and the realised FX to 7030. Its settlement's realised FX is what the credit
// carried less what was paid: a gain (+) when less went out. Every refusal is the payment rule's, worded for a refund
// (ENG-22).
func PostCustomerRefund(r CustomerRefund, s Settings) (Entry, Settlement, error) {
	p := BillPayment{ID: r.ID, Version: r.Version, Date: r.Date, Memo: r.Memo, Account: r.Account, Currency: r.Currency, Rate: r.Rate, Paid: r.Paid,
		Bills: []PaidBill{r.Credit}, Unapplied: money.Zero(r.Currency)}
	e, settled, err := postPayment(p, s, RoleAccountsReceivable)
	if probs, ok := err.(Problems); ok {
		for i := range probs {
			probs[i].Message, probs[i].Fix = asCustomerRefund.Replace(probs[i].Message), asCustomerRefund.Replace(probs[i].Fix)
			probs[i].Field = customerRefundFields.Replace(probs[i].Field)
		}
		return Entry{}, Settlement{}, probs
	}
	if err != nil {
		return Entry{}, Settlement{}, err
	}
	for i := range e.Lines {
		if e.Lines[i].Description == "Realised FX and rounding on settlement" {
			e.Lines[i].Description = "Realised FX and rounding on a refund"
		}
	}
	e.Source = Source{Type: SourceCustomerRefund, ID: r.ID.String(), Version: r.Version}
	e.RuleVersion = RuleCustomerRefund
	e.PostingIdentity = CustomerRefundIdentity(r.ID, r.Version, RuleCustomerRefund)
	return e, settled[0], nil
}

// CustomerRefundRule turns one version of a customer refund into its entry under one rule version.
type CustomerRefundRule func(r CustomerRefund, s Settings) (Entry, Settlement, error)

// CustomerRefundRules are every customer refund rule version the engine has posted with, by name: like Rules, a
// version is never removed or changed while an entry names it.
var CustomerRefundRules = map[string]CustomerRefundRule{
	RuleCustomerRefund: PostCustomerRefund,
}

// ReplayCustomerRefund re-runs the rule version that posted a customer refund's entry over the same version (ENG-06).
func ReplayCustomerRefund(rule string, r CustomerRefund, s Settings) (Entry, Settlement, error) {
	f, ok := CustomerRefundRules[rule]
	if !ok {
		return Entry{}, Settlement{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(r, s)
}
