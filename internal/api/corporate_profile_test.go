//go:build local

package api

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

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

type corporateFixture struct{ detail, detailFile, officer, officerFile, nominee, nomineeFile, statements, statementsFile string }

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
	holder := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix Nominees Pte. Ltd.", "shareholder": true}).str("id")
	n := e.call(http.MethodPost, "/v1/corporate-profile/nominees", acc, map[string]any{"kind": "shareholder", "holder_id": holder,
		"nominator": "Matrix Fund", "since": "2026-06-18"})
	nf := e.upload("/v1/corporate-profile/nominees/"+n.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Notice"}, "notice.pdf",
		[]byte("%PDF-1.7 matrix notice"), "If-Match", n.Header.Get("ETag"))
	a := e.call(http.MethodPost, "/v1/corporate-profile/statements-approvals", acc, map[string]any{"fy_end": "2025-12-31", "approved_on": "2026-04-15"})
	af := e.upload("/v1/corporate-profile/statements-approvals/"+a.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Resolution"},
		"resolution.pdf", []byte("%PDF-1.7 matrix resolution"), "If-Match", a.Header.Get("ETag"))
	for _, r := range []response{d, df, o, of, n, nf, a, af} {
		if r.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's corporate profile: %d %s", r.Status, r.Raw)
		}
	}
	f := corporateFixture{detail: d.str("id"), detailFile: df.str("id"), officer: o.str("id"), officerFile: of.str("id"), nominee: n.str("id"),
		nomineeFile: nf.str("id"), statements: a.str("id"), statementsFile: af.str("id")}
	matrixCorporateOf[e] = f
	return f
}

// matrixCorporateState is what a refused call must leave as it was: the drafts and their files.
func matrixCorporateState(e *env, f corporateFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/corporate-profile/details/" + f.detail, "/v1/corporate-profile/details/" + f.detail + "/evidence",
		"/v1/corporate-profile/officers/" + f.officer, "/v1/corporate-profile/officers/" + f.officer + "/evidence",
		"/v1/corporate-profile/nominees/" + f.nominee, "/v1/corporate-profile/nominees/" + f.nominee + "/evidence",
		"/v1/corporate-profile/statements-approvals/" + f.statements, "/v1/corporate-profile/statements-approvals/" + f.statements + "/evidence"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

var (
	detailKeys  = []string{"from", "name", "registered_office", "ssic"}
	officerKeys = []string{"role", "name", "kind", "appointed_on", "nominee"}
	nomineeKeys = []string{"kind", "holder_id", "nominator", "since"}
	approveKeys = []string{"fy_end", "approved_on"}
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
	"createNomineeEntry": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/nominees/"+f.nominee, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/corporate-profile/nominees", tok, bodyOf(response{Body: cur.Body["entry"].(map[string]any)}, nomineeKeys...), "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"getNomineeEntry": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/nominees/"+matrixCorporateFixture(e).nominee, tok, nil), nil
	},
	"replaceNomineeEntry": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/nominees/"+f.nominee, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/corporate-profile/nominees/"+f.nominee, tok, bodyOf(response{Body: cur.Body["entry"].(map[string]any)}, nomineeKeys...),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"requestNomineeEntryApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodPost, "/v1/corporate-profile/nominees/"+f.nominee+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"addNomineeEntryEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.upload("/v1/corporate-profile/nominees/"+f.nominee+"/evidence", tok, map[string]string{"evidence_type": "Notice"}, "n.pdf", []byte("%PDF-1.7 notice"),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"listNomineeEntryEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/nominees/"+matrixCorporateFixture(e).nominee+"/evidence", tok, nil), nil
	},
	"getNomineeEntryEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		return e.call(http.MethodGet, "/v1/corporate-profile/nominees/"+f.nominee+"/evidence/"+f.nomineeFile, tok, nil), nil
	},
	"removeNomineeEntryEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodDelete, "/v1/corporate-profile/nominees/"+f.nominee+"/evidence/"+f.nomineeFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"createStatementsApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/statements-approvals/"+f.statements, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/corporate-profile/statements-approvals", tok, bodyOf(response{Body: cur.Body["approval"].(map[string]any)}, approveKeys...), "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"getStatementsApproval": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/statements-approvals/"+matrixCorporateFixture(e).statements, tok, nil), nil
	},
	"replaceStatementsApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		cur := e.call(http.MethodGet, "/v1/corporate-profile/statements-approvals/"+f.statements, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/corporate-profile/statements-approvals/"+f.statements, tok, bodyOf(response{Body: cur.Body["approval"].(map[string]any)}, approveKeys...),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"requestStatementsApprovalApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodPost, "/v1/corporate-profile/statements-approvals/"+f.statements+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"addStatementsApprovalEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.upload("/v1/corporate-profile/statements-approvals/"+f.statements+"/evidence", tok, map[string]string{"evidence_type": "Notice"}, "n.pdf", []byte("%PDF-1.7 notice"),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
	"listStatementsApprovalEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/corporate-profile/statements-approvals/"+matrixCorporateFixture(e).statements+"/evidence", tok, nil), nil
	},
	"getStatementsApprovalEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		return e.call(http.MethodGet, "/v1/corporate-profile/statements-approvals/"+f.statements+"/evidence/"+f.statementsFile, tok, nil), nil
	},
	"removeStatementsApprovalEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixCorporateFixture(e)
		was := matrixCorporateState(e, f)
		r := e.call(http.MethodDelete, "/v1/corporate-profile/statements-approvals/"+f.statements+"/evidence/"+f.statementsFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixCorporateState(e, f) == was }
	},
}

