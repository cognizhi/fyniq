// Package outbox runs background work (PRD-001 §12.11, ADR-T7). A command writes its work as an
// outbox row in its own unit of work; after commit the Dispatcher sends the row as a task to its
// worker class; the class's Handler takes a lease on the row, runs the work, and finishes the row.
// Nothing stays running: Cloud Tasks (locally the push dispatcher) delivers each task as an HTTP
// request, and the tick sweeps rows that never left or overran (p0-outbox-background-work).
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
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// TaskName is the name of a row's nth dispatch. Cloud Tasks remembers names for 24 hours, so the
// platform's own re-dispatches use a new number (§12.11).
func TaskName(id uuid.UUID, dispatch int) string { return fmt.Sprintf("%s-%d", id, dispatch) }

// Message is a task's payload: which row to run.
type Message struct {
	OrganisationID uuid.UUID `json:"organisation_id"`
	OutboxID       uuid.UUID `json:"outbox_id"`
}

// Dispatcher sends outbox rows to their worker classes after commit.
type Dispatcher struct {
	Store    storage.Store
	Queue    ports.Queue
	Clock    ports.Clock
	Deadline time.Duration // how long a dispatched row may take before the sweep sends it again; 0 means 15 min
}

func (d Dispatcher) deadline() time.Duration {
	if d.Deadline > 0 {
		return d.Deadline
	}
	return 15 * time.Minute
}

// Enqueue writes work in u (Transactional outbox) and dispatches it once u commits; if u rolls back
// there is neither a row nor a task.
func (d Dispatcher) Enqueue(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, item storage.NewOutboxItem) (storage.OutboxRow, error) {
	row, err := u.Outbox().Enqueue(ctx, item)
	if err != nil {
		return storage.OutboxRow{}, err
	}
	u.AfterCommit(func(ctx context.Context) {
		// Best effort: a row that is not dispatched now stays pending, and the tick sends it.
		_ = d.Dispatch(context.WithoutCancel(ctx), org, row)
	})
	return row, nil
}

// Dispatch sends a row as a task under a new name. The row is marked dispatched first, so a task
// never finds its row still pending; if the enqueue then fails, the row is overdue at its deadline
// and the sweep sends it again.
func (d Dispatcher) Dispatch(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
	name := TaskName(row.ID, row.Dispatches+1)
	if err := d.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Outbox().MarkDispatched(ctx, row.ID, name, d.Clock.Now().Add(d.deadline()))
	}); err != nil {
		return err
	}
	payload, err := json.Marshal(Message{OrganisationID: org, OutboxID: row.ID})
	if err != nil {
		return err
	}
	return d.Queue.Enqueue(ctx, ports.Task{Queue: string(row.Class), Name: name, Payload: payload})
}

// ErrUnknownOutcome is what work returns when it cannot know whether an outside call took effect (a
// time-out after sending). The row waits for reconciliation and is never retried blind (§12.11).
var ErrUnknownOutcome = errors.New("the outcome of the outside call is unknown")

// Work does one row's work, in its own unit of work. It must be idempotent on the row: a posting
// uses a posting identity derived from the row, so a second run cannot post twice.
type Work func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error

// Handler is a worker class's /tasks endpoint.
type Handler struct {
	Class  storage.WorkerClass
	Store  storage.Store
	Tokens ports.TaskTokens
	Clock  ports.Clock
	Holder string        // this instance, recorded on the lease
	Lease  time.Duration // how long a lease lasts; 0 means 10 min
	Kinds  map[string]Work
	// Dead runs after a row is marked dead, as for the tick; nil does nothing.
	Dead Dead
}

// Dead runs in its own unit of work once a row is marked dead after its attempts ran out, with why, such as telling
// those who look after the organisation (notify.JobDead). Its failure never keeps the row from dying.
type Dead func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, row storage.OutboxRow, why string) error

// ServeHTTP runs one delivery: check the class's credential, lease the row, run the work, finish.
// 2xx stops the queue's retries; anything else asks for another delivery.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := h.Tokens.Verify(token, string(h.Class)); err != nil {
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
	var row storage.OutboxRow
	var leased bool
	now := h.Clock.Now()
	err := h.Store.Do(ctx, m.OrganisationID, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		row, leased, err = u.Outbox().Lease(ctx, m.OutboxID, h.Holder, now, now.Add(lease))
		return err
	})
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return http.StatusOK, "no such outbox row"
	case err != nil:
		return http.StatusInternalServerError, err.Error()
	case !leased && row.State == storage.OutboxRunning:
		return http.StatusConflict, "another delivery holds the row" // try again later
	case !leased:
		return http.StatusOK, "nothing to do: the row is " + string(row.State)
	case row.Class != h.Class:
		h.finish(ctx, m.OrganisationID, row, storage.OutboxDead, fmt.Sprintf("delivered to %s, but the row is for %s", h.Class, row.Class))
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
		h.finish(ctx, m.OrganisationID, row, storage.OutboxDone, "")
		return http.StatusOK, "done"
	case errors.Is(err, ErrUnknownOutcome):
		h.finish(ctx, m.OrganisationID, row, storage.OutboxUnknownOutcome, err.Error())
		return http.StatusOK, "unknown outcome: reconcile before any retry"
	case row.Attempts >= row.MaxAttempts:
		h.finish(ctx, m.OrganisationID, row, storage.OutboxDead, err.Error())
		return http.StatusOK, "dead after " + fmt.Sprint(row.Attempts) + " attempts"
	default:
		h.finish(ctx, m.OrganisationID, row, storage.OutboxFailed, err.Error())
		return http.StatusInternalServerError, err.Error()
	}
}

// finish ends the lease; a dead row is also recorded in the audit trail (outbox.row.dead) for Checks, then told of.
func (h *Handler) finish(ctx context.Context, org uuid.UUID, row storage.OutboxRow, state storage.OutboxState, msg string) {
	err := h.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := u.Outbox().Finish(ctx, row.ID, h.Holder, state, msg); err != nil {
			return err
		}
		if state != storage.OutboxDead {
			return nil
		}
		return u.Audit().Record(ctx, storage.AuditRecord{Action: "outbox.row.dead", ObjectType: "outbox", ObjectID: row.ID,
			Changes: map[string][2]any{"state": {string(row.State), string(state)}, "kind": {nil, row.Kind},
				"attempts": {nil, row.Attempts}, "last_error": {nil, msg}}})
	})
	if err == nil && state == storage.OutboxDead && h.Dead != nil {
		_ = h.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return h.Dead(ctx, u, org, row, msg) })
	}
}

// Once makes a repeated posting harmless: when work's unit of work fails because its posting
// identity (or business key) is already used, the work was done by an earlier run (Posting runs at
// most once), so it counts as success.
func Once(err error) error {
	if storage.CodeOf(err) == storage.CodeDuplicate {
		return nil
	}
	return err
}
