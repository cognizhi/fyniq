// Package automation is the pure part of workflow rules without code (PRD-001 FR-AUT-03; p2-mobile-approvals-automation design D5). A rule is a trigger from a fixed list, typed
// conditions and one action from a fixed list. There is no expression language and no script: a condition is a kind of document and an amount at least, and the only action is to tell
// members. Nothing here can post, approve, pay, file or send outside Fyniq, and the lists are held by the database too.
package automation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
)

// The triggers and actions a rule may name. Both lists are CHECKs of the table (migration 001202).
const (
	TriggerDocumentPosted = "document.posted"
	ActionNotify          = "notify"
)

// The codes of the rule refusals.
const (
	CodeRule     = "AUT-01" // a rule that cannot be saved: its name, kind of document, amount or recipients
	CodeOff      = "AUT-02" // workflow rules are off for the organisation
	CodeNotFound = "AUT-03" // no such rule
)

// Limits of a rule and of an organisation's rules.
const (
	MaxRecipients = 20
	MaxRules      = 50
)

// Rule is what matching needs of a rule.
type Rule struct {
	DocumentType  string       // "" for any kind
	AmountAtLeast *apd.Decimal // nil for any amount
}

// Posted is what a rule is matched against: a document of a kind was posted, with its debits in the base currency.
type Posted struct {
	DocumentType string
	AmountBase   *apd.Decimal
}

// Matches reports whether a posting satisfies every condition of r that is set.
func Matches(r Rule, p Posted) bool {
	if r.DocumentType != "" && r.DocumentType != p.DocumentType {
		return false
	}
	if r.AmountAtLeast != nil {
		if p.AmountBase == nil || p.AmountBase.Cmp(r.AmountAtLeast) < 0 {
			return false
		}
	}
	return true
}

// FieldProblem is a problem with one field of a rule.
type FieldProblem struct{ Field, Code, Message, Fix string }

// Check returns what is wrong with a rule's fields: its name, a kind of document from kinds, an amount of at most four decimals (the base currency's own scale is checked
// when it is compared), and between one and MaxRecipients recipients, none twice, each among members (the active members).
func Check(name, documentType, amount string, recipients []uuid.UUID, kinds, members []string) []FieldProblem {
	var out []FieldProblem
	if n := len([]rune(strings.TrimSpace(name))); n < 1 || n > 100 {
		out = append(out, FieldProblem{"body.name", CodeRule, "is empty or longer than 100 characters", "Give the rule a short name"})
	}
	if documentType != "" && !slices.Contains(kinds, documentType) {
		out = append(out, FieldProblem{"body.document_type", CodeRule, "is not a kind of document a rule watches", "Choose one of: " + strings.Join(kinds, ", ") + ", or leave it out for any kind"})
	}
	if amount != "" {
		d, _, err := apd.NewFromString(amount)
		if err != nil || d.Negative || d.Form != apd.Finite || d.Exponent < -4 {
			out = append(out, FieldProblem{"body.amount_at_least", CodeRule, "is not an amount of zero or more with at most four decimals", `Write it as a decimal string such as "5000.00"`})
		}
	}
	if len(recipients) < 1 || len(recipients) > MaxRecipients {
		out = append(out, FieldProblem{"body.recipients", CodeRule, fmt.Sprintf("must name between 1 and %d members", MaxRecipients), "Choose who is told"})
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range recipients {
		switch {
		case seen[r]:
			out = append(out, FieldProblem{"body.recipients", CodeRule, "names " + r.String() + " twice", "Name each member once"})
		case !slices.Contains(members, r.String()):
			out = append(out, FieldProblem{"body.recipients", CodeRule, r.String() + " is not an active member of this organisation", "Choose active members"})
		}
		seen[r] = true
	}
	return out
}
