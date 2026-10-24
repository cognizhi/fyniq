// Package statement is a customer's statement of account (PRD-001 FR-AR-08; p1-sales task 7.2), as the workbook's
// Statement sheet kept it: in one currency, from one day to another, the balance brought forward, each document that
// moved what the customer owes (invoices, credit notes, receipts, refunds of credits, write-offs, and the voids of
// any of them) with a running balance, the closing balance, and an ageing of it by days past due that adds up to it.
// The balances come from the AR sub-ledger (package receivables) and the lines from the documents' entries on the
// accounts-receivable account, so a statement that does not add up says so, with the difference, rather than hide it.
package statement

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/customercredit"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The validation codes.
const (
	CodeRange        = "STATEMENT-01" // the period is empty, backwards, longer than MaxDays, or ends after today
	CodeCustomer     = "STATEMENT-02" // the contact is not a customer
	CodeAddsUp       = "STATEMENT-03" // the lines do not add up to the closing balance the sub-ledger gives: it is not sent
	CodeCurrency     = "STATEMENT-04" // the customer has nothing in that currency
	CodeNotStatement = "STATEMENT-05" // the document is not a statement
)

// Error is a refusal, in plain words, with how to fix it (ENG-23).
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// MaxDays is the longest period a statement covers: two years.
const MaxDays = 731

// The kinds of a statement's lines.
const (
	KindInvoice    = "invoice"
	KindCreditNote = "credit_note"
	KindReceipt    = "receipt"
	KindRefund     = "refund"
	KindWriteOff   = "write_off"
	KindVoid       = "void" // the void of a line's document, in the period
)

// Line is one movement of what the customer owes: positive when it owes more.
type Line struct {
	Date       time.Time
	Kind       string
	DocumentID uuid.UUID
	Number     string
	DueDate    time.Time // an invoice's
	Amount     money.Money
	Balance    money.Money // the running balance after it
}

// Description is the line as a statement prints it, such as "Invoice INV-0012, due 1 Nov 2026".
func (l Line) Description() string {
	what := map[string]string{KindInvoice: "Invoice", KindCreditNote: "Credit note", KindReceipt: "Payment received", KindRefund: "Refund of credit",
		KindWriteOff: "Written off", KindVoid: "Voided"}[l.Kind]
	out := strings.TrimSpace(what + " " + l.Number)
	if l.Kind == KindInvoice && !l.DueDate.IsZero() {
		out += ", due " + l.DueDate.Format("2 Jan 2006")
	}
	return out
}

// Statement is a customer's statement of account in one currency.
type Statement struct {
	CustomerID   uuid.UUID
	CustomerName string
	Currency     money.Currency
	From, To     time.Time
	Opening      money.Money // brought forward: what the customer owed at the end of the day before From
	Lines        []Line
	Closing      money.Money // what it owes at the end of To, as the sub-ledger gives it
	Ageing       subledger.Row
	Overdue      money.Money // what of the closing balance is past due on To
	Difference   money.Money // the closing balance less the opening and the lines: zero when it adds up
}

// AddsUp says whether the opening balance and the lines come to the closing balance.
func (s Statement) AddsUp() bool { return s.Difference.IsZero() }

// CheckPeriod checks a statement's period: from on or before to, at most MaxDays long, ending on or before today.
func CheckPeriod(from, to, today time.Time) error {
	switch {
	case from.IsZero() || to.IsZero() || to.Before(from):
		return &Error{CodeRange, "the statement's period ends before it starts", "Give a start on or before the end"}
	case to.Sub(from).Hours()/24 >= MaxDays:
		return &Error{CodeRange, "the statement's period is longer than two years", "Choose a period of two years or less"}
	case to.After(today):
		return &Error{CodeRange, "the statement's period ends after today", "End it today or earlier"}
	}
	return nil
}

// Of is customer's statement in cur from from to to. base is the organisation's base currency.
func Of(ctx context.Context, u storage.UnitOfWork, base money.Currency, customer uuid.UUID, cur money.Currency, from, to time.Time) (Statement, error) {
	c, err := u.Contacts().Get(ctx, customer)
	if err != nil {
		return Statement{}, err
	}
	if !c.Customer {
		return Statement{}, &Error{CodeCustomer, fmt.Sprintf("%s is not a customer", c.LegalName), "Choose a customer, or make the contact one"}
	}
	out := Statement{CustomerID: customer, CustomerName: c.LegalName, Currency: cur, From: from, To: to}
	ar, err := receivablesAccounts(ctx, u)
	if err != nil {
		return Statement{}, err
	}
	if out.Opening, _, err = openIn(ctx, u, base, ar, customer, cur, from.AddDate(0, 0, -1)); err != nil {
		return Statement{}, err
	}
	var items []subledger.Item
	if out.Closing, items, err = openIn(ctx, u, base, ar, customer, cur, to); err != nil {
		return Statement{}, err
	}
	if out.Lines, err = lines(ctx, u, base, ar, customer, cur, from, to); err != nil {
		return Statement{}, err
	}
	balance := out.Opening
	for i := range out.Lines {
		if balance, err = balance.Add(out.Lines[i].Amount); err != nil {
			return Statement{}, err
		}
		out.Lines[i].Balance = balance
	}
	if out.Difference, err = out.Closing.Sub(balance); err != nil {
		return Statement{}, err
	}
	a, err := subledger.Age(to, base, []subledger.Group{{ID: customer, Name: c.LegalName, Items: items}})
	if err != nil {
		return Statement{}, err
	}
	out.Ageing = subledger.Row{Currency: cur, Credits: money.Zero(cur), Total: money.Zero(cur), Carrying: money.Zero(base)}
	for i := range out.Ageing.Buckets {
		out.Ageing.Buckets[i] = money.Zero(cur)
	}
	if len(a.Groups) == 1 {
		for _, r := range a.Groups[0].Rows {
			if r.Currency == cur {
				out.Ageing = r
			}
		}
	}
	out.Overdue = money.Zero(cur)
	for _, b := range out.Ageing.Buckets[1:] {
		if out.Overdue, err = out.Overdue.Add(b); err != nil {
			return Statement{}, err
		}
	}
	return out, nil
}

