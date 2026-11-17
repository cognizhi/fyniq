package differential

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The workbook's golden sets are the recorded ones (p1-launch-readiness task 3.1, design D4): sources.json names the
// workbook's repository and the commit its sets were read at, with the SHA-256 of each source file, and of each fixture
// scripts/workbook_golden.py wrote or a person kept (expected.json). A fixture edited by hand since, an expected value
// above all, or one added or left out, fails here until the script records it again. Each document mapping.json says
// a workbook row became is in the set's scenario.
func TestTheFixturesAreTheRecordedOnes(t *testing.T) {
	root := filepath.Join("..", "..", "test/golden/workbook") // the repository's, in both build flavours
	raw, err := os.ReadFile(filepath.Join(root, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		Sets       map[string]struct {
			Sources  map[string]string `json:"sources"`
			Fixtures map[string]string `json:"fixtures"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	hash := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if rec.Repository != "cognizhi/zh-sheet-accounting" || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(rec.Commit) || len(rec.Sets) == 0 {
		t.Fatalf("sources.json: repository %q, commit %q, %d sets", rec.Repository, rec.Commit, len(rec.Sets))
	}
	for name, set := range rec.Sets {
		for path, sum := range set.Sources {
			if !hash.MatchString(sum) {
				t.Errorf("%s: the source %s has no SHA-256", name, path)
			}
		}
		found, _ := filepath.Glob(filepath.Join(root, name, "*.json"))
		var files []string
		for _, f := range found {
			files = append(files, name+"/"+filepath.Base(f))
		}
		var recorded []string
		for f := range set.Fixtures {
			recorded = append(recorded, f)
		}
		slices.Sort(files)
		slices.Sort(recorded)
		if !slices.Equal(files, recorded) {
			t.Errorf("%s: the fixtures are %v; sources.json records %v", name, files, recorded)
		}
		for f, sum := range set.Fixtures {
			data, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				t.Errorf("%s: %v", f, err)
				continue
			}
			if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != sum {
				t.Errorf("%s changed since scripts/workbook_golden.py recorded it: run it again on the workbook's commit, never edit a figure by hand", f)
			}
		}
		var scenario struct {
			Documents []struct {
				ID string `json:"id"`
			} `json:"documents"`
		}
		var mapping []struct {
			Document  string `json:"document"`
			NotPorted string `json:"not_ported"`
		}
		sdata, err := os.ReadFile(filepath.Join(root, name, "scenario.json"))
		if os.IsNotExist(err) {
			continue // a set that posts nothing: TestTheNonPostingCasesHaveTheirTests reads its mapping
		}
		mdata, _ := os.ReadFile(filepath.Join(root, name, "mapping.json"))
		if json.Unmarshal(sdata, &scenario) != nil || json.Unmarshal(mdata, &mapping) != nil || len(mapping) == 0 {
			t.Fatalf("%s: its scenario or mapping", name)
		}
		ids := map[string]bool{}
		for _, d := range scenario.Documents {
			ids[d.ID] = true
		}
		for _, m := range mapping {
			if (m.Document == "") == (m.NotPorted == "") || (m.Document != "" && !ids[m.Document]) {
				t.Errorf("%s: a row maps to %q (or is not ported: %q), not a document of the scenario", name, m.Document, m.NotPorted)
			}
		}
	}
}

// The workbook's non-posting edge cases (tests/edge_nonposting.py; p1-launch-readiness task 3.1): its tables' statuses
// and report messages, and the cases it once got wrong (test_bug_*), post nothing, so they are ported as a mapping.
// statuses.json lists every case, read from the workbook's files by scripts/workbook_golden.py; mapping.json, kept by
// hand, gives each block of them the Fyniq rule that answers it and the tests that hold that rule, saying where Fyniq
// reads a status otherwise, or why it waits for after the launch (pay runs, projects) or has no counterpart. Every
// case is mapped once, and every test named is one of the repository's.
//
// The miscellaneous set's tests are held the same way (functions.json, cases.json): each is ported as figures, which its
// expected.json checks under the test's name, or held by a Fyniq test, or neither, said why; a test may be several.
// req: quality/calculation-correctness Golden data set, non-posting edge cases
// req: quality/calculation-correctness Golden data set, miscellaneous cases
func TestTheNonPostingCasesHaveTheirTests(t *testing.T) {
	root := filepath.Join("..", "..", "test/golden/workbook")
	defined := map[string]bool{}
	for _, top := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(filepath.Join("..", "..", top), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, _ := os.ReadFile(path)
			for _, m := range regexp.MustCompile(`(?m)^func (Test\w+)\(`).FindAllSubmatch(data, -1) {
				defined[string(m[1])] = true
			}
			return nil
		})
	}
	nonposting := otherCases(t, filepath.Join(root, "non-posting"), "statuses.json", "mapping.json", true, defined)
	misc := otherCases(t, filepath.Join(root, "miscellaneous"), "functions.json", "cases.json", false, defined)
	if nonposting < 100 || misc < 5 {
		t.Fatalf("%d non-posting cases and %d miscellaneous tests: the workbook has more", nonposting, misc)
	}
}

// otherCases checks a set's cases (listed) against how each is held (held), and says how many there are: with one,
// each entry is held one way only. defined is every test the repository has.
func otherCases(t *testing.T, dir, listed, held string, one bool, defined map[string]bool) int {
	t.Helper()
	var cases []struct{ Block, Status string }
	var mapping []struct {
		Block         string            `json:"block"`
		Statuses      []string          `json:"statuses"`
		Figures       string            `json:"figures"`
		Fyniq         string            `json:"fyniq"`
		Tests         []string          `json:"tests"`
		Read          map[string]string `json:"read"`
		AfterGoLive   string            `json:"after_go_live"`
		NotApplicable string            `json:"not_applicable"`
	}
	for f, v := range map[string]any{listed: &cases, held: &mapping} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || json.Unmarshal(data, v) != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	pinned := map[string]bool{}
	if data, err := os.ReadFile(filepath.Join(dir, "expected.json")); err == nil {
		var g struct {
			Checks []struct {
				Test string `json:"test"`
			} `json:"checks"`
		}
		if json.Unmarshal(data, &g) != nil {
			t.Fatalf("%s's expected.json", dir)
		}
		for _, c := range g.Checks {
			pinned[c.Test] = true
		}
	}
	mapped := map[string]int{}
	for _, m := range mapping {
		kinds := 0
		for _, ok := range []bool{m.Figures != "", m.Fyniq != "", m.AfterGoLive != "", m.NotApplicable != ""} {
			if ok {
				kinds++
			}
		}
		if (one && kinds != 1) || kinds == 0 || (m.Fyniq == "") != (len(m.Tests) == 0) {
			t.Errorf("%s %v: give the Fyniq rule with its tests, or why it waits for after the launch, or why it has no counterpart", m.Block, m.Statuses)
		}
		if m.Figures != "" {
			for _, s := range m.Statuses {
				if !pinned[s] {
					t.Errorf("%s: ported as figures, but expected.json checks none under its name", s)
				}
			}
		}
		for _, name := range m.Tests {
			if !defined[name] {
				t.Errorf("%s: %s is no test of the repository", m.Block, name)
			}
		}
		for s := range m.Read {
			if !slices.Contains(m.Statuses, s) {
				t.Errorf("%s: it reads %q, which is not one of its statuses", m.Block, s)
			}
		}
		for _, s := range m.Statuses {
			mapped[m.Block+"\x00"+s]++
		}
	}
	for _, c := range cases {
		if n := mapped[c.Block+"\x00"+c.Status]; n != 1 {
			t.Errorf("%s %q is mapped %d times; want once", c.Block, c.Status, n)
		}
	}
	return len(cases)
}
