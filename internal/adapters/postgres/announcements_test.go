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

// A banner changes only in being withdrawn, once (FY190); a dismissal never changes or goes (FY191), and a banner that
// cannot be dismissed refuses one (FY192); a person dismisses a banner once, however often they ask. The installation's
// banners are published only by an active Super admin (FY085), never written by the API directly, and its dismissals
// are checked against them too.
// req: FR-NTF-03
func TestBannersKeepTheirHistory(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Banners Pte Ltd")
	now := clock.System{}.Now()
	user := founder(t, store(t, "fyniq_api"), b.Org, 67, now)
	var info, critical storage.Announcement
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if info, err = u.Announcements().Publish(ctx, storage.NewAnnouncement{Message: "Closed on 3 November", Link: "/help", Severity: "info",
			StartsAt: now, EndsAt: now.Add(time.Hour), Dismissible: true, At: now}); err != nil {
			return err
		}
		if critical, err = u.Announcements().Publish(ctx, storage.NewAnnouncement{Message: "Bank feed down", Severity: "critical",
			Roles: []string{"owner"}, StartsAt: now, EndsAt: now.Add(time.Hour), At: now}); err != nil {
			return err
		}
		for range 2 {
			if err := u.Announcements().Dismiss(ctx, user, info.ID, false, now); err != nil {
				return err
			}
		}
		if ids, err := u.Announcements().Dismissed(ctx, user); err != nil || len(ids) != 1 || ids[0] != info.ID {
			t.Errorf("dismissed: %v, %v", ids, err)
		}
		if _, err := u.Announcements().Withdraw(ctx, info.ID, now); err != nil {
			return err
		}
		if _, err := u.Announcements().Withdraw(ctx, info.ID, now); storage.CodeOf(err) != storage.CodeVersion {
			t.Errorf("withdrawing twice: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Announcements().Dismiss(ctx, user, critical.ID, false, now)
	}); storage.CodeOf(err) != storage.CodeBanner {
		t.Fatalf("dismissing a critical banner: %v", err)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change a banner's message", "UPDATE fyniq.announcement SET message = 'x' WHERE id = $1", "FY190"},
		{"fyniq_migrator", "un-withdraw a banner", "UPDATE fyniq.announcement SET withdrawn_at = NULL, withdrawn_by = NULL WHERE id = $1", "FY190"},
		{"fyniq_migrator", "delete a banner", "DELETE FROM fyniq.announcement WHERE id = $1", "FY190"},
		{"fyniq_migrator", "change a dismissal", "UPDATE fyniq.announcement_dismissal SET dismissed_at = now() WHERE announcement_id = $1", "FY191"},
		{"fyniq_migrator", "delete a dismissal", "DELETE FROM fyniq.announcement_dismissal WHERE announcement_id = $1", "FY191"},
		{"fyniq_api", "dismiss a banner that is no banner", "INSERT INTO fyniq.announcement_dismissal (announcement_id, installation, user_id, dismissed_at) SELECT gen_random_uuid(), true, user_id, now() FROM fyniq.announcement_dismissal WHERE announcement_id = $1", "FY192"},
		{"fyniq_api", "write the installation's banners directly", "INSERT INTO fyniq.installation_announcement (message, severity, starts_at, ends_at, dismissible, published_by, published_at) SELECT 'x', 'info', now(), now() + interval '1 hour', true, user_id, now() FROM fyniq.announcement_dismissal WHERE announcement_id = $1", "42501"},
		{"fyniq_api", "publish to everyone without being a Super admin", "SELECT fyniq.publish_installation_announcement(gen_random_uuid(), 'x', NULL, 'info', '{}', now(), now() + interval '1 hour', true, now()) WHERE $1::uuid IS NOT NULL", "FY085"},
		{"fyniq_worker", "publish a banner as the worker", "INSERT INTO fyniq.announcement (message, severity, starts_at, ends_at, dismissible, published_by, published_at) SELECT 'x', 'info', now(), now() + interval '1 hour', true, 'service:worker', now() WHERE $1::uuid IS NOT NULL", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, info.ID); return err })
		wantState(t, x.what, err, x.state)
	}
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Announcements().PublishInstallation(ctx, uuid.New(), storage.NewAnnouncement{Message: "x", Severity: "info", StartsAt: now,
			EndsAt: now.Add(time.Hour), Dismissible: true, At: now})
		return err
	}); storage.CodeOf(err) != storage.CodeGrant {
		t.Fatalf("publishing to everyone as nobody: %v", err)
	}
}
