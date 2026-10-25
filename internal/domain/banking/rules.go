package banking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Bank rules (FR-BNK-05; p1-bank-import-reconcile task 4.2): a rule's conditions select lines, and its action codes
// them: the category, the contact, the GST code and the reference. Rules apply in their order, the first active rule
// whose every condition holds; a person accepts what it suggests unless an approver made it auto-apply (FR-BNK-14,
// task 5.2). A back-test runs a rule's conditions over the lines held and says how many it would have matched.
// Dimensions join the action with FR-DIM, as they join bill and invoice lines.

// The limits of a rule's text conditions.
const (
	MaxPattern   = 200 // a regular expression's length; RE2 runs in time linear in the text, so no pattern runs away
	MaxContains  = 100 // a "contains" condition's length
	MaxRuleName  = 100
	MaxReference = banktransaction.MaxReference
)

// The directions a rule may require.
const (
	DirectionIn  = "in"
	DirectionOut = "out"
)

// Conditions are what a line must meet, all of them: its description containing a text or matching a regular
// expression, its direction and its amount within a range (of its size, in its currency), its currency, its money
// account, and its counterparty containing a text. Texts compare without regard to case or Unicode form (NFKC).
type Conditions struct {
	DescriptionContains  string     `json:"description_contains,omitempty"`
	DescriptionRegex     string     `json:"description_regex,omitempty"`
	Direction            string     `json:"direction,omitempty"`
	AmountMin            string     `json:"amount_min,omitempty"`
	AmountMax            string     `json:"amount_max,omitempty"`
	Currency             string     `json:"currency,omitempty"`
	MoneyAccountID       *uuid.UUID `json:"money_account_id,omitempty"`
	CounterpartyContains string     `json:"counterparty_contains,omitempty"`
}

// Action is how a rule codes the lines it matches: the account, the contact, the GST code ("" for the account's
// default) and the reference.
type Action struct {
	AccountID uuid.UUID  `json:"account_id"`
	ContactID *uuid.UUID `json:"contact_id,omitempty"`
	GSTCode   string     `json:"gst_code,omitempty"`
	Reference string     `json:"reference,omitempty"`
}

// Rule is a stored rule as the domain reads it.
type Rule struct {
	storage.BankRule
	When Conditions
	Then Action
	m    *matcher
}

// matcher is a rule's conditions ready to test lines.
type matcher struct {
	contains, party string
	re              *regexp.Regexp
	dir             string
	min, max        *apd.Decimal
	cur             string
	account         *uuid.UUID
}

// fold is a text as rules compare it: NFKC, lower case, spaces collapsed.
func fold(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(s))), " ")
}

// compile checks c and readies it, with every problem at once.
func (c Conditions) compile() (*matcher, []string) {
	var probs []string
	m := &matcher{contains: fold(c.DescriptionContains), party: fold(c.CounterpartyContains), dir: c.Direction, cur: c.Currency, account: c.MoneyAccountID}
	if c == (Conditions{}) {
		probs = append(probs, "a rule needs at least one condition")
	}
	for name, s := range map[string]string{"description_contains": c.DescriptionContains, "counterparty_contains": c.CounterpartyContains} {
		if utf8.RuneCountInString(s) > MaxContains {
			probs = append(probs, fmt.Sprintf("%s has %d characters; the most is %d", name, utf8.RuneCountInString(s), MaxContains))
		}
	}
	if c.DescriptionRegex != "" {
		if utf8.RuneCountInString(c.DescriptionRegex) > MaxPattern {
			probs = append(probs, fmt.Sprintf("description_regex has %d characters; the most is %d", utf8.RuneCountInString(c.DescriptionRegex), MaxPattern))
		} else if re, err := regexp.Compile("(?i)" + c.DescriptionRegex); err != nil {
			probs = append(probs, fmt.Sprintf("description_regex is not a regular expression Fyniq runs: %v", err))
		} else {
			m.re = re
		}
	}
	switch c.Direction {
	case "", DirectionIn, DirectionOut:
	default:
		probs = append(probs, fmt.Sprintf("direction %q is not in or out", c.Direction))
	}
	for _, x := range []struct {
		name, s string
		to      **apd.Decimal
	}{{"amount_min", c.AmountMin, &m.min}, {"amount_max", c.AmountMax, &m.max}} {
		if x.s == "" {
			continue
		}
		d, _, err := apd.NewFromString(x.s)
		if err != nil || d.Negative || d.Form != apd.Finite {
			probs = append(probs, fmt.Sprintf("%s %q is not an amount of zero or more, such as 25.00", x.name, x.s))
			continue
		}
		*x.to = d
	}
	if m.min != nil && m.max != nil && m.min.Cmp(m.max) > 0 {
		probs = append(probs, fmt.Sprintf("amount_min %s is more than amount_max %s", c.AmountMin, c.AmountMax))
	}
	if c.Currency != "" {
		if _, err := money.Lookup(c.Currency); err != nil {
			probs = append(probs, fmt.Sprintf("currency %q is not a currency Fyniq knows", c.Currency))
		}
	}
	return m, probs
}

