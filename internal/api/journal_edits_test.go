//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// edit is the body of an edit of a test journal: rent of amount on date, with a reason.
func (e *env) edit(amount, date, reason string) map[string]any {
	d := e.draft(amount)
	d["date"], d["memo"], d["reason"] = date, "October rent, corrected", reason
	return d
}

// In Standard mode a person who holds P edits a posted journal in an open period: the journal's next version, whose post
// reverses the entry it had on that entry's date and posts the new one, in one transaction; it stays posted, keeps its
// number and records who posted the edit. A preview shows both entries and saves nothing.
// req: FR-GL-12
// req: ledger/posting-rules Edits of posted documents
// req: FR-INT-14
func TestEditAPostedJournal(t *testing.T) {
	e := newEnv(t)
	standardMode(e) // in Founder mode an edit needs a second person (FM-1)
	accountant := e.as(authz.Accountant)
	id, tag := e.postedJournal("100.00") // dated 2026-10-20
	path := "/v1/journals/" + id + "/edit"
	before := e.call(http.MethodGet, "/v1/journals/"+id, accountant, nil)

	// The refusals first.
	e.problemIs(e.call(http.MethodPost, path, accountant, e.edit("120.00", "2026-10-22", "Rent was 120")), "editJournal", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPost, path, accountant, e.edit("120.00", "2026-10-22", "Rent was 120"), "If-Match", `"1-draft"`), "editJournal", 412, CodeStale)
	e.problemIs(e.call(http.MethodPost, path, accountant, e.edit("120.00", "2026-10-22", "  "), "If-Match", tag), "editJournal", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, path, accountant, e.edit("2000.01", "2026-10-22", "Rent was 2,000.01"), "If-Match", tag), "editJournal", 409, CodeNeedsApproval)
	unbalanced := e.edit("120.00", "2026-10-22", "Rent was 120")
	unbalanced["lines"].([]map[string]any)[1]["amount"] = map[string]any{"amount": "-119.99", "currency": "SGD"}
	e.problemIs(e.call(http.MethodPost, path, accountant, unbalanced, "If-Match", tag), "editJournal", 422, "LEDGER-09")
	draft, draftTag := e.createDraft("10.00")
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+draft+"/edit", accountant, e.edit("12.00", "2026-10-22", "Typo"), "If-Match", draftTag),
		"editJournal", 409, journal.CodeNotPosted)

	// A preview: both entries, nothing saved.
	p := e.call(http.MethodPost, path, accountant, e.edit("120.00", "2026-10-22", "Rent was 120"), "If-Match", tag, "Prefer", "return=preview")
	if p.Status != 200 || p.Header.Get("Preference-Applied") != "return=preview" || p.str("reversal", "posting_date") != "2026-10-20" ||
		p.str("entry", "posting_date") != "2026-10-22" {
		t.Fatalf("a preview of the edit: %d %v %s", p.Status, p.Header, p.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/journals/"+id, accountant, nil); g.Header.Get("ETag") != tag {
		t.Fatalf("the preview changed the journal: %s", g.Raw)
	}

	// The edit.
	r := e.call(http.MethodPost, path, accountant, e.edit("120.00", "2026-10-22", "Rent was 120"), "If-Match", tag)
	if r.Status != 200 || r.Header.Get("ETag") != `"2-posted"` || r.str("journal", "status") != "posted" || r.str("journal", "date") != "2026-10-22" ||
		r.str("journal", "memo") != "October rent, corrected" || r.str("journal", "number") != before.str("number") ||
		r.str("reversal", "posting_date") != "2026-10-20" || r.str("reversal", "reverses_entry_id") == "" || r.str("entry", "posting_date") != "2026-10-22" ||
		!strings.Contains(r.str("reversal", "memo"), "Rent was 120") {
		t.Fatalf("the edit: %d %v %s", r.Status, r.Header, r.Raw)
	}
	// The ledger keeps all three: the first entry, its reversal and the new one; the account ends at the new amount.
	gl := e.call(http.MethodGet, "/v1/reports/general-ledger?account_id="+e.books.Rent.ID.String()+"&from=2026-10-01&to=2026-10-31", e.as(authz.Controller), nil)
	ledgerOf := gl.Body["accounts"].([]any)[0].(map[string]any)
	if lines := ledgerOf["lines"].([]any); len(lines) != 3 || ledgerOf["closing"].(map[string]any)["amount"] != "120.00" {
		t.Fatalf("the rent account after the edit: %s", gl.Raw)
	}
	// The same version is never edited twice.
	e.problemIs(e.call(http.MethodPost, path, accountant, e.edit("130.00", "2026-10-22", "Again"), "If-Match", tag), "editJournal", 412, CodeStale)
	trail := auditTrail(t, e)
	for _, want := range []string{`"action":"document.edited"`, `"reason":"Rent was 120"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("the audit trail lacks %s", want)
		}
	}

	// A closed period refuses the reversal the edit needs.
	ctx := context.Background()
	as := func(who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	c, err := e.api.closing.RunChecks(as("edit-preparer"), e.books.Org, e.books.Oct.ID)
	if err == nil {
		_, _, err = e.api.closing.Close(as("edit-attester"), e.books.Org, e.books.Oct.ID, c.ID, "Reviewed.")
	}
	if err != nil {
		t.Fatal(err)
	}
	e.problemIs(e.call(http.MethodPost, path, accountant, e.edit("125.00", "2026-11-02", "Rent was 125"), "If-Match", r.Header.Get("ETag")),
		"editJournal", 409, storage.CodePeriodClosed)
}

// In Founder mode an edit is asked for: the request keeps the content and lists what changes, line by line; its
// envelope holds the content's hash. Another founder approves it, and the edit posts as them, with the requester as the
// one who prepared the new version.
// req: FR-GL-12
// req: FM-1
// req: FR-APR-02
func TestEditAPostedJournalInFounderMode(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	r := e.call(http.MethodPost, "/v1/journals", ahkow, e.draft("100.00"))
	id := r.str("id")
	posted := e.call(http.MethodPost, "/v1/journals/"+id+"/post", ahkow, nil, "If-Match", r.Header.Get("ETag"))
	if posted.Status != 200 {
		t.Fatalf("posting: %d %s", posted.Status, posted.Raw)
	}
	tag := posted.Header.Get("ETag")
	body := e.edit("120.00", "2026-10-22", "Rent was 120")
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/edit", ahkow, body, "If-Match", tag), "editJournal", 409, CodeEditApproval)

	asked := e.call(http.MethodPost, "/v1/journals/"+id+"/edit-approval", ahkow, body, "If-Match", tag)
	if asked.Status != 202 || asked.str("operation") != approval.OpJournalEdit || !strings.HasSuffix(asked.str("summary"), ": Rent was 120") ||
		!strings.Contains(asked.Raw, `{"field":"debits","before":"100.00","after":"120.00"}`) ||
		!strings.Contains(asked.Raw, `{"field":"date","before":"2026-10-20","after":"2026-10-22"}`) ||
		!strings.Contains(asked.Raw, `"field":"line 1"`) || !strings.Contains(asked.str("envelope"), `"content_sha256":"`) ||
		strings.Contains(asked.str("envelope"), `"lines"`) {
		t.Fatalf("asking to edit: %d %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/journals/"+id, weiling, nil); g.str("approval_asked") != asked.str("id") {
		t.Errorf("the journal does not name the request: %s", g.Raw)
	}
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 {
		t.Fatalf("approving the edit: %d %s", a.Status, a.Raw)
	}
	g := e.call(http.MethodGet, "/v1/journals/"+id, ahkow, nil)
	if g.Header.Get("ETag") != `"2-posted"` || g.str("date") != "2026-10-22" || g.str("prepared_by", "id") != f.ahkow || g.str("posted_by", "id") != f.weiling {
		t.Fatalf("after the approval: %s", g.Raw)
	}
	if !strings.Contains(auditTrail(t, e), `"approval_ids":["`+asked.str("id")+`"],"action":"document.edited"`) {
		t.Errorf("the edit's audit event does not name its approval")
	}
	// A request at a version the journal has left is void.
	again := e.call(http.MethodPost, "/v1/journals/"+id+"/edit-approval", ahkow, e.edit("130.00", "2026-10-22", "Rent was 130"), "If-Match", g.Header.Get("ETag"))
	if again.Status != 202 {
		t.Fatalf("asking again: %d %s", again.Status, again.Raw)
	}
	reverse := e.call(http.MethodPost, "/v1/journals/"+id+"/reverse-approval", ahkow, map[string]any{"date": "2026-10-25", "reason": "x"}, "If-Match", g.Header.Get("ETag"))
	e.problemIs(reverse, "requestJournalReversal", 409, approval.CodePending)
}
