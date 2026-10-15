package differential

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/seed"
)

var update = flag.Bool("update", false, "rewrite the golden scenarios generated from the seed")

const golden = "../../test/differential/golden"

func load(t *testing.T, name string) Scenario {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(golden, name))
	if err != nil {
		t.Fatal(err)
	}
	var s Scenario
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

func run(t *testing.T, s Scenario) Result {
	t.Helper()
	r, err := RunEngine(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The seed's golden scenarios are generated, never edited by hand (DEV-04).
//
// req: DEV-04
func TestGoldenScenariosComeFromTheSeed(t *testing.T) {
	vocab, err := seed.LoadVocabulary("../../docs/design/screens/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct {
		seed     uint64
		journals int
	}{{1, 24}, {2, 120}} {
		name := fmt.Sprintf("sample-company-seed-%d", g.seed)
		b, err := json.MarshalIndent(FromSeed(seed.Generate(vocab, g.seed, g.journals), name), "", " ")
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, '\n')
		path := filepath.Join(golden, name+".json")
		if *update {
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if have, err := os.ReadFile(path); err != nil || string(have) != string(b) {
			t.Errorf("%s is out of date with the seed generator: run `go test ./internal/differential -update` (%v)", path, err)
		}
	}
}

// scenario-run's figures for the sample company equal the totals the database test computed from
// the seed in integer cents, without the engine.
//
// req: QA-ENG-02
func TestEngineMatchesTheIndependentSeedTotals(t *testing.T) {
	res := run(t, load(t, "sample-company-seed-1.json"))
	want := map[string]string{}
	b, err := os.ReadFile("../adapters/postgres/testdata/sample-company-trial-balance.golden")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) == 2 && !strings.HasPrefix(line, "#") {
			want[f[0]] = f[1]
		}
	}
	year := map[string]map[string]string{"2026": {}}
	totals := Result{TrialBalance: year}
	for _, accounts := range res.TrialBalance {
		for code, amount := range accounts {
			year["2026"][code] = add(t, year["2026"][code], amount)
		}
	}
	if err := Compare(totals, Result{TrialBalance: map[string]map[string]string{"2026": want}}); err != nil {
		t.Fatalf("the engine's yearly totals differ from the seed's: %v", err)
	}
	if len(res.Rejected) != 0 || len(res.Entries) != 25 {
		t.Fatalf("%d entries and rejected %v; want 24 postings and 1 reversal", len(res.Entries), res.Rejected)
	}
}

// add sums two decimal strings with two places, in integer cents.
func add(t *testing.T, a, b string) string {
	t.Helper()
	cents := func(s string) int64 {
		if s == "" {
			return 0
		}
		neg := strings.HasPrefix(s, "-")
		s = strings.TrimPrefix(s, "-")
		var whole, frac int64
		if _, err := fmt.Sscanf(strings.Replace(s, ".", " ", 1), "%d %d", &whole, &frac); err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		n := whole*100 + frac
		if neg {
			n = -n
		}
		return n
	}
	n := cents(a) + cents(b)
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	return fmt.Sprintf("%s%d.%02d", sign, n/100, n%100)
}

// The hand-built scenario's figures, worked out by hand: never by running the engine.
//
// req: QA-ENG-02
// req: RND-FX
func TestEngineOnTheHandWorkedScenario(t *testing.T) {
	res := run(t, load(t, "multi-currency.json"))
	want := Result{
		Rejected: []string{"JNL-0004", "JNL-0005", "JNL-0006", "JNL-0007", "JNL-0008", "JNL-0009", "JNL-0010",
			"JNL-0004/reversal", "JNL-0001/reversal"},
		Entries: []Entry{
			{Document: "JNL-0001", Kind: "post", PostingDate: "2026-01-05", Lines: []EntryLine{
				{"1020", "USD", "100.00", "134.72"}, {"4010", "SGD", "-134.72", "-134.72"}}},
			{Document: "JNL-0002", Kind: "post", PostingDate: "2026-01-20", Lines: []EntryLine{
				{"1020", "USD", "-0.05", "-0.13"}, {"7030", "SGD", "0.13", "0.13"}}}, // −0.125 → −0.13
			{Document: "JNL-0003", Kind: "post", PostingDate: "2026-02-02", Lines: []EntryLine{
				{"6110", "JPY", "10000", "91.00"}, {"6110", "KWD", "1.005", "4.45"}, {"1010", "SGD", "-95.45", "-95.45"}}},
			{Document: "JNL-0001", Kind: "reversal", PostingDate: "2026-02-01", Lines: []EntryLine{
				{"1020", "USD", "-100.00", "-134.72"}, {"4010", "SGD", "134.72", "134.72"}}},
		},
		TrialBalance: map[string]map[string]string{
			"2026-01": {"1020": "134.59", "4010": "-134.72", "7030": "0.13"},
			"2026-02": {"1010": "-95.45", "1020": "-134.72", "4010": "134.72", "6110": "95.45"},
		},
	}
	if err := Compare(res, want); err != nil {
		t.Fatal(err)
	}
}

// req: QA-ENG-02
func TestComparatorFindsAOneCentDifference(t *testing.T) {
	engine := run(t, load(t, "multi-currency.json"))
	same := run(t, load(t, "multi-currency.json"))
	if err := Compare(engine, same); err != nil {
		t.Fatalf("identical results differ: %v", err)
	}
	clone := func() Result {
		b, _ := json.Marshal(engine)
		var r Result
		_ = json.Unmarshal(b, &r)
		return r
	}
	for name, plant := range map[string]func(*Result){
		"a trial-balance cent":  func(r *Result) { r.TrialBalance["2026-01"]["7030"] = "0.14" },
		"a base amount":         func(r *Result) { r.Entries[1].Lines[0].BaseAmount = "-0.12" },
		"an account that moved": func(r *Result) { r.TrialBalance["2026-02"]["9999"] = "0.01" },
		"a rejection":           func(r *Result) { r.Rejected = r.Rejected[1:] },
		"an entry's date":       func(r *Result) { r.Entries[3].PostingDate = "2026-02-02" },
	} {
		oracle := clone()
		plant(&oracle)
		err := Compare(engine, oracle)
		if err == nil {
			t.Errorf("%s: a planted difference went unnoticed", name)
			continue
		}
		t.Logf("%s → %v", name, err)
	}
	// Formatting is not a difference: "0.10" equals "0.1", and a missing account equals zero.
	oracle := clone()
	oracle.TrialBalance["2026-01"]["7030"] = "0.130"
	oracle.TrialBalance["2026-01"]["8010"] = "0.00"
	if err := Compare(engine, oracle); err != nil {
		t.Errorf("equal amounts written differently: %v", err)
	}
}

// req: QA-ENG-02
func TestScenarioRoundTrip(t *testing.T) {
	s := load(t, "multi-currency.json")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Scenario
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(s, back) {
		t.Fatalf("round trip changed the scenario: %v", err)
	}
}
