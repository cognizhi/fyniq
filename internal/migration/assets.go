package migration

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	invoicedoc "github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/purchaseorder"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The kinds of the history's last records (design D5).
const (
	KindQuote         = "quote"
	KindSalesOrder    = "sales_order"
	KindPurchaseOrder = "purchase_order"
	KindAsset         = "asset"
	KindDepreciation  = "depreciation"
)

// defaultCategory is a contact's usual category, the account a quote's or order's line of its amount goes to: the
// workbook keeps no category on them, as they post nothing.
func defaultCategory(ctx context.Context, u storage.UnitOfWork, contact uuid.UUID, number string) (uuid.UUID, error) {
	c, err := u.Contacts().Get(ctx, contact)
	if err != nil {
		return uuid.Nil, err
	}
	if c.DefaultAccountID == nil {
		return uuid.Nil, Refused{{Code: CodeImport, Field: "category", Message: fmt.Sprintf("%s has no Doc Lines, and %s no usual category for its amount", number, c.LegalName),
			Fix: "Give the document Doc Lines, or the contact a usual category, in the workbook"}}
	}
	return *c.DefaultAccountID, nil
}

// quoteRecord imports a row of Quotes & Orders in the state the workbook keeps (FR-AR-01, FR-AP-01): a quote or a sales
// order sent, accepted or declined, a purchase order approved and, once received or billed, closed. It keeps the
// workbook's number (rev 0.259); none posts. A quote the workbook says was invoiced is accepted: its invoice is the
// Invoices sheet's own row.
func (w *Workbook) quoteRecord(m Mapping, q QuoteOrder) (dated, bool) {
	state := strings.ToLower(strings.TrimSpace(q.State))
	kind := strings.ToLower(strings.TrimSpace(q.Kind))
	var k string
	switch {
	case strings.Contains(kind, "purchase"):
		k = KindPurchaseOrder
	case strings.Contains(kind, "order"):
		k = KindSalesOrder
	case strings.Contains(kind, "quote"):
		k = KindQuote
	default:
		return dated{}, false
	}
	return dated{on: q.Date, order: 1, Record: Record{Key: k + ":" + strings.TrimSpace(q.Number), Kind: k, Origin: q.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			cur := base
			if q.Currency != "" {
				if cur, err = money.Lookup(q.Currency); err != nil {
					return Made{}, err
				}
			}
			contact, err := contactOf(e, q.Contact)
			if err != nil {
				return Made{}, err
			}
			note := "From the workbook's Quotes & Orders, " + cmp.Or(q.State, "no status")
			if q.Converted != "" {
				note += ", as " + q.Converted
			}
			if k == KindPurchaseOrder {
				return w.purchaseOrder(ctx, u, e, m, q, contact, cur, state, note)
			}
			values, err := projectOf(ctx, u, e, m, q.Project)
			if err != nil {
				return Made{}, err
			}
			category := uuid.Nil
			if len(w.docLines(q.Number)) == 0 {
				if category, err = defaultCategory(ctx, u, contact, q.Number); err != nil {
					return Made{}, err
				}
			}
			lines, err := w.salesLines(ctx, u, e, q.Number, q.Net, cmp.Or(q.Description, q.Kind+" "+q.Number), category, m.salesGST(q.GST), values, cur, nil)
			if err != nil {
				return Made{}, err
			}
			svc := journal.Quotes{Store: e.Store}
			doc, err := svc.Create(ctx, e.Org, quote.Draft{Content: invoicedoc.Draft{CustomerID: contact, Date: q.Date, Currency: cur, Lines: lines,
				Notes: strings.TrimSpace(q.Notes + "\n" + note)}, ValidUntil: q.Until}, k == KindSalesOrder)
			if err != nil {
				return Made{}, err
			}
			typ := storage.DocumentQuote
			if k == KindSalesOrder {
				typ = storage.DocumentSalesOrder
			}
			if _, err := u.Sequences().Keep(ctx, doc.ID, typ, strings.TrimSpace(q.Number)); err != nil {
				return Made{}, err
			}
			if state == "" || state == "draft" {
				return Made{Type: "document", ID: doc.ID}, nil
			}
			if _, err := svc.Send(ctx, e.Org, doc.ID); err != nil {
				return Made{}, err
			}
			answer := journal.QuoteAnswer{On: q.Date, Name: q.Contact, Note: note}
			switch state {
			case "accepted", "invoiced", "won":
				_, err = svc.Accept(ctx, e.Org, doc.ID, answer)
			case "declined", "lost", "rejected":
				_, err = svc.Decline(ctx, e.Org, doc.ID, answer)
			}
			return Made{Type: "document", ID: doc.ID}, err
		}}}, true
}

