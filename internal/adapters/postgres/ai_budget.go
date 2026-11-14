package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AIBudgets() storage.AIBudgets { return aiBudgets{u} }

type aiBudgets struct{ u *unitOfWork }

func toAIBudget(r sqlcdb.FyniqAiBudget) storage.AIBudget {
	b := storage.AIBudget{ID: r.ID, Scope: r.Scope, Subject: r.Subject, Version: int(r.Version), SetBy: r.SetBy, SetAt: r.SetAt}
	if r.Amount != nil {
		b.Amount = aiprice.Trim(*r.Amount)
	}
	if r.Alerted != nil {
		b.Alerted = *r.Alerted
	}
	return b
}

func (r aiBudgets) List(ctx context.Context, month string) ([]storage.AIBudget, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIBudgetsUsed(ctx, month)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIBudget, 0, len(rows))
	for _, row := range rows {
		b := toAIBudget(sqlcdb.FyniqAiBudget{OrganisationID: row.OrganisationID, ID: row.ID, Scope: row.Scope, Subject: row.Subject,
			Amount: row.Amount, Version: row.Version, Alerted: row.Alerted, SetBy: row.SetBy, SetAt: row.SetAt})
		b.Used = aiprice.Trim(row.Used)
		out = append(out, b)
	}
	return out, nil
}

func (r aiBudgets) Set(ctx context.Context, b storage.AIBudget) (storage.AIBudget, error) {
	if err := r.u.check(); err != nil {
		return storage.AIBudget{}, err
	}
	was, err := r.u.q.GetAIBudget(ctx, sqlcdb.GetAIBudgetParams{Scope: b.Scope, Subject: b.Subject})
	var row sqlcdb.FyniqAiBudget
	before := ""
	switch {
	case notFound(err):
		if b.Version != 1 {
			return storage.AIBudget{}, &storage.Error{Code: storage.CodeVersion, Message: "the budget does not exist yet"}
		}
		row, err = r.u.q.InsertAIBudget(ctx, sqlcdb.InsertAIBudgetParams{Scope: b.Scope, Subject: b.Subject, Amount: optional(b.Amount), SetBy: b.SetBy,
			SetAt: b.SetAt})
	case err != nil:
		return storage.AIBudget{}, mapError(err)
	default:
		if was.Amount != nil {
			before = aiprice.Trim(*was.Amount)
		}
		row, err = r.u.q.UpdateAIBudget(ctx, sqlcdb.UpdateAIBudgetParams{Amount: optional(b.Amount), SetBy: b.SetBy, SetAt: b.SetAt, ID: was.ID,
			Version: int32(min(max(b.Version-1, 0), 1<<30))}) // #nosec G115 -- capped
		if notFound(err) {
			return storage.AIBudget{}, &storage.Error{Code: storage.CodeVersion, Message: "the budget was changed since it was read"}
		}
	}
	if err != nil {
		return storage.AIBudget{}, mapError(err)
	}
	out := toAIBudget(row)
	version := out.Version
	return out, r.u.event(ctx, "ai_budget.set", "ai_budget", out.ID, &version, map[string][2]any{
		"amount": {before, out.Amount}, "scope": {nil, out.Scope}, "subject": {nil, out.Subject}})
}

func (r aiBudgets) Reserve(ctx context.Context, res storage.AIReservation) (uuid.UUID, []storage.AIBudgetAlert, error) {
	if err := r.u.check(); err != nil {
		return uuid.Nil, nil, err
	}
	raw, err := r.u.q.ReserveAI(ctx, sqlcdb.ReserveAIParams{Run: res.Run, Step: int32(min(max(res.Step, 0), 5)), Agent: res.Agent, // #nosec G115 -- capped
		Connection: res.Connection, Person: res.Person, Month: res.Month, Amount: optional(res.Amount), Tokens: max(res.Tokens, 0)})
	if err != nil {
		return uuid.Nil, nil, mapError(err)
	}
	var out struct {
		ID     uuid.UUID               `json:"id"`
		Alerts []storage.AIBudgetAlert `json:"alerts"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return uuid.Nil, nil, err
	}
	for i := range out.Alerts {
		out.Alerts[i].Amount, out.Alerts[i].Used = aiprice.Trim(out.Alerts[i].Amount), aiprice.Trim(out.Alerts[i].Used)
	}
	return out.ID, out.Alerts, nil
}

func (r aiBudgets) Settle(ctx context.Context, id uuid.UUID, actual string, tokens int64, known bool) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if id == uuid.Nil {
		return ports.ErrNotFound
	}
	return mapError(r.u.q.SettleAI(ctx, sqlcdb.SettleAIParams{Reservation: id, Actual: optional(actual), ActualTokens: max(tokens, 0), Known: known}))
}
