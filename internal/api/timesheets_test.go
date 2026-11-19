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
	"github.com/cognizhi/fyniq/internal/domain/timesheet"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// timeTeam is a project with a manager who holds the Approver role (Wei), a manager who holds none (Sam), a member (Priya) and
// a Controller's person (Una), each a member of e's organisation.
type timeTeam struct {
	project, task        string
	priya, wei, sam, una uuid.UUID
	week                 string
}

func (e *env) timeTeam(code string) timeTeam {
	e.t.Helper()
	e.projectsOn()
	acc := e.as(authz.Accountant)
	t := timeTeam{priya: e.joinedIn(e.books.Org, "Priya "+code, "employee"), wei: e.joinedIn(e.books.Org, "Wei "+code, "approver"),
		sam: e.joinedIn(e.books.Org, "Sam "+code, "employee"), una: e.joinedIn(e.books.Org, "Una "+code, "controller"), week: "2090-01-02"}
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": code, "name": "Project " + code, "currency": "SGD", "billing_method": "time_and_materials",
		"people": []map[string]any{{"user_id": t.priya.String()}, {"user_id": t.wei.String(), "manager": true}, {"user_id": t.sam.String(), "manager": true}}})
	if p.Status != 201 {
		e.t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	t.project = p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": t.project, "name": "Data migration"})
	t.task = task.str("id")
	return t
}

func (t timeTeam) entry(day string, minutes int, extra ...map[string]any) map[string]any {
	m := map[string]any{"project_id": t.project, "task_id": t.task, "work_date": day, "minutes": minutes}
	for _, x := range extra {
		for k, v := range x {
			m[k] = v
		}
	}
	return m
}

// weekOf reads a person's week and returns it with its ETag.
func (e *env) weekOf(tok, week string) (response, string) {
	e.t.Helper()
	r := e.call(http.MethodGet, "/v1/timesheet/"+week, tok, nil)
	if r.Status != 200 {
		e.t.Fatalf("the week %s: %d %s", week, r.Status, r.Raw)
	}
	return r, r.Header.Get("ETag")
}

// save saves a week's entries, as the person read it.
func (e *env) saveWeek(tok, week string, entries ...map[string]any) response {
	e.t.Helper()
	_, tag := e.weekOf(tok, week)
	return e.call(http.MethodPut, "/v1/timesheet/"+week, tok, map[string]any{"entries": entries}, "If-Match", tag)
}

