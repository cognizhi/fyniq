package budget

import (
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
)

// The department scope of a budget holder (FR-ADM-01, rev 0.253 and the owner's decisions of 20 Nov 2026 [owner to confirm]).
// Appendix D gives the Approver "C dept" on "Budgets, forecasts": they read and change the lines of their department's values,
// and nothing else of the company's budget. A line is in a department when it carries one of its values; a line with none is the
// company's.

// holdsLine reports whether a line is in the scope: a nil scope holds every line.
func (s *Scope) holdsLine(l BudgetLine) bool {
	if s == nil {
		return true
	}
	var values []uuid.UUID
	for _, v := range l.Dimensions {
		if id, err := uuid.Parse(v); err == nil {
			values = append(values, id)
		}
	}
	return s.Holds(values)
}

// Within is b with only the lines in the scope: the budget as a department's budget holder reads it.
func (s *Scope) Within(b Budget) Budget {
	if s == nil {
		return b
	}
	out := b
	out.Lines = []BudgetLine{}
	for _, l := range b.Lines {
		if s.holdsLine(l) {
			out.Lines = append(out.Lines, l)
		}
	}
	return out
}

// Merge is the draft a department's budget holder saves: the lines of current outside the scope, which are not theirs and stay as
// they are, and the lines they submit, every one in the scope. Their name, kind, year and revised budget are the draft's own:
// a department holder changes lines, not the company's budget. A submission that goes outside the scope, or changes the
// draft's header, is refused whole with every problem.
func (s *Scope) Merge(current, submitted Budget) (Budget, error) {
	var probs ledger.Problems
	if submitted.Name != current.Name || submitted.Kind != current.Kind || submitted.Year != current.Year || submitted.Revises != current.Revises {
		probs = append(probs, ledger.Problem{Code: CodeBudget, Field: "name",
			Message: "a department's budget holder changes the lines of the draft, not its name, kind, year or the budget it revises",
			Fix:     "Send the draft's own name, kind, year and revises as they are, with your department's lines"})
	}
	for i, l := range submitted.Lines {
		if !s.holdsLine(l) {
			probs = append(probs, ledger.Problem{Code: CodeBudget, Field: fmt.Sprintf("lines[%d].dimension_value_ids", i),
				Message: "is for no department you hold: a department's budget holder plans their own department's lines",
				Fix:     "Give each line a value of the department dimension from your departments"})
		}
	}
	if len(probs) > 0 {
		return Budget{}, probs
	}
	out := current
	out.Lines = slices.DeleteFunc(slices.Clone(current.Lines), s.holdsLine)
	out.Lines = append(out.Lines, submitted.Lines...)
	return out, nil
}
