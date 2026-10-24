// Package customercredit applies a credit with a customer to one of the customer's invoices (FR-AR-04, FR-AR-18;
// p1-sales task 3.1): an issued credit note, or what a receipt left unapplied. Reading an application for the rule
// happens here: the credit and the invoice as they stand (issued or posted, the customer's, in one currency, with what
// is open of each and its carrying amount left), and the rates they were booked at. The posting service in package
// journal writes it (the one posting path).
package customercredit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of an application's refusals (ENG-23).
const (
	CodeNotApplication = "APPLY-01" // the document is not a customer credit application
	CodeBadDraft       = "APPLY-02" // the saved application cannot be read
	CodeCredit         = "APPLY-03" // not an issued credit note, or a receipt with something unapplied
	CodeInvoice        = "APPLY-04" // not an issued invoice of the credit's customer in its currency
	CodeTooMuch        = "APPLY-05" // more than the invoice has open, or than is left of the credit
	CodeNotPosted      = "APPLY-06" // only a posted application is voided
	CodeVoidDate       = "APPLY-07" // a void dated before the application
	CodeApplied        = "APPLY-08" // a receipt whose unapplied amount is applied or refunded is not voided until those are
)

// Error is an application command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is an application as entered: the credit, the invoice, the date and the amount, in their currency.
type Draft struct {
	CreditID  uuid.UUID
	InvoiceID uuid.UUID
	Date      time.Time
	Amount    money.Money
}

type payload struct {
	CreditID  uuid.UUID   `json:"credit_id"`
	InvoiceID uuid.UUID   `json:"invoice_id"`
	Date      string      `json:"date"`
	Amount    money.Money `json:"amount"`
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	p := payload{CreditID: d.CreditID, InvoiceID: d.InvoiceID, Amount: d.Amount}
	if !d.Date.IsZero() {
		p.Date = d.Date.Format(time.DateOnly)
	}
	return json.Marshal(p)
}

