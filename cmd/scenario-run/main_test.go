//go:build local

package main

import (
	"bytes"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.result.json")

// scenario-run's own golden case for the rules PRD-001 rev 0.24 decided (p0-manual-journal-rules 2.6),
// kept out of test/differential/golden/ until the oracle follows them (task 2.7). It reads the
// accounts' role: the journal to share capital is rejected (INV-13), as are the journal balanced in
// dollars but not in base (ENG-24 rule 7) and the reversal dated before its journal (FR-GL-12); the
// same-day reversal posts.
// req: INV-13
// req: FR-GL-12
// req: ENG-24
func TestScenarioRunOnTheDecidedRules(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"testdata/decided-rules.json"}, &out, &errs); code != 0 {
		t.Fatalf("scenario-run exited %d: %s", code, errs.String())
	}
	const golden = "testdata/decided-rules.result.json"
	if *update {
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil || !bytes.Equal(want, out.Bytes()) {
		t.Fatalf("scenario-run's result differs from %s (run with -update and review the diff): %v\n%s", golden, err, out.String())
	}
}

// A scenario the format refuses exits 2, like the oracle; a missing file exits 1.
// req: QA-ENG-02
func TestScenarioRunExitStatus(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run(nil, &out, &errs); code != 2 {
		t.Errorf("no argument: %d", code)
	}
	if code := run([]string{"testdata/no-such-file.json"}, &out, &errs); code != 1 {
		t.Errorf("a missing file: %d", code)
	}
	bad := t.TempDir() + "/bad.json"
	if err := os.WriteFile(bad, []byte(`{"schema":"fyniq-scenario@1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{bad}, &out, &errs); code != 2 {
		t.Errorf("an invalid scenario: %d", code)
	}
}
