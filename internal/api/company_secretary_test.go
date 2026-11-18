//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixCompanySecretary are the authorisation matrix's rows for the company secretary assistant's workspace (design D9).
var matrixCompanySecretary = map[string]func(e *env, tok string) (response, func() bool){
	"getCompanySecretary": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/secretary", tok, nil), nil
	},
	"listAgentRuns": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/secretary/runs", tok, nil), nil
	},
	"getAgentRun": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/secretary/runs/"+runningRun(e, matrixApprover(e)).ID.String(), tok, nil), nil
	},
	"runCompanySecretary": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/compliance/secretary/runs", steppedUp(e, tok), nil, "Idempotency-Key", uuid.NewString()), nil
	},
}

// The company secretary assistant's workspace (FR-CS-04 in part, AI-62, AI-63, AI-65, AI-67; design D9): off until an Owner
// turns it on; then it shows who it acts for, its schedule and when it runs next, its latest run and the open findings
// of its latest finished run, Not assessed first. The history marks what is new since the run done before; a run's report
// shows what it read, every finding and the tasks they have. Run now queues a run for the person who asked, never while
// it is off or paused, and the Board viewer reads it all but runs nothing. The AI reaches none of it.
// req: FR-CS-04
// req: AI-62
// req: AI-65
// req: AI-67
// asvs: V8.2.1
func TestTheCompanySecretaryWorkspace(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	ahkow := f.as(f.ahkow)
	if off := e.call(http.MethodGet, "/v1/compliance/secretary", ahkow, nil); off.Status != http.StatusOK || off.str("state") != "off" ||
		off.Body["latest"] != nil || len(off.Body["next_runs"].([]any)) != 0 {
		t.Fatalf("the assistant while off: %d %s", off.Status, off.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/secretary/runs", ahkow, nil), "runCompanySecretary", 409, CodeAgentOff)
	enableSecretary(e, f)

	// A finished run: its findings, Not assessed first, and the open ones in the workspace.
	e.clock.Set(time.Date(2026, 11, 16, 7, 0, 30, 0, clock.Singapore))
	dispatcher := outbox.Dispatcher{Store: workerStore(t), Queue: e.queue, Clock: e.clock}
	runner := agents.Runner{Store: workerStore(t), Dispatcher: dispatcher, Clock: e.clock, Skills: agents.Default(),
		Model: func(context.Context, uuid.UUID, storage.AgentRun, []agents.Finding) (agents.ModelOutcome, error) {
			return agents.ModelOutcome{Model: "test-model", Text: "The corporate profile is not recorded yet.", Tokens: 900, ToolCalls: 1}, nil
		}}
	work := func(r storage.AgentRun) storage.AgentRun {
		t.Helper()
		payload, _ := json.Marshal(agents.RunWork{RunID: r.ID})
		if err := runner.Work(context.Background(), e.books.Org, storage.OutboxRow{ID: uuid.New(), Kind: agents.RunKind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		var out storage.AgentRun
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AgentRuns().Get(ctx, r.ID)
			return err
		})
		return out
	}
	now := e.call(http.MethodPost, "/v1/compliance/secretary/runs", steppedUp(e, ahkow), nil, "Idempotency-Key", uuid.NewString())
	if now.Status != http.StatusAccepted || now.str("trigger") != agents.TriggerRunNow || now.str("status") != storage.AgentRunQueued ||
		now.Header.Get("Location") != "/v1/compliance/secretary/runs/"+now.str("id") {
		t.Fatalf("Run now: %d %s", now.Status, now.Raw)
	}
	var queued storage.AgentRun
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		queued, err = u.AgentRuns().Get(ctx, uuid.MustParse(now.str("id")))
		return err
	})
	if queued.RequestedBy == nil || queued.RequestedBy.String() != f.ahkow || queued.ActsFor.String() != f.ahkow {
		t.Fatalf("the run queued: %+v", queued)
	}
	done := work(queued)
	if done.Status != storage.AgentRunDone {
		t.Fatalf("the run: %+v", done)
	}
	ws := e.call(http.MethodGet, "/v1/compliance/secretary", ahkow, nil)
	findings, _ := ws.Body["findings"].([]any)
	if ws.str("state") != "on" || ws.str("acts_for") != f.ahkow || ws.str("acts_for_name") == "" || len(ws.Body["next_runs"].([]any)) != 2 ||
		ws.Body["last_done"] == nil || len(findings) == 0 || findings[0].(map[string]any)["status"] != agents.FindingNotAssessed {
		t.Fatalf("the workspace: %s", ws.Raw)
	}
	if ai, _ := ws.Body["ai_summary"].(map[string]any); ai["text"] != "The corporate profile is not recorded yet." || ai["origin"] != "ai" {
		t.Errorf("the AI's words in the workspace: %v", ws.Body["ai_summary"])
	}
	schedule := ws.Body["schedule"].(map[string]any)
	if schedule["monitor"] != "0 7 * * *" || schedule["run_tokens"] != float64(200000) {
		t.Errorf("the schedule: %v", schedule)
	}

	// The history: the next day's run finds the same, so it is not new; its report shows what it read.
	e.clock.Set(time.Date(2026, 11, 16, 9, 0, 0, 0, clock.Singapore))
	again := e.call(http.MethodPost, "/v1/compliance/secretary/runs", steppedUp(e, ahkow), nil, "Idempotency-Key", uuid.NewString())
	var second storage.AgentRun
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		second, err = u.AgentRuns().Get(ctx, uuid.MustParse(again.str("id")))
		return err
	})
	work(second)
	runs := e.call(http.MethodGet, "/v1/compliance/secretary/runs?status=done", ahkow, nil)
	items, _ := runs.Body["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["new"] != false || items[1].(map[string]any)["new"] != true {
		t.Fatalf("the history: %s", runs.Raw)
	}
	report := e.call(http.MethodGet, "/v1/compliance/secretary/runs/"+done.ID.String(), ahkow, nil)
	if report.Status != http.StatusOK || len(report.Body["findings"].([]any)) == 0 || report.Body["packs"] == nil || report.Body["model"] == nil {
		t.Fatalf("the report: %d %s", report.Status, report.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/secretary/runs/"+uuid.NewString(), ahkow, nil), "getAgentRun", 404, CodeNotFound)

	// The Board viewer reads it and runs nothing; the AI reaches none of it.
	board := e.as(authz.BoardViewer)
	if r := e.call(http.MethodGet, "/v1/compliance/secretary", board, nil); r.Status != http.StatusOK {
		t.Errorf("the Board viewer reading the workspace: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/compliance/secretary/runs", steppedUp(e, board), nil); r.Status != http.StatusForbidden {
		t.Errorf("the Board viewer's Run now: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/compliance/secretary", proposeTurn(e, f.ahkow), nil); r.Status < 400 {
		t.Errorf("the AI reading the workspace: %d %s", r.Status, r.Raw)
	}

	// Paused, Run now is refused.
	const pause = "/v1/organisation/settings/agents.company_secretary.paused"
	if r := e.call(http.MethodPatch, pause, ahkow, map[string]any{"value": "true"}, "If-Match", e.call(http.MethodGet, pause, ahkow, nil).Header.Get("ETag")); r.Status >= 300 {
		t.Fatalf("pausing: %d %s", r.Status, r.Raw)
	}
	if ws := e.call(http.MethodGet, "/v1/compliance/secretary", ahkow, nil); ws.str("state") != "paused" || len(ws.Body["next_runs"].([]any)) != 0 {
		t.Errorf("the workspace while paused: %s", ws.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/secretary/runs", steppedUp(e, ahkow), nil), "runCompanySecretary", 409, CodeAgentOff)
}
