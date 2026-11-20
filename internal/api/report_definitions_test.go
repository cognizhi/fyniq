//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/reportsend"
)

// reportRows are a gross margin report's rows over kpiBooks' accounts.
func reportRows() []map[string]any {
	acct := func(id, label, code string) map[string]any {
		return map[string]any{"id": id, "label": label, "kind": "account", "code": code}
	}
	calc := func(id, label, formula, format string) map[string]any {
		return map[string]any{"id": id, "label": label, "kind": "calc", "formula": formula, "format": format}
	}
	return []map[string]any{
		{"id": "hd", "label": "Margin", "kind": "heading"},
		acct("subs", "Subscriptions", "4010"), acct("svc", "Services", "4020"), acct("cogs", "Hosting", "5010"),
		calc("rev", "Revenue", "subs + svc", "amount"), calc("gm", "Gross margin", "rev - cogs", "amount"),
		calc("gmp", "Gross margin %", "gm / rev", "percent"), calc("per_svc", "Margin per service pound", "gm / svc", "ratio"),
		acct("cash", "Cash", "1010"),
	}
}

func reportBody(name, visibility string, rows []map[string]any) map[string]any {
	return map[string]any{"name": name, "description": "A test report", "visibility": visibility, "definition": map[string]any{
		"schema": "fyniq-report@1", "rows": rows, "filters": map[string]any{},
		"columns": []map[string]any{{"key": "current"}, {"key": "prior_period"}, {"key": "prior_year"}},
	}}
}

