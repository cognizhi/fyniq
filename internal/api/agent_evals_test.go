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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/assistant/evalsuite"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/seed"
	"github.com/cognizhi/fyniq/internal/seedload"
)

// The scheduled agents' eval suites (QA-AI-07; p1-scheduled-agents task 8.1, design D10). Each case of
// evals/<suite>/agent.json runs the agent on an organisation of its own: the synthetic sample company with the suite's
// plants and the case's own, made on the case's business date, the agent turned on and its task types on. Each day the
// checks run, then its daily monitor runs through the real worker, the API's model step and the AI service, as in
// production; a case whose model is unavailable has a provider that never answers. The case is judged by the last run's
// findings, Fyniq's summary, the model's words, the open tasks and the proposals. The corporate cases (p1-company-
// secretary task 7.1): the nominee and controller registers' filings overdue (G4-102), and a run whose model does not
// answer, with no financial year or corporate profile (G4-110).
//
// First the harness checks itself: a right model (the case's own solution) passes each case, and the same run with a
// planted finding missing, with "all clear" in its words or, for a red-team case, carrying the injected words, fails it.
// Then every model recorded in evals/cassettes/agent runs each case from its recordings, as the assistant's suites do.
// With FYNIQ_EVALS_OUT the report is written there as agent.json, which `make evals` folds into its report.
// req: QA-AI-07
// req: CTL-AI-05
// req: G4-102
// req: G4-110
func TestTheAgentSuites(t *testing.T) {
	suites, err := evalsuite.LoadAgents(os.DirFS("../../evals"))
	if err != nil {
		t.Fatal(err)
	}
	vocab, err := seed.LoadVocabulary("../../docs/design/screens/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	cassettes := "../../evals/cassettes/agent"
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
			company := seed.Generate(vocab, s.Company.Seed, s.Company.Journals)
			var weak []string
			for _, c := range s.Cases {
				v, o := runAgentCase(t, s, c, company, &agentSolver{c: c}, "eval-model")
				t.Logf("%s: %s; %d findings, tasks %v, %d proposals; the model: %q", c.ID, o.Status, len(o.Findings), o.Tasks, len(o.Proposals), o.ModelText)
				if !v.Pass {
					weak = append(weak, c.ID+": its own solution fails: "+v.Why)
					continue
				}
				missing := o
				missing.Findings = slices.DeleteFunc(slices.Clone(o.Findings), func(f evalsuite.WantFinding) bool {
					return evalsuite.SameKey(f.Key, c.Expect.Findings[0].Key)
				})
				if evalsuite.JudgeAgent(c, missing).Pass {
					weak = append(weak, c.ID+": a run missing "+c.Expect.Findings[0].Key+" passes")
				}
				clear := o
				clear.ModelText = "All clear: nothing needs you this week."
				if evalsuite.JudgeAgent(c, clear).Pass {
					weak = append(weak, c.ID+": a run saying all clear passes")
				}
				if c.RedTeam {
					obeyed := o
					obeyed.ModelText += " " + c.Expect.Absent[0]
					if evalsuite.JudgeAgent(c, obeyed).Pass {
						weak = append(weak, c.ID+": a run repeating the injection passes")
					}
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
				run := evalRun{Model: m, Failed: map[string]string{}}
				for _, c := range s.Cases {
					p := &unrecorded{inner: inner}
					switch v, _ := runAgentCase(t, s, c, company, p, m); {
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
					continue
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
				sr.Note = "no model's runs are recorded for it: record them with FYNIQ_AI_LIVE=1 FYNIQ_EVALS_MODEL=<model id>, on the synthetic company only"
				if release {
					t.Errorf("%s: a release needs every case recorded and passed, and no model's runs are recorded", s.ID)
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
			err = os.WriteFile(filepath.Join(dir, "agent.json"), append(raw, '\n'), 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// runAgentCase runs case c of suite s on an organisation of its own, its model step answered by provider p as model (or
// by none, when the case's model is unavailable), and judges it.
func runAgentCase(t *testing.T, s evalsuite.AgentSuite, c evalsuite.AgentCase, company seed.Company, p ports.ModelProvider, model string) (evalsuite.Verdict, evalsuite.AgentOutcome) {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	org, err := e.store.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Sample company for " + s.ID + "/" + c.ID, BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seedload.Load(ctx, e.store, org.ID, company); err != nil {
		t.Fatal(err)
	}
	e.books = porttest.Books{Org: org.ID}
	today, _ := time.Parse(time.DateOnly, c.Today)
	e.clock.Set(time.Date(today.Year(), today.Month(), today.Day(), 6, 0, 0, 0, clock.Singapore)) // the plants are made on the case's day
	f := foundersIn(e)
	aiSwitchedOn(e)
	owner := steppedUp(e, f.as(f.ahkow))
	secretaryToken, secretary := companySecretary(e, f) // the role a reminder may reach, by their verified email
	verifiedEmail(t, e, secretary, "secretary@example.test")
	if len(s.TaskTypes) > 0 {
		enableTaskTypes(e, strings.Join(s.TaskTypes, ","), 1)
	}
	as := map[string]string{"": owner, "owner": owner, "accountant": f.as(f.weiling, authz.Accountant), "company_secretary": secretaryToken}
	ids := map[string]string{}
	plant(t, e, c.ID, s.Plant, as, ids)
	plant(t, e, c.ID, c.Plant, as, ids)
	enableSecretary(e, f)
	sg := routable(e, "Agent evals "+model, "SG", []string{model})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "company_secretary", Version: 1, Classes: []string{"business-figures", "personal-data"},
			MadeBy: "user:evals", MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: model}}})
		return err
	})
	if c.Model == "unavailable" {
		p = downModel{}
	}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return p, nil
	}
	withAIRuns(e)

	dispatcher := outbox.Dispatcher{Store: workerStore(t), Queue: e.queue, Clock: e.clock}
	step := agents.APIModel{URL: e.srv.URL + "/internal/v1", Token: func(context.Context) (string, error) { return e.services.Token(agents.ModelClass) },
		HTTP: e.srv.Client()}
	runner := agents.Runner{Store: workerStore(t), Dispatcher: dispatcher, Clock: e.clock, Skills: agents.Default(), Model: step.Step}
	scheduler := agents.Scheduler{Store: workerStore(t), Dispatcher: dispatcher, Clock: e.clock, Skills: agents.Default()}
	var last storage.AgentRun
	for i := range max(c.Runs, 1) {
		day := today.AddDate(0, 0, i)
		e.clock.Set(time.Date(day.Year(), day.Month(), day.Day(), 6, 58, 0, 0, clock.Singapore))
		e.runCatalogue() // the checks run before the monitor, as they do each night, so its check findings are the day's
		if _, err := scheduler.ScheduleIn(ctx, e.books.Org); err != nil {
			t.Fatal(err)
		}
		slot := time.Date(day.Year(), day.Month(), day.Day(), 7, 0, 0, 0, clock.Singapore)
		var queued storage.AgentRun
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			list, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: "company-secretary", Statuses: []string{storage.AgentRunQueued}})
			for _, r := range list {
				if r.Trigger == "schedule:monitor" && r.SlotAt.Equal(slot) {
					queued = r
				}
			}
			return err
		})
		if queued.ID == uuid.Nil {
			t.Fatalf("%s: no monitor run queued for %s", c.ID, slot)
		}
		e.clock.Set(slot.Add(30 * time.Second))
		payload, _ := json.Marshal(agents.RunWork{RunID: queued.ID})
		if err := runner.Work(ctx, e.books.Org, storage.OutboxRow{ID: uuid.New(), Kind: agents.RunKind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			last, err = u.AgentRuns().Get(ctx, queued.ID)
			return err
		})
	}

	o := evalsuite.AgentOutcome{Status: last.Status, Tasks: map[string]int{}}
	if last.Status == storage.AgentRunFailed {
		o.Status += ": " + last.Reason
	}
	if rep := last.Report; rep != nil {
		o.Summary, o.ModelText, o.ModelMissed = rep.Summary, rep.ModelText, rep.ModelMissed
		var fs []agents.Finding
		_ = json.Unmarshal(rep.Findings, &fs)
		for _, x := range fs {
			if x.Detail == "The check has not run yet" {
				t.Fatalf("%s: %s did not run before the monitor, so its finding says nothing of the case", c.ID, x.Key)
			}
			o.Findings = append(o.Findings, evalsuite.WantFinding{Key: x.Key, Status: x.Status, Window: x.Window})
		}
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		tasks, err := u.ComplianceTasks().List(ctx, storage.ComplianceTaskQuery{Statuses: []string{storage.TaskOpen}, Limit: 500})
		if err != nil {
			return err
		}
		for _, x := range tasks {
			o.Tasks[x.FindingKey]++
		}
		ps, err := u.AIProposals().List(ctx, storage.AIProposalQuery{Agent: "company_secretary", Limit: 100})
		for _, pr := range ps {
			o.Proposals = append(o.Proposals, evalsuite.Made{TaskType: pr.TaskType, Status: pr.Status, Reason: pr.Reason, Payload: json.RawMessage(pr.Payload)})
		}
		return err
	})
	return evalsuite.JudgeAgent(c, o), o
}

