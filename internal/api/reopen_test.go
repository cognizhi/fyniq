//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A closed period reopens only on another person's approval, with a reason; the reopening keeps the snapshots before
// and after; only the latest closed period reopens, and a request it no longer fits is void. The others who close and
// post hear of each close and reopening.
// req: FR-GL-08
// req: FR-NTF-01
// req: FM-1
// req: AUD-04
func TestReopeningAClosedPeriod(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Controller), f.as(f.weiling, authz.Owner, authz.Controller)
	path := func(id, op string) string { return "/v1/periods/" + id + "/" + op }
	etagOf := func(id string) string {
		return e.call(http.MethodGet, path(id, "close"), ahkow, nil).Header.Get("ETag")
	}
	closeIt := func(id string) response {
		t.Helper()
		checklistDone(t, e.store, e.books.Org, uuid.MustParse(id))
		c := e.call(http.MethodPost, path(id, "close-checks"), ahkow, nil, "If-Match", etagOf(id))
		r := e.call(http.MethodPost, path(id, "close"), weiling, map[string]any{"checks_id": c.str("checks", "id"), "statement": "Reviewed."}, "If-Match", etagOf(id))
		if r.Status != 200 {
			t.Fatalf("closing %s: %d %s", id, r.Status, r.Raw)
		}
		return r
	}
	ask := func(who, id string) response {
		return e.call(http.MethodPost, path(id, "reopen"), who, map[string]any{"reason": "A supplier's October invoice arrived late"}, "If-Match", etagOf(id))
	}
	oct, nov := e.books.Oct.ID.String(), e.books.Nov.ID.String()
	e.postedJournal("100.00")
	closed := closeIt(oct)
	before := closed.str("close", "snapshot_sha256")

	// An open period does not reopen; a closed one only on a request another person approves.
	e.problemIs(ask(ahkow, nov), "requestPeriodReopen", 409, closing.CodeNotClosed)
	r := ask(ahkow, oct)
	if r.Status != 202 || r.str("status") != "pending" || r.str("object_type") != approval.ObjectPeriod {
		t.Fatalf("asking to reopen: %d %s", r.Status, r.Raw)
	}
	request := r.str("id")
	if g := e.call(http.MethodGet, path(oct, "close"), ahkow, nil); g.str("reopen_asked") != request || g.str("period", "status") != "closed" {
		t.Fatalf("the close while the reopening waits: %s", g.Raw)
	}
	e.problemIs(ask(weiling, oct), "requestPeriodReopen", 409, approval.CodePending)
	view := e.call(http.MethodGet, "/v1/approvals/"+request, weiling, nil)
	e.problemIs(f.decide(ahkow, request, "approve", hashOf(view)), "approveApproval", 403, approval.CodeOwnRequest)

	// Wei Ling approves: it reopens, keeping the close's snapshot as the one before.
	if d := f.decide(weiling, request, "approve", hashOf(view)); d.Status != 200 || d.str("status") != "approved" {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	g := e.call(http.MethodGet, path(oct, "close"), ahkow, nil)
	if g.str("period", "status") != "open" || g.str("reopen", "before_sha256") != before || len(g.str("reopen", "after_sha256")) != 64 ||
		g.str("reopen", "reason") != "A supplier's October invoice arrived late" || g.str("reopen", "approval_id") != request ||
		g.str("reopen", "reopened_by", "name") != "Wei Ling" || g.Body["reopen_asked"] != nil {
		t.Fatalf("after the reopening: %s", g.Raw)
	}
	e.postedJournal("12.00") // October takes postings again

	// Those who close and post hear of the close and the reopening, and why; the approver who reopened it does not.
	fanOutAll(t, e)
	told := e.call(http.MethodGet, "/v1/notifications", ahkow, nil).Raw
	for _, want := range []string{`was closed: nothing more posts into it`, `was reopened: A supplier's October invoice arrived late`, `"type":"period.reopened"`} {
		if !strings.Contains(told, want) {
			t.Errorf("Ah Kow was not told %s: %s", want, told)
		}
	}
	if theirs := e.call(http.MethodGet, "/v1/notifications", weiling, nil).Raw; strings.Contains(theirs, "period.reopened") || strings.Contains(theirs, "period.closed") {
		t.Errorf("Wei Ling, who closed and reopened it, was told: %s", theirs)
	}

	// Only the latest closed period reopens.
	closeIt(oct)
	closeIt(nov)
	e.problemIs(ask(ahkow, oct), "requestPeriodReopen", 409, closing.CodeLater)

	// A request the period no longer fits is void when approved: while November's waits, December is opened and
	// closed, so November is no longer the latest closed period (G4-055's rule, for periods).
	waiting := ask(ahkow, nov).str("id")
	waitingView := e.call(http.MethodGet, "/v1/approvals/"+waiting, weiling, nil)
	var dec storage.Period
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		dec, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	closeIt(dec.ID.String())
	e.problemIs(f.decide(weiling, waiting, "approve", hashOf(waitingView)), "approveApproval", 409, approval.CodeChanged)
	if s := e.call(http.MethodGet, path(nov, "close"), ahkow, nil); s.str("period", "status") != "closed" {
		t.Fatalf("November after a void request: %s", s.Raw)
	}

	// The matrix: an Owner alone does not ask; a Bookkeeper sees nothing.
	e.problemIs(ask(f.as(f.weiling, authz.Owner), oct), "requestPeriodReopen", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, path(oct, "close"), e.as(authz.Bookkeeper), nil), "getPeriodClose", 403, CodeForbidden)
}
