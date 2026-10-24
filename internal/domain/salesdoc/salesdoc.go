// Package salesdoc fills the data a sales document renders with (FR-AR-05, FR-AR-06; p1-sales task 5.1): an invoice's
// fields, lines and GST summary, from its version as the rules read it, and what decides its IRAS particulars: whether
// the organisation is GST-registered on its date, whether it is in a foreign currency, and whether it is a simplified
// tax invoice. Package render lays the data out with a template; nothing here formats a page.
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
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// The stable codes of a document that cannot be laid out (ENG-23).
const (
	CodeNotInvoice = "SALESDOC-01" // only an invoice is laid out as a tax invoice; a credit note follows
	CodeUnready    = "SALESDOC-02" // the invoice's checks stop it, so its totals cannot be shown
)

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

// Invoice is an invoice's data for its template, and when it was issued: the PDF's date, so a reprint is the same
// bytes; its date for a draft.
type Invoice struct {
	Data    render.Data
	Created time.Time
}

// InvoiceData is the data an invoice renders with (FR-AR-05): the organisation's details and its GST registration
// number on the invoice's date, the customer's, the invoice's number, dates, terms, currency and reference, each line
// with its GST rate, the GST summary, the totals, what deposits, receipts and credits have settled and what is left,
// and for a foreign-currency tax invoice its totals in SGD at its rate, the output tax it posts. It is a simplified tax
// invoice when the organisation chose them on its date and its total with GST is at most S$1,000.00 in SGD.
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
		return Invoice{}, &Error{CodeUnready, fmt.Sprintf("the invoice cannot be laid out: %d check(s) stop it, the first %s", len(probs), firstCode(probs)),
			"Correct the invoice as its checks say"}
	}
	d := p.Draft
	f := map[string]render.Value{}
	text := func(path, v string) {
		if v = strings.TrimSpace(v); v != "" {
			f[path] = render.Value{Text: v}
		}
	}
	day := func(path string, t time.Time) {
		if !t.IsZero() {
			f[path] = render.Value{Date: t.Format(time.DateOnly)}
		}
	}
	amount := func(path string, m money.Money) { f[path] = render.Value{Amount: amountOf(m)} }

	o, err := u.Organisation(ctx)
	if err != nil {
		return Invoice{}, err
	}
	pr := o.Profile
	text("organisation.name", cmpOr(pr.LegalName, o.Name))
	text("organisation.uen", pr.UEN)
	text("organisation.address", pr.RegisteredAddress)
	text("organisation.email", pr.Email)
	text("organisation.phone", pr.Phone)
	text("payment.bank_name", pr.BankName)
	text("payment.account_name", pr.BankAccountName)
	text("payment.account_number", pr.BankAccountNumber)
	text("payment.paynow", pr.PayNowUEN)
	if p.Registered {
		regs, err := u.GSTRegistrations().List(ctx)
		if err != nil {
			return Invoice{}, err
		}
		for _, r := range regs {
			if !r.RegisteredFrom.After(d.Date) && (r.DeregisteredFrom == nil || r.DeregisteredFrom.After(d.Date)) {
				text("organisation.gst_number", r.GSTNumber)
			}
		}
	}
	c := p.Customer
	text("customer.name", c.LegalName)
	text("customer.address", c.Address)
	text("customer.uen", c.UENOrTaxID)
	text("customer.email", c.BillingEmail)

	text("document.number", doc.Number)
	day("document.date", d.Date)
	day("document.due_date", p.DueDate)
	if d.DueDate.IsZero() && !d.Date.IsZero() && !p.DueDate.IsZero() { // from the terms, not typed
		text("document.payment_terms", fmt.Sprintf("%d days", int(p.DueDate.Sub(d.Date).Hours()/24)))
	}
	text("document.currency", d.Currency.Code)
	text("document.reference", d.Reference)
	t := p.Totals
	amount("document.subtotal", t.Net)
	amount("document.gst_total", t.GST)
	amount("document.total", t.Total)

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
	amount("document.amount_paid", paid)
	amount("document.amount_due", open)

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

	sgdTotal := t.Total
	if p.Registered && d.Currency != s.Base && s.Base.Code == "SGD" {
		b, err := ledger.BaseTotalsOf(p.Invoice, s.Base)
		if err != nil {
			return Invoice{}, err
		}
		amount("document.subtotal_sgd", b.Net)
		amount("document.gst_total_sgd", b.GST)
		amount("document.total_sgd", b.Total)
		sgdTotal = b.Total
	}
	if p.Registered && sgdTotal.Currency() == SimplifiedLimit.Currency() {
		on, err := switchOn(ctx, u, SimplifiedSetting, d.Date)
		if err != nil {
			return Invoice{}, err
		}
		if c, err := sgdTotal.Cmp(SimplifiedLimit); on && err == nil && c <= 0 {
			data.Simplified = true
		}
	}

	created := d.Date
	if doc.PostedAt != nil {
		created = doc.PostedAt.UTC()
	}
	return Invoice{Data: data, Created: created}, nil
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
