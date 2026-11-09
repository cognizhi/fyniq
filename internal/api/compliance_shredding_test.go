//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// listRun lists a shredding run of 2015's keys in e's organisation, as the daily job does.
func listRun(t *testing.T, e *env) {
	t.Helper()
	if err := workerStore(t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Compliance().ListShreddingRun(ctx, time.Date(2015, 12, 31, 0, 0, 0, 0, time.UTC), []uuid.UUID{uuid.New(), uuid.New()}, e.clock.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// The shredding runs and the anonymisations (§11.8, §11.9; rev 0.244; p1-pdpa-retention D9, D10): a year past its
// retention waits for the DPO; approving needs step-up and the run's version, and goes once; the daily run then
// shreds it, register first; the DPO anonymises a person, register first, whose emails and phone go and whose address
// reads Anonymised while their name stays, and refuses a company; a Controller sees none of it.
// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
// req: data/backup-and-recovery Retention of personal data not needed for records
// req: access/roles-and-segregation Permission matrix: Data protection (PDPA)
func TestShreddingAndAnonymisation(t *testing.T) {
	e := newEnv(t)
	listRun(t, e)
	dpo := e.personAs(uuid.New(), authz.DPO)
	runs := e.call(http.MethodGet, "/v1/compliance/shredding-runs", e.as(authz.Auditor), nil)
	items, _ := runs.Body["items"].([]any)
	if runs.Status != 200 || len(items) != 1 {
		t.Fatalf("the runs: %d %s", runs.Status, runs.Raw)
	}
	run := items[0].(map[string]any)
	if run["status"] != "listed" || run["people"] != float64(2) || run["cohort"] != "2015-12-31" {
		t.Fatalf("the run: %v", run)
	}
	path := "/v1/compliance/shredding-runs/" + run["id"].(string) + "/approval"
	e.problemIs(e.call(http.MethodPost, path, e.as(authz.DPO), nil, "If-Match", `"v1"`, "Idempotency-Key", "a-0"), "approveShreddingRun", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, path, dpo, nil, "If-Match", `"v9"`, "Idempotency-Key", "a-1"), "approveShreddingRun", 412, CodeStale)
	ok := e.call(http.MethodPost, path, dpo, nil, "If-Match", `"v1"`, "Idempotency-Key", "a-2")
	if ok.Status != 200 || ok.str("status") != "approved" || ok.Header.Get("ETag") != `"v2"` || !strings.HasPrefix(ok.str("approved_by"), "user:") {
		t.Fatalf("approving: %d %s", ok.Status, ok.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path, dpo, nil, "If-Match", `"v2"`, "Idempotency-Key", "a-3"), "approveShreddingRun", 409, storage.CodeShredding)
	if _, err := audit.Shred(context.Background(), workerStore(t), e.anchors, e.signer, nil, e.clock.Now(), e.books.Org); err != nil {
		t.Fatal(err)
	}
	if r := e.call(http.MethodGet, "/v1/compliance/shredding-runs", e.as(authz.Auditor), nil); !strings.Contains(r.Raw, `"status":"done"`) || !strings.Contains(r.Raw, "pdpa-shredding/") {
		t.Errorf("the run after the daily job: %s", r.Raw)
	}

	// Anonymising a person, never a company.
	acc := e.as(authz.Accountant)
	ada := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Ada Tan", "customer": true, "individual": true,
		"billing_email": "ada@example.com", "phone": "+65 9123 4567", "address": "2 Ada Road"})
	firm := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Northwind Pte Ltd", "vendor": true})
	body := map[string]any{"contact_id": ada.str("id"), "reason": "Their purpose ended: no business since 2022."}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/anonymisations", e.as(authz.DPO), body, "Idempotency-Key", "n-0"), "anonymiseContact", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/anonymisations", dpo, map[string]any{"contact_id": firm.str("id"), "reason": "x"}, "Idempotency-Key", "n-1"),
		"anonymiseContact", 422, CodeInvalid)
	if p := e.call(http.MethodPost, "/v1/compliance/anonymisations", dpo, body, "Prefer", "return=preview"); p.Status != 200 {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	if c := e.call(http.MethodGet, "/v1/contacts/"+ada.str("id"), acc, nil); c.str("phone") != "+65 9123 4567" {
		t.Errorf("a preview anonymised: %s", c.Raw)
	}
	n := e.call(http.MethodPost, "/v1/compliance/anonymisations", dpo, body, "Idempotency-Key", "n-2")
	if n.Status != 201 || n.str("contact_name") != "Ada Tan" {
		t.Fatalf("anonymising: %d %s", n.Status, n.Raw)
	}
	c := e.call(http.MethodGet, "/v1/contacts/"+ada.str("id"), acc, nil)
	if c.str("legal_name") != "Ada Tan" || c.str("billing_email") != "" || c.str("phone") != "" || c.str("address") != "Anonymised" {
		t.Errorf("the anonymised contact: %s", c.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/compliance/anonymisations", e.as(authz.Auditor), nil); !strings.Contains(l.Raw, "Ada Tan") {
		t.Errorf("the anonymisations: %s", l.Raw)
	}
	if keys, _ := e.anchors.List(context.Background(), audit.AnonymisationPrefix(e.books.Org)); len(keys) != 1 {
		t.Errorf("the anonymisation register: %v", keys)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "contact.anonymised") || !strings.Contains(trail, "pdpa.shredding_approved") {
		t.Error("the anonymisation or the approval is not in the audit trail")
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/shredding-runs", e.as(authz.Controller), nil), "listShreddingRuns", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/anonymisations", e.as(authz.Owner), body, "Idempotency-Key", "n-o"), "anonymiseContact", 403, CodeForbidden)
}

