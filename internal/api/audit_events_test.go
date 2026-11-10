//go:build local

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// recordAudit writes one event about record as a person, from an address.
func recordAudit(t *testing.T, e *env, record uuid.UUID, action string, changes map[string][2]any) {
	t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "explorer-user"},
		Request: storage.Request{ID: "req-x", IP: "203.0.113.7", UserAgent: "Firefox/140.0", Session: "s-x"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Audit().Record(ctx, storage.AuditRecord{Action: action, ObjectType: "contact", ObjectID: record, Changes: changes})
	}); err != nil {
		t.Fatal(err)
	}
}

// The audit trail explorer (FR-CTL-03, AUD-07): a person with V on "Audit trail" lists the events newest first,
// filtered by the actor, the record, a field, the action and the days, a page at a time; opens one with each field's
// before and after; and exports what the filters match. A sensitive value shows only whether it changed, or changed
// back (AUD-02); a security event shows its IP address while it is kept (AUD-06). A Bookkeeper sees none of it.
// req: FR-CTL-03
// req: AUD-07
// req: AUD-02
// req: REL-05
// req: AUD-05
func TestTheAuditTrailExplorer(t *testing.T) {
	e := newEnv(t)
	journal, _ := e.postedJournal("100.00")
	contact := uuid.New()
	recordAudit(t, e, contact, "contact.bank_changed", map[string][2]any{"account_number": {"0123456789", "9876543210"}, "note": {nil, "new bank"}})
	recordAudit(t, e, contact, "contact.bank_changed", map[string][2]any{"account_number": {"9876543210", "0123456789"}})
	recordAudit(t, e, contact, "contact.bank_checked", map[string][2]any{"account_number": {"0123456789", "0123456789"}})
	recordAudit(t, e, contact, "session.started", nil)
	auditor := e.as(authz.Auditor)
	list := func(query string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/audit/events?"+query, auditor, nil)
		if r.Status != 200 {
			t.Fatalf("listing %s: %d %s", query, r.Status, r.Raw)
		}
		return r
	}
	items := func(r response) []map[string]any {
		var out []map[string]any
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}

	all := items(list(""))
	if len(all) < 6 || all[0]["action"] != "session.started" || all[0]["seq"].(float64) <= all[1]["seq"].(float64) {
		t.Fatalf("every event, newest first: %v", all[:2])
	}
	byRecord := items(list("record_type=contact&record_id=" + contact.String()))
	if len(byRecord) != 4 {
		t.Errorf("the contact's events: %v", byRecord)
	}
	if got := items(list("action=contact.bank_&record_id=" + contact.String())); len(got) != 0 {
		t.Errorf("an action prefix must end in a dot: %v", got)
	}
	if got := items(list("action=contact.&field=note")); len(got) != 1 || got[0]["action"] != "contact.bank_changed" {
		t.Errorf("by action prefix and field: %v", got)
	}
	if got := items(list("actor=explorer-user")); len(got) != 4 {
		t.Errorf("by actor: %v", got)
	}
	if got := items(list("record_id=" + journal)); len(got) == 0 {
		t.Errorf("the journal's events: %v", got)
	}
	if got := items(list("from=2020-01-01&to=2020-01-31")); len(got) != 0 {
		t.Errorf("days with nothing: %v", got)
	}
	first := list("page_size=2")
	next := first.str("next_cursor")
	second := items(list("page_size=2&cursor=" + url.QueryEscape(next)))
	if next == "" || len(second) != 2 || second[0]["seq"].(float64) >= items(first)[1]["seq"].(float64) {
		t.Errorf("paging: %s then %v", first.Raw, second)
	}

	// One event: before and after; a sensitive value as changed, changed back or unchanged, with its hint.
	open := func(id any) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/audit/events/"+id.(string), auditor, nil)
		if r.Status != 200 {
			t.Fatalf("opening %v: %d %s", id, r.Status, r.Raw)
		}
		return r
	}
	proofOf := func(r response) map[string]any {
		for _, c := range r.Body["changes"].([]any) {
			if c.(map[string]any)["field"] == "account_number" {
				return c.(map[string]any)
			}
		}
		t.Fatalf("no account_number: %s", r.Raw)
		return nil
	}
	changed, back, same := open(byRecord[3]["id"]), open(byRecord[2]["id"]), open(byRecord[1]["id"])
	if p := proofOf(changed); p["proof"] != audit.ProofChanged || p["hint"] != "******3210" || strings.Contains(changed.Raw, "9876543210") {
		t.Errorf("a changed number: %v", p)
	}
	if p := proofOf(back); p["proof"] != audit.ProofChangedBack {
		t.Errorf("a number changed back: %v", p)
	}
	if p := proofOf(same); p["proof"] != audit.ProofUnchanged {
		t.Errorf("a number unchanged: %v", p)
	}
	if !strings.Contains(changed.Raw, `"field":"note","before":null,"after":"new bank"`) || changed.str("request", "details") != "in_body" ||
		changed.str("request", "ip") != "203.0.113.7" || len(changed.str("hash")) != 64 {
		t.Errorf("an event's fields, request and chain: %s", changed.Raw)
	}
	if s := open(byRecord[0]["id"]); s.str("request", "details") != "kept" || s.str("request", "ip") != "203.0.113.7" {
		t.Errorf("a sign-in's details beside the chain: %s", s.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/audit/events/"+uuid.NewString(), auditor, nil), "getAuditEvent", 404, CodeNotFound)

	// The export: the events the filters match over the days, oldest first, audited.
	x := e.call(http.MethodPost, "/v1/audit/exports", auditor, map[string]any{"from": "2020-01-01", "to": "2099-12-31", "format": "csv",
		"filters": map[string]any{"record_id": contact.String()}})
	if x.Status != 202 || x.str("report") != reportexport.AuditTrail {
		t.Fatalf("asking for the export: %d %s", x.Status, x.Raw)
	}
	worker := workerStore(t)
	p := reportexport.Preparer{Store: worker, Files: e.blobs, Outbox: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock}, Clock: e.clock,
		Stamp: "0.0.0-test (3f2c1a9d)"}
	payload, _ := json.Marshal(map[string]string{"export_id": x.str("id")})
	if err := p.Work()(context.Background(), e.books.Org, storage.OutboxRow{Kind: reportexport.Kind, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	f := e.call(http.MethodGet, "/v1/report-exports/"+x.str("id")+"/file", auditor, nil)
	content, _ := base64.StdEncoding.DecodeString(f.str("content_base64"))
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	// The header, four events, a blank line and the footer naming the version and the commit (REL-05).
	if f.Status != 200 || len(lines) != 7 || !strings.Contains(lines[1], "contact.bank_changed") || !strings.Contains(lines[4], "session.started") ||
		strings.TrimSpace(lines[5]) != "" || !strings.HasPrefix(lines[6], `"Prepared by Fyniq 0.0.0-test (3f2c1a9d) on `) ||
		strings.Contains(string(content), "9876543210") {
		t.Errorf("the export: %d\n%s", f.Status, content)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `"action":"report_export.requested"`) {
		t.Error("the export is not in the trail")
	}
	e.problemIs(e.call(http.MethodPost, "/v1/report-exports", auditor, map[string]any{"report": "audit_trail", "to": "2026-10-31", "from": "2026-10-01"}),
		"requestReportExport", 422, CodeInvalid)

	// Without V on "Audit trail": nothing.
	e.problemIs(e.call(http.MethodGet, "/v1/audit/events", e.as(authz.Bookkeeper), nil), "listAuditEvents", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/audit/exports", e.as(authz.Bookkeeper), map[string]any{"from": "2026-10-01", "to": "2026-10-31"}),
		"requestAuditTrailExport", 403, CodeForbidden)
}

// matrixAuditEvent is the matrix organisation's newest event, for opening one.
func matrixAuditEvent(e *env) string {
	e.t.Helper()
	var id string
	if err := workerStore(e.t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		h, err := u.Audit().Head(ctx)
		if err != nil {
			return err
		}
		events, err := u.Audit().Events(ctx, h.Seq-1, 1)
		if err == nil && len(events) == 1 {
			id = events[0].ID.String()
		}
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// matrixAuditEvents are the authorisation matrix's rows for the explorer (FR-CTL-03).
var matrixAuditEvents = map[string]func(e *env, tok string) (response, func() bool){
	"listAuditEvents": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/audit/events?page_size=5", tok, nil), nil
	},
	"getAuditEvent": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/audit/events/"+matrixAuditEvent(e), tok, nil), nil
	},
	"requestAuditTrailExport": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/audit/exports", tok, map[string]any{"from": "2026-10-01", "to": "2026-10-31", "format": "csv"}), nil
	},
}

// The explorer opens a person's sealed values (§11.8, rev 0.244; p1-pdpa-retention D8): an individual contact's name,
// email and phone are sealed in the trail, and getAuditEvent shows them to the Auditor, marked opened; a company's are
// kept as written and marked nothing; the list gives no values.
// req: FR-CTL-03
// req: AUD-02
// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
func TestTheExplorerOpensSealedValues(t *testing.T) {
	e := newEnv(t)
	owner := e.as(authz.Accountant)
	person := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Ada Tan", "customer": true, "individual": true,
		"billing_email": "ada@example.com", "phone": "+65 9123 4567"})
	company := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Northwind Pte Ltd", "vendor": true})
	if person.Status != 201 || person.Body["individual"] != true || company.Status != 201 {
		t.Fatalf("the contacts: %d %s %d", person.Status, person.Raw, company.Status)
	}
	auditor := e.as(authz.Auditor)
	event := func(id string) map[string]map[string]any {
		t.Helper()
		l := e.call(http.MethodGet, "/v1/audit/events?record_type=contact&record_id="+id, auditor, nil)
		items, _ := l.Body["items"].([]any)
		if l.Status != 200 || len(items) != 1 || strings.Contains(l.Raw, "Ada") {
			t.Fatalf("the contact's events: %d %s", l.Status, l.Raw)
		}
		r := e.call(http.MethodGet, "/v1/audit/events/"+items[0].(map[string]any)["id"].(string), auditor, nil)
		out := map[string]map[string]any{}
		for _, c := range r.Body["changes"].([]any) {
			out[c.(map[string]any)["field"].(string)] = c.(map[string]any)
		}
		return out
	}
	p := event(person.str("id"))
	if p["legal_name"]["after"] != "Ada Tan" || p["legal_name"]["sealed"] != "opened" || p["billing_email"]["after"] != "ada@example.com" ||
		p["phone"]["after"] != "+65 9123 4567" || p["is_customer"]["sealed"] != nil {
		t.Errorf("the person's event: %v", p)
	}
	if c := event(company.str("id")); c["legal_name"]["after"] != "Northwind Pte Ltd" || c["legal_name"]["sealed"] != nil {
		t.Errorf("the company's event: %v", c)
	}
}
