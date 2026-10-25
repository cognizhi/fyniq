package banking_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// rule is a stored rule read back, with conditions c, at a position, active or not.
func rule(t testing.TB, name string, pos int, active bool, c banking.Conditions) banking.Rule {
	t.Helper()
	when, _ := json.Marshal(c)
	then, _ := json.Marshal(banking.Action{AccountID: uuid.New()})
	r, err := banking.ReadRule(storage.BankRule{ID: uuid.New(), Name: name, Position: pos, Conditions: when, Action: then, Active: active, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func bankLine(desc, party, amount, cur string, acct uuid.UUID) storage.BankLine {
	return storage.BankLine{ID: uuid.New(), MoneyAccountID: acct, Description: desc, Counterparty: party, Amount: storage.Decimal(amount), Currency: cur,
		Status: storage.LineUnreconciled}
}

// Each condition selects the lines that meet it and no others; texts compare without regard to case, spacing or
// Unicode form (NFKC), amounts by their size in the line's currency, and a rule needs every one of its conditions.
// req: FR-BNK-05
func TestEachRuleConditionSelectsItsLines(t *testing.T) {
	acct, other := uuid.New(), uuid.New()
	base := bankLine("GIRO  AWS EMEA SARL", "Amazon Web Services", "-120.50", "SGD", acct)
	with := func(f func(*storage.BankLine)) storage.BankLine { l := base; f(&l); return l }
	for name, c := range map[string]struct {
		when    banking.Conditions
		yes, no storage.BankLine
	}{
		"description contains, in any case": {banking.Conditions{DescriptionContains: "aws emea"}, base, with(func(l *storage.BankLine) { l.Description = "GIRO GCP" })},
		"description contains, full width":  {banking.Conditions{DescriptionContains: "ＡＷＳ"}, base, with(func(l *storage.BankLine) { l.Description = "GIRO GCP" })},
		"description regex":                 {banking.Conditions{DescriptionRegex: `^giro\s+aws`}, base, with(func(l *storage.BankLine) { l.Description = "FAST AWS" })},
		"money out":                         {banking.Conditions{Direction: banking.DirectionOut}, base, with(func(l *storage.BankLine) { l.Amount = "120.50" })},
		"money in":                          {banking.Conditions{Direction: banking.DirectionIn}, with(func(l *storage.BankLine) { l.Amount = "0.01" }), base},
		"at least an amount":                {banking.Conditions{AmountMin: "120.50"}, base, with(func(l *storage.BankLine) { l.Amount = "-120.49" })},
		"at most an amount":                 {banking.Conditions{AmountMax: "100"}, with(func(l *storage.BankLine) { l.Amount = "-100.00" }), base},
		"a currency":                        {banking.Conditions{Currency: "USD"}, with(func(l *storage.BankLine) { l.Currency = "USD" }), base},
		"a money account":                   {banking.Conditions{MoneyAccountID: &acct}, base, with(func(l *storage.BankLine) { l.MoneyAccountID = other })},
		"counterparty contains":             {banking.Conditions{CounterpartyContains: "amazon"}, base, with(func(l *storage.BankLine) { l.Counterparty = "Google Asia" })},
		"every condition": {banking.Conditions{DescriptionContains: "aws", Direction: banking.DirectionOut, AmountMin: "100", AmountMax: "200", Currency: "SGD",
			MoneyAccountID: &acct, CounterpartyContains: "amazon"}, base, with(func(l *storage.BankLine) { l.Amount = "-200.01" })},
	} {
		r := rule(t, name, 1, true, c.when)
		if !r.Matches(c.yes) || r.Matches(c.no) {
			t.Errorf("%s: matches %+v: %v; matches %+v: %v", name, c.yes, r.Matches(c.yes), c.no, r.Matches(c.no))
		}
	}
	if rule(t, "bad amount", 1, true, banking.Conditions{AmountMin: "1"}).Matches(with(func(l *storage.BankLine) { l.Amount = "NaN?" })) {
		t.Error("a line whose amount does not read matches nothing")
	}
}

// Rules apply in their order: the first active rule whose conditions a line meets codes it.
// req: FR-BNK-05
func TestRulesApplyInTheirOrder(t *testing.T) {
	acct := uuid.New()
	rules := []banking.Rule{
		rule(t, "AWS, switched off", 1, false, banking.Conditions{DescriptionContains: "aws"}),
		rule(t, "AWS", 2, true, banking.Conditions{DescriptionContains: "aws"}),
		rule(t, "Any GIRO", 3, true, banking.Conditions{DescriptionContains: "giro"}),
	}
	for _, c := range []struct {
		desc, want string
	}{{"GIRO AWS EMEA", "AWS"}, {"GIRO SP SERVICES", "Any GIRO"}, {"FAST TRANSFER", ""}} {
		r, ok := banking.First(rules, bankLine(c.desc, "", "-10.00", "SGD", acct))
		if got := map[bool]string{true: r.Name}[ok]; got != c.want {
			t.Errorf("%s: %q, want %q", c.desc, got, c.want)
		}
	}
	d := rules[1].Draft(bankLine("GIRO AWS", "", "-10.00", "SGD", acct))
	if len(d.Parts) != 1 || d.Parts[0].AccountID != rules[1].Then.AccountID || d.Parts[0].Amount != nil {
		t.Errorf("the rule's draft codes the whole line to its account: %+v", d)
	}
}

// A regular expression runs in time linear in the text (RE2), so no pattern runs away however it is written; one RE2
// cannot run (a back-reference), or longer than the limit, is refused, as are conditions that do not fit.
// req: FR-BNK-05
func TestARuleRegexCannotRunAway(t *testing.T) {
	text := bankLine(strings.Repeat("a", 500)+"!", "", "-1.00", "SGD", uuid.New())
	for _, p := range []string{`(a+)+$`, `(a|aa)+$`, `(.*a){20}$`, `^(a?){25}a{25}$`} {
		r := rule(t, p, 1, true, banking.Conditions{DescriptionRegex: p})
		done := make(chan bool, 1)
		go func() { done <- r.Matches(text) }()
		select {
		case got := <-done:
			if got {
				t.Errorf("%s matched a text that ends in !", p)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s ran away", p)
		}
	}
	for name, c := range map[string]banking.Conditions{
		"a back-reference":    {DescriptionRegex: `(a)\1`},
		"too long a pattern":  {DescriptionRegex: strings.Repeat("a", banking.MaxPattern+1)},
		"no condition":        {},
		"a negative amount":   {AmountMin: "-1"},
		"not an amount":       {AmountMax: "ten"},
		"min above max":       {AmountMin: "10", AmountMax: "5"},
		"a direction":         {Direction: "sideways"},
		"an unknown currency": {Currency: "ABC"},
		"too long a text":     {DescriptionContains: strings.Repeat("x", banking.MaxContains+1)},
	} {
		when, _ := json.Marshal(c)
		_, err := banking.ReadRule(storage.BankRule{Name: name, Conditions: when, Action: []byte(`{}`)})
		var e *banking.Error
		if !errors.As(err, &e) || e.Code != banking.CodeRule {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := banking.ReadRule(storage.BankRule{Name: "garbled", Conditions: []byte(`{`), Action: []byte(`{}`)}); err == nil {
		t.Error("conditions that do not read")
	}
	if _, err := banking.ReadRule(storage.BankRule{Name: "garbled", Conditions: []byte(`{"direction":"in"}`), Action: []byte(`[`)}); err == nil {
		t.Error("an action that does not read")
	}
}
