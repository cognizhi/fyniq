//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
)

// Journals are listed newest first and narrowed by status, dates and text; a draft changes as a new version at the
// version read; the preparer and the poster are recorded by name.
// req: FR-GL-01
func TestJournalListDraftVersionsAndWhoPrepared(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Accountant), f.as(f.weiling, authz.Accountant)
	draft := func(date, memo, amount string) map[string]any {
		d := e.draft(amount)
		d["date"], d["memo"] = date, memo
		if date == "" {
			delete(d, "date")
		}
		return d
	}
	save := func(who string, d map[string]any) response {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/journals", who, d)
		if r.Status != 201 {
			t.Fatalf("saving a draft: %d %s", r.Status, r.Raw)
		}
		return r
	}
	items := func(r response) []map[string]any {
		t.Helper()
		if r.Status != 200 {
			t.Fatalf("listJournals: %d %s", r.Status, r.Raw)
		}
		var out []map[string]any
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	list := func(query string) []map[string]any { return items(e.call(http.MethodGet, "/v1/journals"+query, ahkow, nil)) }

	rent := save(ahkow, draft("2026-10-20", "October rent", "1200.00"))
	if rent.str("prepared_by", "name") != "Ah Kow" || rent.str("prepared_by", "kind") != "user" || rent.Body["posted_by"] != nil {
		t.Fatalf("the preparer of a new draft: %s", rent.Raw)
	}
	accrual := save(ahkow, draft("2026-10-31", "Audit fee accrual", "800.00"))
	save(ahkow, draft("", "October rent, undated copy", "5.00"))
	save(ahkow, draft("2026-11-03", "November rent", "1200.00"))

	// Wei Ling changes the accrual: a new version, at the version she read, saved by her.
	path, tag := "/v1/journals/"+accrual.str("id"), accrual.Header.Get("ETag")
	change := draft("2026-10-31", "Audit fee accrual, revised", "850.00")
	e.problemIs(e.call(http.MethodPut, path, weiling, change), "replaceJournal", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPut, path, weiling, change, "If-Match", `"9-draft"`), "replaceJournal", 412, CodeStale)
	pv := e.call(http.MethodPut, path, weiling, change, "If-Match", tag, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || pv.Body["version"] != float64(2) {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	if g := e.call(http.MethodGet, path, weiling, nil); g.Body["version"] != float64(1) || g.Header.Get("ETag") != tag {
		t.Fatalf("the preview saved the change: %s", g.Raw)
	}
	r := e.call(http.MethodPut, path, weiling, change, "If-Match", tag)
	if r.Status != 200 || r.Body["version"] != float64(2) || r.Body["memo"] != "Audit fee accrual, revised" || r.str("prepared_by", "name") != "Wei Ling" ||
		r.Header.Get("ETag") == tag {
		t.Fatalf("the change: %d %s", r.Status, r.Raw)
	}

	// Wei Ling posts Ah Kow's rent: he prepared it, she posted it.
	p := e.call(http.MethodPost, "/v1/journals/"+rent.str("id")+"/post", weiling, nil, "If-Match", rent.Header.Get("ETag"))
	if p.Status != 200 || p.str("journal", "prepared_by", "name") != "Ah Kow" || p.str("journal", "posted_by", "name") != "Wei Ling" ||
		p.str("journal", "posted_at") == "" {
		t.Fatalf("posting: %d %s", p.Status, p.Raw)
	}
	number := p.str("journal", "number")
	// A posted journal does not change: it is reversed.
	e.problemIs(e.call(http.MethodPut, "/v1/journals/"+rent.str("id"), weiling, change, "If-Match", p.Header.Get("ETag")), "replaceJournal", 409, journal.CodeNotDraft)

	// The list: newest date first, a draft without a date last, with debit totals and people.
	all := list("")
	order := []string{"November rent", "Audit fee accrual, revised", "October rent", "October rent, undated copy"}
	if len(all) != len(order) {
		t.Fatalf("want %d journals, got %d", len(order), len(all))
	}
	for i, memo := range order {
		if all[i]["memo"] != memo {
			t.Errorf("journal %d is %q, want %q", i, all[i]["memo"], memo)
		}
	}
	debits := all[0]["debits"].([]any)[0].(map[string]any)
	if debits["amount"] != "1200.00" || debits["currency"] != "SGD" || all[0]["lines"] != float64(2) {
		t.Errorf("the debit total: %v", all[0])
	}
	if all[2]["status"] != "posted" || all[2]["posted_by"].(map[string]any)["name"] != "Wei Ling" || all[2]["number"] != number {
		t.Errorf("the posted journal in the list: %v", all[2])
	}

	// Narrowed by status, dates and text (the memo or the number, ignoring case).
	if n := len(list("?status=draft")); n != 3 {
		t.Errorf("drafts: %d", n)
	}
	if got := list("?status=posted"); len(got) != 1 || got[0]["memo"] != "October rent" {
		t.Errorf("posted: %v", got)
	}
	if got := list("?from=2026-10-21&to=2026-10-31"); len(got) != 1 || got[0]["memo"] != "Audit fee accrual, revised" {
		t.Errorf("a date range: %v", got)
	}
	if got := list("?q=RENT"); len(got) != 3 {
		t.Errorf("text in the memo: %d", len(got))
	}
	if got := list("?q=" + number); len(got) != 1 || got[0]["memo"] != "October rent" {
		t.Errorf("the number: %v", got)
	}
	if got := list("?q=%25"); len(got) != 0 {
		t.Errorf("a %% is text, not a wildcard: %d", len(got))
	}
	e.problemIs(e.call(http.MethodGet, "/v1/journals?from=31-10-2026", ahkow, nil), "listJournals", 422, CodeInvalid)

	// A page at a time.
	first := e.call(http.MethodGet, "/v1/journals?page_size=3", ahkow, nil)
	cursor, _ := first.Body["next_cursor"].(string)
	if len(items(first)) != 3 || cursor == "" {
		t.Fatalf("the first page: %s", first.Raw)
	}
	rest := e.call(http.MethodGet, "/v1/journals?page_size=3&cursor="+cursor, ahkow, nil)
	if got := items(rest); len(got) != 1 || got[0]["memo"] != "October rent, undated copy" || rest.Body["next_cursor"] != nil {
		t.Fatalf("the last page: %s", rest.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/journals?cursor=bm90LWEtY3Vyc29y", ahkow, nil), "listJournals", 422, CodeCursor)

	// The matrix: the Owner reads journals; a Bookkeeper does not; the Owner changes no draft.
	if r := e.call(http.MethodGet, "/v1/journals", e.as(authz.Owner), nil); r.Status != 200 {
		t.Errorf("the Owner lists journals: %d", r.Status)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/journals", e.as(authz.Bookkeeper), nil), "listJournals", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPut, path, e.as(authz.Owner), change, "If-Match", "*"), "replaceJournal", 403, CodeForbidden)
}
