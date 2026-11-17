//go:build local

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/proposal"
)

func init() { run = evaluate }

// Report is a run of the suites: CTL-AI-05's evidence. Assistant is the run of the assistant's own suites
// (evals/<suite>/assistant.json), which the API's TestTheAssistantSuites makes through the real orchestrator just
// before (scripts/evals.sh) and writes beside this report; Agents the scheduled agents' (evals/<suite>/agent.json,
// QA-AI-07), which TestTheAgentSuites makes through the worker, the API and the AI service.
type Report struct {
	Mode      string          `json:"mode"`
	RanAt     time.Time       `json:"ran_at"`
	Passed    bool            `json:"passed"`
	Versions  Versions        `json:"versions"`
	Suites    []SuiteReport   `json:"suites"`
	Assistant json.RawMessage `json:"assistant,omitempty"`
	Agents    json.RawMessage `json:"agents,omitempty"`
}

// Versions are the AI contracts the run checked (§12.10, "AI catalogue, prompt and tool versions"; CMP-AI-03): each
// read tool, task type and agent's system prompt in use, with its version and hash as the build embeds them, so a
// release's evidence names what its evals passed on.
type Versions struct {
	Tools   []Pinned `json:"tools"`
	Tasks   []Pinned `json:"tasks"`
	Prompts []Pinned `json:"prompts"`
}

// Pinned is one contract's version and the hash of its definition.
type Pinned struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

// versions are the release's AI contracts, from contracts/ai.
func versions() (Versions, error) {
	v := Versions{Tools: []Pinned{}, Tasks: []Pinned{}, Prompts: []Pinned{}}
	for _, t := range aitool.Default().Tools() {
		v.Tools = append(v.Tools, Pinned{t.Name, t.Version, t.SHA256})
	}
	for _, t := range proposal.Default().Types() {
		v.Tasks = append(v.Tasks, Pinned{t.ID, t.Version, t.SHA256})
	}
	files, err := fs.Glob(contracts.Prompts, "prompts/*@*.md")
	if err != nil {
		return v, err
	}
	var agents []string
	for _, f := range files {
		agents = append(agents, strings.SplitN(path.Base(f), "@", 2)[0])
	}
	slices.Sort(agents)
	for _, a := range slices.Compact(agents) {
		p, err := assistant.DefaultPrompt(a)
		if err != nil {
			return v, err
		}
		v.Prompts = append(v.Prompts, Pinned{p.Agent, p.Version, p.SHA256})
	}
	return v, nil
}

// SuiteReport is one suite: whether its harness is sound, and each recorded model's run.
type SuiteReport struct {
	ID        string     `json:"id"`
	Agent     string     `json:"agent"`
	Version   int        `json:"version"`
	Threshold string     `json:"threshold"`
	Cases     int        `json:"cases"`
	Harness   string     `json:"harness"` // "sound", or what is not
	Runs      []ModelRun `json:"runs"`
	Note      string     `json:"note,omitempty"`
}

// ModelRun is a model's run of a suite.
type ModelRun struct {
	Model      string   `json:"model"`
	Version    string   `json:"version,omitempty"`
	Passed     int      `json:"passed"`
	Result     string   `json:"result"` // pass, fail, or incomplete when some cases have no recorded answer
	Failed     []string `json:"failed,omitempty"`
	Unrecorded []string `json:"unrecorded,omitempty"`
}

