//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The transactional outbox end to end (p0-outbox-background-work).

func outboxRow(t *testing.T, org, id uuid.UUID) storage.OutboxRow {
	t.Helper()
	var row storage.OutboxRow
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		row, err = u.Outbox().Get(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return row
}

// req: Transactional outbox
// req: Outbox row contract
func TestOutboxRowsCommitWithTheirChangeOrNotAtAll(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	b := books(t, "Outbox Pte Ltd")
	q := local.NewMemory()
	d := outbox.Dispatcher{Store: s, Queue: q, Clock: clock.NewFrozen(time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC))}

	err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "5100", Name: "Notified", Type: storage.Expense}); err != nil {
			return err
		}
		if _, err := d.Enqueue(ctx, u, b.Org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.notify"}); err != nil {
			return err
		}
		return errors.New("the command fails after writing its work")
	})
	if err == nil {
		t.Fatal("the failing command committed")
	}
	if _, err := q.Next(ctx, "worker"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("a task was sent for a command that rolled back (a phantom job)")
	}
	if n := count(t, b.Org, "outbox"); n != 0 {
		t.Fatalf("%d outbox rows after the rollback", n)
	}

	var row storage.OutboxRow
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		row, err = d.Enqueue(ctx, u, b.Org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.notify",
			Payload: []byte(`{"to":"finance@example.com"}`), BusinessKey: "notify-1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	task, err := q.Next(ctx, "worker")
	if err != nil || task.Name != outbox.TaskName(row.ID, 1) {
		t.Fatalf("task after commit = %+v, %v; want %s", task, err, outbox.TaskName(row.ID, 1))
	}
	var m outbox.Message
	if err := json.Unmarshal(task.Payload, &m); err != nil || m.OrganisationID != b.Org || m.OutboxID != row.ID {
		t.Fatalf("task payload %s", task.Payload)
	}
	got := outboxRow(t, b.Org, row.ID)
	if got.State != storage.OutboxDispatched || got.Dispatches != 1 || got.TaskName != task.Name || got.Deadline == nil ||
		!got.Deadline.Equal(time.Date(2026, 10, 15, 8, 15, 0, 0, time.UTC)) || got.MaxAttempts != 5 || got.BusinessKey != "notify-1" {
		t.Fatalf("the dispatched row = %+v", got)
	}
	// The same work is never queued twice.
	err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := d.Enqueue(ctx, u, b.Org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.notify", BusinessKey: "notify-1"})
		return err
	})
	if storage.CodeOf(err) != storage.CodeDuplicate {
		t.Fatalf("a second row with the same business key: %v", err)
	}
	// The row's identity and the contract's order are enforced by the database.
	for name, stmt := range map[string]string{
		"change its kind":          "UPDATE fyniq.outbox SET kind = 'test.other' WHERE id = $1",
		"go back to pending":       "UPDATE fyniq.outbox SET state = 'pending' WHERE id = $1",
		"count attempts backwards": "UPDATE fyniq.outbox SET attempts = attempts - 1 WHERE id = $1",
		"delete it":                "DELETE FROM fyniq.outbox WHERE id = $1",
	} {
		err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, row.ID); return err })
		wantState(t, name, err, "FY070", "FY071", "42501", "23514")
	}
}

// harness is a local outbox: a push dispatcher delivering over HTTP to a worker and an executor
// handler, with local task tokens.
type harness struct {
	store    storage.Store
	clock    *clock.Frozen
	tokens   *local.HMACTokens
	push     *local.Push
	disp     outbox.Dispatcher
	worker   *outbox.Handler
	executor *outbox.Handler
	urls     map[string]string
}

