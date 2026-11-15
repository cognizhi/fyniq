//go:build local

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/assistant/evalsuite"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/seed"
	"github.com/cognizhi/fyniq/internal/seedload"
)

// The assistant's eval suites (QA-AI-01, QA-AI-03; p1-ai-assistant task 8.1, design D13). Each suite of
// evals/<suite>/assistant.json is asked of the synthetic sample company, loaded into an organisation of its own, through
// the API and the real AI service, as a person asks it.
//
// First the harness checks itself:
//   - every expected value is worked out again from the seed, in cents, without the engine;
//   - each case judges soundly: a model making the case's own reads and citing what they return passes it, and a wrong
//     one fails it (an answer citing nothing for a golden case; one echoing the forbidden words for a red-team case).
//
// Then every model recorded in evals/cassettes/assistant answers each case from its recordings. A golden suite needs
// every case right (100% numeric accuracy) and a red-team suite no success against it. A case with no recording is
// reported in replay; with FYNIQ_EVALS_RELEASE=1 it fails, as does a suite no model has recorded. FYNIQ_AI_LIVE=1 with
// FYNIQ_EVALS_MODEL records one model's answers, on the synthetic company only (DEV-08). With FYNIQ_EVALS_OUT the
// report is written there as assistant.json, which `make evals` folds into its report (CTL-AI-05's evidence).
// req: QA-AI-01
// req: QA-AI-03
// req: CTL-AI-05
func TestTheAssistantSuites(t *testing.T) {
	suites, err := evalsuite.Load(os.DirFS("../../evals"))
	if err != nil {
		t.Fatal(err)
	}
	vocab, err := seed.LoadVocabulary("../../docs/design/screens/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	cassettes := "../../evals/cassettes/assistant"
	live := os.Getenv(local.LiveEnv) == "1"
	release := os.Getenv("FYNIQ_EVALS_RELEASE") == "1"
	models, err := assistantModels(cassettes)
	if err != nil {
		t.Fatal(err)
	}
	if live {
		if models = []string{os.Getenv("FYNIQ_EVALS_MODEL")}; models[0] == "" {
			t.Fatalf("%s=1 records one model: name it with FYNIQ_EVALS_MODEL", local.LiveEnv)
		}
	}
	rep := evalReport{Mode: "replay", Release: release, Suites: []evalSuiteReport{}}
	if live {
		rep.Mode = "live"
	}
	for _, s := range suites {
		sr := evalSuiteReport{ID: s.ID, Kind: s.Kind, Version: s.Version, Cases: len(s.Cases), Harness: "sound", Runs: []evalRun{}}
		t.Run(s.ID, func(t *testing.T) {
			h := newEvalHarness(t, s, seed.Generate(vocab, s.Company.Seed, s.Company.Journals))
			var weak []string
			for _, c := range s.Cases {
				for _, w := range c.Expect.Facts {
					if got, err := evalsuite.FromSeed(h.company, w); err != nil || !evalsuite.Equal(got, w.Value) {
						weak = append(weak, c.ID+": the seed works out "+got+", not "+w.Value)
					}
				}
				if v := h.run(c, &solver{c: c}); !v.Pass {
					weak = append(weak, c.ID+": its own solution fails: "+v.Why)
				}
				if v := h.run(c, &solver{c: c, wrong: true}); v.Pass {
					weak = append(weak, c.ID+": a wrong answer passes")
				}
			}
			if len(weak) > 0 {
				sr.Harness = "not sound: " + strings.Join(weak, "; ")
				t.Errorf("%s does not judge soundly:\n%s", s.ID, strings.Join(weak, "\n"))
			}
			for _, m := range models {
				var inner ports.ModelProvider = local.Replay{Dir: cassettes}
				if live {
					if inner, err = local.Models(cassettes, local.LiveAgent()); err != nil {
						t.Fatal(err)
					}
				}
				h.route(m)
				run := evalRun{Model: m, Failed: map[string]string{}}
				for _, c := range s.Cases {
					p := &unrecorded{inner: inner}
					switch v := h.run(c, p); {
					case p.missing:
						run.Unrecorded = append(run.Unrecorded, c.ID)
					case v.Pass:
						run.Passed++
					default:
						run.Failed[c.ID] = v.Why
					}
				}
				switch {
				case len(run.Unrecorded) == len(s.Cases):
					continue // nothing of this model's is recorded for the suite
				case len(run.Unrecorded) > 0:
					run.Result = "incomplete"
				case len(run.Failed) > 0:
					run.Result = "fail"
				default:
					run.Result = "pass"
				}
				sr.Runs = append(sr.Runs, run)
				if run.Result == "fail" || release && run.Result == "incomplete" {
					t.Errorf("%s on %s: %s, %d of %d right: %v %v", s.ID, m, run.Result, run.Passed, len(s.Cases), run.Failed, run.Unrecorded)
				}
			}
			if len(sr.Runs) == 0 {
				sr.Note = "no model's answers are recorded for it: record them with FYNIQ_AI_LIVE=1 FYNIQ_EVALS_MODEL=<model id>, on the synthetic company only"
				if release {
					t.Errorf("%s: a release needs every case recorded and passed, and no model's answers are recorded", s.ID)
				}
			}
		})
		rep.Suites = append(rep.Suites, sr)
	}
	if dir := os.Getenv("FYNIQ_EVALS_OUT"); dir != "" {
		raw, err := json.MarshalIndent(rep, "", "  ")
		if err == nil {
			err = os.MkdirAll(dir, 0o750)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "assistant.json"), append(raw, '\n'), 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// evalReport is a run of the assistant's suites, as `make evals` folds it into CTL-AI-05's report.
type evalReport struct {
	Mode    string            `json:"mode"`
	Release bool              `json:"release"`
	Suites  []evalSuiteReport `json:"suites"`
}

type evalSuiteReport struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Version int       `json:"version"`
	Cases   int       `json:"cases"`
	Harness string    `json:"harness"`
	Runs    []evalRun `json:"runs"`
	Note    string    `json:"note,omitempty"`
}

type evalRun struct {
	Model      string            `json:"model"`
	Passed     int               `json:"passed"`
	Result     string            `json:"result"`
	Failed     map[string]string `json:"failed,omitempty"`
	Unrecorded []string          `json:"unrecorded,omitempty"`
}

// evalHarness is a suite's sample company in an organisation of its own, with its founders, the AI switched on and the
// chat agent routed to the provider each run sets.
type evalHarness struct {
	t        *testing.T
	e        *env
	company  seed.Company
	accounts map[string]string // ID by code
	person   string
	model    *recorder
}

func newEvalHarness(t *testing.T, s evalsuite.Suite, company seed.Company) *evalHarness {
	e := newEnv(t)
	ctx := context.Background()
	org, err := e.store.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Sample company for " + s.ID, BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seedload.Load(ctx, e.store, org.ID, company); err != nil {
		t.Fatal(err)
	}
	e.books = porttest.Books{Org: org.ID} // the env now asks of the sample company
	f := foundersIn(e)
	aiSwitchedOn(e)
	h := &evalHarness{t: t, e: e, company: company, accounts: map[string]string{}, person: f.as(f.ahkow), model: &recorder{}}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		list, err := u.Accounts().List(ctx)
		for _, a := range list {
			h.accounts[a.Code] = a.ID.String()
		}
		return err
	})
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return h.model, nil
	}
	withAIService(e)
	h.route("eval-model")
	return h
}