// Matches says whether a line meets every condition.
func (m *matcher) Matches(l storage.BankLine) bool {
	if m.account != nil && *m.account != l.MoneyAccountID {
		return false
	}
	if m.cur != "" && m.cur != l.Currency {
		return false
	}
	amount, _, err := apd.NewFromString(string(l.Amount))
	if err != nil {
		return false
	}
	switch {
	case m.dir == DirectionIn && amount.Sign() <= 0, m.dir == DirectionOut && amount.Sign() >= 0:
		return false
	}
	size := new(apd.Decimal).Abs(amount)
	if (m.min != nil && size.Cmp(m.min) < 0) || (m.max != nil && size.Cmp(m.max) > 0) {
		return false
	}
	if m.contains != "" && !strings.Contains(fold(l.Description), m.contains) {
		return false
	}
	if m.party != "" && !strings.Contains(fold(l.Counterparty), m.party) {
		return false
	}
	if m.re != nil && !m.re.MatchString(norm.NFKC.String(l.Description)) {
		return false
	}
	return true
}

// ReadRule reads a stored rule; CodeRule when its conditions or action no longer read.
func ReadRule(r storage.BankRule) (Rule, error) {
	out := Rule{BankRule: r}
	if err := json.Unmarshal(r.Conditions, &out.When); err != nil {
		return Rule{}, refuse(CodeRule, "Edit the rule and save its conditions again.", "rule %q: its conditions cannot be read: %v", r.Name, err)
	}
	if err := json.Unmarshal(r.Action, &out.Then); err != nil {
		return Rule{}, refuse(CodeRule, "Edit the rule and save its action again.", "rule %q: its action cannot be read: %v", r.Name, err)
	}
	m, probs := out.When.compile()
	if len(probs) > 0 {
		return Rule{}, refuse(CodeRule, "Edit the rule's conditions.", "rule %q: %s", r.Name, strings.Join(probs, "; "))
	}
	out.m = m
	return out, nil
}

// Matches says whether the rule's conditions hold for a line.
func (r Rule) Matches(l storage.BankLine) bool { return r.m != nil && r.m.Matches(l) }

// Draft is the categorisation the rule suggests for a line: the whole line to its account.
func (r Rule) Draft(l storage.BankLine) banktransaction.Draft {
	return banktransaction.Draft{LineID: l.ID, ContactID: r.Then.ContactID, Reference: r.Then.Reference,
		Parts: []banktransaction.Part{{AccountID: r.Then.AccountID, GSTCode: r.Then.GSTCode}}}
}

// Rules are the organisation's rules that read, in their order. A rule that no longer reads is left out with its
// refusal, so one bad rule never stops the others.
func Rules(ctx context.Context, u storage.UnitOfWork) ([]Rule, []error, error) {
	stored, err := u.BankMatching().Rules(ctx)
	if err != nil {
		return nil, nil, err
	}
	var out []Rule
	var bad []error
	for _, s := range stored {
		r, err := ReadRule(s)
		if err != nil {
			bad = append(bad, err)
			continue
		}
		out = append(out, r)
	}
	return out, bad, nil
}

// First is the first active rule, in their order, whose conditions a line meets.
func First(rules []Rule, l storage.BankLine) (Rule, bool) {
	for _, r := range rules {
		if r.Active && r.Matches(l) {
			return r, true
		}
	}
	return Rule{}, false
}

// RuleRequest is a rule as a person writes it.
type RuleRequest struct {
	Name     string
	Position int
	When     Conditions
	Then     Action
	Active   bool
	At       time.Time
}

