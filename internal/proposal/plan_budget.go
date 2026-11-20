package proposal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// plan.propose_budget (PRD-001 Appendix E; p1-ai-proposals task 4.1, design D8; FR-PLN-01): a budget version for a financial year, DRAFTED, through the
// function a person's Draft a budget runs (budget.DraftIn). A draft posts nothing and moves no money, and its approval and so its recording and locking
// are separate acts a person asks for (requestBudgetApproval), so the proposal never approves or records one. v1 budgets the whole company: a line names an
// income or expense account by its code and gives the year's twelve monthly amounts as decimal strings; a line by dimension value is a person's. A draft is
// not undoable (design D4): no function deletes a draft budget, and a person edits it or leaves it; a recorded budget is locked by its approval.

// MaxBudgetLines is how many lines one budget proposal holds.
const MaxBudgetLines = 100

// BudgetLineIn is a line of a proposed budget.
type BudgetLineIn struct {
	Account string   `json:"account"`
	Amounts []string `json:"amounts"`
}

// BudgetPayload is plan.propose_budget's payload.
type BudgetPayload struct {
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	FinancialYear string         `json:"financial_year"`
	Revises       string         `json:"revises"`
	Lines         []BudgetLineIn `json:"lines"`
}

func (p BudgetPayload) budget(ctx context.Context, u storage.UnitOfWork) (budget.Budget, int64, []map[string]any, error) {
	out := budget.Budget{Name: strings.TrimSpace(p.Name), Kind: p.Kind, Year: p.FinancialYear, Revises: p.Revises, Lines: []budget.BudgetLine{}}
	if len(p.Lines) == 0 || len(p.Lines) > MaxBudgetLines {
		return out, 0, nil, refusal(fmt.Sprintf("payload.lines holds %d lines", len(p.Lines)), fmt.Sprintf("Send 1 to %d lines", MaxBudgetLines))
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return out, 0, nil, err
	}
	h := fnv.New64a()
	var shown []map[string]any
	for i, l := range p.Lines {
		ai := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.Code == strings.TrimSpace(l.Account) })
		if ai < 0 {
			return out, 0, nil, refusal(fmt.Sprintf("payload.lines[%d].account is not the code of an account of this organisation", i), "Use the code of an income or expense account as records_search returned it")
		}
		a := accounts[ai]
		fmt.Fprintf(h, "%s:%d:%t;", a.ID, a.Version, a.Active)
		out.Lines = append(out.Lines, budget.BudgetLine{AccountID: a.ID.String(), Dimensions: []string{}, Amounts: slices.Clone(l.Amounts)})
		shown = append(shown, map[string]any{"account": a.Code + " " + a.Name, "amounts": l.Amounts})
	}
	return out, int64(h.Sum64() >> 1), shown, nil // #nosec G115 -- 63 bits
}

// CreateBudgetVersion is plan.propose_budget's command.
type CreateBudgetVersion struct{}

// Policy implements Command: the right to draft a budget.
func (CreateBudgetVersion) Policy() authz.Policy { return authz.CreateBudget }

// Measure implements Command: one budget; its lines are its items for the limit.
func (CreateBudgetVersion) Measure(payload json.RawMessage) (Measure, error) {
	var p BudgetPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Measure{}, err
	}
	return Measure{Items: len(p.Lines)}, nil
}

// DryRun implements Command: the draft is made in the rolled-back unit of work, so every problem the rules find is the person's to read.
func (CreateBudgetVersion) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	b, fingerprint, shown, err := budgetOf(ctx, u, payload)
	if err != nil {
		return Impact{}, err
	}
	if _, err := budget.DraftIn(ctx, u, b); err != nil {
		return Impact{}, budgetProblems(err)
	}
	changes, err := json.Marshal(map[string]any{"budgets": []map[string]any{{"change": "draft", "name": b.Name, "kind": b.Kind, "financial_year": b.Year, "lines": shown}},
		"ledger": "A budget posts nothing. It stays a draft: a person asks for its approval, which records and locks it"})
	if err != nil {
		return Impact{}, err
	}
	return Impact{Summary: fmt.Sprintf("Drafts the %s budget %q for the year from %s with %d line(s); it is not approved or recorded", b.Kind, b.Name, b.Year, len(b.Lines)),
		Changes: changes, Base: map[string]int64{"accounts": fingerprint}}, nil
}

func budgetOf(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage) (budget.Budget, int64, []map[string]any, error) {
	var p BudgetPayload
	if err := decodeStrict(payload, &p); err != nil {
		return budget.Budget{}, 0, nil, err
	}
	return p.budget(ctx, u)
}

// budgetProblems turns the rules' problems, which come together, into the proposal's refusal of the first, naming how many more.
func budgetProblems(err error) error {
	var be *budget.Error
	if errors.As(err, &be) {
		return refusal(be.Message, be.Fix)
	}
	var ps ledger.Problems
	if errors.As(err, &ps) && len(ps) > 0 {
		msg := "payload." + ps[0].Field + " " + ps[0].Message
		if len(ps) > 1 {
			msg += fmt.Sprintf(" (and %d more)", len(ps)-1)
		}
		return refusal(msg, ps[0].Fix)
	}
	return err
}

// Execute implements Command: only while the accounts it names are as the dry run read them (AI-P5).
func (CreateBudgetVersion) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	b, fingerprint, _, err := budgetOf(ctx, u, payload)
	if err != nil {
		return Execution{}, err
	}
	if want, ok := base["accounts"]; !ok || want != fingerprint {
		return Execution{}, ErrStale
	}
	doc, err := budget.DraftIn(ctx, u, b)
	if err != nil {
		return Execution{}, budgetProblems(err)
	}
	return Execution{Records: []string{"budget:" + doc.ID.String()}, Entries: []string{}}, nil
}