func newHarness(t *testing.T, kinds map[string]outbox.Work) *harness {
	t.Helper()
	h := &harness{clock: clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)), urls: map[string]string{}}
	h.tokens = local.NewHMACTokens([]byte("local-only-test-key"), h.clock, time.Hour)
	h.worker = &outbox.Handler{Class: storage.ClassWorker, Store: store(t, "fyniq_worker"), Tokens: h.tokens, Clock: h.clock, Holder: "worker-1", Kinds: kinds}
	h.executor = &outbox.Handler{Class: storage.ClassExecutor, Store: store(t, "fyniq_executor"), Tokens: h.tokens, Clock: h.clock, Holder: "executor-1", Kinds: kinds}
	for class, handler := range map[string]http.Handler{"worker": h.worker, "executor": h.executor} {
		mux := http.NewServeMux()
		mux.Handle("/tasks", handler)
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		h.urls[class] = srv.URL
	}
	h.push = local.NewPush(h.urls, h.tokens, 4, time.Millisecond)
	h.store = store(t, "fyniq_api")
	h.disp = outbox.Dispatcher{Store: h.store, Queue: h.push, Clock: h.clock}
	return h
}

func (h *harness) enqueue(t *testing.T, org uuid.UUID, item storage.NewOutboxItem) storage.OutboxRow {
	t.Helper()
	var row storage.OutboxRow
	if err := h.store.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		row, err = h.disp.Enqueue(ctx, u, org, item)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h.push.Wait()
	return row
}

// deliver posts a task directly, as a duplicate delivery from the queue would.
func (h *harness) deliver(t *testing.T, class, token string, org, id uuid.UUID) int {
	t.Helper()
	body, _ := json.Marshal(outbox.Message{OrganisationID: org, OutboxID: id})
	req, _ := http.NewRequest(http.MethodPost, h.urls[class]+"/tasks", bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// req: Idempotent handlers and worker classes
// req: Posting runs at most once
func TestADeliveredTwiceTaskHasOneEffect(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Twice delivered Pte Ltd")
	var runs atomic.Int32
	post := func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		runs.Add(1)
		// The posting identity comes from the outbox row: a second run cannot post again.
		return outbox.Once(store(t, "fyniq_worker").Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			e := porttest.Sale("outbox:"+row.ID.String(), porttest.Day(10, 15), b, "40.00")
			_, err := u.Ledger().Append(ctx, e)
			return err
		}))
	}
	h := newHarness(t, map[string]outbox.Work{"test.post": post})
	row := h.enqueue(t, b.Org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.post"})
	if got := outboxRow(t, b.Org, row.ID); got.State != storage.OutboxDone || got.Attempts != 1 {
		t.Fatalf("after delivery: %+v", got)
	}
	// Duplicate deliveries, some at the same time: the row is done, so nothing runs again.
	token, _ := h.tokens.Token("worker")
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code := h.deliver(t, "worker", token, b.Org, row.ID); code != http.StatusOK {
				t.Errorf("a duplicate delivery answered %d; want 200 so the queue stops", code)
			}
		}()
	}
	wg.Wait()
	if runs.Load() != 1 || count(t, b.Org, "journal_entry") != 1 {
		t.Fatalf("%d runs and %d entries; want one of each", runs.Load(), count(t, b.Org, "journal_entry"))
	}

	// A run that overran its lease and ran again (the sweep re-dispatched it) still posts once.
	r3 := h.enqueue(t, b.Org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.post"})
	if err := post(ctx, b.Org, r3); err != nil { // the second run of the same row
		t.Fatalf("a repeated run: %v", err)
	}
	if n := count(t, b.Org, "journal_entry"); n != 2 {
		t.Fatalf("%d entries after two rows (one run twice); want 2", n)
	}
}

