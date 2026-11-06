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
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-DIM-01 and FR-DIM-02 on bills (rev 0.171; slice 3d): a bill's line keeps its dimension values through its draft and
// its cost posts with them; Rent requiring Department, a line without one does not post (DIM-05); a spread line's
// prepayment and the releases due at once carry its values; a vendor credit keeps them; every entry replays.
// req: FR-DIM-01
// req: FR-DIM-02
// req: ENG-06
func TestBillLinesCarryTheirDimensionValues(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newSpreadBooks(t, ctx, s, "Dimension purchases Pte Ltd")
	var eng storage.DimensionValue
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		dept, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"})
		if err != nil {
			return err
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
		a, err := u.Accounts().Get(ctx, b.Rent.ID)
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
	bills := journal.Bills{Store: s, Clock: clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))}
	sgd := money.MustLookup("SGD")
	draft := func(ref string, values ...uuid.UUID) bill.Draft {
		return bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: ref, Date: porttest.Day(10, 14), Currency: sgd, Lines: []bill.Line{
			{AccountID: b.Rent.ID, Description: "Co-working", Net: sgdOf(t, "300.00"), GSTCode: "TX", GST: sgdOf(t, "0.00"), Values: values}}}
	}
	rentValues := func(e storage.Entry) []uuid.UUID {
		for _, l := range e.Lines {
			if l.AccountID == b.Rent.ID {
				return l.DimensionValues
			}
		}
		return []uuid.UUID{uuid.Nil}
	}

	doc, err := bills.Create(ctx, b.Org, draft("CW-1", eng.ID))
	if err != nil {
		t.Fatal(err)
	}
	e, err := bills.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := rentValues(e); !slices.Equal(got, []uuid.UUID{eng.ID}) {
		t.Errorf("the cost's values: %v", got)
	}
	none, err := bills.Create(ctx, b.Org, draft("CW-2"))
	if err != nil {
		t.Fatal(err)
	}
	var probs ledger.Problems
	if _, err := bills.Post(ctx, b.Org, none.ID); !errors.As(err, &probs) || !probs.Has(ledger.CodeDimensionRequired) {
		t.Errorf("a line with no department: %v", err)
	}

	sp := spreadDraft(t, b, "JIRA-DIM", "1200.00", "108.00")
	sp.Lines[0].Values, sp.Lines[1].Values = []uuid.UUID{eng.ID}, []uuid.UUID{eng.ID}
	spread, err := bills.Create(ctx, b.Org, sp)
	if err != nil {
		t.Fatal(err)
	}
	se, err := bills.Post(ctx, b.Org, spread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range se.Lines {
		if l.AccountID == b.Prepaid.ID && !slices.Equal(l.DimensionValues, []uuid.UUID{eng.ID}) {
			t.Errorf("the prepayment's values: %v", l.DimensionValues)
		}
	}
	r, err := releaseOf(t, s, b.Org, spread.ID, 1, "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range r.Lines {
		if !slices.Equal(l.DimensionValues, []uuid.UUID{eng.ID}) {
			t.Errorf("September's release line on %s: %v", l.AccountID, l.DimensionValues)
		}
	}

	credit, err := bills.CreateCredit(ctx, b.Org, draft("CW-1C", eng.ID))
	if err != nil {
		t.Fatal(err)
	}
	ce, err := bills.Post(ctx, b.Org, credit.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := rentValues(ce); !slices.Equal(got, []uuid.UUID{eng.ID}) {
		t.Errorf("the vendor credit's values: %v", got)
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
