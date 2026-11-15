//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// runBooks is an organisation with an Owner its agent's runs act for.
type runBooks struct {
	org, owner uuid.UUID
	now        time.Time
}

func newRunBooks(t *testing.T, name string) runBooks {
	t.Helper()
	b := books(t, name)
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	return runBooks{org: b.Org, owner: founder(t, store(t, "fyniq_api"), b.Org, 8, now), now: now}
}

func (b runBooks) slot(trigger string, at time.Time) storage.NewAgentRun {
	return storage.NewAgentRun{Agent: "company-secretary", SkillVersion: 1, SkillSHA256: hexSHA("company-secretary@1"), Trigger: trigger,
		SlotAt: at, ActsFor: b.owner, QueuedAt: b.now}
}

// runs runs fn in a unit of work on b's organisation as role.
func (b runBooks) runs(t *testing.T, role string, fn func(context.Context, storage.AgentRuns) (storage.AgentRun, error)) storage.AgentRun {
	t.Helper()
	var out storage.AgentRun
	if err := store(t, role).Do(signedIn(), b.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = fn(ctx, u.AgentRuns())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// An agent's run is queued once per slot, then runs forward once: started, then done with its report, failed or skipped
// with why (AI-65, AI-67; design D3). Each step is audited, and the report records what the run read.
// req: AI-65
// req: AI-67
func TestAgentRunsRunForwardOnce(t *testing.T) {
	b := newRunBooks(t, "Agent runs Pte Ltd")
	slot := b.now.Add(time.Hour).Truncate(time.Minute)
	var first storage.AgentRun
	for i := range 2 {
		var queued bool
		run := b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (out storage.AgentRun, err error) {
			out, queued, err = r.Queue(ctx, b.slot("schedule:monitor", slot))
			return out, err
		})
		if i == 0 {
			first = run
		}
		if run.ID != first.ID || queued != (i == 0) || run.Status != storage.AgentRunQueued {
			t.Fatalf("queueing the slot, time %d: %+v queued %v", i+1, run, queued)
		}
	}
	started := b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		return r.Start(ctx, first.ID, first.Version, b.now.Add(time.Minute))
	})
	report := storage.AgentReport{Fingerprint: hexSHA("inputs"), LedgerRevision: 42, PackVersions: map[string]string{"sg-gst": "sg-gst@2026.1"},
		KBSnapshot: "none", Findings: json.RawMessage(`[{"key":"calendar:gst-f5-2026-q3","status":"due"}]`), Summary: "1 thing needs you this week.",
		ModelUsed: true, Model: "claude-opus-5-5", ModelText: "The GST return is due in 7 days.", Tokens: 1200, ToolCalls: 3}
	done := b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		return r.Finish(ctx, started.ID, started.Version, report, b.now.Add(2*time.Minute))
	})
	if done.Status != storage.AgentRunDone || done.Report == nil || done.Report.LedgerRevision != 42 || done.Report.PackVersions["sg-gst"] != "sg-gst@2026.1" ||
		done.Report.ModelText != report.ModelText || done.FinishedAt == nil {
		t.Fatalf("the finished run: %+v %+v", done, done.Report)
	}
	last := b.runs(t, "fyniq_api", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		return r.LastDone(ctx, "company-secretary")
	})
	if last.ID != done.ID || last.Report.Fingerprint != report.Fingerprint {
		t.Errorf("the last run done: %+v", last)
	}

	// Another slot is skipped with why; a third fails as it runs.
	skipped := b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		run, _, err := r.Queue(ctx, b.slot("schedule:monitor", slot.Add(24*time.Hour)))
		if err != nil {
			return run, err
		}
		return r.Skip(ctx, run.ID, run.Version, "the agent is paused", b.now.Add(time.Minute))
	})
	failed := b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		run, _, err := r.Queue(ctx, b.slot("schedule:cross_check", slot))
		if err == nil {
			run, err = r.Start(ctx, run.ID, run.Version, b.now.Add(time.Minute))
		}
		if err != nil {
			return run, err
		}
		return r.Fail(ctx, run.ID, run.Version, "the model step did not finish", b.now.Add(2*time.Minute))
	})
	if skipped.Status != storage.AgentRunSkipped || skipped.StartedAt != nil || failed.Status != storage.AgentRunFailed || failed.Reason == "" {
		t.Errorf("skipped %+v, failed %+v", skipped, failed)
	}
	b.runs(t, "fyniq_api", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		list, err := r.List(ctx, storage.AgentRunQuery{Agent: "company-secretary", Statuses: []string{storage.AgentRunSkipped, storage.AgentRunFailed}})
		if err == nil && len(list) != 2 {
			err = errors.New("want the skipped and the failed run")
		}
		return storage.AgentRun{}, err
	})
	// A run moved since it was read is a version conflict, and a run of another organisation is not found.
	if err := store(t, "fyniq_worker").Do(signedIn(), b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AgentRuns().Start(ctx, done.ID, started.Version, b.now)
		return err
	}); storage.CodeOf(err) != storage.CodeVersion && storage.CodeOf(err) != storage.CodeAgentRun {
		t.Errorf("starting a run that ended: %v", err)
	}
	other := newRunBooks(t, "Agent runs elsewhere Pte Ltd")
	if err := store(t, "fyniq_api").Do(signedIn(), other.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AgentRuns().Get(ctx, done.ID)
		return err
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another organisation's run: %v", err)
	}
}

