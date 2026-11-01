//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// runCatalogue runs every check of e's organisation as the worker does on Run now.
func (e *env) runCatalogue() {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "service", ID: "checks"}})
	if _, err := (checks.Runner{Store: workerStore(e.t), Clock: e.clock}).Request(ctx, e.books.Org, "user:founder"); err != nil {
		e.t.Fatal(err)
	}
}

// joined makes a member of e's organisation holding roles and returns their user ID.
func (e *env) joined(name string, roles ...string) uuid.UUID {
	e.t.Helper()
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Administrator), map[string]any{"display_name": name, "roles": roles}).str("code")
	id := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "member-" + name, Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": name}).str("member_id")
	for _, x := range e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil).Body["items"].([]any) {
		if m := x.(map[string]any); m["id"] == id {
			return uuid.MustParse(m["user_id"].(string))
		}
	}
	e.t.Fatalf("%s did not join", name)
	return uuid.Nil
}

func checkNamed(r response, key string) map[string]any {
	for _, x := range r.Body["checks"].([]any) {
		if c := x.(map[string]any); c["key"] == key {
			return c
		}
	}
	return nil
}

// Every reader sees each check's status, count and words; its items only when their roles open the items' own row, and
// an area's reader lists that area's checks only; books health follows the worst status they list (design D7, D9).
// req: FR-CTL-01
// req: FR-CTL-02
// req: QA-SEC-03
// asvs: V8.2.2
func TestTheChecksAsEachReaderSeesThem(t *testing.T) {
	e := newEnv(t)
	before := e.call(http.MethodGet, "/v1/checks", e.as(authz.Controller), nil)
	if before.Status != http.StatusOK || before.Body["last_run"] != nil {
		t.Fatalf("before any run = %d: %s", before.Status, before.Raw)
	}
	if c := checkNamed(before, "setup.system-accounts-exist"); c["status"] != "not_assessed" || c["needs"] != "its first run" {
		t.Errorf("a check before its first run: %v", c)
	}
	e.postedJournal("100.00") // the test books' accounts have no roles and no cash-flow lines
	e.runCatalogue()

	controller := e.call(http.MethodGet, "/v1/checks", e.as(authz.Controller), nil)
	health := controller.Body["health"].(map[string]any)
	if health["score"] != "problems" || health["problems"] == float64(0) || controller.Body["may_name_owners"] != true || controller.Body["last_run"] == nil {
		t.Fatalf("the Controller's checks: %s", controller.Raw)
	}
	if n := len(controller.Body["checks"].([]any)); n != len(checks.Catalogue()) {
		t.Errorf("the Controller sees %d checks of %d", n, len(checks.Catalogue()))
	}
	roles := checkNamed(controller, "setup.system-accounts-exist")
	if roles["status"] != "error" || roles["count"] != float64(len(storage.SystemRoles)) || len(roles["items"].([]any)) != len(storage.SystemRoles) ||
		roles["items_hidden"] != false || roles["ref"] != "C.1 #7" || roles["owner"].(map[string]any)["role"] != "controller" {
		t.Errorf("the system accounts, as the Controller sees them: %v", roles)
	}
	// The Administrator reads the checks but not the chart: the count, never the items.
	admin := checkNamed(e.call(http.MethodGet, "/v1/checks", e.as(authz.Administrator), nil), "setup.system-accounts-exist")
	if admin["count"] != float64(len(storage.SystemRoles)) || len(admin["items"].([]any)) != 0 || admin["items_hidden"] != true {
		t.Errorf("the system accounts, as the Administrator sees them: %v", admin)
	}
	// The AR specialist's view is of sales: none of the books' checks in their list, none of their items when opened.
	ar := e.call(http.MethodGet, "/v1/checks", e.as(authz.ARSpecialist), nil)
	if len(ar.Body["checks"].([]any)) != 0 || ar.Body["health"].(map[string]any)["score"] != "ok" || ar.Body["may_name_owners"] != false {
		t.Errorf("the AR specialist's checks: %s", ar.Raw)
	}
	// Opened by its key, a check shows its items to the AR specialist where their own row allows: they view the chart.
	one := e.call(http.MethodGet, "/v1/checks/setup.system-accounts-exist", e.as(authz.ARSpecialist), nil)
	if one.Status != http.StatusOK || one.Body["items_hidden"] != false || len(one.Body["items"].([]any)) == 0 || one.Header.Get("ETag") != `"0"` {
		t.Errorf("a check the AR specialist opens = %d: %s", one.Status, one.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/checks/books.no-such-check", e.as(authz.Controller), nil); r.Status != http.StatusNotFound || r.problemCode() != CodeUnknownCheck {
		t.Errorf("a check that does not exist = %d %s", r.Status, r.problemCode())
	}
	if r := e.call(http.MethodGet, "/v1/checks", e.as(authz.Employee), nil); r.Status != http.StatusForbidden {
		t.Errorf("an Employee reads the checks: %d", r.Status)
	}
	// Within an area, the checks not assessed come first.
	var area string
	var assessed bool
	for _, x := range controller.Body["checks"].([]any) {
		c := x.(map[string]any)
		if c["area"] != area {
			area, assessed = c["area"].(string), false
		}
		if c["status"] == "not_assessed" && assessed {
			t.Errorf("%s is not assessed but follows an assessed check of %s", c["key"], area)
		}
		assessed = assessed || c["status"] != "not_assessed"
	}
}

// An Owner or Controller names a member whose roles open a check's items as its owner, at the owner's version, audited;
// someone else is CHECK-02, a stale ETag 412; null gives the check back to its role (design D8).
// req: FR-CTL-01
// asvs: V8.2.2
func TestNamingACheckOwner(t *testing.T) {
	e := newEnv(t)
	accountant := e.joined("Accountant Tan", "accountant")
	employee := e.joined("Employee Lim", "employee")
	path := "/v1/checks/setup.system-accounts-exist"
	name := func(person any, tag string) response {
		return e.call(http.MethodPut, path+"/owner", e.as(authz.Controller), map[string]any{"person_user_id": person}, "If-Match", tag)
	}
	r := name(accountant.String(), `"0"`)
	owner, _ := r.Body["owner"].(map[string]any)
	if r.Status != http.StatusOK || r.Header.Get("ETag") != `"1"` || owner["person"].(map[string]any)["name"] != "Accountant Tan" || owner["role"] != "controller" {
		t.Fatalf("naming the Accountant = %d: %s", r.Status, r.Raw)
	}
	if r := name(nil, `"0"`); r.Status != http.StatusPreconditionFailed {
		t.Errorf("a stale ETag = %d", r.Status)
	}
	if r := e.call(http.MethodPut, path+"/owner", e.as(authz.Controller), map[string]any{"person_user_id": nil}); r.Status != http.StatusPreconditionRequired {
		t.Errorf("no If-Match = %d", r.Status)
	}
	for who, person := range map[string]string{"an Employee, who cannot open the chart": employee.String(), "someone who is not a member": uuid.NewString()} {
		if r := name(person, `"1"`); r.Status != http.StatusUnprocessableEntity || r.problemCode() != CodeCheckOwner {
			t.Errorf("naming %s = %d %s", who, r.Status, r.problemCode())
		}
	}
	if r := e.call(http.MethodPut, path+"/owner", e.as(authz.Bookkeeper), map[string]any{"person_user_id": nil}, "If-Match", `"1"`); r.Status != http.StatusForbidden {
		t.Errorf("a Bookkeeper names an owner: %d", r.Status)
	}
	back := name(nil, `"1"`)
	if back.Status != http.StatusOK || back.Body["owner"].(map[string]any)["person"] != nil || back.Header.Get("ETag") != `"2"` {
		t.Errorf("giving it back to the role = %d: %s", back.Status, back.Raw)
	}
	var trail strings.Builder
	if err := workerStore(t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range events {
			trail.WriteString(ev.Body)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(trail.String(), `"action":"check.owner_set"`) != 2 {
		t.Errorf("the owner's changes were not each audited")
	}
}

// Run now asks the worker for a run of every check: a second while one waits joins it, and once it has run, another
// within the same minute is refused with Retry-After; the page sees the request until the run.
// req: FR-CTL-01
func TestRunNow(t *testing.T) {
	e := newEnv(t)
	first := e.call(http.MethodPost, "/v1/checks/runs", e.as(authz.Bookkeeper), nil)
	if first.Status != http.StatusAccepted || first.Body["already_waiting"] != false || first.str("requested_at") == "" {
		t.Fatalf("Run now = %d: %s", first.Status, first.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/checks", e.as(authz.Bookkeeper), nil); r.Body["run_requested"] != true {
		t.Errorf("the page does not see the request: %s", r.Raw)
	}
	again := e.call(http.MethodPost, "/v1/checks/runs", e.as(authz.Controller), nil)
	if again.Status != http.StatusAccepted || again.Body["already_waiting"] != true {
		t.Errorf("Run now while one waits = %d: %s", again.Status, again.Raw)
	}
	// The worker runs it.
	worker := workerStore(t)
	ctx := context.Background()
	if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		due, err := u.Sweeping().Due(ctx, e.clock.Now().AddDate(1, 0, 0), e.clock.Now().AddDate(1, 0, 0))
		for _, row := range due {
			if row.Kind != checks.RequestKind {
				continue
			}
			by, err := checks.RequestedBy(row.Payload)
			if err != nil || by == "" {
				t.Errorf("the request names %q: %v", by, err)
			}
			if err := u.Outbox().MarkDispatched(ctx, row.ID, "task", e.clock.Now().AddDate(1, 0, 0)); err != nil {
				return err
			}
			if _, _, err := u.Outbox().Lease(ctx, row.ID, "test", e.clock.Now(), e.clock.Now().Add(time.Minute)); err != nil {
				return err
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.runCatalogue()
	if r := e.call(http.MethodGet, "/v1/checks", e.as(authz.Bookkeeper), nil); r.Body["run_requested"] != false || r.Body["last_run"] == nil {
		t.Errorf("after the run: %s", r.Raw)
	}
	late := e.call(http.MethodPost, "/v1/checks/runs", e.as(authz.Bookkeeper), nil)
	if late.Status != http.StatusTooManyRequests || late.problemCode() != CodeRateLimited || late.Header.Get("Retry-After") == "" {
		t.Errorf("a second Run now in the minute = %d %s", late.Status, late.problemCode())
	}
}

// A check of the catalogue that the close does not run itself and that found ✖ refuses the close (design D2).
// req: FR-CTL-01
// req: FR-GL-07
func TestACatalogueProblemRefusesTheClose(t *testing.T) {
	e := newEnv(t)
	e.runCatalogue() // the test books have none of the system accounts
	oct := e.books.Oct.ID.String()
	tag := e.call(http.MethodGet, "/v1/periods/"+oct+"/close", e.as(authz.Accountant), nil).Header.Get("ETag")
	r := e.call(http.MethodPost, "/v1/periods/"+oct+"/close-checks", e.as(authz.Accountant), nil, "If-Match", tag)
	if r.Status != http.StatusOK {
		t.Fatalf("running the close checks = %d: %s", r.Status, r.Raw)
	}
	cc, _ := r.Body["checks"].(map[string]any)
	if cc["clean"] != false || !strings.Contains(r.Raw, "checks.setup.system-accounts-exist") || !strings.Contains(r.Raw, string(closing.Failed)) {
		t.Errorf("the close's checks: %s", r.Raw)
	}
}
