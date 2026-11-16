package differential

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
		sdata, _ := os.ReadFile(filepath.Join(root, name, "scenario.json"))
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
