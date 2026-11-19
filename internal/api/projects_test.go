//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/project"
	"github.com/cognizhi/fyniq/internal/domain/projectbilling"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// projectsOn switches Projects and time on for e's organisation, once (FR-ORG-06).
func (e *env) projectsOn() {
	e.t.Helper()
	ctl := e.as(authz.Controller)
	if g := e.call(http.MethodGet, "/v1/organisation/settings/features.projects", ctl, nil); g.str("value") == "true" {
		return
	}
	if r := e.call(http.MethodPatch, "/v1/organisation/settings/features.projects", ctl, map[string]any{"value": "true"}, "If-Match", `"0"`); r.Status != 200 {
		e.t.Fatalf("switching projects on: %d %s", r.Status, r.Raw)
	}
}

// person is a session of a member holding roles.
func (e *env) person(user uuid.UUID, roles ...authz.Role) string {
	return e.principal(authn.Principal{Subject: "person-" + user.String(), UserID: user, Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: roles})
}

// customerOf adds a customer contact and returns its ID.
func (e *env) customerOf(name string) string {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": name, "customer": true})
	if r.Status != 201 {
		e.t.Fatalf("a customer: %d %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// While Projects and time are switched off (the default) every operation refuses, whatever the caller's rights; switched
// on, they work, and the Project dimension is made with the first project (FR-ORG-06, FR-UX-40d).
// req: FR-ORG-06
// req: FR-UX-40
// req: FR-PRJ-01
func TestProjectsRefuseWhileSwitchedOff(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	for _, c := range []struct{ method, path, op string }{
		{http.MethodGet, "/v1/projects", "listProjects"},
		{http.MethodGet, "/v1/projects/" + uuid.NewString(), "getProject"},
		{http.MethodGet, "/v1/projects/" + uuid.NewString() + "/tasks", "listProjectTasks"},
	} {
		e.problemIs(e.call(c.method, c.path, e.as(authz.Controller), nil), c.op, 403, CodeProjectsOff)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "ACME-01", "name": "Acme", "currency": "SGD", "billing_method": "internal"}),
		"createProject", 403, CodeProjectsOff)
	if d := e.call(http.MethodGet, "/v1/dimensions", acc, nil); len(d.Body["items"].([]any)) != 0 {
		t.Fatalf("a refused project made a dimension: %s", d.Raw)
	}
	e.projectsOn()
	r := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "ACME-01", "name": "Acme", "currency": "SGD", "billing_method": "internal"})
	if r.Status != 201 || r.str("code") != "ACME-01" || r.str("status") != "active" {
		t.Fatalf("a project once switched on: %d %s", r.Status, r.Raw)
	}
	if d := e.call(http.MethodGet, "/v1/dimensions", acc, nil); len(d.Body["items"].([]any)) != 1 || d.Body["items"].([]any)[0].(map[string]any)["kind"] != "project" {
		t.Errorf("the first project makes the Project dimension: %s", d.Raw)
	}
}

