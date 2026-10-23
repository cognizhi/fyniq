package journal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/purchaseorder"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// PurchaseOrders runs the purchase order commands (FR-AP-01; PRD-001 rev 0.87; p1-purchases task 8.1) against a store,
// each in one unit of work. An order posts nothing: approving it posts the document with no entry, closing it reverses
// the document with no entry, and bills are made from what is left of its lines.
type PurchaseOrders struct {
	Store storage.Store
}

func isOrder(doc storage.Document) error {
	if doc.Type != storage.DocumentPurchaseOrder {
		return &purchaseorder.Error{Code: purchaseorder.CodeNotOrder, Message: fmt.Sprintf("document %s is a %s, not a purchase order", doc.ID, doc.Type),
			Fix: "Open it with its own module"}
	}
	return nil
}

// Create saves a draft order (✎), numbered now when its sequence is not gap-free; Check shows what it comes to.
func (s PurchaseOrders) Create(ctx context.Context, org uuid.UUID, d purchaseorder.Draft) (storage.Document, error) {
	b, err := purchaseorder.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err = createNumbered(ctx, u, storage.DocumentPurchaseOrder, b)
		return err
	})
	return doc, err
}

// Revise saves a draft order's new content as its next version. An approved order never changes: close it and raise
// another.
func (s PurchaseOrders) Revise(ctx context.Context, org, id uuid.UUID, d purchaseorder.Draft) (storage.Document, error) {
	b, err := purchaseorder.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isOrder(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &purchaseorder.Error{Code: purchaseorder.CodeNotDraft, Message: fmt.Sprintf("the order is %s, not a draft", orderState(doc.Status)),
				Fix: "An approved order never changes: close it and raise another"}
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

func orderState(s storage.DocumentStatus) string {
	switch s {
	case storage.DocumentPosted:
		return "approved"
	case storage.DocumentReversed:
		return "closed"
	}
	return string(s)
}

// Check reads an order's current version as approving it would: what it comes to and what stops it (✖).
func (s PurchaseOrders) Check(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (purchaseorder.Prepared, ledger.Problems, error) {
	if err := isOrder(doc); err != nil {
		return purchaseorder.Prepared{}, nil, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return purchaseorder.Prepared{}, nil, err
	}
	return purchaseorder.Prepare(ctx, u, doc, settings.Base)
}

// Approve approves a draft order: its number (gap-free, in this transaction) and the document posted, by the actor,
// with no entry. One the checks refuse is not approved, and every problem comes back. Who may approve it (the
// threshold, FM-2; never its preparer above it, SOD-3) is the caller's to decide.
func (s PurchaseOrders) Approve(ctx context.Context, org, id uuid.UUID) (storage.Document, error) {
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isOrder(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &purchaseorder.Error{Code: purchaseorder.CodeNotDraft, Message: fmt.Sprintf("the order is %s, not a draft", orderState(doc.Status)),
				Fix: "Only a draft is approved"}
		}
		if _, probs, err := s.Check(ctx, u, doc); err != nil {
			return err
		} else if len(probs) > 0 {
			return probs
		}
		if doc.Number, err = u.Sequences().Number(ctx, id, doc.Type); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out = doc
		return nil
	})
	return out, err
}

// Close closes an approved order, with a reason: no more bills are made from it; those made already stand.
func (s PurchaseOrders) Close(ctx context.Context, org, id uuid.UUID, reason string) (storage.Document, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isOrder(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &purchaseorder.Error{Code: purchaseorder.CodeNotApproved, Message: fmt.Sprintf("the order is %s, not approved", orderState(doc.Status)),
				Fix: "Close an approved order; a draft is simply not approved"}
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out = doc
		return nil
	})
	return out, err
}

// Bill makes a draft bill from an approved order (FR-AP-01): of each line, the quantity asked for (quantities, by line
// from 1), or, when quantities is nil, all that no bill takes yet; at most that. Each bill line names its order line
// and quantity, its amount is the part by cumulative RND-LINE after what the order's other bills take, and its GST is
// RND-GST-EXCL at the rate on the order's date for a standard-rated code, as the supplier is expected to charge: the
// person entering the supplier's invoice corrects either to it. The bill has no date or supplier's number yet.
func (s PurchaseOrders) Bill(ctx context.Context, org, id uuid.UUID, quantities map[int]money.Quantity) (storage.Document, error) {
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isOrder(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &purchaseorder.Error{Code: purchaseorder.CodeNotApproved, Message: fmt.Sprintf("the order is %s, not approved", orderState(doc.Status)),
				Fix: "Bill an approved order that is not closed"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		o, err := purchaseorder.Read(doc)
		if err != nil {
			return err
		}
		states, _, err := purchaseorder.Billing(ctx, u, id, o, settings.Base, uuid.Nil)
		if err != nil {
			return err
		}
		codes, err := bill.PurchaseCodes(ctx, u, o.Date)
		if err != nil {
			return err
		}
		d := bill.Draft{SupplierID: o.SupplierID, Currency: o.Currency, Notes: fmt.Sprintf("From purchase order %s", doc.Number)}
		var probs ledger.Problems
		for i, l := range o.Lines {
			n := i + 1
			left := states[i].Left()
			q := left
			if quantities != nil {
				var ok bool
				if q, ok = quantities[n]; !ok {
					continue
				}
			}
			if q.Sign() <= 0 {
				continue
			}
			if q.Cmp(left) > 0 {
				probs = append(probs, ledger.Problem{Code: purchaseorder.CodeLeft, Field: fmt.Sprintf("lines[%d].quantity", n),
					Message: fmt.Sprintf("line %d has %s left to bill; %s was asked for", n, left, q), Fix: "Bill at most what is left of the line"})
				continue
			}
			net, err := rounding.LinePart(states[i].Billed.Add(states[i].Drafted), q, l.UnitPrice)
			if err != nil {
				return err
			}
			gst := money.Zero(o.Currency)
			for _, c := range codes.Codes {
				if c.Code == l.GSTCode && c.StandardRated && codes.Rate != "" {
					sub, err := rounding.GSTExcl([]rounding.GSTNet{{Code: l.GSTCode, Rate: codes.Rate, Net: net}}, rounding.GSTPerSubtotal)
					if err != nil {
						return err
					}
					gst = sub[0].GST
				}
			}
			d.Lines = append(d.Lines, bill.Line{AccountID: l.AccountID, Description: l.Description, Net: net, GSTCode: l.GSTCode, GST: gst,
				Order: &bill.OrderRef{PurchaseOrderID: id, Line: n, Quantity: q}})
		}
		for n := range quantities {
			if n < 1 || n > len(o.Lines) {
				probs = append(probs, ledger.Problem{Code: purchaseorder.CodeLeft, Field: "lines", Message: fmt.Sprintf("the order has no line %d", n),
					Fix: "Ask for the order's own lines"})
			}
		}
		if len(probs) > 0 {
			return probs
		}
		if len(d.Lines) == 0 {
			return &purchaseorder.Error{Code: purchaseorder.CodeLeft, Message: "nothing is left of the order to bill: its bills, posted or draft, take every line",
				Fix: "Post or change the draft bills made from it, or close the order"}
		}
		b, err := bill.Encode(d)
		if err != nil {
			return err
		}
		out, err = createNumbered(ctx, u, storage.DocumentBill, b)
		return err
	})
	return out, err
}
