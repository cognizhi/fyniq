//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// verified gives user's identity a verified sign-in email, as their next sign-in would.
func verified(t *testing.T, org, user uuid.UUID, email string) {
	t.Helper()
	ctx := context.Background()
	var issuer, subject string
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT issuer, subject FROM fyniq.user_identity WHERE user_id = $1", user).Scan(&issuer, &subject)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_api").RecordIdentityEmail(ctx, issuer, subject, email, clock.System{}.Now()); err != nil {
		t.Fatal(err)
	}
}

// emitted records an event of type for user, as the API does, and returns it.
func emitted(t *testing.T, org, user uuid.UUID, typ, title string, at time.Time) storage.NotificationEvent {
	t.Helper()
	var ev storage.NotificationEvent
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		ev, err = notify.Emit(ctx, outbox.Dispatcher{Store: store(t, "fyniq_api"), Queue: local.NewMemory(), Clock: clock.NewFrozen(at)}, u, org,
			notify.Event{Type: typ, Title: title, Link: "/approvals/x", Audience: notify.Audience{Users: []uuid.UUID{user}}, At: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ev
}

// fanout fans ev out as the worker, at at.
func fanout(t *testing.T, org uuid.UUID, ev storage.NotificationEvent, at time.Time) notify.Fanout {
	t.Helper()
	worker := store(t, "fyniq_worker")
	f := notify.Fanout{Store: worker, Dispatcher: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: clock.NewFrozen(at)},
		Clock: clock.NewFrozen(at), Origin: "https://fyniq.test"}
	payload, _ := json.Marshal(map[string]any{"event_id": ev.ID})
	if err := f.Work()(context.Background(), org, storage.OutboxRow{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	return f
}

func mine(t *testing.T, org, user uuid.UUID) []storage.Notification {
	t.Helper()
	var ns []storage.Notification
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		ns, err = u.Notifications().List(ctx, storage.NotificationFilter{UserID: user})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ns
}

func emailOf(t *testing.T, org uuid.UUID, id uuid.UUID) storage.EmailMessage {
	t.Helper()
	return emailMessage(t, org, id)
}

// An event is fanned out once to each person it concerns: a notification in their centre, and an approval emailed at
// once from the worker, which may queue only notification and digest email, naming only its type unless the
// organisation chose details; quiet hours hold immediate email until the hourly job finds them over; a daily digest
// carries what came before 08:00 Singapore time in one email.
// req: FR-NTF-01
// req: FR-NTF-02
// req: FR-NTF-05
func TestNotificationsAreFannedOutEmailedAndDigested(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Notify fan-out Pte Ltd")
	sgt := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, clock.Singapore) }
	user := founder(t, store(t, "fyniq_api"), b.Org, 61, sgt(24, 9))
	verified(t, b.Org, user, "mei@example.com")

	ev := emitted(t, b.Org, user, "approval.requested", "Post journal JV-0042: waits for your approval", sgt(24, 10))
	fanout(t, b.Org, ev, sgt(24, 10))
	fanout(t, b.Org, ev, sgt(24, 10)) // a second run of the same job adds nothing
	ns := mine(t, b.Org, user)
	if len(ns) != 1 || ns[0].Email != storage.EmailImmediate || ns[0].EmailMessageID == nil || !ns[0].Actionable || ns[0].Title != ev.Title {
		t.Fatalf("after fan-out: %+v", ns)
	}
	if m := emailOf(t, b.Org, *ns[0].EmailMessageID); m.Purpose != "notification" || m.Recipient != "mei@example.com" || m.Subject != "An approval waits for you" {
		t.Fatalf("the email, which names only the type until the organisation chooses details: %+v", m)
	}

	// Quiet hours hold immediate email; a daily preference digests; with details on, email carries the title.
	from, to := 22*60, 7*60
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Settings().Record(ctx, notify.DetailsSetting, "false", "true", sgt(24, 0), 1); err != nil {
			return err
		}
		if _, err := u.Notifications().Prefer(ctx, storage.NotificationPreference{UserID: user, Type: storage.QuietHours, QuietFrom: &from, QuietTo: &to}); err != nil {
			return err
		}
		_, err := u.Notifications().Prefer(ctx, storage.NotificationPreference{UserID: user, Type: "approval.decided", Email: storage.EmailDaily})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	quiet := emitted(t, b.Org, user, "approval.requested", "Pay bill B-7: waits for your approval", sgt(24, 23))
	fanout(t, b.Org, quiet, sgt(24, 23))
	daily := emitted(t, b.Org, user, "approval.decided", "Approved: Post journal JV-0042", sgt(24, 23))
	f := fanout(t, b.Org, daily, sgt(24, 23))
	for _, n := range mine(t, b.Org, user)[:2] {
		if n.EmailMessageID != nil {
			t.Fatalf("%s was emailed in quiet hours or before its digest", n.Title)
		}
	}
	f.Clock = clock.NewFrozen(sgt(25, 6)) // still quiet, and before 08:00
	if n, err := f.Digest(ctx); err != nil || n != 0 {
		t.Fatalf("in quiet hours: %d sent, %v", n, err)
	}
	f.Clock = clock.NewFrozen(sgt(25, 8))
	if n, err := f.Digest(ctx); err != nil || n != 2 {
		t.Fatalf("at 08:00: %d sent, %v; want the held approval and the digest", n, err)
	}
	ns = mine(t, b.Org, user)
	if ns[0].DigestID == nil || ns[1].EmailMessageID == nil {
		t.Fatalf("after the job: %+v", ns[:2])
	}
	if m := emailOf(t, b.Org, *ns[1].EmailMessageID); m.Subject != quiet.Title {
		t.Fatalf("the held email, with details: %+v", m)
	}
	if n, err := f.Digest(ctx); err != nil || n != 0 {
		t.Fatalf("again: %d sent, %v", n, err)
	}
}

// An event changes only in its fan-out, once (FY187); a notification only in being read, unread or archived, and in
// the email that carried it (FY188); digests and preferences never change (FY189). The worker queues notification and
// digest email only, never a test message or a sign-in alert, and a person reads only their own notifications.
// req: FR-NTF-05
// req: FR-NTF-01
func TestNotificationsKeepTheirHistory(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Notify history Pte Ltd")
	now := clock.System{}.Now()
	user := founder(t, store(t, "fyniq_api"), b.Org, 62, now)
	ev := emitted(t, b.Org, user, "security.roles", "Your roles changed: controller assigned", now)
	fanout(t, b.Org, ev, now)
	n := mine(t, b.Org, user)[0]
	if n.Severity != "warning" || n.Email != storage.EmailOff {
		t.Fatalf("a person with no verified email: %+v", n)
	}
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Notifications().Mark(ctx, n.ID, user, true, now); err != nil {
			return err
		}
		if c, err := u.Notifications().Unread(ctx, user); err != nil || c != 0 {
			t.Errorf("unread after reading: %d, %v", c, err)
		}
		if _, err := u.Notifications().Archive(ctx, n.ID, user, now); err != nil {
			return err
		}
		if _, err := u.Notifications().Mark(ctx, n.ID, uuid.New(), false, now); err == nil {
			t.Error("someone else marked it")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change an event's title", "UPDATE fyniq.notification_event SET title = 'x' WHERE id = $1", "FY187"},
		{"fyniq_migrator", "fan an event out twice", "UPDATE fyniq.notification_event SET fanned_out_at = now() WHERE id = $1", "FY187"},
		{"fyniq_migrator", "delete an event", "DELETE FROM fyniq.notification_event WHERE id = $1", "FY187"},
		{"fyniq_migrator", "change a notification's type", "UPDATE fyniq.notification SET type = 'approval.decided' WHERE event_id = $1", "FY188"},
		{"fyniq_migrator", "unarchive a notification", "UPDATE fyniq.notification SET archived_at = NULL WHERE event_id = $1", "FY188"},
		{"fyniq_migrator", "delete a notification", "DELETE FROM fyniq.notification WHERE event_id = $1", "FY188"},
		{"fyniq_api", "fan out as the API", "INSERT INTO fyniq.notification (event_id, user_id, type, severity, actionable, email, created_at) SELECT id, $1, type, 'info', false, 'off', now() FROM fyniq.notification_event LIMIT 0", "42501"},
		{"fyniq_worker", "queue a test message as the worker", "INSERT INTO fyniq.email_message (purpose, recipient, subject) SELECT 'test', 'x@example.com', 's' WHERE $1::uuid IS NOT NULL", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, ev.ID); return err })
		wantState(t, x.what, err, x.state, x.state)
	}
	err := asOrg(ctx, conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.email_message (purpose, recipient, subject) VALUES ('notification', 'x@example.com', 's')")
		return err
	})
	if err != nil && !strings.Contains(err.Error(), "audit") {
		t.Fatalf("the worker queueing a notification email: %v", err)
	}
}
