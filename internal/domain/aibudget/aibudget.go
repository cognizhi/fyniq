// Package aibudget keeps AI spending within the Owner's budgets (PRD-001 AI-77, AI-86, AI-13; p1-ai-providers task
// 5.3, design D8): monthly budgets in the base currency for the organisation, an agent, a connection or a person;
// before an attempt is sent, its most it could cost is reserved against every budget that applies, and after it the
// reservation settles to what it cost, or stays whole when the outcome is unknown. Probes, evals and retries reserve
// like any call. At 80% of a budget its alert is raised once a month; at 100% nothing it cannot cover is admitted, while
// accounting, checks and deadlines keep working.
package aibudget

import (
	"fmt"
	"regexp"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The codes of a refused budget step (ENG-23).
const (
	CodeBudget = "AI-BUDGET-01" // not a valid budget: an unknown scope or subject, or an amount that is not a decimal above zero
	CodeSpent  = "AI-BUDGET-02" // a budget is spent this month, or cannot apply: the call is not sent
)

// Error is a refused budget step, in Fyniq's words (ENG-23).
type Error struct {
	Code, Field, Message, Fix string
}

func (e *Error) Error() string { return e.Code + ": " + e.Field + " " + e.Message }

func refuse(code, field, message, fix string) *Error {
	return &Error{Code: code, Field: field, Message: message, Fix: fix}
}

// Spent is a budget that stopped a call.
func Spent(message string) error {
	return refuse(CodeSpent, "budget", message, "Wait for next month, or ask the Owner to raise the budget; accounting, checks and deadlines keep working")
}

// Scopes are the scopes of a budget.
var Scopes = []string{"organisation", "agent", "connection", "person"}

// AlertAt is the share of a budget, in percent, at which its alert is raised.
const AlertAt = 80

var (
	agentID = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,59}$`)
	amount  = regexp.MustCompile(`^[0-9]{1,14}(\.[0-9]{1,4})?$`)
)

// Check checks a budget: a scope Fyniq knows with its subject (none for the organisation, an agent's ID, a connection's
// ID, or a person as user:<id>), and an amount in the base currency above zero with at most four places, or none to
// remove it.
func Check(scope, subject, amountText string) error {
	switch scope {
	case "organisation":
		if subject != "" {
			return refuse(CodeBudget, "subject", "is not empty for the organisation's budget", "Leave it out")
		}
	case "agent":
		if !agentID.MatchString(subject) {
			return refuse(CodeBudget, "subject", "is not an agent's ID", "Name an agent GET /v1/ai/routes lists")
		}
	case "connection":
		if _, err := uuid.Parse(subject); err != nil {
			return refuse(CodeBudget, "subject", "is not a connection's ID", "Name a connection GET /v1/ai/connections lists")
		}
	case "person":
		if len(subject) < 6 || subject[:5] != "user:" {
			return refuse(CodeBudget, "subject", "is not a person as user:<id>", "Name a member as user: and their user ID")
		}
		if _, err := uuid.Parse(subject[5:]); err != nil {
			return refuse(CodeBudget, "subject", "is not a person as user:<id>", "Name a member as user: and their user ID")
		}
	default:
		return refuse(CodeBudget, "scope", "is not organisation, agent, connection or person", "Choose one of them")
	}
	if amountText != "" {
		d, _, err := apd.NewFromString(amountText)
		if !amount.MatchString(amountText) || err != nil || d.Sign() <= 0 {
			return refuse(CodeBudget, "amount", "is not an amount above zero with at most four decimal places", "Give the monthly amount, such as 50.00")
		}
	}
	return nil
}

// singapore is the calendar a budget's month follows.
var singapore = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		return time.FixedZone("SGT", 8*60*60)
	}
	return loc
}()

// Month is the calendar month in Singapore a moment counts against, such as 2026-11.
func Month(t time.Time) string { return t.In(singapore).Format("2006-01") }

// The estimates of a request's input the reservation takes: a token for every three bytes of text, schema or document
// (a generous count for English and JSON), and a fixed allowance per image [verify: each provider's image tokens].
const (
	bytesPerToken  = 3
	imageTokens    = 1600
	defaultOutputs = 8192
)

// MostUsage is the most a request could use: its input estimated generously from its size, and all the output its
// limit allows (AI-86).
func MostUsage(r ports.ModelRequest) ports.ModelUsage {
	size := len(r.System)
	for _, t := range r.Tools {
		size += len(t.Name) + len(t.Description) + len(t.Schema)
	}
	if r.Output != nil {
		size += len(r.Output.Schema)
	}
	var images int64
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			size += len(p.Text) + len(p.Data) + len(p.Input)
			if p.Kind == ports.PartImage {
				images++
			}
		}
	}
	output := int64(r.MaxTokens)
	if output <= 0 {
		output = defaultOutputs
	}
	return ports.ModelUsage{Input: int64(size)/bytesPerToken + 1 + images*imageTokens, Output: output}
}

// calc multiplies exactly: a cost of up to 16 places at a rate of up to 10.
var calc = apd.BaseContext.WithPrecision(80)

// InBase is an amount in the price's currency at a rate into the base currency, exactly, as decimal text.
func InBase(amount string, rate *apd.Decimal) (string, error) {
	d, _, err := apd.NewFromString(amount)
	if err != nil {
		return "", fmt.Errorf("the amount %q is not a decimal", amount)
	}
	var out apd.Decimal
	if _, err := calc.Mul(&out, d, rate); err != nil {
		return "", err
	}
	if _, _, err := calc.Reduce(&out, &out); err != nil {
		return "", err
	}
	return out.Text('f'), nil
}
