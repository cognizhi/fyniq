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
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The checks of the payments out: a supplier whose bank details were approved in the last 30 days with a bill still to
// pay (C.2 #11), and a cash advance open past the claim deadline (C.2 #39).
// req: C.2 #11
// req: C.2 #39
// req: FR-CT-03
// req: FR-AP-17
func TestThePaymentChecks(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Accountant), f.as(f.weiling)
	drainWork(t, e)
	supplier := e.supplier()
	e.postedBill("1000.00", "90.00")
	wantStatus(t, e.checked("purchases.bank-details-changed"), "ok")

	// Wei Ling approves the supplier's first account: added today, with the bill still to pay.
	id, tag := e.bankChange(supplier, ahkow, sgAccount("012-345678-9"))
	e.bankLetter(id, ahkow)
	asked := e.call(http.MethodPost, "/v1/bank-changes/"+id+"/approval", ahkow, nil, "If-Match", tag)
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking = %d: %s", asked.Status, asked.Raw)
	}
	if r := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); r.Status != http.StatusOK {
		t.Fatalf("approving = %d: %s", r.Status, r.Raw)
	}
	c := e.checked("purchases.bank-details-changed")
	wantStatus(t, c, "warning")
	if it := itemNamed(c, supplier); it == nil || !strings.Contains(it["detail"].(string), "SGD account added ending 6789 on 15 Oct 2026; still to pay: ") {
		t.Fatalf("C.2 #11's item: %s", c.Raw)
	}
	// Thirty days on, the change is no longer new. The checks' runs only move forward, and so does the clock.
	e.clock.Advance(31 * 24 * time.Hour)
	wantStatus(t, e.checked("purchases.bank-details-changed"), "ok")

	// An advance of 13 October still open after the claim deadline of 60 days.
	advance, _ := e.recordedAdvance(f.ahkow, "300.00")
	wantStatus(t, e.checked("claims.advances-unsettled"), "ok")
	e.clock.Set(time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC))
	c = e.checked("claims.advances-unsettled")
	wantStatus(t, c, "warning")
	if it := itemNamed(c, advance); it == nil || !strings.Contains(it["detail"].(string), "300.00 SGD is still open on an advance paid on 2026-10-13") {
		t.Fatalf("C.2 #39's item: %s", c.Raw)
	}
}

// The checks of the platform: a scheduled job that has not run within its time (C.2 #12), a statutory pack in force with
// a later version loaded and not activated, or past its review date (C.2 #16), and the email Fyniq sends: the night's
// check of the sending domains (C.2 #37; its rates are TestTheEmailRates').
// req: C.2 #12
// req: C.2 #16
// req: C.2 #37
// req: FR-AUT-01
// req: ENG-30
// req: ENG-33
// req: FR-INT-21
func TestThePlatformChecks(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "service", ID: "checks"}})

	// C.2 #12, with a job of its own: the heartbeats are the installation's, shared by every test.
	worker := workerStore(t)
	job := "checkstest.job_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	runner := checks.Runner{Store: worker, Clock: e.clock, Platform: worker, Jobs: []checks.WatchedJob{{Name: job, Stale: 3 * time.Hour}}}
	scheduler := func() response {
		if _, err := runner.Request(ctx, e.books.Org, "user:founder"); err != nil {
			t.Fatal(err)
		}
		return e.call(http.MethodGet, "/v1/checks/scheduler.jobs-on-time", e.as(authz.Controller), nil)
	}
	c := scheduler()
	wantStatus(t, c, "error")
	if it := itemNamed(c, job); it == nil || it["detail"] != "It has not run" {
		t.Fatalf("a job that never ran: %s", c.Raw)
	}
	if err := worker.Beat(ctx, job, e.clock.Now(), "ok"); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, scheduler(), "ok")
	e.clock.Advance(4 * time.Hour) // the checks' runs only move forward, and so does the clock
	if it := itemNamed(scheduler(), job); it == nil || !strings.HasPrefix(it["detail"].(string), "It last ran 4h0m0s ago") {
		t.Fatalf("a late job: %s", scheduler().Raw)
	}

	// C.2 #16: sg-gst@2026.1 activated while sg-gst@2026.2, which corrects it, is loaded and not.
	wantStatus(t, e.checked("parameters.packs-current"), "ok")
	r := e.call(http.MethodPost, "/v1/statutory-packs/sg-gst@2026.1/activate", f.as(f.ann), nil, "If-Match", `"0"`)
	if a := f.decide(f.as(f.ben), r, "approve"); a.Status != http.StatusOK {
		t.Fatalf("activating: %d %s", a.Status, a.Raw)
	}
	c = e.checked("parameters.packs-current")
	wantStatus(t, c, "warning")
	if it := itemNamed(c, "sg-gst@2026.2"); it == nil || it["detail"] != "A later version of sg-gst, in force from 1 Apr 1994, is loaded and not activated" {
		t.Fatalf("C.2 #16's later version: %s", c.Raw)
	}

	// C.2 #37: not assessed until the night's check of the domains; then its outcome, and the organisation's own rates.
	wantStatus(t, e.checked("email.sending-healthy"), "not_assessed")
	domains := func(status string, failing []string) {
		e.fixture(func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: email.ActionDomainsChecked, ObjectType: "organisation", ObjectID: e.books.Org,
				Changes: map[string][2]any{"status": {nil, status}, "domains": {nil, 1}, "failing": {nil, failing}}})
		})
	}
	domains("failed", []string{"mail.example.com: TXT k._domainkey.mail.example.com: missing"})
	c = e.checked("email.sending-healthy")
	wantStatus(t, c, "warning")
	if it := itemNamed(c, "mail.example.com"); it == nil || it["detail"] != "Its records are not right: TXT k._domainkey.mail.example.com: missing" {
		t.Fatalf("C.2 #37's domain: %s", c.Raw)
	}
	// Between nights the domain stays as the night left it.
	if itemNamed(e.checked("email.sending-healthy"), "mail.example.com") == nil {
		t.Fatal("a run between nights forgot the night's outcome")
	}
	domains("passed", []string{})
	wantStatus(t, e.checked("email.sending-healthy"), "ok")
	// C.2 #16 again, once sg-gst@2026.1's review date has passed.
	e.clock.Set(time.Date(2027, 2, 1, 9, 0, 0, 0, time.UTC))
	if it := itemNamed(e.checked("parameters.packs-current"), "sg-gst@2026.1"); it == nil || !strings.HasPrefix(it["detail"].(string), "Its review date, 31 Jan 2027, has passed") {
		t.Fatal("C.2 #16 does not flag the version past its review date")
	}
}
