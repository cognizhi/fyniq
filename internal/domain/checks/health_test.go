package checks_test

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/cognizhi/fyniq/internal/domain/checks"
)

// Books health follows the worst status found: problems when any check found ✖ or could not run, review when any found ⚠
// or ⏰, OK otherwise; checks not assessed are counted beside it and never change it; every check is counted once.
// req: FR-CTL-02
func TestBooksHealthIsTheWorstStatusFound(t *testing.T) {
	all := []checks.Status{checks.OK, checks.Warning, checks.Error, checks.Due, checks.NotAssessed, checks.Failed}
	rapid.Check(t, func(t *rapid.T) {
		statuses := rapid.SliceOf(rapid.SampledFrom(all)).Draw(t, "statuses")
		h := checks.HealthOf(statuses)
		if h.Problems+h.Warnings+h.Due+h.OK+h.NotAssessed != len(statuses) {
			t.Fatalf("%v counted as %+v", statuses, h)
		}
		want := "ok"
		switch {
		case slices.Contains(statuses, checks.Error) || slices.Contains(statuses, checks.Failed):
			want = "problems"
		case slices.Contains(statuses, checks.Warning) || slices.Contains(statuses, checks.Due):
			want = "review"
		}
		if h.Score != want {
			t.Fatalf("%v scored %s, want %s", statuses, h.Score, want)
		}
		without := slices.DeleteFunc(slices.Clone(statuses), func(s checks.Status) bool { return s == checks.NotAssessed })
		if checks.HealthOf(without).Score != h.Score {
			t.Fatalf("the checks not assessed in %v changed the score", statuses)
		}
	})
}

// The catalogue: each check has a stable key, an Appendix C row, words, an owner, a row of Appendix D for its items, and
// a worst status the engine knows; no key or row repeats.
// req: FR-CTL-01
func TestEveryCheckOfTheCatalogueIsWhole(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range checks.Catalogue() {
		if d.Key == "" || d.Ref == "" || d.Area == "" || d.Title == "" || d.Explain == "" || d.Owner == "" || d.View == "" || d.Run == nil {
			t.Errorf("%s is not whole: %+v", d.Key, d)
		}
		if !slices.Contains([]checks.Status{checks.Error, checks.Warning, checks.Due}, d.Worst) {
			t.Errorf("%s: worst %q", d.Key, d.Worst)
		}
		if seen[d.Key] || seen[d.Ref] {
			t.Errorf("%s or %s repeats", d.Key, d.Ref)
		}
		seen[d.Key], seen[d.Ref] = true, true
		if got, ok := checks.Find(d.Key); !ok || got.Ref != d.Ref {
			t.Errorf("Find(%s) = %+v, %v", d.Key, got, ok)
		}
	}
}
