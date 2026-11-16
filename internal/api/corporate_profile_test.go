//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/corporate"
	"github.com/cognizhi/fyniq/internal/domain/equity"
)

// The corporate profile (FR-CS-01, FR-CS-06; p1-company-secretary task 2.1, design D2, rev 0.282). Wei Ling drafts the
// company's details from 2 Mar 2026 as the Accountant and asks, with the business profile attached; Ah Kow approves and
// they are in force. Tan Mei Lin, appointed a director on 2 Mar and lodged on 9 Mar, is recorded the same way; her
// cessation on 30 Sep is a later version that supersedes the first, so the profile on 1 Jun shows her and on 10 Oct does
// not. Ah Kow, drafted as the secretary and signing in as himself, may not approve his own appointment (FM-1). A company
// as a director, a full NRIC, a five-figure-short SSIC code and a day after today are refused with every field named; a
// later version must follow an officer with a version recorded. Mei, an Approver, sees nothing of it; the DPO reads it
// masked.
// req: FR-CS-01
// req: FR-CS-06
// req: access/roles-and-segregation Permission matrix: Corporate profile and registers
// asvs: V8.2.1, V8.2.2
func TestTheCorporateProfile(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner)
	mei := c.as(c.mei, authz.Approver)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	// record drafts, attaches evidence, asks and has Ah Kow approve: the record in force.
	record := func(path string, draft response) {
		t.Helper()
		if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": "BizFile+ acknowledgement"}, "ack.pdf", []byte("%PDF-1.7 ack"),
			"If-Match", draft.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("the evidence of %s: %d %s", path, f.Status, f.Raw)
		}
		cur := e.call(http.MethodGet, path, prep, nil)
		req := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
		if req.Status != http.StatusAccepted {
			t.Fatalf("asking for %s: %d %s", path, req.Status, req.Raw)
		}
		if r := approve(req, kow); r.Status != http.StatusOK || r.Body["status"] != "approved" {
			t.Fatalf("approving %s: %d %s", path, r.Status, r.Raw)
		}
	}

	bad := map[string]any{"from": "2099-01-01", "name": " ", "registered_office": "1 North Bridge Road", "ssic": "6201"}
	if r := e.call(http.MethodPost, "/v1/corporate-profile/details", prep, bad); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, corporate.CodeDetail) || !strings.Contains(r.Raw, `"field":"body.from"`) || !strings.Contains(r.Raw, `"field":"body.name"`) ||
		!strings.Contains(r.Raw, `"field":"body.ssic"`) {
		t.Errorf("details that do not fit: %d %s", r.Status, r.Raw)
	}
	detail := e.call(http.MethodPost, "/v1/corporate-profile/details", prep, map[string]any{"from": "2026-03-02", "name": "Cognizhi Pte. Ltd.",
		"registered_office": "1 North Bridge Road, #08-08, Singapore 179094", "ssic": "62011", "lodged_on": "2026-03-02"})
	if detail.Status != http.StatusCreated || detail.Body["status"] != "draft" || !strings.Contains(detail.Raw, equity.CodeNoEvidence) {
		t.Fatalf("the details drafted: %d %s", detail.Status, detail.Raw)
	}
	dpath := "/v1/corporate-profile/details/" + detail.str("id")
	e.problemIs(e.call(http.MethodPost, dpath+"/approval", prep, nil, "If-Match", detail.Header.Get("ETag")), "requestCorporateDetailApproval", 422,
		equity.CodeNoEvidence)
	record(dpath, detail)
	if r := e.call(http.MethodGet, dpath, prep, nil); r.Body["status"] != "recorded" || r.Header.Get("ETag") != `"1-posted"` {
		t.Errorf("the details recorded: %s %s", r.Header.Get("ETag"), r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, dpath, prep, map[string]any{"from": "2026-03-02", "name": "Cognizhi Pte. Ltd.", "registered_office": "Elsewhere",
		"ssic": "62011"}, "If-Match", `"1-posted"`), "replaceCorporateDetail", 409, equity.CodeRecorded)

	officer := map[string]any{"role": "director", "name": "Tan Mei Lin", "kind": "individual", "id_type": "nric", "id_last4": "567A",
		"appointed_on": "2026-03-02", "appointment_lodged_on": "2026-03-09", "nominee": false}
	wrong := map[string]any{"role": "director", "name": "Lion Rock Ventures Pte. Ltd.", "kind": "entity", "appointed_on": "2026-03-02",
		"ceased_on": "2026-03-01", "nominee": false, "nominator": "Lion Rock", "note": "NRIC S1234567D"}
	if r := e.call(http.MethodPost, "/v1/corporate-profile/officers", prep, wrong); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, corporate.CodeOfficer) || !strings.Contains(r.Raw, `"field":"body.kind"`) || !strings.Contains(r.Raw, `"field":"body.ceased_on"`) ||
		!strings.Contains(r.Raw, `"field":"body.nominator"`) || !strings.Contains(r.Raw, `"field":"body.note"`) {
		t.Errorf("an officer who does not fit: %d %s", r.Status, r.Raw)
	}
	tan := e.call(http.MethodPost, "/v1/corporate-profile/officers", prep, officer)
	if tan.Status != http.StatusCreated || tan.Body["officer_id"] != tan.Body["id"] {
		t.Fatalf("Tan Mei Lin drafted: %d %s", tan.Status, tan.Raw)
	}
	tpath := "/v1/corporate-profile/officers/" + tan.str("id")
	follows := map[string]any{"officer_id": tan.str("id")}
	for k, v := range officer {
		follows[k] = v
	}
	follows["ceased_on"], follows["cessation_lodged_on"] = "2026-09-30", "2026-10-05"
	if r := e.call(http.MethodPost, "/v1/corporate-profile/officers", prep, follows); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, `"code":"CORP-02","field":"body.officer_id"`) {
		t.Errorf("a later version before her first is recorded: %d %s", r.Status, r.Raw)
	}
	record(tpath, tan)
	ceased := e.call(http.MethodPost, "/v1/corporate-profile/officers", prep, follows)
	if ceased.Status != http.StatusCreated || ceased.Body["officer_id"] != tan.str("id") {
		t.Fatalf("her cessation drafted: %d %s", ceased.Status, ceased.Raw)
	}
	record("/v1/corporate-profile/officers/"+ceased.str("id"), ceased)

	secretary := map[string]any{"role": "secretary", "name": "Ah Kow", "member_user_id": c.ahkow, "kind": "individual", "appointed_on": "2026-03-02",
		"nominee": false}
	ahkow := e.call(http.MethodPost, "/v1/corporate-profile/officers", prep, secretary)
	if ahkow.Status != http.StatusCreated {
		t.Fatalf("Ah Kow drafted as the secretary: %d %s", ahkow.Status, ahkow.Raw)
	}
	apath := "/v1/corporate-profile/officers/" + ahkow.str("id")
	if f := e.upload(apath+"/evidence", prep, map[string]string{"evidence_type": "Consent to act"}, "consent.pdf", []byte("%PDF-1.7 consent"),
		"If-Match", ahkow.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("his consent: %d %s", f.Status, f.Raw)
	}
	cur := e.call(http.MethodGet, apath, prep, nil)
	req := e.call(http.MethodPost, apath+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || !strings.Contains(req.Raw, "Record Ah Kow's appointment as secretary") {
		t.Fatalf("asking for Ah Kow: %d %s", req.Status, req.Raw)
	}
	if r := approve(req, kow); r.Status != http.StatusForbidden || r.problemCode() != approval.CodeNotApprover || !strings.Contains(r.Raw, "your own appointment") {
		t.Errorf("Ah Kow approving his own appointment: %d %s", r.Status, r.Raw)
	}

	profile := func(token, on string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/corporate-profile?on="+on, token, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("the profile on %s: %d %s", on, r.Status, r.Raw)
		}
		return r
	}
	june := profile(prep, "2026-06-01")
	officers := june.Body["officers"].([]any)
	if d, _ := june.Body["detail"].(map[string]any); d == nil || d["status"] != "recorded" || len(officers) != 1 ||
		officers[0].(map[string]any)["officer"].(map[string]any)["name"] != "Tan Mei Lin" || june.Body["secretary"] != false {
		t.Errorf("the profile on 1 Jun: %s", june.Raw)
	}
	october := profile(prep, "2026-10-10")
	if len(october.Body["officers"].([]any)) != 0 || len(october.Body["officer_history"].([]any)) != 3 {
		t.Errorf("the profile on 10 Oct: %s", october.Raw)
	}
	for _, x := range october.Body["officer_history"].([]any) {
		v := x.(map[string]any)
		want := map[string]string{tan.str("id"): "superseded", ceased.str("id"): "recorded", ahkow.str("id"): "draft"}[v["id"].(string)]
		if v["status"] != want {
			t.Errorf("version %s is %s, want %s", v["id"], v["status"], want)
		}
	}
	if before := profile(prep, "2026-03-01"); before.Body["detail"] != nil || len(before.Body["officers"].([]any)) != 0 {
		t.Errorf("the profile before incorporation: %s", before.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/corporate-profile", mei, nil); r.Status != http.StatusForbidden {
		t.Errorf("an Approver reading the profile: %d %s", r.Status, r.Raw)
	}
	dpo := profile(c.as(c.mei, authz.DPO), "2026-06-01")
	o := dpo.Body["officers"].([]any)[0].(map[string]any)
	if o["masked"] != true || o["officer"].(map[string]any)["id_last4"] != nil || o["officer"].(map[string]any)["name"] != "Tan Mei Lin" {
		t.Errorf("the DPO's profile: %s", dpo.Raw)
	}
}

