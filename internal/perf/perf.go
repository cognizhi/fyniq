// Package perf is the performance harness (NFR-PERF-01 to NFR-PERF-06, NFR-SCALE-01, QA-PERF-01; PRD-001 rev 0.249,
// p1-operations-dr design D7): it grows a synthetic organisation's ledger through the posting engine, so every invariant
// holds, then times each budget through the running local stack's API, and writes what it measured. A gated budget it
// misses fails the run. Local and CI tooling only: the organisation is synthetic (DEV-08).
package perf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Budget is one of the performance requirements as the harness measures it.
type Budget struct {
	ID    string        `json:"id"`    // the requirement, such as NFR-PERF-01
	What  string        `json:"what"`  // in words
	Limit time.Duration `json:"limit"` // the 95th percentile's limit, or the whole run's
	Each  bool          `json:"each"`  // true: the limit is on the 95th percentile of the samples; false: on one run
	Gated bool          `json:"gated"` // a miss fails the run
}

// Result is what one budget measured.
type Result struct {
	Budget
	Samples int           `json:"samples"`
	P50     time.Duration `json:"p50,omitempty"`
	P95     time.Duration `json:"p95,omitempty"`
	Max     time.Duration `json:"max,omitempty"`
	Took    time.Duration `json:"took,omitempty"` // a single run's
	Passed  bool          `json:"passed"`
	Note    string        `json:"note,omitempty"`
	Error   string        `json:"error,omitempty"`
}

// Percentile is the nearest-rank percentile p (0 to 100) of ds; zero for none.
func Percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	rank := int(p/100*float64(len(s))+0.999999) - 1
	return s[max(0, min(rank, len(s)-1))]
}

// sampled is a budget's result from its samples: their median, 95th percentile and maximum against its limit.
func sampled(b Budget, ds []time.Duration, err error) Result {
	r := Result{Budget: b, Samples: len(ds), P50: Percentile(ds, 50), P95: Percentile(ds, 95), Max: Percentile(ds, 100)}
	r.Passed = err == nil && len(ds) > 0 && r.P95 <= b.Limit
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// once is a budget's result from a single run.
func once(b Budget, took time.Duration, err error) Result {
	r := Result{Budget: b, Samples: 1, Took: took, Passed: err == nil && took <= b.Limit}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// Report is a whole run: how the ledger grew and what each budget measured.
type Report struct {
	At      time.Time  `json:"at"`
	Version string     `json:"version"`
	Lines   int        `json:"lines"` // the ledger lines the run added
	Grown   GrowReport `json:"grown"`
	Results []Result   `json:"results"`
	Passed  bool       `json:"passed"` // every gated budget met
}

// Settle works out whether the run passed: every gated budget met.
func (r *Report) Settle() {
	r.Passed = true
	for _, x := range r.Results {
		if x.Gated && !x.Passed {
			r.Passed = false
		}
	}
}

func ms(d time.Duration) string {
	if d >= 10*time.Second {
		return d.Round(time.Second).String()
	}
	return fmt.Sprintf("%d ms", d.Milliseconds())
}

// Markdown is the report as people read it, a table of the budgets.
func (r Report) Markdown() string {
	var b strings.Builder
	verdict := "every gated budget met"
	if !r.Passed {
		verdict = "a gated budget missed"
	}
	fmt.Fprintf(&b, "# Performance run (QA-PERF-01)\n\n%s, Fyniq %s: %s.\n\n", r.At.UTC().Format(time.RFC3339), r.Version, verdict)
	fmt.Fprintf(&b, "The ledger grew by %d lines in %d journals through the posting engine in %s (%.0f lines a second).\n\n",
		r.Grown.Lines, r.Grown.Journals, r.Grown.Took.Round(time.Second), r.Grown.LinesPerSecond)
	b.WriteString("| Requirement | What | Limit | Measured | Samples | Result |\n|---|---|---|---|---|---|\n")
	for _, x := range r.Results {
		limit, measured := ms(x.Limit), ms(x.Took)
		if x.Each {
			limit, measured = "p95 ≤ "+ms(x.Limit), fmt.Sprintf("p50 %s, p95 %s, max %s", ms(x.P50), ms(x.P95), ms(x.Max))
		}
		result := "✔ met"
		switch {
		case x.Error != "":
			result = "✖ " + x.Error
		case !x.Passed && !x.Gated:
			result = "reported"
		case !x.Passed:
			result = "✖ missed"
		}
		if x.Note != "" {
			result += " (" + x.Note + ")"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %s |\n", x.ID, x.What, limit, measured, x.Samples, result)
	}
	return b.String()
}

// Write writes the report as report.json and report.md in dir.
func Write(dir string, r Report) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), j, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.md"), []byte(r.Markdown()), 0o600)
}
