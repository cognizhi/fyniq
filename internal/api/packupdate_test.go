//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/packupdate"
)

// A statutory pack update waiting becomes the platform's own compliance tasks (ENG-33, p2-tax-filings task 4.1): with sg-gst@2026.1 activated and sg-gst@2026.3, which corrects it (the
// same effective day), loaded and in force, the job raises pack.update_available and pack.correction_assessment for the version waiting, of the source `system`, owned by the Finance
// controller; a second run raises nothing twice; the correction's task counts the posted documents dated in the period it covers; once 2026.3 is activated the next run clears both. A
// person can still mark a task done through the API, and nothing was re-posted.
// req: ENG-33
// req: AI-65
// asvs: V4.1.3
func TestAPackUpdateWaitingBecomesTheplatformsOwnTasksAndClearsWhenAccepted(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	weiling := f.as(f.weiling, authz.Owner, authz.Controller)
	activate := func(version string) {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/statutory-packs/"+version+"/activate", ahkow, nil, "If-Match", `"0"`)
		if a := f.decide(weiling, r.str("id"), "approve", hashOf(r)); a.Status != http.StatusOK || a.str("status") != "approved" {
			t.Fatalf("activating %s: %d %s / %d %s", version, r.Status, r.Raw, a.Status, a.Raw)
		}
	}
	e.issuedInvoice("1", "100.00") // a posted invoice the correction reaches
	worker := workerStore(t)
	run := func() packupdate.Result {
		t.Helper()
		r, err := packupdate.RaiseAll(context.Background(), worker, func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) })
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	tasks := func() response { return e.call(http.MethodGet, "/v1/compliance/tasks", e.as(authz.Controller), nil) }
	if r := run(); r.Raised != 0 {
		t.Fatalf("nothing is activated, so no update waits: %+v", r)
	}
	activate("sg-gst@2026.1")
	if r := run(); r.Raised != 2 || r.Cleared != 0 {
		t.Fatalf("the first run: %+v", r)
	}
	r := tasks()
	if r.Status != 200 || strings.Count(r.Raw, `"source":"system"`) != 2 || !strings.Contains(r.Raw, "Statutory pack update waiting: sg-gst@2026.3") ||
		!strings.Contains(r.Raw, "Assess a pack correction: sg-gst@2026.3") || !strings.Contains(r.Raw, "1 invoice") || !strings.Contains(r.Raw, `"owner_role":"controller"`) {
		t.Fatalf("the tasks: %d %s", r.Status, r.Raw)
	}
	if again := run(); again.Raised != 0 || again.Cleared != 0 {
		t.Errorf("a second run: %+v", again)
	}
	activate("sg-gst@2026.3")
	if after := run(); after.Cleared != 2 {
		t.Errorf("once accepted: %+v", after)
	}
	if left := tasks(); strings.Count(left.Raw, `"status":"cleared"`) != 2 || strings.Contains(left.Raw, `"status":"open"`) {
		t.Errorf("the tasks after the update was accepted: %s", left.Raw)
	}
}
