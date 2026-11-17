//go:build local

package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A run keeps its findings when its AI step cannot run (AI-64, C.2 #36; the owner's decision of 16 Nov 2026, rev 0.283;
// G4-110 in part). On 17 Jan 2027 the model does not answer: the run still ends done, with the GST return due in 14 days,
// its task, and Fyniq's summary saying the AI could not explain this time; its report says why, and C.2 #36 shows the run
// ⚠. On 24 Jan, the window having moved to 7 days, the AI is switched off: the run ends the same way, and the model is
// never asked.
// req: AI-64
// req: C.2 #36
// req: G4-110
func TestARunKeepsItsFindingsWithoutItsAIStep(t *testing.T) {
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
			return agents.ModelOutcome{}, errors.New("the provider did not answer")
		}}
	scheduler := agents.Scheduler{Store: workerStore(t), Dispatcher: dispatcher, Clock: e.clock, Skills: agents.Default()}
	run := func(day time.Time) storage.AgentRun {
		t.Helper()
		e.clock.Set(time.Date(day.Year(), day.Month(), day.Day(), 6, 58, 0, 0, clock.Singapore))
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
		e.clock.Set(slot.Add(30 * time.Second))
		payload, _ := json.Marshal(agents.RunWork{RunID: queued.ID})
		if err := runner.Work(ctx, e.books.Org, storage.OutboxRow{ID: uuid.New(), Kind: agents.RunKind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		var out storage.AgentRun
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AgentRuns().Get(ctx, queued.ID)
			return err
		})
		return out
	}
	gstDue := func(r storage.AgentRun) agents.Finding {
		var list []agents.Finding
		_ = json.Unmarshal(r.Report.Findings, &list)
		for _, x := range list {
			if x.Key == "calendar:gst_f5:2026-12-31" {
				return x
			}
		}
		return agents.Finding{}
	}

	first := run(time.Date(2027, 1, 17, 0, 0, 0, 0, time.UTC))
	if first.Status != storage.AgentRunDone || first.Report == nil || first.Report.ModelUsed || asked != 1 ||
		!strings.Contains(first.Report.ModelMissed, "the AI step could not run: ") || !strings.Contains(first.Report.ModelMissed, "the provider did not answer") ||
		!strings.HasSuffix(first.Report.Summary, "The AI could not add its explanation this time.") || first.Report.ModelText != "" {
		t.Fatalf("the run with the model down: %+v %+v (asked %d)", first, first.Report, asked)
	}
	if g := gstDue(first); g.Status != agents.FindingDue || g.Window != "14" {
		t.Errorf("the GST return's finding: %+v", g)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		tasks, err := u.ComplianceTasks().List(ctx, storage.ComplianceTaskQuery{Statuses: []string{storage.TaskOpen}, KeyPrefix: "calendar:gst_f5:"})
		if err == nil && len(tasks) != 1 {
			t.Errorf("the GST return's task: %+v", tasks)
		}
		return err
	})
	if w := e.checked("agents.runs"); w.Body["status"] != "warning" || !strings.Contains(w.Raw, "ran without its AI step (the AI step could not run: the provider did not answer)") {
		t.Errorf("C.2 #36 after a run without its AI step: %s", w.Raw)
	}

	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, ai.Switch, "true", "false", time.Date(2027, 1, 18, 0, 0, 0, 0, time.UTC), 2)
		return err
	})
	second := run(time.Date(2027, 1, 24, 0, 0, 0, 0, time.UTC))
	if second.Status != storage.AgentRunDone || second.Report == nil || second.Report.ModelMissed != agents.AIOff || second.Report.ModelUsed || asked != 1 {
		t.Fatalf("the run with the AI off: %+v %+v (asked %d)", second, second.Report, asked)
	}
	if g := gstDue(second); g.Status != agents.FindingDue || g.Window != "7" {
		t.Errorf("the GST return's finding a week later: %+v", g)
	}
}
