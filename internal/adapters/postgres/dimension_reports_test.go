//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-DIM-02 (rev 0.171; p1-organisation-chart task 3.3, slice 4), worked by hand: in October, sales of 100.00 for
// Engineering, 40.00 for Operations and 10.00 with no department, and rent of 30.00 for Engineering. The P&L by Department
// has a column each for ENG (revenue 100.00, rent 30.00, profit 70.00) and OPS (40.00, 0.00, 40.00), one for the lines
// with no department (10.00, 0.00, 10.00) and their total (150.00, 30.00, 120.00); Engineering's P&L alone is its column;
// as at the revision before a later 5.00 for Engineering it is unchanged by it. The general ledger of Sales for
// Engineering in November opens at October's 100.00 for Engineering and lists only its November line.
// req: FR-DIM-02
// req: FR-RPT-08
// req: ENG-04
func TestThePLAndTheGeneralLedgerByDimension(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := porttest.NewBooks(t, s, "Dimension reports Pte Ltd")
	var dept storage.Dimension
	var eng, ops storage.DimensionValue
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dept, err = u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"}); err != nil {
			return err
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
		ops, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "OPS", Name: "Operations"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	svc := journal.Service{Store: s}
	post := func(on time.Time, acc storage.Account, amt string, values ...uuid.UUID) {
		t.Helper()
		other := b.Bank
		lines := []ledger.JournalLine{{AccountID: other.ID, Amount: amount(t, amt, sgd)}, {AccountID: acc.ID, Amount: amount(t, "-"+amt, sgd), Values: values}}
		if acc.ID == b.Rent.ID {
			lines = []ledger.JournalLine{{AccountID: acc.ID, Amount: amount(t, amt, sgd), Values: values}, {AccountID: other.ID, Amount: amount(t, "-"+amt, sgd)}}
		}
		d, err := svc.Create(ctx, b.Org, journal.Draft{Date: on, Memo: "By department", Lines: lines})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Post(ctx, b.Org, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	post(porttest.Day(10, 5), b.Sales, "100.00", eng.ID)
	post(porttest.Day(10, 6), b.Sales, "40.00", ops.ID)
	post(porttest.Day(10, 7), b.Sales, "10.00")
	post(porttest.Day(10, 8), b.Rent, "30.00", eng.ID)
	var before int64
	lay := func(r financials.Request) financials.Statement {
		t.Helper()
		var out financials.Statement
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Periods().List(ctx)
			if err != nil {
				return err
			}
			r.Kind, r.All = financials.ProfitAndLoss, all
			if r.Revision == 0 {
				if r.Revision, err = u.Ledger().Revision(ctx); err != nil {
					return err
				}
			}
			out, err = financials.Lay(ctx, u, money.MustLookup("SGD"), r)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	totals := func(st financials.Statement, key string) string {
		for _, x := range st.Blocks {
			if x.Key == key {
				return fmt.Sprint(x.Totals)
			}
		}
		return "none"
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		before, err = u.Ledger().Revision(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	post(porttest.Day(10, 9), b.Sales, "5.00", eng.ID) // after the revision the as-at statement reads

	pivot := lay(financials.Request{Pivot: dept.ID, Revision: before})
	var labels []string
	for _, c := range pivot.Columns {
		labels = append(labels, c.Label)
	}
	if got := strings.Join(labels, ", "); got != "ENG Engineering, OPS Operations, No Department, Total" {
		t.Errorf("the columns: %s", got)
	}
	if got := totals(pivot, financials.KeyRevenue); got != "[100.00 40.00 10.00 150.00]" {
		t.Errorf("revenue by department: %s", got)
	}
	if got := totals(pivot, financials.KeyOperatingExpenses); got != "[30.00 0.00 0.00 30.00]" {
		t.Errorf("expenses by department: %s", got)
	}
	if got := totals(pivot, financials.KeyNetProfit); got != "[70.00 40.00 10.00 120.00]" {
		t.Errorf("profit by department: %s", got)
	}
	if got := totals(lay(financials.Request{Only: []uuid.UUID{eng.ID}}), financials.KeyRevenue); got != "[105.00]" {
		t.Errorf("Engineering's revenue as the ledger stands: %s", got)
	}
	if got := totals(lay(financials.Request{Only: []uuid.UUID{eng.ID}, Revision: before}), financials.KeyNetProfit); got != "[70.00]" {
		t.Errorf("Engineering's profit as at the earlier revision: %s", got)
	}

	post(porttest.Day(11, 3), b.Sales, "20.00", eng.ID)
	post(porttest.Day(11, 4), b.Sales, "7.00", ops.ID)
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		gl, err := balance.GeneralLedgerOnly(ctx, u, money.MustLookup("SGD"), b.Sales.ID, porttest.Day(11, 1), porttest.Day(11, 30),
			func(vs []uuid.UUID) bool { return len(vs) == 1 && vs[0] == eng.ID })
		if err != nil {
			return err
		}
		if gl.Opening.String() != "-105.00" || len(gl.Lines) != 1 || gl.Lines[0].Base.String() != "-20.00" || gl.Closing.String() != "-125.00" {
			t.Errorf("Sales for Engineering in November: opening %s, %d lines, closing %s", gl.Opening, len(gl.Lines), gl.Closing)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