type corporateFixture struct{ detail, detailFile, officer, officerFile string }

var matrixCorporateOf = map[*env]corporateFixture{}

// matrixCorporateFixture is a drafted detail and officer, each with a file, the Accountant's.
func matrixCorporateFixture(e *env) corporateFixture {
	e.t.Helper()
	if f, ok := matrixCorporateOf[e]; ok {
		return f
	}
	acc := e.as(authz.Accountant)
	d := e.call(http.MethodPost, "/v1/corporate-profile/details", acc, map[string]any{"from": "2026-03-02", "name": "Matrix Pte. Ltd.",
		"registered_office": "1 Matrix Road, Singapore 000001", "ssic": "62011"})
	df := e.upload("/v1/corporate-profile/details/"+d.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Business profile"}, "profile.pdf",
		[]byte("%PDF-1.7 matrix profile"), "If-Match", d.Header.Get("ETag"))
	o := e.call(http.MethodPost, "/v1/corporate-profile/officers", acc, map[string]any{"role": "director", "name": "Matrix Director", "kind": "individual",
		"appointed_on": "2026-03-02", "nominee": false})
	of := e.upload("/v1/corporate-profile/officers/"+o.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Consent to act"}, "consent.pdf",
		[]byte("%PDF-1.7 matrix consent"), "If-Match", o.Header.Get("ETag"))
	if d.Status != http.StatusCreated || df.Status != http.StatusCreated || o.Status != http.StatusCreated || of.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's corporate profile: %d %d %d %d %s %s", d.Status, df.Status, o.Status, of.Status, d.Raw, o.Raw)
	}
	f := corporateFixture{detail: d.str("id"), detailFile: df.str("id"), officer: o.str("id"), officerFile: of.str("id")}
	matrixCorporateOf[e] = f
	return f
}

