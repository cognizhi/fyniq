//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/anomalyjob"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A category's spend three standard deviations above its trend raises one alert for the month (FR-KPI-06), worked by hand on kpiBooks with the
// months from March added. Hosting: 500, 520, 480, 510, 490, 505, 495 for March to September and October's 500, so the mean is 500 and nQ - S^2 =
// 8,400 for n = 8; November's 600 and a further 400 make 1,000: (8 x 1,000 - 4,000)^2 x 7 = 112,000,000 against 9 x 8 x 8,400 = 604,800, an
// anomaly. The rent is 6,000.00 in every month, so no deviation and no alert; advertising went from nothing to 2,000.00 and down to 1,000.00,
// well inside its own spread. The alert is raised once.
// req: FR-KPI-06
// asvs: V4.2.1
func TestASpendAnomalyIsRaisedOnceForTheClosedMonth(t *testing.T) {
	e := newEnv(t)
	acc := kpiBooks(e)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for m := time.March; m <= time.September; m++ {
			if _, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(m, 1), EndsOn: porttest.Day(m+1, 1).AddDate(0, 0, -1)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	post := func(month time.Month, expense, amount string) {
		t.Helper()
		id := uuid.NewString()
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(month, 10), PostingIdentity: "alert:" + id + ":v1:test@1", RuleVersion: "test@1",
				Source: storage.Source{Type: ledger.SourceManualJournal, ID: id, Version: 1}, Lines: []storage.NewLine{
					{AccountID: acc[expense].ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
					{AccountID: acc["2010"].ID, Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)}}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i, amount := range []string{"500.00", "520.00", "480.00", "510.00", "490.00", "505.00", "495.00"} {
		post(time.March+time.Month(i), "5010", amount)
		post(time.March+time.Month(i), "6200", "6000.00")
	}
	post(time.November, "5010", "400.00") // November's hosting is 600 from the books and 400 more
	closeAll := func() {
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			ps, err := u.Periods().List(ctx)
			for _, p := range ps {
				if err == nil && p.Status != storage.PeriodClosed {
					_, err = u.Periods().Close(ctx, p.ID)
				}
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	worker := workerStore(t)
	runner := anomalyjob.Runner{Store: worker, Outbox: outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock}, Clock: e.clock}
	// Nothing while no month is closed.
	if n, err := runner.RunOrganisation(context.Background(), e.books.Org); err != nil || n != 0 {
		t.Fatalf("no month closed: %d, %v", n, err)
	}
	closeAll()
	if n, err := runner.RunOrganisation(context.Background(), e.books.Org); err != nil || n != 1 {
		t.Fatalf("the closed months: %d alert(s), %v", n, err)
	}
	if n, err := runner.RunOrganisation(context.Background(), e.books.Org); err != nil || n != 0 {
		t.Fatalf("the same night again: %d, %v", n, err)
	}
	r := e.call(http.MethodGet, "/v1/kpi-alerts", e.person(uuid.New(), authz.Accountant), nil)
	items, _ := r.Body["items"].([]any)
	if r.Status != http.StatusOK || len(items) != 1 {
		t.Fatalf("the alerts = %d: %s", r.Status, r.Raw)
	}
	a := items[0].(map[string]any)
	if a["category"] != "Hosting" || a["month"] != "2026-11" || a["history_months"].(float64) != 8 || a["spend"].(map[string]any)["amount"] != "1000.0000" && a["spend"].(map[string]any)["amount"] != "1000.00" ||
		a["mean"].(map[string]any)["amount"] != "500.0000" && a["mean"].(map[string]any)["amount"] != "500.00" {
		t.Errorf("the alert: %v", a)
	}
	// Only readers of the whole ledger's reports see them.
	e.problemIs(e.call(http.MethodGet, "/v1/kpi-alerts", e.person(uuid.New(), authz.BoardViewer), nil), "listKpiAlerts", 403, "API-AUTHZ-01")
}