// checkRule refuses a rule whose name, conditions or action do not fit, with every problem at once: an account that
// is not an active posting account, or one a line reaches by another path (a money account, receivables, payables);
// a GST code the sg-gst pack does not have; a contact that does not exist; a reference too long.
func checkRule(ctx context.Context, u storage.UnitOfWork, r RuleRequest) error {
	var probs []string
	if n := utf8.RuneCountInString(strings.TrimSpace(r.Name)); n == 0 || n > MaxRuleName {
		probs = append(probs, fmt.Sprintf("the name has %d characters; it needs 1 to %d", n, MaxRuleName))
	}
	if r.Position < 1 {
		probs = append(probs, fmt.Sprintf("the position %d is not 1 or more", r.Position))
	}
	_, cond := r.When.compile()
	probs = append(probs, cond...)
	a, err := u.Accounts().Get(ctx, r.Then.AccountID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		probs = append(probs, "the action's account does not exist in this organisation")
	case err != nil:
		return err
	case a.Group || !a.Active:
		probs = append(probs, fmt.Sprintf("account %s is not an active account lines post to", a.Code))
	case a.MoneyAccount:
		probs = append(probs, fmt.Sprintf("account %s is a money account: a transfer, not a rule, moves money between accounts", a.Code))
	case string(a.Role) == ledger.RoleAccountsReceivable || string(a.Role) == ledger.RoleAccountsPayable:
		probs = append(probs, fmt.Sprintf("account %s holds what customers or suppliers owe: lines are matched to their documents instead", a.Code))
	}
	if r.Then.GSTCode != "" {
		v, err := gst.Version(ctx, u, time.Time{})
		if err != nil {
			return err
		}
		if _, ok := pack.Code(v, r.Then.GSTCode); !ok {
			probs = append(probs, fmt.Sprintf("%q is not a GST code of %s", r.Then.GSTCode, v.ID))
		}
	}
	if r.Then.ContactID != nil {
		if _, err := u.Contacts().Get(ctx, *r.Then.ContactID); errors.Is(err, ports.ErrNotFound) {
			probs = append(probs, "the action's contact does not exist in this organisation")
		} else if err != nil {
			return err
		}
	}
	if len(r.Then.Reference) > MaxReference {
		probs = append(probs, fmt.Sprintf("the reference has %d characters; the most is %d", len(r.Then.Reference), MaxReference))
	}
	if len(probs) > 0 {
		return refuse(CodeRule, "Change the rule as each problem says.", "%s", strings.Join(probs, "; "))
	}
	return nil
}

func (r RuleRequest) stored() (storage.NewBankRule, error) {
	when, err := json.Marshal(r.When)
	if err != nil {
		return storage.NewBankRule{}, err
	}
	then, err := json.Marshal(r.Then)
	if err != nil {
		return storage.NewBankRule{}, err
	}
	return storage.NewBankRule{Name: strings.TrimSpace(r.Name), Position: r.Position, Conditions: when, Action: then, Active: r.Active, At: r.At}, nil
}

// CreateRule saves a new rule, in u, after checking it. A new rule never auto-applies: an approver decides that with
// its back-test (FR-BNK-14).
func CreateRule(ctx context.Context, u storage.UnitOfWork, r RuleRequest) (Rule, error) {
	if err := checkRule(ctx, u, r); err != nil {
		return Rule{}, err
	}
	n, err := r.stored()
	if err != nil {
		return Rule{}, err
	}
	s, err := u.BankMatching().CreateRule(ctx, n)
	if err != nil {
		return Rule{}, err
	}
	return ReadRule(s)
}

// ChangeRule changes a rule at version, in u, after checking it: storage.CodeVersion when it changed since.
func ChangeRule(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, version int, r RuleRequest) (Rule, error) {
	if err := checkRule(ctx, u, r); err != nil {
		return Rule{}, err
	}
	n, err := r.stored()
	if err != nil {
		return Rule{}, err
	}
	s, err := u.BankMatching().ChangeRule(ctx, id, version, n)
	if err != nil {
		return Rule{}, err
	}
	return ReadRule(s)
}

// BackTest is what a rule's conditions would have matched among the lines held: how many of how many, by status, and
// the first few, latest first.
type BackTest struct {
	Examined int
	Matched  int
	ByStatus map[string]int
	Sample   []storage.BankLine
}

// MaxSample is how many matched lines a back-test shows.
const MaxSample = 20

// RunBackTest runs conditions over every line held, in u (FR-BNK-05: "this rule would have matched 23 lines"). It
// writes nothing; conditions that do not fit are refused with every problem.
func RunBackTest(ctx context.Context, u storage.UnitOfWork, c Conditions) (BackTest, error) {
	m, probs := c.compile()
	if len(probs) > 0 {
		return BackTest{}, refuse(CodeRule, "Change the conditions as each problem says.", "%s", strings.Join(probs, "; "))
	}
	lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: c.MoneyAccountID})
	if err != nil {
		return BackTest{}, err
	}
	out := BackTest{Examined: len(lines), ByStatus: map[string]int{}}
	for i := len(lines) - 1; i >= 0; i-- { // latest first
		l := lines[i]
		if !m.Matches(l) {
			continue
		}
		out.Matched++
		out.ByStatus[l.Status]++
		if len(out.Sample) < MaxSample {
			out.Sample = append(out.Sample, l)
		}
	}
	return out, nil
}
