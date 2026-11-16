package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The database's refusals of AI budgets (PRD-001 AI-86; p1-ai-providers task 5.3).
const (
	CodeAIBudget          = "STORAGE-AI-BUDGET-01" // FY336: a budget or reservation changed as it may not, or made outside an organisation
	CodeAIConnectionSpent = "STORAGE-AI-BUDGET-02" // FY337: the connection's budget is spent this month: the next step may serve
	CodeAIBudgetSpent     = "STORAGE-AI-BUDGET-03" // FY338: the organisation's, agent's or person's budget is spent, or cannot apply
)

// The scopes of an AI budget (AI-77).
const (
	AIBudgetOrganisation = "organisation"
	AIBudgetAgent        = "agent"
	AIBudgetConnection   = "connection"
	AIBudgetPerson       = "person"
	// Token budgets (AI-13): a month's tokens for the organisation and for a person, whatever their connections cost.
	AIBudgetTokensOrganisation = "tokens_organisation"
	AIBudgetTokensPerson       = "tokens_person"
)

// AIBudget is a monthly AI budget (AI-77, AI-86, AI-13): in the base currency, the organisation's, an agent's, a
// connection's or a person's; or in tokens, the organisation's or a person's.
type AIBudget struct {
	ID      uuid.UUID
	Scope   string
	Subject string // the agent, the connection's ID or user:<id>; "" for the organisation
	Amount  string // exact decimal text, or a whole number of tokens; "" when removed
	Version int
	Alerted string // the month whose 80% alert it raised
	SetBy   string
	SetAt   time.Time
	Used    string // what this month's reservations hold or settled against it, as List reads it
}

// AIReservation is an attempt's reservation against the budgets that apply (AI-86).
type AIReservation struct {
	Run        uuid.UUID
	Step       int
	Agent      string
	Connection uuid.UUID
	Person     string // who the call ran for
	Month      string // such as 2026-11, in Singapore
	Amount     string // the most it could cost in the base currency, exact; "" when it could not be converted
	Tokens     int64  // the most tokens it could use
}

// AIBudgetAlert is a budget a reservation took to 80% of its amount this month.
type AIBudgetAlert struct {
	Scope   string `json:"scope"`
	Subject string `json:"subject"`
	Amount  string `json:"amount"`
	Used    string `json:"used"`
}

// AIBudgets keeps the organisation's AI budgets and the reservations against them.
type AIBudgets interface {
	// List are the budgets, each with what month's reservations hold or settled against it.
	List(ctx context.Context, month string) ([]AIBudget, error)
	// Set sets a budget as its next version, or the first: audited as ai_budget.set. CodeVersion when another change was
	// made since b.Version-1.
	Set(ctx context.Context, b AIBudget) (AIBudget, error)
	// Reserve reserves an attempt's most against every budget that applies, under their locks, and returns the
	// reservation and the budgets it took to 80%; CodeAIConnectionSpent or CodeAIBudgetSpent when one cannot cover it.
	Reserve(ctx context.Context, r AIReservation) (uuid.UUID, []AIBudgetAlert, error)
	// Settle settles a reservation to what the attempt cost and the tokens it used, or keeps it whole as unknown (known
	// false).
	Settle(ctx context.Context, id uuid.UUID, actual string, tokens int64, known bool) error
}
