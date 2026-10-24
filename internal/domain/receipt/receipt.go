// Package receipt is money a customer paid, recorded by hand (FR-AR-18; the owner's decision of 24 Oct 2026, until bank
// import matches receipts): its content as it is saved, and what the posting rule needs from it. Reading a receipt for
// the rule happens here: its customer, the money account it came into, the rate (the typed one, else the cascade's),
// and each invoice it settles as the invoice stands: issued, the customer's, in the receipt's currency, with the amount
// and the carrying amount still open. The posting service in package journal writes it (the one posting path).
package receipt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a receipt's refusals (ENG-23).
const (
	CodeNotReceipt = "RECEIPT-01" // the document is not a customer receipt
	CodeBadDraft   = "RECEIPT-02" // the saved receipt cannot be read
	CodeCustomer   = "RECEIPT-03" // no customer, or one that is not a customer
	CodeInvoice    = "RECEIPT-04" // an invoice that is not an issued invoice of the receipt's customer and currency, or named twice
	CodeOverpaid   = "RECEIPT-05" // more than an invoice has open
	CodeNotPosted  = "RECEIPT-06" // only a posted receipt is voided
	CodeVoidDate   = "RECEIPT-07" // a void dated before the receipt
	CodeTooLong    = "RECEIPT-08" // the reference or the notes are too long
	CodeTooMany    = "RECEIPT-09" // more invoices than one receipt settles
)

// The longest reference and notes a receipt keeps, and the most invoices it settles.
const (
	MaxReference = 140
	MaxNotes     = 2000
	MaxInvoices  = 100
)

// Error is a receipt command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Part is what a receipt settles of one invoice, in the invoice's currency.
type Part struct {
	InvoiceID uuid.UUID
	Amount    money.Money
}

// Draft is a receipt as recorded: the customer, the date the money came in, the money account it came into, the
// invoices' currency and its rate, what the account received when it is in the base currency and the invoices are not,
// the bank's reference, the parts it settles, what is left over as a credit with the customer, and notes.
type Draft struct {
	CustomerID uuid.UUID
	Date       time.Time
	AccountID  uuid.UUID
	Currency   money.Currency
	Rate       *money.Rate  // as typed; nil takes the cascade's rate for the date (ENG-40)
	Received   *money.Money // in the base currency
	Reference  string
	Invoices   []Part
	Unapplied  money.Money
	Notes      string
}

// payload is a receipt as a document version stores it: dates as text, amounts with their currency.
type payload struct {
	CustomerID   uuid.UUID     `json:"customer_id"`
	Date         string        `json:"date"`
	AccountID    uuid.UUID     `json:"money_account_id"`
	Currency     string        `json:"currency"`
	FXRate       string        `json:"fx_rate,omitempty"`
	FXRateSource string        `json:"fx_rate_source,omitempty"`
	Received     *money.Money  `json:"received,omitempty"`
	Reference    string        `json:"reference,omitempty"`
	Invoices     []payloadPart `json:"invoices"`
	Unapplied    money.Money   `json:"unapplied"`
	Notes        string        `json:"notes,omitempty"`
}

type payloadPart struct {
	InvoiceID uuid.UUID   `json:"invoice_id"`
	Amount    money.Money `json:"amount"`
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	if len(d.Reference) > MaxReference {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the reference has %d characters; the most is %d", len(d.Reference), MaxReference), "Shorten it"}
	}
	if len(d.Notes) > MaxNotes {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the notes have %d characters; the most is %d", len(d.Notes), MaxNotes), "Shorten them"}
	}
	if len(d.Invoices) > MaxInvoices {
		return nil, &Error{CodeTooMany, fmt.Sprintf("the receipt settles %d invoices; the most is %d", len(d.Invoices), MaxInvoices), "Record it as more than one receipt"}
	}
	p := payload{CustomerID: d.CustomerID, AccountID: d.AccountID, Currency: d.Currency.Code, Received: d.Received, Reference: d.Reference,
		Invoices: []payloadPart{}, Unapplied: d.Unapplied, Notes: d.Notes}
	if !d.Date.IsZero() {
		p.Date = d.Date.Format(time.DateOnly)
	}
	if d.Rate != nil {
		p.FXRate, p.FXRateSource = d.Rate.String(), d.Rate.Source
	}
	for _, x := range d.Invoices {
		p.Invoices = append(p.Invoices, payloadPart{InvoiceID: x.InvoiceID, Amount: x.Amount})
	}
	return json.Marshal(p)
}

