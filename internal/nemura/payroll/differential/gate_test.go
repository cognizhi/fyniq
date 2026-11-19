//go:build local

package differential

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

const repo = "../../../.."

func uv(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("uv", append([]string{"run", "--project", "oracle", "--locked", "--quiet"}, args...)...)
	cmd.Dir = repo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("uv %v: %v\n%s(install the toolchain with `make setup`; the differential gate needs uv and the oracle)", args, err, stderr.String())
	}
	return out
}

// Nemura's payroll engine and the independent oracle work out every golden pay run and a fixed-seed sample of generated ones, and agree on every figure to the
// cent: each person's employee-month, the run's totals and B.3's entry (golden rule 3; p1-nemura-payroll 1.5). `make differential` lets Hypothesis search more.
// req: QA-ENG-02
// req: QA-ENG-01
// req: FR-PAY-18
func TestNemurasPayrollAgreesWithTheOracle(t *testing.T) {
	seed := cmp.Or(os.Getenv("FYNIQ_DIFFERENTIAL_SEED"), "20261120")
	count := cmp.Or(os.Getenv("FYNIQ_PAYROLL_COUNT"), "150")
	generated := t.TempDir()
	uv(t, "python", "-m", "oracle", "generate-payrolls", "--seed", seed, "--count", count, "--out", generated)
	var files []string
	for _, dir := range []string{filepath.Join(repo, "test/differential/payroll-runs"), generated} {
		found, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range found {
			abs, _ := filepath.Abs(f)
			files = append(files, abs)
		}
	}
	sort.Strings(files)
	if len(files) < 20 {
		t.Fatalf("only %d pay runs", len(files))
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
		doc, err := Parse(b)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		res, err := Run(doc)
		if err != nil {
			t.Errorf("%s: the engine: %v", filepath.Base(f), err)
			continue
		}
		got, _ := json.Marshal(res)
		if d := Difference(got, oracle[f]); d != "" {
			t.Errorf("%s: %s", filepath.Base(f), d)
		}
	}
}