// Currencies are the currencies of customer's documents that moved what it owes, or of what it owed, from from to to:
// a statement each. The organisation's base currency comes first when it is one.
func Currencies(ctx context.Context, u storage.UnitOfWork, base money.Currency, customer uuid.UUID, from, to time.Time) ([]money.Currency, error) {
	ar, err := receivablesAccounts(ctx, u)
	if err != nil {
		return nil, err
	}
	seen := map[string]money.Currency{}
	for _, day := range []time.Time{from.AddDate(0, 0, -1), to} {
		for _, acc := range ar {
			l, err := receivables.Open(ctx, u, base, acc, day)
			if err != nil {
				return nil, err
			}
			for _, c := range l.Customers {
				if c.ID != customer {
					continue
				}
				for _, it := range c.Items {
					seen[it.Open.Currency().Code] = it.Open.Currency()
				}
			}
		}
	}
	ls, err := lines(ctx, u, base, ar, customer, money.Currency{}, from, to)
	if err != nil {
		return nil, err
	}
	for _, l := range ls {
		seen[l.Amount.Currency().Code] = l.Amount.Currency()
	}
	out := make([]money.Currency, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b money.Currency) int {
		switch {
		case a.Code == b.Code:
			return 0
		case a.Code == base.Code:
			return -1
		case b.Code == base.Code:
			return 1
		}
		return strings.Compare(a.Code, b.Code)
	})
	return out, nil
}

// receivablesAccounts are the organisation's accounts-receivable accounts.
func receivablesAccounts(ctx context.Context, u storage.UnitOfWork) ([]uuid.UUID, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, a := range accounts {
		if string(a.Role) == ledger.RoleAccountsReceivable {
			out = append(out, a.ID)
		}
	}
	return out, nil
}

// openIn is what customer owed in cur at the end of day, and its open items, from the AR sub-ledger.
func openIn(ctx context.Context, u storage.UnitOfWork, base money.Currency, ar []uuid.UUID, customer uuid.UUID, cur money.Currency,
	day time.Time) (money.Money, []subledger.Item, error) {
	total := money.Zero(cur)
	var items []subledger.Item
	for _, acc := range ar {
		l, err := receivables.Open(ctx, u, base, acc, day)
		if err != nil {
			return total, nil, err
		}
		for _, c := range l.Customers {
			if c.ID != customer {
				continue
			}
			for _, it := range c.Items {
				if it.Open.Currency() != cur {
					continue
				}
				items = append(items, it)
				if total, err = total.Add(it.Open); err != nil {
					return total, nil, err
				}
			}
		}
	}
	return total, items, nil
}

// source is how a document type moves what a customer owes: its posting identity, and who its customer is.
type source struct {
	kind     string
	identity func(id uuid.UUID, version int, rule string) string
	rule     string
	customer func(ctx context.Context, doc storage.Document) (uuid.UUID, time.Time, error) // and its date
}