func evaluate(ctx context.Context, o Options) error {
	suites, err := aiqual.Load(os.DirFS(o.Suites))
	if err != nil {
		return err
	}
	live := os.Getenv(local.LiveEnv) == "1"
	rep := Report{Mode: "replay", RanAt: clock.System{}.Now().UTC(), Passed: true, Suites: []SuiteReport{}}
	if rep.Versions, err = versions(); err != nil {
		return err
	}
	var models []string
	if live {
		if o.Model == "" {
			return fmt.Errorf("%s=1 runs one model: name it with MODEL=<model id>", local.LiveEnv)
		}
		rep.Mode, models = "live", []string{o.Model}
	} else if models, err = recorded(o.Cassettes); err != nil {
		return err
	}
	provider, err := local.Models(o.Cassettes, local.LiveAgent())
	if err != nil {
		return err
	}
	agents := make([]string, 0, len(suites))
	for a := range suites {
		agents = append(agents, a)
	}
	sort.Strings(agents)
	for _, agent := range agents {
		s := suites[agent]
		sr := SuiteReport{ID: s.ID, Agent: s.Agent, Version: s.Version, Threshold: s.Threshold, Cases: len(s.Cases), Harness: harness(s), Runs: []ModelRun{}}
		if sr.Harness != "sound" {
			rep.Passed = false
		}
		for _, m := range models {
			mr, ran, err := runSuite(ctx, provider, s, m)
			if err != nil {
				return fmt.Errorf("%s on %s: %w", s.ID, m, err)
			}
			if !ran {
				continue
			}
			if mr.Result != aiqual.Pass {
				rep.Passed = false
			}
			sr.Runs = append(sr.Runs, mr)
		}
		if len(sr.Runs) == 0 {
			sr.Note = "no model's answers are recorded for it: record them with FYNIQ_AI_LIVE=1 make evals MODEL=<model id>, on synthetic text only"
		}
		rep.Suites = append(rep.Suites, sr)
	}
	for name, into := range map[string]*json.RawMessage{"assistant.json": &rep.Assistant, "agent.json": &rep.Agents} {
		if raw, err := os.ReadFile(filepath.Join(o.Out, name)); err == nil { // #nosec G304 -- the report directory this run writes
			*into = raw
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := write(o.Out, rep); err != nil {
		return err
	}
	for _, sr := range rep.Suites {
		fmt.Printf("%s %s v%d (%s): harness %s", mark(sr.Harness == "sound" && !slices.ContainsFunc(sr.Runs, func(r ModelRun) bool {
			return r.Result != aiqual.Pass
		})), sr.ID, sr.Version, sr.Agent, sr.Harness)
		for _, r := range sr.Runs {
			fmt.Printf("; %s %s, %d of %d", r.Model, r.Result, r.Passed, sr.Cases)
		}
		if sr.Note != "" {
			fmt.Printf("; %s", sr.Note)
		}
		fmt.Println()
	}
	if !rep.Passed {
		return fmt.Errorf("a suite did not pass; see %s", filepath.Join(o.Out, "report.json"))
	}
	fmt.Printf("✔ evals (%s): %d of %d suites run on a recorded model, every suite sound; report in %s\n", rep.Mode, ranOn(rep), len(rep.Suites),
		filepath.Join(o.Out, "report.json"))
	if len(rep.Assistant) > 0 {
		fmt.Println("✔ the assistant's suites are in the report, as TestTheAssistantSuites ran them")
	}
	if len(rep.Agents) > 0 {
		fmt.Println("✔ the scheduled agents' suites are in the report, as TestTheAgentSuites ran them")
	}
	return nil
}

// ranOn is how many suites ran on at least one recorded model.
func ranOn(rep Report) int {
	n := 0
	for _, s := range rep.Suites {
		if len(s.Runs) > 0 {
			n++
		}
	}
	return n
}

func mark(ok bool) string {
	if ok {
		return "✔"
	}
	return "✖"
}

// harness checks a suite judges as it should: each case's own expected answer passes it, an empty answer fails it, and
// the threshold passes all its cases and fails none of them.
func harness(s aiqual.Suite) string {
	var weak []string
	for _, c := range s.Cases {
		right := c.Expect.Contains + c.Expect.Equals
		if (right != "" && !c.Expect.Met(right)) || c.Expect.Met("") {
			weak = append(weak, c.ID)
		}
	}
	switch {
	case len(weak) > 0:
		return "not sound: the cases " + strings.Join(weak, ", ") + " do not tell a right answer from an empty one"
	case !s.Passes(len(s.Cases)) || s.Passes(0):
		return "not sound: its threshold does not tell all cases passed from none"
	}
	return "sound"
}

// runSuite runs a suite against model: ran is false when none of its cases has an answer recorded for model.
func runSuite(ctx context.Context, p ports.ModelProvider, s aiqual.Suite, model string) (ModelRun, bool, error) {
	mr := ModelRun{Model: model}
	for _, c := range s.Cases {
		resp, err := p.Call(ctx, c.Request(model))
		switch {
		case errors.Is(err, ports.ErrNoCassette):
			mr.Unrecorded = append(mr.Unrecorded, c.ID)
			continue
		case err != nil:
			return mr, true, err
		}
		mr.Version = cmpOr(mr.Version, resp.Version)
		if c.Expect.Met(text(resp)) {
			mr.Passed++
		} else {
			mr.Failed = append(mr.Failed, c.ID)
		}
	}
	switch {
	case len(mr.Unrecorded) == len(s.Cases):
		return mr, false, nil
	case len(mr.Unrecorded) > 0:
		mr.Result = "incomplete"
	case s.Passes(mr.Passed):
		mr.Result = aiqual.Pass
	default:
		mr.Result = aiqual.Fail
	}
	return mr, true, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func text(r ports.ModelResponse) string {
	var b strings.Builder
	for _, p := range r.Parts {
		if p.Kind == ports.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// recorded are the models the cassettes hold answers of.
func recorded(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range paths {
		if filepath.Base(p) == "models.json" {
			continue
		}
		raw, err := os.ReadFile(p) // #nosec G304 -- a cassette of the repository's evals
		if err != nil {
			return nil, err
		}
		var c local.Cassette
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if c.Model != "" && !slices.Contains(out, c.Model) {
			out = append(out, c.Model)
		}
	}
	sort.Strings(out)
	return out, nil
}

func write(dir string, rep Report) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.json"), append(raw, '\n'), 0o600)
}