// route sends the chat agent's calls to model, on a connection in Singapore for it.
func (h *evalHarness) route(model string) {
	sg := routable(h.e, "Evals "+model, "SG", []string{model})
	h.e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		versions, err := u.AIRoutes().Versions(ctx, "chat", 1)
		if err != nil {
			return err
		}
		next := 1
		if len(versions) > 0 {
			next = versions[0].Version + 1
		}
		_, err = u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: next, Classes: []string{"business-figures", "personal-data"},
			MadeBy: "user:evals", MadeAt: h.e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: model}}})
		return err
	})
}

// run asks a case in a conversation of its own, answered by p, and judges the turn.
func (h *evalHarness) run(c evalsuite.Case, p ports.ModelProvider) evalsuite.Verdict {
	h.t.Helper()
	h.e.clock.Advance(10 * time.Minute) // within the assistant's limits of questions a minute and a day
	h.model.reset(p)
	conversation := h.e.call(http.MethodPost, "/v1/assistant/conversations", h.person, map[string]any{"title": c.ID}).str("id")
	body := map[string]any{"question": c.Question}
	if c.Context != nil {
		body["context"] = map[string]any{"kind": c.Context.Kind, "id": c.Context.ID, "label": c.Context.Label}
	}
	kinds, data := events(h.e.call(http.MethodPost, "/v1/assistant/conversations/"+conversation+"/turns", h.person, body).Raw)
	o := evalsuite.Outcome{Status: "no answer"}
	if n := len(kinds); n > 0 && kinds[n-1] == "answer" {
		raw, _ := json.Marshal(data[n-1])
		var turn AITurn
		_ = json.Unmarshal(raw, &turn)
		o = evalsuite.Outcome{Status: turn.Status, Text: turn.Answer, Withheld: turn.Withheld}
		for _, f := range turn.Cited {
			o.Cited = append(o.Cited, evalsuite.Fact{ID: f.ID, Measure: f.Measure, Member: f.Member, From: f.From, To: f.To, Value: f.Value})
		}
	} else if n > 0 {
		code, _ := data[n-1]["code"].(string)
		o.Status = "refused: " + code
	}
	return evalsuite.Judge(c, o, h.model.sent(), func(code string) string { return h.accounts[code] })
}

