//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aiqual"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// evalModel answers eval cases as its script says, by a word of the prompt, and reports its version.
type evalModel struct {
	answers map[string]string
	version string
	calls   int
}

func (m *evalModel) Capabilities() ports.ModelCapabilities { return full }
func (m *evalModel) Call(_ context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	m.calls++
	prompt := r.Messages[len(r.Messages)-1].Parts[0].Text
	answer := "I cannot say."
	for word, a := range m.answers {
		if strings.Contains(prompt, word) {
			answer = a
		}
	}
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: answer}}, StopReason: ports.StopEnd,
		Usage: ports.ModelUsage{Input: 20, Output: 2}, Model: r.Model, Version: m.version}, nil
}
func (m *evalModel) Stream(ctx context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	resp, err := m.Call(ctx, r)
	if err == nil {
		err = each(ports.ModelEvent{Text: resp.Parts[0].Text})
	}
	return resp, err
}
func (m *evalModel) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}

// chatSuite is a fixture of the chat agent's evals: both cases must pass.
var chatSuite = fstest.MapFS{"chat/suite.json": {Data: []byte(`{"id": "chat-fixture", "agent": "chat", "version": 1,
  "description": "A fixture of the tests: synthetic text only.", "threshold": "1", "cases": [
  {"id": "greets", "prompt": "Say hello.", "max_tokens": 16, "expect": {"contains": "hello"}},
  {"id": "sums", "prompt": "What is 2 plus 2? Reply with the number only.", "max_tokens": 16, "expect": {"equals": "4"}}]}`)}}

