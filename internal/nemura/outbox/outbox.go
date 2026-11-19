// Package outbox runs Nemura's background work (PRD-001 §12.11, ADR-T7; PRD-002 NM-NFR-03). A command writes its work as an
// outbox row in its own transaction; after commit the Dispatcher sends the row as a task to its worker class; the class's
// Handler takes a lease on the row, runs the work and finishes it. Nothing stays running: the queue (Cloud Tasks, locally the
// push dispatcher) delivers each task as an HTTP request, and the Sweep sends again what never left or overran. The same
// outbox queues the hand-off to Fyniq when Fyniq is down, so Nemura keeps working (NM-NFR-03).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// State is where a row is in its life: pending, dispatched, running, done, failed or dead.
type State string

// The states of a row (the contract of migration 000003).
const (
	Pending    State = "pending"
	Dispatched State = "dispatched"
	Running    State = "running"
	Done       State = "done"
	Failed     State = "failed"
	Dead       State = "dead"
)

// Worker classes: each is a service with its own credential and queue.
const (
	ClassWorker   = "worker"
	ClassExecutor = "executor"
)

// Row is one unit of background work.
type Row struct {
	ID          uuid.UUID
	Class       string
	Kind        string
	Payload     []byte
	BusinessKey string
	State       State
	Attempts    int
	MaxAttempts int
	Dispatches  int
	TaskName    string
	Deadline    *time.Time
	LeaseUntil  *time.Time
}

// NewItem is work to enqueue. A business key makes the work idempotent: a second item with the same kind and key is refused.
type NewItem struct {
	Class       string
	Kind        string
	Payload     []byte
	BusinessKey string
	MaxAttempts int // 0 means 5
}

// Store is the outbox's database, as the Nemura adapter implements it. Each call runs in its own transaction for org.
type Store interface {
	MarkDispatched(ctx context.Context, org, id uuid.UUID, task string, deadline time.Time) error
	// Lease takes the row for holder until until when it is dispatched, failed or its lease ran out, counting an attempt; leased is
	// false when another holder has it or it is finished.
	Lease(ctx context.Context, org, id uuid.UUID, holder string, now, until time.Time) (row Row, leased bool, err error)
	// Finish ends holder's lease with state; a dead row is audited in the same transaction.
	Finish(ctx context.Context, org, id uuid.UUID, holder string, state State, msg string, now time.Time) error
	// Due lists rows pending since before pendingBefore, dispatched or failed past their deadline, or running past their lease.
	Due(ctx context.Context, org uuid.UUID, now, pendingBefore time.Time) ([]Row, error)
	MarkDead(ctx context.Context, org, id uuid.UUID, reason string, now time.Time) error
	// Organisations lists every organisation, for the sweep.
	Organisations(ctx context.Context) ([]uuid.UUID, error)
}

// Message is a task's payload: which row to run.
type Message struct {
	OrganisationID uuid.UUID `json:"organisation_id"`
	OutboxID       uuid.UUID `json:"outbox_id"`
}

// TaskName is the name of a row's nth dispatch. Cloud Tasks remembers names for 24 hours, so the platform's own
// re-dispatches use a new number (§12.11).
func TaskName(id uuid.UUID, dispatch int) string { return fmt.Sprintf("%s-%d", id, dispatch) }

// Dispatcher sends outbox rows to their worker classes after commit.
type Dispatcher struct {
	Store    Store
	Queue    ports.Queue
	Clock    ports.Clock
	Deadline time.Duration // how long a dispatched row may take before the sweep sends it again; 0 means 15 min
	// Raise, when set, runs for every organisation at the end of its sweep and may enqueue rows of its own: the pay-day alert for the jobs
	// that died (internal/nemura/alert). A failure is returned after the other organisations are swept.
	Raise func(ctx context.Context, org uuid.UUID, now time.Time) (int, error)
}

func (d Dispatcher) deadline() time.Duration {
	if d.Deadline > 0 {
		return d.Deadline
	}
	return 15 * time.Minute
}

// Dispatch sends a row as a task under a new name. The row is marked dispatched first, so a task never finds its row still
// pending; if the enqueue then fails the row is overdue at its deadline and the sweep sends it again.
func (d Dispatcher) Dispatch(ctx context.Context, org uuid.UUID, row Row) error {
	name := TaskName(row.ID, row.Dispatches+1)
	if err := d.Store.MarkDispatched(ctx, org, row.ID, name, d.Clock.Now().Add(d.deadline())); err != nil {
		return err
	}
	payload, err := json.Marshal(Message{OrganisationID: org, OutboxID: row.ID})
	if err != nil {
		return err
	}
	return d.Queue.Enqueue(ctx, ports.Task{Queue: row.Class, Name: name, Payload: payload})
}

// Work does one row's work, in its own transaction. It must be idempotent on the row.
type Work func(ctx context.Context, org uuid.UUID, row Row) error

