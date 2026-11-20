//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
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

// The narrow function (FR-PRJ-11, rev 0.587) makes a month's draft journal and its run for the platform's job, and nothing else: only for a project elected for revenue over time, once a month,
// and only a draft of the proposal's own shape (two or three lines, on active postable asset, liability or revenue accounts, each carrying the project alone, balanced, dated the month's last day);
// it posts nothing, and the worker holds no right to create a document itself.
// req: FR-PRJ-11
// asvs: V4.2.1
func TestTheNarrowFunctionMakesOnlyTheMonthsDraftOfAnElectedProject(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Draft Pte Ltd")
	other := porttest.NewBooks(t, api, "Another draft Pte Ltd")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	var project, rival uuid.UUID
	var liability storage.Account
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"})
		if err != nil {
			return err
		}
		for _, code := range []string{"DRAFT-1", "DRAFT-2"} {
			v, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: d.ID, Code: code, Name: code})
			if err != nil {
				return err
			}
			if _, err := u.Projects().Create(ctx, storage.NewProject{ID: v.ID, Currency: "SGD", Status: storage.ProjectActive, BillingMethod: storage.BillingFixedFee}); err != nil {
				return err
			}
			if project == uuid.Nil {
				project = v.ID
			} else {
				rival = v.ID
			}
		}
		if _, err := u.ProjectRevenue().Elect(ctx, project, 0, storage.RevenueOverTime, func() *storage.Decimal { d := storage.Decimal("600.00"); return &d }(), "0", "", at); err != nil {
			return err
		}
		liability, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2499", Name: "Contract liability", Type: storage.Liability})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	line := func(acct uuid.UUID, amount string, values ...uuid.UUID) map[string]any {
		ids := []string{}
		for _, v := range values {
			ids = append(ids, v.String())
		}
		return map[string]any{"account_id": acct.String(), "amount": map[string]any{"amount": amount, "currency": "SGD"}, "dimension_value_ids": ids}
	}
	good := func(lines ...map[string]any) map[string]any {
		return map[string]any{"date": "2026-10-31", "memo": "Revenue over time", "lines": lines}
	}
	call := func(org uuid.UUID, proj uuid.UUID, m time.Time, payload map[string]any) error {
		b, _ := json.Marshal(payload)
		c := conn(t, "fyniq_worker")
		return asOrg(ctx, c, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SELECT fyniq.create_project_revenue_draft($1, $2, $3::jsonb, 100, 500)", proj, m, string(b))
			return err
		})
	}
	// Each of these is refused: an unbalanced draft, an expense account, no project on a line, another project's tag, four lines, a wrong date, a month that is not a first day, and a project
	// that is not elected.
	for name, c := range map[string]struct {
		proj    uuid.UUID
		month   time.Time
		payload map[string]any
	}{
		"unbalanced":       {project, month, good(line(liability.ID, "-100.00", project), line(books.Sales.ID, "90.00", project))},
		"an expense line":  {project, month, good(line(books.Rent.ID, "100.00", project), line(books.Sales.ID, "-100.00", project))},
		"no project":       {project, month, good(line(liability.ID, "-100.00"), line(books.Bank.ID, "100.00"))},
		"another project":  {project, month, good(line(liability.ID, "-100.00", rival), line(books.Bank.ID, "100.00", rival))},
		"two tags":         {project, month, good(line(liability.ID, "-100.00", project, rival), line(books.Bank.ID, "100.00", project))},
		"four lines":       {project, month, good(line(liability.ID, "-100.00", project), line(books.Bank.ID, "50.00", project), line(books.Bank.ID, "25.00", project), line(books.Bank.ID, "25.00", project))},
		"one line":         {project, month, good(line(liability.ID, "0.00", project))},
		"a wrong date":     {project, month, map[string]any{"date": "2026-10-15", "lines": good(line(liability.ID, "-100.00", project), line(books.Bank.ID, "100.00", project))["lines"]}},
		"not a first day":  {project, month.AddDate(0, 0, 3), good(line(liability.ID, "-100.00", project), line(books.Bank.ID, "100.00", project))},
		"not elected":      {rival, month, good(line(liability.ID, "-100.00", rival), line(books.Bank.ID, "100.00", rival))},
		"a reversing date": {project, month, map[string]any{"date": "2026-10-31", "reverse_on": "2026-11-01", "lines": good(line(liability.ID, "-100.00", project), line(books.Bank.ID, "100.00", project))["lines"]}},
	} {
		if err := call(books.Org, c.proj, c.month, c.payload); err == nil {
			t.Errorf("%s was made", name)
		}
	}
	// Another organisation's project cannot be proposed from here, and the worker cannot create a document or a run itself.
	if err := call(other.Org, project, month, good(line(liability.ID, "-100.00", project), line(books.Bank.ID, "100.00", project))); err == nil {
		t.Error("another organisation's project was proposed")
	}
	for _, sql := range []string{"INSERT INTO fyniq.document (type) VALUES ('journal')", "INSERT INTO fyniq.project_revenue_run (project_id, month, journal_id, adjustment, earned, created_by, created_at) VALUES (gen_random_uuid(), '2026-10-01', gen_random_uuid(), 0, 0, 'user:x', now())"} {
		c := conn(t, "fyniq_worker")
		if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err }); err == nil {
			t.Errorf("the worker ran %s", sql)
		}
	}
	// The valid draft is made once, as a draft, with its run; a second call for the month is refused.
	ok := good(line(liability.ID, "-100.00", project), line(books.Bank.ID, "100.00", project))
	// A call that records no audit event is refused by the database, whatever its payload: the change must be audited in its own transaction (AUD-01).
	if err := call(books.Org, project, month, ok); err == nil {
		t.Error("a draft with no audit event was made")
	}
	payload, _ := json.Marshal(ok)
	makeIt := func() error {
		wctx := storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "scheduler", ID: "projects.revenue"}})
		return worker.Do(wctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.ProjectRevenue().DraftByFunction(ctx, project, month, payload, "100.00", "500.00")
			return err
		})
	}
	if err := makeIt(); err != nil {
		t.Fatalf("the draft: %v", err)
	}
	if err := makeIt(); err == nil {
		t.Error("a second draft for the month was made")
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		runs, err := u.ProjectRevenue().Runs(ctx, project)
		if err != nil || len(runs) != 1 {
			t.Fatalf("the runs: %v, %v", runs, err)
		}
		doc, err := u.Documents().Get(ctx, runs[0].JournalID)
		if err != nil || doc.Type != storage.DocumentJournal || doc.Status != storage.DocumentDraft {
			t.Errorf("the document: %+v, %v", doc, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
