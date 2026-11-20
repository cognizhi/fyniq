package invoicenow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Receive reads the documents the access point has for the organisation since the last one kept, keeps each as received, and drafts a bill from each whose supplier is
// known (FR-AP-05). A document that is not PINT-SG, or that a sender sent before under the same number, is kept and refused with why; one whose supplier is not a contact is
// kept as received for a person to add the supplier and draft it. A draft is only a draft: it posts nothing, and its input-tax facts are left to confirm (FR-AP-20). An
// outage returns ports.ErrAccessPointUnavailable and keeps nothing.
func Receive(ctx context.Context, u storage.UnitOfWork, ap ports.AccessPoint, now time.Time) ([]storage.InvoiceInbound, error) {
	cursor, err := u.InvoiceNow().InboundCursor(ctx)
	if err != nil {
		return nil, err
	}
	docs, next, err := ap.Poll(ctx, cursor)
	if err != nil {
		return nil, err
	}
	var out []storage.InvoiceInbound
	for _, d := range docs {
		n := storage.NewInvoiceInbound{MessageID: d.MessageID, Cursor: next, Sender: d.From, Payload: d.Payload, At: now}
		rx, perr := Parse(d.Payload)
		var pe *ParseError
		switch {
		case errors.As(perr, &pe):
			n.Status, n.Note = "refused", pe.Error()
		case perr != nil:
			return nil, perr
		default:
			n.Number = rx.Number
			if first, err := u.InvoiceNow().InboundDuplicate(ctx, d.From, rx.Number); err != nil {
				return nil, err
			} else if first != nil {
				n.Status, n.Note = "refused", fmt.Sprintf("the sender's invoice %s was received before (%s): no second bill is made", rx.Number, first)
			}
		}
		kept, err := u.InvoiceNow().KeepInbound(ctx, n)
		if err != nil {
			return nil, err
		}
		if kept.Status == "received" {
			if kept, err = DraftInbound(ctx, u, kept.ID); err != nil {
				return nil, err
			}
		}
		out = append(out, kept)
	}
	return out, nil
}

// DraftInbound makes the draft bill of a received document: the supplier is the contact with the sender's Peppol ID, else with the UEN the document states; the lines go to the
// supplier's default account, else the first active expense account, with the GST category read as a purchase code (SR as TX, ZR as ZP). Without a supplier the document stays
// received with the reason, to be drafted again once the supplier is added.
func DraftInbound(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.InvoiceInbound, error) {
	in, err := u.InvoiceNow().Inbound(ctx, id)
	if err != nil {
		return in, err
	}
	payload, err := u.InvoiceNow().InboundPayload(ctx, id)
	if err != nil {
		return in, err
	}
	rx, err := Parse(payload)
	if err != nil {
		return u.InvoiceNow().SettleInbound(ctx, id, "refused", nil, err.Error())
	}
	waits := func(why string) (storage.InvoiceInbound, error) {
		return u.InvoiceNow().SettleInbound(ctx, id, "received", nil, why)
	}
	supplier, ok, err := supplierOf(ctx, u, in.Sender, rx)
	if err != nil {
		return in, err
	}
	if !ok {
		return waits(fmt.Sprintf("no active supplier has the Peppol ID %s or the UEN %s: add the supplier, then draft the bill", in.Sender, rx.Seller.UEN))
	}
	cur, err := money.Lookup(rx.Currency)
	if err != nil {
		return u.InvoiceNow().SettleInbound(ctx, id, "refused", nil, "the currency "+rx.Currency+" is not one Fyniq knows")
	}
	issued, err := time.Parse(time.DateOnly, rx.Issued)
	if err != nil {
		return u.InvoiceNow().SettleInbound(ctx, id, "refused", nil, "the issue date is not YYYY-MM-DD")
	}
	draft := bill.Draft{SupplierID: supplier.ID, SupplierRef: rx.Number, Date: issued, Currency: cur, Notes: "Received through InvoiceNow (" + in.MessageID + ")"}
	if rx.Due != "" {
		if draft.DueDate, err = time.Parse(time.DateOnly, rx.Due); err != nil {
			return u.InvoiceNow().SettleInbound(ctx, id, "refused", nil, "the due date is not YYYY-MM-DD")
		}
	}
	draft.InputTax.Document = bill.DocNone
	if rx.Seller.GST != "" {
		draft.InputTax.Document = bill.DocTaxInvoice
	}
	account, ok, err := defaultAccount(ctx, u, supplier)
	if err != nil {
		return in, err
	}
	if !ok {
		return waits("the chart has no active expense account to put the lines on: add one, then draft the bill")
	}
	for i, l := range rx.Lines {
		net, err := money.Parse(l.Net, cur)
		if err != nil {
			return u.InvoiceNow().SettleInbound(ctx, id, "refused", nil, fmt.Sprintf("line %d: the amount %q is not an amount in %s", i+1, l.Net, cur.Code))
		}
		code, gst := "NR", money.Zero(cur)
		switch l.Category {
		case "SR", "SRCA-C", "SRRC":
			code = "TX"
			if gst, err = percentOf(net, l.Rate, cur); err != nil {
				return u.InvoiceNow().SettleInbound(ctx, id, "refused", nil, fmt.Sprintf("line %d: the rate %q is not a percentage", i+1, l.Rate))
			}
		case "ZR":
			code = "ZP"
		}
		name := l.Name
		if name == "" {
			name = fmt.Sprintf("Line %d", i+1)
		}
		draft.Lines = append(draft.Lines, bill.Line{AccountID: account, Description: name, Net: net, GSTCode: code, GST: gst})
	}
	if len(draft.Lines) == 0 {
		return u.InvoiceNow().SettleInbound(ctx, id, "refused", nil, "the document has no lines")
	}
	doc, err := journal.CreateBillIn(ctx, u, draft)
	if err != nil {
		return waits("the draft bill was not made: " + err.Error())
	}
	return u.InvoiceNow().SettleInbound(ctx, id, "drafted", &doc.ID, "")
}