// matrixCorporateState is what a refused call must leave as it was: the drafts and their files.
func matrixCorporateState(e *env, f corporateFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/corporate-profile/details/" + f.detail, "/v1/corporate-profile/details/" + f.detail + "/evidence",
		"/v1/corporate-profile/officers/" + f.officer, "/v1/corporate-profile/officers/" + f.officer + "/evidence"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

var (
	detailKeys  = []string{"from", "name", "registered_office", "ssic"}
	officerKeys = []string{"role", "name", "kind", "appointed_on", "nominee"}
)

// matrixCorporate are the authorisation matrix's rows for the corporate profile (FR-CS-01, rev 0.282).
var matrixCorporate = map[string]func(e *env, tok string) (response, func() bool){
	"getCorporateProfile": func(e *env, tok string) (response, func() bool) {
		matrixCorporateFixture(e)
		return e.call(http.MethodGet, "/v1/corporate-profile", tok, nil), nil
	},
	"createCorporateDetail": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/details/"+f.detail, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/corporate-profile/details", tok, bodyOf(response{Body: cur.Body["detail"].(map[string]any)}, detailKeys...),
			"Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"getCorporateDetail": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/details/"+matrixCorporateFixture(e).detail, tok, nil), nil
	},
	"replaceCorporateDetail": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/details/"+f.detail, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/corporate-profile/details/"+f.detail, tok, bodyOf(response{Body: cur.Body["detail"].(map[string]any)}, detailKeys...),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"requestCorporateDetailApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodPost, "/v1/corporate-profile/details/"+f.detail+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"addCorporateDetailEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.upload("/v1/corporate-profile/details/"+f.detail+"/evidence", tok, map[string]string{"evidence_type": "Business profile"}, "bp.pdf",
			[]byte("%PDF-1.7 business profile"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"listCorporateDetailEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/details/"+matrixCorporateFixture(e).detail+"/evidence", tok, nil), nil
	},
	"getCorporateDetailEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		return e.call(http.MethodGet, "/v1/corporate-profile/details/"+f.detail+"/evidence/"+f.detailFile, tok, nil), nil
	},
	"removeCorporateDetailEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodDelete, "/v1/corporate-profile/details/"+f.detail+"/evidence/"+f.detailFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"createCorporateOfficer": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/officers/"+f.officer, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/corporate-profile/officers", tok, bodyOf(response{Body: cur.Body["officer"].(map[string]any)}, officerKeys...),
			"Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"getCorporateOfficer": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/officers/"+matrixCorporateFixture(e).officer, tok, nil), nil
	},
	"replaceCorporateOfficer": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/officers/"+f.officer, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/corporate-profile/officers/"+f.officer, tok, bodyOf(response{Body: cur.Body["officer"].(map[string]any)}, officerKeys...),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"requestCorporateOfficerApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodPost, "/v1/corporate-profile/officers/"+f.officer+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"addCorporateOfficerEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.upload("/v1/corporate-profile/officers/"+f.officer+"/evidence", tok, map[string]string{"evidence_type": "Consent to act"}, "c.pdf",
			[]byte("%PDF-1.7 consent"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"listCorporateOfficerEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/officers/"+matrixCorporateFixture(e).officer+"/evidence", tok, nil), nil
	},
	"getCorporateOfficerEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		return e.call(http.MethodGet, "/v1/corporate-profile/officers/"+f.officer+"/evidence/"+f.officerFile, tok, nil), nil
	},
	"removeCorporateOfficerEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodDelete, "/v1/corporate-profile/officers/"+f.officer+"/evidence/"+f.officerFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
}
