//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// Legal holds on records (AUD-11, rev 0.617; p2-security-certification D3): the DPO places a hold on a financial year, a contact and a
// record, each with a reason, and the Compliance page lists them with who placed them; a second hold on one subject, a hold of no
// subject or two subjects, a short reason and a record or contact that does not exist are refused; the DPO or an Owner lifts one with
// a reason, a second lift is refused, and the lifted hold leaves the list of those in force; each is in the audit trail and told to
// those who read the holds; a Controller reads and places nothing, and the Bookkeeper not even reads. The purge itself is the database's
// (TestAHoldKeepsTheTrailFromThePurgeAndTheDatabaseRefusesIt).
// req: AUD-11
// req: access/roles-and-segregation Permission matrix: Retention holds
// asvs: V8.2.1, V16.4.2
func TestLegalHoldsAreRecordedListedAndLifted(t *testing.T) {
	e := newEnv(t)
	e.booksFrom()
	dpo, owner := e.as(authz.DPO), e.as(authz.Owner)
	contact := e.supplier()
	bill, _ := e.postedBill("100.00", "0.00")
	list := func(state string) response {
		return e.call(http.MethodGet, "/v1/compliance/holds?state="+state, e.as(authz.Auditor), nil)
	}
	if r := list("in_force"); r.Status != 200 || r.Body["in_force"] != float64(0) {
		t.Fatalf("no hold at first: %d %s", r.Status, r.Raw)
	}
	year := e.call(http.MethodPost, "/v1/compliance/holds", dpo, map[string]any{"kind": "financial_year", "financial_year": 1, "reason": "An audit of the first year is open"}, "Idempotency-Key", "h-1")
	if year.Status != 201 || year.str("year_starts_on") != "2026-10-01" || year.str("year_ends_on") != "2026-12-31" || year.Body["in_force"] != true {
		t.Fatalf("a hold on the first year: %d %s", year.Status, year.Raw)
	}
	byContact := e.call(http.MethodPost, "/v1/compliance/holds", owner, map[string]any{"kind": "contact", "contact_id": contact, "reason": "The supplier disputes a bill"}, "Idempotency-Key", "h-2")
	if byContact.Status != 201 || byContact.str("contact_id") != contact || byContact.str("contact_name") == "" {
		t.Fatalf("a hold on a contact: %d %s", byContact.Status, byContact.Raw)
	}
	record := e.call(http.MethodPost, "/v1/compliance/holds", dpo, map[string]any{"kind": "record", "document_id": bill, "reason": "The bill is evidence in a claim"}, "Idempotency-Key", "h-3")
	if record.Status != 201 || record.str("document_id") != bill || record.str("document_number") == "" {
		t.Fatalf("a hold on a record: %d %s", record.Status, record.Raw)
	}
	if r := list("in_force"); r.Body["in_force"] != float64(3) || len(r.Body["items"].([]any)) != 3 {
		t.Fatalf("three holds stand: %s", r.Raw)
	}
	for _, c := range []struct {
		what string
		body map[string]any
		code string
		st   int
	}{
		{"a second hold on the year", map[string]any{"kind": "financial_year", "financial_year": 1, "reason": "again"}, storageHold, 409},
		{"a second hold on the contact", map[string]any{"kind": "contact", "contact_id": contact, "reason": "again"}, storageHold, 409},
		{"a hold of no subject", map[string]any{"kind": "record", "reason": "no subject"}, CodeInvalid, 422},
		{"a hold of two subjects", map[string]any{"kind": "record", "document_id": bill, "contact_id": contact, "reason": "two subjects"}, CodeInvalid, 422},
		{"a year with the contact's field", map[string]any{"kind": "financial_year", "financial_year": 2, "contact_id": contact, "reason": "year and contact"}, CodeInvalid, 422},
		{"a short reason", map[string]any{"kind": "financial_year", "financial_year": 2, "reason": "x"}, "", 422},
		{"a record that does not exist", map[string]any{"kind": "record", "document_id": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e99", "reason": "no such record"}, CodeNotFound, 404},
		{"a contact that does not exist", map[string]any{"kind": "contact", "contact_id": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e98", "reason": "no such contact"}, CodeNotFound, 404},
	} {
		r := e.call(http.MethodPost, "/v1/compliance/holds", dpo, c.body, "Idempotency-Key", "h-x-"+strings.ReplaceAll(c.what, " ", "-"))
		if r.Status != c.st || (c.code != "" && r.str("code") != c.code) {
			t.Errorf("%s: %d %s", c.what, r.Status, r.Raw)
		}
	}
	lift := "/v1/compliance/holds/" + record.str("hold_id") + "/lift"
	if r := e.call(http.MethodPost, lift, steppedUp(e, dpo), map[string]any{"reason": "x"}, "Idempotency-Key", "l-x1"); r.Status != 422 {
		t.Errorf("a short lift reason: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, lift, dpo, map[string]any{"reason": "Not stepped up"}, "Idempotency-Key", "l-0"), "liftRetentionHold", 403, CodeStepUpRequired)
	l := e.call(http.MethodPost, lift, steppedUp(e, dpo), map[string]any{"reason": "The claim is settled"}, "Idempotency-Key", "l-1")
	if l.Status != 200 || l.Body["in_force"] != false || l.str("lift_reason") != "The claim is settled" || l.str("lifted_by") == "" {
		t.Fatalf("lifting: %d %s", l.Status, l.Raw)
	}
	if r := e.call(http.MethodPost, lift, steppedUp(e, owner), map[string]any{"reason": "Lifting it again"}, "Idempotency-Key", "l-2"); r.Status != 409 || r.str("code") != storageHold {
		t.Errorf("a second lift: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/compliance/holds/01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e97/lift", steppedUp(e, owner), map[string]any{"reason": "No such hold"}, "Idempotency-Key", "l-3"); r.Status != 404 {
		t.Errorf("a hold that does not exist: %d %s", r.Status, r.Raw)
	}
	if r := list("in_force"); r.Body["in_force"] != float64(2) || len(r.Body["items"].([]any)) != 2 {
		t.Errorf("two holds stand after the lift: %s", r.Raw)
	}
	if r := list("all"); len(r.Body["items"].([]any)) != 3 || r.Body["in_force"] != float64(2) {
		t.Errorf("all three are listed: %s", r.Raw)
	}
	trail := auditTrail(t, e)
	if strings.Count(trail, "retention_hold.placed") != 3 || strings.Count(trail, "retention_hold.lifted") != 1 {
		t.Errorf("the holds are not in the audit trail as placed and lifted: %s", trail)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/holds", e.as(authz.Bookkeeper), nil), "listRetentionHolds", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/holds", e.as(authz.Controller), map[string]any{"kind": "financial_year", "financial_year": 1, "reason": "no right"},
		"Idempotency-Key", "h-c"), "placeRetentionHold", 403, CodeForbidden)
	if r := e.call(http.MethodGet, "/v1/compliance/holds", e.as(authz.Controller), nil); r.Status != 200 {
		t.Errorf("the Controller reads the holds: %d %s", r.Status, r.Raw)
	}
}

const storageHold = "STORAGE-HOLD-01"

// matrixRetentionHolds are the authorisation matrix's rows for the legal holds (AUD-11, Appendix D "Retention holds").
var matrixRetentionHolds = map[string]func(e *env, tok string) (response, func() bool){
	"listRetentionHolds": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/holds", tok, nil), nil
	},
	"placeRetentionHold": func(e *env, tok string) (response, func() bool) {
		contact := e.supplier()
		was := matrixHoldState(e)
		r := e.call(http.MethodPost, "/v1/compliance/holds", tok, map[string]any{"kind": "contact", "contact_id": contact, "reason": "matrix"}, "Prefer", "return=preview")
		return r, func() bool { return matrixHoldState(e) == was }
	},
	"liftRetentionHold": func(e *env, tok string) (response, func() bool) {
		id := matrixHold(e)
		was := matrixHoldState(e)
		r := e.call(http.MethodPost, "/v1/compliance/holds/"+id+"/lift", steppedUp(e, tok), map[string]any{"reason": "matrix"}, "Prefer", "return=preview")
		return r, func() bool { return matrixHoldState(e) == was }
	},
}

var matrixHoldOf = map[*env]string{}

// matrixHold is a hold on the first financial year, placed once, so the matrix lifts one that stands.
func matrixHold(e *env) string {
	e.t.Helper()
	if id, ok := matrixHoldOf[e]; ok {
		return id
	}
	e.booksFrom()
	r := e.call(http.MethodPost, "/v1/compliance/holds", e.as(authz.DPO), map[string]any{"kind": "financial_year", "financial_year": 1, "reason": "matrix"}, "Idempotency-Key", "matrix-hold")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's hold: %d %s", r.Status, r.Raw)
	}
	matrixHoldOf[e] = r.str("hold_id")
	return r.str("hold_id")
}

func matrixHoldState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/holds?state=all", e.as(authz.DPO), nil).Raw
}