// A person logs a week, submits it, and the project's manager approves it: nobody decides their own time, a person named
// manager without the Approver role decides nothing, and another project's manager cannot decide it (FR-PRJ-05, FR-PRJ-07; UC-PRJ-01).
// req: FR-PRJ-05
// req: FR-PRJ-07
// req: FR-PRJ-02
// req: FR-UX-40
// req: FR-INT-09
func TestAWeekOfTimeIsLoggedSubmittedAndApprovedByTheProjectsManager(t *testing.T) {
	e := newEnv(t)
	team := e.timeTeam("TT-1")
	priya := e.person(team.priya, authz.Employee)
	week := team.week

	// The week starts empty; a Monday is needed; a person on no project logs nothing.
	r, tag := e.weekOf(priya, week)
	if len(r.Body["entries"].([]any)) != 0 || len(r.Body["days"].([]any)) != 7 || len(r.Body["projects"].([]any)) != 1 || r.Body["total_minutes"] != float64(0) {
		t.Fatalf("an empty week: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/timesheet/2090-01-03", priya, nil), "getTimesheet", 422, CodeInvalid)
	stranger := e.person(e.joinedIn(e.books.Org, "Una Koh", "employee"), authz.Employee)
	if x := e.saveWeek(stranger, week, team.entry("2090-01-03", 60)); x.Status != 422 || fields(x)["body.entries[0].project_id"] != timesheet.CodeProject {
		t.Errorf("a person not on the project: %d %s", x.Status, x.Raw)
	}

	// Every problem at once, in plain words; a preview saves nothing.
	bad := e.call(http.MethodPut, "/v1/timesheet/"+week, priya, map[string]any{"entries": []map[string]any{
		team.entry("2090-01-03", 1500), team.entry("2090-02-07", 60), {"project_id": team.project, "task_id": uuid.NewString(), "work_date": "2090-01-04", "minutes": 30}}}, "If-Match", tag)
	e.problemIs(bad, "saveTimesheet", 422, CodeInvalid)
	bad = e.call(http.MethodPut, "/v1/timesheet/"+week, priya, map[string]any{"entries": []map[string]any{
		team.entry("2090-01-03", 60), team.entry("2090-02-07", 60), {"project_id": team.project, "task_id": uuid.NewString(), "work_date": "2090-01-04", "minutes": 30}}}, "If-Match", tag)
	if f := fields(bad); f["body.entries[1].work_date"] != timesheet.CodeDay || f["body.entries[2].task_id"] != timesheet.CodeTask {
		t.Errorf("every problem at once: %v", f)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/timesheet/"+week, priya, map[string]any{"entries": []map[string]any{}}), "saveTimesheet", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPut, "/v1/timesheet/"+week, priya, map[string]any{"entries": []map[string]any{}}, "If-Match", etag(9, 9, 9)), "saveTimesheet", 412, CodeStale)
	if p := e.call(http.MethodPut, "/v1/timesheet/"+week, priya, map[string]any{"entries": []map[string]any{team.entry("2090-01-03", 135)}}, "If-Match", tag, "Prefer", "return=preview"); p.Status != 200 ||
		p.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	if r, _ := e.weekOf(priya, week); len(r.Body["entries"].([]any)) != 0 {
		t.Fatalf("a preview saved an entry: %s", r.Raw)
	}

	// UC-PRJ-01: 2 h 15 min on Tuesday, a day in the grid, and a note.
	saved := e.saveWeek(priya, week, team.entry("2090-01-03", 135, map[string]any{"note": "Migrated the customer tables"}), team.entry("2090-01-04", 480))
	if saved.Status != 200 || saved.Body["total_minutes"] != float64(615) || len(saved.Body["entries"].([]any)) != 2 {
		t.Fatalf("a saved week: %d %s", saved.Status, saved.Raw)
	}
	first := saved.Body["entries"].([]any)[0].(map[string]any)
	if first["minutes"] != float64(135) || first["status"] != "draft" || first["billable"] != true || first["note"] != "Migrated the customer tables" {
		t.Errorf("the first entry: %v", first)
	}
	// Changing and removing: the entry left out goes, the one named changes at its version.
	saved = e.saveWeek(priya, week, map[string]any{"id": first["id"], "project_id": team.project, "task_id": team.task, "work_date": "2090-01-03", "minutes": 150})
	if saved.Body["total_minutes"] != float64(150) || len(saved.Body["entries"].([]any)) != 1 {
		t.Fatalf("changed and removed: %s", saved.Raw)
	}
	saved = e.saveWeek(priya, week, map[string]any{"id": first["id"], "project_id": team.project, "task_id": team.task, "work_date": "2090-01-03", "minutes": 150}, team.entry("2090-01-04", 480))

	// The timer: started, one at a time, stopped into a draft entry of at least a minute on the day it started.
	start := e.call(http.MethodPost, "/v1/timer/start", priya, map[string]any{"project_id": team.project, "task_id": team.task, "note": "Support"})
	if start.Status != 200 || start.Body["running"] != true {
		t.Fatalf("the timer: %d %s", start.Status, start.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/timer/start", priya, map[string]any{"project_id": team.project, "task_id": team.task}), "startTimer", 409, storage.CodeTime)
	e.clock.Advance(2*time.Hour + 15*time.Minute)
	if g := e.call(http.MethodGet, "/v1/timer", priya, nil); g.Body["timer"].(map[string]any)["elapsed_minutes"] != float64(135) {
		t.Errorf("the timer shows 2 h 15 min: %s", g.Raw)
	}
	stop := e.call(http.MethodPost, "/v1/timer/stop", priya, nil)
	if stop.Status != 200 || stop.Body["entry"].(map[string]any)["minutes"] != float64(135) || stop.Body["entry"].(map[string]any)["note"] != "Support" {
		t.Fatalf("the timer stopped: %d %s", stop.Status, stop.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/timer/stop", priya, nil), "stopTimer", 404, CodeNotFound)

	// Submit the week; an entry then cannot change; a submit with nothing open is refused.
	cur, tag := e.weekOf(priya, week)
	if len(cur.Body["entries"].([]any)) == 0 {
		// The timer ran on the clock's day, not this week: its entry is in another week.
		t.Log("the timer's entry is in the week of the clock's day")
	}
	sub := e.call(http.MethodPost, "/v1/timesheet/"+week+"/submit", priya, map[string]any{}, "If-Match", tag)
	if sub.Status != 200 || len(sub.Body["submissions"].([]any)) != 1 {
		t.Fatalf("submitted: %d %s", sub.Status, sub.Raw)
	}
	s := sub.Body["submissions"].([]any)[0].(map[string]any)
	if s["status"] != "submitted" || s["minutes"] != float64(630) || s["can_decide"] != false {
		t.Errorf("the submission: %v", s)
	}
	locked := e.saveWeek(priya, week, map[string]any{"id": first["id"], "project_id": team.project, "task_id": team.task, "work_date": "2090-01-03", "minutes": 200})
	if locked.Status != 422 || fields(locked)["body.entries[0].id"] != timesheet.CodeLocked {
		t.Errorf("a submitted entry cannot change: %d %s", locked.Status, locked.Raw)
	}
	_, tag = e.weekOf(priya, week)
	e.problemIs(e.call(http.MethodPost, "/v1/timesheet/"+week+"/submit", priya, map[string]any{}, "If-Match", tag), "submitTimesheet", 422, timesheet.CodeSubmit)

	// Who may decide: a manager holding the Approver role, the Controller; never the person, never a manager without the role
	// or another project's manager.
	subID := s["id"].(string)
	wei, sam, una := e.person(team.wei, authz.Approver), e.person(team.sam, authz.Employee), e.person(team.una, authz.Controller)
	elsewhere := e.person(e.joinedIn(e.books.Org, "Otto", "approver"), authz.Approver)
	waiting := e.call(http.MethodGet, "/v1/timesheet-submissions?whose=waiting_for_me", wei, nil)
	if items := waiting.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["can_decide"] != true || items[0].(map[string]any)["person_name"] != "Priya TT-1" {
		t.Fatalf("Wei's waiting list: %s", waiting.Raw)
	}
	if w := e.call(http.MethodGet, "/v1/timesheet-submissions?whose=waiting_for_me", elsewhere, nil); len(w.Body["items"].([]any)) != 0 {
		t.Errorf("another project's manager sees none waiting: %s", w.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/timesheet-submissions/"+subID, elsewhere, nil), "getTimesheetSubmission", 404, CodeNotFound)
	detail := e.call(http.MethodGet, "/v1/timesheet-submissions/"+subID, wei, nil)
	if detail.Status != 200 || len(detail.Body["entries"].([]any)) != 2 || detail.Header.Get("ETag") == "" {
		t.Fatalf("the submission: %d %s", detail.Status, detail.Raw)
	}
	tagS := detail.Header.Get("ETag")
	approve := func(tok, tag string) response {
		return e.call(http.MethodPost, "/v1/timesheet-submissions/"+subID+"/approve", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString())
	}
	e.problemIs(approve(sam, tagS), "approveTimesheetSubmission", 403, CodeForbidden) // named manager, no Approver role: Appendix D gives an Employee no A
	e.problemIs(approve(priya, tagS), "approveTimesheetSubmission", 403, CodeForbidden)
	if x := approve(elsewhere, tagS); x.Status != 404 {
		t.Errorf("another project's manager: %d %s", x.Status, x.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/timesheet-submissions/"+subID+"/return", wei, map[string]any{}, "If-Match", tagS, "Idempotency-Key", uuid.NewString()), "returnTimesheetSubmission", 422, CodeInvalid)
	returned := e.call(http.MethodPost, "/v1/timesheet-submissions/"+subID+"/return", wei, map[string]any{"comment": "Add what was done on Wednesday."}, "If-Match", tagS, "Idempotency-Key", uuid.NewString())
	if returned.Status != 200 || returned.str("status") != "returned" || returned.str("comment") != "Add what was done on Wednesday." {
		t.Fatalf("returned: %d %s", returned.Status, returned.Raw)
	}
	// Returned: the entries are the person's again, and a new submission follows.
	back, tag := e.weekOf(priya, week)
	for _, x := range back.Body["entries"].([]any) {
		if x.(map[string]any)["status"] != "returned" {
			t.Fatalf("a returned entry: %v", x)
		}
	}
	e.problemIs(e.call(http.MethodPost, "/v1/timesheet-submissions/"+subID+"/return", wei, map[string]any{"comment": "again"}, "If-Match", returned.Header.Get("ETag"), "Idempotency-Key", uuid.NewString()),
		"returnTimesheetSubmission", 403, timesheet.CodeDecide)
	sub = e.call(http.MethodPost, "/v1/timesheet/"+week+"/submit", priya, map[string]any{}, "If-Match", tag)
	subID = sub.Body["submissions"].([]any)[0].(map[string]any)["id"].(string)
	if subID == "" || len(sub.Body["submissions"].([]any)) != 2 {
		t.Fatalf("a second submission keeps the first in the history: %s", sub.Raw)
	}
	// The Controller approves any project's time.
	for _, x := range sub.Body["submissions"].([]any) {
		if m := x.(map[string]any); m["status"] == "submitted" {
			subID = m["id"].(string)
		}
	}
	tagS = e.call(http.MethodGet, "/v1/timesheet-submissions/"+subID, una, nil).Header.Get("ETag")
	ok := approve(una, tagS)
	if ok.Status != 200 || ok.str("status") != "approved" || ok.str("decided_by_name") != "Una TT-1" {
		t.Fatalf("approved by the Controller: %d %s", ok.Status, ok.Raw)
	}
	e.problemIs(approve(una, ok.Header.Get("ETag")), "approveTimesheetSubmission", 403, timesheet.CodeDecide)

	// An approved entry never changes; a correction is a reversing entry plus a new one, approved again.
	done, tag := e.weekOf(priya, week)
	approved := done.Body["entries"].([]any)[0].(map[string]any)
	if approved["status"] != "approved" {
		t.Fatalf("approved entries: %s", done.Raw)
	}
	if x := e.saveWeek(priya, week, map[string]any{"id": approved["id"], "project_id": team.project, "task_id": team.task, "work_date": approved["work_date"], "minutes": 90}); x.Status != 422 {
		t.Errorf("an approved entry changes: %d %s", x.Status, x.Raw)
	}
	rev := e.call(http.MethodPost, "/v1/time-entries/"+approved["id"].(string)+"/reverse", priya, nil, "Idempotency-Key", uuid.NewString())
	if rev.Status != 201 || rev.Body["minutes"] != -approved["minutes"].(float64) || rev.str("reverses_id") != approved["id"] || rev.str("status") != "draft" {
		t.Fatalf("the reversing entry: %d %s", rev.Status, rev.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/time-entries/"+approved["id"].(string)+"/reverse", priya, nil, "Idempotency-Key", uuid.NewString()), "reverseTimeEntry", 409, timesheet.CodeReverse)
	e.problemIs(e.call(http.MethodPost, "/v1/time-entries/"+rev.str("id")+"/reverse", priya, nil, "Idempotency-Key", uuid.NewString()), "reverseTimeEntry", 409, timesheet.CodeReverse)
	_ = tag
	fixed, tag := e.weekOf(priya, week)
	total := fixed.Body["total_minutes"].(float64)
	if total != 630-approved["minutes"].(float64) {
		t.Errorf("a reversal nets the entry out: %v", total)
	}
	if after := e.call(http.MethodPost, "/v1/timesheet/"+week+"/submit", priya, map[string]any{}, "If-Match", tag); after.Status != 200 {
		t.Errorf("the correction is submitted again: %d %s", after.Status, after.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/timer/drop", priya, nil), "dropTimer", 404, CodeNotFound)
}

// Self-approval is refused whatever the roles: a person who holds the Approver role, manages the project and logs time on it
// does not approve their own week, and the database refuses it too (FR-PRJ-07; FY602).
// req: FR-PRJ-07
// req: FR-UX-40
func TestNobodyApprovesTheirOwnTime(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	zed := e.joinedIn(e.books.Org, "Zed", "approver", "employee")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "SELF-1", "name": "Self", "currency": "SGD", "billing_method": "internal",
		"people": []map[string]any{{"user_id": zed.String(), "manager": true}}})
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": p.str("id"), "name": "Work"}).str("id")
	tok := e.person(zed, authz.Approver, authz.Employee)
	e.saveWeek(tok, "2090-01-09", map[string]any{"project_id": p.str("id"), "task_id": task, "work_date": "2090-01-10", "minutes": 60})
	_, tag := e.weekOf(tok, "2090-01-09")
	subr := e.call(http.MethodPost, "/v1/timesheet/2090-01-09/submit", tok, map[string]any{}, "If-Match", tag)
	if subr.Status != 200 {
		t.Fatalf("submit: %d %s", subr.Status, subr.Raw)
	}
	sub := subr.Body["submissions"].([]any)[0].(map[string]any)
	got := e.call(http.MethodGet, "/v1/timesheet-submissions/"+sub["id"].(string), tok, nil)
	if got.Body["can_decide"] != false {
		t.Errorf("their own week is not theirs to decide: %s", got.Raw)
	}
	r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+sub["id"].(string)+"/approve", tok, map[string]any{}, "If-Match", got.Header.Get("ETag"), "Idempotency-Key", uuid.NewString())
	e.problemIs(r, "approveTimesheetSubmission", 403, timesheet.CodeDecide)
	if !strings.Contains(r.Raw, "nobody decides their own time") {
		t.Errorf("the words: %s", r.Raw)
	}
	// The database repeats it, whatever the API did.
	if err := e.store.Do(storage.WithAudit(t.Context(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "attacker"}}), e.books.Org,
		func(ctx2 context.Context, u storage.UnitOfWork) error {
			_, err := u.Timesheets().Decide(ctx2, uuid.MustParse(sub["id"].(string)), 1, zed, true, "", e.clock.Now())
			return err
		}); storage.CodeOf(err) != storage.CodeTime {
		t.Errorf("the database approves their own week: %v", err)
	}
}

// matrixTime is the team the matrix's time rows act on, made once: its person and manager are members, and the principal of a
// row is that person or manager with the roles of the role under test.
var matrixTeam *orgTeam

// orgTeam is a time team in an organisation of its own: the matrix's organisation keeps one member, so the founder bootstrap the
// other rows rely on holds.
type orgTeam struct {
	timeTeam
	org uuid.UUID
}

func matrixTimeTeam(e *env) orgTeam {
	if matrixTeam == nil {
		org := porttest.NewBooks(e.t, e.store, "Time matrix "+uuid.NewString()[:8]+" Pte Ltd").Org
		on := e.principalIn(org, authz.Controller)
		if r := e.call(http.MethodPatch, "/v1/organisation/settings/features.projects", on, map[string]any{"value": "true"}, "If-Match", `"0"`); r.Status != 200 {
			e.t.Fatalf("switching projects on: %d %s", r.Status, r.Raw)
		}
		acc := e.principalIn(org, authz.Accountant)
		t := timeTeam{priya: e.joinedIn(org, "Priya MX", "employee"), wei: e.joinedIn(org, "Wei MX", "approver"), sam: e.joinedIn(org, "Sam MX", "employee"),
			una: e.joinedIn(org, "Una MX", "controller"), week: "2090-01-02"}
		p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "MX-T", "name": "Matrix time", "currency": "SGD", "billing_method": "time_and_materials",
			"people": []map[string]any{{"user_id": t.priya.String()}, {"user_id": t.wei.String(), "manager": true}}})
		if p.Status != 201 {
			e.t.Fatalf("the matrix's project: %d %s", p.Status, p.Raw)
		}
		t.project = p.str("id")
		t.task = e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": t.project, "name": "Work"}).str("id")
		matrixTeam = &orgTeam{timeTeam: t, org: org}
	}
	return *matrixTeam
}

// fresh is a session of a member of the team's organisation holding roles, as the matrix's rows make their own state with.
func (m orgTeam) fresh(e *env, user uuid.UUID, roles ...authz.Role) string {
	return e.principal(authn.Principal{Subject: "session-" + uuid.NewString(), UserID: user, Client: "web", Class: authn.ClassUser, Organisation: m.org, Roles: roles})
}

// as is the person user of the team's organisation, with the roles of the principal behind tok, in a session of its own: the
// matrix's frozen clock never ends the rate limit's minute, so each row's writes count against a session of theirs.
func (m orgTeam) as(e *env, tok string, user uuid.UUID) string {
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	p.UserID, p.Subject, p.Organisation = user, "session-"+uuid.NewString(), m.org
	return e.principal(p)
}

// matrixWeeks keeps the matrix's weeks apart, a Monday each from 2091.
var matrixWeeks int

func matrixWeek() (monday, tuesday string) {
	matrixWeeks++
	d := time.Date(2091, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*matrixWeeks)
	return d.Format(time.DateOnly), d.AddDate(0, 0, 1).Format(time.DateOnly)
}

// matrixSubmitted is a submitted week of the matrix's person, whose manager decides it: its submission and ETag.
func matrixSubmitted(e *env) (id, week, tag string) {
	t := matrixTimeTeam(e)
	week, day := matrixWeek()
	priya := t.fresh(e, t.priya, authz.Employee)
	e.saveWeek(priya, week, t.entry(day, 60))
	_, wtag := e.weekOf(priya, week)
	r := e.call(http.MethodPost, "/v1/timesheet/"+week+"/submit", priya, map[string]any{}, "If-Match", wtag, "Idempotency-Key", uuid.NewString())
	if r.Status != 200 {
		e.t.Fatalf("the matrix's week %s: %d %s", week, r.Status, r.Raw)
	}
	id = r.Body["submissions"].([]any)[0].(map[string]any)["id"].(string)
	return id, week, e.call(http.MethodGet, "/v1/timesheet-submissions/"+id, t.fresh(e, t.una, authz.Controller), nil).Header.Get("ETag")
}

var matrixTime = map[string]func(e *env, tok string) (response, func() bool){
	"getTimesheet": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		return e.call(http.MethodGet, "/v1/timesheet/"+t.week, t.as(e, tok, t.priya), nil), nil
	},
	"saveTimesheet": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		week, day := matrixWeek()
		who := t.as(e, tok, t.priya)
		_, tag := e.weekOf(t.fresh(e, t.priya, authz.Employee), week)
		r := e.call(http.MethodPut, "/v1/timesheet/"+week, who, map[string]any{"entries": []map[string]any{t.entry(day, 45)}}, "If-Match", tag)
		return r, func() bool {
			x, _ := e.weekOf(t.fresh(e, t.priya, authz.Employee), week)
			return len(x.Body["entries"].([]any)) == 0
		}
	},
	"submitTimesheet": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		week, day := matrixWeek()
		own := t.fresh(e, t.priya, authz.Employee)
		e.saveWeek(own, week, t.entry(day, 30))
		_, tag := e.weekOf(own, week)
		r := e.call(http.MethodPost, "/v1/timesheet/"+week+"/submit", t.as(e, tok, t.priya), map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString())
		return r, func() bool {
			x, _ := e.weekOf(own, week)
			return len(x.Body["submissions"].([]any)) == 0
		}
	},
	"getTimer": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		return e.call(http.MethodGet, "/v1/timer", t.as(e, tok, t.priya), nil), nil
	},
	"startTimer": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		own := t.fresh(e, t.priya, authz.Employee)
		e.call(http.MethodPost, "/v1/timer/drop", own, nil, "Idempotency-Key", uuid.NewString())
		r := e.call(http.MethodPost, "/v1/timer/start", t.as(e, tok, t.priya), map[string]any{"project_id": t.project, "task_id": t.task}, "Idempotency-Key", uuid.NewString())
		return r, func() bool { return e.call(http.MethodGet, "/v1/timer", own, nil).Body["running"] != true }
	},
	"stopTimer": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		own := t.fresh(e, t.priya, authz.Employee)
		e.call(http.MethodPost, "/v1/timer/drop", own, nil, "Idempotency-Key", uuid.NewString())
		e.call(http.MethodPost, "/v1/timer/start", own, map[string]any{"project_id": t.project, "task_id": t.task}, "Idempotency-Key", uuid.NewString())
		r := e.call(http.MethodPost, "/v1/timer/stop", t.as(e, tok, t.priya), nil, "Idempotency-Key", uuid.NewString())
		return r, func() bool { return e.call(http.MethodGet, "/v1/timer", own, nil).Body["running"] == true }
	},
	"dropTimer": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		own := t.fresh(e, t.priya, authz.Employee)
		e.call(http.MethodPost, "/v1/timer/drop", own, nil, "Idempotency-Key", uuid.NewString())
		e.call(http.MethodPost, "/v1/timer/start", own, map[string]any{"project_id": t.project, "task_id": t.task}, "Idempotency-Key", uuid.NewString())
		r := e.call(http.MethodPost, "/v1/timer/drop", t.as(e, tok, t.priya), nil, "Idempotency-Key", uuid.NewString())
		return r, func() bool { return e.call(http.MethodGet, "/v1/timer", own, nil).Body["running"] == true }
	},
	"reverseTimeEntry": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		id, week, tag := matrixSubmitted(e)
		e.call(http.MethodPost, "/v1/timesheet-submissions/"+id+"/approve", t.fresh(e, t.wei, authz.Approver), map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString())
		entry := e.call(http.MethodGet, "/v1/timesheet-submissions/"+id, t.fresh(e, t.una, authz.Controller), nil).Body["entries"].([]any)[0].(map[string]any)["id"].(string)
		r := e.call(http.MethodPost, "/v1/time-entries/"+entry+"/reverse", t.as(e, tok, t.priya), nil, "Idempotency-Key", uuid.NewString())
		return r, func() bool {
			x, _ := e.weekOf(t.fresh(e, t.priya, authz.Employee), week)
			return len(x.Body["entries"].([]any)) == 1
		}
	},
	"listTimesheetSubmissions": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		return e.call(http.MethodGet, "/v1/timesheet-submissions", t.as(e, tok, t.wei), nil), nil
	},
	"getTimesheetSubmission": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		id, _, _ := matrixSubmitted(e)
		return e.call(http.MethodGet, "/v1/timesheet-submissions/"+id, t.as(e, tok, t.wei), nil), nil
	},
	"approveTimesheetSubmission": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		id, _, tag := matrixSubmitted(e)
		r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+id+"/approve", t.as(e, tok, t.wei), map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString())
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/timesheet-submissions/"+id, t.fresh(e, t.una, authz.Controller), nil).str("status") == "submitted"
		}
	},
	"returnTimesheetSubmission": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		id, _, tag := matrixSubmitted(e)
		r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+id+"/return", t.as(e, tok, t.wei), map[string]any{"comment": "Fix it."}, "If-Match", tag, "Idempotency-Key", uuid.NewString())
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/timesheet-submissions/"+id, t.fresh(e, t.una, authz.Controller), nil).str("status") == "submitted"
		}
	},
}
