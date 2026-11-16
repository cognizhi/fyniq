package approval

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The conditions of a policy (FR-APR-01; PRD-001 rev 0.178, the owner's decision of 6 Nov 2026): a policy is an ordered
// list of rules, each with conditions and its own steps, and its default steps. The first rule whose conditions all hold
// decides a request's steps; when none does, the default steps do. Entity and risk tier wait for several entities and
// for AI proposals.

// CodeRules: a policy's rules are not ones that can be applied (FR-APR-01).
const CodeRules = "APR-16"

// MaxRules is how many rules a policy has at most.
const MaxRules = 10

// Conditions are what a rule matches: each one set must hold.
type Conditions struct {
	AmountAtLeast string      // a decimal in the base currency, the request's amount at or above it; "" for none
	Departments   []uuid.UUID // a line carries one of these dimension values
	Accounts      []uuid.UUID // a line posts to one of these accounts
	Vendors       []uuid.UUID // the vendor is one of these contacts
}

// Empty reports whether no condition is set.
func (c Conditions) Empty() bool {
	return c.AmountAtLeast == "" && len(c.Departments) == 0 && len(c.Accounts) == 0 && len(c.Vendors) == 0
}

// Rule is a rule of a policy: its conditions, and the steps a request that matches them takes.
type Rule struct {
	Conditions Conditions
	Steps      []Step
}

// Facts are what a request's change says about itself, which the conditions match on. A condition on something the
// change does not carry never holds.
type Facts struct {
	Amount   *money.Money // in the base currency; nil when it has none
	Values   []uuid.UUID  // the dimension values on its lines
	Accounts []uuid.UUID  // the accounts its lines post to
	Vendors  []uuid.UUID  // its vendor
}

// Match reports whether every condition set holds for f.
func (c Conditions) Match(f Facts) (bool, error) {
	if c.Empty() {
		return false, nil
	}
	if c.AmountAtLeast != "" {
		if f.Amount == nil {
			return false, nil
		}
		at, err := money.Parse(c.AmountAtLeast, f.Amount.Currency())
		if err != nil {
			return false, err
		}
		if n, err := f.Amount.Cmp(at); err != nil || n < 0 {
			return false, err
		}
	}
	anyOf := func(want, have []uuid.UUID) bool {
		return len(want) == 0 || slices.ContainsFunc(have, func(id uuid.UUID) bool { return slices.Contains(want, id) })
	}
	return anyOf(c.Departments, f.Values) && anyOf(c.Accounts, f.Accounts) && anyOf(c.Vendors, f.Vendors), nil
}

// StepsFor are the steps a request with facts f takes under p, and the rule that chose them, numbered from 1; 0 when
// no rule matches and p's default steps apply.
func (p Policy) StepsFor(f Facts) ([]Step, int, error) {
	for i, r := range p.Rules {
		ok, err := r.Conditions.Match(f)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			return r.Steps, i + 1, nil
		}
	}
	return p.Steps, 0, nil
}

// CheckRules checks a policy's rules: at most MaxRules, each with a condition and an amount that is a decimal of the
// base currency at or above zero, each condition naming at most 50 records, and each rule's steps as CheckSteps checks
// a policy's.
func CheckRules(row string, rules []Rule, base money.Currency, approves func(role string) bool, members []Member) []FieldProblem {
	if len(rules) > MaxRules {
		return []FieldProblem{{"body.rules", CodeRules, fmt.Sprintf("a policy has at most %d rules, not %d", MaxRules, len(rules)),
			fmt.Sprintf("Keep at most %d rules; the default steps cover the rest", MaxRules)}}
	}
	var out []FieldProblem
	for i, r := range rules {
		at := fmt.Sprintf("body.rules[%d]", i)
		c := r.Conditions
		if c.Empty() {
			out = append(out, FieldProblem{at + ".conditions", CodeRules, "a rule has no condition, so it would match nothing",
				"Set an amount, departments, accounts or vendors, or remove the rule"})
		}
		if c.AmountAtLeast != "" {
			if m, err := money.Parse(c.AmountAtLeast, base); err != nil || m.Sign() < 0 {
				out = append(out, FieldProblem{at + ".conditions.amount_at_least", CodeRules,
					fmt.Sprintf("is not an amount of %s at or above zero", base.Code), fmt.Sprintf("Send a decimal such as 5000.00, in %s", base.Code)})
			}
		}
		for name, ids := range map[string][]uuid.UUID{"departments": c.Departments, "accounts": c.Accounts, "vendors": c.Vendors} {
			if len(ids) > 50 {
				out = append(out, FieldProblem{at + ".conditions." + name, CodeRules, "names more than 50 records", "Name at most 50"})
			}
		}
		for _, x := range CheckSteps(row, r.Steps, approves, members) {
			x.Field = strings.Replace(x.Field, "body.steps", at+".steps", 1)
			out = append(out, x)
		}
	}
	return out
}
