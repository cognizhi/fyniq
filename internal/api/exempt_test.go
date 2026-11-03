//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
)

// FR-EQ-11 (PRD-001 rev 0.167; design D9), worked by hand: S$400.00 is paid out to Mei Lin on 5 Oct 2026 with nothing
// owed to her, so her account is in debit. With no status recorded, the accounts say so and her card cites s162 as for
// a company that is not exempt. An Owner records the company exempt from 14 Mar 2025, with the ACRA business profile
// attached (refused without it); her card then says the loan is allowed but disclosed. A status recorded changes no
// more; only an Owner or the Controller records one.
// req: FR-EQ-11
// req: equity/founders-money Status recorded before the corporate profile (rev 0.167)
// req: equity/founders-money No status recorded (rev 0.167)
// req: equity/founders-money Message for a company that is not exempt
func TestTheExemptPrivateCompanyStatus(t *testing.T) {
	e := newEnv(t)
	loan, _ := e.directorChart()
	mei := e.directorContact("Mei Lin")
	bank := e.books.Bank.ID.String()
	if r := e.postFounderJournal("2026-10-05", founderLine(loan, "400.00", mei, ledger.OriginPaidOut), founderLine(bank, "-400.00", "", "")); r.Status != http.StatusOK {
		t.Fatalf("paying Mei Lin out: %d %s", r.Status, r.Raw)
	}
	owner := e.as(authz.Owner)
	accounts := func() response { return e.call(http.MethodGet, "/v1/director-accounts?as_at=2026-10-31", e.as(authz.Accountant), nil) }
	warning := func(r response) string {
		for _, it := range r.Body["items"].([]any) {
			if x := it.(map[string]any); x["contact_id"] == mei {
				w, _ := x["debit_warning"].(string)
				return w
			}
		}
		return ""
	}
	before := accounts()
	if before.str("exempt_status", "status") != "not_recorded" || !strings.Contains(warning(before), "section 162 of the Companies Act bans loans to directors") {
		t.Errorf("with no status recorded: %s", before.Raw)
	}

	draft := map[string]any{"from": "2025-03-14", "exempt": true, "note": "ACRA business profile of 14 Mar 2025"}
	e.problemIs(e.call(http.MethodPost, "/v1/exempt-statuses", e.as(authz.Accountant), draft), "createExemptStatus", 403, CodeForbidden)
	r := e.call(http.MethodPost, "/v1/exempt-statuses", owner, draft)
	if r.Status != http.StatusCreated || r.str("status") != "draft" || !strings.Contains(r.Raw, director.CodeNoProfile) {
		t.Fatalf("drafting: %d %s", r.Status, r.Raw)
	}
	path := "/v1/exempt-statuses/" + r.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/record", owner, nil, "If-Match", r.Header.Get("ETag")), "recordExemptStatus", 422, director.CodeNoProfile)
	if f := e.upload(path+"/evidence", owner, map[string]string{"evidence_type": "ACRA business profile"}, "bizfile.pdf", []byte("%PDF-1.7 bizfile"),
		"If-Match", r.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the profile: %d %s", f.Status, f.Raw)
	}
	rec := e.call(http.MethodPost, path+"/record", owner, nil, "If-Match", r.Header.Get("ETag"))
	if rec.Status != http.StatusOK || rec.str("status") != "recorded" {
		t.Fatalf("recording: %d %s", rec.Status, rec.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, owner, draft, "If-Match", rec.Header.Get("ETag")), "replaceExemptStatus", 409, director.CodeStatusRecorded)

	after := accounts()
	if after.str("exempt_status", "status") != "exempt" || after.str("exempt_status", "from") != "2025-03-14" ||
		!strings.Contains(warning(after), "exempt private company, so a loan to a director is allowed") {
		t.Errorf("once exempt: %s", after.Raw)
	}
}

// matrixExemptOf is the status the matrix tries, drafted once per env in the costs' organisation with its evidence
// attached: its ID and the file's.
var matrixExemptOf = map[*env][2]string{}

