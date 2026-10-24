// Package salesdoc fills the data a sales document renders with (FR-AR-05, FR-AR-06; p1-sales tasks 5.1 and 5.2): an
// invoice's or a quote's fields, lines and GST summary, from its version as the rules read it, and what decides an
// invoice's IRAS particulars: whether the organisation is GST-registered on its date, whether it is in a foreign
// currency, and whether it is a simplified tax invoice; and how a document sent by email stands. Package render lays
// the data out with a template; nothing here formats a page.
package salesdoc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// The stable codes of a document that cannot be laid out (ENG-23).
const (
	CodeNotInvoice  = "SALESDOC-01" // only an invoice is laid out as a tax invoice; a credit note follows
	CodeUnready     = "SALESDOC-02" // the invoice's checks stop it, so its totals cannot be shown
	CodeNotSent     = "SALESDOC-03" // only a sent quote or order, or an issued invoice, is emailed: never a draft, a cancelled or a voided one
	CodeNoRecipient = "SALESDOC-04" // the customer has no billing email and none was given
	CodeParticulars = "SALESDOC-05" // the tax invoice rendered lacks a particular IRAS requires, so it is not sent
)

// The delivery states of a document sent by email (FR-AR-06; the owner's decision of 24 Oct 2026): sent until the
// provider says more, delivered, or bounced; viewed comes with the online link (FR-AR-16).
const (
	NotSent   = "not_sent"
	Sent      = "sent"
	Delivered = "delivered"
	Bounced   = "bounced"
)

// Delivery is how a document stands from its latest send, nil for none: bounced when the provider refused the message
// or its latest event is a bounce, a suppression or a failure; delivered when that event is a delivery or a complaint
// (it arrived); else sent, as for providers that never report delivered (FR-INT-21).
func Delivery(latest *storage.DocumentSend) string {
	switch {
	case latest == nil:
		return NotSent
	case latest.MessageStatus == storage.EmailFailed:
		return Bounced
	}
	switch latest.LastEvent {
	case storage.EventHardBounce, storage.EventSoftBounce, storage.EventSuppressed, storage.EventFailed:
		return Bounced
	case storage.EventDelivered, storage.EventComplaint:
		return Delivered
	}
	return Sent
}

// Error is a refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// SimplifiedSetting is the organisation's choice to issue simplified tax invoices (the owner's decision of 24 Oct
// 2026: off unless turned on).
const SimplifiedSetting = "sales.simplified_tax_invoices"

// SimplifiedLimit is the most, with GST and in SGD, a simplified tax invoice is issued for. Source: IRAS, "Invoicing
// customers" (retrieved 23 Oct 2026).
var SimplifiedLimit, _ = money.Parse("1000.00", money.MustLookup("SGD"))

// Invoice is a sales document's data for its template, and when it was issued: the PDF's date, so a reprint is the
// same bytes; its date for a draft.
type Invoice struct {
	Data    render.Data
	Created time.Time
}

// fields fills a document's fields, leaving out what it has none of.
type fields map[string]render.Value

func (f fields) text(path, v string) {
	if v = strings.TrimSpace(v); v != "" {
		f[path] = render.Value{Text: v}
	}
}

func (f fields) day(path string, t time.Time) {
	if !t.IsZero() {
		f[path] = render.Value{Date: t.Format(time.DateOnly)}
	}
}

func (f fields) amount(path string, m money.Money) { f[path] = render.Value{Amount: amountOf(m)} }

