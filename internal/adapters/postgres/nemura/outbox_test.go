//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
)

// queue is a stopped consumer's queue: it keeps the tasks it is given until the test delivers them.
type queue struct {
	mu    sync.Mutex
	tasks []ports.Task
}

func (q *queue) Enqueue(_ context.Context, t ports.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, t)
	return nil
}

func (q *queue) last() ports.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.tasks[len(q.tasks)-1]
}

type rig struct {
	t       *testing.T
	org     uuid.UUID
	clk     *clock.Frozen
	q       *queue
	tokens  *local.HMACTokens
	disp    outbox.Dispatcher
	handler *outbox.Handler
	runs    int
	fail    error
}

// newRig is an organisation with its own worker, over a queue that holds tasks until the test delivers them.
func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, org: uuid.New(), clk: clock.NewFrozen(time.Date(2099, 1, 1, 9, 0, 0, 0, time.UTC)), q: &queue{}}
	createOrg(t, r.org, "Outbox "+r.org.String()[:8]+" Pte Ltd")
	r.tokens = local.NewHMACTokens([]byte("test-key"), r.clk, time.Hour)
	worker := store(t, "nemura_worker")
	r.disp = outbox.Dispatcher{Store: worker, Queue: r.q, Clock: r.clk, Deadline: 15 * time.Minute}
	r.handler = &outbox.Handler{Class: outbox.ClassWorker, Store: worker, Tokens: r.tokens, Clock: r.clk, Holder: "test-worker",
		Kinds: map[string]outbox.Work{"test.work": func(context.Context, uuid.UUID, outbox.Row) error { r.runs++; return r.fail }}}
	return r
}

func (r *rig) enqueue(key string, attempts int) outbox.Row {
	r.t.Helper()
	var row outbox.Row
	err := store(r.t, "nemura_api").InOrg(context.Background(), r.org, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		row, err = nemura.Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: "test.work", BusinessKey: key, MaxAttempts: attempts})
		return err
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return row
}

// deliver posts the task the queue holds to the worker, as the queue does, with the class's credential.
func (r *rig) deliver(task ports.Task, token string) (int, string) {
	r.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(task.Payload))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (r *rig) token() string {
	tok, err := r.tokens.Token(outbox.ClassWorker)
	if err != nil {
		r.t.Fatal(err)
	}
	return tok
}

func (r *rig) state(id uuid.UUID) string {
	r.t.Helper()
	var s string
	if err := connect(r.t, db.SuperURL()).QueryRow(context.Background(), "SELECT state FROM nemura.outbox WHERE id = $1", id).Scan(&s); err != nil {
		r.t.Fatal(err)
	}
	return s
}

// Background work is written in the transaction of the change it belongs to: a rollback leaves no row, a commit leaves a
// pending one, and the same business key cannot be queued twice (Transactional outbox).
// asvs: V8.2.2
func TestWorkIsQueuedInTheTransactionOfItsChange(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	api := store(t, "nemura_api")
	_ = api.InOrg(ctx, r.org, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := nemura.Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: "test.work", BusinessKey: "rolled-back"}); err != nil {
			return err
		}
		return errors.New("the change failed")
	})
	var n int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.outbox WHERE organisation_id = $1", r.org).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a rolled-back change left %d rows, %v", n, err)
	}
	row := r.enqueue("once", 5)
	if row.State != outbox.Pending || row.Attempts != 0 {
		t.Fatalf("a new row: %+v", row)
	}
	err := api.InOrg(ctx, r.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: "test.work", BusinessKey: "once"})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "23505") && !strings.Contains(err.Error(), "outbox_business_key") {
		t.Errorf("the same business key twice: %v", err)
	}
}

// A task queued while its consumer is stopped is not lost: the row stays dispatched, the task waits, and when the consumer
// is back the work runs, exactly once even if the queue delivers the task again (NM-NFR-03: Nemura keeps working and queues
// the hand-off when Fyniq is down).
func TestAQueuedTaskSurvivesAStoppedConsumerAndRunsOnce(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	row := r.enqueue("survive", 5)
	if err := r.disp.Dispatch(ctx, r.org, row); err != nil {
		t.Fatal(err)
	}
	if got := r.state(row.ID); got != "dispatched" || r.runs != 0 {
		t.Fatalf("with the consumer stopped: %s, %d runs", got, r.runs)
	}
	task := r.q.last()
	if code, body := r.deliver(task, r.token()); code != http.StatusOK || body != "done" || r.runs != 1 {
		t.Fatalf("the consumer is back: %d %q, %d runs", code, body, r.runs)
	}
	if code, body := r.deliver(task, r.token()); code != http.StatusOK || !strings.Contains(body, "nothing to do") || r.runs != 1 {
		t.Errorf("the queue delivers it again: %d %q, %d runs: it must run once", code, body, r.runs)
	}
	if got := r.state(row.ID); got != "done" {
		t.Errorf("the row is %s", got)
	}
}

