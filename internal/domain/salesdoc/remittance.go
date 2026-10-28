package salesdoc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// RemittanceData is the data a supplier's remittance advice renders with (FR-BNK-12; design D8): the organisation's
// details, the supplier's, the payment's number, date, amount and reference, how it was paid (method, such as "FAST
// bank transfer"), and one line per bill it settled, with the supplier's own number for it. Created is when the payment
// was recorded, so a reprint is the same bytes.
func RemittanceData(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency, method string) (Invoice, error) {
	if doc.Type != storage.DocumentBillPayment {
		return Invoice{}, fmt.Errorf("salesdoc: document %s is a %s, not a payment to a supplier", doc.ID, doc.Type)
	}
	d, err := billpayment.Read(doc, base)
	if err != nil {
		return Invoice{}, err
	}
	f := fields{}
	o, err := u.Organisation(ctx)
	if err != nil {
		return Invoice{}, err
	}
	f.organisation(o)
	c, err := u.Contacts().Get(ctx, d.SupplierID)
	if err != nil {
		return Invoice{}, err
	}
	f.text("supplier.name", c.LegalName)
	f.text("supplier.address", c.Address)
	f.text("supplier.uen", c.UENOrTaxID)
	f.text("supplier.email", cmpOr(c.RemittanceEmail, c.BillingEmail))
	f.text("document.number", doc.Number)
	f.day("document.date", d.Date)
	f.text("document.currency", d.Currency.Code)
	f.text("document.reference", d.Reference)
	f.text("document.method", method)
	total := money.Zero(d.Currency)
	data := render.Data{Fields: f}
	for _, p := range d.Bills {
		if total, err = total.Add(p.Amount); err != nil {
			return Invoice{}, err
		}
		b, err := u.Documents().Get(ctx, p.BillID)
		if err != nil {
			return Invoice{}, err
		}
		bd, err := bill.Read(b, base)
		if err != nil {
			return Invoice{}, err
		}
		what := "Your invoice " + strings.TrimSpace(bd.SupplierRef)
		if strings.TrimSpace(bd.SupplierRef) == "" {
			what = "Our bill " + b.Number
		}
		data.Lines = append(data.Lines, render.Line{Description: fmt.Sprintf("%s of %s", what, bd.Date.Format("2 Jan 2006")), Amount: *amountOf(p.Amount)})
	}
	f.amount("document.amount", total)
	created := d.Date
	if doc.PostedAt != nil {
		created = doc.PostedAt.UTC()
	}
	return Invoice{Data: data, Created: created.Truncate(time.Second)}, nil
}
