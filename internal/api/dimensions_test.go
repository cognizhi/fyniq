//go:build local

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/dimension"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-DIM-01 (p1-organisation-chart task 3.3, slice 2): the dimensions and their values are read by everyone who sees the
// chart and kept by those who keep it (Appendix D, "Chart of accounts, dimensions"). Department and Project once each
// and up to 8 custom dimensions, each named once; values with a code unique in their dimension; every problem at once;
// renamed and put out of use at the version read, never deleted; a preview saves nothing.
// req: FR-DIM-01
// req: ENG-22
// req: FR-INT-09
// req: FR-INT-14
func TestDimensionsAndTheirValues(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)

	if l := e.call(http.MethodGet, "/v1/dimensions", e.as(authz.Bookkeeper), nil); l.Status != 200 || len(l.Body["items"].([]any)) != 0 {
		t.Fatalf("a Bookkeeper lists none yet: %d %s", l.Status, l.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/dimensions", e.as(authz.Bookkeeper), map[string]any{"kind": "department", "name": "Department"}),
		"createDimension", 403, CodeForbidden)

	preview := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Region"}, "Prefer", "return=preview")
	if preview.Status != 200 || preview.Header.Get("Preference-Applied") != "return=preview" || preview.Body["place"] != float64(1) {
		t.Fatalf("a preview: %d %s", preview.Status, preview.Raw)
	}
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": " Department "})
	if dept.Status != 201 || dept.str("name") != "Department" || dept.Body["place"] != nil || dept.Header.Get("ETag") != etag(1) {
		t.Fatalf("Department: %d %s", dept.Status, dept.Raw)
	}
	region := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Region"})
	grant := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Grant"})
	if region.Body["place"] != float64(1) || grant.Body["place"] != float64(2) {
		t.Fatalf("custom places: %s %s", region.Raw, grant.Raw)
	}
	bad := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "region"})
	e.problemIs(bad, "createDimension", 422, CodeInvalid)
	if f := fields(bad); f["body.kind"] != dimension.CodeKind || f["body.name"] != dimension.CodeName {
		t.Errorf("a second Department named like Region: %v", f)
	}
	if !strings.Contains(bad.Raw, "the organisation has its department dimension already, named Department") {
		t.Errorf("the words: %s", bad.Raw)
	}

	// Values: a code unique in its dimension, whatever the case; a dimension out of use takes none.
	deptID := dept.str("id")
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": deptID, "code": "ENG", "name": "Engineering"})
	if eng.Status != 201 || eng.str("dimension_id") != deptID || eng.str("code") != "ENG" || eng.Header.Get("ETag") != etag(1) {
		t.Fatalf("Engineering: %d %s", eng.Status, eng.Raw)
	}
	e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": deptID, "code": "OPS", "name": "Operations"})
	e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": region.str("id"), "code": "APAC", "name": "Asia Pacific"})
	dup := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": deptID, "code": "eng", "name": "R&D"})
	e.problemIs(dup, "createDimensionValue", 422, CodeInvalid)
	if f := fields(dup); f["body.code"] != dimension.CodeCode {
		t.Errorf("ENG again: %v", f)
	}
	nobody := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": uuid.NewString(), "code": "X Y", "name": ""})
	if f := fields(nobody); f["body.dimension_id"] != dimension.CodeDimension || f["body.code"] != dimension.CodeCode || f["body.name"] != dimension.CodeName {
		t.Errorf("every problem at once: %v", f)
	}
	all := e.call(http.MethodGet, "/v1/dimension-values?page_size=2", e.as(authz.Auditor), nil)
	if items := all.Body["items"].([]any); len(items) != 2 || items[0].(map[string]any)["code"] != "ENG" || all.str("next_cursor") == "" {
		t.Fatalf("the first page of every value: %s", all.Raw)
	}
	rest := e.call(http.MethodGet, "/v1/dimension-values?page_size=2&cursor="+all.str("next_cursor"), e.as(authz.Auditor), nil)
	if items := rest.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["code"] != "APAC" || rest.Body["next_cursor"] != nil {
		t.Errorf("the second page: %s", rest.Raw)
	}
	if one := e.call(http.MethodGet, "/v1/dimension-values?dimension_id="+region.str("id"), accountant, nil); len(one.Body["items"].([]any)) != 1 {
		t.Errorf("the region's values: %s", one.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/dimension-values?dimension_id=nope", accountant, nil), "listDimensionValues", 422, CodeInvalid)

	// Renamed and put out of use at the version read; a stale ETag or none is refused.
	path := "/v1/dimensions/" + region.str("id")
	e.problemIs(e.call(http.MethodPut, path, accountant, map[string]any{"name": "Market"}), "replaceDimension", 428, CodeIfMatchRequired)
	off := e.call(http.MethodPut, path, accountant, map[string]any{"name": "Market", "active": false}, "If-Match", region.Header.Get("ETag"))
	if off.Status != 200 || off.str("name") != "Market" || off.Body["active"] != false || off.Body["version"] != float64(2) || off.Body["place"] != float64(1) {
		t.Fatalf("Region renamed Market, out of use: %d %s", off.Status, off.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, accountant, map[string]any{"name": "Region"}, "If-Match", region.Header.Get("ETag")), "replaceDimension", 412, CodeStale)
	e.problemIs(e.call(http.MethodPut, path, accountant, map[string]any{"name": "grant"}, "If-Match", off.Header.Get("ETag")), "replaceDimension", 422, CodeInvalid)
	if f := fields(e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": region.str("id"), "code": "EU", "name": "Europe"})); f["body.dimension_id"] != dimension.CodeDimension {
		t.Errorf("a value of a dimension out of use: %v", f)
	}
	vpath := "/v1/dimension-values/" + eng.str("id")
	v := e.call(http.MethodPut, vpath, accountant, map[string]any{"name": "Engineering & product"}, "If-Match", eng.Header.Get("ETag"))
	if v.Status != 200 || v.str("name") != "Engineering & product" || v.str("code") != "ENG" || v.Body["active"] != true || v.Body["version"] != float64(2) {
		t.Fatalf("Engineering renamed: %d %s", v.Status, v.Raw)
	}
	e.problemIs(e.call(http.MethodPut, vpath, accountant, map[string]any{"name": "Operations"}, "If-Match", v.Header.Get("ETag")), "replaceDimensionValue", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPut, "/v1/dimension-values/"+uuid.NewString(), accountant, map[string]any{"name": "X"}, "If-Match", etag(1)),
		"replaceDimensionValue", 404, CodeNotFound)

	// Up to 8 custom dimensions, places kept by those out of use.
	for i := 3; i <= dimension.MaxCustom; i++ {
		if r := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Custom " + string(rune('A'+i))}); r.Body["place"] != float64(i) {
			t.Fatalf("custom %d: %s", i, r.Raw)
		}
	}
	if f := fields(e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Ninth"})); f["body.kind"] != dimension.CodeKind {
		t.Errorf("a ninth custom dimension: %v", f)
	}
	if l := e.call(http.MethodGet, "/v1/dimensions", accountant, nil); len(l.Body["items"].([]any)) != 9 || l.Body["items"].([]any)[0].(map[string]any)["kind"] != "department" {
		t.Errorf("the dimensions: %s", l.Raw)
	}
}

