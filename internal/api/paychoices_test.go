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
)

// FR-EQ-04 (PRD-001 rev 0.167; p1-founders-money design D8), worked by hand: with nothing recorded, Mei Lin's pay
// choice is the default, no salary. An Owner drafts "salary deferred" from October 2026 (any day of it counts as the
// month); it cannot be recorded until the signed decision is attached, and once recorded it changes no more. Her
// account then shows salary deferred from October, and no salary in September; a later choice, salary paid from
// November, supersedes it from its month. Only an Owner or the Controller drafts and records, and only for a director or
// shareholder.
// req: FR-EQ-04
// req: equity/founders-money Choice recorded with the signed decision
// req: equity/founders-money Default for new directors
func TestDirectorsPayChoices(t *testing.T) {
	e := newEnv(t)
	e.directorChart()
	mei := e.directorContact("Mei Lin")
	owner := e.as(authz.Owner)
	accountant := e.as(authz.Accountant)
	choiceOf := func(asAt string) map[string]any {
		r := e.call(http.MethodGet, "/v1/director-accounts?as_at="+asAt, accountant, nil)
		for _, it := range r.Body["items"].([]any) {
			if x := it.(map[string]any); x["contact_id"] == mei {
				return x["pay_choice"].(map[string]any)
			}
		}
		t.Fatalf("no account for Mei Lin: %s", r.Raw)
		return nil
	}
	if c := choiceOf("2026-10-31"); c["choice"] != director.ChoiceNoSalary || c["default"] != true {
		t.Errorf("with nothing recorded: %v", c)
	}

	draft := map[string]any{"contact_id": mei, "from": "2026-10-15", "choice": director.ChoiceSalaryDeferred, "note": "Board minute of 1 Oct 2026"}
	e.problemIs(e.call(http.MethodPost, "/v1/director-pay-choices", accountant, draft), "createDirectorPayChoice", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/director-pay-choices", owner, map[string]any{"contact_id": e.supplier(), "from": "2026-10-01",
		"choice": director.ChoiceNoSalary}), "createDirectorPayChoice", 422, director.CodeNotPerson)
	r := e.call(http.MethodPost, "/v1/director-pay-choices", owner, draft)
	if r.Status != http.StatusCreated || r.str("status") != "draft" || r.str("from") != "2026-10-01" || !strings.Contains(r.Raw, director.CodeNoDecision) {
		t.Fatalf("drafting: %d %s", r.Status, r.Raw)
	}
	path := "/v1/director-pay-choices/" + r.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/record", owner, nil, "If-Match", r.Header.Get("ETag")), "recordDirectorPayChoice", 422, director.CodeNoDecision)
	if f := e.upload(path+"/evidence", owner, map[string]string{"evidence_type": "board minute"}, "minute.pdf", []byte("%PDF-1.7 minute"),
		"If-Match", r.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the decision: %d %s", f.Status, f.Raw)
	}
	rec := e.call(http.MethodPost, path+"/record", owner, nil, "If-Match", r.Header.Get("ETag"))
	if rec.Status != http.StatusOK || rec.str("status") != "recorded" || rec.Body["decisions"] != float64(1) {
		t.Fatalf("recording: %d %s", rec.Status, rec.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, owner, draft, "If-Match", rec.Header.Get("ETag")), "replaceDirectorPayChoice", 409, director.CodeRecorded)

	if c := choiceOf("2026-10-31"); c["choice"] != director.ChoiceSalaryDeferred || c["default"] != false || c["from"] != "2026-10-01" {
		t.Errorf("in October: %v", c)
	}
	if c := choiceOf("2026-09-30"); c["choice"] != director.ChoiceNoSalary || c["default"] != true {
		t.Errorf("in September: %v", c)
	}

	// A later choice supersedes it from its month.
	later := e.call(http.MethodPost, "/v1/director-pay-choices", owner, map[string]any{"contact_id": mei, "from": "2026-11-01", "choice": director.ChoiceSalaryPaid})
	e.upload("/v1/director-pay-choices/"+later.str("id")+"/evidence", owner, map[string]string{"evidence_type": "board minute"}, "minute2.pdf",
		[]byte("%PDF-1.7 minute two"), "If-Match", later.Header.Get("ETag"))
	if x := e.call(http.MethodPost, "/v1/director-pay-choices/"+later.str("id")+"/record", owner, nil, "If-Match", later.Header.Get("ETag")); x.Status != http.StatusOK {
		t.Fatalf("recording the later choice: %d %s", x.Status, x.Raw)
	}
	if c := choiceOf("2026-11-30"); c["choice"] != director.ChoiceSalaryPaid {
		t.Errorf("in November: %v", c)
	}
	if c := choiceOf("2026-10-31"); c["choice"] != director.ChoiceSalaryDeferred {
		t.Errorf("October after the later choice: %v", c)
	}
	list := e.call(http.MethodGet, "/v1/director-pay-choices?contact_id="+mei+"&status=recorded", accountant, nil)
	if items, _ := list.Body["items"].([]any); len(items) != 2 || items[0].(map[string]any)["from"] != "2026-11-01" {
		t.Errorf("her recorded choices, the latest month first: %s", list.Raw)
	}
}

// matrixPayChoiceOf is the pay choice the matrix tries, drafted once per env in the costs' organisation for its
// director, with its signed decision attached: its ID and the file's.
var matrixPayChoiceOf = map[*env][2]string{}

