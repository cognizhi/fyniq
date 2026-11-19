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

func (u *unitOfWork) Rates() storage.Rates { return rates{u} }

type rates struct{ u *unitOfWork }

func toCostRate(r sqlcdb.FyniqPersonCostRate) storage.CostRate {
	return storage.CostRate{ID: r.ID, UserID: r.UserID, Role: r.RoleName, From: r.EffectiveFrom, Hourly: storage.Decimal(r.HourlyRate), Currency: r.Currency, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}

func toBillingRate(r sqlcdb.FyniqBillingRate) storage.BillingRate {
	role := ""
	if r.RoleName != nil {
		role = *r.RoleName
	}
	return storage.BillingRate{ID: r.ID, ProjectID: r.ProjectID, CustomerID: r.CustomerID, UserID: r.UserID, Role: role, From: r.EffectiveFrom, Hourly: storage.Decimal(r.HourlyRate),
		Currency: r.Currency, CreatedAt: r.CreatedAt}
}

func (r rates) CostRates(ctx context.Context) ([]storage.CostRate, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.CostRates(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CostRate, 0, len(rows))
	for _, x := range rows {
		out = append(out, toCostRate(x))
	}
	return out, nil
}

func (r rates) AddCostRate(ctx context.Context, n storage.NewCostRate) (storage.CostRate, error) {
	if err := r.u.check(); err != nil {
		return storage.CostRate{}, err
	}
	row, err := r.u.q.InsertCostRate(ctx, sqlcdb.InsertCostRateParams{UserID: n.UserID, RoleName: n.Role, EffectiveFrom: n.From, HourlyRate: string(n.Hourly), By: r.u.actor(ctx), Currency: n.Currency})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CostRate{}, &storage.Error{Code: storage.CodeInvalid, Message: "unknown currency " + n.Currency}
	}
	if err != nil {
		return storage.CostRate{}, mapError(err)
	}
	out := toCostRate(row)
	changes := map[string][2]any{"cost_rate": {nil, string(out.Hourly)}, "role": {nil, out.Role}, "effective_from": {nil, out.From.Format(time.DateOnly)}}
	if out.UserID != nil {
		changes["user_id"] = [2]any{nil, out.UserID.String()}
	}
	return out, r.u.event(ctx, "cost_rate.entered", "person_cost_rate", out.ID, nil, changes)
}

func (r rates) BillingRates(ctx context.Context, project uuid.UUID, customer *uuid.UUID) ([]storage.BillingRate, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BillingRates(ctx, sqlcdb.BillingRatesParams{ProjectID: &project, CustomerID: customer})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BillingRate, 0, len(rows))
	for _, x := range rows {
		out = append(out, toBillingRate(x))
	}
	return out, nil
}

func (r rates) AddBillingRate(ctx context.Context, n storage.NewBillingRate) (storage.BillingRate, error) {
	if err := r.u.check(); err != nil {
		return storage.BillingRate{}, err
	}
	var role *string
	if n.Role != "" {
		role = &n.Role
	}
	row, err := r.u.q.InsertBillingRate(ctx, sqlcdb.InsertBillingRateParams{ProjectID: n.ProjectID, CustomerID: n.CustomerID, UserID: n.UserID, RoleName: role, EffectiveFrom: n.From,
		HourlyRate: string(n.Hourly), By: r.u.actor(ctx), Currency: n.Currency})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BillingRate{}, &storage.Error{Code: storage.CodeInvalid, Message: "unknown currency " + n.Currency}
	}
	if err != nil {
		return storage.BillingRate{}, mapError(err)
	}
	out := toBillingRate(row)
	changes := map[string][2]any{"hourly_rate": {nil, string(out.Hourly)}, "effective_from": {nil, out.From.Format(time.DateOnly)}, "project_id": {nil, idText(out.ProjectID)},
		"customer_id": {nil, idText(out.CustomerID)}, "user_id": {nil, idText(out.UserID)}, "role": {nil, out.Role}}
	return out, r.u.event(ctx, "billing_rate.entered", "billing_rate", out.ID, nil, changes)
}