// Read reads a receipt version as it was saved; base is the organisation's base currency, which a typed rate converts
// into.
func Read(doc storage.Document, base money.Currency) (Draft, error) {
	if doc.Type != storage.DocumentCustomerReceipt {
		return Draft{}, &Error{CodeNotReceipt, fmt.Sprintf("document %s is a %s, not a customer receipt", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, fmt.Sprintf("the saved receipt cannot be read: %s", what), "Record the receipt again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, bad(err.Error())
	}
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Draft{}, bad(err.Error())
	}
	d := Draft{CustomerID: p.CustomerID, AccountID: p.AccountID, Currency: cur, Received: p.Received, Reference: p.Reference, Unapplied: p.Unapplied,
		Notes: p.Notes}
	if p.Unapplied.Currency().Code == "" {
		d.Unapplied = money.Zero(cur)
	}
	if p.Date != "" {
		if d.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
			return Draft{}, bad(fmt.Sprintf("the date %q", p.Date))
		}
	}
	if p.FXRate != "" {
		r, err := money.ParseRate(p.FXRate, cur, base, p.FXRateSource)
		if err != nil {
			return Draft{}, bad(err.Error())
		}
		d.Rate = &r
	}
	for _, x := range p.Invoices {
		d.Invoices = append(d.Invoices, Part{InvoiceID: x.InvoiceID, Amount: x.Amount})
	}
	return d, nil
}

// Total is what the receipt received in the invoices' currency: every part and the unapplied amount.
func (d Draft) Total() (money.Money, error) {
	out := d.Unapplied
	if out.Currency() != d.Currency {
		out = money.Zero(d.Currency)
	}
	for _, x := range d.Invoices {
		var err error
		if out, err = out.Add(x.Amount); err != nil {
			return money.Money{}, err
		}
	}
	return out, nil
}

// Open is an issued invoice as a receipt finds it: its number, its total and its rate as its entry debited accounts
// receivable, and what is open of it before the receipt, in its currency and as a carrying amount in the base currency
// (its debit to 1100 less every allocation's base amount).
type Open struct {
	Invoice  storage.Document
	Number   string
	Total    money.Money
	Rate     money.Rate
	Open     money.Money
	Carrying money.Money
}

// OpenInvoice is what is open of an issued invoice: CodeInvoice when the document is not an issued invoice. s finds its
// accounts receivable line.
func OpenInvoice(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (Open, error) {
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return Open{}, &Error{CodeInvoice, fmt.Sprintf("invoice %s does not exist in this organisation", id), "Choose one of the customer's issued invoices"}
	}
	if err != nil {
		return Open{}, err
	}
	if doc.Type != storage.DocumentInvoice || doc.Status != storage.DocumentPosted {
		return Open{}, &Error{CodeInvoice, fmt.Sprintf("%s is not an issued invoice", cmp(doc.Number, doc.ID.String())), "Choose one of the customer's issued invoices"}
	}
	e, err := u.Ledger().ByIdentity(ctx, ledger.InvoiceIdentity(doc.ID, doc.Version, ledger.RuleInvoice))
	if err != nil {
		return Open{}, err
	}
	out := Open{Invoice: doc, Number: doc.Number}
	found := false
	for _, l := range e.Lines {
		if s.Accounts[l.AccountID].Role != ledger.RoleAccountsReceivable {
			continue
		}
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return Open{}, err
		}
		amount, err := money.ParseStored(string(l.Amount), cur)
		if err != nil {
			return Open{}, err
		}
		base, err := money.ParseStored(string(l.BaseAmount), s.Base)
		if err != nil {
			return Open{}, err
		}
		if out.Rate, err = money.ParseRate(string(l.FXRate), cur, s.Base, l.FXRateSource); err != nil {
			return Open{}, err
		}
		out.Total, out.Carrying, found = amount, base, true
		break
	}
	if !found {
		return Open{}, &Error{CodeInvoice, fmt.Sprintf("%s debits no accounts receivable account", cmp(doc.Number, doc.ID.String())), "Check the chart's accounts receivable account"}
	}
	out.Open = out.Total
	allocs, err := u.Allocations().OfDocument(ctx, id)
	if err != nil {
		return Open{}, err
	}
	for _, a := range allocs {
		amount, err := money.ParseStored(string(a.Amount), out.Total.Currency())
		if err != nil {
			return Open{}, err
		}
		base, err := money.ParseStored(string(a.BaseAmount), s.Base)
		if err != nil {
			return Open{}, err
		}
		if out.Open, err = out.Open.Sub(amount); err != nil {
			return Open{}, err
		}
		if out.Carrying, err = out.Carrying.Sub(base); err != nil {
			return Open{}, err
		}
	}
	return out, nil
}

func cmp(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}

