// Package search reads a global search's query (PRD-001 FR-UX-02; rev 0.190): an amount, such as 1,234.56, S$1,234.56,
// > 5k or <= 2.5m, or text, such as a reference, a name or words of a note. Amounts are exact decimals, never floats
// (ENG-10), compared without their sign: a credit of 1,234.56 is found as 1,234.56.
package search

import (
	"regexp"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

// MaxLength is the longest query read, in characters.
const MaxLength = 200

// Query is a query, read. Text is what text must hold, empty for none. Min and Max bound an amount, as decimal strings,
// nil for no bound, each inclusive or not. Either says a record matches on its text or its amount: a bare whole number,
// such as 138 or 2026, may be either a reference's digits or an amount.
type Query struct {
	Text                       string
	Min, Max                   *string
	MinInclusive, MaxInclusive bool
	Either                     bool
}

// HasAmount reports whether q bounds an amount.
func (q Query) HasAmount() bool { return q.Min != nil || q.Max != nil }

var amountForm = regexp.MustCompile(`^(>=|<=|>|<|=)?\s*(S\$|\$|SGD\s*)?-?([0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(\.[0-9]+)?\s*([kKmM])?$`)

// Parse reads s: an amount when the whole of it is one, else text.
func Parse(s string) Query {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > MaxLength {
		s = string(r[:MaxLength])
	}
	m := amountForm.FindStringSubmatch(s)
	if m == nil {
		return Query{Text: s}
	}
	op, sign, whole, fraction, suffix := m[1], m[2], m[3], m[4], m[5]
	v, _, err := apd.NewFromString(strings.ReplaceAll(whole, ",", "") + fraction)
	if err != nil {
		return Query{Text: s}
	}
	switch strings.ToLower(suffix) {
	case "k":
		v.Exponent += 3
	case "m":
		v.Exponent += 6
	}
	if _, _, err := apd.BaseContext.Reduce(v, v); err != nil {
		return Query{Text: s}
	}
	amount := v.Text('f')
	q := Query{}
	switch op {
	case ">":
		q.Min = &amount
	case ">=":
		q.Min, q.MinInclusive = &amount, true
	case "<":
		q.Max = &amount
	case "<=":
		q.Max, q.MaxInclusive = &amount, true
	default:
		q.Min, q.Max, q.MinInclusive, q.MaxInclusive = &amount, &amount, true, true
		if op == "" && sign == "" && fraction == "" && suffix == "" && !strings.Contains(whole, ",") {
			q.Text, q.Either = s, true
		}
	}
	return q
}