// An agent's run keeps its course in the database (design D3): only the worker moves it, only forward, its report
// written once as it ends; what it is never changes, it is never removed, and the AI's role cannot read it.
// req: AI-65
// req: AI-67
// asvs: V8.2.2, V16.3.3
func TestAttackAgentRunsKeepTheirCourse(t *testing.T) {
	ctx := context.Background()
	b := newRunBooks(t, "Attack agent runs Pte Ltd")
	run := b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		run, _, err := r.Queue(ctx, b.slot("schedule:monitor", b.now.Add(time.Hour)))
		return run, err
	})
	insert := `INSERT INTO fyniq.agent_run (agent, skill_version, skill_sha256, trigger, slot_at, acts_for, queued_at)
	  VALUES ('company-secretary', 1, $1, $2, now(), $3, now())`
	for _, c := range []struct {
		what  string
		role  string
		stmt  string
		args  []any
		state []string
	}{
		{"a trigger of no kind", "fyniq_worker", insert, []any{hexSHA("s"), "whenever", b.owner}, []string{"23514"}},
		{"Run now with no one who chose it", "fyniq_api", insert, []any{hexSHA("s"), "run_now", b.owner}, []string{"23514"}},
		{"a run made done", "fyniq_migrator", `INSERT INTO fyniq.agent_run (agent, skill_version, skill_sha256, trigger, slot_at, acts_for,
		  queued_at, status, started_at, finished_at) VALUES ('company-secretary', 1, $1, 'schedule:monitor', now(), $2, now(), 'done', now(), now())`,
			[]any{hexSHA("s"), b.owner}, []string{"FY350", "23514"}},
		{"the API starting a run", "fyniq_api", `UPDATE fyniq.agent_run SET status = 'running', started_at = now(), version = version + 1 WHERE id = $1`,
			[]any{run.ID}, []string{"42501"}},
		{"a report written as it starts", "fyniq_worker", `UPDATE fyniq.agent_run SET status = 'running', started_at = now(), summary = 'All clear',
			version = version + 1 WHERE id = $1`, []any{run.ID}, []string{"FY350"}},
		{"a queued run made done", "fyniq_worker", `UPDATE fyniq.agent_run SET status = 'done', finished_at = now(), version = version + 1 WHERE id = $1`,
			[]any{run.ID}, []string{"FY350", "23514"}},
		{"its trigger changed", "fyniq_migrator", `UPDATE fyniq.agent_run SET trigger = 'run_now', requested_by = acts_for, version = version + 1
			WHERE id = $1`, []any{run.ID}, []string{"FY350"}},
		{"who it acts for changed", "fyniq_worker", `UPDATE fyniq.agent_run SET acts_for = $2 WHERE id = $1`, []any{run.ID, uuid.New()}, []string{"42501"}},
		{"a step that skips a version", "fyniq_worker", `UPDATE fyniq.agent_run SET status = 'skipped', reason = 'x', finished_at = now(),
			version = version + 2 WHERE id = $1`, []any{run.ID}, []string{"FY350"}},
		{"the worker deleting a run", "fyniq_worker", `DELETE FROM fyniq.agent_run WHERE id = $1`, []any{run.ID}, []string{"42501"}},
		{"deleting a run", "fyniq_migrator", `DELETE FROM fyniq.agent_run WHERE id = $1`, []any{run.ID}, []string{"FY350"}},
		{"truncating the runs", "fyniq_migrator", `TRUNCATE fyniq.agent_run`, nil, []string{"FY060"}},
		{"the AI reading the runs", "fyniq_ai", `SELECT count(*) FROM fyniq.agent_run`, nil, []string{"42501"}},
		{"the AI queueing a run", "fyniq_ai", insert, []any{hexSHA("s"), "schedule:monitor", b.owner}, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, c.role), b.org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.state...)
	}

	// Once ended, nothing about it changes.
	ended := b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		return r.Skip(ctx, run.ID, run.Version, "the agent is off", b.now.Add(time.Minute))
	})
	for _, c := range []struct{ what, stmt string }{
		{"a skipped run started", `UPDATE fyniq.agent_run SET status = 'running', reason = '', finished_at = NULL, started_at = now(), version = version + 1 WHERE id = $1`},
		{"its reason rewritten", `UPDATE fyniq.agent_run SET reason = 'nothing to see', version = version + 1 WHERE id = $1`},
	} {
		err := asOrg(ctx, conn(t, "fyniq_worker"), b.org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, ended.ID); return err })
		wantState(t, c.what, err, "FY350")
	}
}
