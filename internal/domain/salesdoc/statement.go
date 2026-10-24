package salesdoc

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/statement"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// The ageing fields of a statement (FR-AR-08), by subledger.Buckets, and its unapplied credits.
var AgeingFields = [5]string{"document.not_yet_due", "document.overdue_month_one", "document.overdue_month_two", "document.overdue_month_three",
	"document.overdue_older"}

// StatementData is the data a statement of account renders with (FR-AR-08; p1-sales task 7.2), from what it said when it
// was made: the organisation's and the customer's details, its number, date, currency and period, a line per movement
// with its running balance, the balance brought forward, the ageing of the closing balance, what of it is overdue, and
// the balance due. It renders as of the day it was sent, so a reprint is the same bytes.
func StatementData(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (Invoice, error) {
	s, err := statement.Read(doc)
	if err != nil {
		return Invoice{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return Invoice{}, err
	}
	c, err := u.Contacts().Get(ctx, s.CustomerID)
	if err != nil {
		return Invoice{}, err
	}
	f := fields{}
	f.organisation(o)
	f.text("customer.name", c.LegalName)
	f.text("customer.address", c.Address)
	f.text("customer.uen", c.UENOrTaxID)
	f.text("customer.email", c.BillingEmail)
	f.text("document.number", doc.Number)
	f.day("document.date", s.Date)
	f.text("document.currency", s.Currency.Code)
	f.day("document.period_start", s.From)
	f.day("document.period_end", s.To)
	f.amount("document.opening_balance", s.Opening)
	f.amount("document.balance_due", s.Closing)
	f.amount("document.overdue", s.Overdue)
	for i, path := range AgeingFields {
		f.amount(path, s.Ageing[i])
	}
	if !s.Credits.IsZero() {
		f.amount("document.unapplied_credits", s.Credits)
	}
	data := render.Data{Fields: f}
	for _, l := range s.Lines {
		data.Lines = append(data.Lines, render.Line{Description: l.Date.Format("2 Jan 2006") + " " + l.Description, Amount: *amountOf(l.Amount)})
	}
	created := s.Date
	if doc.PostedAt != nil {
		created = doc.PostedAt.UTC()
	}
	return Invoice{Data: data, Created: created.In(time.UTC)}, nil
}
