package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- outbound webhooks (FR-INT-12; p2-api-third-parties)

func (u *unitOfWork) Webhooks() storage.Webhooks { return webhooks{u} }

type webhooks struct{ u *unitOfWork }

// endpointRow is every query's row of an endpoint: sqlc gives each its own type, with the same fields.
type endpointRow = sqlcdb.GetWebhookEndpointRow

func toEndpoint(r endpointRow) storage.WebhookEndpoint {
	return storage.WebhookEndpoint{ID: r.ID, URL: r.Url, Label: r.Label, EventTypes: r.EventTypes, State: r.State, SecretSealed: r.SecretSealed, SecretHint: r.SecretHint,
		PreviousSealed: r.PreviousSecretSealed, PreviousUntil: r.PreviousSecretUntil, ChallengeHash: str(r.ChallengeHash), VerifiedAt: r.VerifiedAt, FailureRun: int(r.FailureRun),
		PausedReason: str(r.PausedReason), Version: int(r.Version), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (w webhooks) Create(ctx context.Context, n storage.NewWebhookEndpoint, by string) (storage.WebhookEndpoint, error) {
	if err := w.u.check(); err != nil {
		return storage.WebhookEndpoint{}, err
	}
	r, err := w.u.q.InsertWebhookEndpoint(ctx, sqlcdb.InsertWebhookEndpointParams{ID: n.ID, Url: n.URL, Label: n.Label, EventTypes: n.EventTypes, SecretSealed: n.SecretSealed,
		SecretHint: n.SecretHint, CreatedBy: by, At: n.At})
	if err != nil {
		return storage.WebhookEndpoint{}, mapError(err)
	}
	e := toEndpoint(endpointRow(r))
	v := e.Version
	return e, w.u.event(ctx, "webhook_endpoint.created", "webhook_endpoint", e.ID, &v, map[string][2]any{"url": {nil, e.URL}, "label": {nil, e.Label},
		"event_types": {nil, e.EventTypes}, "state": {nil, e.State}}) // the secret is never in an audit event
}

func (w webhooks) Get(ctx context.Context, id uuid.UUID) (storage.WebhookEndpoint, error) {
	if err := w.u.check(); err != nil {
		return storage.WebhookEndpoint{}, err
	}
	r, err := w.u.q.GetWebhookEndpoint(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.WebhookEndpoint{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.WebhookEndpoint{}, mapError(err)
	}
	return toEndpoint(r), nil
}

func (w webhooks) Lock(ctx context.Context, id uuid.UUID) (storage.WebhookEndpoint, error) {
	if err := w.u.check(); err != nil {
		return storage.WebhookEndpoint{}, err
	}
	r, err := w.u.q.LockWebhookEndpoint(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.WebhookEndpoint{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.WebhookEndpoint{}, mapError(err)
	}
	return toEndpoint(endpointRow(r)), nil
}

func (w webhooks) List(ctx context.Context) ([]storage.WebhookEndpoint, error) {
	if err := w.u.check(); err != nil {
		return nil, err
	}
	rows, err := w.u.q.ListWebhookEndpoints(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.WebhookEndpoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, toEndpoint(endpointRow(r)))
	}
	return out, nil
}

func (w webhooks) Subscribed(ctx context.Context, eventType string) ([]storage.WebhookEndpoint, error) {
	if err := w.u.check(); err != nil {
		return nil, err
	}
	rows, err := w.u.q.SubscribedWebhookEndpoints(ctx, eventType)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.WebhookEndpoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, toEndpoint(endpointRow(r)))
	}
	return out, nil
}

func (w webhooks) Change(ctx context.Context, id uuid.UUID, version int, c storage.WebhookEndpointChange) (storage.WebhookEndpoint, error) {
	before, err := w.Get(ctx, id)
	if err != nil {
		return storage.WebhookEndpoint{}, err
	}
	r, err := w.u.q.ChangeWebhookEndpoint(ctx, sqlcdb.ChangeWebhookEndpointParams{ID: id, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		Url: c.URL, Label: c.Label, EventTypes: c.EventTypes, State: c.State, SecretSealed: c.SecretSealed, SecretHint: c.SecretHint, ClearPrevious: c.ClearPrevious,
		PreviousSealed: c.PreviousSealed, PreviousUntil: c.PreviousUntil, ClearChallenge: c.ClearChallenge, ChallengeHash: c.ChallengeHash, ClearVerified: c.ClearVerified,
		VerifiedAt: c.VerifiedAt, ClearPaused: c.ClearPaused, PausedReason: c.PausedReason, ResetFailures: c.ResetFailures, At: c.At})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.WebhookEndpoint{}, &storage.Error{Code: storage.CodeVersion, Message: "the endpoint changed since it was read"}
	}
	if err != nil {
		return storage.WebhookEndpoint{}, mapError(err)
	}
	e := toEndpoint(endpointRow(r))
	v := e.Version
	changes := map[string][2]any{"state": {before.State, e.State}}
	if before.URL != e.URL {
		changes["url"] = [2]any{before.URL, e.URL}
	}
	if before.Label != e.Label {
		changes["label"] = [2]any{before.Label, e.Label}
	}
	if len(before.EventTypes) != len(e.EventTypes) || (len(c.EventTypes) > 0) {
		changes["event_types"] = [2]any{before.EventTypes, e.EventTypes}
	}
	if len(c.SecretSealed) > 0 {
		changes["secret_hint"] = [2]any{before.SecretHint, e.SecretHint} // the secret rolled: its last four characters, never the secret
	}
	return e, w.u.event(ctx, "webhook_endpoint.changed", "webhook_endpoint", e.ID, &v, changes)
}

