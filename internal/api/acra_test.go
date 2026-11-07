//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The ACRA reconciliation (FR-EQ-26, design D11; the owner's decision of 7 Nov 2026). Ah Kow holds 700,000 ordinary
// shares and Lion Rock 300,000; with no extract, C.2 #26 is not assessed. The business profile of 1 Oct 2026 is typed in
// with Lion Rock at 299,000, its PDF attached and recorded by the Accountant, who may post: the register differs on Lion
// Rock's ordinary shares, 300,000 against 299,000. Typed in again from the profile of 2 Oct, it agrees, the first
// superseded. An Owner, who may not post, records none; an Employee reads none.
// req: FR-EQ-26
// asvs: V8.2.1
func TestTheRegisterReconcilesToACRA(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	})
	founder := e.directorContact("Ah Kow")
	lion := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Lion Rock Ventures Pte. Ltd.", "investor": true, "shareholder": true}).str("id")
	customer := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")
	ordinary := e.shareCapitalIn(e.books.Org)
	for _, x := range []struct{ holder, shares, on string }{{founder, "700000", "2025-01-02"}, {lion, "300000", "2025-03-14"}} {
		if r := e.call(http.MethodPost, "/v1/share-issues", weiling, map[string]any{"holder_id": x.holder, "class_account_id": ordinary, "shares": x.shares,
			"price": "0.01", "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": x.on}); r.Status != http.StatusCreated {
			t.Fatalf("the shares of %s: %d %s", x.holder, r.Status, r.Raw)
		}
	}
	wantStatus(t, e.checked("equity.acra-register"), "not_assessed")

	holding := func(holder, shares string) map[string]any {
		return map[string]any{"holder_id": holder, "class_account_id": ordinary, "shares": shares}
	}
	bad := map[string]any{"profile_on": "2099-01-01", "holdings": []map[string]any{holding(customer, "10"), holding(founder, "700000"), holding(founder, "1")}}
	if r := e.call(http.MethodPost, "/v1/acra-extracts", prep, bad); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodeAcra) ||
		!strings.Contains(r.Raw, `"field":"body.profile_on"`) || !strings.Contains(r.Raw, `"field":"body.holdings[0].holder_id"`) ||
		!strings.Contains(r.Raw, `"field":"body.holdings[2]"`) {
		t.Errorf("an extract that does not fit: %d %s", r.Status, r.Raw)
	}
	first := e.call(http.MethodPost, "/v1/acra-extracts", prep, map[string]any{"profile_on": "2026-10-01",
		"holdings": []map[string]any{holding(founder, "700000"), holding(lion, "299000")}})
	if first.Status != http.StatusCreated || !strings.Contains(first.Raw, equity.CodeNoEvidence) || !strings.Contains(first.Raw, `"holder_name":"Lion Rock Ventures Pte. Ltd."`) {
		t.Fatalf("the first extract: %d %s", first.Status, first.Raw)
	}
	path := "/v1/acra-extracts/" + first.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/record", prep, nil, "If-Match", first.Header.Get("ETag")), "recordAcraExtract", 422, equity.CodeNoEvidence)
	if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": "ACRA business profile"}, "bizfile.pdf", []byte("%PDF-1.7 bizfile"),
		"If-Match", first.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("the business profile: %d %s", f.Status, f.Raw)
	}
	cur := e.call(http.MethodGet, path, prep, nil)
	e.problemIs(e.call(http.MethodPost, path+"/record", weiling, nil, "If-Match", cur.Header.Get("ETag")), "recordAcraExtract", 403, CodeForbidden)
	if r := e.call(http.MethodPost, path+"/record", prep, nil, "If-Match", cur.Header.Get("ETag")); r.Status != http.StatusOK || r.str("status") != "recorded" ||
		r.Body["recorded_by"] == nil {
		t.Fatalf("recording the extract: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, prep, map[string]any{"profile_on": "2026-10-01", "holdings": []map[string]any{holding(founder, "700000")}},
		"If-Match", `"1-posted"`), "replaceAcraExtract", 409, equity.CodeRecorded)
	r := e.call(http.MethodGet, "/v1/cap-table/acra-reconciliation", weiling, nil)
	if r.Status != http.StatusOK || !strings.Contains(r.Raw, `"differences":[{"holder_id":"`+lion+`","holder_name":"Lion Rock Ventures Pte. Ltd.","class_account_id":"`+ordinary+
		`","class_code":"3010","register":"300000","acra":"299000"}]`) || !strings.Contains(r.Raw, `"profile_on":"2026-10-01"`) {
		t.Errorf("the register against the first extract: %d %s", r.Status, r.Raw)
	}
	check := e.checked("equity.acra-register")
	wantStatus(t, check, "warning")
	if it := itemNamed(check, lion+"/3010"); it == nil || !strings.Contains(it["detail"].(string), "The register holds 300,000 shares and ACRA's business profile of 1 Oct 2026 lists 299,000") {
		t.Errorf("C.2 #26: %s", check.Raw)
	}

	second := e.call(http.MethodPost, "/v1/acra-extracts", prep, map[string]any{"profile_on": "2026-10-02",
		"holdings": []map[string]any{holding(founder, "700000"), holding(lion, "300000")}, "note": "Typed in again from the profile of 2 Oct"})
	e.upload("/v1/acra-extracts/"+second.str("id")+"/evidence", prep, map[string]string{"evidence_type": "ACRA business profile"}, "bizfile2.pdf",
		[]byte("%PDF-1.7 bizfile 2"), "If-Match", second.Header.Get("ETag"))
	cur = e.call(http.MethodGet, "/v1/acra-extracts/"+second.str("id"), prep, nil)
	if r := e.call(http.MethodPost, "/v1/acra-extracts/"+second.str("id")+"/record", prep, nil, "If-Match", cur.Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("recording the second extract: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/cap-table/acra-reconciliation", weiling, nil); !strings.Contains(r.Raw, `"differences":[]`) || !strings.Contains(r.Raw, `"profile_on":"2026-10-02"`) {
		t.Errorf("the register against the second extract: %s", r.Raw)
	}
	l := e.call(http.MethodGet, "/v1/acra-extracts", weiling, nil)
	if items := l.Body["items"].([]any); len(items) != 2 || items[0].(map[string]any)["status"] != "recorded" || items[1].(map[string]any)["status"] != "superseded" {
		t.Errorf("the extracts, the latest first: %s", l.Raw)
	}
	wantStatus(t, e.checked("equity.acra-register"), "ok")
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table/acra-reconciliation", e.as(authz.Employee), nil), "getAcraReconciliation", 403, CodeForbidden)
}

