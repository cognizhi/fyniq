package aibudget

import (
	"errors"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/ports"
)

// A budget names a scope Fyniq knows with its subject, and an amount above zero with at most four places, or for a
// token budget a whole number of tokens, or none to remove it (AI-77, AI-13).
// req: AI-77
// req: AI-13
func TestABudgetIsChecked(t *testing.T) {
	for _, ok := range [][3]string{{"organisation", "", "50"}, {"agent", "chat", "12.5"}, {"connection", "0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b", ""},
		{"person", "user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d", "0.0001"}, {"tokens_organisation", "", "5000000"},
		{"tokens_person", "user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d", "200000"}} {
		if err := Check(ok[0], ok[1], ok[2]); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	var e *Error
	for name, bad := range map[string][4]string{
		"a scope it lacks":         {"team", "", "5", "scope"},
		"the organisation's named": {"organisation", "x", "5", "subject"},
		"an agent's ID":            {"agent", "Chat Loop", "5", "subject"},
		"a connection's ID":        {"connection", "openai", "5", "subject"},
		"a person without user:":   {"person", "0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d", "5", "subject"},
		"zero":                     {"organisation", "", "0", "amount"},
		"five places":              {"organisation", "", "1.00001", "amount"},
		"a negative amount":        {"organisation", "", "-5", "amount"},
		"part of a token":          {"tokens_organisation", "", "1.5", "amount"},
		"no tokens":                {"tokens_person", "user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d", "0", "amount"},
		"an agent's tokens":        {"tokens_agent", "chat", "5", "scope"},
	} {
		if err := Check(bad[0], bad[1], bad[2]); !errors.As(err, &e) || e.Code != CodeBudget || e.Field != bad[3] {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := Spent("the agent's budget is spent"); !errors.As(err, &e) || e.Code != CodeSpent {
		t.Errorf("spent: %v", err)
	}
	if got := MostTokens(ports.ModelRequest{MaxTokens: 100, Messages: []ports.ModelMessage{{Parts: []ports.ModelPart{{Kind: ports.PartText,
		Text: "123456"}}}}}); got != 103 {
		t.Errorf("the most tokens: %d", got)
	}
	if got := UsedTokens(ports.ModelUsage{Input: 10, Output: 5, CacheRead: 100, CacheWrite: 20, Reasoning: 3}); got != 135 {
		t.Errorf("the tokens used: %d", got)
	}
}

// A call reserves the most it could use: its input estimated generously from its size and all its output (AI-86); the
// month is Singapore's; an amount is put in the base currency exactly.
// req: AI-86
func TestTheMostACallCouldCost(t *testing.T) {
	r := ports.ModelRequest{System: "abcdef", MaxTokens: 1000, Tools: []ports.ModelTool{{Name: "ab", Description: "c", Schema: []byte(`{}`)}},
		Messages: []ports.ModelMessage{{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "123456789"}, {Kind: ports.PartImage, Data: []byte("abc")}}}}}
	// 6 + 2 + 1 + 2 + 9 + 3 = 23 bytes: 7 tokens, one more, and an image.
	if u := MostUsage(r); u.Input != 8+imageTokens || u.Output != 1000 {
		t.Errorf("the most usage: %+v", u)
	}
	if u := MostUsage(ports.ModelRequest{}); u.Output != defaultOutputs || u.Input != 1 {
		t.Errorf("no limit: %+v", u)
	}
	if got := Month(time.Date(2026, 11, 30, 16, 30, 0, 0, time.UTC)); got != "2026-12" {
		t.Errorf("the month in Singapore: %s", got)
	}
	rate, _, _ := apd.NewFromString("1.3456789012")
	if got, err := InBase("0.0000000000000001", rate); err != nil || got != "0.00000000000000013456789012" {
		t.Errorf("an exact conversion: %q %v", got, err)
	}
	if _, err := InBase("ten", rate); err == nil {
		t.Error("not a decimal")
	}
}
