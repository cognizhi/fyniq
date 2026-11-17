//go:build local

package differential

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cockroachdb/apd/v3"
)

// workbookGolden is a set's expected.json (fyniq-workbook-golden@1): the documents both sides refuse, and the figures
// the workbook's own tests pin, each naming its test. A check reads one of:
//   - month and account: the account's movement in the month (the trial balance; a missing one is zero);
//   - months [from, to] and account: its movements from the one month to the other, summed;
//   - documents and account: the sum of those documents' lines on the account in their entries of kind (post by
//     default) dated on or before on_or_before when given, in base (base) or in the line's currency (amount); with
//     each, every such entry's sum on its own.
//
// A tolerance, when given, is the workbook test's own (its float figures; Fyniq's are in cents).
type workbookGolden struct {
	Schema   string          `json:"schema"`
	Set      string          `json:"set"`
	Scenario string          `json:"scenario"`
	Rejected []string        `json:"rejected"`
	Checks   []workbookCheck `json:"checks"`
}

type workbookCheck struct {
	Test       string   `json:"test"`
	What       string   `json:"what"`
	Month      string   `json:"month"`
	Months     []string `json:"months"`
	Documents  []string `json:"documents"`
	Kind       string   `json:"kind"`
	Account    string   `json:"account"`
	Base       string   `json:"base"`
	Amount     string   `json:"amount"`
	Each       string   `json:"each"`
	OnOrBefore string   `json:"on_or_before"`
	Tolerance  string   `json:"tolerance"`
}

var exact = apd.BaseContext.WithPrecision(50)

func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	if s == "" {
		s = "0"
	}
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return d
}

// within says whether got is want, give or take tolerance.
func within(t *testing.T, got, want *apd.Decimal, tolerance string) bool {
	diff := new(apd.Decimal)
	if _, err := exact.Sub(diff, got, want); err != nil {
		t.Fatal(err)
	}
	diff.Abs(diff)
	return diff.Cmp(dec(t, tolerance)) <= 0
}