// Read reads an application version as it was saved.
func Read(doc storage.Document) (Draft, error) {
	if doc.Type != storage.DocumentCustomerCreditApplication {
		return Draft{}, &Error{CodeNotApplication, fmt.Sprintf("document %s is a %s, not a customer credit application", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, &Error{CodeBadDraft, "the saved application cannot be read: " + err.Error(), "Apply the credit again"}
	}
	d := Draft{CreditID: p.CreditID, InvoiceID: p.InvoiceID, Amount: p.Amount}
	if p.Date != "" {
		var err error
		if d.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
			return Draft{}, &Error{CodeBadDraft, fmt.Sprintf("the saved application's date %q cannot be read", p.Date), "Apply the credit again"}
		}
	}
	return d, nil
}

// Credit is a credit with a customer as an application finds it: the document, its customer, what is left of it in its
// currency and its carrying amount left in the base currency (both positive), and the rate it was booked at.
type Credit struct {
	Doc      storage.Document
	Customer uuid.UUID
	Left     money.Money
	Carrying money.Money
	Rate     money.Rate
}

// arLines are an entry's lines on the accounts-receivable account with an amount, in order.
func arLines(e storage.Entry, s ledger.Settings) []storage.Line {
	var out []storage.Line
	for _, l := range e.Lines {
		if s.Accounts[l.AccountID].Role == ledger.RoleAccountsReceivable && !isZero(string(l.Amount)) {
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

// lessApplied takes off a credit's applications and refunds: the allocations that settle it as a document.
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

// CreditOf is what is left of a credit with a customer: an issued credit note (its total and its credit to 1100), or
// a posted receipt's unapplied amount (and the line that carries it), less its applications and refunds. CodeCredit
// when it is neither.
func CreditOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (Credit, error) {
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return Credit{}, &Error{CodeCredit, fmt.Sprintf("credit %s does not exist in this organisation", id), "Choose an issued credit note, or a receipt with an amount unapplied"}
	}
	if err != nil {
		return Credit{}, err
	}
	refuse := &Error{CodeCredit, fmt.Sprintf("%s is not an issued credit note or a receipt with an amount unapplied", Label(doc)),
		"Choose an issued credit note, or a receipt with an amount unapplied"}
	if doc.Status != storage.DocumentPosted {
		return Credit{}, refuse
	}
	out := Credit{Doc: doc}
	var line storage.Line
	switch doc.Type {
	case storage.DocumentCreditNote:
		d, err := invoice.Read(doc, s.Base)
		if err != nil {
			return Credit{}, err
		}
		e, err := u.Ledger().ByIdentity(ctx, ledger.CreditNoteIdentity(doc.ID, doc.Version, ledger.RuleCreditNote))
		if err != nil {
			return Credit{}, err
		}
		lines := arLines(e, s)
		if len(lines) != 1 {
			return Credit{}, refuse
		}
		out.Customer, line = d.CustomerID, lines[0]
	case storage.DocumentCustomerReceipt:
		d, err := receipt.Read(doc, s.Base)
		if err != nil {
			return Credit{}, err
		}
		e, err := u.Ledger().ByIdentity(ctx, ledger.CustomerReceiptIdentity(doc.ID, doc.Version, ledger.RuleCustomerReceipt))
		if err != nil {
			return Credit{}, err
		}
		lines := arLines(e, s) // the invoices' parts, in order, then the unapplied amount (customer_receipt@1)
		if d.Unapplied.IsZero() || len(lines) != len(d.Invoices)+1 {
			return Credit{}, refuse
		}
		out.Customer, line = d.CustomerID, lines[len(lines)-1]
	default:
		return Credit{}, refuse
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return Credit{}, err
	}
	amount, err := money.ParseStored(string(line.Amount), cur) // a credit to 1100: negative
	if err != nil {
		return Credit{}, err
	}
	base, err := money.ParseStored(string(line.BaseAmount), s.Base)
	if err != nil {
		return Credit{}, err
	}
	out.Left, out.Carrying = amount.Neg(), base.Neg()
	if out.Rate, err = money.ParseRate(string(line.FXRate), cur, s.Base, line.FXRateSource); err != nil {
		return Credit{}, err
	}
	if out.Left, out.Carrying, err = lessApplied(ctx, u, doc.ID, out.Left, out.Carrying); err != nil {
		return Credit{}, err
	}
	return out, nil
}

// Label names a document in a memo or a message: its number, or its type and ID before it has one.
func Label(doc storage.Document) string {
	if doc.Number != "" {
		return doc.Number
	}
	return string(doc.Type) + " " + doc.ID.String()
}

// Prepared is an application version as the rule reads it, with the credit and the invoice it found.
type Prepared struct {
	Draft       Draft
	Application ledger.CustomerCreditApplication
	Credit      Credit
	Invoice     receipt.Open
}

// Prepare reads one version of an application for the rule: the credit and the invoice as they stand, both the
// customer's and in one currency; an amount above what the invoice has open or what is left of the credit is refused
// (APPLY-05), and the application that settles all that is open of either clears its carrying amount left. Lock both documents first when the application is to be written.
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
	o, err := receipt.OpenInvoice(ctx, u, d.InvoiceID, s)
	var re *receipt.Error
	if errors.As(err, &re) {
		add(CodeInvoice, "invoice_id", re.Message, re.Fix)
	} else if err != nil {
		return Prepared{}, nil, err
	}
	if len(probs) > 0 {
		return Prepared{}, probs, nil
	}
	out.Credit, out.Invoice = c, o
	inv, err := invoice.Read(o.Invoice, s.Base)
	if err != nil {
		return Prepared{}, nil, err
	}
	name := o.Number
	switch {
	case inv.CustomerID != c.Customer:
		add(CodeInvoice, "invoice_id", fmt.Sprintf("%s is another customer's", name), "Apply a credit to its own customer's invoices")
	case o.Total.Currency() != c.Left.Currency():
		add(CodeInvoice, "invoice_id", fmt.Sprintf("%s is in %s; the credit is in %s", name, o.Total.Currency(), c.Left.Currency()), "Apply a credit to an invoice of its currency")
	}
	if len(probs) > 0 {
		return Prepared{}, probs, nil
	}
	a := ledger.CustomerCreditApplication{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: fmt.Sprintf("%s applied to %s", Label(c.Doc), name),
		Amount: d.Amount, InvoiceRate: o.Rate, CreditRate: c.Rate}
	if d.Amount.Currency() == c.Left.Currency() {
		for _, x := range []struct {
			what     string
			open     money.Money
			carrying money.Money
			clears   **money.Money
		}{{name + " has", o.Open, o.Carrying, &a.InvoiceClears}, {Label(c.Doc) + " has", c.Left, c.Carrying, &a.CreditClears}} {
			cmp, err := d.Amount.Cmp(x.open)
			if err != nil {
				return Prepared{}, nil, err
			}
			switch {
			case cmp > 0:
				add(CodeTooMuch, "amount", fmt.Sprintf("%s %s %s left; the application is %s", x.what, x.open, x.open.Currency(), d.Amount),
					"Apply at most what the invoice has open and what is left of the credit")
			case cmp == 0:
				left := x.carrying
				*x.clears = &left
			}
		}
	}
	out.Application = a
	return out, probs, nil
}
