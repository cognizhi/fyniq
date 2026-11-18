// Package aiqual keeps models qualified before they serve (PRD-001 AI-78, AI-87, CMP-AI-03; p1-ai-providers task 5.4,
// design D10): a model serves an agent only on its latest run of that agent's eval suite passing the suite's threshold;
// a later answer reporting another version, or, for a moving alias, a week passing, puts it out of date, and then it
// carries no personal data or payroll until it is qualified again. A route that names a model needs its passing,
// current qualification for the route's agent.
package aiqual

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The codes of a refused qualification step (ENG-23).
const (
	CodeNoSuite     = "AI-QUAL-01" // the agent has no eval suite yet: it arrives with the agent
	CodeUnanswered  = "AI-QUAL-02" // the provider did not answer a case, so nothing was recorded
	CodeUnqualified = "AI-QUAL-03" // a route names a model that has not passed the agent's evals there
)

// Error is a refused qualification step, in Fyniq's words (ENG-23).
type Error struct {
	Code, Field, Message, Fix string
	Err                       error // what the provider or the budgets said, for an unanswered case
}

func (e *Error) Error() string { return e.Code + ": " + e.Field + " " + e.Message }

func (e *Error) Unwrap() error { return e.Err }

// The results of a run.
const (
	Pass = "pass"
	Fail = "fail"
)

// The standings of a model for an agent.
const (
	Qualified   = "qualified"
	OutOfDate   = "out_of_date" // passed, but another version answered since, or an alias's week passed: no personal data or payroll
	Unqualified = "unqualified" // never passed, or its latest run failed: it does not serve the agent
)

// AliasWeek is how long a moving alias's qualification lasts before it is run again (AI-78).
const AliasWeek = 7 * 24 * time.Hour

// Run is a qualification as its standing is judged.
type Run struct {
	Passed    bool
	RanAt     time.Time
	OutOfDate bool
}

// Standing is a model's standing for an agent from its latest run, nil when it has none (AI-78, AI-87).
func Standing(latest *Run, alias bool, now time.Time) string {
	switch {
	case latest == nil || !latest.Passed:
		return Unqualified
	case latest.OutOfDate, alias && now.Sub(latest.RanAt) > AliasWeek:
		return OutOfDate
	}
	return Qualified
}

// Suite is an agent's eval suite: cases a model must answer as expected, enough of them to reach the threshold.
type Suite struct {
	ID          string `json:"id"`
	Agent       string `json:"agent"`
	Version     int    `json:"version"`
	Description string `json:"description"`
	Threshold   string `json:"threshold"` // the share of cases that must pass, such as 1 or 0.95
	Cases       []Case `json:"cases"`
}

// Case is one question of a suite: synthetic text only, never an organisation's data.
type Case struct {
	ID        string `json:"id"`
	System    string `json:"system,omitempty"`
	Prompt    string `json:"prompt"`
	MaxTokens int    `json:"max_tokens"`
	Expect    Expect `json:"expect"`
}

// Request is the case as a model request to model: the same for a qualification and for make evals, so an answer
// recorded by one replays for the other.
func (c Case) Request(model string) ports.ModelRequest {
	return ports.ModelRequest{Model: model, System: c.System, MaxTokens: c.MaxTokens,
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: c.Prompt}}}}}
}

// Expect is what an answer must hold: text it contains, ignoring case; the whole answer, trimmed; or a pattern.
type Expect struct {
	Contains string `json:"contains,omitempty"`
	Equals   string `json:"equals,omitempty"`
	Matches  string `json:"matches,omitempty"`
}

// Met reports whether an answer holds what the case expects.
func (e Expect) Met(answer string) bool {
	switch {
	case e.Contains != "":
		return strings.Contains(strings.ToLower(answer), strings.ToLower(e.Contains))
	case e.Equals != "":
		return strings.TrimSpace(answer) == e.Equals
	case e.Matches != "":
		re, err := regexp.Compile(e.Matches)
		return err == nil && re.MatchString(answer)
	}
	return false
}

