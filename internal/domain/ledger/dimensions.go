package ledger

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// The dimensions' validation codes (FR-DIM-01, FR-DIM-02; rev 0.171).
const (
	CodeDimensionRequired = "DIM-05" // a line a person enters lacks a value its account requires
	CodeDimensionValue    = "DIM-06" // a value the organisation does not have, one out of use, or two values of one dimension
)

// Dimension is one of the organisation's dimensions, as a line's values are checked against it.
type Dimension struct {
	Name   string
	Active bool
}

// DimensionValue is one of the organisation's dimension values: the dimension it is a value of, its code, and whether it
// is in use.
type DimensionValue struct {
	Dimension uuid.UUID
	Code      string
	Active    bool
}

// canonical is a line's values as a dimension set holds them: once each, in ascending order (§11.4). nil for none.
func canonical(values []uuid.UUID) []uuid.UUID {
	if len(values) == 0 {
		return nil
	}
	out := slices.Clone(values)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return slices.Compact(out)
}

// dimensionProblems says what is wrong with a line's values on acc: a value the organisation does not have or that is out
// of use, two values of one dimension (DIM-06), and, when required, a dimension the account requires with no value
// (DIM-05: the lines a person enters; rev 0.171).
func dimensionProblems(s Settings, acc Account, values []uuid.UUID, required bool, field string) []Problem {
	var probs []Problem
	seen := map[uuid.UUID]bool{}
	of := map[uuid.UUID]string{} // dimension → the code of the line's value of it
	for _, v := range values {
		if seen[v] {
			probs = append(probs, Problem{CodeDimensionValue, field, "the line names a value twice", "Name each value once"})
			continue
		}
		seen[v] = true
		x, ok := s.Values[v]
		if !ok {
			probs = append(probs, Problem{CodeDimensionValue, field, "the line names a dimension value the organisation does not have",
				"Choose a value of one of the organisation's dimensions"})
			continue
		}
		d := s.Dimensions[x.Dimension]
		switch {
		case !x.Active:
			probs = append(probs, Problem{CodeDimensionValue, field, fmt.Sprintf("%s %s is out of use", d.Name, x.Code),
				fmt.Sprintf("Choose a value of %s in use, or put %s back in use", d.Name, x.Code)})
		case !d.Active:
			probs = append(probs, Problem{CodeDimensionValue, field, fmt.Sprintf("%s is out of use", d.Name),
				fmt.Sprintf("Leave %s out, or put it back in use", d.Name)})
		}
		if code, twice := of[x.Dimension]; twice {
			probs = append(probs, Problem{CodeDimensionValue, field, fmt.Sprintf("the line names two values of %s: %s and %s", d.Name, code, x.Code),
				fmt.Sprintf("Keep one value of %s", d.Name)})
		}
		of[x.Dimension] = x.Code
	}
	if !required {
		return probs
	}
	var missing []string
	for _, d := range acc.Requires {
		if !s.Dimensions[d].Active {
			continue // a dimension out of use takes no values, so no line needs one
		}
		if _, ok := of[d]; !ok {
			missing = append(missing, s.Dimensions[d].Name)
		}
	}
	if len(missing) > 0 {
		probs = append(probs, Problem{CodeDimensionRequired, field, fmt.Sprintf("account %s needs a value of %s on every line", acc.Code, strings.Join(missing, " and ")),
			fmt.Sprintf("Choose the line's %s", strings.Join(missing, " and "))})
	}
	return probs
}