// A project end to end (FR-PRJ-01): added as a value of the Project dimension with its customer, budget and people, with
// every problem at once; read by those who see every project and, narrowed, by a member and a manager; changed at its
// version; closed, which puts its value out of use; a value added through the dimensions is set up with If-Match "0".
// A manager who holds no role that approves time is warned about, and gets no right from being named (design D4).
// req: FR-PRJ-01
// req: FR-DIM-01
// req: FR-INT-09
// req: FR-INT-14
// req: ENG-22
// req: FR-UX-40
func TestProjectsAreKeptAndNarrowedToTheirPeople(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc, aud := e.as(authz.Accountant), e.as(authz.Auditor)
	acme := e.customerOf("Acme Pte Ltd")
	priya := e.joinedIn(e.books.Org, "Priya Nair", "employee")
	wei := e.joinedIn(e.books.Org, "Wei Lim", "approver")
	sam := e.joinedIn(e.books.Org, "Sam Tan", "employee")
	// Wei is named a manager and holds the Approver role; Sam, an Employee, is named a manager too and cannot approve.
	body := func(extra map[string]any) map[string]any {
		m := map[string]any{"code": "ACME-01", "name": "Acme rollout", "customer_id": acme, "currency": "SGD", "billing_method": "time_and_materials",
			"starts_on": "2026-11-01", "ends_on": "2027-02-28", "budget_minutes": 24000, "budget_amount": "60000.00",
			"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}, {"user_id": sam.String(), "manager": true}}}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	// Every problem at once, in plain words with codes.
	bad := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "X Y", "name": "", "currency": "XXX", "billing_method": "internal", "customer_id": acme,
		"starts_on": "2027-03-01", "ends_on": "2027-02-01", "budget_amount": "12.345",
		"people": []map[string]any{{"user_id": uuid.NewString()}}})
	e.problemIs(bad, "createProject", 422, CodeInvalid)
	f := fields(bad)
	for field, code := range map[string]string{"body.currency": project.CodeCurrency, "body.customer_id": project.CodeCustomer, "body.ends_on": project.CodeDates,
		"body.people[0].user_id": project.CodePeople, "body.code": "DIM-03", "body.name": "DIM-02"} {
		if f[field] != code {
			t.Errorf("%s: %q, want %s (%v)", field, f[field], code, f)
		}
	}
	if !strings.Contains(bad.Raw, "an internal project has no customer") {
		t.Errorf("the words: %s", bad.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/projects", acc, body(map[string]any{"people": []map[string]any{{"user_id": priya.String()}}})), "createProject", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/projects", e.as(authz.Bookkeeper), body(nil)), "createProject", 403, CodeForbidden)

	if p := e.call(http.MethodPost, "/v1/projects", acc, body(nil), "Prefer", "return=preview"); p.Status != 200 || p.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/projects", aud, nil); len(l.Body["items"].([]any)) != 0 {
		t.Fatalf("a preview saved a project: %s", l.Raw)
	}
	made := e.call(http.MethodPost, "/v1/projects", acc, body(nil))
	if made.Status != 201 || made.str("customer_name") != "Acme Pte Ltd" || made.str("budget_amount") != "60000.00" || made.Body["budget_minutes"] != float64(24000) ||
		made.Body["version"] != float64(1) || made.Header.Get("ETag") != etag(1) || len(made.Body["people"].([]any)) != 3 {
		t.Fatalf("the project: %d %s", made.Status, made.Raw)
	}
	if w := made.Body["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "Sam Tan is named a manager but holds no role that approves time") {
		t.Errorf("a manager without a role warns: %v", w)
	}
	people := map[string]map[string]any{}
	for _, x := range made.Body["people"].([]any) {
		people[x.(map[string]any)["display_name"].(string)] = x.(map[string]any)
	}
	if people["Wei Lim"]["can_approve"] != true || people["Sam Tan"]["can_approve"] != false || people["Priya Nair"]["manager"] != false {
		t.Errorf("who can approve: %v", people)
	}
	id := made.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/projects", acc, body(map[string]any{"name": "Another"})), "createProject", 422, CodeInvalid)

	// The project's value is the dimension's: lines can already name it.
	vals := e.call(http.MethodGet, "/v1/dimension-values", acc, nil)
	if !strings.Contains(vals.Raw, `"id":"`+id+`"`) || !strings.Contains(vals.Raw, `"code":"ACME-01"`) {
		t.Errorf("the project's value among the dimension's: %s", vals.Raw)
	}

	// The people a project can name: names and whether they can approve, for those who keep projects, who cannot list the members.
	ppl := e.call(http.MethodGet, "/v1/project-people", acc, nil)
	if ppl.Status != 200 || !strings.Contains(ppl.Raw, `"display_name":"Wei Lim"`) || !strings.Contains(ppl.Raw, `"can_approve":true`) {
		t.Errorf("the people a project can name: %d %s", ppl.Status, ppl.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/members", acc, nil), "listMembers", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/project-people", e.as(authz.Auditor), nil), "listProjectPeople", 403, CodeForbidden)

	// Who sees what: the Auditor every project; a member only theirs; a person on none, none.
	if g := e.call(http.MethodGet, "/v1/projects/"+id, aud, nil); g.Status != 200 {
		t.Errorf("the Auditor reads it: %d %s", g.Status, g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/projects/"+id, e.person(priya, authz.Employee), nil); g.Status != 200 {
		t.Errorf("a member reads it: %d %s", g.Status, g.Raw)
	}
	stranger := e.person(e.joinedIn(e.books.Org, "Una Koh", "employee"), authz.Employee)
	e.problemIs(e.call(http.MethodGet, "/v1/projects/"+id, stranger, nil), "getProject", 404, CodeNotFound)
	if l := e.call(http.MethodGet, "/v1/projects", stranger, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("a person on no project lists %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/projects", e.person(wei, authz.Approver), nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("a manager lists their project: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/projects/"+id+"/tasks", stranger, nil), "listProjectTasks", 404, CodeNotFound)

	// Changed at the version read: stale and missing ETags refused; the people stay when left out.
	path := "/v1/projects/" + id
	change := func(extra map[string]any) map[string]any {
		m := map[string]any{"name": "Acme rollout", "customer_id": acme, "status": "active", "billing_method": "time_and_materials", "budget_minutes": 24000}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	e.problemIs(e.call(http.MethodPut, path, acc, change(nil)), "replaceProject", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPut, path, acc, change(nil), "If-Match", etag(9)), "replaceProject", 412, CodeStale)
	hold := e.call(http.MethodPut, path, acc, change(map[string]any{"status": "on_hold", "name": "Acme rollout phase 1"}), "If-Match", made.Header.Get("ETag"))
	if hold.Status != 200 || hold.str("status") != "on_hold" || hold.str("name") != "Acme rollout phase 1" || hold.Body["version"] != float64(2) ||
		len(hold.Body["people"].([]any)) != 3 || hold.str("currency") != "SGD" {
		t.Fatalf("on hold, renamed: %d %s", hold.Status, hold.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, acc, change(map[string]any{"currency": "USD"}), "If-Match", hold.Header.Get("ETag")), "replaceProject", 422, CodeInvalid)
	closed := e.call(http.MethodPut, path, acc, change(map[string]any{"status": "closed"}), "If-Match", hold.Header.Get("ETag"))
	if closed.Status != 200 || closed.str("status") != "closed" {
		t.Fatalf("closed: %d %s", closed.Status, closed.Raw)
	}
	if v := e.call(http.MethodGet, "/v1/dimension-values", acc, nil); !strings.Contains(v.Raw, `"active":false`) {
		t.Errorf("closing puts the value out of use: %s", v.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": id, "name": "Handover"}), "createProjectTask", 409, storage.CodeProject)
	reopened := e.call(http.MethodPut, path, acc, change(map[string]any{"status": "active", "people": []map[string]any{{"user_id": wei.String(), "manager": true}}}), "If-Match", closed.Header.Get("ETag"))
	if reopened.Status != 200 || len(reopened.Body["people"].([]any)) != 1 || len(reopened.Body["warnings"].([]any)) != 0 {
		t.Fatalf("reopened with one manager: %d %s", reopened.Status, reopened.Raw)
	}

	// A value added through the dimensions is an unconfigured project until it is set up with If-Match "0".
	dim := e.call(http.MethodGet, "/v1/dimensions", acc, nil).Body["items"].([]any)[0].(map[string]any)["id"].(string)
	raw := e.call(http.MethodPost, "/v1/dimension-values", acc, map[string]any{"dimension_id": dim, "code": "OLD-1", "name": "Old project"})
	got := e.call(http.MethodGet, "/v1/projects/"+raw.str("id"), acc, nil)
	if got.Body["configured"] != false || got.Body["version"] != float64(0) || got.Header.Get("ETag") != etag(0) {
		t.Fatalf("an unconfigured project: %s", got.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": raw.str("id"), "name": "Work"}), "createProjectTask", 422, CodeInvalid)
	setup := e.call(http.MethodPut, "/v1/projects/"+raw.str("id"), acc, map[string]any{"name": "Old project", "currency": "SGD", "status": "active", "billing_method": "internal"}, "If-Match", etag(0))
	if setup.Status != 200 || setup.Body["configured"] != true || setup.Body["version"] != float64(1) {
		t.Fatalf("set up: %d %s", setup.Status, setup.Raw)
	}
}

// A project's tasks (FR-PRJ-05): a name unique among the live ones whatever the case, billable unless said otherwise, put
// out of use and never deleted, at the version read.
// req: FR-PRJ-05
// req: FR-PRJ-01
// req: FR-INT-09
func TestProjectTasks(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "INT-1", "name": "Internal tools", "currency": "SGD", "billing_method": "internal"})
	path, pid := "/v1/projects/"+p.str("id")+"/tasks", p.str("id")
	task := func(tok string, name string, extra ...map[string]any) response {
		b := map[string]any{"project_id": pid, "name": name}
		for _, x := range extra {
			for k, v := range x {
				b[k] = v
			}
		}
		return e.call(http.MethodPost, "/v1/project-tasks", tok, b)
	}
	e.problemIs(task(e.as(authz.Employee), "Build"), "createProjectTask", 403, CodeForbidden)
	t1 := task(acc, " Data migration ")
	if t1.Status != 201 || t1.str("name") != "Data migration" || t1.Body["billable"] != true || t1.Header.Get("ETag") != etag(1) {
		t.Fatalf("a task: %d %s", t1.Status, t1.Raw)
	}
	e.problemIs(task(acc, "data MIGRATION"), "createProjectTask", 422, CodeInvalid)
	t2 := task(acc, "Support", map[string]any{"billable": false})
	if t2.Body["billable"] != false {
		t.Errorf("a task that is not billable: %s", t2.Raw)
	}
	tp := "/v1/project-tasks/" + t1.str("id")
	e.problemIs(e.call(http.MethodPut, tp, acc, map[string]any{"name": "Migration"}), "replaceProjectTask", 428, CodeIfMatchRequired)
	off := e.call(http.MethodPut, tp, acc, map[string]any{"name": "Data migration", "active": false}, "If-Match", t1.Header.Get("ETag"))
	if off.Status != 200 || off.Body["active"] != false || off.Body["version"] != float64(2) {
		t.Fatalf("put out of use: %d %s", off.Status, off.Raw)
	}
	e.problemIs(e.call(http.MethodPut, tp, acc, map[string]any{"name": "x"}, "If-Match", t1.Header.Get("ETag")), "replaceProjectTask", 412, CodeStale)
	again := task(acc, "Data migration")
	if again.Status != 201 {
		t.Errorf("a name of a task out of use is free again: %d %s", again.Status, again.Raw)
	}
	l := e.call(http.MethodGet, path, e.as(authz.Auditor), nil)
	if items := l.Body["items"].([]any); len(items) != 3 || items[2].(map[string]any)["active"] != false {
		t.Errorf("the tasks, those out of use last: %s", l.Raw)
	}
}

// A person who reads only the projects they are on (an Employee, the Approver) is answered not found for the matrix's project,
// which they are not on: Appendix D allows the read, and the record decides what they see (design D4).
func init() {
	pastPolicy["getProject"], pastPolicy["listProjectTasks"] = CodeNotFound, CodeNotFound
	pastPolicy["draftProjectInvoice"] = projectbilling.CodeNothing // the matrix project has no customer to bill
}

var matrixProjects = map[string]func(e *env, tok string) (response, func() bool){
	"listProjects": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		return e.call(http.MethodGet, "/v1/projects", tok, nil), nil
	},
	"getProject": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		return e.call(http.MethodGet, "/v1/projects/"+matrixProject(e), tok, nil), nil
	},
	"listProjectTasks": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		return e.call(http.MethodGet, "/v1/projects/"+matrixProject(e)+"/tasks", tok, nil), nil
	},
	"listProjectPeople": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		return e.call(http.MethodGet, "/v1/project-people", tok, nil), nil
	},
	"createProject": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		code := "M" + strings.ToUpper(uuid.NewString()[:8])
		r := e.call(http.MethodPost, "/v1/projects", tok, map[string]any{"code": code, "name": code, "currency": "SGD", "billing_method": "internal"})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/projects?page_size=500", e.as(authz.Auditor), nil).Raw, `"code":"`+code+`"`)
		}
	},
	"replaceProject": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		id := matrixProject(e)
		name := "Project " + uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/projects/"+id, tok, map[string]any{"name": name, "status": "active", "billing_method": "internal"}, "If-Match", "*")
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/projects/"+id, e.as(authz.Auditor), nil).Raw, name)
		}
	},
	"createProjectTask": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		id := matrixProject(e)
		name := "Task " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/project-tasks", tok, map[string]any{"project_id": id, "name": name})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/projects/"+id+"/tasks", e.as(authz.Auditor), nil).Raw, name)
		}
	},
	"replaceProjectTask": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		id := matrixProject(e)
		task := e.call(http.MethodPost, "/v1/project-tasks", e.as(authz.Accountant), map[string]any{"project_id": id, "name": "Task " + uuid.NewString()[:8]}).str("id")
		name := "Renamed " + uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/project-tasks/"+task, tok, map[string]any{"name": name}, "If-Match", "*")
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/projects/"+id+"/tasks", e.as(authz.Auditor), nil).Raw, name)
		}
	},
}

// matrixProject is an internal project of the matrix's organisation, made once.
func matrixProject(e *env) string {
	e.projectsOn()
	l := e.call(http.MethodGet, "/v1/projects?page_size=500", e.as(authz.Auditor), nil)
	for _, x := range l.Body["items"].([]any) {
		if p := x.(map[string]any); p["code"] == "MATRIX-PRJ" {
			return p["id"].(string)
		}
	}
	r := e.call(http.MethodPost, "/v1/projects", e.as(authz.Accountant), map[string]any{"code": "MATRIX-PRJ", "name": "Matrix project", "currency": "SGD", "billing_method": "internal"})
	if r.Status != 201 {
		e.t.Fatalf("the matrix's project: %d %s", r.Status, r.Raw)
	}
	return r.str("id")
}