func toDelivery(r sqlcdb.GetWebhookDeliveryRow) storage.WebhookDelivery {
	d := storage.WebhookDelivery{ID: r.ID, EndpointID: r.EndpointID, EventID: r.EventID, EventType: r.EventType, MessageID: r.MessageID, Payload: r.Payload, State: r.State,
		Attempts: int(r.Attempts), Replays: int(r.Replays), NextAttemptAt: r.NextAttemptAt, FirstAttemptAt: r.FirstAttemptAt, LastAttemptAt: r.LastAttemptAt,
		LastError: str(r.LastError), DeliveredAt: r.DeliveredAt, CreatedAt: r.CreatedAt}
	if r.LastStatus != nil {
		s := int(*r.LastStatus)
		d.LastStatus = &s
	}
	return d
}

func (w webhooks) Queue(ctx context.Context, n storage.NewWebhookDelivery) (storage.WebhookDelivery, bool, error) {
	if err := w.u.check(); err != nil {
		return storage.WebhookDelivery{}, false, err
	}
	r, err := w.u.q.InsertWebhookDelivery(ctx, sqlcdb.InsertWebhookDeliveryParams{EndpointID: n.EndpointID, EventID: n.EventID, EventType: n.EventType, MessageID: n.MessageID,
		Payload: n.Payload, At: n.At})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.WebhookDelivery{}, false, nil // queued before
	}
	if err != nil {
		return storage.WebhookDelivery{}, false, mapError(err)
	}
	return toDelivery(sqlcdb.GetWebhookDeliveryRow(r)), true, nil
}

func (w webhooks) Due(ctx context.Context, now time.Time, limit int) ([]storage.WebhookDelivery, error) {
	if err := w.u.check(); err != nil {
		return nil, err
	}
	rows, err := w.u.q.DueWebhookDeliveries(ctx, sqlcdb.DueWebhookDeliveriesParams{Now: now, MaxRows: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- bounded
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.WebhookDelivery, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDelivery(sqlcdb.GetWebhookDeliveryRow(r)))
	}
	return out, nil
}

func (w webhooks) Delivery(ctx context.Context, id uuid.UUID) (storage.WebhookDelivery, error) {
	if err := w.u.check(); err != nil {
		return storage.WebhookDelivery{}, err
	}
	r, err := w.u.q.GetWebhookDelivery(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.WebhookDelivery{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.WebhookDelivery{}, mapError(err)
	}
	return toDelivery(r), nil
}

func (w webhooks) Deliveries(ctx context.Context, q storage.WebhookQuery) ([]storage.WebhookDelivery, error) {
	if err := w.u.check(); err != nil {
		return nil, err
	}
	rows, err := w.u.q.ListWebhookDeliveries(ctx, sqlcdb.ListWebhookDeliveriesParams{EndpointID: q.EndpointID, State: q.State, After: q.After, MaxRows: int32(min(max(q.Limit, 1), 1000))}) // #nosec G115 -- bounded
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.WebhookDelivery, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDelivery(sqlcdb.GetWebhookDeliveryRow(r)))
	}
	return out, nil
}

func (w webhooks) Attempts(ctx context.Context, delivery uuid.UUID) ([]storage.WebhookAttempt, error) {
	if err := w.u.check(); err != nil {
		return nil, err
	}
	rows, err := w.u.q.ListWebhookAttempts(ctx, delivery)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.WebhookAttempt, 0, len(rows))
	for _, r := range rows {
		a := storage.WebhookAttempt{ID: r.ID, DeliveryID: r.DeliveryID, Number: int(r.Number), At: r.At, Error: str(r.Error), DurationMS: int(r.DurationMs)}
		if r.Status != nil {
			s := int(*r.Status)
			a.Status = &s
		}
		out = append(out, a)
	}
	return out, nil
}

