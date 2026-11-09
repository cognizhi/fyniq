//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
)

// Access and correction requests (CMP-PD-04, rev 0.244; p1-pdpa-retention D5): a member asks about their own data,
// signed in, due 30 days on, and sees only their own; the Compliance officer records a contact's request, acknowledges
// it, extends it within the first 30 days to a later day, reads the subject's data (audited), answers and closes; a step
// out of order, a stale one and an extension to an earlier day are refused; the reviews list says what waits; a
// Controller sees none of it, and a token cannot ask for a member.
// req: CMP-PD-04
// req: AUD-05
// req: access/roles-and-segregation Permission matrix: Own personal-data requests
// req: access/roles-and-segregation Permission matrix: Data protection (PDPA)
func TestAccessAndCorrectionRequests(t *testing.T) {
	e := newEnv(t)
	dpo := e.as(authz.DPO)
	hal := e.joined("Hal", "bookkeeper")
	member := e.personAs(hal, authz.Bookkeeper)
	mine := e.call(http.MethodPost, "/v1/me/personal-data-requests", member, map[string]any{"kind": "access", "details": "A copy of my data."},
		"Idempotency-Key", "my-1")
	today := e.clock.Now().In(time.FixedZone("SGT", 8*3600)).Format(time.DateOnly)
	if mine.Status != 201 || mine.str("subject") != "member:"+hal.String() || mine.str("received_on") != today || mine.str("status") != "received" {
		t.Fatalf("a member's request: %d %s", mine.Status, mine.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/me/personal-data-requests", member, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the member's own requests: %s", l.Raw)
	}

	// The Compliance officer records a contact's request and works it.
	contact := "contact:" + e.directorContact("Ada Tan")
	r := e.call(http.MethodPost, "/v1/compliance/personal-data-requests", dpo, map[string]any{"kind": "correction", "subject": contact,
		"requester": "Ada Tan, ada@example.com", "details": "My address is wrong."}, "Idempotency-Key", "rq-1")
	if r.Status != 201 || r.Header.Get("ETag") != `"s0"` {
		t.Fatalf("a contact's request: %d %s", r.Status, r.Raw)
	}
	path := "/v1/compliance/personal-data-requests/" + r.str("id")
	step := func(tag, key string, body map[string]any) response {
		return e.call(http.MethodPost, path+"/steps", dpo, body, "If-Match", tag, "Idempotency-Key", key)
	}
	e.problemIs(step(`"s0"`, "s-x", map[string]any{"step": "closed"}), "stepPersonalDataRequest", 409, "STORAGE-PDREQUEST-01")
	if s := step(`"s0"`, "s-1", map[string]any{"step": "acknowledged", "note": "Received, thank you."}); s.Status != 201 || s.str("status") != "acknowledged" {
		t.Fatalf("acknowledging: %d %s", s.Status, s.Raw)
	}
	e.problemIs(step(`"s0"`, "s-stale", map[string]any{"step": "answered", "outcome": "corrected"}), "stepPersonalDataRequest", 412, CodeStale)
	e.problemIs(step(`"s1"`, "s-early", map[string]any{"step": "extended", "new_due_on": "2020-01-01"}), "stepPersonalDataRequest", 409, "STORAGE-PDREQUEST-01")
	due := r.str("due_on")
	later := strings.Replace(due, due[:4], "2099", 1)
	if s := step(`"s1"`, "s-2", map[string]any{"step": "extended", "new_due_on": later, "note": "Told by email: we will answer by then."}); s.Status != 201 ||
		s.str("due_on") != later {
		t.Fatalf("extending: %d %s", s.Status, s.Raw)
	}
	rep := e.call(http.MethodGet, path+"/report", dpo, nil)
	if rep.Status != 200 || !strings.Contains(rep.Raw, "Ada Tan") {
		t.Fatalf("the subject's report: %d %s", rep.Status, rep.Raw)
	}
	e.problemIs(step(`"s2"`, "s-3", map[string]any{"step": "answered"}), "stepPersonalDataRequest", 422, CodeInvalid)
	if s := step(`"s2"`, "s-4", map[string]any{"step": "answered", "outcome": "corrected", "note": "Corrected the address."}); s.Status != 201 {
		t.Fatalf("answering: %d %s", s.Status, s.Raw)
	}
	if s := step(`"s3"`, "s-5", map[string]any{"step": "closed"}); s.Status != 201 || s.str("status") != "closed" {
		t.Fatalf("closing: %d %s", s.Status, s.Raw)
	}

	reviews := e.call(http.MethodGet, "/v1/compliance/reviews", e.as(authz.Auditor), nil).Body["items"].([]any)
	var line map[string]any
	for _, x := range reviews {
		if x.(map[string]any)["kind"] == "pd-requests" {
			line = x.(map[string]any)
		}
	}
	if line == nil || line["waiting"] != float64(1) || line["status"] != "due" {
		t.Errorf("the reviews' requests line: %v", line)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "pd_request.received") || !strings.Contains(trail, "personal_data.exported") ||
		!strings.Contains(trail, "pd_request.closed") {
		t.Error("the requests are not in the audit trail")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/personal-data-requests", e.as(authz.Controller), nil), "listPersonalDataRequests", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/me/personal-data-requests", e.as(authz.Bookkeeper), map[string]any{"kind": "access", "details": "x"},
		"Idempotency-Key", "tok-1"), "askAboutMyPersonalData", 403, CodeSessionRequired)
}

// matrixPDRequests are the authorisation matrix's rows for the requests (CMP-PD-04, Appendix D "Data protection (PDPA)" and
// "Own personal-data requests").
var matrixPDRequests = map[string]func(e *env, tok string) (response, func() bool){
	"listPersonalDataRequests": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/personal-data-requests", tok, nil), nil
	},
	"getPersonalDataReport": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/personal-data-requests/01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10/report", tok, nil), nil
	},
	"listMyPersonalDataRequests": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/me/personal-data-requests", tok, nil), nil
	},
	"createPersonalDataRequest": func(e *env, tok string) (response, func() bool) {
		was := matrixPDRequestState(e)
		r := e.call(http.MethodPost, "/v1/compliance/personal-data-requests", tok, map[string]any{"kind": "access", "subject": "person", "requester": "x",
			"details": "y"}, "Prefer", "return=preview")
		return r, func() bool { return matrixPDRequestState(e) == was }
	},
	"stepPersonalDataRequest": func(e *env, tok string) (response, func() bool) {
		was := matrixPDRequestState(e)
		r := e.call(http.MethodPost, "/v1/compliance/personal-data-requests/01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10/steps", tok, map[string]any{"step": "acknowledged"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPDRequestState(e) == was }
	},
	"askAboutMyPersonalData": func(e *env, tok string) (response, func() bool) {
		was := matrixPDRequestState(e)
		r := e.call(http.MethodPost, "/v1/me/personal-data-requests", tok, map[string]any{"kind": "access", "details": "y"}, "Prefer", "return=preview")
		return r, func() bool { return matrixPDRequestState(e) == was }
	},
}

func matrixPDRequestState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/personal-data-requests", e.as(authz.DPO), nil).Raw
}
