package knowledge_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/knowledge"
)

// The Go lint and scripts/kb.py give the same findings on every fixture of test/kb/fixtures (scripts/test_kb_fixtures.py
// is the other half): a rule changed in one and not the other fails one of them. The index each fixture ships is the one
// kb.py generated, so RenderIndex is checked against it too.
func TestTheGoLintAgreesWithKbPyOnTheFixtures(t *testing.T) {
	dirs, err := filepath.Glob("../../../test/kb/fixtures/*")
	if err != nil || len(dirs) < 20 {
		t.Fatalf("fixtures: %d, %v", len(dirs), err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			c, err := knowledge.Load(os.DirFS(filepath.Join(dir, "kb")), os.DirFS(filepath.Join(dir, "docs", "user-guide")))
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, f := range knowledge.Lint(c) {
				level := "error"
				if f.Warning {
					level = "warn"
				}
				got[level+" "+f.Rule+" "+f.Where] = true
			}
			raw, err := os.ReadFile(filepath.Join(dir, "expect.txt"))
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{}
			for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if l != "" {
					want[l] = true
				}
			}
			if diff := diffSets(got, want); diff != "" {
				t.Errorf("the Go lint and kb.py differ:\n%s", diff)
			}
		})
	}
}

func diffSets(got, want map[string]bool) string {
	var out []string
	for g := range got {
		if !want[g] {
			out = append(out, "  Go only: "+g)
		}
	}
	for w := range want {
		if !got[w] {
			out = append(out, "  kb.py only: "+w)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}