// lines are customer's lines from from to to in cur (every currency when cur is the zero currency), by date.
func lines(ctx context.Context, u storage.UnitOfWork, base money.Currency, ar []uuid.UUID, customer uuid.UUID, cur money.Currency,
	from, to time.Time) ([]Line, error) {
	customerOfInvoice := map[uuid.UUID]uuid.UUID{}
	invoiceCustomer := func(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
		if c, ok := customerOfInvoice[id]; ok {
			return c, nil
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return uuid.Nil, err
		}
		d, err := invoice.Read(doc, base)
		if err != nil {
			return uuid.Nil, err
		}
		customerOfInvoice[id] = d.CustomerID
		return d.CustomerID, nil
	}
	sources := map[storage.DocumentType]source{
		storage.DocumentInvoice: {KindInvoice, ledger.InvoiceIdentity, ledger.RuleInvoice, func(ctx context.Context, doc storage.Document) (uuid.UUID, time.Time, error) {
			d, err := invoice.Read(doc, base)
			return d.CustomerID, d.Date, err
		}},
		storage.DocumentCreditNote: {KindCreditNote, ledger.CreditNoteIdentity, ledger.RuleCreditNote, func(ctx context.Context, doc storage.Document) (uuid.UUID, time.Time, error) {
			d, err := invoice.Read(doc, base)
			return d.CustomerID, d.Date, err
		}},
		storage.DocumentCustomerReceipt: {KindReceipt, ledger.CustomerReceiptIdentity, ledger.RuleCustomerReceipt, func(ctx context.Context, doc storage.Document) (uuid.UUID, time.Time, error) {
			d, err := receipt.Read(doc, base)
			return d.CustomerID, d.Date, err
		}},
		storage.DocumentWriteOff: {KindWriteOff, ledger.WriteOffIdentity, ledger.RuleWriteOff, func(ctx context.Context, doc storage.Document) (uuid.UUID, time.Time, error) {
			d, err := writeoff.Read(doc)
			if err != nil {
				return uuid.Nil, time.Time{}, err
			}
			c, err := invoiceCustomer(ctx, d.InvoiceID)
			return c, d.Date, err
		}},
		storage.DocumentCustomerRefund: {KindRefund, ledger.CustomerRefundIdentity, ledger.RuleCustomerRefund, func(ctx context.Context, doc storage.Document) (uuid.UUID, time.Time, error) {
			d, err := customercredit.ReadRefund(doc, base)
			if err != nil {
				return uuid.Nil, time.Time{}, err
			}
			credit, err := u.Documents().Get(ctx, d.CreditID) // a credit note or a receipt's unapplied amount
			if err != nil {
				return uuid.Nil, time.Time{}, err
			}
			if credit.Type == storage.DocumentCustomerReceipt {
				r, err := receipt.Read(credit, base)
				return r.CustomerID, d.Date, err
			}
			c, err := invoiceCustomer(ctx, credit.ID)
			return c, d.Date, err
		}},
	}
	var out []Line
	for _, t := range []storage.DocumentType{storage.DocumentInvoice, storage.DocumentCreditNote, storage.DocumentCustomerReceipt,
		storage.DocumentWriteOff, storage.DocumentCustomerRefund} {
		src := sources[t]
		err := subledger.Pages(ctx, u, t, to, func(doc storage.Document) error {
			if doc.Status == storage.DocumentDraft {
				return nil
			}
			who, date, err := src.customer(ctx, doc)
			if err != nil || who != customer {
				return err
			}
			if t == storage.DocumentInvoice {
				customerOfInvoice[doc.ID] = who
			}
			identity := src.identity(doc.ID, doc.Version, src.rule)
			e, err := u.Ledger().ByIdentity(ctx, identity)
			if err != nil {
				return err
			}
			for _, acc := range ar {
				amounts, err := controlAmounts(e, acc)
				if err != nil {
					return err
				}
				for _, amount := range amounts {
					if cur.Code != "" && amount.Currency() != cur {
						continue
					}
					l := Line{Date: date, Kind: src.kind, DocumentID: doc.ID, Number: doc.Number, Amount: amount}
					if t == storage.DocumentInvoice {
						d, err := invoice.Read(doc, base)
						if err != nil {
							return err
						}
						c, err := u.Contacts().Get(ctx, d.CustomerID)
						if err != nil {
							return err
						}
						if l.DueDate, err = invoice.Due(ctx, u, d, c); err != nil {
							return err
						}
					}
					if !date.Before(from) && !date.After(to) {
						out = append(out, l)
					}
					if doc.Status == storage.DocumentReversed {
						r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(identity))
						if err != nil {
							return err
						}
						if !r.PostingDate.Before(from) && !r.PostingDate.After(to) {
							out = append(out, Line{Date: r.PostingDate, Kind: KindVoid, DocumentID: doc.ID, Number: doc.Number, Amount: amount.Neg()})
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(out, func(a, b Line) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		if a.Kind == KindVoid != (b.Kind == KindVoid) { // a void after what it voids, on the same day
			if a.Kind == KindVoid {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Number, b.Number)
	})
	return out, nil
}

// controlAmounts sum an entry's lines on account by currency, a debit positive: what the document moved on it.
func controlAmounts(e storage.Entry, account uuid.UUID) ([]money.Money, error) {
	var out []money.Money
	for _, l := range e.Lines {
		if l.AccountID != account {
			continue
		}
		c, err := money.Lookup(l.Currency)
		if err != nil {
			return nil, err
		}
		x, err := money.ParseStored(string(l.Amount), c)
		if err != nil {
			return nil, err
		}
		if x.IsZero() {
			continue // a base-only line, such as a revaluation's: nothing in the customer's currency
		}
		i := slices.IndexFunc(out, func(m money.Money) bool { return m.Currency() == c })
		if i < 0 {
			out = append(out, x)
			continue
		}
		if out[i], err = out[i].Add(x); err != nil {
			return nil, err
		}
	}
	return out, nil
}
