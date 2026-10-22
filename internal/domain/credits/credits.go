// Package credits applies a credit with a supplier to one of the supplier's bills (FR-AP-03, FR-AR-18): a posted
// vendor credit, or what a payment left unapplied. Reading an application for the rule happens here: the credit and
// the bill as they stand (posted, the supplier's, in one currency, with what is open of each and its carrying amount
// left), and the rates they were booked at. The posting service in package journal writes it (the one posting path).
package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of an application's refusals (ENG-23).
const (
	CodeNotApplication = "CREDIT-01" // the document is not a credit application
	CodeBadDraft       = "CREDIT-02" // the saved application cannot be read
	CodeCredit         = "CREDIT-03" // not a posted vendor credit, or a payment with something unapplied
	CodeBill           = "CREDIT-04" // not a posted bill of the credit's supplier in its currency
	CodeTooMuch        = "CREDIT-05" // more than the bill has open, or than is left of the credit
	CodeNotPosted      = "CREDIT-06" // only a posted application is voided
	CodeVoidDate       = "CREDIT-07" // a void dated before the application
	CodeApplied        = "CREDIT-08" // a credit that is applied is not voided until its applications are
)

// Error is an application command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is an application as entered: the credit, the bill, the date and the amount, in their currency.
type Draft struct {
	CreditID uuid.UUID
	BillID   uuid.UUID
	Date     time.Time
	Amount   money.Money
}

type payload struct {
	CreditID uuid.UUID   `json:"credit_id"`
	BillID   uuid.UUID   `json:"bill_id"`
	Date     string      `json:"date"`
	Amount   money.Money `json:"amount"`
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	p := payload{CreditID: d.CreditID, BillID: d.BillID, Amount: d.Amount}
	if !d.Date.IsZero() {
		p.Date = d.Date.Format(time.DateOnly)
	}
	return json.Marshal(p)
}