// common is what every sales document shows, from its content as an invoice reads it (p): the organisation's details
// and its GST registration number on the document's date when registered, the customer's, the number, date, currency
// and reference, each line with its GST rate when registered, the GST summary and the totals; and when it was issued.
func common(ctx context.Context, u storage.UnitOfWork, doc storage.Document, p invoice.Prepared) (Invoice, fields, error) {
	d := p.Draft
	f := fields{}
	o, err := u.Organisation(ctx)
	if err != nil {
		return Invoice{}, nil, err
	}
	pr := o.Profile
	f.text("organisation.name", cmpOr(pr.LegalName, o.Name))
	f.text("organisation.uen", pr.UEN)
	f.text("organisation.address", pr.RegisteredAddress)
	f.text("organisation.email", pr.Email)
	f.text("organisation.phone", pr.Phone)
	f.text("payment.bank_name", pr.BankName)
	f.text("payment.account_name", pr.BankAccountName)
	f.text("payment.account_number", pr.BankAccountNumber)
	f.text("payment.paynow", pr.PayNowUEN)
	if p.Registered {
		regs, err := u.GSTRegistrations().List(ctx)
		if err != nil {
			return Invoice{}, nil, err
		}
		for _, r := range regs {
			if !r.RegisteredFrom.After(d.Date) && (r.DeregisteredFrom == nil || r.DeregisteredFrom.After(d.Date)) {
				f.text("organisation.gst_number", r.GSTNumber)
			}
		}
	}
	c := p.Customer
	f.text("customer.name", c.LegalName)
	f.text("customer.address", c.Address)
	f.text("customer.uen", c.UENOrTaxID)
	f.text("customer.email", c.BillingEmail)
	f.text("document.number", doc.Number)
	f.day("document.date", d.Date)
	f.text("document.currency", d.Currency.Code)
	f.text("document.reference", d.Reference)
	t := p.Totals
	f.amount("document.subtotal", t.Net)
	f.amount("document.gst_total", t.GST)
	f.amount("document.total", t.Total)

	data := render.Data{Fields: f, GSTRegistered: p.Registered}
	for i, l := range d.Lines {
		rl := render.Line{Description: l.Description, Quantity: l.Quantity.String(), UnitPrice: l.UnitPrice.String(), Amount: *amountOf(p.Invoice.Lines[i].Net)}
		if p.Registered {
			rl.GSTRate = rateText(p.Invoice.Lines[i])
		}
		data.Lines = append(data.Lines, rl)
	}
	for _, x := range t.Subtotals {
		data.GSTSummary = append(data.GSTSummary, render.GSTRow{Rate: percent(x.Rate), Net: *amountOf(x.Net), GST: *amountOf(x.GST)})
	}
	created := d.Date
	if doc.PostedAt != nil {
		created = doc.PostedAt.UTC()
	}
	return Invoice{Data: data, Created: created}, f, nil
}

func unready(kind string, probs ledger.Problems) error {
	return &Error{CodeUnready, fmt.Sprintf("the %s cannot be laid out: %d check(s) stop it, the first %s", kind, len(probs), firstCode(probs)),
		fmt.Sprintf("Correct the %s as its checks say", kind)}
}