// The registers of nominee directors and shareholders and the directors' approval of the statements (s386AL, FR-CS-01,
// FR-CS-03; p1-company-secretary task 2.2). Lion Rock's fund holds shares through Seedling Nominees from 18 Jun 2026:
// told on 25 Jun, entered on 30 Jun and filed centrally on 2 Jul, recorded on Ah Kow's approval. A director among the
// officers acts for Lion Rock. A customer is no nominee shareholder, ACRA is never told before the company's register,
// and a nominee entry names whom they act for. The directors approve the statements for 2025 on 15 Apr 2026, recorded
// with their resolution; an approval on the year's last day is refused. Ah Kow, holding through his own company as a
// nominee, may not approve his own entry.
// req: FR-CS-01
// req: FR-CS-03
// asvs: V8.2.1
func TestTheNomineeRegistersAndTheStatementsApproval(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner)
	owner := c.as(c.weiling, authz.Owner, authz.Controller)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	record := func(path string, draft response) {
		t.Helper()
		if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": "Register extract"}, "extract.pdf", []byte("%PDF-1.7 extract"),
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
	seedling := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Seedling Nominees Pte. Ltd.", "shareholder": true}).str("id")
	customer := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")

	bad := map[string]any{"kind": "shareholder", "holder_id": customer, "nominator": " ", "since": "2026-06-18", "central_on": "2026-07-02"}
	if r := e.call(http.MethodPost, "/v1/corporate-profile/nominees", prep, bad); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, corporate.CodeNominee) || !strings.Contains(r.Raw, `"field":"body.holder_id"`) ||
		!strings.Contains(r.Raw, `"field":"body.nominator"`) || !strings.Contains(r.Raw, `"field":"body.central_on"`) {
		t.Errorf("a nominee entry that does not fit: %d %s", r.Status, r.Raw)
	}
	entry := e.call(http.MethodPost, "/v1/corporate-profile/nominees", prep, map[string]any{"kind": "shareholder", "holder_id": seedling,
		"nominator": "Lion Rock Ventures Fund I", "since": "2026-06-18", "informed_on": "2026-06-25", "register_on": "2026-06-30", "central_on": "2026-07-02"})
	if entry.Status != http.StatusCreated || entry.Body["name"] != "Seedling Nominees Pte. Ltd." || entry.Body["entry_id"] != entry.Body["id"] {
		t.Fatalf("Seedling's entry drafted: %d %s", entry.Status, entry.Raw)
	}
	record("/v1/corporate-profile/nominees/"+entry.str("id"), entry)

	officer := e.call(http.MethodPost, "/v1/corporate-profile/officers", prep, map[string]any{"role": "director", "name": "Lee Wei", "kind": "individual",
		"appointed_on": "2026-06-18", "nominee": true, "nominator": "Lion Rock Ventures Pte. Ltd."})
	record("/v1/corporate-profile/officers/"+officer.str("id"), officer)
	director := e.call(http.MethodPost, "/v1/corporate-profile/nominees", prep, map[string]any{"kind": "director", "officer_id": officer.str("id"),
		"nominator": "Lion Rock Ventures Pte. Ltd.", "since": "2026-06-18", "informed_on": "2026-06-20"})
	if director.Status != http.StatusCreated || director.Body["name"] != "Lee Wei" {
		t.Fatalf("Lee Wei's entry drafted: %d %s", director.Status, director.Raw)
	}

	if r := e.call(http.MethodPost, "/v1/corporate-profile/statements-approvals", prep, map[string]any{"fy_end": "2025-12-31", "approved_on": "2025-12-31"}); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, `"code":"CORP-04","field":"body.approved_on"`) {
		t.Errorf("statements approved on the year's last day: %d %s", r.Status, r.Raw)
	}
	approval := e.call(http.MethodPost, "/v1/corporate-profile/statements-approvals", prep, map[string]any{"fy_end": "2025-12-31", "approved_on": "2026-04-15",
		"sent_on": "2026-04-20"})
	if approval.Status != http.StatusCreated {
		t.Fatalf("the approval drafted: %d %s", approval.Status, approval.Raw)
	}
	record("/v1/corporate-profile/statements-approvals/"+approval.str("id"), approval)

	p := e.call(http.MethodGet, "/v1/corporate-profile?on=2026-07-15", prep, nil)
	nominees := p.Body["nominees"].([]any)
	if p.Status != http.StatusOK || len(nominees) != 1 || nominees[0].(map[string]any)["name"] != "Seedling Nominees Pte. Ltd." ||
		len(p.Body["nominee_history"].([]any)) != 2 || len(p.Body["statements_approvals"].([]any)) != 1 ||
		p.Body["statements_approvals"].([]any)[0].(map[string]any)["status"] != "recorded" {
		t.Errorf("the profile with its registers: %d %s", p.Status, p.Raw)
	}

	own := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Ah Kow Holdings", "shareholder": true}).str("id")
	ct := e.call(http.MethodGet, "/v1/contacts/"+own, owner, nil)
	link := e.call(http.MethodPut, "/v1/contacts/"+own+"/member", kow, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag"))
	if r := approve(link, owner); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow's company: %d %s", r.Status, r.Raw)
	}
	mine := e.call(http.MethodPost, "/v1/corporate-profile/nominees", prep, map[string]any{"kind": "shareholder", "holder_id": own, "nominator": "A family trust",
		"since": "2026-06-18"})
	mpath := "/v1/corporate-profile/nominees/" + mine.str("id")
	if f := e.upload(mpath+"/evidence", prep, map[string]string{"evidence_type": "Notice"}, "notice.pdf", []byte("%PDF-1.7 notice"),
		"If-Match", mine.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("the notice: %d %s", f.Status, f.Raw)
	}
	cur := e.call(http.MethodGet, mpath, prep, nil)
	req := e.call(http.MethodPost, mpath+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
	if r := approve(req, kow); r.Status != http.StatusForbidden || !strings.Contains(r.Raw, "your own entry as a nominee") {
		t.Errorf("Ah Kow approving his own entry: %d %s", r.Status, r.Raw)
	}
}

