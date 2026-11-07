//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// Every output follows the roles a person holds now (SEC-34): an approval asked for reaches the Controller's centre,
// and while they hold only the Accountant role it leaves the list, the bell's count, a read by its ID and the count
// "read all" reports, and comes back with the role. A ready export downloads only while its person may still read the
// reports. A department's approver, whose view of the reports is limited to the department, is refused the statement
// an Accountant reads with the same parameters, and no answer is cached (G4-064). Held email is withdrawn in
// postgres's TestHeldEmailFollowsTheRole; tokens are revoked within 60 seconds in TestTokenLifecycle.
// req: SEC-34
// req: security/identity-and-access No event about a record the user cannot see
// req: security/identity-and-access Role change takes effect within 60 seconds
// req: security/identity-and-access G4-064 Cached report results do not cross scopes
// req: security/identity-and-access A notification by row follows the role (rev 0.186)
// asvs: V8.2.2, V8.3.2, V14.3.2
func TestEveryOutputFollowsTheRolesHeldNow(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	controller := f.as(f.weiling, authz.Accountant, authz.Controller)
	accountantOnly := f.as(f.weiling, authz.Accountant)
	drainWork(t, e)
	d := e.call(http.MethodPost, "/v1/journals", ahkow, e.draft("2000.01"))
	if asked := e.call(http.MethodPost, "/v1/journals/"+d.str("id")+"/post-approval", ahkow, nil, "If-Match", d.Header.Get("ETag")); asked.Status != 202 {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	fanOutAll(t, e)
	got := e.call(http.MethodGet, "/v1/notifications", controller, nil)
	items, _ := got.Body["items"].([]any)
	if got.Status != 200 || got.Body["unread"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["type"] != "approval.requested" {
		t.Fatalf("the Controller's centre: %d %s", got.Status, got.Raw)
	}
	id := items[0].(map[string]any)["id"].(string)

	if r := e.call(http.MethodGet, "/v1/notifications", accountantOnly, nil); len(r.Body["items"].([]any)) != 0 || r.Body["unread"] != float64(0) {
		t.Fatalf("without the right it is about, it still shows or counts: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/notifications?unread=true&page_size=1", accountantOnly, nil); len(r.Body["items"].([]any)) != 0 || r.Body["next_cursor"] != nil {
		t.Fatalf("a page of what they no longer see: %s", r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/notifications/"+id+"/read", accountantOnly, nil); r.Status != http.StatusNotFound {
		t.Fatalf("reading it by its ID without the right: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/notifications/read-all", accountantOnly, nil); r.Status != 200 || r.Body["marked"] != float64(0) {
		t.Fatalf("reading all without the right: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/notifications", controller, nil); len(r.Body["items"].([]any)) != 1 || r.Body["unread"] != float64(0) {
		t.Fatalf("with the role back, it shows again, read by the read-all: %s", r.Raw)
	}

	// A ready export follows the role at download.
	export := matrixExport(e, controller, true)
	if r := e.call(http.MethodGet, "/v1/report-exports/"+export+"/file", controller, nil); r.Status != 200 {
		t.Fatalf("downloading one's own export: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/report-exports/"+export+"/file", f.as(f.weiling, authz.Employee), nil); r.Status != http.StatusForbidden {
		t.Fatalf("downloading it after the reporting roles went: %d %s", r.Status, r.Raw)
	}

	// G4-064: each scope its own answer, none cached.
	const pl = "/v1/reports/statements/pl?from=2026-10&to=2026-10"
	dept := e.call(http.MethodGet, pl, e.as(authz.Approver), nil)
	full := e.call(http.MethodGet, pl, e.as(authz.Accountant), nil)
	if dept.Status != http.StatusForbidden || full.Status != 200 {
		t.Fatalf("a department's view and the Accountant's: %d, %d %s", dept.Status, full.Status, full.Raw)
	}
	for _, r := range []response{dept, full, got} {
		if c := r.Header.Get("Cache-Control"); c != "no-store" {
			t.Fatalf("an answer that may be cached: %q", c)
		}
	}
}
