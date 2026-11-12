//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The personal-data inventory keeps every entry's versions and every review (migration 000303, FY312): an entry is saved
// and revised and a quarter reviewed, each audited; a version out of order, a second review of a quarter, a quarter that
// does not start one, and anything changed or deleted are refused; the worker writes none of it; another organisation
// sees none.
// req: CMP-PD-02
// req: CTL-DP-05
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheDataInventoryKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Inventory A Pte Ltd"), books(t, "Inventory B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := "user:" + founder(t, api, a.Org, 1, now).String()
	entry := storage.InventoryEntry{What: "Job applicants' CVs", Why: "To hire", Where: "A shared drive", Who: "The founders", Retention: "6 months",
		Transfers: "None", State: "open", SavedBy: dpo, SavedAt: now, Version: 1}
	quarter := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		e, err := u.Compliance().SaveInventoryEntry(ctx, entry)
		if err != nil {
			return err
		}
		entry = e
		e.Version, e.State = 2, "retired"
		if _, err := u.Compliance().SaveInventoryEntry(ctx, e); err != nil {
			return err
		}
		_, err = u.Compliance().ReviewInventory(ctx, storage.InventoryReview{Quarter: quarter, ReviewedBy: dpo, ReviewedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.Compliance().InventoryEntries(ctx)
		if err == nil && (len(es) != 1 || es[0].Version != 2 || es[0].State != "retired") {
			t.Errorf("the entries: %+v", es)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type IN ('data_inventory_entry', 'data_inventory_review') ORDER BY seq`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 3 || actions[0] != "data_inventory_entry.saved" || actions[2] != "data_inventory.reviewed" {
		t.Errorf("the inventory's audit events: %v %v", actions, err)
	}
	version := `INSERT INTO fyniq.data_inventory_entry (entry_id, version, what, why, where_held, who, retention, transfers, needs_dpia, state, saved_by, saved_at)
	            VALUES ($1, $2, 'x', 'y', 'z', 'w', 'r', 'None', false, 'open', 'user:x', now())`
	review := `INSERT INTO fyniq.data_inventory_review (quarter, reviewed_by, reviewed_at) VALUES ($1, 'user:x', now())`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker adding an entry", "fyniq_worker", version, []any{uuid.New(), 1}, []string{"42501"}},
		{"a version skipping one", "fyniq_api", version, []any{entry.EntryID, 4}, []string{"FY312"}},
		{"a quarter reviewed twice", "fyniq_api", review, []any{quarter}, []string{"23505"}},
		{"a quarter that does not start one", "fyniq_api", review, []any{time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}, []string{"23514"}},
		{"the API changing an entry", "fyniq_api", `UPDATE fyniq.data_inventory_entry SET what = 'z'`, nil, []string{"42501"}},
		{"the owner changing an entry", "fyniq_migrator", `UPDATE fyniq.data_inventory_entry SET what = 'z' WHERE entry_id = $1`, []any{entry.EntryID}, []string{"FY312"}},
		{"the owner deleting a review", "fyniq_migrator", `DELETE FROM fyniq.data_inventory_review`, nil, []string{"FY312"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.Compliance().InventoryEntries(ctx)
		if err == nil && len(es) != 0 {
			t.Errorf("another organisation sees %d entries", len(es))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
