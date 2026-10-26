package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) ClaimCategories() storage.ClaimCategories { return claimCategories{u} }

type claimCategories struct{ u *unitOfWork }

func toClaimCategory(r sqlcdb.FyniqClaimCategory) storage.ClaimCategory {
	return storage.ClaimCategory{ID: r.ID, Name: r.Name, AccountID: r.AccountID, Mileage: r.Mileage, Currency: r.Currency,
		LineLimit: toDecPtr(r.LineLimit), ReceiptAbove: toDecPtr(r.ReceiptAbove), AttendeesAbove: toDecPtr(r.AttendeesAbove),
		Active: r.Active, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

// decText is a limit as the audit trail records it: nil when not set.
func decText(d *storage.Decimal) any {
	if d == nil {
		return nil
	}
	return string(*d)
}

func (r claimCategories) Create(ctx context.Context, n storage.NewClaimCategory) (storage.ClaimCategory, error) {
	if err := r.u.check(); err != nil {
		return storage.ClaimCategory{}, err
	}
	row, err := r.u.q.InsertClaimCategory(ctx, sqlcdb.InsertClaimCategoryParams{Name: n.Name, AccountID: n.AccountID, Mileage: n.Mileage,
		LineLimit: decPtr(n.LineLimit), ReceiptAbove: decPtr(n.ReceiptAbove), AttendeesAbove: decPtr(n.AttendeesAbove), By: r.u.actor(ctx),
		At: n.At, Currency: n.Currency})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ClaimCategory{}, &storage.Error{Code: storage.CodeInvalid, Message: "unknown currency " + n.Currency}
	}
	if err != nil {
		return storage.ClaimCategory{}, mapError(err)
	}
	out := toClaimCategory(row)
	return out, r.u.event(ctx, "claim_category.created", "claim_category", out.ID, &out.Version, map[string][2]any{"name": {nil, out.Name},
		"account_id": {nil, out.AccountID.String()}, "mileage": {nil, out.Mileage}, "currency": {nil, out.Currency},
		"line_limit": {nil, decText(out.LineLimit)}, "receipt_above": {nil, decText(out.ReceiptAbove)},
		"attendees_above": {nil, decText(out.AttendeesAbove)}})
}

func (r claimCategories) Change(ctx context.Context, id uuid.UUID, version int, n storage.NewClaimCategory) (storage.ClaimCategory, error) {
	before, err := r.Get(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.UpdateClaimCategory(ctx, sqlcdb.UpdateClaimCategoryParams{Name: n.Name, AccountID: n.AccountID,
		LineLimit: decPtr(n.LineLimit), ReceiptAbove: decPtr(n.ReceiptAbove), AttendeesAbove: decPtr(n.AttendeesAbove), Active: n.Active,
		By: r.u.actor(ctx), At: n.At, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ClaimCategory{}, &storage.Error{Code: storage.CodeVersion, Message: "the claim category changed since it was read"}
	}
	if err != nil {
		return storage.ClaimCategory{}, mapError(err)
	}
	after := toClaimCategory(row)
	changes := map[string][2]any{}
	if before.Name != after.Name {
		changes["name"] = [2]any{before.Name, after.Name}
	}
	if before.AccountID != after.AccountID {
		changes["account_id"] = [2]any{before.AccountID.String(), after.AccountID.String()}
	}
	for _, f := range []struct {
		name string
		b, a *storage.Decimal
	}{{"line_limit", before.LineLimit, after.LineLimit}, {"receipt_above", before.ReceiptAbove, after.ReceiptAbove},
		{"attendees_above", before.AttendeesAbove, after.AttendeesAbove}} {
		if decText(f.b) != decText(f.a) {
			changes[f.name] = [2]any{decText(f.b), decText(f.a)}
		}
	}
	if before.Active != after.Active {
		changes["active"] = [2]any{before.Active, after.Active}
	}
	return after, r.u.event(ctx, "claim_category.changed", "claim_category", id, &after.Version, changes)
}

func (r claimCategories) List(ctx context.Context) ([]storage.ClaimCategory, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ClaimCategories(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ClaimCategory, 0, len(rows))
	for _, x := range rows {
		out = append(out, toClaimCategory(x))
	}
	return out, nil
}

func (r claimCategories) Get(ctx context.Context, id uuid.UUID) (storage.ClaimCategory, error) {
	if err := r.u.check(); err != nil {
		return storage.ClaimCategory{}, err
	}
	row, err := r.u.q.ClaimCategory(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ClaimCategory{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ClaimCategory{}, mapError(err)
	}
	return toClaimCategory(row), nil
}

func (u *unitOfWork) ClaimPlaces() storage.ClaimPlaces { return claimPlaces{u} }

type claimPlaces struct{ u *unitOfWork }

func toClaimPlace(r sqlcdb.FyniqClaimPlace) storage.ClaimPlace {
	return storage.ClaimPlace{ID: r.ID, UserID: r.UserID, Name: r.Name, Address: r.Address, Active: r.Active, CreatedAt: r.CreatedAt,
		ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

func (r claimPlaces) Create(ctx context.Context, n storage.NewClaimPlace) (storage.ClaimPlace, error) {
	if err := r.u.check(); err != nil {
		return storage.ClaimPlace{}, err
	}
	row, err := r.u.q.InsertClaimPlace(ctx, sqlcdb.InsertClaimPlaceParams{UserID: n.UserID, Name: n.Name, Address: n.Address, By: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.ClaimPlace{}, mapError(err)
	}
	out := toClaimPlace(row)
	return out, r.u.event(ctx, "claim_place.created", "claim_place", out.ID, &out.Version, map[string][2]any{"user_id": {nil, out.UserID.String()},
		"name": {nil, out.Name}, "address": {nil, out.Address}})
}

func (r claimPlaces) Change(ctx context.Context, id uuid.UUID, version int, n storage.NewClaimPlace) (storage.ClaimPlace, error) {
	before, err := r.Get(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.UpdateClaimPlace(ctx, sqlcdb.UpdateClaimPlaceParams{Name: n.Name, Address: n.Address, Active: n.Active, By: r.u.actor(ctx), At: n.At,
		ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ClaimPlace{}, &storage.Error{Code: storage.CodeVersion, Message: "the saved place changed since it was read"}
	}
	if err != nil {
		return storage.ClaimPlace{}, mapError(err)
	}
	after := toClaimPlace(row)
	changes := map[string][2]any{}
	if before.Name != after.Name {
		changes["name"] = [2]any{before.Name, after.Name}
	}
	if before.Address != after.Address {
		changes["address"] = [2]any{before.Address, after.Address}
	}
	if before.Active != after.Active {
		changes["active"] = [2]any{before.Active, after.Active}
	}
	return after, r.u.event(ctx, "claim_place.changed", "claim_place", id, &after.Version, changes)
}

func (r claimPlaces) List(ctx context.Context, user uuid.UUID) ([]storage.ClaimPlace, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ClaimPlaces(ctx, user)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ClaimPlace, 0, len(rows))
	for _, x := range rows {
		out = append(out, toClaimPlace(x))
	}
	return out, nil
}

func (r claimPlaces) Get(ctx context.Context, id uuid.UUID) (storage.ClaimPlace, error) {
	if err := r.u.check(); err != nil {
		return storage.ClaimPlace{}, err
	}
	row, err := r.u.q.ClaimPlace(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ClaimPlace{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ClaimPlace{}, mapError(err)
	}
	return toClaimPlace(row), nil
}
