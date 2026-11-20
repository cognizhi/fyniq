//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// tasks runs fn in a unit of work on b's organisation as role.
func (b runBooks) tasks(t *testing.T, role string, fn func(context.Context, storage.ComplianceTasks) (storage.ComplianceTask, error)) (storage.ComplianceTask, error) {
	t.Helper()
	var out storage.ComplianceTask
	err := store(t, role).Do(signedIn(), b.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = fn(ctx, u.ComplianceTasks())
		return err
	})
	return out, err
}

// aRun is a run of b's agent that has started, for its tasks.
func (b runBooks) aRun(t *testing.T, trigger string) storage.AgentRun {
	t.Helper()
	return b.runs(t, "fyniq_worker", func(ctx context.Context, r storage.AgentRuns) (storage.AgentRun, error) {
		run, _, err := r.Queue(ctx, b.slot(trigger, b.now))
		if err != nil {
			return run, err
		}
		return r.Start(ctx, run.ID, run.Version, b.now)
	})
}

// A finding has one open task (AI-65; design D7): a run raises it, a repeat run updates it, and it clears when its finding
// clears; a person marks one done or dismisses it with a reason; a closed task stays closed, and a finding that returns
// raises a new one.
// req: AI-65
func TestAFindingHasOneOpenTask(t *testing.T) {
	b := newRunBooks(t, "Compliance tasks Pte Ltd")
	run := b.aRun(t, "schedule:monitor")
	due := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	raise := func(key string) (storage.ComplianceTask, error) {
		return b.tasks(t, "fyniq_worker", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
			return r.RaiseForRun(ctx, run.ID, storage.NewComplianceTask{FindingKey: key, Title: "File the GST return", OwnerRole: "owner", DueOn: &due,
				CreatedAt: b.now})
		})
	}
	task, err := raise("calendar:gst_f5:2026-12-31")
	if err != nil || task.Status != storage.TaskOpen || task.Source != "agent_run" || task.Run == nil || *task.Run != run.ID || task.DueOn == nil {
		t.Fatalf("the raised task: %+v %v", task, err)
	}
	if _, err := raise("calendar:gst_f5:2026-12-31"); storage.CodeOf(err) != storage.CodeDuplicate {
		t.Errorf("a second open task for the finding: %v", err)
	}
	updated, err := b.tasks(t, "fyniq_worker", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		open, err := r.OpenFor(ctx, "calendar:gst_f5:2026-12-31")
		if err != nil {
			return open, err
		}
		return r.Update(ctx, open.ID, open.Version, storage.TaskChange{Title: "File the GST return: 7 days left", OwnerRole: "owner", DueOn: &due, Escalated: true})
	})
	if err != nil || !updated.Escalated || updated.Version != 2 {
		t.Fatalf("the updated task: %+v %v", updated, err)
	}
	cleared, err := b.tasks(t, "fyniq_worker", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		return r.Clear(ctx, updated.ID, updated.Version, b.now.Add(time.Hour))
	})
	if err != nil || cleared.Status != storage.TaskCleared || cleared.ClosedBy != nil {
		t.Fatalf("the cleared task: %+v %v", cleared, err)
	}
	again, err := raise("calendar:gst_f5:2026-12-31")
	if err != nil || again.ID == task.ID {
		t.Fatalf("the finding returning: %+v %v", again, err)
	}
	done, err := b.tasks(t, "fyniq_api", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		return r.Close(ctx, again.ID, again.Version, storage.TaskDone, "", b.owner, b.now.Add(2*time.Hour))
	})
	if err != nil || done.Status != storage.TaskDone || done.ClosedBy == nil || *done.ClosedBy != b.owner {
		t.Fatalf("the task done: %+v %v", done, err)
	}
	other, _ := raise("check:c2_36:company-secretary")
	dismissed, err := b.tasks(t, "fyniq_api", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		return r.Close(ctx, other.ID, other.Version, storage.TaskDismissed, "The run was late because of a holiday", b.owner, b.now.Add(time.Hour))
	})
	if err != nil || dismissed.Reason == "" {
		t.Fatalf("the task dismissed: %+v %v", dismissed, err)
	}
	if _, err := b.tasks(t, "fyniq_api", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		list, err := r.List(ctx, storage.ComplianceTaskQuery{Statuses: []string{storage.TaskOpen}, KeyPrefix: "calendar:"})
		if err == nil && len(list) != 0 {
			err = errors.New("an open task is listed")
		}
		return storage.ComplianceTask{}, err
	}); err != nil {
		t.Error(err)
	}
	if _, err := b.tasks(t, "fyniq_api", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		return r.OpenFor(ctx, "calendar:gst_f5:2026-12-31")
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("the open task of a finding with none: %v", err)
	}
}