// Read reads an application version as it was saved.
func Read(doc storage.Document) (Draft, error) {
	if doc.Type != storage.DocumentCreditApplication {
		return Draft{}, &Error{CodeNotApplication, fmt.Sprintf("document %s is a %s, not a credit application", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, &Error{CodeBadDraft, "the saved application cannot be read: " + err.Error(), "Apply the credit again"}
	}
	d := Draft{CreditID: p.CreditID, BillID: p.BillID, Amount: p.Amount}
	if p.Date != "" {
		var err error
		if d.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
			return Draft{}, &Error{CodeBadDraft, fmt.Sprintf("the saved application's date %q cannot be read", p.Date), "Apply the credit again"}
		}
	}
	return d, nil
}

// Credit is a credit with a supplier as an application finds it: the document, its supplier, what is left of it in its
// currency and its carrying amount left in the base currency, and the rate it was booked at.
type Credit struct {
	Doc      storage.Document
	Supplier uuid.UUID
	Left     money.Money
	Carrying money.Money
	Rate     money.Rate
}

// apLines are an entry's lines on the accounts-payable account with an amount, in order.
func apLines(e storage.Entry, s ledger.Settings) []storage.Line {
	var out []storage.Line
	for _, l := range e.Lines {
		if s.Accounts[l.AccountID].Role == ledger.RoleAccountsPayable && !isZero(string(l.Amount)) {
			out = append(out, l)
		}
	}
	return out
}

func isZero(s string) bool {
	for _, c := range s {
		if c != '0' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// lessApplied takes off a credit's or a bill's applications: the allocations that settle it as a document.
func lessApplied(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, left, carrying money.Money) (money.Money, money.Money, error) {
	allocs, err := u.Allocations().OfDocument(ctx, id)
	if err != nil {
		return left, carrying, err
	}
	for _, a := range allocs {
		x, err := money.ParseStored(string(a.Amount), left.Currency())
		if err != nil {
			return left, carrying, err
		}
		y, err := money.ParseStored(string(a.BaseAmount), carrying.Currency())
		if err != nil {
			return left, carrying, err
		}
		if left, err = left.Sub(x); err != nil {
			return left, carrying, err
		}
		if carrying, err = carrying.Sub(y); err != nil {
			return left, carrying, err
		}
	}
	return left, carrying, nil
}

// CreditOf is what is left of a credit: a posted vendor credit (its total and its debit to 2010), or a posted payment's
// unapplied amount (and the line that carries it), less its applications. CodeCredit when it is neither.
func CreditOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (Credit, error) {
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return Credit{}, &Error{CodeCredit, fmt.Sprintf("credit %s does not exist in this organisation", id), "Choose a posted vendor credit, or a payment with an amount unapplied"}
	}
	if err != nil {
		return Credit{}, err
	}
	refuse := &Error{CodeCredit, fmt.Sprintf("%s is not a posted vendor credit or a payment with an amount unapplied", label(doc)),
		"Choose a posted vendor credit, or a payment with an amount unapplied"}
	if doc.Status != storage.DocumentPosted {
		return Credit{}, refuse
	}
	out := Credit{Doc: doc}
	var line storage.Line
	switch doc.Type {
	case storage.DocumentVendorCredit:
		d, err := bill.Read(doc, s.Base)
		if err != nil {
			return Credit{}, err
		}
		e, err := u.Ledger().ByIdentity(ctx, ledger.VendorCreditIdentity(doc.ID, doc.Version, ledger.RuleVendorCredit))
		if err != nil {
			return Credit{}, err
		}
		lines := apLines(e, s)
		if len(lines) != 1 {
			return Credit{}, refuse
		}
		out.Supplier, line = d.SupplierID, lines[0]
	case storage.DocumentBillPayment:
		d, err := billpayment.Read(doc, s.Base)
		if err != nil {
			return Credit{}, err
		}
		e, err := u.Ledger().ByIdentity(ctx, ledger.BillPaymentIdentity(doc.ID, doc.Version, ledger.RuleBillPayment))
		if err != nil {
			return Credit{}, err
		}
		lines := apLines(e, s) // the bills' parts, in order, then the unapplied amount (bill_payment@1)
		if d.Unapplied.IsZero() || len(lines) != len(d.Bills)+1 {
			return Credit{}, refuse
		}
		out.Supplier, line = d.SupplierID, lines[len(lines)-1]
	default:
		return Credit{}, refuse
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return Credit{}, err
	}
	if out.Left, err = money.ParseStored(string(line.Amount), cur); err != nil {
		return Credit{}, err
	}
	if out.Carrying, err = money.ParseStored(string(line.BaseAmount), s.Base); err != nil {
		return Credit{}, err
	}
	if out.Rate, err = money.ParseRate(string(line.FXRate), cur, s.Base, line.FXRateSource); err != nil {
		return Credit{}, err
	}
	if out.Left, out.Carrying, err = lessApplied(ctx, u, doc.ID, out.Left, out.Carrying); err != nil {
		return Credit{}, err
	}
	return out, nil
}

func label(doc storage.Document) string {
	if doc.Number != "" {
		return doc.Number
	}
	return string(doc.Type) + " " + doc.ID.String()
}

// Prepared is an application version as the rule reads it, with the credit and the bill it found.
type Prepared struct {
	Draft       Draft
	Application ledger.CreditApplication
	Credit      Credit
	Bill        billpayment.Open
}

// Prepare reads one version of an application for the rule: the credit and the bill as they stand, both the supplier's
// and in one currency; an amount above what the bill has open or what is left of the credit is refused (CREDIT-05),
// and the application that settles all that is open of either clears its carrying amount left. Lock both documents
// first when the application is to be written.
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Prepared, ledger.Problems, error) {
	d, err := Read(doc)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	c, err := CreditOf(ctx, u, d.CreditID, s)
	var ce *Error
	if errors.As(err, &ce) {
		add(ce.Code, "credit_id", ce.Message, ce.Fix)
	} else if err != nil {
		return Prepared{}, nil, err
	}
	o, err := billpayment.OpenBill(ctx, u, d.BillID, s)
	var be *billpayment.Error
	if errors.As(err, &be) {
		add(CodeBill, "bill_id", be.Message, be.Fix)
	} else if err != nil {
		return Prepared{}, nil, err
	}
	if len(probs) > 0 {
		return Prepared{}, probs, nil
	}
	out.Credit, out.Bill = c, o
	b, err := bill.Read(o.Bill, s.Base)
	if err != nil {
		return Prepared{}, nil, err
	}
	name := o.Number
	switch {
	case b.SupplierID != c.Supplier:
		add(CodeBill, "bill_id", fmt.Sprintf("%s is another supplier's", name), "Apply a credit to its own supplier's bills")
	case o.Total.Currency() != c.Left.Currency():
		add(CodeBill, "bill_id", fmt.Sprintf("%s is in %s; the credit is in %s", name, o.Total.Currency(), c.Left.Currency()), "Apply a credit to a bill of its currency")
	}
	if len(probs) > 0 {
		return Prepared{}, probs, nil
	}
	a := ledger.CreditApplication{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: fmt.Sprintf("%s applied to %s", label(c.Doc), name),
		Amount: d.Amount, BillRate: o.Rate, CreditRate: c.Rate}
	if d.Amount.Currency() == c.Left.Currency() {
		for _, x := range []struct {
			field, what string
			open        money.Money
			carrying    money.Money
			clears      **money.Money
		}{{"amount", name + " has", o.Open, o.Carrying, &a.BillClears}, {"amount", label(c.Doc) + " has", c.Left, c.Carrying, &a.CreditClears}} {
			cmp, err := d.Amount.Cmp(x.open)
			if err != nil {
				return Prepared{}, nil, err
			}
			switch {
			case cmp > 0:
				add(CodeTooMuch, x.field, fmt.Sprintf("%s %s %s left; the application is %s", x.what, x.open, x.open.Currency(), d.Amount),
					"Apply at most what the bill has open and what is left of the credit")
			case cmp == 0:
				left := x.carrying
				*x.clears = &left
			}
		}
	}
	out.Application = a
	return out, probs, nil
}
