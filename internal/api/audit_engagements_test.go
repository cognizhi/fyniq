//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The Auditor's engagement (FR-UX-42; the owner's decision of 23 Nov 2026): an Owner opens a time-boxed engagement for a member holding the Auditor role and a
// second Owner confirms it (Team mode); only the engagement's own auditor asks, draws and accepts; the Controller, the Accountant or an Owner answers; nothing is added
// to a pending or closed engagement or outside its dates; a draw is reproducible from its seed and only reads the ledger; the auditor changes nothing else.
// req: FR-UX-42
// req: access/roles-and-segregation Auditor role
// asvs: V8.2.1, V8.3.1
func TestTheAuditorsEngagementIsOpenedByAnOwnerAndAskedAndSampledByTheAuditorOnly(t *testing.T) {
	e := newEnv(t)
	auditor := matrixAuditor(e)
	audTok := asMatrixMember(e, e.as(authz.Auditor))
	owner := asMatrixMember(e, e.as(authz.Owner))
	acc := e.as(authz.Accountant)
	body := matrixOpenBody(e, "FY2026 statutory audit")

	// Who opens: an Owner alone; the auditor must hold the Auditor role; the dates are bounded.
	for who, tok := range map[string]string{"an Auditor": audTok, "an Accountant": acc, "an Administrator": e.as(authz.Administrator), "a Board viewer": e.as(authz.BoardViewer)} {
		e.problemIs(e.call(http.MethodPost, "/v1/audit-engagements", tok, body), "openAuditEngagement", 403, "API-AUTHZ-01")
		_ = who
	}
	bad := func(what string, mutate func(m map[string]any), status int) {
		m := matrixOpenBody(e, "Bad engagement")
		mutate(m)
		if r := e.call(http.MethodPost, "/v1/audit-engagements", owner, m); r.Status != status {
			t.Errorf("%s = %d: %s", what, r.Status, r.Raw)
		}
	}
	bad("an auditor who holds no Auditor role", func(m map[string]any) { m["auditor_user_id"] = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01" }, 422)
	bad("more than 366 days", func(m map[string]any) { m["closes_on"] = "2027-10-03" }, 422)
	bad("a close before the open", func(m map[string]any) { m["closes_on"] = "2026-09-30" }, 422)
	bad("a close already past", func(m map[string]any) { m["opens_on"], m["closes_on"] = "2026-01-01", "2026-10-14" }, 422)
	bad("a period backwards", func(m map[string]any) { m["period_from"], m["period_to"] = "2026-12-31", "2026-01-01" }, 422)
	bad("a date that is not one", func(m map[string]any) { m["opens_on"] = "soon" }, 422)

	made := e.call(http.MethodPost, "/v1/audit-engagements", owner, body)
	if made.Status != http.StatusCreated || made.str("status") != "pending" || made.str("auditor_user_id") != auditor {
		t.Fatalf("opening: %d %s", made.Status, made.Raw)
	}
	id := made.str("id")

	// Pending: nothing is added until it is confirmed.
	ask := map[string]any{"title": "Bank confirmation at 31 December", "detail": "The main account", "due_on": "2026-11-20", "assignee_user_id": auditor}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/requests", audTok, ask); r.Status != http.StatusConflict || r.str("code") != CodeAuditWindow {
		t.Errorf("a request of a pending engagement: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/confirm", acc, nil); r.Status != http.StatusForbidden {
		t.Errorf("an Accountant confirming: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/confirm", owner, nil); r.Status != http.StatusOK || r.str("status") != "active" {
		t.Fatalf("confirming (Founder mode): %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/confirm", owner, nil); r.Status < 400 {
		t.Errorf("confirming twice: %d %s", r.Status, r.Raw)
	}

	// Asking: only the engagement's auditor, naming someone who can answer.
	for who, tok := range map[string]string{"an Owner": owner, "an Accountant": acc, "a Board viewer": e.as(authz.BoardViewer)} {
		if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/requests", tok, ask); r.Status != http.StatusForbidden {
			t.Errorf("%s asking: %d %s", who, r.Status, r.Raw)
		}
	}
	other := e.as(authz.Auditor) // an Auditor who is not this engagement's
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/requests", other, ask); r.Status != http.StatusForbidden || r.str("code") != CodeAuditAuditor {
		t.Errorf("another Auditor asking: %d %s", r.Status, r.Raw)
	}
	noOne := map[string]any{"title": "x y", "assignee_user_id": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/requests", audTok, noOne); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("naming a stranger: %d %s", r.Status, r.Raw)
	}
	req := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/requests", audTok, ask)
	if req.Status != http.StatusCreated || req.str("status") != "open" || req.Body["number"].(float64) != 1 {
		t.Fatalf("asking: %d %s", req.Status, req.Raw)
	}
	rid := req.str("id")

	// Answering: the person named or an Owner, with evidence that exists; closing: the auditor, once answered.
	if r := e.call(http.MethodPost, "/v1/audit-requests/"+rid+"/close", audTok, nil); r.Status != http.StatusConflict {
		t.Errorf("closing an unanswered request: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-requests/"+rid+"/answer", audTok, map[string]any{"answer": "Mine"}); r.Status != http.StatusForbidden {
		t.Errorf("the auditor answering their own request: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-requests/"+rid+"/answer", owner, map[string]any{"answer": "Attached", "evidence_ids": []string{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"}}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("evidence that does not exist: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-requests/"+rid+"/answer", owner, map[string]any{"answer": "Attached"}); r.Status != http.StatusOK || r.str("status") != "answered" {
		t.Fatalf("answering: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-requests/"+rid+"/close", owner, nil); r.Status != http.StatusForbidden {
		t.Errorf("an Owner closing the auditor's request: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-requests/"+rid+"/close", audTok, nil); r.Status != http.StatusOK || r.str("status") != "closed" {
		t.Fatalf("closing: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-requests/"+rid+"/answer", owner, map[string]any{"answer": "Again"}); r.Status < 400 {
		t.Errorf("answering a closed request: %d %s", r.Status, r.Raw)
	}

	// Samples: inside the period, reproducible by seed, read-only.
	draw := func(tok string, rule map[string]any, seed string) response {
		b := map[string]any{"rule": rule}
		if seed != "" {
			b["seed"] = seed
		}
		return e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/samples", tok, b)
	}
	rule := map[string]any{"from": "2026-01-01", "to": "2026-12-31", "count": 5}
	if r := draw(owner, rule, ""); r.Status != http.StatusForbidden {
		t.Errorf("an Owner drawing: %d %s", r.Status, r.Raw)
	}
	if r := draw(audTok, map[string]any{"from": "2025-12-31", "to": "2026-12-31", "count": 5}, ""); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a rule outside the period: %d %s", r.Status, r.Raw)
	}
	if r := draw(audTok, map[string]any{"from": "2026-01-01", "to": "2026-12-31", "count": 5, "min_amount": "lots"}, ""); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a minimum that is not a decimal: %d %s", r.Status, r.Raw)
	}
	if r := draw(audTok, map[string]any{"from": "2026-01-01", "to": "2026-12-31", "count": 5, "account": "9999"}, ""); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("an account that does not exist: %d %s", r.Status, r.Raw)
	}
	for _, amount := range []string{"100.00", "250.00", "400.00"} {
		e.postedJournal(amount)
	}
	// Over a ledger with postings: the population is exact, the sample keeps to the rule and is the same for the same seed, and nothing is posted.
	rent := map[string]any{"from": "2026-01-01", "to": "2026-12-31", "count": 2, "account": e.books.Rent.Code}
	r1, r2 := draw(audTok, rent, "seed-rent"), draw(audTok, rent, "seed-rent")
	items1 := r1.Body["items"].([]any)
	if r1.Status != http.StatusCreated || r1.Body["population"].(float64) != 3 || len(items1) != 2 || r2.Raw[strings.Index(r2.Raw, `"items"`):strings.Index(r2.Raw, `"drawn_at"`)] != r1.Raw[strings.Index(r1.Raw, `"items"`):strings.Index(r1.Raw, `"drawn_at"`)] {
		t.Fatalf("a sample over the ledger: %s / %s", r1.Raw, r2.Raw)
	}
	for _, x := range items1 {
		if l := x.(map[string]any); l["account"] != e.books.Rent.Code || l["source_type"] == "" {
			t.Errorf("a sampled line: %v", l)
		}
	}
	if big := draw(audTok, map[string]any{"from": "2026-01-01", "to": "2026-12-31", "count": 5, "account": e.books.Rent.Code, "min_amount": "300.00"}, "s"); big.Body["population"].(float64) != 1 {
		t.Errorf("a minimum of 300.00: %s", big.Raw)
	}
	first := draw(audTok, rule, "seed-one")
	again := draw(audTok, rule, "seed-one")
	if first.Status != http.StatusCreated || again.Status != http.StatusCreated || first.str("seed") != "seed-one" || first.Raw == "" {
		t.Fatalf("drawing: %d %s / %d %s", first.Status, first.Raw, again.Status, again.Raw)
	}
	if list := e.call(http.MethodGet, "/v1/audit-engagements/"+id+"/samples", owner, nil); list.Status != 200 || len(list.Body["items"].([]any)) != 5 {
		t.Errorf("the samples: %d %s", list.Status, list.Raw)
	}

	// Closing ends it: nothing more is added.
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/close", acc, nil); r.Status != http.StatusForbidden {
		t.Errorf("an Accountant closing: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/close", owner, nil); r.Status != http.StatusOK || r.str("status") != "closed" {
		t.Fatalf("closing the engagement: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/requests", audTok, ask); r.Status != http.StatusConflict || r.str("code") != CodeAuditWindow {
		t.Errorf("a request of a closed engagement: %d %s", r.Status, r.Raw)
	}
	if r := draw(audTok, rule, ""); r.Status != http.StatusConflict || r.str("code") != CodeAuditWindow {
		t.Errorf("a sample of a closed engagement: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/close", owner, nil); r.Status != http.StatusConflict {
		t.Errorf("closing twice: %d %s", r.Status, r.Raw)
	}

	// The window: an engagement that opens in the future takes nothing until its day.
	future := matrixOpenBody(e, "Next year")
	future["opens_on"], future["closes_on"] = "2026-11-01", "2026-12-31"
	f := e.call(http.MethodPost, "/v1/audit-engagements", owner, future)
	e.call(http.MethodPost, "/v1/audit-engagements/"+f.str("id")+"/confirm", owner, nil)
	if r := e.call(http.MethodPost, "/v1/audit-engagements/"+f.str("id")+"/requests", audTok, ask); r.Status != http.StatusConflict || !strings.Contains(r.Raw, "outside") {
		t.Errorf("a request before the engagement's first day: %d %s", r.Status, r.Raw)
	}

	// An Employee and the Board see none of it.
	for _, tok := range []string{e.as(authz.Employee), e.as(authz.BoardViewer)} {
		e.problemIs(e.call(http.MethodGet, "/v1/audit-engagements", tok, nil), "listAuditEngagements", 403, "API-AUTHZ-01")
	}
}