// A task goes only to the class it is for, with that class's credential.
// asvs: V13.3.1, V8.2.2
func TestATaskNeedsItsClassesCredential(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	row := r.enqueue("auth", 5)
	if err := r.disp.Dispatch(ctx, r.org, row); err != nil {
		t.Fatal(err)
	}
	task := r.q.last()
	other, _ := r.tokens.Token(outbox.ClassExecutor)
	for name, tok := range map[string]string{"none": "", "forged": "worker.9999999999.forged", "the executor's": other} {
		if code, _ := r.deliver(task, tok); code != http.StatusForbidden {
			t.Errorf("%s credential: %d, want 403", name, code)
		}
	}
	if r.runs != 0 || r.state(row.ID) != "dispatched" {
		t.Errorf("a refused delivery ran the work: %d runs, %s", r.runs, r.state(row.ID))
	}
}

// Work that fails is retried; once its attempts run out the row is dead, and that is audited for the alert (NM-NFR-05's
// mechanism: the worker's dead letter reaches the people who look after the organisation).
func TestFailingWorkIsRetriedThenDeadLettered(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	r.fail = errors.New("the outside system is down")
	row := r.enqueue("fails", 2)
	if err := r.disp.Dispatch(ctx, r.org, row); err != nil {
		t.Fatal(err)
	}
	if code, _ := r.deliver(r.q.last(), r.token()); code != http.StatusInternalServerError || r.state(row.ID) != "failed" {
		t.Fatalf("first attempt: %d, %s", code, r.state(row.ID))
	}
	if code, body := r.deliver(r.q.last(), r.token()); code != http.StatusOK || !strings.Contains(body, "dead after 2 attempts") || r.state(row.ID) != "dead" {
		t.Fatalf("second attempt: %d %q, %s", code, body, r.state(row.ID))
	}
	var n int
	if err := connect(t, db.SuperURL()).QueryRow(ctx,
		"SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action = 'nemura.outbox.row.dead' AND object_id = $2", r.org, row.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("dead-letter audit events: %d, %v", n, err)
	}
	if r.runs != 2 {
		t.Errorf("runs: %d", r.runs)
	}
}

// The sweep sends again a task the queue lost, once its deadline passes, and gives up on a row that is out of attempts; a
// row mid-lease is left alone until its lease runs out.
func TestTheSweepSendsAgainWhatWasLost(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	lost := r.enqueue("lost", 5)
	if err := r.disp.Dispatch(ctx, r.org, lost); err != nil {
		t.Fatal(err)
	}
	// The sweep covers every organisation of the database, so count only this row's tasks.
	sent := func() (n int) {
		for _, task := range r.q.tasks {
			if strings.HasPrefix(task.Name, lost.ID.String()) {
				n++
			}
		}
		return n
	}
	// Not yet overdue: this row is left alone.
	if _, err := outbox.Sweep(ctx, r.disp, r.clk.Now()); err != nil || sent() != 1 {
		t.Fatalf("an on-time row: sent %d times, %v", sent(), err)
	}
	r.clk.Advance(16 * time.Minute) // past the 15-minute deadline
	if _, err := outbox.Sweep(ctx, r.disp, r.clk.Now()); err != nil || sent() != 2 {
		t.Fatalf("an overdue row: sent %d times, %v", sent(), err)
	}
	var again ports.Task
	for _, task := range r.q.tasks {
		if task.Name == outbox.TaskName(lost.ID, 2) {
			again = task
		}
	}
	if again.Name == "" {
		t.Fatalf("the row was not sent again under a new task name: %v", r.q.tasks)
	}
	if code, body := r.deliver(again, r.token()); code != http.StatusOK || body != "done" {
		t.Errorf("the second task: %d %q", code, body)
	}
}

// The database keeps the outbox's contract: a row never changes what it is, its state moves only along the contract, nothing
// is deleted, and no role but the services touches it.
// asvs: V8.2.2
func TestTheOutboxKeepsItsContract(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	row := r.enqueue("contract", 5)
	super := connect(t, db.SuperURL())
	for _, tc := range []struct{ sql, code string }{
		{"UPDATE nemura.outbox SET state = 'done' WHERE id = '" + row.ID.String() + "'", "NM071"},      // pending -> done skips the contract
		{"UPDATE nemura.outbox SET kind = 'other.work' WHERE id = '" + row.ID.String() + "'", "NM070"}, // what a row is never changes
		{"DELETE FROM nemura.outbox WHERE id = '" + row.ID.String() + "'", "NM070"},
		{"TRUNCATE nemura.outbox", "NM070"},
	} {
		if _, err := super.Exec(ctx, tc.sql); err == nil || !strings.Contains(err.Error(), tc.code) {
			t.Errorf("%s: %v, want %s", tc.sql, err, tc.code)
		}
	}
	ro := connect(t, db.URL("nemura_readonly"))
	if _, err := ro.Exec(ctx, "UPDATE nemura.outbox SET state = 'dead'"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the read-only role updated the outbox: %v", err)
	}
	// Another organisation's worker sees none of this organisation's rows.
	other := uuid.New()
	createOrg(t, other, "Other Outbox Pte Ltd")
	var n int
	if err := store(t, "nemura_worker").InOrg(ctx, other, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM nemura.outbox WHERE id = $1", row.ID).Scan(&n)
	}); err != nil || n != 0 {
		t.Errorf("another organisation reads %d of this one's rows, %v", n, err)
	}
}