// acraFixture is the matrix's ACRA extract: a draft with the business profile attached.
type acraFixture struct{ extract, file string }

var matrixAcraOf = map[*env]acraFixture{}

func matrixAcra(e *env) acraFixture {
	e.t.Helper()
	if f, ok := matrixAcraOf[e]; ok {
		return f
	}
	acc := e.as(authz.Accountant)
	var capital string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		for _, a := range all {
			if a.Role == storage.RoleShareCapital && a.Active && !a.Group {
				capital = a.ID.String()
			}
		}
		return err
	})
	if capital == "" {
		capital = e.shareCapitalIn(e.books.Org)
	}
	holder := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix ACRA holder", "investor": true}).str("id")
	x := e.call(http.MethodPost, "/v1/acra-extracts", acc, map[string]any{"profile_on": "2026-10-01",
		"holdings": []map[string]any{{"holder_id": holder, "class_account_id": capital, "shares": "100"}}})
	f := e.upload("/v1/acra-extracts/"+x.str("id")+"/evidence", acc, map[string]string{"evidence_type": "ACRA business profile"}, "bizfile.pdf",
		[]byte("%PDF-1.7 matrix bizfile"), "If-Match", x.Header.Get("ETag"))
	if x.Status != http.StatusCreated || f.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's extract: %d %d %s", x.Status, f.Status, x.Raw)
	}
	out := acraFixture{extract: x.str("id"), file: f.str("id")}
	matrixAcraOf[e] = out
	return out
}