// plant makes plants in order through the public API, each as its role (as, the Owner when it names none), a body's
// "$<id>" taking that earlier plant's ID in ids. A recorded plant has its evidence attached and its approval asked by its
// role, then signed by the Owner, so it is in force as a person would have recorded it.
func plant(t *testing.T, e *env, caseID string, plants []evalsuite.Plant, as map[string]string, ids map[string]string) {
	t.Helper()
	for _, pl := range plants {
		var body map[string]any
		_ = json.Unmarshal(pl.Body, &body)
		for k, v := range body {
			if ref, ok := v.(string); ok && strings.HasPrefix(ref, "$") {
				body[k] = ids[ref[1:]]
			}
		}
		tok := as[pl.As]
		r := e.call(http.MethodPost, pl.Path, tok, body, "Idempotency-Key", uuid.NewString())
		if r.Status != http.StatusCreated {
			t.Fatalf("%s: plant %s: %d %s", caseID, pl.ID, r.Status, r.Raw)
		}
		ids[pl.ID] = r.str("id")
		if !pl.Record {
			continue
		}
		path := pl.Path + "/" + r.str("id")
		if f := e.upload(path+"/evidence", tok, map[string]string{"evidence_type": "Evidence"}, "evidence.pdf", []byte("%PDF-1.7 evidence"),
			"If-Match", r.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("%s: plant %s's evidence: %d %s", caseID, pl.ID, f.Status, f.Raw)
		}
		cur := e.call(http.MethodGet, path, tok, nil)
		req := e.call(http.MethodPost, path+"/approval", tok, nil, "If-Match", cur.Header.Get("ETag"))
		if req.Status != http.StatusAccepted {
			t.Fatalf("%s: plant %s's approval asked: %d %s", caseID, pl.ID, req.Status, req.Raw)
		}
		if a := e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", as["owner"], hashOf(req), "If-Match", `"1"`); a.Status != http.StatusOK ||
			a.Body["status"] != "approved" {
			t.Fatalf("%s: plant %s approved: %d %s", caseID, pl.ID, a.Status, a.Raw)
		}
	}
}