// The report builder through the API (FR-RPT-04, FR-RPT-08; p2-reporting-planning), worked by hand on kpiBooks. In November:
// subscriptions 5,000.00 and services 1,000.00, hosting 600.00, cash 92,500.00; in October (the prior period): 4,000.00, none,
// 500.00 and 95,000.00; the prior year is not in the books. So revenue is 6,000.00 and 4,000.00, the gross margin 5,400.00 and
// 3,500.00, the margin 90.0% and 87.5% (3,500 / 4,000 exactly), the margin per pound of services 5,400 / 1,000 = 5.40 in
// November and not available in October, when services were nothing; every cell of the prior year is not available.
// req: FR-RPT-04
// req: FR-RPT-08
// req: RND-KPI
// asvs: V8.2.1
func TestAReportIsSavedRunAndChangedThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	kpiBooks(e)
	owner, accountantUser := uuid.New(), uuid.New()
	accountant := e.person(accountantUser, authz.Accountant)
	_ = owner

	// A definition that breaks a rule is refused with the rule's code and the field.
	bad := reportBody("Bad", "private", append(reportRows(), map[string]any{"id": "x", "label": "X", "kind": "calc", "formula": "later + 1", "format": "amount"}))
	e.problemIs(e.call(http.MethodPost, "/v1/report-definitions", accountant, bad), "createReportDefinition", 422, "API-INPUT-02")
	if r := e.call(http.MethodPost, "/v1/report-definitions", accountant, bad); r.Raw == "" || !contains(r.Raw, "RDEF-05") {
		t.Errorf("a forward reference was not named RDEF-05: %s", r.Raw)
	}

	// A preview saves nothing; the report is saved at version 1.
	preview := e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody("Gross margin", "shared", reportRows()), "Prefer", "return=preview")
	if preview.Status != http.StatusOK || preview.Header.Get("Preference-Applied") == "" {
		t.Fatalf("a preview = %d: %s", preview.Status, preview.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/report-definitions", accountant, nil); len(l.Body["items"].([]any)) != 0 {
		t.Fatalf("a preview saved a report: %s", l.Raw)
	}
	created := e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody("Gross margin", "shared", reportRows()), "Idempotency-Key", "rd-1")
	if created.Status != http.StatusCreated || created.Header.Get("ETag") != `"1"` || created.Body["mine"] != true {
		t.Fatalf("saving = %d: %s", created.Status, created.Raw)
	}
	id := created.str("id")
	if replay := e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody("Gross margin", "shared", reportRows()), "Idempotency-Key", "rd-1"); replay.str("id") != id {
		t.Errorf("a repeat with the same key made another report: %s", replay.Raw)
	}
	// The name is used once among the live reports.
	e.problemIs(e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody(" GROSS margin ", "private", reportRows())), "createReportDefinition", 409, "STORAGE-RDEF-02")

	// Run it for November.
	r := e.call(http.MethodGet, "/v1/report-definitions/"+id+"/run?from=2026-11&to=2026-11", accountant, nil)
	if r.Status != http.StatusOK || r.str("currency") != "SGD" || r.str("from") != "2026-11-01" || r.str("to") != "2026-11-30" || r.Body["ledger_revision"].(float64) < 1 {
		t.Fatalf("running = %d: %.600s", r.Status, r.Raw)
	}
	cols := r.Body["columns"].([]any)
	if len(cols) != 3 || cols[0].(map[string]any)["label"] != "Nov 2026" || cols[1].(map[string]any)["available"] != true || cols[2].(map[string]any)["available"] != false {
		t.Fatalf("the columns: %v", cols)
	}
	got := map[string][]string{}
	for _, x := range r.Body["rows"].([]any) {
		row := x.(map[string]any)
		var cells []string
		for _, c := range row["cells"].([]any) {
			cell := c.(map[string]any)
			if cell["not_available"] == true {
				cells = append(cells, "n/a")
				continue
			}
			v, _ := cell["value"].(string)
			cells = append(cells, v)
		}
		got[row["id"].(string)] = cells
	}
	for id, want := range map[string][]string{
		"subs": {"5000.00", "4000.00", "n/a"}, "svc": {"1000.00", "0.00", "n/a"}, "cogs": {"600.00", "500.00", "n/a"}, "rev": {"6000.00", "4000.00", "n/a"},
		"gm": {"5400.00", "3500.00", "n/a"}, "gmp": {"90.0", "87.5", "n/a"}, "per_svc": {"5.40", "n/a", "n/a"}, "cash": {"92500.00", "95000.00", "n/a"},
	} {
		if g := got[id]; len(g) != 3 || g[0] != want[0] || g[1] != want[1] || g[2] != want[2] {
			t.Errorf("row %s = %v, want %v", id, g, want)
		}
	}
	if len(got["hd"]) != 3 {
		t.Errorf("the heading: %v", got["hd"])
	}
	// As at an earlier revision the same report gives the same figures; a later revision is REPORT-04.
	e.problemIs(e.call(http.MethodGet, "/v1/report-definitions/"+id+"/run?to=2026-11&as_at_revision=99999", accountant, nil), "runReportDefinition", 422, "API-INPUT-02")
	e.problemIs(e.call(http.MethodGet, "/v1/report-definitions/"+id+"/run?from=2030-01&to=2030-01", accountant, nil), "runReportDefinition", 422, "API-INPUT-02")

	// A draft runs without saving it.
	draft := e.call(http.MethodPost, "/v1/report-builder/run", accountant, map[string]any{"definition": reportBody("x", "private", reportRows())["definition"], "to": "2026-11"})
	if draft.Status != http.StatusOK || draft.str("definition_id") != "" {
		t.Fatalf("a draft run = %d: %.300s", draft.Status, draft.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/report-builder/run", accountant, map[string]any{"definition": reportBody("x", "private", []map[string]any{
		{"id": "a", "label": "A", "kind": "account", "code": "9999"}})["definition"], "to": "2026-11"}), "runReportDraft", 422, "API-INPUT-02")

	// A change is the next version over its ETag; a stale one is refused; another person who is not an Owner or Administrator may not.
	change := reportBody("Gross margin, revised", "shared", reportRows())
	e.problemIs(e.call(http.MethodPut, "/v1/report-definitions/"+id, accountant, change), "replaceReportDefinition", 428, CodeIfMatchRequired)
	changed := e.call(http.MethodPut, "/v1/report-definitions/"+id, accountant, change, "If-Match", `"1"`)
	if changed.Status != http.StatusOK || changed.Header.Get("ETag") != `"2"` || changed.str("name") != "Gross margin, revised" {
		t.Fatalf("changing = %d: %s", changed.Status, changed.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/report-definitions/"+id, accountant, change, "If-Match", `"1"`), "replaceReportDefinition", 412, CodeStale)
	bookkeeper := e.person(uuid.New(), authz.Bookkeeper)
	e.problemIs(e.call(http.MethodPut, "/v1/report-definitions/"+id, bookkeeper, change, "If-Match", `"2"`), "replaceReportDefinition", 403, CodeReportNotYours)
	if got := e.call(http.MethodGet, "/v1/report-definitions/"+id+"/run?to=2026-11", bookkeeper, nil); got.Status != http.StatusOK {
		t.Errorf("a shared report is run by another reader = %d: %s", got.Status, got.Raw)
	}
	admin := e.person(uuid.New(), authz.Administrator)
	if got := e.call(http.MethodPut, "/v1/report-definitions/"+id, admin, reportBody("Gross margin, by Admin", "shared", reportRows()), "If-Match", `"2"`); got.Status != http.StatusOK {
		t.Errorf("an Administrator changes a shared report = %d: %s", got.Status, got.Raw)
	}

	// A private report is its owner's alone: another person finds none.
	priv := e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody("Mine alone", "private", reportRows()))
	pid := priv.str("id")
	e.problemIs(e.call(http.MethodGet, "/v1/report-definitions/"+pid, bookkeeper, nil), "getReportDefinition", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, "/v1/report-definitions/"+pid+"/run?to=2026-11", admin, nil), "runReportDefinition", 404, CodeNotFound)
	if l := e.call(http.MethodGet, "/v1/report-definitions", bookkeeper, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("another person lists a private report: %s", l.Raw)
	}

	// Archiving ends it: it is never deleted, and it leaves the list.
	arch := e.call(http.MethodPost, "/v1/report-definitions/"+pid+"/archive", accountant, nil, "If-Match", `"1"`, "Idempotency-Key", "arch-1")
	if arch.Status != http.StatusOK || arch.Body["archived"] != true {
		t.Fatalf("archiving = %d: %s", arch.Status, arch.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/report-definitions", accountant, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("an archived report is listed: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/report-definitions?include_archived=true", accountant, nil); len(l.Body["items"].([]any)) != 2 {
		t.Errorf("include_archived: %s", l.Raw)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// A role that reads only part of the ledger never uses the builder (a definition can name any account), and a role the matrix gives
// no write cannot save a report (FR-RPT-04; QA-SEC-03).
// req: FR-RPT-04
// req: QA-SEC-03
// asvs: V4.2.1
func TestOnlyReadersOfTheWholeLedgerUseTheBuilder(t *testing.T) {
	e := newEnv(t)
	kpiBooks(e)
	body := reportBody("Blocked", "shared", reportRows())
	for _, role := range []authz.Role{authz.BoardViewer, authz.ARSpecialist, authz.APSpecialist, authz.PayrollAdmin, authz.Approver, authz.Employee} {
		tok := e.person(uuid.New(), role)
		e.problemIs(e.call(http.MethodGet, "/v1/report-definitions", tok, nil), "listReportDefinitions", 403, "API-AUTHZ-01")
		e.problemIs(e.call(http.MethodPost, "/v1/report-definitions", tok, body), "createReportDefinition", 403, "API-AUTHZ-01")
		e.problemIs(e.call(http.MethodPost, "/v1/report-builder/run", tok, map[string]any{"definition": body["definition"], "to": "2026-11"}), "runReportDraft", 403, "API-AUTHZ-01")
	}
	// An Auditor reads shared reports but saves none; the AI acting for an Accountant reads but never writes.
	if r := e.call(http.MethodPost, "/v1/report-definitions", e.person(uuid.New(), authz.Auditor), body); r.Status != http.StatusForbidden {
		t.Errorf("an Auditor saved a report = %d: %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/report-definitions", e.person(uuid.New(), authz.Auditor), nil); r.Status != http.StatusOK {
		t.Errorf("an Auditor lists reports = %d: %s", r.Status, r.Raw)
	}
}

// A schedule: the roles that schedule, recipients who may read the report, next run by the cadence, a change over its ETag; a recipient
// who is no member, who reads part of the ledger only, or cannot open a private report, is refused (FR-RPT-07).
// req: FR-RPT-07
// asvs: V4.2.1
func TestAReportIsScheduledOnlyForPeopleWhoMayReadIt(t *testing.T) {
	e := newEnv(t)
	kpiBooks(e)
	maker := uuid.New()
	accountant := e.person(maker, authz.Accountant)
	shared := e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody("Board margin", "shared", reportRows())).str("id")
	private := e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody("Mine alone", "private", reportRows())).str("id")
	ctl := e.joined("Carol", "controller")
	boardMember := e.joined("Bob", "board_viewer")
	sched := func(def string, recipients ...uuid.UUID) map[string]any {
		var rs []string
		for _, r := range recipients {
			rs = append(rs, r.String())
		}
		return map[string]any{"definition_id": def, "cadence": "monthly", "day_of_month": 5, "period_rule": "last_closed_month", "channel": "email", "recipients": rs, "active": true}
	}

	// Only the roles that schedule do.
	e.problemIs(e.call(http.MethodPost, "/v1/report-schedules", e.person(uuid.New(), authz.Bookkeeper), sched(shared, ctl)), "createReportSchedule", 403, CodeReportScheduler)
	e.problemIs(e.call(http.MethodPost, "/v1/report-schedules", e.person(maker, authz.Auditor), sched(shared, ctl)), "createReportSchedule", 403, "API-AUTHZ-01")
	// Recipients: a board viewer reads part of the ledger only, a stranger is no member, and a private report is its owner's.
	e.problemIs(e.call(http.MethodPost, "/v1/report-schedules", accountant, sched(shared, boardMember)), "createReportSchedule", 422, "API-INPUT-02")
	if r := e.call(http.MethodPost, "/v1/report-schedules", accountant, sched(shared, boardMember)); !contains(r.Raw, CodeReportRecipient) {
		t.Errorf("the refusal does not name %s: %s", CodeReportRecipient, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/report-schedules", accountant, sched(shared, uuid.New())), "createReportSchedule", 422, "API-INPUT-02")
	e.problemIs(e.call(http.MethodPost, "/v1/report-schedules", accountant, sched(private, ctl)), "createReportSchedule", 422, "API-INPUT-02")
	// A weekly schedule names its weekday and no day of the month.
	wrong := sched(shared, ctl)
	wrong["cadence"] = "weekly"
	e.problemIs(e.call(http.MethodPost, "/v1/report-schedules", accountant, wrong), "createReportSchedule", 422, "API-INPUT-02")

	made := e.call(http.MethodPost, "/v1/report-schedules", accountant, sched(shared, ctl), "Idempotency-Key", "sch-1")
	if made.Status != http.StatusCreated || made.Header.Get("ETag") != `"1"` || made.str("definition_id") != shared || made.Body["active"] != true {
		t.Fatalf("scheduling = %d: %s", made.Status, made.Raw)
	}
	// The first run is the next 5th on or after the organisation's today.
	if next := made.str("next_run_on"); len(next) != 10 || next[8:] != "05" {
		t.Errorf("a monthly schedule on the 5th: next run %s", next)
	}
	sid := made.str("id")
	list := e.call(http.MethodGet, "/v1/report-schedules?definition_id="+shared, accountant, nil)
	if list.Status != http.StatusOK || len(list.Body["items"].([]any)) != 1 {
		t.Fatalf("listing = %d: %s", list.Status, list.Raw)
	}
	if runs := e.call(http.MethodGet, "/v1/report-schedules/"+sid+"/runs", accountant, nil); runs.Status != http.StatusOK || len(runs.Body["items"].([]any)) != 0 {
		t.Errorf("runs before any = %d: %s", runs.Status, runs.Raw)
	}
	// Switching it off is a change over its ETag.
	off := sched(shared, ctl)
	delete(off, "definition_id")
	off["active"] = false
	e.problemIs(e.call(http.MethodPut, "/v1/report-schedules/"+sid, accountant, off), "replaceReportSchedule", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPut, "/v1/report-schedules/"+sid, accountant, off, "If-Match", `"7"`), "replaceReportSchedule", 412, CodeStale)
	if r := e.call(http.MethodPut, "/v1/report-schedules/"+sid, accountant, off, "If-Match", `"1"`); r.Status != http.StatusOK || r.Body["active"] != false || r.Header.Get("ETag") != `"2"` {
		t.Fatalf("switching off = %d: %s", r.Status, r.Raw)
	}
	// Another person's private report has no schedules to them.
	e.problemIs(e.call(http.MethodGet, "/v1/report-schedules?definition_id="+private, e.person(uuid.New(), authz.Bookkeeper), nil), "listReportSchedules", 404, CodeNotFound)
}

// The job that sends a due report (FR-RPT-07; design D3): each recipient's right is checked again at send time, so one whose reading role
// ended since is skipped and the run says why; the others are told by a notification that carries no figure; the run is recorded once per
// day and the schedule moves on to its next weekday.
// req: FR-RPT-07
// asvs: V4.2.1
func TestADueReportIsSentOnlyToThoseWhoStillMayReadIt(t *testing.T) {
	e := newEnv(t)
	kpiBooks(e)
	e.clock.Set(time.Date(2099, 10, 15, 9, 0, 0, 0, time.UTC)) // a Thursday; role expiries are read against the database's own clock, so a day far ahead
	accountant := e.person(uuid.New(), authz.Accountant)
	def := e.call(http.MethodPost, "/v1/report-definitions", accountant, reportBody("Weekly margin", "shared", reportRows())).str("id")
	keeps := e.joined("Kay", "controller")
	// Lee's controller role expires at midnight, after the schedule is made and before it runs.
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Administrator), map[string]any{"display_name": "Lee", "roles": []string{"controller"},
		"roles_expire_at": "2099-10-16T00:00:00Z"}).str("code")
	leeMember := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "member-Lee", Client: "web"}), map[string]any{"invitation_code": code, "display_name": "Lee"}).str("member_id")
	var loses uuid.UUID
	for _, x := range e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil).Body["items"].([]any) {
		if m := x.(map[string]any); m["id"] == leeMember {
			loses = uuid.MustParse(m["user_id"].(string))
		}
	}
	made := e.call(http.MethodPost, "/v1/report-schedules", accountant, map[string]any{"definition_id": def, "cadence": "weekly", "weekday": 5, "period_rule": "month_to_date",
		"channel": "email", "recipients": []string{keeps.String(), loses.String()}, "active": true})
	if made.Status != http.StatusCreated || made.str("next_run_on") != "2099-10-16" {
		t.Fatalf("scheduling = %d: %s", made.Status, made.Raw)
	}
	sid := made.str("id")
	worker := workerStore(t)
	runner := reportsend.Runner{Store: worker, Outbox: outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock}, Clock: e.clock}
	// Before its day, nothing is due.
	if n, err := runner.RunOrganisation(context.Background(), e.books.Org); err != nil || n != 0 {
		t.Fatalf("the day before: %d, %v", n, err)
	}
	e.clock.Set(time.Date(2099, 10, 16, 9, 0, 0, 0, time.UTC))
	if n, err := runner.RunOrganisation(context.Background(), e.books.Org); err != nil || n != 1 {
		t.Fatalf("the day of: %d told, %v", n, err)
	}
	// The same day again: the run is recorded once and the schedule has moved on.
	if n, err := runner.RunOrganisation(context.Background(), e.books.Org); err != nil || n != 0 {
		t.Fatalf("the same day again: %d, %v", n, err)
	}
	runs := e.call(http.MethodGet, "/v1/report-schedules/"+sid+"/runs", accountant, nil)
	items := runs.Body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("the runs: %s", runs.Raw)
	}
	run := items[0].(map[string]any)
	skipped := run["skipped"].([]any)
	if run["ran_on"] != "2099-10-16" || run["period"] != "2099-10" || run["sent"].(float64) != 1 || len(skipped) != 1 ||
		skipped[0].(map[string]any)["user_id"] != loses.String() || !contains(skipped[0].(map[string]any)["why"].(string), "no longer") {
		t.Errorf("the run: %v", run)
	}
	next := e.call(http.MethodGet, "/v1/report-schedules?definition_id="+def, accountant, nil).Body["items"].([]any)[0].(map[string]any)
	if next["next_run_on"] != "2099-10-23" || next["last_run_on"] != "2099-10-16" {
		t.Errorf("the schedule moved on to %v, last run %v", next["next_run_on"], next["last_run_on"])
	}
}