// InvoiceData is the data an invoice renders with (FR-AR-05): what every sales document shows (common), its due date
// and terms, what deposits, receipts and credits have settled and what is left, and for a foreign-currency tax invoice
// its totals in SGD at its rate, the output tax it posts. It is a simplified tax invoice when the organisation chose
// them on its date and its total with GST is at most S$1,000.00 in SGD.
func InvoiceData(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Invoice, error) {
	if doc.Type != storage.DocumentInvoice {
		return Invoice{}, &Error{CodeNotInvoice, fmt.Sprintf("document %s is a %s, not an invoice", doc.ID, strings.ReplaceAll(string(doc.Type), "_", " ")),
			"Lay out an invoice; credit notes follow"}
	}
	p, probs, err := invoice.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return Invoice{}, err
	}
	if len(probs) > 0 || len(p.Invoice.Lines) != len(p.Draft.Lines) {
		return Invoice{}, unready("invoice", probs)
	}
	out, f, err := common(ctx, u, doc, p)
	if err != nil {
		return Invoice{}, err
	}
	d, t := p.Draft, p.Totals
	f.day("document.due_date", p.DueDate)
	if d.DueDate.IsZero() && !d.Date.IsZero() && !p.DueDate.IsZero() { // from the terms, not typed
		f.text("document.payment_terms", fmt.Sprintf("%d days", int(p.DueDate.Sub(d.Date).Hours()/24)))
	}

	// What is settled: the deposits it deducts, and, once issued, what receipts, credits and write-offs settle.
	open := t.Total
	for _, x := range d.Deposits {
		if open, err = open.Sub(x.Amount); err != nil {
			return Invoice{}, err
		}
	}
	if doc.Status == storage.DocumentPosted {
		allocs, err := u.Allocations().OfDocument(ctx, doc.ID)
		if err != nil {
			return Invoice{}, err
		}
		for _, x := range allocs {
			a, err := money.ParseStored(string(x.Amount), d.Currency)
			if err != nil {
				return Invoice{}, err
			}
			if open, err = open.Sub(a); err != nil {
				return Invoice{}, err
			}
		}
	}
	paid, err := t.Total.Sub(open)
	if err != nil {
		return Invoice{}, err
	}
	f.amount("document.amount_paid", paid)
	f.amount("document.amount_due", open)

	sgdTotal := t.Total
	if p.Registered && d.Currency != s.Base && s.Base.Code == "SGD" {
		b, err := ledger.BaseTotalsOf(p.Invoice, s.Base)
		if err != nil {
			return Invoice{}, err
		}
		f.amount("document.subtotal_sgd", b.Net)
		f.amount("document.gst_total_sgd", b.GST)
		f.amount("document.total_sgd", b.Total)
		sgdTotal = b.Total
	}
	if p.Registered && sgdTotal.Currency() == SimplifiedLimit.Currency() {
		on, err := switchOn(ctx, u, SimplifiedSetting, d.Date)
		if err != nil {
			return Invoice{}, err
		}
		if c, err := sgdTotal.Cmp(SimplifiedLimit); on && err == nil && c <= 0 {
			out.Data.Simplified = true
		}
	}
	return out, nil
}

// QuoteData is the data a quote or sales order renders with (FR-AR-06): what every sales document shows (common), read
// as an invoice of the same content would be, and the date it is valid until. A quote is not a tax invoice, so it
// carries no particulars of its own.
func QuoteData(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Invoice, error) {
	qp, probs, err := quote.Check(ctx, u, doc, s)
	if err != nil {
		return Invoice{}, err
	}
	if len(probs) > 0 || len(qp.Invoice.Invoice.Lines) != len(qp.Draft.Content.Lines) {
		return Invoice{}, unready(quote.Kind(doc), probs)
	}
	out, f, err := common(ctx, u, doc, qp.Invoice)
	if err != nil {
		return Invoice{}, err
	}
	f.day("document.valid_until", qp.Draft.ValidUntil)
	return out, nil
}

// rateText is a line's GST as the line table shows it: its rate for a line the pack rates, such as "9%" for
// standard-rated or "0%" for zero-rated, else its code, such as "ES" for exempt or "OS" for out of scope.
func rateText(l ledger.InvoiceLine) string {
	if l.GSTRate != "" {
		return percent(l.GSTRate)
	}
	return l.GSTCode
}

// percent is a rate as a percentage: "0.09" is "9%", "0.085" is "8.5%".
func percent(rate string) string {
	var r apd.Decimal
	if _, _, err := r.SetString(rate); err != nil {
		return rate
	}
	var p apd.Decimal
	if _, err := apd.BaseContext.WithPrecision(34).Mul(&p, &r, apd.New(100, 0)); err != nil {
		return rate
	}
	p.Reduce(&p)
	return p.Text('f') + "%"
}

func amountOf(m money.Money) *render.Amount {
	return &render.Amount{Amount: m.String(), Currency: m.Currency().Code}
}

// switchOn is a switch setting's value on a day.
func switchOn(ctx context.Context, u storage.UnitOfWork, key string, on time.Time) (bool, error) {
	def, ok := settings.Lookup(key)
	if !ok {
		return false, fmt.Errorf("salesdoc: no setting %s", key)
	}
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return false, err
	}
	return settings.On(def, changes, on).Value == "true", nil
}

func firstCode(probs ledger.Problems) string {
	if len(probs) == 0 {
		return "line"
	}
	return probs[0].Code
}

func cmpOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
