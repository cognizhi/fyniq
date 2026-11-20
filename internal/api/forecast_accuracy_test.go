//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/cashsnapshotjob"
	"github.com/cognizhi/fyniq/internal/domain/forecastaccuracy"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func decOf(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return d
}

// Each week the worker keeps the 13-week projection once, under the Monday of the organisation's week, and only once something is posted (FR-PLN-04). The accuracy view
// compares a kept projection with the cash at the end of its fourth week once that week has ended: the difference is exact and the three causes add to it exactly, an
// invoice the projection expected inside the window that was not settled counting as receipts later than expected. Only readers of the budgets see it.
// req: FR-PLN-04
// req: FR-PLN-09
// asvs: V4.2.1
func TestTheWorkerKeepsTheWeeksProjectionAndTheAccuracyComparesItWithTheCash(t *testing.T) {
	e := newEnv(t)
	worker := workerStore(t)
	runner := cashsnapshotjob.Runner{Store: worker, Clock: e.clock}
	ctx := context.Background()
	if n, err := runner.RunOrganisation(ctx, e.books.Org); err != nil || n != 0 {
		t.Fatalf("nothing posted: %d, %v", n, err)
	}
	e.postedJournal("100.00")
	if n, err := runner.RunOrganisation(ctx, e.books.Org); err != nil || n != 1 {
		t.Fatalf("the first night of the week: %d, %v", n, err)
	}
	if n, err := runner.RunOrganisation(ctx, e.books.Org); err != nil || n != 0 {
		t.Fatalf("the same week again: %d, %v", n, err)
	}
	e.clock.Set(time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)) // the next Thursday
	if n, err := runner.RunOrganisation(ctx, e.books.Org); err != nil || n != 1 {
		t.Fatalf("the next week: %d, %v", n, err)
	}
	var snaps []storage.CashSnapshot
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		snaps, err = u.CashSnapshots().List(ctx)
		return err
	}); err != nil || len(snaps) != 2 || snaps[0].WeekOf.Format(time.DateOnly) != "2026-10-12" || snaps[1].WeekOf.Format(time.DateOnly) != "2026-10-19" {
		t.Fatalf("the snapshots: %+v %v", snaps, err)
	}
	var kept forecastaccuracy.Projection
	if err := json.Unmarshal(snaps[0].Projection, &kept); err != nil || len(kept.Weekly) != 13 || kept.Today != "2026-10-15" {
		t.Fatalf("the kept projection: %+v %v", kept, err)
	}
	e.clock.Set(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))

	// A projection kept on Monday 7 September, expecting an invoice inside its fourth week (ending 4 October) that nothing settled.
	weeks := []forecastaccuracy.Week{{From: "2026-09-07", To: "2026-09-13", In: "0.00", Out: "0.00", Close: "1000.00"}, {From: "2026-09-14", To: "2026-09-20", In: "0.00", Out: "0.00", Close: "1000.00"},
		{From: "2026-09-21", To: "2026-09-27", In: "0.00", Out: "0.00", Close: "1000.00"}, {From: "2026-09-28", To: "2026-10-04", In: "2500.00", Out: "0.00", Close: "3500.00"}}
	raw, _ := json.Marshal(forecastaccuracy.Projection{Today: "2026-09-07", Opening: "1000.00", Weekly: weeks, Items: []forecastaccuracy.Item{
		{Key: "11111111-1111-4111-8111-111111111111", Kind: "invoice", ID: "11111111-1111-4111-8111-111111111111", ExpectedOn: "2026-10-01", Amount: "2500.00"}}})
	audited := storage.WithAudit(ctx, storage.AuditContext{Actor: &cashsnapshotjob.Actor})
	if err := worker.Do(audited, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.CashSnapshots().Take(ctx, storage.CashSnapshot{WeekOf: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
			TakenOn: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), Currency: "SGD", LedgerRevision: 1, Projection: raw, TakenAt: time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	acct := e.as(authz.Accountant)
	r := e.call(http.MethodGet, "/v1/forecast-accuracy?weeks=4", acct, nil)
	items, _ := r.Body["items"].([]any)
	if r.Status != http.StatusOK || len(items) != 1 {
		t.Fatalf("the accuracy = %d: %s", r.Status, r.Raw)
	}
	row := items[0].(map[string]any)
	amount := func(k string) *apd.Decimal { return decOf(t, row[k].(map[string]any)["amount"].(string)) }
	diff := new(apd.Decimal)
	if _, err := apd.BaseContext.Sub(diff, amount("actual"), amount("projected")); err != nil {
		t.Fatal(err)
	}
	sum := new(apd.Decimal)
	for _, k := range []string{"receipts_later", "timing", "unplanned"} {
		if _, err := apd.BaseContext.Add(sum, sum, amount(k)); err != nil {
			t.Fatal(err)
		}
	}
	if row["weeks"].(float64) != 4 || row["window_end"] != "2026-10-04" || amount("projected").Cmp(decOf(t, "3500.00")) != 0 || diff.Cmp(amount("difference")) != 0 || sum.Cmp(diff) != 0 ||
		amount("receipts_later").Cmp(decOf(t, "-2500.00")) != 0 || amount("timing").Sign() != 0 {
		t.Errorf("the accuracy row: %v", row)
	}
	if r := e.call(http.MethodGet, "/v1/forecast-accuracy?weeks=13", acct, nil); r.Status != http.StatusOK || len(r.Body["items"].([]any)) != 0 {
		t.Errorf("13 weeks of a 4-week snapshot: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/forecast-accuracy?weeks=5", acct, nil), "getForecastAccuracy", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodGet, "/v1/forecast-accuracy", e.as(authz.Bookkeeper), nil), "getForecastAccuracy", 403, CodeForbidden)
}