// A compliance task keeps its course in the database (design D7): the worker raises a run's, the API closes one for a
// person and nothing else, what it is about never changes, closed it stays closed, and it is never removed.
// req: AI-65
// asvs: V8.2.2, V16.3.3
func TestAttackComplianceTasksKeepTheirCourse(t *testing.T) {
	ctx := context.Background()
	b := newRunBooks(t, "Attack compliance tasks Pte Ltd")
	run := b.aRun(t, "schedule:monitor")
	task, err := b.tasks(t, "fyniq_worker", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		return r.RaiseForRun(ctx, run.ID, storage.NewComplianceTask{FindingKey: "calendar:annual_return:2027-07-31", Title: "File the annual return",
			OwnerRole: "owner", CreatedAt: b.now})
	})
	if err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO fyniq.compliance_task (finding_key, title, owner_role, source, run_id, created_at)
	  VALUES ($1, 'x', 'owner', 'agent_run', $2, now())`
	for _, c := range []struct {
		what  string
		role  string
		stmt  string
		args  []any
		state []string
	}{
		{"the API raising a task", "fyniq_api", insert, []any{"calendar:x:1", run.ID}, []string{"42501"}},
		{"the executor raising a run's task", "fyniq_executor", insert, []any{"calendar:x:1", run.ID}, []string{"42501"}},
		{"a key of no shape", "fyniq_worker", insert, []any{"GST due", run.ID}, []string{"23514"}},
		{"a task of a run that is none", "fyniq_worker", insert, []any{"calendar:x:2", uuid.New()}, []string{"23503"}},
		{"a task with no owner", "fyniq_worker", `INSERT INTO fyniq.compliance_task (finding_key, title, source, run_id, created_at)
		  VALUES ('calendar:x:3', 'x', 'agent_run', $1, now())`, []any{run.ID}, []string{"23514"}},
		{"the API rewriting a task", "fyniq_api", `UPDATE fyniq.compliance_task SET title = 'Nothing to do' WHERE id = $1`, []any{task.ID}, []string{"42501"}},
		{"the worker marking a task done", "fyniq_worker", `UPDATE fyniq.compliance_task SET status = 'done', closed_at = now(),
		  version = version + 1 WHERE id = $1`, []any{task.ID}, []string{"42501", "FY351", "23514"}},
		{"the API clearing a task", "fyniq_api", `UPDATE fyniq.compliance_task SET status = 'cleared', closed_at = now(), version = version + 1
		  WHERE id = $1`, []any{task.ID}, []string{"FY351"}},
		{"what a task is about changed", "fyniq_migrator", `UPDATE fyniq.compliance_task SET finding_key = 'calendar:other:1', version = version + 1
		  WHERE id = $1`, []any{task.ID}, []string{"FY351"}},
		{"a change that skips a version", "fyniq_worker", `UPDATE fyniq.compliance_task SET title = 'x', version = version + 2 WHERE id = $1`,
			[]any{task.ID}, []string{"FY351"}},
		{"the worker deleting a task", "fyniq_worker", `DELETE FROM fyniq.compliance_task WHERE id = $1`, []any{task.ID}, []string{"42501"}},
		{"deleting a task", "fyniq_migrator", `DELETE FROM fyniq.compliance_task WHERE id = $1`, []any{task.ID}, []string{"FY351"}},
		{"truncating the tasks", "fyniq_migrator", `TRUNCATE fyniq.compliance_task`, nil, []string{"FY060"}},
		{"the AI reading the tasks", "fyniq_ai", `SELECT count(*) FROM fyniq.compliance_task`, nil, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, c.role), b.org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.state...)
	}
	done, err := b.tasks(t, "fyniq_api", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		return r.Close(ctx, task.ID, task.Version, storage.TaskDone, "", b.owner, b.now.Add(time.Minute))
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ what, role, stmt string }{
		{"a closed task reopened", "fyniq_worker", `UPDATE fyniq.compliance_task SET status = 'open', closed_at = NULL, version = version + 1 WHERE id = $1`},
		{"a closed task rewritten", "fyniq_worker", `UPDATE fyniq.compliance_task SET title = 'Never happened', version = version + 1 WHERE id = $1`},
	} {
		err := asOrg(ctx, conn(t, c.role), b.org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, done.ID); return err })
		wantState(t, c.what, err, "FY351")
	}
}

// The platform's own task (ENG-33, rev 0.611): a pack update waiting is raised by the worker's job with no run and no proposal, source system; the API and the executor cannot raise one
// (the worker's alone, by the guard), a system task with a run is refused by the check, it has one open task per finding like any other, and the worker clears it when its finding clears.
// req: ENG-33
// req: AI-65
// asvs: V4.1.3, V8.2.1
func TestThePlatformRaisesItsOwnTasksAndNobodyElseDoes(t *testing.T) {
	b := newRunBooks(t, "System tasks Pte Ltd")
	raise := func(role string) (storage.ComplianceTask, error) {
		return b.tasks(t, role, func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
			return r.RaiseSystem(ctx, storage.NewComplianceTask{FindingKey: "pack_update:sg-gst:2026.3", Title: "Statutory pack update waiting: sg-gst@2026.3", OwnerRole: "controller", CreatedAt: b.now})
		})
	}
	for _, role := range []string{"fyniq_api", "fyniq_executor"} {
		if _, err := raise(role); err == nil {
			t.Errorf("%s raised a system task", role)
		}
	}
	task, err := raise("fyniq_worker")
	if err != nil || task.Source != "system" || task.Run != nil || task.Status != storage.TaskOpen {
		t.Fatalf("the worker's system task: %+v %v", task, err)
	}
	if _, err := raise("fyniq_worker"); storage.CodeOf(err) != storage.CodeDuplicate {
		t.Errorf("a second open task for the finding: %v", err)
	}
	if _, err := b.tasks(t, "fyniq_worker", func(ctx context.Context, r storage.ComplianceTasks) (storage.ComplianceTask, error) {
		return r.Clear(ctx, task.ID, task.Version, b.now.Add(time.Hour))
	}); err != nil {
		t.Errorf("clearing it: %v", err)
	}
}
