package differential

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mutated is golden/bank.json with change made to it, as JSON.
func mutated(t *testing.T, change func(s map[string]any)) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(golden, "bank.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	change(s)
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A scenario that breaks a rule of the P1 sources, the statements or the KPIs is invalid, as the oracle finds it
// (test/differential/README.md, "Invalid scenario or rejected document").
// req: QA-ENG-02
func TestP1FormatRules(t *testing.T) {
	if _, err := ParseScenario(mutated(t, func(map[string]any) {})); err != nil {
		t.Fatalf("golden/bank.json: %v", err)
	}
	settings := func(s map[string]any) map[string]any { return s["settings"].(map[string]any) }
	for name, change := range map[string]func(s map[string]any){
		"no as_of":                func(s map[string]any) { delete(s, "as_of") },
		"no settings":             func(s map[string]any) { delete(s, "settings") },
		"as_of before a document": func(s map[string]any) { s["as_of"] = "2026-04-06" },
		"a reversal of a bank line": func(s map[string]any) {
			s["reversals"] = []any{map[string]any{"document": "BT-0001", "date": "2026-03-06"}}
		},
		"a settings account not in accounts": func(s map[string]any) { settings(s)["accounts"].(map[string]any)["fx"] = "7999" },
		"a money account not in accounts":    func(s map[string]any) { settings(s)["money_accounts"].(map[string]any)["1099"] = "SGD" },
		"a money account's undeclared currency": func(s map[string]any) {
			settings(s)["money_accounts"].(map[string]any)["1021"] = "EUR"
		},
		"a books start that is not a date":     func(s map[string]any) { settings(s)["books_start"] = "2026-02-30" },
		"statements with no retained earnings": func(s map[string]any) { s["statements"] = true },
		"kpis with no statements": func(s map[string]any) {
			s["kpis"] = map[string]any{"year_start_month": 1, "counts": map[string]any{}}
		},
		"two retained-earnings accounts": func(s map[string]any) {
			accounts := s["accounts"].([]any)
			for _, i := range []int{8, 9} { // 2400 and 2999, made equity
				a := accounts[i].(map[string]any)
				a["type"], a["role"] = "equity", "retained-earnings"
			}
		},
		"retained earnings on a liability": func(s map[string]any) { s["accounts"].([]any)[9].(map[string]any)["role"] = "retained-earnings" },
	} {
		var invalid *InvalidError
		if _, err := ParseScenario(mutated(t, change)); !errors.As(err, &invalid) {
			t.Errorf("%s: want an invalid scenario, got %v", name, err)
		}
	}
}

// The bank lines of golden/bank.json, worked out by hand: BT-0001 pays 109.00 coded TX at 9% while registered, so
// 2200 takes 109.00 × 9 ÷ 109 = 9.00 of input tax and 6010 the 100.00 rest; BT-0002 receives 327.00 coded SR and
// spread over 3 months, so 2200 takes 27.00 of output tax and 2400 the 300.00 rest, released 100.00 a month: on
// its own date in March (the month starts before it), on 1 April, and not on 1 May, after as_of (30 April). The
// releases come after every document, in canonical form; the money account's line, a part on a money account and a
// spread on an asset (BT-0005, BT-0006) are refused.
// req: FR-BNK-06
// req: FR-GL-03
// req: ENG-05
// req: QA-ENG-02
func TestP1EntriesAndReleases(t *testing.T) {
	r := run(t, load(t, "bank.json"))
	if want := []string{"BT-0005", "BT-0006"}; !reflect.DeepEqual(r.Rejected, want) {
		t.Errorf("rejected %v, want %v", r.Rejected, want)
	}
	var order []string
	for _, e := range r.Entries {
		order = append(order, e.Kind+" "+e.Document+" "+e.PostingDate)
	}
	want := []string{"post BT-0001 2026-03-05", "post BT-0002 2026-03-10", "post BT-0003 2026-03-12", "post BT-0004 2026-03-20",
		"post TR-0001 2026-04-02", "post TR-0002 2026-04-03", "post BT-0007 2026-04-07",
		"release BT-0002 2026-03-10", "release BT-0002 2026-04-01"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("entries %v\nwant %v", order, want)
	}
	for i, lines := range map[int][]EntryLine{
		0: {el("1010", "SGD", "-109.00", "-109.00"), el("2200", "SGD", "9.00", "9.00"), el("6010", "SGD", "100.00", "100.00")},
		1: {el("1010", "SGD", "327.00", "327.00"), el("2200", "SGD", "-27.00", "-27.00"), el("2400", "SGD", "-300.00", "-300.00")},
		7: {el("2400", "SGD", "100.00", "100.00"), el("4010", "SGD", "-100.00", "-100.00")},
		8: {el("2400", "SGD", "100.00", "100.00"), el("4010", "SGD", "-100.00", "-100.00")},
	} {
		if got := r.Entries[i].Lines; !reflect.DeepEqual(got, lines) {
			t.Errorf("%s: %v, want %v", order[i], got, lines)
		}
	}
}