// purchaseOrder imports a purchase order: one line of its amount on the supplier's usual category, approved once sent,
// and closed once received or billed, as the workbook keeps it.
func (w *Workbook) purchaseOrder(ctx context.Context, u storage.UnitOfWork, e *Env, m Mapping, q QuoteOrder, supplier uuid.UUID, cur money.Currency,
	state, note string) (Made, error) {
	category, err := defaultCategory(ctx, u, supplier, q.Number)
	if err != nil {
		return Made{}, err
	}
	price, err := money.ParseUnitPrice(textOf(q.Net), cur, "amount")
	if err != nil {
		return Made{}, Refused{{Code: CodeValue, Field: "amount", Message: err.Error(), Fix: "Enter the amount before GST in the workbook"}}
	}
	gst := m.purchaseGST(q.GST)
	if gst == "" {
		if gst, err = defaultGST(ctx, u, category); err != nil {
			return Made{}, err
		}
	}
	svc := journal.PurchaseOrders{Store: e.Store}
	doc, err := svc.Create(ctx, e.Org, purchaseorder.Draft{SupplierID: supplier, Reference: q.Converted, Date: q.Date, ExpectedOn: q.Until, Currency: cur,
		Lines: []purchaseorder.Line{{AccountID: category, Description: cmp.Or(q.Description, "Order "+q.Number), Quantity: mustQuantity("1"), UnitPrice: price,
			GSTCode: gst}}, Notes: strings.TrimSpace(q.Notes + "\n" + note)})
	if err != nil {
		return Made{}, err
	}
	if _, err := u.Sequences().Keep(ctx, doc.ID, storage.DocumentPurchaseOrder, strings.TrimSpace(q.Number)); err != nil {
		return Made{}, err
	}
	switch state {
	case "", "draft":
		return Made{Type: "document", ID: doc.ID}, nil
	}
	if _, err := svc.Approve(ctx, e.Org, doc.ID); err != nil {
		return Made{}, err
	}
	switch state {
	case "received", "billed", "closed", "complete", "completed":
		_, err = svc.Close(ctx, e.Org, doc.ID, note)
	}
	return Made{Type: "document", ID: doc.ID}, err
}

// monthsBetween is the number of months from the month of from to the month before to: 0 when to's month is from's.
func monthsBetween(from, to time.Time) int {
	return max((to.Year()-from.Year())*12+int(to.Month())-int(from.Month()), 0)
}

// openingAccumulated is an asset's depreciation before the books start, as the workbook works it: straight-line, a
// month's (cost less residual) ÷ life for each month from the month it starts depreciating to the month before the books
// start, rounded once to the currency's minor units and never more than it depreciates in all.
func openingAccumulated(a Asset, start time.Time, cur money.Currency) *apd.Decimal {
	from := a.Bought
	if !a.From.IsZero() {
		from = a.From
	}
	n := monthsBetween(from, start)
	life := int64(1)
	if a.Life != nil {
		if l, err := a.Life.Int64(); err == nil && l > 0 {
			life = l
		}
	}
	n = min(n, int(life))
	ctx := apd.BaseContext.WithPrecision(40)
	base := new(apd.Decimal)
	if a.Cost != nil {
		base.Set(a.Cost)
	}
	if a.Residual != nil {
		_, _ = ctx.Sub(base, base, a.Residual)
	}
	out := new(apd.Decimal)
	_, _ = ctx.Mul(out, base, apd.New(int64(n), 0))
	_, _ = ctx.Quo(out, out, apd.New(life, 0))
	return rounding.Import(out, cur)
}

