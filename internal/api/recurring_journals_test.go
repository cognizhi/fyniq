//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
)

// makeDrafts runs the scheduler's recurring journals for e's organisation up to day, as the worker does.
func (e *env) makeDrafts(day time.Time) journal.RecurringRun {
	e.t.Helper()
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(worker.Close)
	run, err := journal.Service{Store: worker}.MakeDue(context.Background(), e.books.Org, day)
	if err != nil {
		e.t.Fatal(err)
	}
	return run
}

func (e *env) recurring(name, amount string, schedule map[string]any) map[string]any {
	return map[string]any{"name": name, "schedule": schedule, "memo": "Office rent", "lines": e.draft(amount)["lines"], "reverses": true}
}

func monthly(day int, starts string) map[string]any {
	return map[string]any{"every_months": 1, "day_of_month": day, "starts_on": starts}
}

// FR-GL-02, FR-AUT-01 (the founders' decisions of rev 0.58 and rev 0.61, built in rev 0.60): a recurring journal is a journal's content
// and a schedule; the scheduler makes the draft of each occurrence, dated on it and named for it, which a person
// posts. Its statuses are the workbook's checks of recurring entries: content the posting rules would refuse (C.1
// #29) and an occurrence due but not made (C.1 #28), which the close checks warn of too.
// req: FR-GL-02
// req: FR-AUT-01
// req: C.1 #28
// req: C.1 #29
func TestRecurringJournals(t *testing.T) {
	e := newEnv(t) // its day is 15 October 2026
	accountant := e.as(authz.Accountant)
	check := func(r response, i int) map[string]any {
		t.Helper()
		c, _ := r.Body["checks"].([]any)
		if len(c) <= i {
			t.Fatalf("no status %d: %s", i, r.Raw)
		}
		return c[i].(map[string]any)
	}

	// A preview checks and saves nothing.
	rent := e.recurring("Office rent accrual", "400.00", monthly(31, "2026-10-01"))
	if p := e.call(http.MethodPost, "/v1/recurring-journals", accountant, rent, "Prefer", "return=preview"); p.Status != http.StatusOK ||
		p.Header.Get("Preference-Applied") != "return=preview" || p.str("next_on") != "2026-10-31" {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/recurring-journals", accountant, nil); len(l.Body["items"].([]any)) != 0 {
		t.Fatalf("a preview saved something: %s", l.Raw)
	}
	c := e.call(http.MethodPost, "/v1/recurring-journals", accountant, rent, "Idempotency-Key", "recurring-rent")
	if c.Status != http.StatusCreated || c.str("status") != "active" || c.str("next_on") != "2026-10-31" || c.Header.Get("ETag") != `"1"` ||
		c.Body["reverses"] != true || !strings.Contains(c.Raw, `"debits":[{"amount":"400.00","currency":"SGD"}]`) || c.str("edited_by", "kind") != "user" ||
		check(c, 0)["code"] != journal.StatusRecurringReady || c.Header.Get("Location") != "/v1/recurring-journals/"+c.str("id") {
		t.Fatalf("adding a recurring journal: %d %s", c.Status, c.Raw)
	}
	id := c.str("id")
	if again := e.call(http.MethodPost, "/v1/recurring-journals", accountant, rent, "Idempotency-Key", "recurring-rent"); again.str("id") != id {
		t.Errorf("a replay made another: %s", again.Raw)
	}

	// Every invalid field at once; a schedule with no occurrence; content the posting rules refuse is kept, and says so.
	bad := e.recurring(" ", "1.00", map[string]any{"every_months": 1, "day_of_month": 1, "starts_on": "2026-10-15", "ends_on": "2026-10-01"})
	r := e.call(http.MethodPost, "/v1/recurring-journals", accountant, bad)
	e.problemIs(r, "createRecurringJournal", 422, CodeInvalid)
	if !strings.Contains(r.Raw, `"field":"body.name"`) || !strings.Contains(r.Raw, `"field":"body.schedule.ends_on"`) {
		t.Errorf("the invalid fields: %s", r.Raw)
	}
	never := e.recurring("Never", "1.00", map[string]any{"every_months": 1, "day_of_month": 1, "starts_on": "2026-10-15", "ends_on": "2026-10-20"})
	e.problemIs(e.call(http.MethodPost, "/v1/recurring-journals", accountant, never), "createRecurringJournal", 422, journal.CodeRecurringSchedule)
	unbalanced := e.recurring("Unbalanced", "100.00", monthly(1, "2026-11-01"))
	unbalanced["lines"].([]map[string]any)[1]["amount"] = map[string]any{"amount": "-90.00", "currency": "SGD"}
	if u := e.call(http.MethodPost, "/v1/recurring-journals", accountant, unbalanced); u.Status != http.StatusCreated ||
		check(u, 0)["severity"] != "error" || check(u, 0)["title"] != "Entry does not balance" {
		t.Errorf("an incomplete recurring journal: %d %s", u.Status, u.Raw)
	}

	// A change at its version: the content is a new version; a stale ETag is refused.
	rent["memo"] = "Office rent, level 3"
	e.problemIs(e.call(http.MethodPut, "/v1/recurring-journals/"+id, accountant, rent), "replaceRecurringJournal", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPut, "/v1/recurring-journals/"+id, accountant, rent, "If-Match", `"7"`), "replaceRecurringJournal", 412, CodeStale)
	p := e.call(http.MethodPut, "/v1/recurring-journals/"+id, accountant, rent, "If-Match", `"1"`)
	if p.Status != http.StatusOK || p.Body["version"] != float64(2) || p.Body["content_version"] != float64(2) || p.str("memo") != "Office rent, level 3" {
		t.Fatalf("changing it: %d %s", p.Status, p.Raw)
	}
	// Paused, it is paused once; resumed, it goes on.
	s := e.call(http.MethodPost, "/v1/recurring-journals/"+id+"/pause", accountant, nil, "If-Match", `"2"`)
	if s.Status != http.StatusOK || s.str("status") != "paused" || check(s, 0)["code"] != journal.StatusRecurringPaused {
		t.Fatalf("pausing: %d %s", s.Status, s.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/recurring-journals/"+id+"/pause", accountant, nil, "If-Match", `"3"`), "pauseRecurringJournal", 409, journal.CodeRecurringPaused)
	if s = e.call(http.MethodPost, "/v1/recurring-journals/"+id+"/resume", accountant, nil, "If-Match", `"3"`); s.Status != http.StatusOK ||
		s.str("status") != "active" || s.str("next_on") != "2026-10-31" {
		t.Fatalf("resuming: %d %s", s.Status, s.Raw)
	}

	// One that started in September is due but not made: its status and November's close checks say so.
	late := e.call(http.MethodPost, "/v1/recurring-journals", accountant, e.recurring("Software subscription", "50.00", monthly(1, "2026-09-01")))
	if late.str("next_on") != "2026-09-01" || check(late, 0)["severity"] != "warning" || check(late, 0)["code"] != journal.StatusRecurringDue ||
		check(late, 0)["title"] != "Due but not created" {
		t.Fatalf("a recurring journal due but not made: %s", late.Raw)
	}
	controller := e.as(authz.Controller)
	closeCheck := func(period, key string) map[string]any {
		t.Helper()
		st := e.call(http.MethodGet, "/v1/periods/"+period+"/close", controller, nil)
		r := e.call(http.MethodPost, "/v1/periods/"+period+"/close-checks", controller, nil, "If-Match", st.Header.Get("ETag"))
		for _, c := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if c.(map[string]any)["key"] == key {
				return c.(map[string]any)
			}
		}
		t.Fatalf("no %s check: %s", key, r.Raw)
		return nil
	}
	oct := e.books.Oct.ID.String()
	// On 15 October, 1 September's occurrence is more than a day late: the hourly scheduler failed (✖, C.1 #28). The
	// unbalanced one's content is warned of (⚠, C.1 #29; the founders' decision of rev 0.61).
	if c := closeCheck(oct, "recurring"); c["severity"] != "error" || !strings.Contains(c["detail"].(string), "1 recurring journal(s) have an occurrence more than a day ago") {
		t.Errorf("October's close, with an occurrence the scheduler did not make: %v", c)
	}
	if c := closeCheck(oct, "recurring-content"); c["severity"] != "warning" || !strings.Contains(c["detail"].(string), "1 active recurring journal(s)") {
		t.Errorf("October's close, with an incomplete recurring journal: %v", c)
	}

	// The scheduler makes the drafts up to 31 October (the rent's of 31 October, the subscription's of 1 September and 1
	// October): dated on each occurrence, saved by it, named for their recurring journal; a person posts them.
	if run := e.makeDrafts(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)); run.Made != 3 {
		t.Fatalf("the run to 31 October made %d drafts", run.Made)
	}
	g := e.call(http.MethodGet, "/v1/recurring-journals/"+id, accountant, nil)
	draft := e.call(http.MethodGet, "/v1/journals/"+g.str("last_journal_id"), accountant, nil)
	if g.str("last_on") != "2026-10-31" || g.str("next_on") != "2026-11-30" || draft.str("status") != "draft" || draft.str("date") != "2026-10-31" ||
		draft.str("reverse_on") != "2026-11-01" || draft.str("memo") != "Office rent, level 3" || draft.str("prepared_by", "kind") != "service" ||
		draft.str("prepared_by", "id") != "scheduler:journals.recurring" || draft.str("recurring_journal", "id") != id ||
		draft.Body["recurring_journal"].(map[string]any)["version"] != float64(2) || draft.Body["number"] != nil {
		t.Fatalf("the October draft: %s %s", g.Raw, draft.Raw)
	}
	if c := closeCheck(oct, "recurring"); c["severity"] != "ok" {
		t.Errorf("October's close once its drafts are made: %v", c)
	}
	if c := closeCheck(e.books.Nov.ID.String(), "recurring"); c["severity"] != "warning" ||
		!strings.Contains(c["detail"].(string), "3 recurring journal(s) have an occurrence on or before 2026-11-30 with no draft yet") {
		t.Errorf("November's close, with occurrences still to come in it: %v", c)
	}
	// Saving the draft keeps where it came from; posting it numbers it.
	body := e.draft("400.00")
	body["date"], body["memo"] = "2026-10-31", "Office rent, level 3 (checked)"
	sv := e.call(http.MethodPut, "/v1/journals/"+draft.str("id"), accountant, body, "If-Match", draft.Header.Get("ETag"))
	if sv.Status != http.StatusOK || sv.str("recurring_journal", "id") != id || sv.str("prepared_by", "kind") != "user" {
		t.Fatalf("saving the scheduler's draft: %d %s", sv.Status, sv.Raw)
	}
	posted := e.call(http.MethodPost, "/v1/journals/"+draft.str("id")+"/post", accountant, nil, "If-Match", sv.Header.Get("ETag"))
	if posted.Status != http.StatusOK || posted.str("journal", "number") == "" {
		t.Fatalf("posting the scheduler's draft: %d %s", posted.Status, posted.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/journals?status=draft", accountant, nil); !strings.Contains(l.Raw, `"recurring_journal_id":"`+late.str("id")+`"`) {
		t.Errorf("the list does not name the recurring journal: %s", l.Raw)
	}
}

