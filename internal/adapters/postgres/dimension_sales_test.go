//go:build local

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-DIM-01 and FR-DIM-02 on invoices (rev 0.171; p1-organisation-chart task 3.3, slice 3d): an invoice's line keeps its
// dimension values through its draft, and its revenue posts with them; Sales requiring Department, a line without one
// does not issue (DIM-05); a spread line's deferred revenue and the releases due at once carry its values; a credit
// note keeps them; every entry replays.
// req: FR-DIM-01
// req: FR-DIM-02
// req: ENG-06
func TestInvoiceLinesCarryTheirDimensionValues(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Dimension sales Pte Ltd")
	var dept storage.Dimension
	var eng storage.DimensionValue
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dept, err = u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"}); err != nil {
			return err
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
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
	invoices := journal.Invoices{Store: s, Clock: clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))}
	draft := func(values ...uuid.UUID) invoice.Draft {
		l := invoiceLine(t, b, "2", "150.00", "", "SR", "SGD")
		l.Values = values
		return invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"), Lines: []invoice.Line{l}}
	}
	salesValues := func(e storage.Entry) []uuid.UUID {
		for _, l := range e.Lines {
			if l.AccountID == b.Sales.ID {
				return l.DimensionValues
			}
		}
		return []uuid.UUID{uuid.Nil}
	}

	doc, err := invoices.Create(ctx, b.Org, draft(eng.ID))
	if err != nil {
		t.Fatal(err)
	}
	if read := readInvoice(t, s, b.Org, doc.ID); !slices.Equal(read.Lines[0].Values, []uuid.UUID{eng.ID}) {
		t.Errorf("the draft's values: %v", read.Lines[0].Values)
	}
	e, err := invoices.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := salesValues(e); !slices.Equal(got, []uuid.UUID{eng.ID}) {
		t.Errorf("the revenue's values: %v", got)
	}
	for _, l := range e.Lines {
		if l.AccountID != b.Sales.ID && l.DimensionValues != nil {
			t.Errorf("a line on %s carries values: %v", l.AccountID, l.DimensionValues)
		}
	}

	none, err := invoices.Create(ctx, b.Org, draft())
	if err != nil {
		t.Fatal(err)
	}
	var probs ledger.Problems
	if _, err := invoices.Post(ctx, b.Org, none.ID); !errors.As(err, &probs) || !probs.Has(ledger.CodeDimensionRequired) {
		t.Errorf("a line with no department: %v", err)
	}

	sp := spreadInvoice(t, b, "PO-DIM", "400.00")
	sp.Lines[0].Values, sp.Lines[1].Values = []uuid.UUID{eng.ID}, []uuid.UUID{eng.ID}
	spread, err := invoices.Create(ctx, b.Org, sp)
	if err != nil {
		t.Fatal(err)
	}
	se, err := invoices.Post(ctx, b.Org, spread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range se.Lines {
		if l.AccountID == b.Deferred.ID && !slices.Equal(l.DimensionValues, []uuid.UUID{eng.ID}) {
			t.Errorf("the deferred revenue's values: %v", l.DimensionValues)
		}
	}
	r, err := invoiceReleaseOf(t, s, b.Org, spread.ID, 1, "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range r.Lines {
		if !slices.Equal(l.DimensionValues, []uuid.UUID{eng.ID}) {
			t.Errorf("September's release line on %s: %v", l.AccountID, l.DimensionValues)
		}
	}

	cd := draft(eng.ID)
	cd.Credits, cd.Date = doc.ID, porttest.Day(10, 21)
	cn, err := invoices.CreateCredit(ctx, b.Org, cd)
	if err != nil {
		t.Fatal(err)
	}
	ce, err := invoices.Post(ctx, b.Org, cn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := salesValues(ce); !slices.Equal(got, []uuid.UUID{eng.ID}) {
		t.Errorf("the credit note's values: %v", got)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rep, err := journal.Replay(ctx, u)
		if err == nil && len(rep.Mismatches) > 0 {
			t.Errorf("replay: %+v", rep.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func readInvoice(t *testing.T, s storage.Store, org, id uuid.UUID) invoice.Draft {
	t.Helper()
	var d invoice.Draft
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		d, err = invoice.Read(doc, money.MustLookup("SGD"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return d
}
