package ledger

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The customer receipt's source type and rule version (the customer half of the posting rule "Customer and supplier
// payments", PRD-001 §10.2; ENG-41; FR-AR-18; the owner's decision of 24 Oct 2026 to record receipts by hand until bank
// import).
const (
	SourceCustomerReceipt = "customer_receipt"
	RuleCustomerReceipt   = "customer_receipt@1"
)

// ReceivedInvoice is one invoice a receipt settles: the amount received of it in its currency, and the invoice's stored
// rate into the base currency, which its accounts receivable line carries (ENG-14). Clears is the invoice's carrying
// amount left in the base currency when the receipt settles all that is open of it; nil otherwise: what the last part
// leaves of RND-FX is the residual, cleared to 7030 in the month of the last receipt (Appendix B, "Settlement
// difference"), so no cent of the invoice stays on 1100.
type ReceivedInvoice struct {
	InvoiceID  uuid.UUID
	Amount     money.Money
	StoredRate money.Rate
	Clears     *money.Money
}

// CustomerReceipt is one version of money a customer paid, as the rules read it: the money account it came into, the
// invoices' currency, the receipt's rate into the base currency, what a base-currency account received for invoices in
// another currency, what it settles, and what is left over as a credit with the customer.
type CustomerReceipt struct {
	ID        uuid.UUID
	Version   int
	Date      time.Time
	Memo      string    // the customer and the receipt's reference
	Account   uuid.UUID // the money account it came into
	Currency  money.Currency
	Rate      *money.Rate  // the receipt's rate into the base currency; nil for a receipt in the base currency
	Received  *money.Money // what came into a base-currency money account for invoices in another currency; nil otherwise
	Invoices  []ReceivedInvoice
	Unapplied money.Money // in the invoices' currency: kept as a credit with the customer (FR-AR-18)
}

// CustomerReceiptIdentity is the posting identity of a customer receipt version under a rule version (§11.4).
func CustomerReceiptIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceCustomerReceipt, id, version, rule)
}

// asReceipt words a payment rule's problems for a receipt, and names its fields.
var asReceipt = strings.NewReplacer(
	"the money left the account", "the money came into the account",
	"Enter what it paid of each bill, or the amount paid", "Enter what it received of each invoice, or the amount received",
	"the payment settles nothing and leaves nothing unapplied", "the receipt settles nothing and leaves nothing unapplied",
	"what it paid implies", "what it received implies",
	"enter what it paid in", "enter what it received in",
	"Enter the amount the account statement shows", "Enter the amount the account statement shows came in",
	"only bills in another currency paid from a base-currency account name what the account paid", "only invoices in another currency received into a base-currency account name what the account received",
	"what the account paid", "what the account received",
	"the bill carries", "the invoice carries",
	"Settle what is open of the bill", "Settle what is open of the invoice",
	"the bill's stored rate is not of the payment's currency", "the invoice's stored rate is not of the receipt's currency",
	"the bills are", "the invoices are",
	"the bills'", "the invoices'",
	"Pay bills of one currency together", "Receive invoices of one currency together",
	"Pay from an account in the bills' currency, or in the base currency", "Receive into an account in the invoices' currency, or in the base currency",
	"the bank, card or wallet account it was paid from", "the bank, card or wallet account it came into",
	"a credit with the supplier", "a credit with the customer",
	"the payment's", "the receipt's",
	"the payment", "the receipt",
	"payment's", "receipt's",
)

var receiptFields = strings.NewReplacer("bills[", "invoices[", "].bill", "].invoice_id", "paid", "received", "bills", "invoices")

// PostCustomerReceipt turns one version of a customer receipt into its entry (customer_receipt@1) and the settlements it
// records: the legs bill_payment@1 posts for a payment of the same amounts of bills carried as the invoices are, on
// accounts receivable in place of accounts payable, every amount and base amount negated:
//   - Cr 1100 with each invoice's amount settled, at that invoice's stored rate (its carrying amount: ENG-14), and for
//     the part that settles what is left of an invoice in another currency, a base-only line on 1100 with the residual
//     (at most MaxResidual);
//   - Cr 1100 with any unapplied amount, at the receipt's rate: a credit with the customer (FR-AR-18);
//   - Dr the money account with what came in: the total at the receipt's rate, or, for invoices in another currency
//     received into a base-currency account, what the account received in the base currency;
//   - and what balances the entry in the base currency to 7030: the realised FX (ENG-41), the residuals, and any cent
//     RND-FX leaves between the lines.
//
// Each settlement's realised FX is what came in less what the invoice carried: a gain (+) when more came in. Every
// refusal is the payment rule's, worded for a receipt (ENG-22).
func PostCustomerReceipt(r CustomerReceipt, s Settings) (Entry, []Settlement, error) {
	p := BillPayment{ID: r.ID, Version: r.Version, Date: r.Date, Memo: r.Memo, Account: r.Account, Currency: r.Currency, Rate: r.Rate, Paid: r.Received,
		Unapplied: r.Unapplied}
	for _, x := range r.Invoices {
		p.Bills = append(p.Bills, PaidBill{BillID: x.InvoiceID, Amount: x.Amount, StoredRate: x.StoredRate, Clears: x.Clears})
	}
	e, settled, err := postPayment(p, s, RoleAccountsReceivable)
	if probs, ok := err.(Problems); ok {
		for i := range probs {
			probs[i].Message, probs[i].Fix = asReceipt.Replace(probs[i].Message), asReceipt.Replace(probs[i].Fix)
			probs[i].Field = receiptFields.Replace(probs[i].Field)
		}
		return Entry{}, nil, probs
	}
	if err != nil {
		return Entry{}, nil, err
	}
	for i := range e.Lines {
		e.Lines[i].Amount, e.Lines[i].Base = e.Lines[i].Amount.Neg(), e.Lines[i].Base.Neg()
		switch e.Lines[i].Description {
		case "Realised FX and rounding on settlement":
			e.Lines[i].Description = "Realised FX and rounding on a receipt"
		case "Unapplied: a credit with the supplier":
			e.Lines[i].Description = "Unapplied: a credit with the customer"
		}
	}
	e.Source = Source{Type: SourceCustomerReceipt, ID: r.ID.String(), Version: r.Version}
	e.RuleVersion = RuleCustomerReceipt
	e.PostingIdentity = CustomerReceiptIdentity(r.ID, r.Version, RuleCustomerReceipt)
	for i := range settled {
		settled[i].RealisedFX = settled[i].RealisedFX.Neg()
	}
	return e, settled, nil
}

// CustomerReceiptRule turns one version of a customer receipt into its entry and settlements under one rule version.
type CustomerReceiptRule func(r CustomerReceipt, s Settings) (Entry, []Settlement, error)

// CustomerReceiptRules are every customer receipt rule version the engine has posted with, by name: like Rules, a
// version is never removed or changed while an entry names it.
var CustomerReceiptRules = map[string]CustomerReceiptRule{
	RuleCustomerReceipt: PostCustomerReceipt,
}

// ReplayCustomerReceipt re-runs the rule version that posted a customer receipt's entry over the same version (ENG-06).
func ReplayCustomerReceipt(rule string, r CustomerReceipt, s Settings) (Entry, []Settlement, error) {
	f, ok := CustomerReceiptRules[rule]
	if !ok {
		return Entry{}, nil, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(r, s)
}
