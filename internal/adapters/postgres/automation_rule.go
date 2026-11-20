package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- workflow rules without code (FR-AUT-03; p2-mobile-approvals-automation)

func (u *unitOfWork) AutomationRules() storage.AutomationRules { return automationRules{u} }

type automationRules struct{ u *unitOfWork }

type automationRuleRow = sqlcdb.GetAutomationRuleRow

func toAutomationRule(r automationRuleRow) storage.AutomationRule {
	return storage.AutomationRule{ID: r.ID, Name: r.Name, Trigger: r.Trigger, DocumentType: str(r.DocumentType), AmountAtLeast: storage.Decimal(str(r.AmountAtLeast)), Action: r.Action,
		Recipients: r.Recipients, State: r.State, Version: int(r.Version), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (a automationRules) fields(r storage.AutomationRule) map[string][2]any {
	return map[string][2]any{"name": {nil, r.Name}, "trigger": {nil, r.Trigger}, "document_type": {nil, nilIfEmpty(r.DocumentType)},
		"amount_at_least": {nil, nilIfEmpty(string(r.AmountAtLeast))}, "action": {nil, r.Action}, "recipients": {nil, r.Recipients}, "state": {nil, r.State}}
}

func (a automationRules) Create(ctx context.Context, n storage.NewAutomationRule) (storage.AutomationRule, error) {
	if err := a.u.check(); err != nil {
		return storage.AutomationRule{}, err
	}
	r, err := a.u.q.InsertAutomationRule(ctx, sqlcdb.InsertAutomationRuleParams{Name: n.Name, DocumentType: opt(n.DocumentType), AmountAtLeast: opt(string(n.AmountAtLeast)),
		Recipients: n.Recipients, By: n.By, At: n.At})
	if err != nil {
		return storage.AutomationRule{}, mapError(err)
	}
	v := toAutomationRule(automationRuleRow(r))
	version := v.Version
	return v, a.u.event(ctx, "automation_rule.created", "automation_rule", v.ID, &version, a.fields(v))
}

func (a automationRules) Get(ctx context.Context, id uuid.UUID) (storage.AutomationRule, error) {
	if err := a.u.check(); err != nil {
		return storage.AutomationRule{}, err
	}
	r, err := a.u.q.GetAutomationRule(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AutomationRule{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.AutomationRule{}, mapError(err)
	}
	return toAutomationRule(r), nil
}

func (a automationRules) Lock(ctx context.Context, id uuid.UUID) (storage.AutomationRule, error) {
	if err := a.u.check(); err != nil {
		return storage.AutomationRule{}, err
	}
	r, err := a.u.q.LockAutomationRule(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AutomationRule{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.AutomationRule{}, mapError(err)
	}
	return toAutomationRule(automationRuleRow(r)), nil
}

func (a automationRules) List(ctx context.Context) ([]storage.AutomationRule, error) {
	if err := a.u.check(); err != nil {
		return nil, err
	}
	rows, err := a.u.q.ListAutomationRules(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AutomationRule, len(rows))
	for i, r := range rows {
		out[i] = toAutomationRule(automationRuleRow(r))
	}
	return out, nil
}

func (a automationRules) Active(ctx context.Context, trigger string) ([]storage.AutomationRule, error) {
	if err := a.u.check(); err != nil {
		return nil, err
	}
	rows, err := a.u.q.ActiveAutomationRules(ctx, trigger)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AutomationRule, len(rows))
	for i, r := range rows {
		out[i] = toAutomationRule(automationRuleRow(r))
	}
	return out, nil
}

func (a automationRules) Change(ctx context.Context, id uuid.UUID, version int, c storage.AutomationRuleChange) (storage.AutomationRule, error) {
	if err := a.u.check(); err != nil {
		return storage.AutomationRule{}, err
	}
	if version < 1 || version > 1_000_000 {
		return storage.AutomationRule{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("version %d of a workflow rule", version)}
	}
	r, err := a.u.q.ChangeAutomationRule(ctx, sqlcdb.ChangeAutomationRuleParams{ID: id, Version: int32(version), Name: c.Name, DocumentType: opt(c.DocumentType), // 1 to 1,000,000, checked above
		AmountAtLeast: opt(string(c.AmountAtLeast)), Recipients: c.Recipients, State: c.State, At: c.At})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AutomationRule{}, &storage.Error{Code: storage.CodeVersion, Message: "the workflow rule changed since it was read"}
	}
	if err != nil {
		return storage.AutomationRule{}, mapError(err)
	}
	v := toAutomationRule(automationRuleRow(r))
	n := v.Version
	return v, a.u.event(ctx, "automation_rule.changed", "automation_rule", v.ID, &n, a.fields(v))
}