// matrixPayChoice returns tok's principal moved into the costs' organisation, that organisation's fixture, and the pay
// choice and its file.
func matrixPayChoice(e *env, tok string) (string, preCostFixture, [2]string) {
	t, f := matrixPreCost(e, tok)
	pc, ok := matrixPayChoiceOf[e]
	if !ok {
		owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: uuid.MustParse(f.org), Roles: []authz.Role{authz.Owner}})
		c := e.call(http.MethodPost, "/v1/director-pay-choices", owner, map[string]any{"contact_id": f.person, "from": "2026-10-01", "choice": "no_salary"})
		if c.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's pay choice: %d %s", c.Status, c.Raw)
		}
		r := e.upload("/v1/director-pay-choices/"+c.str("id")+"/evidence", owner, map[string]string{"evidence_type": "board minute"}, "minute.pdf",
			[]byte("%PDF-1.7 matrix minute"), "If-Match", c.Header.Get("ETag"))
		if r.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's minute: %d %s", r.Status, r.Raw)
		}
		pc = [2]string{c.str("id"), r.str("id")}
		matrixPayChoiceOf[e] = pc
	}
	return t, f, pc
}

// matrixPayChoiceState is the pay choice's ETag and files, as an Owner of its organisation reads them.
func matrixPayChoiceState(e *env, f preCostFixture, pc [2]string) string {
	owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: uuid.MustParse(f.org), Roles: []authz.Role{authz.Owner}})
	c := e.call(http.MethodGet, "/v1/director-pay-choices/"+pc[0], owner, nil)
	ev := e.call(http.MethodGet, "/v1/director-pay-choices/"+pc[0]+"/evidence", owner, nil)
	l := e.call(http.MethodGet, "/v1/director-pay-choices", owner, nil)
	return c.Header.Get("ETag") + "|" + ev.Raw + "|" + l.Raw
}

var matrixPayChoices = map[string]func(e *env, tok string) (response, func() bool){
	"listDirectorPayChoices": func(e *env, tok string) (response, func() bool) {
		t, _, _ := matrixPayChoice(e, tok)
		return e.call(http.MethodGet, "/v1/director-pay-choices", t, nil), nil
	},
	"getDirectorPayChoice": func(e *env, tok string) (response, func() bool) {
		t, _, pc := matrixPayChoice(e, tok)
		return e.call(http.MethodGet, "/v1/director-pay-choices/"+pc[0], t, nil), nil
	},
	"listDirectorPayChoiceEvidence": func(e *env, tok string) (response, func() bool) {
		t, _, pc := matrixPayChoice(e, tok)
		return e.call(http.MethodGet, "/v1/director-pay-choices/"+pc[0]+"/evidence", t, nil), nil
	},
	"getDirectorPayChoiceEvidence": func(e *env, tok string) (response, func() bool) {
		t, _, pc := matrixPayChoice(e, tok)
		return e.call(http.MethodGet, "/v1/director-pay-choices/"+pc[0]+"/evidence/"+pc[1], t, nil), nil
	},
	// The writes: an allowed role previews, which saves nothing; a refused one changes nothing.
	"createDirectorPayChoice": func(e *env, tok string) (response, func() bool) {
		t, f, pc := matrixPayChoice(e, tok)
		was := matrixPayChoiceState(e, f, pc)
		r := e.call(http.MethodPost, "/v1/director-pay-choices", t, map[string]any{"contact_id": f.person, "from": "2026-11-01", "choice": "salary_paid"},
			"Prefer", "return=preview")
		return r, func() bool { return matrixPayChoiceState(e, f, pc) == was }
	},
	"replaceDirectorPayChoice": func(e *env, tok string) (response, func() bool) {
		t, f, pc := matrixPayChoice(e, tok)
		was := matrixPayChoiceState(e, f, pc)
		r := e.call(http.MethodPut, "/v1/director-pay-choices/"+pc[0], t, map[string]any{"contact_id": f.person, "from": "2026-10-01", "choice": "salary_paid"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPayChoiceState(e, f, pc) == was }
	},
	"recordDirectorPayChoice": func(e *env, tok string) (response, func() bool) {
		t, f, pc := matrixPayChoice(e, tok)
		was := matrixPayChoiceState(e, f, pc)
		r := e.call(http.MethodPost, "/v1/director-pay-choices/"+pc[0]+"/record", t, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPayChoiceState(e, f, pc) == was }
	},
	"addDirectorPayChoiceEvidence": func(e *env, tok string) (response, func() bool) {
		t, f, pc := matrixPayChoice(e, tok)
		was := matrixPayChoiceState(e, f, pc)
		r := e.upload("/v1/director-pay-choices/"+pc[0]+"/evidence", t, map[string]string{"evidence_type": "board minute"}, "more.pdf",
			[]byte("%PDF-1.7 more"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPayChoiceState(e, f, pc) == was }
	},
	"removeDirectorPayChoiceEvidence": func(e *env, tok string) (response, func() bool) {
		t, f, pc := matrixPayChoice(e, tok)
		was := matrixPayChoiceState(e, f, pc)
		r := e.call(http.MethodDelete, "/v1/director-pay-choices/"+pc[0]+"/evidence/"+pc[1], t, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixPayChoiceState(e, f, pc) == was }
	},
}
