//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A run of the company secretary's assistant (AI-64, AI-65, AI-66; design D5; G4-076): the deterministic findings first,
// the GST return due in 7 days among them with its rule; one open task for it, escalated to the Owner; the corporate
// profile's checks Not assessed, so the summary is no all clear; the model asked only when an input changed. Run again
// the next day with nothing changed, the task is updated, not doubled, and the model is not asked; with the return
// recorded, the next run clears the task; paused, a run is skipped with why.
// req: AI-64
// req: AI-65
// req: AI-66
// req: G4-076
func TestARunChecksFirstAndKeepsOneTaskPerFinding(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	ctx := context.Background()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), GSTNumber: "M90000001X",
			ReturnFrequency: storage.ReturnQuarterly, QuarterStartMonth: 1, AccountingBasis: storage.BasisInvoice, RecordedAt: e.clock.Now()})
		return err
	})
	enableSecretary(e, f)
	asked := 0
	dispatcher := outbox.Dispatcher{Store: workerStore(t), Queue: e.queue, Clock: e.clock}
	runner := agents.Runner{Store: workerStore(t), Dispatcher: dispatcher, Clock: e.clock, Skills: agents.Default(),
		Model: func(context.Context, uuid.UUID, storage.AgentRun, []agents.Finding) (agents.ModelOutcome, error) {
			asked++
			return agents.ModelOutcome{Model: "test-model", Text: "The GST return for October to December is due in a week.", Tokens: 900, ToolCalls: 2}, nil
		}}
	scheduler := agents.Scheduler{Store: workerStore(t), Dispatcher: dispatcher, Clock: e.clock, Skills: agents.Default()}
	// run queues the day's 07:00 monitor and runs it at its slot.
	run := func(day time.Time) storage.AgentRun {
		t.Helper()
		e.clock.Set(time.Date(day.Year(), day.Month(), day.Day(), 6, 58, 0, 0, clock.Singapore))
		if _, err := scheduler.ScheduleIn(ctx, e.books.Org); err != nil {
			t.Fatal(err)
		}
		var queued storage.AgentRun
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			list, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: "company-secretary", Statuses: []string{storage.AgentRunQueued}})
			for _, r := range list {
				if r.SlotAt.Equal(time.Date(day.Year(), day.Month(), day.Day(), 7, 0, 0, 0, clock.Singapore)) {
					queued = r
				}
			}
			return err
		})
		payload, _ := json.Marshal(agents.RunWork{RunID: queued.ID})
		row := storage.OutboxRow{ID: uuid.New(), Kind: agents.RunKind, Payload: payload}
		if err := runner.Work(ctx, e.books.Org, row); err == nil {
			t.Fatal("a run before its slot ran instead of holding")
		}
		e.clock.Set(time.Date(day.Year(), day.Month(), day.Day(), 7, 0, 30, 0, clock.Singapore))
		if err := runner.Work(ctx, e.books.Org, row); err != nil {
			t.Fatal(err)
		}
		var out storage.AgentRun
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AgentRuns().Get(ctx, queued.ID)
			return err
		})
		return out
	}
	findings := func(r storage.AgentRun) map[string]agents.Finding {
		out := map[string]agents.Finding{}
		var list []agents.Finding
		_ = json.Unmarshal(r.Report.Findings, &list)
		for _, f := range list {
			out[f.Key] = f
		}
		return out
	}
	openTasks := func() []storage.ComplianceTask {
		var out []storage.ComplianceTask
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.ComplianceTasks().List(ctx, storage.ComplianceTaskQuery{Statuses: []string{storage.TaskOpen}, KeyPrefix: "calendar:gst_f5:"})
			return err
		})
		return out
	}

	first := run(time.Date(2027, 1, 24, 0, 0, 0, 0, time.UTC))
	gst := findings(first)["calendar:gst_f5:2026-12-31"]
	if first.Status != storage.AgentRunDone || gst.Status != agents.FindingDue || gst.Window != "7" || gst.DueOn != "2027-01-31" || gst.Rule == "" || gst.Pack == "" {
		t.Fatalf("the first run: %+v %+v", first, gst)
	}
	if na := findings(first)["check:corporate.secretary"]; na.Status != agents.FindingNotAssessed || strings.Contains(strings.ToLower(first.Report.Summary), "all clear") &&
		!strings.Contains(first.Report.Summary, "not an all clear") {
		t.Errorf("the corporate profile's secretary check and the summary: %+v %q", na, first.Report.Summary)
	}
	if !first.Report.ModelUsed || first.Report.ModelText == "" || asked != 1 || first.Report.LedgerRevision < 0 || first.Report.PackVersions["sg-deadlines"] == "" {
		t.Errorf("the model step and the inputs: %+v (asked %d)", first.Report, asked)
	}
	tasks := openTasks()
	if len(tasks) != 1 || !tasks[0].Escalated || tasks[0].OwnerRole != "owner" || gst.Task != tasks[0].ID.String() {
		t.Fatalf("the finding's task: %+v", tasks)
	}

	// The next day, nothing changed but the day: one task, updated; the model is not asked again.
	second := run(time.Date(2027, 1, 25, 0, 0, 0, 0, time.UTC))
	if second.Status != storage.AgentRunDone || second.Report.ModelUsed || asked != 1 || second.Report.Fingerprint != first.Report.Fingerprint {
		t.Errorf("the second run: %+v (asked %d)", second.Report, asked)
	}
	if again := openTasks(); len(again) != 1 || again[0].ID != tasks[0].ID || !strings.Contains(again[0].Detail, "in 6 days") {
		t.Errorf("the task after the second run: %+v", again)
	}

	// The return recorded, the next run clears its task: the finding no longer needs anyone.
	ahkow := f.as(f.ahkow)
	d := e.call(http.MethodPost, "/v1/compliance/filings", ahkow, map[string]any{"key": "gst_f5:2026-12-31", "outcome": "not_required", "reason": "Filed by the tax agent"})
	if r := e.call(http.MethodPost, "/v1/compliance/filings/"+d.str("id")+"/record", ahkow, nil, "If-Match", d.Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("recording the return: %d %s", r.Status, r.Raw)
	}
	if third := run(time.Date(2027, 1, 26, 0, 0, 0, 0, time.UTC)); third.Status != storage.AgentRunDone || findings(third)["calendar:gst_f5:2026-12-31"].Status != agents.FindingOK {
		t.Errorf("the run after the return was recorded: %+v", third)
	}
	if open := openTasks(); len(open) != 0 {
		t.Errorf("the task after its finding cleared: %+v", open)
	}

	// Paused, the next run is skipped with why.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "agents.company_secretary.paused", "false", "true", time.Date(2027, 1, 25, 0, 0, 0, 0, time.UTC), 1)
		return err
	})
	var skipped storage.AgentRun
	ahkowID := uuid.MustParse(f.ahkow)
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		skipped, _, err = u.AgentRuns().Queue(ctx, storage.NewAgentRun{Agent: "company-secretary", SkillVersion: agents.Default().All()[0].Version,
			SkillSHA256: agents.Default().All()[0].SHA256, Trigger: "run_now", SlotAt: e.clock.Now(), ActsFor: uuid.MustParse(f.ahkow),
			RequestedBy: &ahkowID, QueuedAt: e.clock.Now()})
		return err
	})
	payload, _ := json.Marshal(agents.RunWork{RunID: skipped.ID})
	if err := runner.Work(ctx, e.books.Org, storage.OutboxRow{ID: uuid.New(), Kind: agents.RunKind, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		skipped, err = u.AgentRuns().Get(ctx, skipped.ID)
		return err
	})
	if skipped.Status != storage.AgentRunSkipped || skipped.Reason != "the agent is paused" {
		t.Errorf("a run while paused: %+v", skipped)
	}
}
