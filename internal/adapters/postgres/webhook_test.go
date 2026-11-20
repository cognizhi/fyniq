//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Webhook records keep their rules in the database (FR-INT-12, rev 0.602): the API makes and changes an endpoint but cannot touch a delivery beyond putting a dead one back;
// the worker queues deliveries, records attempts and counts failures, and pauses or disables an active endpoint, and nothing else of an endpoint; a delivered delivery, a
// delivery's payload and every attempt never change; nothing is deleted; a delivery is queued once per event and endpoint; and another organisation sees none of it.
// req: FR-INT-12
// asvs: V4.2.1, V13.2.1
func TestWebhookRecordsKeepTheirRulesInTheDatabase(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Webhook Pte Ltd")
	other := porttest.NewBooks(t, api, "Another webhook Pte Ltd")
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	ep := uuid.Must(uuid.NewV7())
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Webhooks().Create(ctx, storage.NewWebhookEndpoint{ID: ep, URL: "https://hooks.example.com/x", Label: "x", EventTypes: []string{"fyniq.ledger.posted.v1"},
			SecretSealed: []byte("sealed"), SecretHint: "abcd", At: at}, "user:test")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Only the API activates; the worker cannot.
	activate := func(s storage.Store) error {
		return s.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			a := storage.WebhookActive
			_, err := u.Webhooks().Change(ctx, ep, 1, storage.WebhookEndpointChange{State: &a, VerifiedAt: &at, At: at})
			return err
		})
	}
	if err := activate(worker); err == nil {
		t.Error("the worker changed an endpoint")
	}
	if err := activate(api); err != nil {
		t.Fatal(err)
	}
	event := uuid.New()
	var delivery storage.WebhookDelivery
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, made, err := u.Webhooks().Queue(ctx, storage.NewWebhookDelivery{EndpointID: ep, EventID: event, EventType: "fyniq.ledger.posted.v1", MessageID: "msg_a", Payload: []byte(`{"organisation_id":"x"}`), At: at})
		delivery = d
		if err == nil && !made {
			t.Error("the delivery was not queued")
		}
		_, again, err2 := u.Webhooks().Queue(ctx, storage.NewWebhookDelivery{EndpointID: ep, EventID: event, EventType: "fyniq.ledger.posted.v1", MessageID: "msg_b", Payload: []byte(`{}`), At: at})
		if err == nil && (err2 != nil || again) {
			t.Errorf("the same event was queued twice for an endpoint: %v %v", again, err2)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// What the roles may not do.
	for _, q := range []struct{ role, sql string }{
		{"fyniq_worker", "UPDATE fyniq.webhook_endpoint SET url = 'https://evil.example.com/'"},
		{"fyniq_worker", "UPDATE fyniq.webhook_endpoint SET secret_sealed = '\\x00'"},
		{"fyniq_worker", "UPDATE fyniq.webhook_endpoint SET event_types = ARRAY['fyniq.security.alert.v1']"},
		{"fyniq_worker", "UPDATE fyniq.webhook_endpoint SET state = 'pending'"},
		{"fyniq_worker", "DELETE FROM fyniq.webhook_endpoint"},
		{"fyniq_worker", "INSERT INTO fyniq.webhook_endpoint (url, label, event_types, secret_sealed, secret_hint, created_by, created_at, updated_at) VALUES ('https://x.example.com', 'x', ARRAY['a'], '\\x00', 'abcd', 'user:x', now(), now())"},
		{"fyniq_worker", "UPDATE fyniq.webhook_delivery SET payload = '{\"a\":1}'"},
		{"fyniq_worker", "UPDATE fyniq.webhook_delivery SET message_id = 'msg_z'"},
		{"fyniq_worker", "DELETE FROM fyniq.webhook_delivery"},
		{"fyniq_api", "UPDATE fyniq.webhook_delivery SET payload = '{\"a\":1}'"},
		{"fyniq_api", "UPDATE fyniq.webhook_delivery SET state = 'delivered', delivered_at = now()"},
		{"fyniq_api", "INSERT INTO fyniq.webhook_delivery (endpoint_id, event_id, event_type, message_id, payload, next_attempt_at, created_at) VALUES (gen_random_uuid(), gen_random_uuid(), 'fyniq.a.b.v1', 'm', '{}', now(), now())"},
		{"fyniq_api", "INSERT INTO fyniq.webhook_attempt (delivery_id, number, at, duration_ms) VALUES (gen_random_uuid(), 1, now(), 1)"},
		{"fyniq_migrator", "UPDATE fyniq.webhook_endpoint SET url = 'https://evil.example.com/'"},
		{"fyniq_migrator", "DELETE FROM fyniq.webhook_delivery"},
		{"fyniq_migrator", "TRUNCATE fyniq.webhook_endpoint"},
	} {
		c := conn(t, q.role)
		if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q.sql); return err }); err == nil {
			t.Errorf("%s: %s succeeded", q.role, q.sql)
		}
	}
	// A delivery is recorded as delivered once; then nothing changes it, and the API cannot replay it; a dead one the worker cannot move, the API replays once.
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		s := 200
		_, err := u.Webhooks().Record(ctx, delivery.ID, storage.WebhookResult{At: at, Status: &s, Delivered: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Webhooks().Replay(ctx, delivery.ID, at)
		return err
	}); err == nil {
		t.Error("a delivered delivery was replayed")
	}
	c := conn(t, "fyniq_worker")
	if err := asOrg(ctx, c, books.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.webhook_delivery SET attempts = 9")
		return err
	}); err == nil {
		t.Error("the worker changed a delivered delivery")
	}
	event2 := uuid.New()
	var dead storage.WebhookDelivery
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, _, err := u.Webhooks().Queue(ctx, storage.NewWebhookDelivery{EndpointID: ep, EventID: event2, EventType: "fyniq.ledger.posted.v1", MessageID: "msg_c", Payload: []byte(`{}`), At: at})
		if err != nil {
			return err
		}
		s := 503
		dead, err = u.Webhooks().Record(ctx, d.ID, storage.WebhookResult{At: at, Status: &s, Error: "the endpoint answered 503", Dead: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if dead.State != storage.DeliveryDead {
		t.Fatalf("the delivery: %+v", dead)
	}
	if err := worker.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		s := 200
		_, err := u.Webhooks().Record(ctx, dead.ID, storage.WebhookResult{At: at, Status: &s, Delivered: true})
		return err
	}); err == nil {
		t.Error("the worker sent a dead delivery again without a person")
	}
	if err := api.Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Webhooks().Replay(ctx, dead.ID, at)
		if err == nil && (d.State != storage.DeliveryPending || d.Replays != 1) {
			t.Errorf("the replay: %+v", d)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Another organisation sees none of it.
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if all, err := u.Webhooks().List(ctx); err != nil || len(all) != 0 {
			t.Errorf("another organisation sees %d endpoints: %v", len(all), err)
		}
		if _, err := u.Webhooks().Delivery(ctx, delivery.ID); err == nil {
			t.Error("another organisation reads a delivery")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