// A model serves an agent only after passing that agent's evals on its connection (AI-78, AI-87, CMP-AI-03): the run
// is the suite's synthetic cases, budgeted and recorded as the eval runner's, its threshold deciding the result, and
// it is kept with the version the provider reported. A route change that names a model without a current pass is
// refused, and one that passes carries the run on its approval request; presets pin only a qualified model. Another
// version answering puts the pass out of date: the step then carries no personal data or payroll and shows it, until
// the daily job qualifies it again. The shipped probe suite qualifies the probe agent.
// req: AI-78
// req: AI-87
// req: CMP-AI-03
func TestModelsServeOnlyOnceQualified(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	sg := routable(e, "Qualify Singapore", "SG", []string{"gpt-6", "gpt-7"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		return qualifiedIn(ctx, u, sg, "gpt-7", "chat", false, e.clock.Now()) // its latest run of chat's evals failed
	})
	model := &evalModel{answers: map[string]string{"hello": "Hello there.", "2 plus 2": "5"}, version: "gpt-7-20261101"}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	e.api.deps.EvalSuites = chatSuite
	qualify := func(m, agent string) response {
		return e.call(http.MethodPost, "/v1/ai/connections/"+sg.String()+"/models/qualify", admin, map[string]any{"model": m, "agent": agent})
	}
	standing := func(m, agent string) map[string]any {
		got := e.call(http.MethodGet, "/v1/ai/connections/"+sg.String()+"/models", admin, nil)
		for _, x := range got.Body["models"].([]any) {
			if x.(map[string]any)["model"] != m {
				continue
			}
			for _, q := range x.(map[string]any)["qualifications"].([]any) {
				if q.(map[string]any)["agent"] == agent {
					return q.(map[string]any)
				}
			}
		}
		t.Fatalf("no run of %s for %s: %s", m, agent, got.Raw)
		return nil
	}

	// A wrong answer fails the run: recorded, and the model serves chat nothing.
	r := qualify("gpt-7", "chat")
	if r.Status != http.StatusOK || r.str("result") != "fail" || r.str("standing") != "unqualified" || r.Body["passed"] != float64(1) ||
		r.Body["failed"].([]any)[0] != "sums" || r.str("version") != "gpt-7-20261101" || r.str("suite") != "chat-fixture" {
		t.Fatalf("a failing run: %d %s", r.Status, r.Raw)
	}
	step := map[string]any{"connection_id": sg.String(), "model": "gpt-7", "cache": true}
	set := func(match string, classes []string, steps ...map[string]any) response {
		return e.call(http.MethodPut, "/v1/ai/routes/chat", steppedUp(e, admin), map[string]any{"classes": classes, "steps": steps}, "If-Match", match)
	}
	e.problemIs(set(`"0"`, []string{"business-figures"}, step), "setAIRoute", 422, aiqual.CodeUnqualified)

	// Answered right, it passes; the route then waits for a second approver, carrying the run.
	model.answers["2 plus 2"] = "4"
	if r := qualify("gpt-7", "chat"); r.Status != http.StatusOK || r.str("result") != "pass" || r.str("standing") != "qualified" ||
		len(r.Body["failed"].([]any)) != 0 {
		t.Fatalf("a passing run: %d %s", r.Status, r.Raw)
	}
	if q := standing("gpt-7", "chat"); q["standing"] != "qualified" || q["passed"] != float64(2) || q["threshold"] != "1" {
		t.Errorf("the models list: %v", q)
	}
	asked := set(`"0"`, []string{"business-figures", "personal-data"}, step)
	approvalBody, _ := asked.Body["approval"].(map[string]any)
	if asked.Status != http.StatusAccepted || approvalBody == nil || !strings.Contains(asked.Raw, `"step 1 evals"`) ||
		!strings.Contains(asked.Raw, "passed 2 of 2 (chat-fixture v1)") {
		t.Fatalf("a qualified route: %d %s", asked.Status, asked.Raw)
	}
	req := response{Status: http.StatusAccepted, Body: approvalBody}
	if d := f.decide(owner, req.str("id"), "approve", hashOf(req)); d.Status != http.StatusOK {
		t.Fatalf("approving it: %d %s", d.Status, d.Raw)
	}
	// The eval runner's calls are budgeted and recorded under the run, outside any route.
	if u := e.call(http.MethodGet, "/v1/ai/usage", admin, nil); !strings.Contains(u.Raw, `"agent":"eval_runner"`) {
		t.Errorf("the eval runner in the usage: %s", u.Raw)
	}

	// Refusals: an agent without a suite, one that does not exist, a model the provider does not list.
	e.problemIs(qualify("gpt-7", "report_commentary"), "qualifyAIConnectionModel", 422, aiqual.CodeNoSuite)
	e.problemIs(qualify("gpt-7", "dreamer"), "qualifyAIConnectionModel", 404, CodeNotFound)
	e.problemIs(qualify("gpt-9", "chat"), "qualifyAIConnectionModel", 404, CodeNotFound)

	// Another version answering puts the pass out of date (AI-87): personal data no longer goes there, the rest does.
	router := ai.Router{Store: e.api.deps.Store, Clock: e.clock, Outbox: e.api.deps.Outbox,
		Provider: func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
			return model, nil
		}}
	ask := func(class string) (ai.Result, error) {
		return router.Run(context.Background(), e.books.Org, ai.Call{Agent: "chat", Classes: []string{class}, Actor: "user:" + f.ahkow,
			Request: ports.ModelRequest{MaxTokens: 64, Messages: []ports.ModelMessage{{Role: ports.RoleUser,
				Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "What was revenue?"}}}}}})
	}
	if _, err := ask("personal-data"); err != nil {
		t.Fatalf("on the qualified version: %v", err)
	}
	if q := standing("gpt-7", "chat"); q["standing"] != "qualified" {
		t.Fatalf("the same version: %v", q)
	}
	model.version = "gpt-7-20261115"
	if _, err := ask("business-figures"); err != nil {
		t.Fatalf("a call on another version: %v", err)
	}
	if q := standing("gpt-7", "chat"); q["standing"] != "out_of_date" || !strings.Contains(q["out_of_date_why"].(string), "gpt-7-20261115") {
		t.Fatalf("out of date: %v", q)
	}
	res, err := ask("personal-data")
	var stop *airoute.Error
	if !errors.As(err, &stop) || stop.Code != airoute.CodeNoStep || res.Attempts[0].Outcome != "skipped" ||
		!strings.Contains(res.Attempts[0].Detail, "out of date") {
		t.Fatalf("personal data on an out-of-date step: %v %+v", err, res.Attempts)
	}
	if _, err := ask("business-figures"); err != nil {
		t.Errorf("business figures on an out-of-date step: %v", err)
	}
	route := e.call(http.MethodGet, "/v1/ai/routes/chat", admin, nil)
	if s := route.Body["route"].(map[string]any)["steps"].([]any)[0].(map[string]any); s["qualification"] != "out_of_date" || s["serves"] != true {
		t.Errorf("the route's ⚠: %s", route.Raw)
	}

	// The daily job qualifies it again on the new version.
	if err := (ai.Qualifier{Router: router, Suites: chatSuite}).Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q := standing("gpt-7", "chat"); q["standing"] != "qualified" || q["version"] != "gpt-7-20261115" || q["ran_by"] != "system:ai.qualify" {
		t.Errorf("qualified again: %v", q)
	}
	if _, err := ask("personal-data"); err != nil {
		t.Errorf("personal data once qualified again: %v", err)
	}

	// The release's own suite qualifies the probe agent.
	e.api.deps.EvalSuites = nil
	model.answers = map[string]string{"single word OK": "OK", "17 plus 25": "42", "READY": "READY"}
	if r := qualify("gpt-6", ai.ProbeAgent); r.Status != http.StatusOK || r.str("result") != "pass" || r.str("suite") != "probe" || r.Body["cases"] != float64(3) {
		t.Errorf("the probe suite: %d %s", r.Status, r.Raw)
	}
}

