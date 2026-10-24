//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authz"
)

// verifiedEmail gives user's sign-in identity the email its provider verified, as their next sign-in would.
func verifiedEmail(t *testing.T, e *env, user, email string) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, db.URL("fyniq_migrator"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	var issuer, subject string
	if err := c.QueryRow(ctx, "SELECT issuer, subject FROM fyniq.user_identity WHERE user_id = $1", user).Scan(&issuer, &subject); err != nil {
		t.Fatal(err)
	}
	if err := e.store.RecordIdentityEmail(ctx, issuer, subject, email, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// A person sends themselves a test notification: it reaches them through the same event stream as any, in the app
// always and by email at once when they have a verified sign-in email, and its email shows in the delivery log beside
// the organisation's test message, newest first, filtered by outcome or purpose, a page at a time, never with a body.
// req: FR-NTF-05
func TestTheDeliveryLogAndATestNotification(t *testing.T) {
	f := newFounders(t)
	e := f.e
	weiling, ahkow := f.as(f.weiling), f.as(f.ahkow)
	drainWork(t, e)

	// Without a verified email it is in the app only.
	r := e.call(http.MethodPost, "/v1/notifications/test-messages", ahkow, nil, "Idempotency-Key", uuid.NewString())
	if r.Status != http.StatusAccepted || r.str("email") != "off" || r.str("event_id") == "" || !strings.HasPrefix(r.str("title"), "A test notification you sent at ") {
		t.Fatalf("Ah Kow's test: %d %s", r.Status, r.Raw)
	}
	verifiedEmail(t, e, f.weiling, "weiling@example.com")
	if r := e.call(http.MethodPost, "/v1/notifications/test-messages", weiling, nil); r.str("email") != "immediate" {
		t.Fatalf("Wei Ling's test: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/notifications/test-messages", weiling, nil, "Prefer", "return=preview"); r.Status != 200 {
		t.Fatalf("a preview: %d %s", r.Status, r.Raw)
	}
	fanOutAll(t, e)
	for who, tok := range map[string]string{"Ah Kow": ahkow, "Wei Ling": weiling} {
		if n := e.call(http.MethodGet, "/v1/notifications", tok, nil); strings.Count(n.Raw, `"type":"notification.test"`) != 1 {
			t.Errorf("%s's notifications: %s", who, n.Raw)
		}
	}

	// The organisation's test message, then the delivery log.
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller), map[string]any{"email": "accounts@example.com"}, "If-Match", "*"); p.Status != 200 {
		t.Fatalf("the profile: %d %s", p.Status, p.Raw)
	}
	if m := e.call(http.MethodPost, "/v1/organisation/email/test-messages", e.as(authz.Administrator), nil); m.Status/100 != 2 {
		t.Fatalf("the test message: %d %s", m.Status, m.Raw)
	}
	log := e.call(http.MethodGet, "/v1/organisation/email/messages", e.as(authz.Auditor), nil)
	items, _ := log.Body["items"].([]any)
	if log.Status != 200 || len(items) != 2 || items[0].(map[string]any)["purpose"] != "test" || items[1].(map[string]any)["purpose"] != "notification" ||
		items[1].(map[string]any)["recipient"] != "weiling@example.com" || items[1].(map[string]any)["status"] != "queued" || strings.Contains(log.Raw, `"body"`) {
		t.Fatalf("the delivery log: %d %s", log.Status, log.Raw)
	}
	first := e.call(http.MethodGet, "/v1/organisation/email/messages?page_size=1", e.as(authz.Auditor), nil)
	next := e.call(http.MethodGet, "/v1/organisation/email/messages?page_size=1&cursor="+first.str("next_cursor"), e.as(authz.Auditor), nil)
	if len(first.Body["items"].([]any)) != 1 || next.Body["items"].([]any)[0].(map[string]any)["purpose"] != "notification" || next.str("next_cursor") != "" {
		t.Fatalf("a page at a time: %s %s", first.Raw, next.Raw)
	}
	if only := e.call(http.MethodGet, "/v1/organisation/email/messages?purpose=notification", e.as(authz.Auditor), nil); len(only.Body["items"].([]any)) != 1 {
		t.Fatalf("by purpose: %s", only.Raw)
	}
	if failed := e.call(http.MethodGet, "/v1/organisation/email/messages?status=failed", e.as(authz.Auditor), nil); len(failed.Body["items"].([]any)) != 0 {
		t.Fatalf("by outcome: %s", failed.Raw)
	}
}
