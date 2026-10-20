//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/period"
)

// The periods and the financial years: listed with their year, opened a year at a time from the books start, never
// a year that is not the next or one too far ahead, and fixed once open.
// req: FR-ORG-02
// req: ledger/posting-rules Posting conventions
func TestPeriodsAndFinancialYears(t *testing.T) {
	e := newEnv(t)
	accountant, controller := e.as(authz.Accountant), e.as(authz.Controller)
	items := func(r response) []any {
		t.Helper()
		l, _ := r.Body["items"].([]any)
		return l
	}
	open := func(starts string, headers ...string) response {
		return e.call(http.MethodPost, "/v1/periods:open-year", accountant, map[string]any{"starts_on": starts}, headers...)
	}

	// The organisation has October and November 2026 from before its years were set: no year, nothing to open.
	r := e.call(http.MethodGet, "/v1/periods", accountant, nil)
	if r.Status != 200 || len(items(r)) != 2 || r.Body["next_year"] != nil || items(r)[0].(map[string]any)["financial_year"] != nil {
		t.Fatalf("periods before the years are set: %d %s", r.Status, r.Raw)
	}
	e.problemIs(open("2026-12-01"), "openFinancialYear", 409, period.CodeNoYear)

	// The years are set for the first time beside those periods: from 1 October 2026 to the end of March 2027.
	tag := e.call(http.MethodGet, "/v1/organisation/profile", controller, nil).Header.Get("ETag")
	years := map[string]any{"fy_start_month": 4, "books_start": "2026-10-01", "first_year_end": "2027-03-31"}
	if r := e.call(http.MethodPatch, "/v1/organisation/profile", controller, years, "If-Match", tag); r.Status != 200 {
		t.Fatalf("setting the years: %d %s", r.Status, r.Raw)
	}

	// The rest of the first year opens next: December to March.
	r = e.call(http.MethodGet, "/v1/periods", accountant, nil)
	next, _ := r.Body["next_year"].(map[string]any)
	if items(r)[0].(map[string]any)["financial_year"] != float64(1) || next["number"] != float64(1) || next["starts_on"] != "2026-12-01" ||
		next["ends_on"] != "2027-03-31" || next["periods"] != float64(4) || next["may_open"] != true {
		t.Fatalf("the next year: %s", r.Raw)
	}
	e.problemIs(open("2027-04-01"), "openFinancialYear", 409, period.CodeNotNext)

	// A preview saves nothing.
	pv := open("2026-12-01", "Prefer", "return=preview")
	if pv.Header.Get("Preference-Applied") != "return=preview" || len(pv.Body["periods"].([]any)) != 4 {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	if n := len(items(e.call(http.MethodGet, "/v1/periods", accountant, nil))); n != 2 {
		t.Fatalf("the preview saved %d periods", n-2)
	}

	// Opened once, whatever the retries.
	r = open("2026-12-01", "Idempotency-Key", "open-first-year")
	ps, _ := r.Body["periods"].([]any)
	if r.Status != 201 || r.Header.Get("Location") != "/v1/periods" || len(ps) != 4 ||
		ps[0].(map[string]any)["starts_on"] != "2026-12-01" || ps[3].(map[string]any)["ends_on"] != "2027-03-31" {
		t.Fatalf("opening the first year: %d %s", r.Status, r.Raw)
	}
	if again := open("2026-12-01", "Idempotency-Key", "open-first-year"); again.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("the replay: %d %v", again.Status, again.Header)
	}
	e.problemIs(open("2026-12-01"), "openFinancialYear", 409, period.CodeNotNext)

	// The second year is twelve calendar months and may open; the third starts more than a year after today's.
	r = e.call(http.MethodGet, "/v1/periods", accountant, nil)
	if next, _ = r.Body["next_year"].(map[string]any); next["number"] != float64(2) || next["starts_on"] != "2027-04-01" || next["periods"] != float64(12) || next["may_open"] != true {
		t.Fatalf("the second year: %s", r.Raw)
	}
	if r := open("2027-04-01"); r.Status != 201 || len(r.Body["periods"].([]any)) != 12 {
		t.Fatalf("opening the second year: %d %s", r.Status, r.Raw)
	}
	r = e.call(http.MethodGet, "/v1/periods", accountant, nil)
	if next, _ = r.Body["next_year"].(map[string]any); next["number"] != float64(3) || next["may_open"] != false {
		t.Fatalf("the third year: %s", r.Raw)
	}
	e.problemIs(open("2028-04-01"), "openFinancialYear", 409, period.CodeTooFar)

	// A page at a time, earliest first; each period with its ETag.
	first := e.call(http.MethodGet, "/v1/periods?page_size=5", accountant, nil)
	cursor, _ := first.Body["next_cursor"].(string)
	if len(items(first)) != 5 || cursor == "" || items(first)[0].(map[string]any)["starts_on"] != "2026-10-01" {
		t.Fatalf("the first page: %s", first.Raw)
	}
	rest := e.call(http.MethodGet, "/v1/periods?page_size=500&cursor="+cursor, accountant, nil)
	if len(items(rest)) != 13 || rest.Body["next_cursor"] != nil {
		t.Fatalf("the rest: %d periods", len(items(rest)))
	}
	id := items(first)[2].(map[string]any)["id"].(string)
	if g := e.call(http.MethodGet, "/v1/periods/"+id, e.as(authz.Auditor), nil); g.Status != 200 || g.Header.Get("ETag") == "" ||
		g.Body["starts_on"] != "2026-12-01" || g.Body["status"] != "open" {
		t.Fatalf("one period: %d %s", g.Status, g.Raw)
	}

	// The years are fixed now; the rest of the profile still changes.
	tag = e.call(http.MethodGet, "/v1/organisation/profile", controller, nil).Header.Get("ETag")
	e.problemIs(e.call(http.MethodPatch, "/v1/organisation/profile", controller, map[string]any{"books_start": "2026-09-01"}, "If-Match", tag),
		"updateOrganisationProfile", 409, period.CodeYearsOpen)
	if r := e.call(http.MethodPatch, "/v1/organisation/profile", controller, map[string]any{"legal_name": "Periods Pte. Ltd."}, "If-Match", tag); r.Status != 200 {
		t.Fatalf("another field: %d %s", r.Status, r.Raw)
	}

	// The matrix: a Bookkeeper sees no periods; the Owner sees them but opens none.
	e.problemIs(e.call(http.MethodGet, "/v1/periods", e.as(authz.Bookkeeper), nil), "listPeriods", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/periods:open-year", e.as(authz.Owner), map[string]any{"starts_on": "2028-04-01"}), "openFinancialYear", 403, CodeForbidden)
	if r := e.call(http.MethodGet, "/v1/periods", e.as(authz.Owner), nil); r.Status != 200 {
		t.Errorf("the Owner reads the periods: %d", r.Status)
	}
	if trail := auditTrail(t, e); strings.Count(trail, `"action":"period.created"`) < 16 {
		t.Errorf("want an audit event for each period opened")
	}
}