func (w webhooks) Record(ctx context.Context, delivery uuid.UUID, r storage.WebhookResult) (storage.WebhookDelivery, error) {
	if err := w.u.check(); err != nil {
		return storage.WebhookDelivery{}, err
	}
	var status *int32
	if r.Status != nil {
		s := int32(*r.Status) // #nosec G115 -- an HTTP status
		status = &s
	}
	var errText *string
	if r.Error != "" {
		e := r.Error
		if len(e) > 200 {
			e = e[:200]
		}
		errText = &e
	}
	var next *time.Time
	if !r.NextAt.IsZero() {
		next = &r.NextAt
	}
	row, err := w.u.q.RecordWebhookResult(ctx, sqlcdb.RecordWebhookResultParams{ID: delivery, Delivered: r.Delivered, Dead: r.Dead, NextAt: next, At: r.At, Status: status, Error: errText})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.WebhookDelivery{}, &storage.Error{Code: storage.CodeVersion, Message: "the delivery is not waiting to be sent"}
	}
	if err != nil {
		return storage.WebhookDelivery{}, mapError(err)
	}
	d := toDelivery(sqlcdb.GetWebhookDeliveryRow(row))
	if err := w.u.q.InsertWebhookAttempt(ctx, sqlcdb.InsertWebhookAttemptParams{DeliveryID: delivery, Number: int32(d.Attempts), At: r.At, Status: status, Error: errText, // #nosec G115 -- bounded
		DurationMs: int32(min(max(r.DurationMS, 0), 1<<30))}); err != nil { // #nosec G115 -- bounded
		return storage.WebhookDelivery{}, mapError(err)
	}
	switch {
	case r.Delivered:
		return d, mapError(w.u.q.ClearWebhookFailures(ctx, sqlcdb.ClearWebhookFailuresParams{ID: d.EndpointID, At: r.At}))
	case r.Status != nil && *r.Status == 410:
		if err := w.u.q.DisableWebhookEndpoint(ctx, sqlcdb.DisableWebhookEndpointParams{ID: d.EndpointID, At: r.At}); err != nil {
			return d, mapError(err)
		}
		ep, err := w.Get(ctx, d.EndpointID)
		if err != nil || ep.State != storage.WebhookDisabled {
			return d, err
		}
		v := ep.Version
		return d, w.u.event(ctx, "webhook_endpoint.disabled", "webhook_endpoint", ep.ID, &v, map[string][2]any{"state": {storage.WebhookActive, storage.WebhookDisabled}, "reason": {nil, ep.PausedReason}})
	}
	run, err := w.u.q.CountWebhookFailure(ctx, sqlcdb.CountWebhookFailureParams{ID: d.EndpointID, At: r.At})
	if err != nil {
		return d, mapError(err)
	}
	if run < tenFailures {
		return d, nil
	}
	if err := w.u.q.PauseWebhookEndpoint(ctx, sqlcdb.PauseWebhookEndpointParams{ID: d.EndpointID, At: r.At}); err != nil {
		return d, mapError(err)
	}
	ep, err := w.Get(ctx, d.EndpointID)
	if err != nil || ep.State != storage.WebhookPaused {
		return d, err
	}
	v := ep.Version
	return d, w.u.event(ctx, "webhook_endpoint.paused", "webhook_endpoint", ep.ID, &v, map[string][2]any{"state": {storage.WebhookActive, storage.WebhookPaused}, "reason": {nil, ep.PausedReason}})
}

// tenFailures is how many deliveries failed in a row pause an active endpoint.
const tenFailures = 10

func (w webhooks) Replay(ctx context.Context, delivery uuid.UUID, at time.Time) (storage.WebhookDelivery, error) {
	if err := w.u.check(); err != nil {
		return storage.WebhookDelivery{}, err
	}
	row, err := w.u.q.ReplayWebhookDelivery(ctx, sqlcdb.ReplayWebhookDeliveryParams{ID: delivery, At: at})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gone := w.Delivery(ctx, delivery); errors.Is(gone, ports.ErrNotFound) {
			return storage.WebhookDelivery{}, ports.ErrNotFound
		}
		return storage.WebhookDelivery{}, &storage.Error{Code: storage.CodeInvalid, Message: "only a dead delivery is replayed", Hint: "A pending or delivered one needs nothing"}
	}
	if err != nil {
		return storage.WebhookDelivery{}, mapError(err)
	}
	d := toDelivery(sqlcdb.GetWebhookDeliveryRow(row))
	return d, w.u.event(ctx, "webhook_delivery.replayed", "webhook_delivery", d.ID, nil, map[string][2]any{"state": {storage.DeliveryDead, storage.DeliveryPending}, "replays": {d.Replays - 1, d.Replays}})
}
