package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AutomationRule is a workflow rule without code (FR-AUT-03): a trigger, typed conditions and one action, both from fixed lists the database holds.
type AutomationRule struct {
	ID            uuid.UUID
	Name          string
	Trigger       string  // document.posted
	DocumentType  string  // the kind of document, such as bill; "" for any kind
	AmountAtLeast Decimal // the document's debits in the base currency; "" for any amount
	Action        string  // notify
	Recipients    []uuid.UUID
	State         string // active, paused or archived
	Version       int
	CreatedBy     uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewAutomationRule is what AutomationRules.Create needs.
type NewAutomationRule struct {
	Name, DocumentType string
	AmountAtLeast      Decimal
	Recipients         []uuid.UUID
	By                 uuid.UUID
	At                 time.Time
}

// AutomationRuleChange is what AutomationRules.Change sets: every field, as the API merged them onto the version it read.
type AutomationRuleChange struct {
	Name, DocumentType, State string
	AmountAtLeast             Decimal
	Recipients                []uuid.UUID
	At                        time.Time
}

// AutomationRules are the organisation's workflow rules. The database refuses an action or trigger off its lists, a change other than as the next version, a change to an
// archived rule and a delete (CodeInvalid). Every change is audited.
type AutomationRules interface {
	Create(ctx context.Context, n NewAutomationRule) (AutomationRule, error)
	// Get returns a rule, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (AutomationRule, error)
	// Lock is Get with the row locked until commit.
	Lock(ctx context.Context, id uuid.UUID) (AutomationRule, error)
	// List returns every rule, oldest first.
	List(ctx context.Context) ([]AutomationRule, error)
	// Active returns the active rules of a trigger, oldest first.
	Active(ctx context.Context, trigger string) ([]AutomationRule, error)
	// Change changes a rule at version: CodeVersion when it changed since.
	Change(ctx context.Context, id uuid.UUID, version int, c AutomationRuleChange) (AutomationRule, error)
}
