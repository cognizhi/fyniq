package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Subscriptions() storage.Subscriptions { return subscriptions{u} }

type subscriptions struct{ u *unitOfWork }

func toSubscription(r sqlcdb.GetSubscriptionContractRow) storage.SubscriptionContract {
	c := storage.SubscriptionContract{ID: r.ID, ContactID: r.ContactID, Name: r.Name, Currency: r.Currency, StartsOn: r.StartsOn.UTC(), MRR: storage.Decimal(r.Mrr),
		Version: int(r.Version), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
	if r.EndsOn.Valid {
		t := r.EndsOn.Time.UTC()
		c.EndsOn = &t
	}
	return c
}

func toAmendment(r sqlcdb.SubscriptionAmendmentsOfRow) storage.SubscriptionAmendment {
	return storage.SubscriptionAmendment{ID: r.ID, ContractID: r.ContractID, Kind: r.Kind, EffectiveOn: r.EffectiveOn.UTC(), MRR: storage.Decimal(r.Mrr), Reason: r.Reason,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}

func (s subscriptions) Create(ctx context.Context, n storage.NewSubscriptionContract) (storage.SubscriptionContract, error) {
	if err := s.u.check(); err != nil {
		return storage.SubscriptionContract{}, err
	}
	var ends pgtype.Date
	if n.EndsOn != nil {
		ends = pgtype.Date{Time: *n.EndsOn, Valid: true}
	}
	r, err := s.u.q.InsertSubscriptionContract(ctx, sqlcdb.InsertSubscriptionContractParams{ContactID: n.ContactID, Name: n.Name, Currency: n.Currency, StartsOn: n.StartsOn,
		EndsOn: ends, Mrr: string(n.MRR), CreatedBy: s.u.actor(ctx), CreatedAt: n.At})
	if err != nil {
		return storage.SubscriptionContract{}, mapError(err)
	}
	out := toSubscription(sqlcdb.GetSubscriptionContractRow(r))
	changes := map[string][2]any{"contact_id": {nil, out.ContactID.String()}, "name": {nil, out.Name}, "currency": {nil, out.Currency}, "starts_on": {nil, out.StartsOn.Format(time.DateOnly)},
		"mrr": {nil, string(out.MRR)}}
	if out.EndsOn != nil {
		changes["ends_on"] = [2]any{nil, out.EndsOn.Format(time.DateOnly)}
	}
	return out, s.u.event(ctx, "subscription_contract.created", "subscription_contract", out.ID, nil, changes)
}

func (s subscriptions) Get(ctx context.Context, id uuid.UUID) (storage.SubscriptionContract, error) {
	if err := s.u.check(); err != nil {
		return storage.SubscriptionContract{}, err
	}
	r, err := s.u.q.GetSubscriptionContract(ctx, id)
	if err != nil {
		return storage.SubscriptionContract{}, mapError(err)
	}
	return toSubscription(r), nil
}

func (s subscriptions) Lock(ctx context.Context, id uuid.UUID) (storage.SubscriptionContract, error) {
	if err := s.u.check(); err != nil {
		return storage.SubscriptionContract{}, err
	}
	r, err := s.u.q.LockSubscriptionContract(ctx, id)
	if err != nil {
		return storage.SubscriptionContract{}, mapError(err)
	}
	return toSubscription(sqlcdb.GetSubscriptionContractRow(r)), nil
}

func (s subscriptions) List(ctx context.Context, contact, after uuid.UUID, limit int) ([]storage.SubscriptionContract, error) {
	if err := s.u.check(); err != nil {
		return nil, err
	}
	rows, err := s.u.q.ListSubscriptionContracts(ctx, sqlcdb.ListSubscriptionContractsParams{After: after, Contact: contact, Max: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SubscriptionContract, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSubscription(sqlcdb.GetSubscriptionContractRow(r)))
	}
	return out, nil
}

func (s subscriptions) Amendments(ctx context.Context, contract uuid.UUID) ([]storage.SubscriptionAmendment, error) {
	if err := s.u.check(); err != nil {
		return nil, err
	}
	rows, err := s.u.q.SubscriptionAmendmentsOf(ctx, contract)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SubscriptionAmendment, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAmendment(r))
	}
	return out, nil
}

func (s subscriptions) AllAmendments(ctx context.Context) ([]storage.SubscriptionAmendment, error) {
	if err := s.u.check(); err != nil {
		return nil, err
	}
	rows, err := s.u.q.AllSubscriptionAmendments(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SubscriptionAmendment, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAmendment(sqlcdb.SubscriptionAmendmentsOfRow(r)))
	}
	return out, nil
}

func (s subscriptions) Amend(ctx context.Context, n storage.NewSubscriptionAmendment, version int) (storage.SubscriptionAmendment, error) {
	if err := s.u.check(); err != nil {
		return storage.SubscriptionAmendment{}, err
	}
	next, err := s.u.q.BumpSubscriptionContract(ctx, sqlcdb.BumpSubscriptionContractParams{ID: n.ContractID, Version: int32(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.SubscriptionAmendment{}, &storage.Error{Code: storage.CodeVersion, Message: "the contract was changed by someone else", Hint: "Reload it and try again"}
	}
	if err != nil {
		return storage.SubscriptionAmendment{}, mapError(err)
	}
	r, err := s.u.q.InsertSubscriptionAmendment(ctx, sqlcdb.InsertSubscriptionAmendmentParams{ContractID: n.ContractID, Kind: n.Kind, EffectiveOn: n.EffectiveOn, Mrr: string(n.MRR),
		Reason: n.Reason, CreatedBy: s.u.actor(ctx), CreatedAt: n.At})
	if err != nil {
		var pg interface{ SQLState() string }
		if errors.As(err, &pg) && pg.SQLState() == "23505" {
			return storage.SubscriptionAmendment{}, &storage.Error{Code: storage.CodeInvalid, Message: "the contract has an amendment for that day already", Hint: "Choose another day"}
		}
		return storage.SubscriptionAmendment{}, mapError(err)
	}
	out := toAmendment(sqlcdb.SubscriptionAmendmentsOfRow(r))
	v := int(next)
	if err := s.u.event(ctx, "subscription_contract.amended", "subscription_contract", n.ContractID, &v, map[string][2]any{"version": {version, int(next)}}); err != nil {
		return out, err
	}
	return out, s.u.event(ctx, "subscription_amendment.created", "subscription_amendment", out.ID, nil, map[string][2]any{"contract_id": {nil, out.ContractID.String()}, "kind": {nil, out.Kind},
		"effective_on": {nil, out.EffectiveOn.Format(time.DateOnly)}, "mrr": {nil, string(out.MRR)}, "reason": {nil, out.Reason}})
}
