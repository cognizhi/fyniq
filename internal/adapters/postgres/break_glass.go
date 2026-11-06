package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ break-glass (FM-8)

func (u *unitOfWork) BreakGlasses() storage.BreakGlasses { return breakGlasses{u} }

type breakGlasses struct{ u *unitOfWork }

func toBreakGlass(r sqlcdb.FyniqBreakGlass) storage.BreakGlass {
	return storage.BreakGlass{ID: r.ID, StartedBy: r.StartedBy, Reason: r.Reason, StartedAt: r.StartedAt, ExpiresAt: r.ExpiresAt, EndedAt: r.EndedAt,
		EndedBy: r.EndedBy, Currency: r.Currency, Cap: storage.Decimal(r.Cap), Version: int(r.Version)}
}

func toBreakGlassUse(r sqlcdb.FyniqBreakGlassUse) storage.BreakGlassUse {
	return storage.BreakGlassUse{ID: r.ID, BreakGlassID: r.BreakGlassID, BatchID: r.BatchID, BatchVersion: int(r.BatchVersion), UsedBy: r.UsedBy,
		UsedAt: r.UsedAt, Currency: r.Currency, Total: storage.Decimal(r.Total)}
}

func (r breakGlasses) List(ctx context.Context) ([]storage.BreakGlass, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListBreakGlass(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	uses, err := r.u.q.ListBreakGlassUses(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	byGlass := map[uuid.UUID][]storage.BreakGlassUse{}
	for _, x := range uses {
		byGlass[x.BreakGlassID] = append(byGlass[x.BreakGlassID], toBreakGlassUse(x))
	}
	out := make([]storage.BreakGlass, len(rows))
	for i, x := range rows {
		out[i] = toBreakGlass(x)
		out[i].Uses = byGlass[x.ID]
	}
	return out, nil
}

func (r breakGlasses) Get(ctx context.Context, id uuid.UUID) (storage.BreakGlass, error) {
	if err := r.u.check(); err != nil {
		return storage.BreakGlass{}, err
	}
	row, err := r.u.q.GetBreakGlass(ctx, id)
	if err != nil {
		return storage.BreakGlass{}, mapError(err)
	}
	out := toBreakGlass(row)
	uses, err := r.u.q.ListBreakGlassUses(ctx)
	if err != nil {
		return storage.BreakGlass{}, mapError(err)
	}
	for _, x := range uses {
		if x.BreakGlassID == id {
			out.Uses = append(out.Uses, toBreakGlassUse(x))
		}
	}
	return out, nil
}

func (r breakGlasses) Start(ctx context.Context, n storage.NewBreakGlass) (storage.BreakGlass, error) {
	if err := r.u.check(); err != nil {
		return storage.BreakGlass{}, err
	}
	row, err := r.u.q.InsertBreakGlass(ctx, sqlcdb.InsertBreakGlassParams{StartedBy: n.StartedBy, Reason: n.Reason, StartedAt: n.StartedAt,
		Cap: string(n.Cap), Currency: n.Currency})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BreakGlass{}, &storage.Error{Code: storage.CodeInvalid, Message: "the currency " + n.Currency + " is not known"}
	}
	if err != nil {
		return storage.BreakGlass{}, mapError(err)
	}
	out := toBreakGlass(row)
	v := out.Version
	return out, r.u.event(ctx, "break_glass.started", "break_glass", out.ID, &v, map[string][2]any{
		"started_by": {nil, out.StartedBy}, "reason": {nil, out.Reason}, "started_at": {nil, out.StartedAt.UTC().Format(time.RFC3339Nano)},
		"expires_at": {nil, out.ExpiresAt.UTC().Format(time.RFC3339Nano)}, "cap": {nil, map[string]any{"amount": string(out.Cap), "currency": out.Currency}}})
}

func (r breakGlasses) End(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time) (storage.BreakGlass, error) {
	if err := r.u.check(); err != nil {
		return storage.BreakGlass{}, err
	}
	row, err := r.u.q.EndBreakGlass(ctx, sqlcdb.EndBreakGlassParams{EndedAt: at, EndedBy: by, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BreakGlass{}, &storage.Error{Code: storage.CodeVersion, Message: "the break-glass changed or ended since it was read"}
	}
	if err != nil {
		return storage.BreakGlass{}, mapError(err)
	}
	out := toBreakGlass(row)
	v := out.Version
	return out, r.u.event(ctx, "break_glass.ended", "break_glass", id, &v, map[string][2]any{
		"ended_at": {nil, at.UTC().Format(time.RFC3339Nano)}, "ended_by": {nil, by}})
}

func (r breakGlasses) Use(ctx context.Context, x storage.BreakGlassUse) (storage.BreakGlassUse, error) {
	if err := r.u.check(); err != nil {
		return storage.BreakGlassUse{}, err
	}
	row, err := r.u.q.InsertBreakGlassUse(ctx, sqlcdb.InsertBreakGlassUseParams{BreakGlassID: x.BreakGlassID, BatchID: x.BatchID,
		BatchVersion: small(x.BatchVersion), UsedBy: x.UsedBy, UsedAt: x.UsedAt, Currency: x.Currency, Total: string(x.Total)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BreakGlassUse{}, &storage.Error{Code: storage.CodeInvalid, Message: "the currency " + x.Currency + " is not known"}
	}
	if err != nil {
		return storage.BreakGlassUse{}, mapError(err)
	}
	out := toBreakGlassUse(row)
	return out, r.u.event(ctx, "break_glass.used", "break_glass", out.BreakGlassID, nil, map[string][2]any{
		"use": {nil, map[string]any{"id": out.ID.String(), "batch_id": out.BatchID.String(), "batch_version": out.BatchVersion,
			"used_at": out.UsedAt.UTC().Format(time.RFC3339Nano), "total": map[string]any{"amount": string(out.Total), "currency": out.Currency}}}})
}