// Prepared is a receipt version as the rule reads it, with what it shows beside its entry: its customer, and each invoice
// it settles as it stood before the receipt.
type Prepared struct {
	Draft    Draft
	Receipt  ledger.CustomerReceipt
	Customer storage.Contact
	Invoices []Open
}

// Prepare reads one version of a receipt for the rule: its customer, its rate (the typed one, else the cascade's for its
// date when the money account is in the invoices' currency), and each invoice it settles. An invoice that is not an
// issued invoice of the customer in the receipt's currency, an invoice named twice, and a part above what the invoice
// has open are refused (over-allocation, ✖); the part that settles all that is open of an invoice clears its carrying
// amount (ledger.ReceivedInvoice). Problems the rule would refuse come back as ledger.Problems, with the engine's own,
// from PostCustomerReceipt. Lock the invoices first when the receipt is to be written, so no other receipt settles them
// meanwhile.
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Prepared, ledger.Problems, error) {
	d, err := Read(doc, s.Base)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}

	if d.CustomerID == uuid.Nil {
		add(CodeCustomer, "customer_id", "the receipt has no customer", "Choose the customer who paid")
	} else {
		c, err := u.Contacts().Get(ctx, d.CustomerID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeCustomer, "customer_id", "the customer does not exist in this organisation", "Choose a customer from your contacts")
		case err != nil:
			return Prepared{}, nil, err
		case !c.Customer:
			add(CodeCustomer, "customer_id", fmt.Sprintf("%s is not a customer", c.LegalName), "Mark the contact as a customer, or choose another")
		default:
			out.Customer = c
		}
	}

	r := ledger.CustomerReceipt{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: memo(out.Customer, d), Account: d.AccountID, Currency: d.Currency,
		Rate: d.Rate, Received: d.Received, Unapplied: d.Unapplied}
	acc, known := s.Accounts[d.AccountID]
	inCurrency := known && acc.Currency == d.Currency.Code
	if d.Currency != s.Base && d.Rate == nil && inCurrency && !d.Date.IsZero() {
		f, err := fx.Lookup(ctx, u, s.Base, d.Currency, d.Date)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			add(fe.Code, "fx_rate", fe.Message, fe.Fix)
		case err != nil:
			return Prepared{}, nil, err
		default:
			rate := f.Rate
			r.Rate = &rate
		}
	}

	seen := map[uuid.UUID]bool{}
	for i, x := range d.Invoices {
		field := fmt.Sprintf("invoices[%d].invoice_id", i+1)
		if seen[x.InvoiceID] {
			add(CodeInvoice, field, "the receipt names the invoice twice", "Settle each invoice once, with the whole amount received of it")
			continue
		}
		seen[x.InvoiceID] = true
		o, err := OpenInvoice(ctx, u, x.InvoiceID, s)
		var re *Error
		if errors.As(err, &re) {
			add(re.Code, field, re.Message, re.Fix)
			continue
		}
		if err != nil {
			return Prepared{}, nil, err
		}
		inv, err := invoice.Read(o.Invoice, s.Base)
		if err != nil {
			return Prepared{}, nil, err
		}
		name := cmp(o.Number, "the invoice")
		switch {
		case inv.CustomerID != d.CustomerID:
			add(CodeInvoice, field, fmt.Sprintf("%s is another customer's", name), "Record each customer's receipts separately")
			continue
		case o.Total.Currency() != d.Currency:
			add(CodeInvoice, field, fmt.Sprintf("%s is in %s; the receipt is in %s", name, o.Total.Currency(), d.Currency), "Receive invoices of one currency together")
			continue
		}
		out.Invoices = append(out.Invoices, o)
		part := ledger.ReceivedInvoice{InvoiceID: x.InvoiceID, Amount: x.Amount, StoredRate: o.Rate}
		if x.Amount.Currency() == d.Currency {
			c, err := x.Amount.Cmp(o.Open)
			if err != nil {
				return Prepared{}, nil, err
			}
			switch {
			case c > 0:
				add(CodeOverpaid, fmt.Sprintf("invoices[%d].amount", i+1), fmt.Sprintf("%s has %s %s open; the receipt settles %s of it", name, o.Open, d.Currency, x.Amount),
					"Settle at most what is open, and keep the rest as a credit with the customer")
			case c == 0:
				left := o.Carrying
				part.Clears = &left
			}
		}
		r.Invoices = append(r.Invoices, part)
	}
	out.Receipt = r
	return out, probs, nil
}

// memo names a receipt in its entry: the customer and the bank's reference.
func memo(c storage.Contact, d Draft) string {
	switch {
	case c.LegalName != "" && d.Reference != "":
		return c.LegalName + ", " + d.Reference
	case c.LegalName != "":
		return c.LegalName
	}
	return d.Reference
}