// The corporate profile's lodgements in the statutory calendar (FR-CS-01, FR-CS-02; p1-company-secretary task 2.4). On
// 20 Nov 2026, with sg-acra in force from 16 Nov: Tan Mei Lin, appointed on Tuesday 17 Nov, is lodged by 1 Dec (14
// days); the office moved on 18 Nov by 2 Dec; Ah Kow, a director since incorporation, ceased on 18 Nov, by 2 Dec; the
// nominee entered in the register on Thursday 19 Nov is filed centrally by Monday 23 Nov (2 business days); each is due
// soon. An officer
// appointed on 1 Oct, before the pack's first day, gives no item. With no detail recorded, an appointment is not
// assessed and names the details. A version with the day lodged clears its item.
// req: FR-CS-01
// req: FR-CS-02
// req: governance/company-secretary FR-CS-01 A change opens its lodgement checklist
func TestTheCorporateLodgementsInTheCalendar(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	e.clock.Set(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)) // before the sessions, so their step-up is fresh
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner)
	owner := c.as(c.weiling, authz.Owner, authz.Controller)
	record := func(path string, body map[string]any) response {
		t.Helper()
		d := e.call(http.MethodPost, path, prep, body)
		if d.Status != http.StatusCreated {
			t.Fatalf("drafting at %s: %d %s", path, d.Status, d.Raw)
		}
		at := path + "/" + d.str("id")
		e.upload(at+"/evidence", prep, map[string]string{"evidence_type": "BizFile+"}, "ack.pdf", []byte("%PDF-1.7 ack"), "If-Match", d.Header.Get("ETag"))
		cur := e.call(http.MethodGet, at, prep, nil)
		req := e.call(http.MethodPost, at+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
		if r := e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", kow, hashOf(req), "If-Match", `"1"`); r.Status != http.StatusOK {
			t.Fatalf("approving %s: %d %s", at, r.Status, r.Raw)
		}
		return d
	}
	lodgements := func() []string {
		t.Helper()
		var out []string
		for _, x := range calendarItems(t, e.call(http.MethodGet, "/v1/compliance/calendar?from=2026-10-01&to=2027-01-31", owner, nil)) {
			if strings.HasPrefix(x, "register_officer") || strings.HasPrefix(x, "register_detail") || strings.HasPrefix(x, "register_nominee") ||
				strings.HasPrefix(x, "sg-acra") {
				out = append(out, x)
			}
		}
		return out
	}
	officers := "/v1/corporate-profile/officers"
	tan := record(officers, map[string]any{"role": "director", "name": "Tan Mei Lin", "kind": "individual", "appointed_on": "2026-11-17", "nominee": false})
	if got := lodgements(); len(got) != 1 || !strings.HasPrefix(got[0], "register_officer_appointment:"+tan.str("id")) || !strings.HasSuffix(got[0], "not_assessed") {
		t.Errorf("with no detail recorded: %v", got)
	}

	details := "/v1/corporate-profile/details"
	record(details, map[string]any{"from": "2026-03-02", "name": "Cognizhi Pte. Ltd.", "registered_office": "1 North Bridge Road", "ssic": "62011"})
	moved := record(details, map[string]any{"from": "2026-11-18", "name": "Cognizhi Pte. Ltd.", "registered_office": "2 Science Park Drive", "ssic": "62011"})
	kowd := record(officers, map[string]any{"role": "director", "name": "Ah Kow", "kind": "individual", "appointed_on": "2026-03-02",
		"appointment_lodged_on": "2026-03-02", "nominee": false})
	record(officers, map[string]any{"officer_id": kowd.str("id"), "role": "director", "name": "Ah Kow", "kind": "individual", "appointed_on": "2026-03-02",
		"appointment_lodged_on": "2026-03-02", "ceased_on": "2026-11-18", "nominee": false})
	record(officers, map[string]any{"role": "secretary", "name": "Lim Hui", "kind": "individual", "appointed_on": "2026-10-01", "nominee": false})
	holder := e.call(http.MethodPost, "/v1/contacts", owner, map[string]any{"legal_name": "Seedling Nominees Pte. Ltd.", "shareholder": true}).str("id")
	entry := record("/v1/corporate-profile/nominees", map[string]any{"kind": "shareholder", "holder_id": holder, "nominator": "Fund I",
		"since": "2026-11-17", "informed_on": "2026-11-17", "register_on": "2026-11-19"})

	want := []string{
		"register_nominee_central:" + entry.str("id") + ":2026-11-19 2026-11-23 due_soon",
		"register_officer_appointment:" + tan.str("id") + ":2026-11-17 2026-12-01 due_soon",
		"register_detail_change:" + moved.str("id") + ":2026-11-18 2026-12-02 due_soon",
		"register_officer_cessation:" + kowd.str("id") + ":2026-11-18 2026-12-02 due_soon",
	}
	got := lodgements()
	slices.Sort(got)
	slices.Sort(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the lodgements:\n got %v\nwant %v", got, want)
	}

	record(officers, map[string]any{"officer_id": tan.str("id"), "role": "director", "name": "Tan Mei Lin", "kind": "individual", "appointed_on": "2026-11-17",
		"appointment_lodged_on": "2026-11-19", "nominee": false})
	for _, x := range lodgements() {
		if strings.HasPrefix(x, "register_officer_appointment") {
			t.Errorf("Tan Mei Lin's appointment, lodged, still open: %s", x)
		}
	}
}