// req: Proposal executor queue isolation
// req: Idempotent handlers and worker classes
func TestTheExecutorRunsOnlyItsOwnTasks(t *testing.T) {
	b := books(t, "Executor isolation Pte Ltd")
	var ran atomic.Int32
	h := newHarness(t, map[string]outbox.Work{"proposal.execute": func(context.Context, uuid.UUID, storage.OutboxRow) error {
		ran.Add(1)
		return nil
	}})
	var row storage.OutboxRow
	if err := h.store.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		row, err = u.Outbox().Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassExecutor, Kind: "proposal.execute"})
		if err == nil {
			err = u.Outbox().MarkDispatched(ctx, row.ID, outbox.TaskName(row.ID, 1), h.clock.Now().Add(time.Hour))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	workerToken, _ := h.tokens.Token("worker")
	jobsToken, _ := h.tokens.Token("jobs")
	for name, token := range map[string]string{"a worker's token": workerToken, "a jobs token": jobsToken, "no token": "", "a forged token": "executor.9999999999.forged"} {
		if code := h.deliver(t, "executor", token, b.Org, row.ID); code != http.StatusForbidden {
			t.Errorf("the executor answered %d to %s; want 403", code, name)
		}
	}
	if ran.Load() != 0 {
		t.Fatal("the executor ran work sent with another class's credential")
	}
	executorToken, _ := h.tokens.Token("executor")
	if code := h.deliver(t, "executor", executorToken, b.Org, row.ID); code != http.StatusOK || ran.Load() != 1 {
		t.Fatalf("with its own token: %d, %d runs", code, ran.Load())
	}
	// A row for the executor delivered to the worker class is refused and parked, never run.
	var stray storage.OutboxRow
	_ = h.store.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		stray, err = u.Outbox().Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassExecutor, Kind: "proposal.execute"})
		if err == nil {
			err = u.Outbox().MarkDispatched(ctx, stray.ID, outbox.TaskName(stray.ID, 1), h.clock.Now().Add(time.Hour))
		}
		return err
	})
	if code := h.deliver(t, "worker", workerToken, b.Org, stray.ID); code != http.StatusOK || ran.Load() != 1 {
		t.Fatalf("an executor row delivered to the worker: %d, %d runs", code, ran.Load())
	}
	if got := outboxRow(t, b.Org, stray.ID); got.State != storage.OutboxDead {
		t.Fatalf("the stray row is %s; want dead", got.State)
	}
}

// req: Outbox row contract
// req: Unknown outcomes of outside calls
func TestFailuresRetryThenDieAndUnknownOutcomesWait(t *testing.T) {
	b := books(t, "Failures Pte Ltd")
	var failing, unknown atomic.Int32
	h := newHarness(t, map[string]outbox.Work{
		"test.fail": func(context.Context, uuid.UUID, storage.OutboxRow) error {
			failing.Add(1)
			return errors.New("the provider answered 503")
		},
		"test.unknown": func(context.Context, uuid.UUID, storage.OutboxRow) error {
			unknown.Add(1)
			return outbox.ErrUnknownOutcome
		},
	})
	dead := h.enqueue(t, b.Org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.fail", MaxAttempts: 3})
	if got := outboxRow(t, b.Org, dead.ID); got.State != storage.OutboxDead || got.Attempts != 3 || got.LastError != "the provider answered 503" {
		t.Fatalf("after the retries ran out: %+v (%d runs)", got, failing.Load())
	}
	if failing.Load() != 3 {
		t.Fatalf("%d runs; want 3, the row's attempts", failing.Load())
	}
	// The dead row is in the audit trail, for Checks (C.2 #12).
	stored := events(t, b.Org)
	var last audit.Event
	_ = json.Unmarshal([]byte(stored[len(stored)-1].Body), &last)
	if last.Action != "outbox.row.dead" || last.Object.ID != dead.ID {
		t.Fatalf("the last event is %s about %s", last.Action, last.Object.ID)
	}

	wait := h.enqueue(t, b.Org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "test.unknown"})
	if got := outboxRow(t, b.Org, wait.ID); got.State != storage.OutboxUnknownOutcome || unknown.Load() != 1 {
		t.Fatalf("an unknown outcome: %+v after %d runs; want unknown_outcome after one", got, unknown.Load())
	}
	token, _ := h.tokens.Token("worker")
	if code := h.deliver(t, "worker", token, b.Org, wait.ID); code != http.StatusOK || unknown.Load() != 1 {
		t.Fatalf("a redelivery of an unknown outcome ran it again (%d runs)", unknown.Load())
	}
}
