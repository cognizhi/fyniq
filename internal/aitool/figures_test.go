package aitool_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/aitool"
)

// Every figure a read returns is a fact (AI-68): a money object an amount in its currency, a whole number a count,
// decimal text a number; IDs, versions and line numbers are not figures. The fact's ID is stable for what it is and
// the revision, and differs for another revision or another place in the record.
// req: AI-68
func TestEveryFigureIsAFact(t *testing.T) {
	v, err := aitool.Decode([]byte(`{"id":"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b","number":"INV-0012","version":3,"total":{"amount":"1090.00","currency":"SGD"},
		"lines":[{"line":1,"quantity":"2.5","net":{"amount":"1000.00","currency":"SGD"}},{"line":2,"quantity":"1.0","net":{"amount":"0.00","currency":"SGD"}}],
		"count":4,"memo":"October fees","rate":"0.09"}`))
	if err != nil {
		t.Fatal(err)
	}
	s := aitool.Subject{Measure: "invoice", Kind: "invoice", Member: "0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b", Lineage: "/v1/invoices/x", Revision: 7, AsAt: "2026-10-15"}
	out, facts := aitool.Figures(v, s)
	byName := map[string]aitool.Fact{}
	for _, f := range facts {
		byName[f.Name] = f
	}
	for name, want := range map[string][3]string{
		"total": {"invoice.total", "1090.00", "amount"}, "lines[0].net": {"invoice.lines.net", "1000.00", "amount"},
		"lines[1].quantity": {"invoice.lines.quantity", "1.0", "number"}, "count": {"invoice.count", "4", "count"}, "rate": {"invoice.rate", "0.09", "number"},
	} {
		f, ok := byName[name]
		if !ok || f.Measure != want[0] || f.Value != want[1] || f.Unit != want[2] || f.Revision != 7 || f.Member != s.Member || !strings.HasPrefix(f.ID, "F") {
			t.Errorf("%s: %+v", name, f)
		}
	}
	if len(facts) != 7 {
		t.Errorf("%d facts: %+v", len(facts), facts)
	}
	b, _ := json.Marshal(out)
	for _, kept := range []string{`"version":3`, `"line":1`, `"number":"INV-0012"`, `"memo":"October fees"`} {
		if !strings.Contains(string(b), kept) {
			t.Errorf("%s is not a figure, but: %s", kept, b)
		}
	}
	if strings.Contains(string(b), "1090.00") || !strings.Contains(string(b), `"total":{"fact":"`+byName["total"].ID+`"}`) {
		t.Errorf("a figure stayed in the content: %s", b)
	}
	_, again := aitool.Figures(v, s)
	s.Revision = 8
	_, later := aitool.Figures(v, s)
	if again[0].ID != facts[0].ID || later[0].ID == facts[0].ID || byName["lines[0].net"].ID == byName["lines[1].net"].ID {
		t.Error("a fact's ID is not stable for what it is and its revision")
	}
}

// A result keeps within its tool's limits (AI-04, design D6): at most its rows of each list, and within its token
// budget, shortening the lists further, with what was left out counted; the facts are of what it shows.
// req: AI-04
func TestAResultKeepsWithinItsLimits(t *testing.T) {
	var lines []any
	for range 120 {
		lines = append(lines, map[string]any{"net": map[string]any{"amount": "10.00", "currency": "SGD"}, "description": strings.Repeat("x", 40)})
	}
	v := map[string]any{"lines": lines}
	content, facts, left := aitool.Fit(v, aitool.Limits{Rows: 50, TimeoutMS: 5000, ResultTokens: 100000}, aitool.Subject{Measure: "bill"})
	if got := content.(map[string]any)["lines"].([]any); len(got) != 50 || len(facts) != 50 || len(left) != 1 || left[0] != (aitool.Omission{Path: "lines", Shown: 50, Of: 120}) {
		t.Errorf("50 rows: %d shown, %d facts, %+v", len(got), len(facts), left)
	}
	content, facts, left = aitool.Fit(v, aitool.Limits{Rows: 50, TimeoutMS: 5000, ResultTokens: 2000}, aitool.Subject{Measure: "bill"})
	got := content.(map[string]any)["lines"].([]any)
	if len(got) >= 50 || len(got) == 0 || len(facts) != len(got) || left[0].Shown != len(got) || left[0].Of != 120 ||
		aitool.Tokens(content)+aitool.Tokens(facts) > 2000 {
		t.Errorf("a budget of 2,000 tokens: %d shown, %d facts, %+v", len(got), len(facts), left)
	}
	if content, facts, left := aitool.Fit(map[string]any{"memo": strings.Repeat("y", 10000)}, aitool.Limits{Rows: 50, ResultTokens: 100},
		aitool.Subject{}); content != nil || facts != nil || len(left) != 1 {
		t.Errorf("a result that never fits: %v %v %v", content, facts, left)
	}
}
