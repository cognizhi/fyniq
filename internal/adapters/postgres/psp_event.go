package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) PSPEvents() storage.PSPEvents { return pspEvents{u} }

type pspEvents struct{ u *unitOfWork }

type pspRow = sqlcdb.GetPSPEventRow

func toPSPEvent(r pspRow) storage.PSPEvent {
	return storage.PSPEvent{ID: r.ID, ConnectorID: r.ConnectorID, Provider: r.Provider, EventID: r.EventID, EventType: r.EventType,
		APIVersion: str(r.ApiVersion), ParserVersion: r.ParserVersion, ObjectKind: str(r.ObjectKind), ObjectID: str(r.ObjectID),
		PayloadSHA256: r.PayloadSha256, ReceivedAt: r.ReceivedAt, Status: r.Status, ParkedReason: str(r.ParkedReason),
		ProcessedAt: r.ProcessedAt, LastError: str(r.LastError)}
}

func (r pspEvents) Receive(ctx context.Context, n storage.NewPSPEvent) (storage.PSPEvent, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.PSPEvent{}, false, err
	}
	row, err := r.u.q.InsertPSPEvent(ctx, sqlcdb.InsertPSPEventParams{ID: n.ID, ConnectorID: n.ConnectorID, Provider: n.Provider,
		EventID: n.EventID, EventType: clipTo(n.EventType, 120), ApiVersion: opt(clipTo(n.APIVersion, 120)), ParserVersion: n.ParserVersion,
		ObjectKind: opt(n.ObjectKind), ObjectID: opt(clipTo(n.ObjectID, 255)), Payload: n.Payload, PayloadSha256: n.PayloadSHA256, ReceivedAt: n.ReceivedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PSPEvent{}, false, nil // the provider sent it before: the first row stands
	}
	if err != nil {
		return storage.PSPEvent{}, false, mapError(err)
	}
	e := toPSPEvent(pspRow(row))
	if err := r.u.event(ctx, "psp_event.received", "psp_event", e.ID, nil, map[string][2]any{
		"connector_id": {nil, e.ConnectorID.String()}, "provider": {nil, e.Provider}, "event_id": {nil, e.EventID}, "event_type": {nil, e.EventType},
		"object_kind": {nil, nilIfEmpty(e.ObjectKind)}, "object_id": {nil, nilIfEmpty(e.ObjectID)}, "payload_sha256": {nil, e.PayloadSHA256}}); err != nil {
		return storage.PSPEvent{}, false, err
	}
	return e, true, nil
}

func (r pspEvents) Get(ctx context.Context, id uuid.UUID) (storage.PSPEvent, error) {
	if err := r.u.check(); err != nil {
		return storage.PSPEvent{}, err
	}
	row, err := r.u.q.GetPSPEvent(ctx, id)
	if err != nil {
		return storage.PSPEvent{}, mapError(err)
	}
	return toPSPEvent(row), nil
}

func (r pspEvents) Payload(ctx context.Context, id uuid.UUID) ([]byte, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	b, err := r.u.q.GetPSPEventPayload(ctx, id)
	return b, mapError(err)
}

func (r pspEvents) Settle(ctx context.Context, id uuid.UUID, status, reason string, at time.Time) (storage.PSPEvent, error) {
	if err := r.u.check(); err != nil {
		return storage.PSPEvent{}, err
	}
	before, err := r.Get(ctx, id)
	if err != nil {
		return storage.PSPEvent{}, err
	}
	p := sqlcdb.SettlePSPEventParams{ID: id, Status: status}
	switch status {
	case storage.PSPProcessed:
		p.ProcessedAt = &at
	case storage.PSPParked:
		p.ParkedReason = opt(clip(reason))
	case storage.PSPFailed:
		p.LastError = opt(clip(reason))
	default:
		return storage.PSPEvent{}, fmt.Errorf("a provider event is settled as processed, parked or failed, not %q", status)
	}
	row, err := r.u.q.SettlePSPEvent(ctx, p)
	if err != nil {
		return storage.PSPEvent{}, mapError(err)
	}
	e := toPSPEvent(pspRow(row))
	return e, r.u.event(ctx, "psp_event."+e.Status, "psp_event", e.ID, nil, map[string][2]any{
		"status": {before.Status, e.Status}, "parked_reason": {nil, nilIfEmpty(e.ParkedReason)}, "last_error": {nil, nilIfEmpty(e.LastError)}})
}

func (r pspEvents) List(ctx context.Context, f storage.PSPEventFilter) ([]storage.PSPEvent, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 || limit > math.MaxInt32 {
		limit = 100
	}
	rows, err := r.u.q.ListPSPEvents(ctx, sqlcdb.ListPSPEventsParams{Status: f.Status, Connector: f.Connector, Before: f.Before, MaxRows: int32(limit)}) // 1 to MaxInt32, checked above
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PSPEvent, 0, len(rows))
	for _, x := range rows {
		out = append(out, toPSPEvent(pspRow(x)))
	}
	return out, nil
}
