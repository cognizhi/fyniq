//go:build local

package differential

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// The differential gate (QA-ENG-02, task 3.1): the engine and the independent oracle (QA-ENG-01)
// post every golden scenario and a fixed-seed sample of generated ones, and must agree to the cent.
// It runs in `make test`, which blocks merge; `make differential` runs the full count with
// shrinking. It runs in the local build only, once per `make test`. uv must be installed (mise does
// it); the gate fails rather than skips without it.

const repo = "../.."

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

// req: QA-ENG-02
// req: QA-ENG-01
// req: DEV-04
func TestTheEngineAgreesWithTheOracle(t *testing.T) {
	seed := cmp.Or(os.Getenv("FYNIQ_DIFFERENTIAL_SEED"), "20261015")
	count := cmp.Or(os.Getenv("FYNIQ_DIFFERENTIAL_COUNT"), "60")
	generated := t.TempDir()
	uv(t, "python", "-m", "oracle", "generate", "--seed", seed, "--count", count, "--out", generated)

	var files []string
	for _, dir := range []string{filepath.Join(repo, "test/differential/golden"), generated} {
		found, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		files = append(files, found...)
	}
	sort.Strings(files)
	if len(files) < 4 {
		t.Fatalf("only %d scenarios", len(files))
	}
	abs := make([]string, len(files))
	for i, f := range files {
		abs[i], _ = filepath.Abs(f)
	}
	oracle := map[string]json.RawMessage{}
	invalid := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(uv(t, append([]string{"python", "test/differential/oracle_batch.py"}, abs...)...)))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
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
			invalid[line.File] = line.Invalid
		} else {
			oracle[line.File] = line.Result
		}
	}

	failures := 0
	for _, f := range abs {
		data, _ := os.ReadFile(f)
		s, err := ParseScenario(data)
		var bad *InvalidError
		if errors.As(err, &bad) || invalid[f] != "" {
			if err == nil || invalid[f] == "" {
				t.Errorf("%s: only one side found the scenario invalid: engine %v, oracle %q", filepath.Base(f), err, invalid[f])
				failures++
			}
			continue
		}
		engine, err := RunEngine(s)
		if err != nil {
			t.Errorf("%s: the engine could not run it: %v", filepath.Base(f), err)
			failures++
			continue
		}
		var want Result
		if err := json.Unmarshal(oracle[f], &want); err != nil {
			t.Fatalf("%s: the oracle's result: %v", filepath.Base(f), err)
		}
		if err := Compare(engine, want); err != nil {
			failures++
			kept := filepath.Join(repo, ".data/differential", filepath.Base(f))
			_ = os.MkdirAll(filepath.Dir(kept), 0o755)
			_ = os.WriteFile(kept, data, 0o644)
			t.Errorf("%s: the engine and the oracle disagree: %v\n  kept in %s; run `make differential` to shrink it to a golden case",
				filepath.Base(f), err, kept)
		}
	}
	t.Logf("%d scenarios (golden and %s generated with seed %s), %d disagreements", len(abs), count, seed, failures)
}