// downModel is a model step's provider that never answers: a case whose model is unavailable (AI-64, rev 0.283).
type downModel struct{}

func (downModel) Capabilities() ports.ModelCapabilities { return full }
func (downModel) Stream(context.Context, ports.ModelRequest, func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return ports.ModelResponse{}, errors.New("the provider did not answer")
}
func (downModel) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}
func (downModel) Call(context.Context, ports.ModelRequest) (ports.ModelResponse, error) {
	return ports.ModelResponse{}, errors.New("the provider did not answer")
}

// agentProposed reports whether the run's model already made its proposals: a propose_ tool's result is in the request.
func agentProposed(req ports.ModelRequest) bool {
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if p.Kind == ports.PartToolResult && strings.HasPrefix(p.Tool, "propose_") {
				return true
			}
		}
	}
	return false
}

// agentSolver is a right model for an agent case: it makes the case's proposals first, through their propose_ tools, then
// answers with the case's words.
type agentSolver struct {
	c evalsuite.AgentCase
}

func (s *agentSolver) Capabilities() ports.ModelCapabilities { return full }
func (s *agentSolver) Stream(ctx context.Context, req ports.ModelRequest, _ func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return s.Call(ctx, req)
}
func (s *agentSolver) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}
func (s *agentSolver) Call(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	usage := ports.ModelUsage{Input: 500, Output: 100}
	if !agentProposed(req) && len(s.c.Solution.Proposals) > 0 {
		var calls []ports.ModelPart
		for i, p := range s.c.Solution.Proposals {
			input, _ := json.Marshal(map[string]any{"payload": p.Payload, "rationale": "It is what the findings ask for.", "evidence": []any{}})
			calls = append(calls, ports.ModelPart{Kind: ports.PartToolCall, CallID: "p" + string(rune('1'+i)), Tool: "propose_" + strings.ReplaceAll(p.TaskType, ".", "_"),
				Input: input})
		}
		return ports.ModelResponse{Parts: calls, StopReason: ports.StopToolCall, Usage: usage, Model: req.Model}, nil
	}
	b, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: s.c.Solution.Text}}})
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(b)}}, StopReason: ports.StopEnd, Usage: usage, Model: req.Model}, nil
}
