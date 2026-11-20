//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An election of revenue over time (FR-PRJ-11) is of a project, needs an estimate above nothing for over_time and none for milestone, and changes only by version; a month's run is
// of a project and a draft journal, one per project and month, on a month's first day, and never changes or goes; another organisation sees none of it.
// req: FR-PRJ-11
// asvs: V4.2.1
func TestAnElectionChangesByVersionAndAMonthsRunNeverChanges(t *testing.T) {
	ctx := context.Background()
	api := store(t, "fyniq_api")
	books := porttest.NewBooks(t, api, "Revenue Pte Ltd")
	other := porttest.NewBooks(t, api, "Another revenue Pte Ltd")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	est := func(s string) *storage.Decimal { d := storage.Decimal(s); return &d }
	var projectID uuid.UUID
	var draft storage.Document
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"})
		if err != nil {
			return err
		}
		v, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: d.ID, Code: "REVPG", Name: "Revenue project"})
		if err != nil {
			return err
		}
		projectID = v.ID
		_, err = u.Projects().Create(ctx, storage.NewProject{ID: v.ID, Currency: "SGD", Status: storage.ProjectActive, BillingMethod: storage.BillingFixedFee})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		draft, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{}`)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	elect := func(version int, method string, e *storage.Decimal, extra ...string) (el storage.ProjectRevenueElection, err error) {
		excluded, why := storage.Decimal("0"), ""
		if len(extra) == 2 {
			excluded, why = storage.Decimal(extra[0]), extra[1]
		}
		err = api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			el, err = u.ProjectRevenue().Elect(ctx, projectID, version, method, e, excluded, why, at)
			return err
		})
		return el, err
	}
	for name, bad := range map[string]struct {
		method string
		e      *storage.Decimal
	}{"over time without an estimate": {storage.RevenueOverTime, nil}, "over time at nothing": {storage.RevenueOverTime, est("0")}, "milestone with an estimate": {storage.RevenueMilestone, est("5")},
		"an unknown method": {"other", nil}} {
		if _, err := elect(0, bad.method, bad.e); err == nil {
			t.Errorf("%s was recorded", name)
		}
	}
	el, err := elect(0, storage.RevenueOverTime, est("600.00"))
	if err != nil || el.Version != 1 || el.EstimatedCost == nil || *el.EstimatedCost != "600.0000" {
		t.Fatalf("electing: %+v, %v", el, err)
	}
	if _, err := elect(0, storage.RevenueMilestone, nil); err == nil {
		t.Error("a second first election was recorded")
	}
	if _, err := elect(5, storage.RevenueMilestone, nil); err == nil {
		t.Error("an election at a stale version was recorded")
	}
	// Costs left out of progress need their reason, and no reason goes without costs.
	if _, err := elect(1, storage.RevenueOverTime, est("600.00"), "50.00", ""); err == nil {
		t.Error("costs left out with no reason were recorded")
	}
	if _, err := elect(1, storage.RevenueOverTime, est("600.00"), "0", "a reason for nothing"); err == nil {
		t.Error("a reason with no costs left out was recorded")
	}
	if el, err = elect(1, storage.RevenueOverTime, est("600.00"), "50.00", "Rework of an approved drawing"); err != nil || el.Version != 2 || el.ExcludedCost != "50.0000" || el.ExcludedReason == "" {
		t.Fatalf("leaving costs out: %+v, %v", el, err)
	}
	if el, err = elect(2, storage.RevenueMilestone, nil); err != nil || el.Version != 3 || el.EstimatedCost != nil || el.ExcludedCost != "0.0000" {
		t.Fatalf("changing it: %+v, %v", el, err)
	}
	record := func(m time.Time) (r storage.ProjectRevenueRun, err error) {
		err = api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, err = u.ProjectRevenue().Record(ctx, storage.NewProjectRevenueRun{ProjectID: projectID, Month: m, JournalID: draft.ID, Adjustment: "100.00", Earned: "500.00", At: at})
			return err
		})
		return r, err
	}
	if _, err := record(month.AddDate(0, 0, 3)); err == nil {
		t.Error("a run on a day that is not the first was recorded")
	}
	run, err := record(month)
	if err != nil || run.Adjustment != "100.0000" {
		t.Fatalf("a run: %+v, %v", run, err)
	}
	if _, err := record(month); err == nil {
		t.Error("a second run for the month was recorded")
	}
	for _, q := range []struct{ role, sql string }{
		{"fyniq_migrator", "UPDATE fyniq.project_revenue_run SET adjustment = 1"},
		{"fyniq_migrator", "DELETE FROM fyniq.project_revenue_run"},
		{"fyniq_api", "DELETE FROM fyniq.project_revenue_run"},
		{"fyniq_api", "DELETE FROM fyniq.project_revenue_election"},
		{"fyniq_worker", "UPDATE fyniq.project_revenue_election SET method = 'milestone'"},
	} {
		c := conn(t, q.role)
		if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q.sql); return err }); err == nil {
			t.Errorf("%s: %s succeeded", q.role, q.sql)
		}
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.ProjectRevenue().Election(ctx, projectID); err != ports.ErrNotFound {
			t.Errorf("another organisation read the election: %v", err)
		}
		if runs, err := u.ProjectRevenue().Runs(ctx, projectID); err != nil || len(runs) != 0 {
			t.Errorf("another organisation sees %d runs: %v", len(runs), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