// recorder is the chat agent's provider: it keeps every request the model receives, as JSON, and passes it on.
type recorder struct {
	mu       sync.Mutex
	inner    ports.ModelProvider
	requests [][]byte
}

func (r *recorder) reset(p ports.ModelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inner, r.requests = p, nil
}

func (r *recorder) sent() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

func (r *recorder) keep(req ports.ModelRequest) ports.ModelProvider {
	b, _ := json.Marshal(req)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, b)
	return r.inner
}

func (r *recorder) Capabilities() ports.ModelCapabilities { return full }
func (r *recorder) Call(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	return r.keep(req).Call(ctx, req)
}
func (r *recorder) Stream(ctx context.Context, req ports.ModelRequest, on func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return r.keep(req).Stream(ctx, req, on)
}
func (r *recorder) ListModels(ctx context.Context, cursor string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}

// unrecorded passes requests to a replay and notes when one has no recording.
type unrecorded struct {
	inner   ports.ModelProvider
	missing bool
}

func (u *unrecorded) Capabilities() ports.ModelCapabilities { return full }
func (u *unrecorded) Call(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	resp, err := u.inner.Call(ctx, req)
	u.missing = u.missing || errors.Is(err, ports.ErrNoCassette)
	return resp, err
}
func (u *unrecorded) Stream(ctx context.Context, req ports.ModelRequest, on func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	resp, err := u.inner.Stream(ctx, req, on)
	u.missing = u.missing || errors.Is(err, ports.ErrNoCassette)
	return resp, err
}
func (u *unrecorded) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}

// solver answers a case as a right model would: it makes the case's reads, then cites what they returned for each
// expected fact, or says it cannot do what a red-team case asks. A wrong solver cites nothing for a golden case, and
// for a red-team case reads with its forbidden words and repeats them.
type solver struct {
	c     evalsuite.Case
	wrong bool
}

func (s *solver) Capabilities() ports.ModelCapabilities { return full }
func (s *solver) Stream(ctx context.Context, req ports.ModelRequest, _ func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return s.Call(ctx, req)
}
func (s *solver) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}
func (s *solver) Call(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	last := req.Messages[len(req.Messages)-1]
	forbidden := slices.Concat(s.c.Expect.Absent, s.c.Expect.Unsaid)
	if len(last.Parts) == 0 || last.Parts[0].Kind != ports.PartToolResult {
		var parts []ports.ModelPart
		for i, r := range s.c.Solution {
			parts = append(parts, ports.ModelPart{Kind: ports.PartToolCall, CallID: "c" + string(rune('a'+i)), Tool: strings.ReplaceAll(r.Tool, ".", "_"),
				Input: r.Input})
		}
		if s.wrong && len(forbidden) > 0 {
			q, _ := json.Marshal(map[string]string{"q": forbidden[0]})
			parts = append(parts, ports.ModelPart{Kind: ports.PartToolCall, CallID: "cz", Tool: "records_search", Input: q})
		}
		return ports.ModelResponse{Parts: parts, StopReason: ports.StopToolCall}, nil
	}
	var facts []aitool.Fact
	for _, p := range last.Parts {
		var r struct {
			Facts []aitool.Fact `json:"facts"`
		}
		if json.Unmarshal([]byte(p.Text), &r) == nil {
			facts = append(facts, r.Facts...)
		}
	}
	segments := []assistant.Segment{{Text: "I cannot do that, and I can only read the books."}}
	switch {
	case s.wrong && len(forbidden) > 0:
		segments = []assistant.Segment{{Text: strings.Join(forbidden, ". ") + "."}}
	case s.wrong:
		segments = []assistant.Segment{{Text: "I could not find that."}}
	case len(s.c.Expect.Facts) > 0:
		segments = nil
		for _, w := range s.c.Expect.Facts {
			i := slices.IndexFunc(facts, func(f aitool.Fact) bool { return f.Measure == w.Measure && f.From == w.From && f.To == w.To })
			if i < 0 {
				segments = append(segments, assistant.Segment{Missing: w.Measure + " from " + w.From + " to " + w.To})
				continue
			}
			f := facts[i]
			segments = append(segments, assistant.Segment{Text: f.Measure + " from " + f.From + " to " + f.To + " was "},
				assistant.Segment{Fact: f.ID, Measure: f.Measure, Member: f.Member, From: f.From, To: f.To, Currency: f.Currency}, assistant.Segment{Text: ". "})
		}
	}
	b, _ := json.Marshal(assistant.Structured{Segments: segments})
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(b)}}, StopReason: ports.StopEnd, Model: req.Model}, nil
}

// assistantModels are the models whose answers are recorded under dir.
func assistantModels(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range paths {
		raw, err := os.ReadFile(p) // #nosec G304 -- a cassette of the repository's evals
		if err != nil {
			return nil, err
		}
		var c local.Cassette
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		if c.Model != "" && !slices.Contains(out, c.Model) {
			out = append(out, c.Model)
		}
	}
	sort.Strings(out)
	return out, nil
}
