//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// SAFEs and convertible notes (FR-EQ-18, design D6; p1-equity-register slice 4a): G4-044's post-money SAFE of S$125,000
// with a S$2,500,000 cap is drafted by the Accountant, every problem of a bad draft at once; it is recorded only once
// the signed instrument is attached and another person approves it, never the member who holds it; a recorded one
// changes no more. YC's Singapore template is enabled the same way, with the lawyer's confirmation, once. An Employee
// reads none of it; the Auditor reads it all.
// req: FR-EQ-18
// req: FM-1
// asvs: V8.2.1
func TestInstruments(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	ahKow := e.directorContact("Ah Kow")
	ct := e.call(http.MethodGet, "/v1/contacts/"+ahKow, weiling, nil)
	if r := approve(e.call(http.MethodPut, "/v1/contacts/"+ahKow+"/member", kow, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag")), weiling); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}
	fund := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Lion Rock Ventures Pte. Ltd.", "investor": true}).str("id")
	ordinary := e.shareCapitalIn(e.books.Org)
	safe := func(holder string) map[string]any {
		return map[string]any{"form": "post_money_safe", "holder_id": holder, "amount": "125000.00", "signed_on": "2026-06-18", "valuation_cap": "2500000",
			"trigger": "priced_round", "class_account_id": ordinary, "denominator": "post_money_capitalisation", "price_decimals": 4,
			"template": "yc-sg-post-money-cap@1", "pro_rata": true}
	}

	bad := safe(fund)
	bad["valuation_cap"], bad["class_account_id"], bad["amount"], bad["interest_rate"] = "", e.books.Sales.ID.String(), "0", "8"
	r := e.call(http.MethodPost, "/v1/instruments", prep, bad)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodeInstrument) {
		t.Fatalf("a bad draft: %d %s", r.Status, r.Raw)
	}
	for _, f := range []string{"body.valuation_cap", "body.class_account_id", "body.amount", "body.interest_rate"} {
		if !strings.Contains(r.Raw, `"field":"`+f+`"`) {
			t.Errorf("a bad draft names no %s: %s", f, r.Raw)
		}
	}
	customer := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")
	if r := e.call(http.MethodPost, "/v1/instruments", prep, safe(customer)); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, `"field":"body.holder_id"`) {
		t.Errorf("a customer as holder: %d %s", r.Status, r.Raw)
	}

	d := e.call(http.MethodPost, "/v1/instruments", prep, safe(fund))
	if d.Status != http.StatusCreated || d.str("status") != "draft" || d.str("holder_name") != "Lion Rock Ventures Pte. Ltd." || d.str("class_code") != "3010" ||
		d.Body["template_enabled"] != nil || !strings.Contains(d.Raw, equity.CodeNoEvidence) {
		t.Fatalf("G4-044's SAFE drafted: %d %s", d.Status, d.Raw)
	}
	path := "/v1/instruments/" + d.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", d.Header.Get("ETag")), "requestInstrumentApproval", 422, equity.CodeNoEvidence)
	if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": "Signed SAFE"}, "safe.pdf", []byte("%PDF-1.7 safe"), "If-Match", d.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the SAFE: %d %s", f.Status, f.Raw)
	}
	req := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", d.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectInstrument || !strings.Contains(req.Raw, `"after":"125000.00 SGD"`) {
		t.Fatalf("asking: %d %s", req.Status, req.Raw)
	}
	if a := approve(req, mei); a.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	got := e.call(http.MethodGet, path, e.as(authz.Auditor), nil)
	if got.Status != http.StatusOK || got.str("status") != "recorded" || got.Body["recorded_by"] == nil || got.Header.Get("ETag") != `"1-posted"` {
		t.Fatalf("recorded, as the Auditor reads it: %d %s", got.Status, got.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, prep, safe(fund), "If-Match", `"1-posted"`), "replaceInstrument", 409, equity.CodeRecorded)

	// Ah Kow's own note: drafted, attached and asked, but never approved by Ah Kow.
	note := safe(ahKow)
	note["form"], note["template"], note["valuation_cap"], note["interest_rate"], note["maturity_on"] = "convertible_note", "", "", "8", "2028-06-18"
	n := e.call(http.MethodPost, "/v1/instruments", prep, note)
	if n.Status != http.StatusCreated {
		t.Fatalf("Ah Kow's note: %d %s", n.Status, n.Raw)
	}
	npath := "/v1/instruments/" + n.str("id")
	if f := e.upload(npath+"/evidence", prep, map[string]string{"evidence_type": "Signed note"}, "note.pdf", []byte("%PDF-1.7 note"), "If-Match", n.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the note: %d %s", f.Status, f.Raw)
	}
	nreq := e.call(http.MethodPost, npath+"/approval", prep, nil, "If-Match", n.Header.Get("ETag"))
	if a := approve(nreq, kow); a.Status == http.StatusOK || !strings.Contains(a.Raw, "your own instrument") {
		t.Errorf("Ah Kow approving his own note: %d %s", a.Status, a.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/instruments", weiling, nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 2 {
		t.Errorf("the instruments: %d %s", l.Status, l.Raw)
	}

	// The template: not enabled until its lawyer's confirmation is recorded on another person's approval; once only.
	tl := e.call(http.MethodGet, "/v1/instrument-templates", weiling, nil)
	if tl.Status != http.StatusOK || !strings.Contains(tl.Raw, `"key":"yc-sg-post-money-cap@1"`) || !strings.Contains(tl.Raw, `"enabled":false`) {
		t.Fatalf("the templates: %d %s", tl.Status, tl.Raw)
	}
	en := e.call(http.MethodPost, "/v1/instrument-templates/yc-sg-post-money-cap@1/enablement", prep, map[string]any{"note": "Advice of 3 Nov 2026"})
	if en.Status != http.StatusCreated || en.str("enablement_status") != "draft" {
		t.Fatalf("drafting the enablement: %d %s", en.Status, en.Raw)
	}
	epath := "/v1/instrument-template-enablements/" + en.str("enablement_id")
	e.problemIs(e.call(http.MethodPost, epath+"/approval", prep, nil, "If-Match", en.Header.Get("ETag")), "requestInstrumentTemplateApproval", 422, equity.CodeNoEvidence)
	if f := e.upload(epath+"/evidence", prep, map[string]string{"evidence_type": "Lawyer's confirmation"}, "advice.pdf", []byte("%PDF-1.7 advice"), "If-Match", en.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the advice: %d %s", f.Status, f.Raw)
	}
	ereq := e.call(http.MethodPost, epath+"/approval", prep, nil, "If-Match", en.Header.Get("ETag"))
	if ereq.Status != http.StatusAccepted || ereq.str("object_type") != approval.ObjectInstrumentTemplate {
		t.Fatalf("asking to enable: %d %s", ereq.Status, ereq.Raw)
	}
	if a := approve(ereq, mei); a.Status != http.StatusOK {
		t.Fatalf("enabling: %d %s", a.Status, a.Raw)
	}
	if tl := e.call(http.MethodGet, "/v1/instrument-templates", weiling, nil); !strings.Contains(tl.Raw, `"enabled":true`) || !strings.Contains(tl.Raw, `"enablement_status":"recorded"`) {
		t.Errorf("the template once enabled: %s", tl.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/instrument-templates/yc-sg-post-money-cap@1/enablement", prep, map[string]any{}), "draftInstrumentTemplateEnablement", 409, equity.CodeTemplate)
	e.problemIs(e.call(http.MethodPost, "/v1/instrument-templates/seed-note@1/enablement", prep, map[string]any{}), "draftInstrumentTemplateEnablement", 409, equity.CodeTemplate)
	if got := e.call(http.MethodGet, path, weiling, nil); got.Body["template_enabled"] != true {
		t.Errorf("the SAFE once its template is enabled: %s", got.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/instruments", e.as(authz.Employee), nil), "listInstruments", 403, CodeForbidden)
	if trail := auditTrail(t, e); !strings.Contains(trail, "instrument_template") || strings.Count(trail, `"action":"document.posted"`) < 2 {
		t.Error("the instrument and the enablement were not audited")
	}
}

// instrumentFixture is the matrix's instruments: a drafted SAFE with its signed copy, and the YC template's drafted
// enablement with its lawyer's confirmation.
type instrumentFixture struct{ draft, draftFile, enablement, enablementFile string }

var matrixInstrumentOf = map[*env]instrumentFixture{}

func matrixInstrument(e *env) instrumentFixture {
	e.t.Helper()
	if f, ok := matrixInstrumentOf[e]; ok {
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
	holder := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix SAFE holder", "investor": true}).str("id")
	d := e.call(http.MethodPost, "/v1/instruments", acc, map[string]any{"form": "post_money_safe", "holder_id": holder, "amount": "100.00",
		"signed_on": "2026-10-01", "valuation_cap": "1000000", "trigger": "priced_round", "class_account_id": capital,
		"denominator": "post_money_capitalisation", "price_decimals": 4})
	df := e.upload("/v1/instruments/"+d.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Signed SAFE"}, "safe.pdf",
		[]byte("%PDF-1.7 matrix safe"), "If-Match", d.Header.Get("ETag"))
	en := e.call(http.MethodPost, "/v1/instrument-templates/yc-sg-post-money-cap@1/enablement", acc, map[string]any{})
	ef := e.upload("/v1/instrument-template-enablements/"+en.str("enablement_id")+"/evidence", acc, map[string]string{"evidence_type": "Lawyer's confirmation"},
		"advice.pdf", []byte("%PDF-1.7 matrix advice"), "If-Match", en.Header.Get("ETag"))
	if d.Status != http.StatusCreated || df.Status != http.StatusCreated || en.Status != http.StatusCreated || ef.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's instruments: %d %d %d %d %s %s", d.Status, df.Status, en.Status, ef.Status, d.Raw, en.Raw)
	}
	f := instrumentFixture{draft: d.str("id"), draftFile: df.str("id"), enablement: en.str("enablement_id"), enablementFile: ef.str("id")}
	matrixInstrumentOf[e] = f
	return f
}

// matrixInstrumentState is what a refused call must leave as it was: the instrument, the templates and both files.
func matrixInstrumentState(e *env, f instrumentFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/instruments/" + f.draft, "/v1/instruments/" + f.draft + "/evidence", "/v1/instruments", "/v1/instrument-templates",
		"/v1/instrument-template-enablements/" + f.enablement + "/evidence"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

// matrixInstruments are the authorisation matrix's rows for SAFEs, notes and their templates (FR-EQ-18): writes as
// previews, so a refused one shows it changed nothing.
var matrixInstruments = map[string]func(e *env, tok string) (response, func() bool){
	"listInstruments": func(e *env, tok string) (response, func() bool) {
		matrixInstrument(e)
		return e.call(http.MethodGet, "/v1/instruments", tok, nil), nil
	},
	"getInstrument": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/instruments/"+matrixInstrument(e).draft, tok, nil), nil
	},
	"createInstrument": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		cur := e.call(http.MethodGet, "/v1/instruments/"+f.draft, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/instruments", tok, draftBodyOf(cur), "Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"replaceInstrument": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		cur := e.call(http.MethodGet, "/v1/instruments/"+f.draft, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/instruments/"+f.draft, tok, draftBodyOf(cur), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"requestInstrumentApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		r := e.call(http.MethodPost, "/v1/instruments/"+f.draft+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"addInstrumentEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		r := e.upload("/v1/instruments/"+f.draft+"/evidence", tok, map[string]string{"evidence_type": "Side letter"}, "letter.pdf",
			[]byte("%PDF-1.7 side letter"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"listInstrumentEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/instruments/"+matrixInstrument(e).draft+"/evidence", tok, nil), nil
	},
	"getInstrumentEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		return e.call(http.MethodGet, "/v1/instruments/"+f.draft+"/evidence/"+f.draftFile, tok, nil), nil
	},
	"removeInstrumentEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		r := e.call(http.MethodDelete, "/v1/instruments/"+f.draft+"/evidence/"+f.draftFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"listInstrumentTemplates": func(e *env, tok string) (response, func() bool) {
		matrixInstrument(e)
		return e.call(http.MethodGet, "/v1/instrument-templates", tok, nil), nil
	},
	"draftInstrumentTemplateEnablement": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		r := e.call(http.MethodPost, "/v1/instrument-templates/yc-sg-post-money-cap@1/enablement", tok, map[string]any{}, "Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"requestInstrumentTemplateApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		r := e.call(http.MethodPost, "/v1/instrument-template-enablements/"+f.enablement+"/approval", steppedUp(e, tok), nil, "If-Match", "*",
			"Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"addInstrumentTemplateEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		r := e.upload("/v1/instrument-template-enablements/"+f.enablement+"/evidence", tok, map[string]string{"evidence_type": "Lawyer's confirmation"},
			"advice.pdf", []byte("%PDF-1.7 more advice"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
	"listInstrumentTemplateEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/instrument-template-enablements/"+matrixInstrument(e).enablement+"/evidence", tok, nil), nil
	},
	"getInstrumentTemplateEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		return e.call(http.MethodGet, "/v1/instrument-template-enablements/"+f.enablement+"/evidence/"+f.enablementFile, tok, nil), nil
	},
	"removeInstrumentTemplateEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixInstrument(e)
		was := matrixInstrumentState(e, f)
		r := e.call(http.MethodDelete, "/v1/instrument-template-enablements/"+f.enablement+"/evidence/"+f.enablementFile, tok, nil, "If-Match", "*",
			"Prefer", "return=preview")
		return r, func() bool { return matrixInstrumentState(e, f) == was }
	},
}

// draftBodyOf is an instrument's terms as a draft takes them, from its view.
func draftBodyOf(r response) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"form", "holder_id", "amount", "signed_on", "valuation_cap", "trigger", "class_account_id", "denominator", "price_decimals"} {
		if v, ok := r.Body[k]; ok {
			out[k] = v
		}
	}
	return out
}