// matrixDimension returns the ID of e's Department, adding it first if needed.
func matrixDimension(e *env) string {
	l := e.call(http.MethodGet, "/v1/dimensions", e.as(authz.Accountant), nil)
	for _, x := range l.Body["items"].([]any) {
		if d := x.(map[string]any); d["kind"] == "department" {
			return d["id"].(string)
		}
	}
	return e.call(http.MethodPost, "/v1/dimensions", e.as(authz.Accountant), map[string]any{"kind": "department", "name": "Department"}).str("id")
}

// matrixDimensionValue returns the ID of a value of e's Department, adding one first if needed.
func matrixDimensionValue(e *env) string {
	dept := matrixDimension(e)
	l := e.call(http.MethodGet, "/v1/dimension-values?dimension_id="+dept, e.as(authz.Accountant), nil)
	if items := l.Body["items"].([]any); len(items) > 0 {
		return items[0].(map[string]any)["id"].(string)
	}
	return e.call(http.MethodPost, "/v1/dimension-values", e.as(authz.Accountant), map[string]any{"dimension_id": dept, "code": "MATRIX", "name": "Matrix"}).str("id")
}

// dimensionNamed is the name of e's dimension or value with ID id, as an Accountant lists them.
func dimensionNamed(e *env, list, id string) string {
	l := e.call(http.MethodGet, list+"?page_size=500", e.as(authz.Accountant), nil)
	for _, x := range l.Body["items"].([]any) {
		if d := x.(map[string]any); d["id"] == id {
			return d["name"].(string)
		}
	}
	return ""
}

