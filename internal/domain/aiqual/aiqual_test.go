package aiqual

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cognizhi/fyniq/evals"
)

// A model's standing for an agent comes from its latest run (AI-78, AI-87): none or a failed one serves nothing; a
// pass serves until another version answers, or, for a moving alias, until its week ends.
// req: AI-78
// req: AI-87
func TestAStandingComesFromTheLatestRun(t *testing.T) {
	now := time.Date(2026, 11, 14, 6, 30, 0, 0, time.UTC)
	pass := &Run{Passed: true, RanAt: now.Add(-8 * 24 * time.Hour)}
	for name, tc := range map[string]struct {
		run   *Run
		alias bool
		want  string
	}{
		"never run":               {nil, false, Unqualified},
		"failed":                  {&Run{RanAt: now}, false, Unqualified},
		"a pinned pass ages well": {pass, false, Qualified},
		"an alias's week ended":   {pass, true, OutOfDate},
		"an alias this week":      {&Run{Passed: true, RanAt: now.Add(-6 * 24 * time.Hour)}, true, Qualified},
		"another version":         {&Run{Passed: true, RanAt: now, OutOfDate: true}, false, OutOfDate},
	} {
		if got := Standing(tc.run, tc.alias, now); got != tc.want {
			t.Errorf("%s: %s", name, got)
		}
	}
}

// A suite's threshold decides a run exactly, and each case's expectation is judged as written (AI-78).
// req: AI-78
func TestASuiteJudgesExactly(t *testing.T) {
	s := Suite{Threshold: "0.75", Cases: make([]Case, 3)}
	if s.Passes(2) || !s.Passes(3) {
		t.Error("0.75 of 3 needs 3")
	}
	s.Cases = make([]Case, 4)
	if s.Passes(2) || !s.Passes(3) {
		t.Error("0.75 of 4 needs 3")
	}
	s.Threshold = "1.5"
	if s.Passes(4) {
		t.Error("a threshold above 1 passes nothing")
	}
	for _, tc := range []struct {
		e      Expect
		answer string
		met    bool
	}{
		{Expect{Contains: "ok"}, "OK.", true},
		{Expect{Contains: "42"}, "forty-two", false},
		{Expect{Equals: "READY"}, " READY\n", true},
		{Expect{Equals: "READY"}, "Ready", false},
		{Expect{Matches: `^\d{4}-\d{2}$`}, "2026-11", true},
		{Expect{}, "anything", false},
	} {
		if tc.e.Met(tc.answer) != tc.met {
			t.Errorf("%+v on %q", tc.e, tc.answer)
		}
	}
}

// The release's suites load, one per agent, each case synthetic and complete; a suite that is not valid is refused
// with where it is (AI-78, CTL-AI-05).
// req: AI-78
// req: CTL-AI-05
func TestTheSuitesLoad(t *testing.T) {
	all, err := Load(evals.Suites)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := all["probe"]; !ok || p.ID != "probe" || len(p.Cases) == 0 || !p.Passes(len(p.Cases)) {
		t.Errorf("the probe agent's suite: %+v", all["probe"])
	}
	good := `{"id": "x-suite", "agent": "chat", "version": 1, "description": "d", "threshold": "1",
	  "cases": [{"id": "a", "prompt": "p", "max_tokens": 8, "expect": {"contains": "x"}}]}`
	for name, tc := range map[string]struct {
		fs   fstest.MapFS
		want string
	}{
		"unknown field": {fstest.MapFS{"x/suite.json": {Data: []byte(strings.Replace(good, `"version"`, `"secret": 1, "version"`, 1))}}, "unknown field"},
		"no threshold":  {fstest.MapFS{"x/suite.json": {Data: []byte(strings.Replace(good, `"1"`, `"0"`, 1))}}, "threshold"},
		"two expectations": {fstest.MapFS{"x/suite.json": {Data: []byte(strings.Replace(good, `{"contains": "x"}`,
			`{"contains": "x", "equals": "y"}`, 1))}}, "one expectation"},
		"a bad pattern": {fstest.MapFS{"x/suite.json": {Data: []byte(strings.Replace(good, `{"contains": "x"}`, `{"matches": "("}`, 1))}}, "pattern"},
		"two for chat": {fstest.MapFS{"x/suite.json": {Data: []byte(good)}, "y/suite.json": {Data: []byte(strings.Replace(good, "x-suite", "y-suite", 1))}},
			"a second suite"},
	} {
		if _, err := Load(tc.fs); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var e *Error
	if !errors.As(NoSuite("chat"), &e) || e.Code != CodeNoSuite {
		t.Errorf("no suite: %v", e)
	}
	if !errors.As(Unanswered("a", errors.New("busy")), &e) || e.Code != CodeUnanswered || e.Unwrap() == nil {
		t.Errorf("unanswered: %v", e)
	}
	if !errors.As(NotQualified(0, "m", "chat", OutOfDate), &e) || e.Code != CodeUnqualified || !strings.Contains(e.Message, "out of date") {
		t.Errorf("not qualified: %v", e)
	}
}