// matrixAcraState is what a refused call must leave as it was: the draft, its files and the reconciliation.
func matrixAcraState(e *env, f acraFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/acra-extracts/" + f.extract, "/v1/acra-extracts/" + f.extract + "/evidence", "/v1/acra-extracts", "/v1/cap-table/acra-reconciliation"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

func acraBody(e *env, f acraFixture) map[string]any {
	cur := e.call(http.MethodGet, "/v1/acra-extracts/"+f.extract, e.as(authz.Accountant), nil)
	var holdings []map[string]any
	for _, h := range cur.Body["holdings"].([]any) {
		m := h.(map[string]any)
		holdings = append(holdings, map[string]any{"holder_id": m["holder_id"], "class_account_id": m["class_account_id"], "shares": m["shares"]})
	}
	return map[string]any{"profile_on": cur.Body["profile_on"], "holdings": holdings}
}

// matrixAcra are the authorisation matrix's rows for the ACRA reconciliation (FR-EQ-26).
var matrixAcraRows = map[string]func(e *env, tok string) (response, func() bool){
	"listAcraExtracts": func(e *env, tok string) (response, func() bool) {
		matrixAcra(e)
		return e.call(http.MethodGet, "/v1/acra-extracts", tok, nil), nil
	},
	"createAcraExtract": func(e *env, tok string) (response, func() bool) {
		f := matrixAcra(e)
		was := matrixAcraState(e, f)
		r := e.call(http.MethodPost, "/v1/acra-extracts", tok, acraBody(e, f), "Prefer", "return=preview")
		return r, func() bool { return matrixAcraState(e, f) == was }
	},
	"getAcraExtract": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/acra-extracts/"+matrixAcra(e).extract, tok, nil), nil
	},
	"replaceAcraExtract": func(e *env, tok string) (response, func() bool) {
		f := matrixAcra(e)
		was := matrixAcraState(e, f)
		r := e.call(http.MethodPut, "/v1/acra-extracts/"+f.extract, tok, acraBody(e, f), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixAcraState(e, f) == was }
	},
	"recordAcraExtract": func(e *env, tok string) (response, func() bool) {
		f := matrixAcra(e)
		was := matrixAcraState(e, f)
		r := e.call(http.MethodPost, "/v1/acra-extracts/"+f.extract+"/record", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixAcraState(e, f) == was }
	},
	"addAcraExtractEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixAcra(e)
		was := matrixAcraState(e, f)
		r := e.upload("/v1/acra-extracts/"+f.extract+"/evidence", tok, map[string]string{"evidence_type": "ACRA business profile"}, "again.pdf",
			[]byte("%PDF-1.7 again"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixAcraState(e, f) == was }
	},
	"listAcraExtractEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/acra-extracts/"+matrixAcra(e).extract+"/evidence", tok, nil), nil
	},
	"getAcraExtractEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixAcra(e)
		return e.call(http.MethodGet, "/v1/acra-extracts/"+f.extract+"/evidence/"+f.file, tok, nil), nil
	},
	"removeAcraExtractEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixAcra(e)
		was := matrixAcraState(e, f)
		r := e.call(http.MethodDelete, "/v1/acra-extracts/"+f.extract+"/evidence/"+f.file, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixAcraState(e, f) == was }
	},
	"getAcraReconciliation": func(e *env, tok string) (response, func() bool) {
		matrixAcra(e)
		return e.call(http.MethodGet, "/v1/cap-table/acra-reconciliation", tok, nil), nil
	},
}
