//go:build local

package differential

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// req: QA-ENG-02
// req: FR-PLN-06
// req: RND-PLAN
// The feasibility calculator and the oracle's independent model work out every golden study and a fixed-seed sample of
// generated ones, and agree on every figure (p1-planning D9).
func TestTheFeasibilityCalculatorAgreesWithTheOracle(t *testing.T) {
	seed := cmp.Or(os.Getenv("FYNIQ_DIFFERENTIAL_SEED"), "20261108")
	count := cmp.Or(os.Getenv("FYNIQ_FEASIBILITY_COUNT"), "40")
	generated := t.TempDir()
	uv(t, "python", "-m", "oracle", "generate-feasibilities", "--seed", seed, "--count", count, "--out", generated)
	var files []string
	for _, dir := range []string{filepath.Join(repo, "test/differential/feasibilities"), generated} {
		found, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range found {
			abs, _ := filepath.Abs(f)
			files = append(files, abs)
		}
	}
	sort.Strings(files)
	if len(files) < 10 {
		t.Fatalf("only %d studies", len(files))
	}
	oracle := map[string]json.RawMessage{}
	sc := bufio.NewScanner(bytes.NewReader(uv(t, append([]string{"python", "test/differential/oracle_batch.py"}, files...)...)))
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	for sc.Scan() {
		var line struct {
			File    string          `json:"file"`
			Result  json.RawMessage `json:"result"`
			Invalid string          `json:"invalid"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		if line.Invalid != "" {
			t.Errorf("%s: the oracle finds it invalid: %s", filepath.Base(line.File), line.Invalid)
			continue
		}
		oracle[line.File] = line.Result
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := ParseFeasibility(b)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		res, err := RunFeasibility(doc)
		if err != nil {
			t.Errorf("%s: the engine: %v", filepath.Base(f), err)
			continue
		}
		got, _ := json.Marshal(res)
		if d := FeasibilityDifference(got, oracle[f]); d != "" {
			t.Errorf("%s: %s", filepath.Base(f), d)
		}
	}
}
