//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A person's favourites and views are the database's too (golden rules 2, 7 and 8; FR-UX-12): each change is a new row with
// its audit event, the latest of a key is in force and a row that is off takes the item away, no one changes or deletes a
// row, an address or filters the app would not send are refused, and another organisation sees none of it.
// req: FR-UX-12
// req: FR-UX-14
// asvs: V8.2.1, V4.2.1
func TestAttackSavedItemsKeepTheirHistoryAndTheirTenant(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Saved items Pte Ltd")
	other := books(t, "Saved items elsewhere Pte Ltd")
	now := clock.System{}.Now()
	user := founder(t, store(t, "fyniq_api"), b.Org, 72, now)
	do := func(fn func(u storage.UnitOfWork) error) {
		t.Helper()
		if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) }); err != nil {
			t.Fatal(err)
		}
	}
	do(func(u storage.UnitOfWork) error {
		_, err := u.SavedItems().Save(ctx, storage.SavedItem{UserID: user, Kind: "view", Key: "v1", On: true, Title: "Overdue", Path: "/sales/invoices", Query: "?status=overdue"})
		return err
	})
	do(func(u storage.UnitOfWork) error {
		_, err := u.SavedItems().Save(ctx, storage.SavedItem{UserID: user, Kind: "view", Key: "v1", On: true, Title: "Overdue now", Path: "/sales/invoices", Query: "?status=overdue&sort=due"})
		return err
	})
	do(func(u storage.UnitOfWork) error {
		_, err := u.SavedItems().Save(ctx, storage.SavedItem{UserID: user, Kind: "favourite", Key: "/banking", On: true, Title: "Banking", Path: "/banking"})
		return err
	})
	do(func(u storage.UnitOfWork) error {
		mine, err := u.SavedItems().Mine(ctx, user)
		if err != nil || len(mine) != 2 {
			t.Errorf("the latest of each key is in force: %v %v", mine, err)
		}
		for _, m := range mine {
			if m.Key == "v1" && m.Title != "Overdue now" {
				t.Errorf("the view keeps its first name: %+v", m)
			}
		}
		_, err = u.SavedItems().Save(ctx, storage.SavedItem{UserID: user, Kind: "view", Key: "v1", On: false, Title: "Overdue now", Path: "/sales/invoices", Query: "?status=overdue&sort=due"})
		return err
	})
	do(func(u storage.UnitOfWork) error {
		mine, err := u.SavedItems().Mine(ctx, user)
		if err != nil || len(mine) != 1 || mine[0].Key != "/banking" {
			t.Errorf("a row that is off takes the item away: %v %v", mine, err)
		}
		return nil
	})
	if n := count(t, b.Org, "user_saved_item"); n != 4 {
		t.Errorf("the history stays: %d rows, want 4", n)
	}
	if err := store(t, "fyniq_api").Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if mine, err := u.SavedItems().Mine(ctx, user); err != nil || len(mine) != 0 {
			t.Errorf("another organisation reads them: %v %v", mine, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	insert := "INSERT INTO fyniq.user_saved_item (user_id, kind, item_key, state, title, path, query, set_at) SELECT user_id, "
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change an item", "UPDATE fyniq.user_saved_item SET title = 'x'", "FY189"},
		{"fyniq_migrator", "delete an item", "DELETE FROM fyniq.user_saved_item", "FY189"},
		{"fyniq_api", "change an item as the API", "UPDATE fyniq.user_saved_item SET title = 'x'", "42501"},
		{"fyniq_api", "delete an item as the API", "DELETE FROM fyniq.user_saved_item", "42501"},
		{"fyniq_worker", "save as the worker", insert + "'favourite', 'k', 'on', 't', '/', '', now() FROM fyniq.user_saved_item LIMIT 1", "42501"},
		{"fyniq_api", "save an address that is a link out", insert + "'favourite', 'k', 'on', 't', '//evil.example', '', now() FROM fyniq.user_saved_item LIMIT 1", "23514"},
		{"fyniq_api", "save a path with a script", insert + "'favourite', 'k', 'on', 't', '/a<b', '', now() FROM fyniq.user_saved_item LIMIT 1", "23514"},
		{"fyniq_api", "save filters that are no query", insert + "'view', 'k', 'on', 't', '/sales', '?a=<x>', now() FROM fyniq.user_saved_item LIMIT 1", "23514"},
		{"fyniq_api", "save a kind the app has not", insert + "'note', 'k', 'on', 't', '/sales', '', now() FROM fyniq.user_saved_item LIMIT 1", "23514"},
		{"fyniq_api", "save a name too long", insert + "'view', 'k', 'on', repeat('x', 81), '/sales', '', now() FROM fyniq.user_saved_item LIMIT 1", "23514"},
		{"fyniq_api", "save for someone who is no member", "INSERT INTO fyniq.user_saved_item (user_id, kind, item_key, state, title, path, set_at) VALUES (gen_random_uuid(), 'favourite', 'k', 'on', 't', '/', now())", "23503"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
}
