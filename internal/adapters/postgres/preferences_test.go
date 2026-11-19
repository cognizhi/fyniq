//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/preference"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A person's preferences are the database's too (golden rules 2, 7 and 8; FR-UX-23): every save is a new row with its
// audit event, the latest is in force and the history stays, no one changes or deletes a row, a value the app does not
// offer is refused, and another organisation sees none of it.
// req: FR-UX-23
// req: NFR-L10N-01
// asvs: V8.2.1, V4.2.1
func TestAttackPreferencesKeepTheirHistoryAndTheirTenant(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Preferences Pte Ltd")
	other := books(t, "Preferences elsewhere Pte Ltd")
	now := clock.System{}.Now()
	user := founder(t, store(t, "fyniq_api"), b.Org, 71, now)
	on := true

	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, ok, err := u.Preferences().Latest(ctx, user); err != nil || ok {
			t.Errorf("before a save: %v, %v", ok, err)
		}
		if _, err := u.Preferences().Save(ctx, user, preference.Preferences{Mode: "basic", Theme: "dark", NumberFormat: "dot_comma"}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Preferences().Save(ctx, user, preference.Preferences{Mode: "expert", ShortcutsOn: &on, ShortcutKeys: map[string]string{"go.home": "g h"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, ok, err := u.Preferences().Latest(ctx, user)
		if err != nil || !ok || p.Mode != "expert" || p.Theme != "" || p.ShortcutsOn == nil || !*p.ShortcutsOn || p.ShortcutKeys["go.home"] != "g h" {
			t.Errorf("the latest save is in force: %+v %v %v", p, ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, b.Org, "user_preference"); n != 2 {
		t.Errorf("the history stays: %d rows, want 2", n)
	}
	// Another organisation sees none of it, and cannot save for this person.
	if err := store(t, "fyniq_api").Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, ok, err := u.Preferences().Latest(ctx, user); err != nil || ok {
			t.Errorf("another organisation reads them: %v, %v", ok, err)
		}
		if _, err := u.Preferences().Save(ctx, user, preference.Preferences{Mode: "basic"}); err == nil {
			t.Error("another organisation saved for this person: no membership there")
		}
		return nil
	}); err != nil {
		t.Logf("the other organisation's unit of work ended with %v (the refused save rolls it back)", err)
	}
	if n := count(t, other.Org, "user_preference"); n != 0 {
		t.Errorf("another organisation holds %d rows", n)
	}

	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change a save", "UPDATE fyniq.user_preference SET mode = 'basic'", "FY189"},
		{"fyniq_migrator", "delete a save", "DELETE FROM fyniq.user_preference", "FY189"},
		{"fyniq_api", "change a save as the API", "UPDATE fyniq.user_preference SET mode = 'basic'", "42501"},
		{"fyniq_api", "delete a save as the API", "DELETE FROM fyniq.user_preference", "42501"},
		{"fyniq_worker", "save as the worker", "INSERT INTO fyniq.user_preference (user_id, set_at) SELECT user_id, now() FROM fyniq.user_preference LIMIT 1", "42501"},
		{"fyniq_api", "save a mode the app does not offer", "INSERT INTO fyniq.user_preference (user_id, mode, set_at) SELECT user_id, 'pro', now() FROM fyniq.user_preference LIMIT 1", "23514"},
		{"fyniq_api", "save a date format the app does not offer", "INSERT INTO fyniq.user_preference (user_id, date_format, set_at) SELECT user_id, 'yy', now() FROM fyniq.user_preference LIMIT 1", "23514"},
		{"fyniq_api", "save a way of writing negatives the app does not offer", "INSERT INTO fyniq.user_preference (user_id, negative_format, set_at) SELECT user_id, 'brackets', now() FROM fyniq.user_preference LIMIT 1", "23514"},
		{"fyniq_api", "save shortcut keys that are not an object", "INSERT INTO fyniq.user_preference (user_id, shortcut_keys, set_at) SELECT user_id, '[]', now() FROM fyniq.user_preference LIMIT 1", "23514"},
		{"fyniq_api", "save for someone who is no member", "INSERT INTO fyniq.user_preference (user_id, set_at) VALUES (gen_random_uuid(), now())", "23503"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
}
