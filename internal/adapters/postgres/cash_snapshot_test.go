//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A week's cash snapshot is kept once by the worker and never changed or deleted, whoever writes (FR-PLN-04): the API cannot take one, a second for a week is refused,
// a week not starting on a Monday is refused, the database refuses any update or delete even from the schema's owner, and another organisation sees none.
// req: FR-PLN-04
// asvs: V4.2.1, V8.2.2
func TestACashSnapshotIsKeptOncePerWeekByTheWorkerAndNeverChanged(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := porttest.NewBooks(t, api, "Snapshot Pte Ltd")
	other := porttest.NewBooks(t, api, "Another snapshot Pte Ltd")
	monday := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	snap := storage.CashSnapshot{WeekOf: monday, TakenOn: monday.AddDate(0, 0, 2), Currency: "SGD", LedgerRevision: 3, TakenAt: monday.Add(time.Hour),
		Projection: []byte(`{"today":"2026-11-04","opening":"100.00","weekly":[],"items":[]}`)}
	take := func(s storage.Store, x storage.CashSnapshot) error {
		return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.CashSnapshots().Take(ctx, x)
			return err
		})
	}
	if err := take(api, snap); err == nil {
		t.Fatal("the API took a snapshot")
	}
	if err := take(worker, snap); err != nil {
		t.Fatalf("the worker's snapshot: %v", err)
	}
	if err := take(worker, snap); storage.CodeOf(err) != storage.CodeCashSnapshotWeek {
		t.Errorf("a second snapshot for the week: %v", err)
	}
	notMonday := snap
	notMonday.WeekOf, notMonday.TakenOn = monday.AddDate(0, 0, 8), monday.AddDate(0, 0, 8)
	if err := take(worker, notMonday); err == nil {
		t.Error("a week that starts on a Tuesday")
	}
	notFloat := snap
	notFloat.WeekOf, notFloat.TakenOn, notFloat.Projection = monday.AddDate(0, 0, 7), monday.AddDate(0, 0, 7), []byte(`{"today":"2026-11-09","opening":100,"weekly":[],"items":[]}`)
	if err := take(worker, notFloat); err == nil {
		t.Error("an opening balance that is a JSON number, not a decimal string")
	}
	su := conn(t, "fyniq")
	if _, err := su.Exec(ctx, `SET app.org_id = '`+b.Org.String()+`'`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE fyniq.cash_snapshot SET currency = 'USD'`, `DELETE FROM fyniq.cash_snapshot`, `TRUNCATE fyniq.cash_snapshot`} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s: a snapshot changed", q)
		}
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.CashSnapshots().List(ctx)
		if err != nil || len(l) != 1 || l[0].LedgerRevision != 3 {
			t.Errorf("the snapshots: %v %v", l, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if l, err := u.CashSnapshots().List(ctx); err != nil || len(l) != 0 {
			t.Errorf("another organisation lists %d snapshots: %v", len(l), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