// Handler is a worker class's /tasks endpoint.
type Handler struct {
	Class  string
	Store  Store
	Tokens ports.TaskTokens
	Clock  ports.Clock
	Holder string        // this instance, recorded on the lease
	Lease  time.Duration // how long a lease lasts; 0 means 10 min
	Kinds  map[string]Work
}

// ServeHTTP runs one delivery: check the class's credential, lease the row, run the work, finish. 2xx stops the queue's
// retries; anything else asks for another delivery.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := h.Tokens.Verify(token, h.Class); err != nil {
		http.Error(w, "this worker class does not accept that task's credential", http.StatusForbidden)
		return
	}
	var m Message
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&m); err != nil || m.OrganisationID == uuid.Nil || m.OutboxID == uuid.Nil {
		http.Error(w, "the task is not an outbox message", http.StatusBadRequest)
		return
	}
	status, msg := h.run(r.Context(), m)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg))
}

func (h *Handler) run(ctx context.Context, m Message) (int, string) {
	lease := h.Lease
	if lease <= 0 {
		lease = 10 * time.Minute
	}
	now := h.Clock.Now()
	row, leased, err := h.Store.Lease(ctx, m.OrganisationID, m.OutboxID, h.Holder, now, now.Add(lease))
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return http.StatusOK, "no such outbox row"
	case err != nil:
		return http.StatusInternalServerError, err.Error()
	case !leased && row.State == Running:
		return http.StatusConflict, "another delivery holds the row" // try again later
	case !leased:
		return http.StatusOK, "nothing to do: the row is " + string(row.State)
	case row.Class != h.Class:
		h.finish(ctx, m.OrganisationID, row, Dead, fmt.Sprintf("delivered to %s, but the row is for %s", h.Class, row.Class))
		return http.StatusOK, "wrong worker class"
	}
	work, ok := h.Kinds[row.Kind]
	if !ok {
		err = fmt.Errorf("worker class %s has no handler for %s", h.Class, row.Kind)
	} else {
		err = work(ctx, m.OrganisationID, row)
	}
	switch {
	case err == nil:
		h.finish(ctx, m.OrganisationID, row, Done, "")
		return http.StatusOK, "done"
	case row.Attempts >= row.MaxAttempts:
		h.finish(ctx, m.OrganisationID, row, Dead, err.Error())
		return http.StatusOK, fmt.Sprintf("dead after %d attempts", row.Attempts)
	default:
		h.finish(ctx, m.OrganisationID, row, Failed, err.Error())
		return http.StatusInternalServerError, err.Error()
	}
}

func (h *Handler) finish(ctx context.Context, org uuid.UUID, row Row, state State, msg string) {
	_ = h.Store.Finish(ctx, org, row.ID, h.Holder, state, msg, h.Clock.Now())
}

// Sweep is the tick's part: for every organisation it sends again each row that never left (pending before pendingBefore) or
// overran its deadline or lease, and marks dead a row whose attempts are used up. It returns how many rows it dispatched.
func Sweep(ctx context.Context, d Dispatcher, pendingBefore time.Time) (int, error) {
	orgs, err := d.Store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	var raised error
	for _, org := range orgs {
		now := d.Clock.Now()
		rows, err := d.Store.Due(ctx, org, now, pendingBefore)
		if err != nil {
			return sent, err
		}
		for _, row := range rows {
			if row.Attempts >= row.MaxAttempts {
				if err := d.Store.MarkDead(ctx, org, row.ID, "out of attempts when the sweep found it", now); err != nil {
					return sent, err
				}
				continue
			}
			if err := d.Dispatch(ctx, org, row); err != nil {
				return sent, err
			}
			sent++
		}
		if d.Raise != nil {
			if _, err := d.Raise(ctx, org, d.Clock.Now()); err != nil {
				raised = errors.Join(raised, err)
			}
		}
	}
	return sent, raised
}

// Tick is the scheduler's endpoint: with the worker class's credential it sweeps every organisation once and answers how many
// rows it sent again. Cloud Scheduler calls it every few minutes in the cloud (ADR-T7), `make dev` and the compose stack call it
// from the worker itself; nothing else has to stay running.
type Tick struct {
	Dispatcher Dispatcher
	Tokens     ports.TaskTokens
	Class      string
	Grace      time.Duration // how long a row may stay pending before the sweep sends it; 0 means a minute
}

// ServeHTTP runs one sweep.
func (t Tick) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if err := t.Tokens.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), t.Class); err != nil {
		http.Error(w, "the tick needs the worker class's credential", http.StatusForbidden)
		return
	}
	grace := t.Grace
	if grace <= 0 {
		grace = time.Minute
	}
	n, err := Sweep(r.Context(), t.Dispatcher, t.Dispatcher.Clock.Now().Add(-grace))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"sent": n})
}
