// Package dimension checks the organisation's dimensions and their values as entered (PRD-001 FR-DIM-01): Department
// and Project once each and up to 8 custom dimensions, each named once, and values with a code unique in their
// dimension. Neither is ever deleted, as dimension sets and the posted lines that name them refer to them: one is put
// out of use instead, and keeps its place, its kind and its code. Every problem comes back together (ENG-22), in plain
// words with its code (ENG-23).
package dimension

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The error codes (ENG-23).
const (
	CodeKind      = "DIM-01" // not a kind of dimension, Department or Project a second time, or a ninth custom one
	CodeName      = "DIM-02" // a name that is empty, too long or another's
	CodeCode      = "DIM-03" // a value's code that is not in the format, or another value's of its dimension
	CodeDimension = "DIM-04" // a value's dimension that the organisation does not have, or that is out of use
)

// MaxCustom is how many custom dimensions an organisation has at most, besides Department and Project (FR-DIM-01).
const MaxCustom = 8

// The longest names, as the database keeps them.
const (
	MaxName      = 100
	MaxValueName = 200
)

// Kinds are the kinds of dimension, in the order the organisation's list shows them.
var Kinds = []string{storage.DimensionDepartment, storage.DimensionProject, storage.DimensionCustom}

// codeFormat is a value's code: a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens.
var codeFormat = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._/-]{0,29}$`)

// Problem is what is wrong with a field, and how to fix it.
type Problem struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// New checks a new dimension against the organisation's others, and gives the place a custom one takes: the first
// that is free, as one out of use keeps its place.
func New(kind, name string, others []storage.Dimension) (storage.NewDimension, []Problem) {
	n := storage.NewDimension{Kind: kind, Name: strings.TrimSpace(name)}
	var probs []Problem
	switch kind {
	case storage.DimensionDepartment, storage.DimensionProject:
		for _, o := range others {
			if o.Kind == kind {
				probs = append(probs, Problem{CodeKind, "kind", fmt.Sprintf("the organisation has its %s dimension already, named %s", kind, o.Name),
					fmt.Sprintf("Change %s, or put it back in use", o.Name)})
			}
		}
	case storage.DimensionCustom:
		taken := map[int]bool{}
		for _, o := range others {
			if o.Kind == storage.DimensionCustom {
				taken[o.Place] = true
			}
		}
		for p := 1; p <= MaxCustom && n.Place == 0; p++ {
			if !taken[p] {
				n.Place = p
			}
		}
		if n.Place == 0 {
			probs = append(probs, Problem{CodeKind, "kind", fmt.Sprintf("the organisation has %d custom dimensions already, the most it has", MaxCustom),
				"Rename one that is out of use and put it back in use, instead of adding another"})
		}
	default:
		probs = append(probs, Problem{CodeKind, "kind", fmt.Sprintf("%q is not a kind of dimension", kind), "Choose department, project or custom"})
	}
	return n, append(probs, checkName("name", n.Name, MaxName, "dimension", names(others, uuid.Nil))...)
}

// Change checks a change of self against the organisation's other dimensions.
func Change(self storage.Dimension, c storage.DimensionChange, others []storage.Dimension) []Problem {
	return checkName("name", strings.TrimSpace(c.Name), MaxName, "dimension", names(others, self.ID))
}

// NewValue checks a new value of dim (nil when the organisation has no such dimension) against the dimension's other
// values.
func NewValue(n storage.NewDimensionValue, dim *storage.Dimension, others []storage.DimensionValue) []Problem {
	var probs []Problem
	switch {
	case dim == nil:
		probs = append(probs, Problem{CodeDimension, "dimension_id", "the organisation has no such dimension", "Choose one of the organisation's dimensions"})
	case !dim.Active:
		probs = append(probs, Problem{CodeDimension, "dimension_id", fmt.Sprintf("%s is out of use", dim.Name),
			fmt.Sprintf("Put %s back in use first", dim.Name)})
	}
	code := strings.TrimSpace(n.Code)
	switch {
	case !codeFormat.MatchString(code):
		probs = append(probs, Problem{CodeCode, "code", fmt.Sprintf("%q is not a code: a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens", code),
			"Enter a short code such as ENG or P-2026-01"})
	case dim != nil:
		for _, o := range others {
			if strings.EqualFold(o.Code, code) {
				probs = append(probs, Problem{CodeCode, "code", fmt.Sprintf("%s has the value %s already, named %s", dim.Name, o.Code, o.Name),
					"Enter another code, or put that value back in use"})
			}
		}
	}
	return append(probs, checkName("name", strings.TrimSpace(n.Name), MaxValueName, "value", valueNames(others, uuid.Nil))...)
}

// ChangeValue checks a change of self against the other values of its dimension.
func ChangeValue(self storage.DimensionValue, c storage.DimensionChange, others []storage.DimensionValue) []Problem {
	return checkName("name", strings.TrimSpace(c.Name), MaxValueName, "value", valueNames(others, self.ID))
}

// names are the names of the dimensions other than self.
func names(ds []storage.Dimension, self uuid.UUID) []string {
	var out []string
	for _, d := range ds {
		if d.ID != self {
			out = append(out, d.Name)
		}
	}
	return out
}

// valueNames are the names of the values other than self.
func valueNames(vs []storage.DimensionValue, self uuid.UUID) []string {
	var out []string
	for _, v := range vs {
		if v.ID != self {
			out = append(out, v.Name)
		}
	}
	return out
}

// checkName says what is wrong with a name: empty, longer than most, or another's, whatever its case.
func checkName(field, name string, most int, what string, others []string) []Problem {
	switch {
	case name == "":
		return []Problem{{CodeName, field, fmt.Sprintf("the %s has no name", what), fmt.Sprintf("Enter the %s's name", what)}}
	case len([]rune(name)) > most:
		return []Problem{{CodeName, field, fmt.Sprintf("the name is %d characters, more than %d", len([]rune(name)), most), "Shorten the name"}}
	case slices.ContainsFunc(others, func(o string) bool { return strings.EqualFold(o, name) }):
		return []Problem{{CodeName, field, fmt.Sprintf("another %s is named %s", what, name), "Choose another name"}}
	}
	return nil
}