func supplierOf(ctx context.Context, u storage.UnitOfWork, sender string, rx Received) (storage.Contact, bool, error) {
	var found []storage.Contact
	for _, text := range []string{rx.Seller.UEN, strings.TrimPrefix(sender, "0195:SGUEN"), sender} {
		if text == "" {
			continue
		}
		cs, err := u.Contacts().Search(ctx, text, 20)
		if err != nil {
			return storage.Contact{}, false, err
		}
		found = append(found, cs...)
	}
	for _, c := range found {
		if c.Vendor && c.Active && (c.PeppolID == sender || (rx.Seller.UEN != "" && strings.EqualFold(c.UENOrTaxID, rx.Seller.UEN))) {
			return c, true, nil
		}
	}
	return storage.Contact{}, false, nil
}

func defaultAccount(ctx context.Context, u storage.UnitOfWork, supplier storage.Contact) (uuid.UUID, bool, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	if supplier.DefaultAccountID != nil {
		if i := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.ID == *supplier.DefaultAccountID && a.Active && !a.Group }); i >= 0 {
			return accounts[i].ID, true, nil
		}
	}
	if i := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.Type == storage.Expense && a.Active && !a.Group }); i >= 0 {
		return accounts[i].ID, true, nil
	}
	return uuid.Nil, false, nil
}

// percentOf is net times a rate in percent, rounded to the currency's cents half away from zero: a draft's GST, for the person to compare with what the document states.
func percentOf(net money.Money, rate string, cur money.Currency) (money.Money, error) {
	r, _, err := apd.NewFromString(rate)
	if err != nil || r.Sign() < 0 {
		return money.Money{}, fmt.Errorf("the rate %q is not a percentage", rate)
	}
	var x, hundred apd.Decimal
	c := apd.BaseContext.WithPrecision(40)
	hundred.SetInt64(100)
	if _, err := c.Mul(&x, net.Decimal(), r); err != nil {
		return money.Money{}, err
	}
	if _, err := c.Quo(&x, &x, &hundred); err != nil {
		return money.Money{}, err
	}
	var y apd.Decimal
	rc := apd.BaseContext.WithPrecision(40)
	rc.Rounding = apd.RoundHalfUp
	if _, err := rc.Quantize(&y, &x, -int32(cur.MinorUnits)); err != nil {
		return money.Money{}, err
	}
	return money.FromRounded(&y, cur)
}