// A provider that does not answer stops the run and records nothing: a busy provider is not the model failing.
// req: AI-78
func TestAnUnansweredRunRecordsNothing(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin := f.as(f.ahkow, authz.Owner, authz.Administrator)
	sg := routable(e, "Busy Singapore", "SG", []string{"gpt-6"})
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return &stepModel{caps: full, fail: ports.ErrModelBusy}, nil
	}
	e.api.deps.EvalSuites = chatSuite
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections/"+sg.String()+"/models/qualify", admin, map[string]any{"model": "gpt-6", "agent": "chat"}),
		"qualifyAIConnectionModel", 502, aiqual.CodeUnanswered)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIQualifications().Latest(ctx, sg)
		for _, q := range all {
			if q.Agent == "chat" && q.RanBy != "user:routes" {
				t.Errorf("an unanswered run was recorded: %+v", q)
			}
		}
		return err
	})
}

// listingModel is an evalModel whose provider lists models.
type listingModel struct {
	*evalModel
	list []ports.ListedModel
}

func (m listingModel) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{Models: m.list}, nil
}

// Where a connection qualifies new models automatically (AI-81, off by default), a model its provider lists for the
// first time is queued for the evals of every agent whose route uses that connection, and runs as the system within
// the eval runner's budget; its result appears on the connection, and it joins no allow-list or route by itself. A
// connection where it is off queues nothing.
// req: AI-81
// req: AI-78
func TestNewModelsQualifyAutomaticallyWhereItIsOn(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin := f.as(f.ahkow, authz.Owner, authz.Administrator)
	on := routableOn(e, storage.AIConnection{Name: "Auto on", Connector: "openai", Auth: "api-key", Configuration: []byte(`{}`),
		Host: "api.openai.com", AutoQualify: true}, "SG", []string{"gpt-6"})
	off := routable(e, "Auto off", "SG", []string{"gpt-6"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures"}, MadeBy: "user:auto",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{Position: 1, ConnectionID: on, Model: "gpt-6"}, {Position: 2, ConnectionID: off, Model: "gpt-6"}}})
		return err
	})
	model := listingModel{evalModel: &evalModel{answers: map[string]string{"hello": "Hello.", "2 plus 2": "4"}, version: "gpt-8-20261114"},
		list: []ports.ListedModel{{ID: "gpt-6"}, {ID: "gpt-8"}}}
	provider := func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	worker := workerStore(t)
	lister := ai.Lister{Store: worker, Provider: provider, Clock: e.clock, Outbox: e.api.deps.Outbox}
	qualifier := ai.Qualifier{Router: ai.Router{Store: worker, Provider: provider, Clock: e.clock, Outbox: e.api.deps.Outbox}, Suites: chatSuite}
	for _, c := range []uuid.UUID{on, off} {
		if _, _, err := lister.Read(context.Background(), e.books.Org, c, "refresh", "user:auto"); err != nil {
			t.Fatal(err)
		}
	}
	noop := func(context.Context, uuid.UUID, storage.OutboxRow) error { return nil }
	e.work(map[string]outbox.Work{ai.QualifyKind: qualifier.Work(), ai.PriceKind: noop, notify.FanoutKind: noop})

	gpt8 := func(c uuid.UUID) map[string]any {
		got := e.call(http.MethodGet, "/v1/ai/connections/"+c.String()+"/models", admin, nil)
		for _, x := range got.Body["models"].([]any) {
			if x.(map[string]any)["model"] == "gpt-8" {
				return x.(map[string]any)
			}
		}
		t.Fatalf("gpt-8 is not listed: %s", got.Raw)
		return nil
	}
	m := gpt8(on)
	qs, _ := m["qualifications"].([]any)
	if len(qs) != 1 || qs[0].(map[string]any)["agent"] != "chat" || qs[0].(map[string]any)["result"] != "pass" ||
		qs[0].(map[string]any)["ran_by"] != "system:"+ai.QualifyJob || m["allowed"] != false {
		t.Errorf("a new model where it is on: %v", m)
	}
	if m := gpt8(off); len(m["qualifications"].([]any)) != 0 {
		t.Errorf("a new model where it is off: %v", m)
	}
	if u := e.call(http.MethodGet, "/v1/ai/usage", admin, nil); !strings.Contains(u.Raw, `"agent":"eval_runner"`) {
		t.Errorf("the eval runner's calls in the usage: %s", u.Raw)
	}
}