// matrixShredding are the authorisation matrix's rows for the shredding runs and the anonymisations (§11.8, §11.9,
// Appendix D "Data protection (PDPA)").
var matrixShredding = map[string]func(e *env, tok string) (response, func() bool){
	"listShreddingRuns": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/shredding-runs", tok, nil), nil
	},
	"approveShreddingRun": func(e *env, tok string) (response, func() bool) {
		id := matrixShreddingRun(e)
		was := matrixShreddingState(e)
		r := e.call(http.MethodPost, "/v1/compliance/shredding-runs/"+id+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixShreddingState(e) == was }
	},
	"listAnonymisations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/anonymisations", tok, nil), nil
	},
	"anonymiseContact": func(e *env, tok string) (response, func() bool) {
		id := matrixPerson(e)
		was := matrixShreddingState(e)
		r := e.call(http.MethodPost, "/v1/compliance/anonymisations", steppedUp(e, tok), map[string]any{"contact_id": id, "reason": "Matrix"},
			"Prefer", "return=preview")
		return r, func() bool { return matrixShreddingState(e) == was }
	},
}

var (
	matrixShreddingRunOf = map[*env]string{}
	matrixPersonOf       = map[*env]string{}
)

// matrixShreddingRun is a listed run of the matrix's organisation, made once, so the matrix approves one that exists.
func matrixShreddingRun(e *env) string {
	e.t.Helper()
	if id, ok := matrixShreddingRunOf[e]; ok {
		return id
	}
	listRun(e.t, e)
	r := e.call(http.MethodGet, "/v1/compliance/shredding-runs", e.as(authz.DPO), nil)
	matrixShreddingRunOf[e] = r.Body["items"].([]any)[0].(map[string]any)["id"].(string)
	return matrixShreddingRunOf[e]
}

// matrixPerson is a contact marked as a person, made once, so the matrix anonymises one the API accepts.
func matrixPerson(e *env) string {
	e.t.Helper()
	if id, ok := matrixPersonOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": "Matrix person", "customer": true, "individual": true})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's person: %d %s", r.Status, r.Raw)
	}
	matrixPersonOf[e] = r.str("id")
	return r.str("id")
}

func matrixShreddingState(e *env) string {
	return e.call(http.MethodGet, "/v1/compliance/shredding-runs", e.as(authz.DPO), nil).Raw +
		e.call(http.MethodGet, "/v1/compliance/anonymisations", e.as(authz.DPO), nil).Raw
}