// figures works out what a check reads from a result: one figure, or for each, one per entry.
func figures(t *testing.T, r Result, c workbookCheck) []*apd.Decimal {
	t.Helper()
	sum := func(ds ...*apd.Decimal) *apd.Decimal {
		out := apd.New(0, 0)
		for _, d := range ds {
			if _, err := exact.Add(out, out, d); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	switch {
	case c.Month != "":
		return []*apd.Decimal{dec(t, r.TrialBalance[c.Month][c.Account])}
	case len(c.Months) == 2:
		var ds []*apd.Decimal
		for m, by := range r.TrialBalance {
			if m >= c.Months[0] && m <= c.Months[1] {
				ds = append(ds, dec(t, by[c.Account]))
			}
		}
		return []*apd.Decimal{sum(ds...)}
	case len(c.Documents) > 0:
		kind := c.Kind
		if kind == "" {
			kind = "post"
		}
		var each []*apd.Decimal
		for _, e := range r.Entries {
			if !slices.Contains(c.Documents, e.Document) || e.Kind != kind || (c.OnOrBefore != "" && e.PostingDate > c.OnOrBefore) {
				continue
			}
			var ds []*apd.Decimal
			for _, l := range e.Lines {
				if l.Account != c.Account {
					continue
				}
				if c.Amount != "" {
					ds = append(ds, dec(t, l.Amount))
				} else {
					ds = append(ds, dec(t, l.BaseAmount))
				}
			}
			each = append(each, sum(ds...))
		}
		if c.Each != "" {
			return each
		}
		return []*apd.Decimal{sum(each...)}
	}
	t.Fatalf("%s: a check names a month, months or documents", c.What)
	return nil
}

// check says what in r differs from the set's expected results.
func (g workbookGolden) check(t *testing.T, r Result) []string {
	t.Helper()
	var bad []string
	if !slices.Equal(r.Rejected, g.Rejected) {
		bad = append(bad, fmt.Sprintf("refused %v; the workbook's ✖ rows are %v", r.Rejected, g.Rejected))
	}
	for _, c := range g.Checks {
		want := dec(t, c.Base+c.Amount+c.Each)
		got := figures(t, r, c)
		if len(got) == 0 {
			bad = append(bad, fmt.Sprintf("%s (%s): nothing posted", c.What, c.Test))
		}
		for _, f := range got {
			if !within(t, f, want, c.Tolerance) {
				bad = append(bad, fmt.Sprintf("%s (%s): %s; the workbook's is %s", c.What, c.Test, f, want))
			}
		}
	}
	return bad
}

// The workbook's golden data sets (QA-ENG-07, p1-launch-readiness task 3.1, design D4). Each set in
// test/golden/workbook/, ported from the workbook's own tests at the commit sources.json records
// (scripts/workbook_golden.py), posts through the engine and the independent oracle: the two agree to the cent, both
// refuse exactly the documents of the workbook's ✖ rows the format can hold, and both reproduce every figure the
// workbook's tests pin, from its documents' totals to each month's FX, spreads, depreciation and GST. The posting edge
// cases (tests/edge_posting.py) give the pinned figures of Appendix G.3: the document totals, 7030's June and August,
// and each asset's depreciation. mapping.json says where each of its rows went, or why Fyniq posts it otherwise.
// req: quality/calculation-correctness Golden data set, posting edge cases
// req: quality/calculation-correctness Pinned document totals
// req: quality/calculation-correctness Pinned FX and rounding on account 7030
// req: quality/calculation-correctness Pinned asset depreciation
func TestTheWorkbookGoldenSets(t *testing.T) {
	sets, _ := filepath.Glob(filepath.Join(repo, "test/golden/workbook/*/expected.json"))
	if len(sets) == 0 {
		t.Fatal("no workbook set in test/golden/workbook")
	}
	for _, path := range sets {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var g workbookGolden
		if err := json.Unmarshal(raw, &g); err != nil || g.Schema != "fyniq-workbook-golden@1" || len(g.Checks) == 0 {
			t.Fatalf("%s: %v (schema %q, %d checks)", path, err, g.Schema, len(g.Checks))
		}
		t.Run(g.Set, func(t *testing.T) {
			file, _ := filepath.Abs(filepath.Join(filepath.Dir(path), g.Scenario))
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			s, err := ParseScenario(data)
			if err != nil {
				t.Fatalf("the scenario: %v", err)
			}
			engine, err := RunEngine(s)
			if err != nil {
				t.Fatalf("the engine: %v", err)
			}
			var oracle Result
			sc := bufio.NewScanner(bytes.NewReader(uv(t, "python", "test/differential/oracle_batch.py", file)))
			sc.Buffer(make([]byte, 1<<20), 1<<26)
			for sc.Scan() {
				var line struct {
					Result  json.RawMessage `json:"result"`
					Invalid string          `json:"invalid"`
				}
				if err := json.Unmarshal(sc.Bytes(), &line); err != nil || line.Invalid != "" {
					t.Fatalf("the oracle: %v %s", err, line.Invalid)
				}
				if err := json.Unmarshal(line.Result, &oracle); err != nil {
					t.Fatal(err)
				}
			}
			if err := Compare(engine, oracle); err != nil {
				t.Errorf("the engine and the oracle disagree: %v", err)
			}
			for side, r := range map[string]Result{"engine": engine, "oracle": oracle} {
				for _, b := range g.check(t, r) {
					t.Errorf("%s: %s", side, b)
				}
			}
			t.Logf("%d figures of the workbook's tests, %d documents refused", len(g.Checks), len(g.Rejected))
		})
	}
}

// A check fails on a figure one cent off, on a refusal the workbook does not have, and on a figure nothing posted.
func TestAWorkbookCheckFindsACentOff(t *testing.T) {
	r := Result{
		Rejected:     []string{"J-E08"},
		TrialBalance: map[string]map[string]string{"2026-06": {"7030": "-119.40"}},
		Entries: []Entry{
			{Document: "INV-E01", Kind: "post", PostingDate: "2026-03-10", Lines: []EntryLine{{Account: "1100", Currency: "SGD", Amount: "10900.00", BaseAmount: "10900.00"}}},
			{Document: "FA-01", Kind: "depreciation", PostingDate: "2026-03-31", Lines: []EntryLine{{Account: "6610", Currency: "SGD", Amount: "100.00", BaseAmount: "100.00"}}},
			{Document: "FA-01", Kind: "depreciation", PostingDate: "2026-04-30", Lines: []EntryLine{{Account: "6610", Currency: "SGD", Amount: "99.99", BaseAmount: "99.99"}}},
		},
	}
	g := workbookGolden{Rejected: []string{"J-E08"}, Checks: []workbookCheck{
		{What: "7030 June", Month: "2026-06", Account: "7030", Base: "-119.40"},
		{What: "INV-E01", Documents: []string{"INV-E01"}, Account: "1100", Base: "10900.00"},
	}}
	if bad := g.check(t, r); len(bad) != 0 {
		t.Fatalf("the right figures: %v", bad)
	}
	g.Checks = append(g.Checks,
		workbookCheck{What: "7030 June a cent off", Month: "2026-06", Account: "7030", Base: "-119.41"},
		workbookCheck{What: "the laptop's month", Documents: []string{"FA-01"}, Kind: "depreciation", Account: "6610", Each: "100.00"},
		workbookCheck{What: "nothing posted", Documents: []string{"INV-E99"}, Account: "1100", Each: "1.00"},
		workbookCheck{What: "within its tolerance", Month: "2026-06", Account: "7030", Base: "-119.399", Tolerance: "0.011"})
	g.Rejected = nil
	if bad := g.check(t, r); len(bad) != 4 {
		t.Errorf("found %d problems; want the refusal, the cent, the laptop's April and the figure nothing posted: %v", len(bad), bad)
	}
}
