package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- the parent's consolidation set-up and runs (FR-ENT-05, FR-ENT-06; p3-multi-entity)

func (u *unitOfWork) Consolidations() storage.Consolidations { return consolidations{u} }

type consolidations struct{ u *unitOfWork }

type pairJSON struct {
	A uuid.UUID `json:"a"`
	B uuid.UUID `json:"b"`
}

func (c consolidations) Pairs(ctx context.Context) ([]storage.GroupPair, int, error) {
	if err := c.u.check(); err != nil {
		return nil, 0, err
	}
	row, err := c.u.q.GetGroupSettings(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, mapError(err)
	}
	var ps []pairJSON
	if err := json.Unmarshal([]byte(row.Pairs), &ps); err != nil {
		return nil, 0, err
	}
	out := make([]storage.GroupPair, len(ps))
	for i, p := range ps {
		out[i] = storage.GroupPair{A: p.A, B: p.B}
	}
	return out, int(row.Version), nil
}

func (c consolidations) SetPairs(ctx context.Context, pairs []storage.GroupPair, version int, by uuid.UUID, at time.Time) error {
	if err := c.u.check(); err != nil {
		return err
	}
	ps := make([]pairJSON, len(pairs))
	for i, p := range pairs {
		ps[i] = pairJSON{A: p.A, B: p.B}
	}
	doc, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	if version == 0 {
		if err := c.u.q.InsertGroupSettings(ctx, sqlcdb.InsertGroupSettingsParams{Pairs: doc, By: by, At: at}); err != nil {
			return mapError(err)
		}
		v := 1
		return c.u.event(ctx, "group_settings.set", "group_settings", c.u.org, &v, map[string][2]any{"pairs": {nil, string(doc)}})
	}
	next, err := c.u.q.UpdateGroupSettings(ctx, sqlcdb.UpdateGroupSettingsParams{Pairs: doc, By: by, At: at, Version: int32(version)}) // a version read from the row
	if errors.Is(err, pgx.ErrNoRows) {
		return &storage.Error{Code: storage.CodeVersion, Message: "the group's set-up changed since it was read"}
	}
	if err != nil {
		return mapError(err)
	}
	v := int(next)
	return c.u.event(ctx, "group_settings.changed", "group_settings", c.u.org, &v, map[string][2]any{"pairs": {nil, string(doc)}})
}

func toGroupMapping(r sqlcdb.GetGroupMappingByCodeRow) storage.GroupMapping {
	m := storage.GroupMapping{ID: r.ID, EntityAccountCode: r.EntityAccountCode, GroupAccountID: r.GroupAccountID, CounterpartyParent: r.CounterpartyParent, Version: int(r.Version), UpdatedAt: r.UpdatedAt}
	if r.EntityRow != nil {
		m.EntityRow = *r.EntityRow
	}
	if r.CounterpartyRow != nil {
		m.CounterpartyRow = *r.CounterpartyRow
	}
	return m
}

func uuidOrNil(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func (c consolidations) Mappings(ctx context.Context) ([]storage.GroupMapping, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListGroupMappings(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.GroupMapping, len(rows))
	for i, r := range rows {
		out[i] = toGroupMapping(sqlcdb.GetGroupMappingByCodeRow(r))
	}
	return out, nil
}

func (c consolidations) Map(ctx context.Context, n storage.NewGroupMapping) (storage.GroupMapping, error) {
	if err := c.u.check(); err != nil {
		return storage.GroupMapping{}, err
	}
	changes := map[string][2]any{"entity_row": {nil, uuidOrNil(n.EntityRow)}, "entity_account_code": {nil, n.EntityAccountCode}, "group_account_id": {nil, n.GroupAccountID},
		"counterparty_row": {nil, uuidOrNil(n.CounterpartyRow)}, "counterparty_parent": {nil, n.CounterpartyParent}}
	before, err := c.u.q.GetGroupMappingByCode(ctx, sqlcdb.GetGroupMappingByCodeParams{EntityRow: uuidOrNil(n.EntityRow), Code: n.EntityAccountCode})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err := c.u.q.InsertGroupMapping(ctx, sqlcdb.InsertGroupMappingParams{EntityRow: uuidOrNil(n.EntityRow), Code: n.EntityAccountCode, GroupAccountID: n.GroupAccountID,
			CounterpartyRow: uuidOrNil(n.CounterpartyRow), CounterpartyParent: n.CounterpartyParent, By: n.By, At: n.At})
		if err != nil {
			return storage.GroupMapping{}, mapError(err)
		}
		m := toGroupMapping(sqlcdb.GetGroupMappingByCodeRow(row))
		v := m.Version
		return m, c.u.event(ctx, "group_account_map.created", "group_account_map", m.ID, &v, changes)
	}
	if err != nil {
		return storage.GroupMapping{}, mapError(err)
	}
	row, err := c.u.q.UpdateGroupMapping(ctx, sqlcdb.UpdateGroupMappingParams{ID: before.ID, GroupAccountID: n.GroupAccountID, CounterpartyRow: uuidOrNil(n.CounterpartyRow),
		CounterpartyParent: n.CounterpartyParent, By: n.By, At: n.At})
	if err != nil {
		return storage.GroupMapping{}, mapError(err)
	}
	m := toGroupMapping(sqlcdb.GetGroupMappingByCodeRow(row))
	v := m.Version
	changes["group_account_id"] = [2]any{before.GroupAccountID, n.GroupAccountID}
	changes["counterparty_row"] = [2]any{before.CounterpartyRow, uuidOrNil(n.CounterpartyRow)}
	changes["counterparty_parent"] = [2]any{before.CounterpartyParent, n.CounterpartyParent}
	return m, c.u.event(ctx, "group_account_map.changed", "group_account_map", m.ID, &v, changes)
}