// matrixDimensions are the authorisation matrix's rows for the dimensions and their values.
var matrixDimensions = map[string]func(e *env, tok string) (response, func() bool){
	"listDimensions": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/dimensions", tok, nil), nil
	},
	"listDimensionValues": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/dimension-values", tok, nil), nil
	},
	// Each call adds a Department to an organisation of its own: an organisation has one, and 8 custom dimensions at most.
	"createDimension": func(e *env, tok string) (response, func() bool) {
		org := porttest.NewBooks(e.t, e.store, "Dimensions "+uuid.NewString()[:8]+" Pte Ltd").Org
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = org
			tok = e.principal(p)
		}
		r := e.call(http.MethodPost, "/v1/dimensions", tok, map[string]any{"kind": "department", "name": "Department"})
		return r, func() bool {
			n := 0
			_ = e.store.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
				ds, err := u.Dimensions().List(ctx)
				n = len(ds)
				return err
			})
			return n == 0
		}
	},
	"replaceDimension": func(e *env, tok string) (response, func() bool) {
		id := matrixDimension(e)
		name := "Department " + uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/dimensions/"+id, tok, map[string]any{"name": name}, "If-Match", "*")
		return r, func() bool { return dimensionNamed(e, "/v1/dimensions", id) != name }
	},
	"createDimensionValue": func(e *env, tok string) (response, func() bool) {
		code := "M" + strings.ToUpper(uuid.NewString()[:8])
		r := e.call(http.MethodPost, "/v1/dimension-values", tok, map[string]any{"dimension_id": matrixDimension(e), "code": code, "name": code})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/dimension-values?page_size=500", e.as(authz.Accountant), nil).Raw, `"code":"`+code+`"`)
		}
	},
	"replaceDimensionValue": func(e *env, tok string) (response, func() bool) {
		id := matrixDimensionValue(e)
		name := "Value " + uuid.NewString()[:8]
		r := e.call(http.MethodPut, "/v1/dimension-values/"+id, tok, map[string]any{"name": name}, "If-Match", "*")
		return r, func() bool { return dimensionNamed(e, "/v1/dimension-values", id) != name }
	},
}

// FR-DIM-01 and FR-DIM-02 (rev 0.171; slice 3): an account requires the dimensions the chart's keepers name, each one of
// the organisation's and in use; a journal line takes dimension values, which the posted entry shows; a journal whose
// line on an account that requires Department has none cannot post (DIM-05).
// req: FR-DIM-01
// req: FR-DIM-02
// req: ENG-22
func TestJournalLinesTakeDimensionValues(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"}).str("id")
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept, "code": "ENG", "name": "Engineering"}).str("id")
	rent := "/v1/accounts/" + e.books.Rent.ID.String()

	bad := e.call(http.MethodPatch, rent, accountant, map[string]any{"required_dimension_ids": []string{uuid.NewString()}}, "If-Match", "*")
	e.problemIs(bad, "updateAccount", 422, CodeInvalid)
	if f := fields(bad); f["body.required_dimension_ids[0]"] != dimension.CodeDimension {
		t.Errorf("a dimension the organisation does not have: %v", f)
	}
	r := e.call(http.MethodPatch, rent, accountant, map[string]any{"required_dimension_ids": []string{dept}}, "If-Match", "*")
	if ids, _ := r.Body["required_dimension_ids"].([]any); r.Status != 200 || len(ids) != 1 || ids[0] != dept {
		t.Fatalf("rent requires Department: %d %s", r.Status, r.Raw)
	}

	id, tag := e.createDraft("100.00")
	refused := e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag)
	e.problemIs(refused, "postJournal", 422, ledger.CodeDimensionRequired)
	if !strings.Contains(refused.Raw, "needs a value of Department on every line") {
		t.Errorf("DIM-05's words: %s", refused.Raw)
	}

	d := e.draft("100.00")
	d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{"nope"}
	e.problemIs(e.call(http.MethodPost, "/v1/journals", accountant, d), "createJournal", 422, CodeInvalid)
	d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{eng}
	j := e.call(http.MethodPost, "/v1/journals", accountant, d)
	if got := j.Body["lines"].([]any)[0].(map[string]any)["dimension_value_ids"]; fmt.Sprint(got) != "["+eng+"]" {
		t.Errorf("the saved line's values: %v (%s)", got, j.Raw)
	}
	posted := e.call(http.MethodPost, "/v1/journals/"+j.str("id")+"/post", accountant, nil, "If-Match", j.Header.Get("ETag"))
	if posted.Status != 200 {
		t.Fatalf("posting with Engineering: %d %s", posted.Status, posted.Raw)
	}
	if !strings.Contains(posted.Raw, `"dimension_value_ids":["`+eng+`"]`) {
		t.Errorf("the posted entry's line shows its value: %s", posted.Raw)
	}
}

