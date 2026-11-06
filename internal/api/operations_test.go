//go:build local

package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// acceptAll accepts every task's credential: the test hands the worker its tasks itself.
type acceptAll struct{}

func (acceptAll) Token(string) (string, error) { return "test", nil }
func (acceptAll) Verify(string, string) error  { return nil }

// work delivers every queued worker task to a worker that runs kinds, as the queue would: each row leased, run and
// finished with its outcome.
func (e *env) work(kinds map[string]outbox.Work) {
	e.t.Helper()
	h := &outbox.Handler{Class: storage.ClassWorker, Store: workerStore(e.t), Tokens: acceptAll{}, Clock: e.clock, Holder: "test-worker", Kinds: kinds}
	for {
		task, err := e.queue.Next(context.Background(), "worker")
		if err != nil {
			return
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(task.Payload)))
		if w.Code >= 300 {
			e.t.Fatalf("a worker task answered %d: %s", w.Code, w.Body)
		}
	}
}

// Long work is polled as an operation (FR-INT-11): asking for an export answers 202 with the operation in Location
// (none for a preview); it runs until the worker has done the job, then succeeded with when, and its resource is the
// export; only whoever asked reads it. A Run now that joins one waiting names the same job, and a job that died is
// failed.
// req: FR-INT-11
// asvs: V8.2.2
func TestLongWorkIsPolledAsAnOperation(t *testing.T) {
	e := newEnv(t)
	me := e.as(authz.Accountant)
	body := map[string]any{"report": "trial_balance", "to": "2026-10-31", "format": "csv"}
	if pv := e.call(http.MethodPost, "/v1/report-exports", me, body, "Prefer", "return=preview"); pv.Status != 200 || pv.Header.Get("Location") != "" || pv.str("operation_id") != "" {
		t.Errorf("a preview names an operation: %d %v %s", pv.Status, pv.Header, pv.Raw)
	}
	r := e.call(http.MethodPost, "/v1/report-exports", me, body)
	loc := r.Header.Get("Location")
	if r.Status != 202 || r.str("operation_id") == "" || loc != "/v1/operations/"+r.str("operation_id") {
		t.Fatalf("asking for an export: %d %v %s", r.Status, r.Header, r.Raw)
	}
	op := e.call(http.MethodGet, loc, me, nil)
	if op.Status != 200 || op.str("status") != "running" || op.str("kind") != "report_export" || op.str("resource") != "/v1/report-exports/"+r.str("id") ||
		op.Header.Get("Retry-After") != "2" || op.str("finished_at") != "" {
		t.Fatalf("while it runs: %d %v %s", op.Status, op.Header, op.Raw)
	}
	e.problemIs(e.call(http.MethodGet, loc, e.as(authz.Accountant), nil), "getOperation", 404, CodeNotFound)

	worker := workerStore(t)
	prep := reportexport.Preparer{Store: worker, Files: e.blobs, Outbox: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock}, Clock: e.clock}
	e.work(map[string]outbox.Work{reportexport.Kind: prep.Work()})
	op = e.call(http.MethodGet, loc, me, nil)
	if op.str("status") != "succeeded" || op.str("finished_at") == "" || op.Header.Get("Retry-After") != "" {
		t.Fatalf("once done: %v %s", op.Header, op.Raw)
	}
	if x := e.call(http.MethodGet, op.str("resource"), me, nil); x.Status != 200 || x.str("status") != "ready" {
		t.Errorf("its resource: %d %s", x.Status, x.Raw)
	}

	owner := e.as(authz.Owner)
	first := e.call(http.MethodPost, "/v1/checks/runs", owner, nil)
	second := e.call(http.MethodPost, "/v1/checks/runs", owner, nil)
	if first.Status != 202 || second.Status != 202 || second.Body["already_waiting"] != true || second.Header.Get("Location") == "" ||
		second.Header.Get("Location") == first.Header.Get("Location") {
		t.Fatalf("running the checks twice: %d %s / %d %s", first.Status, first.Raw, second.Status, second.Raw)
	}
	c, err := pgx.Connect(context.Background(), db.URL("fyniq_migrator"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	// The outbox forces row-level security on its owner too: the job's organisation is set first.
	if _, err := c.Exec(context.Background(), `SELECT set_config('app.org_id', $1, false)`, e.books.Org.String()); err != nil {
		t.Fatal(err)
	}
	if tag, err := c.Exec(context.Background(), `UPDATE fyniq.outbox SET state = 'dead', updated_at = now() WHERE kind = 'checks.request'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("the run's job dies: %v %v", tag, err)
	}
	for _, x := range []response{first, second} {
		if op := e.call(http.MethodGet, x.Header.Get("Location"), owner, nil); op.str("status") != "failed" || op.str("kind") != "checks_run" ||
			op.str("resource") != "/v1/checks" || op.str("finished_at") == "" {
			t.Errorf("a run whose job died: %s", op.Raw)
		}
	}
}

// matrixOperations are the authorisation matrix's rows for polling long work (FR-INT-11): a principal that may ask for
// an export polls its own operation; one that may not is given another person's, which it never reads (404).
var matrixOperations = map[string]func(e *env, tok string) (response, func() bool){
	"getOperation": func(e *env, tok string) (response, func() bool) {
		body := map[string]any{"report": "trial_balance", "to": "2026-10-31", "format": "csv"}
		loc := e.call(http.MethodPost, "/v1/report-exports", tok, body).Header.Get("Location")
		if loc == "" {
			loc = e.call(http.MethodPost, "/v1/report-exports", e.as(authz.Accountant), body).Header.Get("Location")
		}
		return e.call(http.MethodGet, loc, tok, nil), nil
	},
}