func toGroupRate(r sqlcdb.ListConsolidationRatesRow) storage.GroupRate {
	return storage.GroupRate{ID: r.ID, Currency: r.Currency, Month: r.Month, Closing: storage.Decimal(r.Closing), Average: storage.Decimal(r.Average), Note: r.Note, EnteredBy: r.EnteredBy, EnteredAt: r.EnteredAt}
}

func (c consolidations) Rates(ctx context.Context) ([]storage.GroupRate, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListConsolidationRates(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.GroupRate, len(rows))
	for i, r := range rows {
		out[i] = toGroupRate(r)
	}
	return out, nil
}

func (c consolidations) AddRate(ctx context.Context, n storage.NewGroupRate) (storage.GroupRate, error) {
	if err := c.u.check(); err != nil {
		return storage.GroupRate{}, err
	}
	row, err := c.u.q.InsertConsolidationRate(ctx, sqlcdb.InsertConsolidationRateParams{Currency: n.Currency, Month: n.Month, Closing: string(n.Closing), Average: string(n.Average), Note: n.Note, By: n.By, At: n.At})
	if err != nil {
		return storage.GroupRate{}, mapError(err)
	}
	r := toGroupRate(sqlcdb.ListConsolidationRatesRow(row))
	return r, c.u.event(ctx, "consolidation_rate.entered", "consolidation_rate", r.ID, nil, map[string][2]any{"currency": {nil, r.Currency}, "month": {nil, r.Month.Format("2006-01")},
		"closing": {nil, string(r.Closing)}, "average": {nil, string(r.Average)}, "note": {nil, r.Note}})
}

func toConsolidationRun(id uuid.UUID, from, to time.Time, currency string, provisional bool, inputs, result string, by uuid.UUID, at time.Time) storage.ConsolidationRun {
	return storage.ConsolidationRun{ID: id, From: from, To: to, Currency: currency, Provisional: provisional, Inputs: []byte(inputs), Result: []byte(result), CreatedBy: by, CreatedAt: at}
}

func (c consolidations) Runs(ctx context.Context) ([]storage.ConsolidationRun, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListConsolidationRuns(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ConsolidationRun, len(rows))
	for i, r := range rows {
		out[i] = toConsolidationRun(r.ID, r.FromMonth, r.ToMonth, r.Currency, r.Provisional, "", "", r.CreatedBy, r.CreatedAt)
	}
	return out, nil
}

func (c consolidations) Run(ctx context.Context, id uuid.UUID) (storage.ConsolidationRun, error) {
	if err := c.u.check(); err != nil {
		return storage.ConsolidationRun{}, err
	}
	r, err := c.u.q.GetConsolidationRun(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ConsolidationRun{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ConsolidationRun{}, mapError(err)
	}
	return toConsolidationRun(r.ID, r.FromMonth, r.ToMonth, r.Currency, r.Provisional, r.Inputs, r.Result, r.CreatedBy, r.CreatedAt), nil
}

func (c consolidations) SaveRun(ctx context.Context, n storage.NewConsolidationRun) (storage.ConsolidationRun, error) {
	if err := c.u.check(); err != nil {
		return storage.ConsolidationRun{}, err
	}
	r, err := c.u.q.InsertConsolidationRun(ctx, sqlcdb.InsertConsolidationRunParams{FromMonth: n.From, ToMonth: n.To, Currency: n.Currency, Provisional: n.Provisional,
		Inputs: n.Inputs, Result: n.Result, By: n.By, At: n.At})
	if err != nil {
		return storage.ConsolidationRun{}, mapError(err)
	}
	run := toConsolidationRun(r.ID, r.FromMonth, r.ToMonth, r.Currency, r.Provisional, r.Inputs, r.Result, r.CreatedBy, r.CreatedAt)
	// The trail names the run, its months and whether it is provisional; the figures stay in the run (SEC-15 holds: no secret is in them).
	return run, c.u.event(ctx, "consolidation_run.made", "consolidation_run", run.ID, nil, map[string][2]any{"from": {nil, n.From.Format("2006-01")}, "to": {nil, n.To.Format("2006-01")},
		"provisional": {nil, n.Provisional}, "currency": {nil, n.Currency}})
}
