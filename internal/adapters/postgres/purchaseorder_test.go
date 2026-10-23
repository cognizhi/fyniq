//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/purchaseorder"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// hasOrderCode says whether err is a purchase order refusal with code.
func hasOrderCode(err error, code string) bool {
	var oe *purchaseorder.Error
	return errors.As(err, &oe) && oe.Code == code
}

func quantityOf(t *testing.T, s string) money.Quantity {
	t.Helper()
	q, err := money.ParseQuantity(s, "quantity")
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func priceOf(t *testing.T, s string) money.UnitPrice {
	t.Helper()
	p, err := money.ParseUnitPrice(s, money.MustLookup("SGD"), "unit_price")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// FR-AP-01 (the owner's decisions of 23 Oct 2026, built in rev 0.87; p1-purchases task 8.1): a purchase order posts
// nothing; approved, it is numbered PO-; bills are made from it by quantity, each line's amount the part by cumulative
// RND-LINE, so the parts of a line add up to its amount; a posted bill never takes more of a line than is left;
// closing the order stops further bills; an approved order never changes, and no entry ever names one.
// req: FR-AP-01
// req: RND-LINE
func TestPurchaseOrdersAreBilledByQuantity(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Purchase orders Pte Ltd")
	orders := journal.PurchaseOrders{Store: s}
	bills := journal.Bills{Store: s}
	sgd := money.MustLookup("SGD")
	draft := purchaseorder.Draft{SupplierID: b.Supplier.ID, Reference: "Q-2026-118", Date: porttest.Day(10, 14), Currency: sgd, Lines: []purchaseorder.Line{
		{AccountID: b.Rent.ID, Description: "Licence, a third of a year", Quantity: quantityOf(t, "3"), UnitPrice: priceOf(t, "33.333333"), GSTCode: "TX"},
		{AccountID: b.Rent.ID, Description: "Set-up", Quantity: quantityOf(t, "1"), UnitPrice: priceOf(t, "200"), GSTCode: "TX"},
	}}
	doc, err := orders.Create(ctx, b.Org, draft)
	if err != nil {
		t.Fatal(err)
	}
	// What it comes to: 100.00 and 200.00, GST at 9% expected, 327.00 in all.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, probs, err := orders.Check(ctx, u, doc)
		if err != nil || len(probs) > 0 || p.Lines[0].Amount.String() != "100.00" || p.Lines[0].GST.String() != "9.00" || p.Net.String() != "300.00" ||
			p.Total.String() != "327.00" || p.TotalBase.String() != "327.00" {
			t.Errorf("the order's check: %+v %v %v", p, probs, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A draft cannot be billed; approved, it is numbered and posts nothing.
	if _, err := orders.Bill(ctx, b.Org, doc.ID, nil); !hasOrderCode(err, purchaseorder.CodeNotApproved) {
		t.Fatalf("billing a draft: %v", err)
	}
	if doc, err = orders.Approve(ctx, b.Org, doc.ID); err != nil || doc.Number != "PO-0001" || doc.Status != storage.DocumentPosted {
		t.Fatalf("approving: %+v %v", doc, err)
	}
	if _, err := orders.Revise(ctx, b.Org, doc.ID, draft); !hasOrderCode(err, purchaseorder.CodeNotDraft) {
		t.Errorf("revising an approved order: %v", err)
	}

	// A bill of one licence of the three: 33.33, with 3.00 of GST expected; no date or supplier's number yet.
	first, err := orders.Bill(ctx, b.Org, doc.ID, map[int]money.Quantity{1: quantityOf(t, "1")})
	if err != nil {
		t.Fatal(err)
	}
	fd, err := bill.Read(first, sgd)
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Lines) != 1 || fd.Lines[0].Net.String() != "33.33" || fd.Lines[0].GST.String() != "3.00" || fd.Lines[0].Order == nil ||
		fd.Lines[0].Order.Line != 1 || fd.Lines[0].Order.Quantity.String() != "1" || !fd.Date.IsZero() || fd.Notes != "From purchase order PO-0001" {
		t.Fatalf("the first bill: %+v", fd)
	}
	fd.Date, fd.SupplierRef = porttest.Day(10, 20), "INV-1"
	if _, err := bills.Revise(ctx, b.Org, first.ID, fd); err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, first.ID); err != nil {
		t.Fatal(err)
	}

	// The rest of the order: two licences, 66.67 (100.00 less 33.33), and the set-up.
	rest, err := orders.Bill(ctx, b.Org, doc.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	rd, err := bill.Read(rest, sgd)
	if err != nil {
		t.Fatal(err)
	}
	if len(rd.Lines) != 2 || rd.Lines[0].Net.String() != "66.67" || rd.Lines[0].Order.Quantity.String() != "2" || rd.Lines[1].Net.String() != "200.00" {
		t.Fatalf("the rest: %+v", rd.Lines)
	}
	if _, err := orders.Bill(ctx, b.Org, doc.ID, nil); !hasOrderCode(err, purchaseorder.CodeLeft) {
		t.Errorf("billing again while the draft takes everything: %v", err)
	}
	// Changed to take three more licences, it would take more than is left: refused at posting.
	rd.Date, rd.SupplierRef = porttest.Day(10, 25), "INV-2"
	over := rd
	over.Lines = append([]bill.Line{}, rd.Lines...)
	big := *over.Lines[0].Order
	big.Quantity = quantityOf(t, "3")
	over.Lines[0].Order = &big
	if _, err := bills.Revise(ctx, b.Org, rest.ID, over); err != nil {
		t.Fatal(err)
	}
	var probs ledger.Problems
	if _, err := bills.Post(ctx, b.Org, rest.ID); !asProblems(err, &probs) || !probs.Has(purchaseorder.CodeOrderLine) {
		t.Fatalf("over-billing a line: %v", err)
	}
	if _, err := bills.Revise(ctx, b.Org, rest.ID, rd); err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, rest.ID); err != nil {
		t.Fatalf("billing the rest: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := purchaseorder.Read(doc)
		if err != nil {
			return err
		}
		st, bs, err := purchaseorder.Billing(ctx, u, doc.ID, o, sgd, [16]byte{})
		if err != nil {
			return err
		}
		if len(bs) != 2 || st[0].Billed.String() != "3" || st[1].Billed.String() != "1" || !st[0].Left().IsZero() || !st[1].Left().IsZero() {
			t.Errorf("billed in full: %+v", st)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Another order, closed: its bill no longer posts. A credit note's line never names an order.
	other, err := orders.Create(ctx, b.Org, draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orders.Approve(ctx, b.Org, other.ID); err != nil {
		t.Fatal(err)
	}
	late, err := orders.Bill(ctx, b.Org, other.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if other, err = orders.Close(ctx, b.Org, other.ID, "Supplier withdrew the quote"); err != nil || other.Status != storage.DocumentReversed {
		t.Fatalf("closing: %+v %v", other, err)
	}
	ld, _ := bill.Read(late, sgd)
	ld.Date, ld.SupplierRef = porttest.Day(10, 26), "INV-3"
	if _, err := bills.Revise(ctx, b.Org, late.ID, ld); err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, late.ID); !asProblems(err, &probs) || !probs.Has(purchaseorder.CodeOrderLine) {
		t.Errorf("billing a closed order: %v", err)
	}
	if _, err := orders.Bill(ctx, b.Org, other.ID, nil); !hasOrderCode(err, purchaseorder.CodeNotApproved) {
		t.Errorf("making a bill of a closed order: %v", err)
	}
	ld.SupplierRef = "CN-3"
	credit, err := bills.CreateCredit(ctx, b.Org, ld)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, credit.ID); !asProblems(err, &probs) || !probs.Has(purchaseorder.CodeOrderLine) {
		t.Errorf("a credit note from an order: %v", err)
	}

	// No entry ever names an order (FY173).
	err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		e := porttest.Sale("po-entry", porttest.Day(10, 20), b.Books, "1.00")
		e.Source = storage.Source{Type: "purchase_order", ID: doc.ID.String(), Version: 1}
		_, err := u.Ledger().Append(ctx, e)
		return err
	})
	if storage.CodeOf(err) != storage.CodePurchaseOrder {
		t.Errorf("an entry naming an order: %v", err)
	}
}
