//go:build local

package api

import (
	"encoding/base64"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/corporate"
)

// The corporate documents (FR-CS-05; p1-company-secretary task 4.1, design D6). With the company, two directors and a
// secretary recorded, Wei Ling drafts the directors' resolution approving the statements for 2026 as the Accountant:
// filled from the records, with the company's name, UEN and office at its head, the resolution in words and a line to
// sign for each director, under the banner that it is Fyniq's draft, never signed or filed by Fyniq. Fields the kind
// does not ask for, and a missing one, are refused together, each named; a members' resolution with no member holding
// shares names who would sign it. Drafted again, it is filled from that day's records. Its PDF is archival and the same
// for the same version. Ah Kow drafts one as the Owner (he approves sec.draft_document); the Board viewer reads them and
// drafts none; the DPO sees what each is, not its text, and its PDF's name with the content masked. The profile lists them
// without their text.
// req: FR-CS-05
// req: governance/company-secretary FR-CS-05 Documents drafted
// req: access/roles-and-segregation Permission matrix: Corporate profile and registers
func TestTheCorporateDocuments(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	e.clock.Set(time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC)) // before the sessions, so their step-up is fresh
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner)
	record := func(path string, body map[string]any) {
		t.Helper()
		d := e.call(http.MethodPost, path, prep, body)
		at := path + "/" + d.str("id")
		e.upload(at+"/evidence", prep, map[string]string{"evidence_type": "BizFile+"}, "ack.pdf", []byte("%PDF-1.7 ack"), "If-Match", d.Header.Get("ETag"))
		cur := e.call(http.MethodGet, at, prep, nil)
		req := e.call(http.MethodPost, at+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
		if r := e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", kow, hashOf(req), "If-Match", `"1"`); r.Status != http.StatusOK {
			t.Fatalf("recording %s: %d %s", path, r.Status, r.Raw)
		}
	}
	record("/v1/corporate-profile/details", map[string]any{"from": "2026-03-02", "name": "Cognizhi Pte. Ltd.", "registered_office": "1 North Bridge Road",
		"ssic": "62011"})
	for _, o := range []map[string]any{{"role": "director", "name": "Tan Mei Lin"}, {"role": "director", "name": "Lim Hui"}, {"role": "secretary", "name": "Rachel Teo"}} {
		o["kind"], o["appointed_on"], o["nominee"] = "individual", "2026-03-02", false
		record("/v1/corporate-profile/officers", o)
	}

	templates := e.call(http.MethodGet, "/v1/corporate-profile/templates", prep, nil)
	list, _ := templates.Body["templates"].([]any)
	if templates.Status != http.StatusOK || len(list) != len(corporate.Kinds) || !strings.Contains(templates.Raw, `"kind":"directors.approve_statements","version":1`) ||
		!strings.Contains(templates.Raw, `{"name":"fy_end","type":"date","label":"The financial year's last day","optional":false}`) {
		t.Fatalf("the templates: %d %s", templates.Status, templates.Raw)
	}

	bad := e.call(http.MethodPost, "/v1/corporate-profile/documents", prep, map[string]any{"kind": "directors.approve_statements", "fields": map[string]any{"place": "Here"}})
	if bad.Status != http.StatusUnprocessableEntity || !strings.Contains(bad.Raw, `"field":"body.fields.place"`) || !strings.Contains(bad.Raw, `"field":"body.fields.fy_end"`) ||
		!strings.Contains(bad.Raw, corporate.CodeDocument) {
		t.Errorf("fields the kind does not ask for, and one missing: %d %s", bad.Status, bad.Raw)
	}
	members := e.call(http.MethodPost, "/v1/corporate-profile/documents", prep, map[string]any{"kind": "members.written_resolution",
		"fields": map[string]any{"resolution_kind": "special", "resolutions": []string{"That the name be changed."}, "circulated_on": "2027-01-05"}})
	if members.Status != http.StatusUnprocessableEntity || !strings.Contains(members.Raw, "no member holds shares on 5 January 2027") {
		t.Errorf("a members' resolution with no member: %d %s", members.Status, members.Raw)
	}

	draft := e.call(http.MethodPost, "/v1/corporate-profile/documents", prep, map[string]any{"kind": "directors.approve_statements",
		"fields": map[string]any{"fy_end": "2026-12-31"}, "note": "For the AGM"})
	if draft.Status != http.StatusCreated || draft.Body["status"] != "draft" || draft.Body["template"] != "directors.approve_statements@1" ||
		draft.Body["drafted_on"] != "2027-01-05" || draft.Header.Get("ETag") != `"1-draft"` {
		t.Fatalf("the resolution drafted: %d %s", draft.Status, draft.Raw)
	}
	for _, want := range []string{`"company":"Cognizhi Pte. Ltd."`, `"Registered office: 1 North Bridge Road"`,
		"That the financial statements of the company for the financial year ended 31 December 2026 be approved.",
		`{"name":"Lim Hui","role":"Director"},{"name":"Tan Mei Lin","role":"Director"}`, "Fyniq does not sign or file it", `"footer":"directors.approve_statements@1"`} {
		if !strings.Contains(draft.Raw, want) {
			t.Errorf("the resolution lacks %s: %s", want, draft.Raw)
		}
	}
	path := "/v1/corporate-profile/documents/" + draft.str("id")

	e.clock.Set(time.Date(2027, 1, 8, 9, 0, 0, 0, time.UTC))
	prep, kow = c.as(c.weiling, authz.Accountant), c.as(c.ahkow, authz.Owner)
	again := e.call(http.MethodPut, path, prep, map[string]any{"fields": map[string]any{"fy_end": "2026-12-31"}}, "If-Match", draft.Header.Get("ETag"))
	if again.Status != http.StatusOK || again.Body["version"] != float64(2) || again.Body["drafted_on"] != "2027-01-08" || again.Body["note"] != nil {
		t.Errorf("drafted again: %d %s", again.Status, again.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, prep, map[string]any{"fields": map[string]any{"fy_end": "2026-12-31"}}, "If-Match", draft.Header.Get("ETag")),
		"replaceCorporateDocument", http.StatusPreconditionFailed, CodeStale)

	pdf := e.call(http.MethodGet, path+"/pdf", prep, nil)
	raw, _ := base64.StdEncoding.DecodeString(pdf.str("content_base64"))
	if pdf.Status != http.StatusOK || !strings.HasPrefix(string(raw), "%PDF-") || pdf.Body["file_name"] != "directors.approve_statements-2027-01-08.pdf" ||
		pdf.Body["media_type"] != "application/pdf" || len(pdf.str("sha256")) != 64 {
		t.Fatalf("the PDF: %d %.300s", pdf.Status, pdf.Raw)
	}
	if same := e.call(http.MethodGet, path+"/pdf", prep, nil); same.str("sha256") != pdf.str("sha256") {
		t.Errorf("the same version's PDF differs: %s, then %s", pdf.str("sha256"), same.str("sha256"))
	}

	owner := e.call(http.MethodPost, "/v1/corporate-profile/documents", kow, map[string]any{"kind": "agm.notice", "fields": map[string]any{"meeting_on": "2027-06-15",
		"time": "10:30", "place": "1 North Bridge Road", "fy_end": "2026-12-31", "notice_on": "2027-05-28"}})
	if owner.Status != http.StatusCreated || !strings.Contains(owner.Raw, `"signatures":[{"name":"Rachel Teo","role":"Secretary"}]`) {
		t.Errorf("the Owner drafts the AGM's notice: %d %s", owner.Status, owner.Raw)
	}
	board := c.as(c.mei, authz.BoardViewer)
	if r := e.call(http.MethodPost, "/v1/corporate-profile/documents", board, map[string]any{"kind": "directors.approve_statements",
		"fields": map[string]any{"fy_end": "2026-12-31"}}); r.Status != http.StatusForbidden {
		t.Errorf("the Board viewer drafting: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, path, board, nil); r.Status != http.StatusOK || r.Body["paper"] == nil {
		t.Errorf("the Board viewer reading: %d %s", r.Status, r.Raw)
	}
	dpo := c.as(c.mei, authz.DPO)
	if r := e.call(http.MethodGet, path, dpo, nil); r.Status != http.StatusOK || r.Body["masked"] != true || r.Body["paper"] != nil || strings.Contains(r.Raw, "2026-12-31") {
		t.Errorf("the DPO reading: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, path+"/pdf", dpo, nil); r.Status != http.StatusOK || r.Body["content_base64"] != MaskedValue || r.Body["sha256"] != MaskedValue ||
		r.Body["file_name"] != "directors.approve_statements-2027-01-08.pdf" {
		t.Errorf("the DPO's PDF: %d %.300s", r.Status, r.Raw)
	}

	profile := e.call(http.MethodGet, "/v1/corporate-profile", prep, nil)
	docs, _ := profile.Body["documents"].([]any)
	if len(docs) != 2 || docs[1].(map[string]any)["id"] != draft.str("id") || docs[0].(map[string]any)["paper"] != nil {
		t.Errorf("the profile's documents, the latest drafted first, without their text: %s", profile.Raw)
	}
}

// Every field a template asks for is a field of CorporateDocumentFields, with the type it takes, and the draft's kinds
// are the templates'; so the API and the templates stay in step.
// req: FR-CS-05
func TestTheDocumentFieldsAreTheTemplates(t *testing.T) {
	fields := map[string]reflect.Kind{}
	ty := reflect.TypeFor[CorporateDocumentFields]()
	for i := range ty.NumField() {
		name, _, _ := strings.Cut(ty.Field(i).Tag.Get("json"), ",")
		fields[name] = ty.Field(i).Type.Kind()
	}
	used := map[string]bool{}
	for _, k := range corporate.Kinds {
		for _, f := range corporate.Templates[k].Fields {
			kind, ok := fields[f.Name]
			if want := map[bool]reflect.Kind{true: reflect.Slice, false: reflect.String}[f.Type == corporate.FieldLines]; !ok || kind != want {
				t.Errorf("%s asks for %s, a %s: the API's field is %v", k, f.Name, f.Type, kind)
			}
			used[f.Name] = true
		}
	}
	for name := range fields {
		if !used[name] {
			t.Errorf("the API's field %s is no template's", name)
		}
	}
	enum, _ := reflect.TypeFor[CorporateDocumentDraft]().FieldByName("Kind")
	if kinds := strings.Split(enum.Tag.Get("enum"), ","); !slices.Equal(kinds, corporate.Kinds) {
		t.Errorf("the draft's kinds %v are not the templates' %v", kinds, corporate.Kinds)
	}
}

var matrixDocumentOf = map[*env]string{}

// matrixDocument is a drafted AGM's minutes, the Accountant's: they need no record to draft.
func matrixDocument(e *env) string {
	e.t.Helper()
	if id, ok := matrixDocumentOf[e]; ok {
		return id
	}
	r := e.call(http.MethodPost, "/v1/corporate-profile/documents", e.as(authz.Accountant), matrixMinutes)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's corporate document: %d %s", r.Status, r.Raw)
	}
	matrixDocumentOf[e] = r.str("id")
	return r.str("id")
}

var matrixMinutes = map[string]any{"kind": "agm.minutes", "fields": map[string]any{"meeting_on": "2026-06-15", "time": "10:00", "place": "1 Matrix Road",
	"chair": "Matrix Director", "present": []string{"Matrix Director"}, "fy_end": "2025-12-31", "resolutions": []string{"That the statements be adopted."}}}

// matrixDocumentState is what a refused call must leave as it was: the document and how many there are.
func matrixDocumentState(e *env, id string) string {
	acc := e.as(authz.Accountant)
	d := e.call(http.MethodGet, "/v1/corporate-profile/documents/"+id, acc, nil)
	p := e.call(http.MethodGet, "/v1/corporate-profile", acc, nil)
	docs, _ := p.Body["documents"].([]any)
	return d.Header.Get("ETag") + d.Raw + "|" + strings.Repeat("d", len(docs))
}

// matrixCorporateDocuments are the authorisation matrix's rows for the corporate documents (FR-CS-05).
var matrixCorporateDocuments = map[string]func(e *env, tok string) (response, func() bool){
	"getCorporateTemplates": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/templates", tok, nil), nil
	},
	"createCorporateDocument": func(e *env, tok string) (response, func() bool) {
		id := matrixDocument(e)
		was := matrixDocumentState(e, id)
		r := e.call(http.MethodPost, "/v1/corporate-profile/documents", tok, matrixMinutes, "Prefer", "return=preview")
		return r, func() bool { return matrixDocumentState(e, id) == was }
	},
	"getCorporateDocument": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/documents/"+matrixDocument(e), tok, nil), nil
	},
	"replaceCorporateDocument": func(e *env, tok string) (response, func() bool) {
		id := matrixDocument(e)
		was := matrixDocumentState(e, id)
		r := e.call(http.MethodPut, "/v1/corporate-profile/documents/"+id, tok, map[string]any{"fields": matrixMinutes["fields"]}, "If-Match", "*",
			"Prefer", "return=preview")
		return r, func() bool { return matrixDocumentState(e, id) == was }
	},
	"getCorporateDocumentPDF": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/documents/"+matrixDocument(e)+"/pdf", tok, nil), nil
	},
}