// FR-DIM-01 and FR-DIM-02 on invoices through the API (rev 0.171; slice 3d): an invoice's line takes dimension_value_ids,
// each an ID, and shows them; with Sales requiring Department, an invoice without one does not issue (DIM-05) and
// one with Engineering does, its entry's revenue line showing the value.
// req: FR-DIM-01
// req: FR-DIM-02
func TestInvoiceLinesTakeDimensionValues(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"}).str("id")
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept, "code": "ENG", "name": "Engineering"}).str("id")
	r := e.call(http.MethodPatch, "/v1/accounts/"+e.books.Sales.ID.String(), accountant, map[string]any{"required_dimension_ids": []string{dept}}, "If-Match", "*")
	if r.Status != 200 {
		t.Fatalf("sales requires Department: %d %s", r.Status, r.Raw)
	}

	id, tag := e.createInvoice("1", "100.00")
	refused := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", accountant, nil, "If-Match", tag)
	e.problemIs(refused, "issueInvoice", 422, ledger.CodeDimensionRequired)

	d := e.invoiceDraft("1", "100.00")
	d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{"nope"}
	e.problemIs(e.call(http.MethodPost, "/v1/invoices", accountant, d), "createInvoice", 422, CodeInvalid)
	d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{eng}
	inv := e.call(http.MethodPost, "/v1/invoices", accountant, d)
	if got := inv.Body["lines"].([]any)[0].(map[string]any)["dimension_value_ids"]; fmt.Sprint(got) != "["+eng+"]" {
		t.Errorf("the saved line's values: %v (%s)", got, inv.Raw)
	}
	issued := e.call(http.MethodPost, "/v1/invoices/"+inv.str("id")+"/issue", accountant, nil, "If-Match", inv.Header.Get("ETag"))
	if issued.Status != 200 || !strings.Contains(issued.Raw, `"dimension_value_ids":["`+eng+`"]`) {
		t.Errorf("issuing with Engineering: %d %s", issued.Status, issued.Raw)
	}
}

// FR-DIM-01 and FR-DIM-02 on bills through the API (rev 0.171; slice 3d): a bill's line takes dimension_value_ids and
// shows them; with Rent requiring Department, a bill without one does not post (DIM-05) and one with Engineering does,
// its cost line showing the value.
// req: FR-DIM-01
// req: FR-DIM-02
func TestBillLinesTakeDimensionValues(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"}).str("id")
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept, "code": "ENG", "name": "Engineering"}).str("id")
	if r := e.call(http.MethodPatch, "/v1/accounts/"+e.books.Rent.ID.String(), accountant, map[string]any{"required_dimension_ids": []string{dept}}, "If-Match", "*"); r.Status != 200 {
		t.Fatalf("rent requires Department: %d %s", r.Status, r.Raw)
	}
	id, tag := e.createBill("100.00", "0.00")
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+id+"/post", accountant, nil, "If-Match", tag), "postBill", 422, ledger.CodeDimensionRequired)

	d := e.billDraft("100.00", "0.00")
	d["supplier_ref"] = "AU-DIM"
	d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{"nope"}
	e.problemIs(e.call(http.MethodPost, "/v1/bills", accountant, d), "createBill", 422, CodeInvalid)
	d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{eng}
	b := e.call(http.MethodPost, "/v1/bills", accountant, d)
	if got := b.Body["lines"].([]any)[0].(map[string]any)["dimension_value_ids"]; fmt.Sprint(got) != "["+eng+"]" {
		t.Errorf("the saved line's values: %v (%s)", got, b.Raw)
	}
	posted := e.call(http.MethodPost, "/v1/bills/"+b.str("id")+"/post", accountant, nil, "If-Match", b.Header.Get("ETag"))
	if posted.Status != 200 || !strings.Contains(posted.Raw, `"dimension_value_ids":["`+eng+`"]`) {
		t.Errorf("posting with Engineering: %d %s", posted.Status, posted.Raw)
	}
}
