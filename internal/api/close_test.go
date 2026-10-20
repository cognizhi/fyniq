//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The close is two acts by two people: one runs the checks, another attests and closes, on clean and current checks,
// in order, stepped up; the period then refuses every posting, and the close keeps its revision and snapshot.
// req: FR-GL-07
// req: FR-GL-08
// req: FM-4
// req: AUD-04
func TestThePeriodClose(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Controller), f.as(f.weiling, authz.Owner, authz.Controller)
	oct, nov := e.books.Oct.ID.String(), e.books.Nov.ID.String()
	state := func(period string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/periods/"+period+"/close", ahkow, nil)
		if r.Status != 200 {
			t.Fatalf("getPeriodClose: %d %s", r.Status, r.Raw)
		}
		return r
	}
	checks := func(who, period string) response {
		return e.call(http.MethodPost, "/v1/periods/"+period+"/close-checks", who, nil, "If-Match", state(period).Header.Get("ETag"))
	}
	closeIt := func(who, period, checksID string) response {
		return e.call(http.MethodPost, "/v1/periods/"+period+"/close", who, map[string]any{"checks_id": checksID,
			"statement": "I have reviewed the close and its checks."}, "If-Match", state(period).Header.Get("ETag"))
	}
	of := func(r response, key string) any {
		c, _ := r.Body["checks"].(map[string]any)
		return c[key]
	}
	severity := func(r response, key string) string {
		for _, c := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if c.(map[string]any)["key"] == key {
				return c.(map[string]any)["severity"].(string)
			}
		}
		return ""
	}
	e.postedJournal("100.00") // dated 20 October

	// Nothing yet.
	if r := state(oct); r.Body["checks"] != nil || r.Body["close"] != nil || r.str("period", "status") != "open" {
		t.Fatalf("a period never checked: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+oct+"/close-checks", ahkow, nil), "runCloseChecks", 428, CodeIfMatchRequired)

	// November first: October is open, so its checks refuse the close.
	r := checks(ahkow, nov)
	if r.Status != 200 || severity(r, "earlier") != "error" || of(r, "clean") != false {
		t.Fatalf("November's checks with October open: %d %s", r.Status, r.Raw)
	}
	e.problemIs(closeIt(weiling, nov, r.str("checks", "id")), "closePeriod", 409, closing.CodeNotClean)

	// October: Ah Kow runs the checks, which see a draft dated in the month (⚠, the close may go on).
	id, _ := e.createDraft("5.00")
	r = checks(ahkow, oct)
	if r.Status != 200 || of(r, "clean") != true || severity(r, "drafts") != "warning" || severity(r, "trial-balance") != "ok" ||
		r.str("checks", "checked_by", "name") != "Ah Kow" || r.Body["you_may_attest"] != false {
		t.Fatalf("October's checks: %d %s", r.Status, r.Raw)
	}
	first := r.str("checks", "id")
	// He may not attest his own checks (FM-4); Wei Ling may.
	e.problemIs(closeIt(f.as(f.ahkow, authz.Owner, authz.Controller), oct, first), "closePeriod", 403, closing.CodeSelfAttest)
	if r := e.call(http.MethodGet, "/v1/periods/"+oct+"/close", weiling, nil); r.Body["you_may_attest"] != true {
		t.Fatalf("Wei Ling may attest: %s", r.Raw)
	}

	// A back-dated posting after the checks makes them stale: the close refuses them.
	e.postedJournal("7.00")
	if r := state(oct); of(r, "stale") != true {
		t.Fatalf("the checks after a back-dated posting: %s", r.Raw)
	}
	e.problemIs(closeIt(weiling, oct, first), "closePeriod", 409, closing.CodeStale)

	// The attester steps up first.
	stale := e.principal(authn.Principal{Subject: "founder-old", UserID: uuid.MustParse(f.weiling), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now().Add(-10 * time.Minute)})
	r = checks(ahkow, oct)
	second := r.str("checks", "id")
	e.problemIs(closeIt(stale, oct, second), "closePeriod", 403, CodeStepUpRequired)

	// Wei Ling attests and closes: the period is closed at the ledger's revision, with the snapshot's hash.
	pv := e.call(http.MethodPost, "/v1/periods/"+oct+"/close", weiling, map[string]any{"checks_id": second, "statement": "Reviewed."},
		"If-Match", state(oct).Header.Get("ETag"), "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || state(oct).str("period", "status") != "open" {
		t.Fatalf("the preview closed it: %d %s", pv.Status, pv.Raw)
	}
	r = closeIt(weiling, oct, second)
	if r.Status != 200 || r.str("period", "status") != "closed" || r.str("close", "attested_by", "name") != "Wei Ling" ||
		r.str("close", "checks_id") != second || len(r.str("close", "snapshot_sha256")) != 64 || r.Body["checks"] != nil {
		t.Fatalf("the close: %d %s", r.Status, r.Raw)
	}
	if cl := r.Body["close"].(map[string]any); cl["closed_revision"] != r.Body["period"].(map[string]any)["closed_revision"] || cl["closed_revision"] == nil {
		t.Errorf("the close's revision %v is not the period's %v", cl["closed_revision"], r.Body["period"])
	}

	// Closed: nothing posts in it, by any path; its draft cannot post; nothing more is checked or closed.
	late := e.draft("3.00")
	late["date"] = "2026-10-31"
	l := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), late)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+l.str("id")+"/post", e.as(authz.Accountant), nil, "If-Match", l.Header.Get("ETag")),
		"postJournal", 409, storage.CodePeriodClosed)
	g := e.call(http.MethodGet, "/v1/journals/"+id, e.as(authz.Accountant), nil)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", e.as(authz.Accountant), nil, "If-Match", g.Header.Get("ETag")),
		"postJournal", 409, storage.CodePeriodClosed)
	e.problemIs(checks(ahkow, oct), "runCloseChecks", 409, closing.CodeNotOpen)

	// November now checks clean of October.
	if r := checks(ahkow, nov); severity(r, "earlier") != "ok" {
		t.Errorf("November after October's close: %s", r.Raw)
	}

	// The matrix: an Accountant prepares but never closes; the Owner closes but runs no checks; the AI does neither.
	e.problemIs(closeIt(f.as(f.ahkow, authz.Accountant), nov, second), "closePeriod", 403, CodeForbidden)
	e.problemIs(checks(f.as(f.weiling, authz.Owner), nov), "runCloseChecks", 403, CodeForbidden)
	trail := auditTrail(t, e)
	for _, want := range []string{`"action":"period.close_checked"`, `"action":"period.closed"`, `"action":"period.attested"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}
}