// SOD-3 (rev 0.60): whoever wrote the version of a recurring journal a draft was made from prepared that draft, so they
// do not approve its posting, whoever asks.
// req: SOD-3
// req: FR-GL-02
func TestTheAuthorOfARecurringJournalDoesNotApproveItsDrafts(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Accountant, authz.Controller), f.as(f.weiling, authz.Accountant, authz.Controller)
	c := e.call(http.MethodPost, "/v1/recurring-journals", weiling, e.recurring("Office rent accrual", "5000.00", monthly(31, "2026-10-01")))
	if c.Status != http.StatusCreated {
		t.Fatalf("Wei Ling's recurring journal: %d %s", c.Status, c.Raw)
	}
	e.makeDrafts(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	id := e.call(http.MethodGet, "/v1/recurring-journals/"+c.str("id"), ahkow, nil).str("last_journal_id")
	draft := e.call(http.MethodGet, "/v1/journals/"+id, ahkow, nil)
	asked := e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", ahkow, nil, "If-Match", draft.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted {
		t.Fatalf("Ah Kow asks to post the scheduler's draft: %d %s", asked.Status, asked.Raw)
	}
	r := f.decide(weiling, asked.str("id"), "approve", hashOf(asked))
	e.problemIs(r, "approveApproval", 403, CodeOwnJournal)
	if !strings.Contains(r.Raw, "You wrote the recurring journal this draft was made from") {
		t.Errorf("the refusal does not say why: %s", r.Raw)
	}
}
