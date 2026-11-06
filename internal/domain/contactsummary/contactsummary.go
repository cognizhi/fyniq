// Package contactsummary is what one customer or supplier stands at (PRD-001 FR-CT-02; rev 0.185): what they owe the
// company and what the company owes them, by currency and days past due, from the AR and AP sub-ledgers; what was
// invoiced to them and billed by them in the financial year, in the base currency; and their documents, newest first,
// each with what it moved on the receivables or payables account.
package contactsummary

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// TimelineLimit is how many of a contact's documents the timeline shows.
const TimelineLimit = 50

// Event is one of the contact's documents. Amount is what it moved on the receivables or payables account, in its
// currency, as owed: positive when the customer owes the company more (an invoice) or the company owes the supplier
// more (a bill), negative for what settles or credits it; nil for a draft or a document that moves neither.
type Event struct {
	Document storage.Document
	Amount   *money.Money
}

// Summary is what a contact stands at on a day.
type Summary struct {
	AsAt       time.Time
	Receivable []subledger.Row // what the customer owes, by currency
	Payable    []subledger.Row // what the company owes the supplier, by currency
	Year       period.Span     // the financial year holding the day; zero when the years are not set
	Invoiced   money.Money     // to the customer in the year, in the base currency: invoices less credit notes
	Billed     money.Money     // by the supplier in the year, in the base currency: bills less vendor credits
	Timeline   []Event         // newest first
}

// Of is what contact stands at on today, in u.
func Of(ctx context.Context, u storage.UnitOfWork, base money.Currency, contact uuid.UUID, today time.Time) (Summary, error) {
	out := Summary{AsAt: today, Invoiced: money.Zero(base), Billed: money.Zero(base)}
	o, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	if cal, err := period.CalendarOf(o.Profile); err == nil {
		if n := cal.Number(today); n > 0 {
			out.Year = cal.Year(n).Span
		}
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return out, err
	}
	ar, ap := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, acc := range accounts {
		switch string(acc.Role) {
		case ledger.RoleAccountsReceivable:
			ar[acc.ID] = true
			l, err := receivables.Open(ctx, u, base, acc.ID, today)
			if err != nil {
				return out, err
			}
			a, err := receivables.Age(l, base)
			if err != nil {
				return out, err
			}
			for _, c := range a.Customers {
				if c.ID == contact {
					out.Receivable = append(out.Receivable, c.Rows...)
				}
			}
		case ledger.RoleAccountsPayable:
			ap[acc.ID] = true
			l, err := payables.Open(ctx, u, base, acc.ID, today)
			if err != nil {
				return out, err
			}
			a, err := payables.Age(l, base)
			if err != nil {
				return out, err
			}
			for _, s := range a.Suppliers {
				if s.ID == contact {
					out.Payable = append(out.Payable, s.Rows...)
				}
			}
		}
	}
	docs, err := u.Documents().OfContact(ctx, contact, TimelineLimit*4)
	if err != nil {
		return out, err
	}
	for _, d := range docs {
		ev := Event{Document: d}
		if d.Status != storage.DocumentDraft {
			amount, value, ok, err := moved(ctx, u, d.ID, ar, ap, base)
			if err != nil {
				return out, err
			}
			if ok {
				ev.Amount = &amount
				if inYear(d.Date, out.Year) && d.Status != storage.DocumentReversed {
					switch d.Type {
					case storage.DocumentInvoice, storage.DocumentCreditNote:
						if out.Invoiced, err = out.Invoiced.Add(value); err != nil {
							return out, err
						}
					case storage.DocumentBill, storage.DocumentVendorCredit:
						if out.Billed, err = out.Billed.Add(value); err != nil {
							return out, err
						}
					}
				}
			}
		}
		if len(out.Timeline) < TimelineLimit {
			out.Timeline = append(out.Timeline, ev)
		}
	}
	return out, nil
}

// inYear reports whether the day date (YYYY-MM-DD) falls in y.
func inYear(date string, y period.Span) bool {
	d, err := time.Parse(time.DateOnly, date)
	return err == nil && !y.StartsOn.IsZero() && !d.Before(y.StartsOn) && !d.After(y.EndsOn)
}

// moved is what a document's current posting moved on the receivables or payables accounts, as owed (see Event), in
// its currency and in the base currency; ok is false when it moved neither. The current posting is its latest entry
// that reverses nothing: the posting itself, or an edit's replacement.
func moved(ctx context.Context, u storage.UnitOfWork, doc uuid.UUID, ar, ap map[uuid.UUID]bool, base money.Currency) (money.Money, money.Money, bool, error) {
	entries, err := u.Ledger().OfSource(ctx, doc)
	if err != nil {
		return money.Money{}, money.Money{}, false, err
	}
	entries = slices.DeleteFunc(entries, func(e storage.Entry) bool { return e.ReversesEntryID != nil })
	if len(entries) == 0 {
		return money.Money{}, money.Money{}, false, nil
	}
	e := slices.MaxFunc(entries, func(x, y storage.Entry) int { return int(x.LedgerRevision - y.LedgerRevision) })
	var amount, value money.Money
	found := false
	for _, l := range e.Lines {
		sign := 0
		switch {
		case ar[l.AccountID]:
			sign = 1 // a debit is owed to the company
		case ap[l.AccountID]:
			sign = -1 // a credit is owed by the company
		default:
			continue
		}
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return amount, value, false, err
		}
		a, err := money.ParseStored(string(l.Amount), cur)
		if err != nil {
			return amount, value, false, err
		}
		v, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return amount, value, false, err
		}
		if sign < 0 {
			a, v = a.Neg(), v.Neg()
		}
		if !found {
			amount, value, found = money.Zero(cur), money.Zero(base), true
		}
		if amount, err = amount.Add(a); err != nil {
			return amount, value, false, err
		}
		if value, err = value.Add(v); err != nil {
			return amount, value, false, err
		}
	}
	return amount, value, found, nil
}