var suiteID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,59}$`)

// threshold is a suite's threshold as a decimal above zero, at most one.
func (s Suite) threshold() (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s.Threshold)
	if err != nil || d.Sign() <= 0 || d.Cmp(apd.New(1, 0)) > 0 {
		return nil, fmt.Errorf("its threshold %q is not a share above zero and at most 1", s.Threshold)
	}
	return d, nil
}

// Passes reports whether passed of the suite's cases reach its threshold, exactly.
func (s Suite) Passes(passed int) bool {
	t, err := s.threshold()
	if err != nil {
		return false
	}
	var need apd.Decimal
	if _, err := apd.BaseContext.WithPrecision(40).Mul(&need, t, apd.New(int64(len(s.Cases)), 0)); err != nil {
		return false
	}
	return apd.New(int64(passed), 0).Cmp(&need) >= 0
}

func (s Suite) check() error {
	if !suiteID.MatchString(s.ID) || s.Agent == "" || s.Version < 1 || len(s.Cases) == 0 {
		return fmt.Errorf("it needs an id, an agent, a version and cases")
	}
	if _, err := s.threshold(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		expectations := 0
		for _, e := range []string{c.Expect.Contains, c.Expect.Equals, c.Expect.Matches} {
			if e != "" {
				expectations++
			}
		}
		if c.ID == "" || seen[c.ID] || c.Prompt == "" || c.MaxTokens <= 0 || expectations != 1 {
			return fmt.Errorf("its case %q needs a unique id, a prompt, a token limit and one expectation", c.ID)
		}
		if c.Expect.Matches != "" {
			if _, err := regexp.Compile(c.Expect.Matches); err != nil {
				return fmt.Errorf("its case %q's pattern: %w", c.ID, err)
			}
		}
		seen[c.ID] = true
	}
	return nil
}

// Load reads every suite from fsys (evals/<suite>/suite.json), by agent: one suite per agent.
func Load(fsys fs.FS) (map[string]Suite, error) {
	paths, err := fs.Glob(fsys, "*/suite.json")
	if err != nil {
		return nil, err
	}
	out := map[string]Suite{}
	for _, p := range paths {
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		var s Suite
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if err := s.check(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if _, twice := out[s.Agent]; twice {
			return nil, fmt.Errorf("%s: a second suite for %s", p, s.Agent)
		}
		out[s.Agent] = s
	}
	return out, nil
}

// Unanswered stops a run whose case the provider did not answer: a busy, failing or refused provider is not the model
// failing its evals, so nothing is recorded and its standing stays as it was.
func Unanswered(caseID string, err error) error {
	return &Error{Code: CodeUnanswered, Field: "model", Message: "the provider did not answer the case " + caseID + " (" + err.Error() +
		"), so the run stopped and nothing was recorded", Fix: "Try again later; a budget that is spent needs the Owner first", Err: err}
}

// NotQualified refuses a route that names a model the agent's evals have not passed on its connection, or whose pass is
// out of date (AI-78, AI-87).
func NotQualified(step int, model, agent, standing string) error {
	msg := model + " has not passed the evals of " + agent + " on this connection"
	if standing == OutOfDate {
		msg = model + "'s pass of the evals of " + agent + " is out of date: another version answered since, or its week as a moving alias ended"
	}
	return &Error{Code: CodeUnqualified, Field: fmt.Sprintf("steps[%d].model", step), Message: msg,
		Fix: "Qualify it on the connection's Models tab first: a route change carries a passing eval run of every step (CMP-AI-03)"}
}

// NoSuite refuses to qualify for an agent that has no suite yet.
func NoSuite(agent string) error {
	return &Error{Code: CodeNoSuite, Field: "agent", Message: agent + " has no eval suite yet, so no model can be qualified for it",
		Fix: "Its suite arrives with the agent; until then the agent does not run"}
}
