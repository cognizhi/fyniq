//go:build local

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/ports"
)

const suite = `{"id": "chat-fixture", "agent": "chat", "version": 1, "description": "A fixture: synthetic text only.", "threshold": "1",
  "cases": [{"id": "greets", "prompt": "Say hello.", "max_tokens": 16, "expect": {"contains": "hello"}},
  {"id": "sums", "prompt": "What is 2 plus 2?", "max_tokens": 16, "expect": {"equals": "4"}}]}`

// record keeps answer as the recorded answer of the case to model.
func record(t *testing.T, dir string, c aiqual.Case, model, answer string) {
	t.Helper()
	key, err := local.CassetteKey(c.Request(model))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(local.Cassette{Key: key, Model: model, Response: ports.ModelResponse{Model: model, Version: model + "-1",
		Parts: []ports.ModelPart{{Kind: ports.PartText, Text: answer}}, StopReason: ports.StopEnd}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// make evals replays each model's recorded answers to each suite and judges them by the suite's threshold, after
// checking the suite judges soundly (AI-78, CTL-AI-05): a recorded run below the threshold, a recording that is not
// whole, or a harness that cannot tell a right answer from an empty one fails it; a suite nobody recorded passes, saying
// so in the report.
// req: CTL-AI-05
// req: AI-78
func TestEvalsReplayTheRecordedRuns(t *testing.T) {
	t.Setenv(local.LiveEnv, "")
	setup := func(s string) (Options, aiqual.Suite) {
		root := t.TempDir()
		o := Options{Suites: root, Cassettes: filepath.Join(root, "cassettes"), Out: filepath.Join(root, "out")}
		for _, d := range []string{filepath.Join(root, "chat"), o.Cassettes} {
			if err := os.MkdirAll(d, 0o750); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "chat", "suite.json"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		all, err := aiqual.Load(os.DirFS(root))
		if err != nil {
			t.Fatal(err)
		}
		return o, all["chat"]
	}
	report := func(o Options) Report {
		raw, err := os.ReadFile(filepath.Join(o.Out, "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		var r Report
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	ctx := context.Background()

	// Nobody recorded it: it passes, and the report says so.
	o, s := setup(suite)
	if err := evaluate(ctx, o); err != nil {
		t.Fatal(err)
	}
	if r := report(o); !r.Passed || r.Mode != "replay" || len(r.Suites) != 1 || r.Suites[0].Harness != "sound" || !strings.Contains(r.Suites[0].Note, "no model") {
		t.Errorf("unrecorded: %+v", r)
	}

	// A model's whole recording passes; another's wrong answer fails the run.
	record(t, o.Cassettes, s.Cases[0], "good-1", "Hello!")
	record(t, o.Cassettes, s.Cases[1], "good-1", "4")
	if err := evaluate(ctx, o); err != nil {
		t.Fatal(err)
	}
	if r := report(o); !r.Passed || len(r.Suites[0].Runs) != 1 || r.Suites[0].Runs[0].Result != "pass" || r.Suites[0].Runs[0].Version != "good-1-1" {
		t.Errorf("a passing recording: %+v", r)
	}
	record(t, o.Cassettes, s.Cases[0], "bad-1", "Hello!")
	record(t, o.Cassettes, s.Cases[1], "bad-1", "5")
	if err := evaluate(ctx, o); err == nil {
		t.Error("a recorded run below its threshold passed")
	}
	if r := report(o); r.Passed || r.Suites[0].Runs[0].Model != "bad-1" || r.Suites[0].Runs[0].Failed[0] != "sums" {
		t.Errorf("a failing recording: %+v", r)
	}

	// A recording that is not whole fails.
	o, s = setup(suite)
	record(t, o.Cassettes, s.Cases[0], "half-1", "Hello!")
	if err := evaluate(ctx, o); err == nil || report(o).Suites[0].Runs[0].Result != "incomplete" {
		t.Errorf("half a recording: %v", err)
	}

	// A case an empty answer passes is not a sound harness.
	o, _ = setup(strings.Replace(suite, `{"equals": "4"}`, `{"matches": ".*"}`, 1))
	if err := evaluate(ctx, o); err == nil || !strings.Contains(report(o).Suites[0].Harness, "sums") {
		t.Errorf("a weak case: %v", err)
	}

	// The release's own suites are sound.
	if err := evaluate(ctx, Options{Suites: "../../evals", Cassettes: "../../evals/cassettes", Out: t.TempDir()}); err != nil {
		t.Errorf("the release's suites: %v", err)
	}
}