// assetRecord registers an asset of the Assets sheet (FR-FA-01): one bought before the books start as an opening asset,
// with the depreciation the workbook carried for it then (its opening balances hold it on 1590); one bought since from
// the line of the bill or bank transaction its cost posted on. Its depreciation runs with the history's last record.
func (w *Workbook) assetRecord(m Mapping, a Asset, start time.Time, carried bool) dated {
	opening := a.Bought.Before(start)
	on := a.Bought
	if opening {
		on = start
	}
	return dated{on: on, order: 5, Record: Record{Key: fmt.Sprintf("asset:%d", a.Row), Kind: KindAsset, Origin: a.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			base, err := baseOf(ctx, u)
			if err != nil {
				return Made{}, err
			}
			acc, err := account(ctx, u, "asset_account", m.accountOf(a.Account))
			if err != nil {
				return Made{}, err
			}
			cost, err := amount(a.Cost, base, false, "cost")
			if err != nil {
				return Made{}, err
			}
			residual, err := amount(a.Residual, base, false, "residual")
			if err != nil {
				return Made{}, err
			}
			life := 0
			if a.Life != nil {
				if l, err := a.Life.Int64(); err == nil {
					life = int(l)
				}
			}
			var from time.Time
			if !a.From.IsZero() {
				from = time.Date(a.From.Year(), a.From.Month(), 1, 0, 0, 0, 0, time.UTC)
			}
			d := journal.AssetDraft{Name: a.Description, Notes: a.Notes, Residual: residual, LifeMonths: life, Method: asset.StraightLine, Start: from}
			if opening {
				accumulated := openingAccumulated(a, start, base)
				if carried && a.Accumulated != nil { // a clean start carries the depreciation the workbook worked to its "as of"
					accumulated = a.Accumulated
				}
				acc0, err := amount(accumulated, base, false, "accumulated")
				if err != nil {
					return Made{}, err
				}
				d.AccountID, d.PurchasedOn, d.Cost, d.OpeningAccumulated = acc.ID, a.Bought, cost, acc0
			} else if d.SourceID, d.SourceLine, err = sourceOf(ctx, u, e, acc.ID, a.Bought, cost); err != nil {
				return Made{}, err
			}
			doc, err := journal.FixedAssets{Store: e.Store}.Register(ctx, u, d)
			return Made{Type: "document", ID: doc.ID}, err
		}}}
}

// sourceOf is the line of an imported bill or bank transaction that put an asset's cost on its account on the day it
// was bought, which the asset is registered from.
func sourceOf(ctx context.Context, u storage.UnitOfWork, e *Env, account uuid.UUID, on time.Time, cost money.Money) (*uuid.UUID, int, error) {
	s, err := journal.LoadSettings(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	for key, id := range e.made {
		if !strings.HasPrefix(key, "transaction:") && !strings.HasPrefix(key, "bill:") {
			continue
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil || doc.Type != storage.DocumentBill && doc.Type != storage.DocumentBankTransaction {
			continue
		}
		lines, err := journal.SourceLines(ctx, u, doc, s)
		if err != nil {
			return nil, 0, err
		}
		for _, l := range lines {
			if l.AccountID == account && l.Date.Equal(on) && l.Cost.Equal(cost) {
				return &doc.ID, l.Line, nil
			}
		}
	}
	return nil, 0, Refused{{Code: CodeImport, Field: "purchase", Message: fmt.Sprintf("no bill or transaction of the workbook put %s on the asset's account on %s",
		cost, on.Format(time.DateOnly)), Fix: "Record the asset's purchase as a bill or a Transactions row on its asset account, at its cost before GST"}}
}

// depreciationRecord posts the registered assets' depreciation of each month to the workbook's "as of" (FR-FA-02),
// as the workbook depreciated them.
func (w *Workbook) depreciationRecord() dated {
	day := w.Settings.AsOf
	return dated{on: day, order: 6, Record: Record{Key: "depreciation:" + day.Format(time.DateOnly), Kind: KindDepreciation, Origin: Origin{Sheet: "Assets"},
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			if _, err := (journal.Depreciations{Store: e.Store}).DepreciateDue(ctx, e.Org, day); err != nil {
				return Made{}, err
			}
			o, err := u.Organisation(ctx)
			return Made{Type: "organisation", ID: o.ID}, err
		}}}
}

// cleanStart is a clean start's records at the cut-over (§17.1 M3, design D5): the Carry Forward sheet's balances as the
// opening balances, its open invoices, bills and credits as opening items (FR-GL-06, rev 0.257, 0.261), and the assets
// as opening assets with the depreciation the workbook worked to its "as of"; one disposed of before the cut-over is
// left in the archive.
func (w *Workbook) cleanStart(m Mapping) []Record {
	var out []Record
	if r, ok := w.carriedBalances(m); ok {
		out = append(out, r)
	}
	for _, d := range w.CarryForward.Invoices {
		out = append(out, w.invoiceRecord(m, d))
	}
	for _, d := range w.CarryForward.Bills {
		out = append(out, w.billRecord(m, d))
	}
	start := w.BooksStart(storage.PathCleanStart)
	for _, a := range w.Assets {
		if !a.Disposed.IsZero() && a.Disposed.Before(start) {
			continue
		}
		out = append(out, w.assetRecord(m, a, start, true).Record)
	}
	return out
}