func matrixExempt(e *env, tok string) (string, preCostFixture, [2]string) {
	t, f := matrixPreCost(e, tok)
	x, ok := matrixExemptOf[e]
	if !ok {
		owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: uuid.MustParse(f.org), Roles: []authz.Role{authz.Owner}})
		c := e.call(http.MethodPost, "/v1/exempt-statuses", owner, map[string]any{"from": "2025-03-14", "exempt": true})
		if c.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's status: %d %s", c.Status, c.Raw)
		}
		r := e.upload("/v1/exempt-statuses/"+c.str("id")+"/evidence", owner, map[string]string{"evidence_type": "ACRA business profile"}, "bizfile.pdf",
			[]byte("%PDF-1.7 matrix bizfile"), "If-Match", c.Header.Get("ETag"))
		if r.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's profile: %d %s", r.Status, r.Raw)
		}
		x = [2]string{c.str("id"), r.str("id")}
		matrixExemptOf[e] = x
	}
	return t, f, x
}

func matrixExemptState(e *env, f preCostFixture, x [2]string) string {
	owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: uuid.MustParse(f.org), Roles: []authz.Role{authz.Owner}})
	c := e.call(http.MethodGet, "/v1/exempt-statuses/"+x[0], owner, nil)
	ev := e.call(http.MethodGet, "/v1/exempt-statuses/"+x[0]+"/evidence", owner, nil)
	l := e.call(http.MethodGet, "/v1/exempt-statuses", owner, nil)
	return c.Header.Get("ETag") + "|" + ev.Raw + "|" + l.Raw
}

var matrixExemptStatuses = map[string]func(e *env, tok string) (response, func() bool){
	"listExemptStatuses": func(e *env, tok string) (response, func() bool) {
		t, _, _ := matrixExempt(e, tok)
		return e.call(http.MethodGet, "/v1/exempt-statuses", t, nil), nil
	},
	"getExemptStatus": func(e *env, tok string) (response, func() bool) {
		t, _, x := matrixExempt(e, tok)
		return e.call(http.MethodGet, "/v1/exempt-statuses/"+x[0], t, nil), nil
	},
	"listExemptStatusEvidence": func(e *env, tok string) (response, func() bool) {
		t, _, x := matrixExempt(e, tok)
		return e.call(http.MethodGet, "/v1/exempt-statuses/"+x[0]+"/evidence", t, nil), nil
	},
	"getExemptStatusEvidence": func(e *env, tok string) (response, func() bool) {
		t, _, x := matrixExempt(e, tok)
		return e.call(http.MethodGet, "/v1/exempt-statuses/"+x[0]+"/evidence/"+x[1], t, nil), nil
	},
	// The writes: an allowed role previews, which saves nothing; a refused one changes nothing.
	"createExemptStatus": func(e *env, tok string) (response, func() bool) {
		t, f, x := matrixExempt(e, tok)
		was := matrixExemptState(e, f, x)
		r := e.call(http.MethodPost, "/v1/exempt-statuses", t, map[string]any{"from": "2026-09-01", "exempt": false}, "Prefer", "return=preview")
		return r, func() bool { return matrixExemptState(e, f, x) == was }
	},
	"replaceExemptStatus": func(e *env, tok string) (response, func() bool) {
		t, f, x := matrixExempt(e, tok)
		was := matrixExemptState(e, f, x)
		r := e.call(http.MethodPut, "/v1/exempt-statuses/"+x[0], t, map[string]any{"from": "2025-03-15", "exempt": true}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixExemptState(e, f, x) == was }
	},
	"recordExemptStatus": func(e *env, tok string) (response, func() bool) {
		t, f, x := matrixExempt(e, tok)
		was := matrixExemptState(e, f, x)
		r := e.call(http.MethodPost, "/v1/exempt-statuses/"+x[0]+"/record", t, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixExemptState(e, f, x) == was }
	},
	"addExemptStatusEvidence": func(e *env, tok string) (response, func() bool) {
		t, f, x := matrixExempt(e, tok)
		was := matrixExemptState(e, f, x)
		r := e.upload("/v1/exempt-statuses/"+x[0]+"/evidence", t, map[string]string{"evidence_type": "ACRA business profile"}, "more.pdf",
			[]byte("%PDF-1.7 more"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixExemptState(e, f, x) == was }
	},
	"removeExemptStatusEvidence": func(e *env, tok string) (response, func() bool) {
		t, f, x := matrixExempt(e, tok)
		was := matrixExemptState(e, f, x)
		r := e.call(http.MethodDelete, "/v1/exempt-statuses/"+x[0]+"/evidence/"+x[1], t, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixExemptState(e, f, x) == was }
	},
}
