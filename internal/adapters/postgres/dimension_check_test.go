//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Appendix C.2 #60 (rev 0.171; p1-organisation-chart task 3.3, slice 3): an amount on Sales with no department, here an
// invoice posted before Sales required one, as a posting the system makes by itself leaves it, and the close warns,
// naming the account, the dimension and the amount; a Department out of use requires nothing; a journal whose lines on Sales net to zero moves
// the amount to Engineering (rev 0.172) and the close finds nothing. No account requiring a dimension, the close says
// nothing of them. The balance service gives each account's totals by dimension set, a set's currencies summed in base.
// req: C.2 #60
// req: FR-DIM-01
// req: FR-DIM-02
// req: ENG-04
func TestTheCloseFlagsAmountsWithoutARequiredDimension(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Dimension check Pte Ltd")
	if _, ok := closeChecks(t, ctx, s, b.Org, b.Oct.ID)["dimensions"]; ok {
		t.Error("the close speaks of dimensions with no account requiring one")
	}
	var dept storage.Dimension
	var eng storage.DimensionValue
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dept, err = u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"}); err != nil {
			return err
		}
		eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	invoices := journal.Invoices{Store: s}
	doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"),
		Lines: []invoice.Line{invoiceLine(t, b, "3", "100.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatalf("the invoice posts to Sales with no department: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Get(ctx, b.Sales.ID)
		if err != nil {
			return err
		}
		c := storage.AccountChange{Name: a.Name, ParentID: a.ParentID, Group: a.Group, Active: a.Active, AccountAttributes: a.AccountAttributes}
		c.RequiredDimensions = []uuid.UUID{dept.ID}
		_, err = u.Accounts().Update(ctx, a.ID, a.Version, c)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	svc := journal.Service{Store: s}
	post := func(lines ...ledger.JournalLine) {
		t.Helper()
		d, err := svc.Create(ctx, b.Org, journal.Draft{Date: porttest.Day(10, 20), Memo: "Department for the October sales", Lines: lines})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Post(ctx, b.Org, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	tagged := saleDraft(t, b.Books, porttest.Day(10, 15), "40.00")
	tagged.Lines[1].Values = []uuid.UUID{eng.ID}
	post(tagged.Lines...)
	r, err := money.ParseRate("1.35", usd, sgd, "document")
	if err != nil {
		t.Fatal(err)
	}
	post(ledger.JournalLine{AccountID: b.Bank.ID, Amount: amount(t, "10.00", usd), Rate: &r},
		ledger.JournalLine{AccountID: b.Sales.ID, Amount: amount(t, "-10.00", usd), Rate: &r, Values: []uuid.UUID{eng.ID}})

	// The balance service's totals by set: the lines with no dimension first, each set's currencies summed in base.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rows, err := balance.BySet(ctx, u, sgd, b.Oct.ID)
		var got []string
		for _, x := range rows {
			if x.AccountID == b.Sales.ID {
				got = append(got, fmt.Sprintf("%v %s", x.Values, x.Base))
			}
		}
		if want := []string{"[] -300.00", fmt.Sprintf("[%s] -53.50", eng.ID)}; !slices.Equal(got, want) {
			t.Errorf("Sales by set: %q, want %q", got, want)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	want := closing.Check{Key: "dimensions", Severity: closing.Warning, Title: "Amounts lack a dimension their account requires",
		Detail: b.Sales.Code + " " + b.Sales.Name + ": -300.00 SGD with no Department: move each to its value with a journal whose lines on the account net to zero"}
	if c := closeChecks(t, ctx, s, b.Org, b.Oct.ID)["dimensions"]; c != want {
		t.Errorf("the invoice's amount with no department:\n got %+v\nwant %+v", c, want)
	}

	change := func(active bool) {
		t.Helper()
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			d, err := u.Dimensions().Get(ctx, dept.ID)
			if err != nil {
				return err
			}
			_, err = u.Dimensions().Change(ctx, d.ID, d.Version, storage.DimensionChange{Name: d.Name, Active: active})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	change(false)
	if c, ok := closeChecks(t, ctx, s, b.Org, b.Oct.ID)["dimensions"]; ok {
		t.Errorf("a Department out of use requires nothing: %+v", c)
	}
	change(true)

	post(ledger.JournalLine{AccountID: b.Sales.ID, Amount: amount(t, "300.00", sgd)},
		ledger.JournalLine{AccountID: b.Sales.ID, Amount: amount(t, "-300.00", sgd), Values: []uuid.UUID{eng.ID}})
	if c := closeChecks(t, ctx, s, b.Org, b.Oct.ID)["dimensions"]; c.Severity != closing.OK || c.Title != "Every amount carries the dimensions its account requires" {
		t.Errorf("moved to Engineering: %+v", c)
	}
}
