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
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Investor rights, registrable controllers and the exempt status re-tested (FR-EQ-22, FR-EQ-25, FR-EQ-11; design D10;
// p1-equity-register slice 7). Ah Kow holds 700,000 shares from 2 Jan 2025 and Lion Rock 300,000 from 14 Mar 2025, 30%:
// both are registrable controllers, to enter by 9 Jan and 21 Mar. Seedling holds no shares but has a board majority and
// consent rights, recorded from the side letter: not assessed until the company secretary concludes it is not a
// controller (G4-101). Ah Kow's entry, entered on Sunday 5 Jan 2025, is due in ACRA's central register by Tuesday 7 Jan;
// lodged, his yearly notice falls due a year on, and a notice unanswered 30 days is followed up. Ah Kow never approves
// his own entry. With Lion Rock recorded as an entity, the register says the company is no longer exempt though the
// status recorded says it is (C.2 #28).
// req: FR-EQ-22
// req: FR-EQ-25
// req: FR-EQ-11
// req: G4-101
// asvs: V8.2.1
func TestHolderRightsAndControllers(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	mei := c.as(c.mei, authz.Owner)
	kow := c.as(c.ahkow, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
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
	seedling := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Seedling Ventures Pte. Ltd.", "investor": true}).str("id")
	customer := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")
	ordinary := e.shareCapitalIn(e.books.Org)
	for _, x := range []struct{ holder, shares, on string }{{founder, "700000", "2025-01-02"}, {lion, "300000", "2025-03-14"}} {
		if r := e.call(http.MethodPost, "/v1/share-issues", weiling, map[string]any{"holder_id": x.holder, "class_account_id": ordinary, "shares": x.shares,
			"price": "0.01", "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": x.on}); r.Status != http.StatusCreated {
			t.Fatalf("the shares of %s: %d %s", x.holder, r.Status, r.Raw)
		}
	}
	posted := func(id string) {
		t.Helper()
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Documents().SetStatus(ctx, uuid.MustParse(id), storage.DocumentPosted)
		})
	}
	tests := func() response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/cap-table/statutory-tests", weiling, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("the statutory tests: %d %s", r.Status, r.Raw)
		}
		return r
	}
	standing := func(r response, holder string) map[string]any {
		for _, x := range r.Body["controllers"].([]any) {
			if m := x.(map[string]any); m["holder_id"] == holder {
				return m
			}
		}
		return nil
	}

	r := tests()
	if a := standing(r, founder); a == nil || a["standing"] != "to_enter" || a["ownership"] != "70.00" || a["since"] != "2025-01-02" ||
		a["company_register_due"] != "2025-01-09" || a["shares"] != true || a["votes"] != true {
		t.Errorf("Ah Kow at 70%%: %s", r.Raw)
	}
	if l := standing(r, lion); l == nil || l["standing"] != "to_enter" || l["ownership"] != "30.00" || l["company_register_due"] != "2025-03-21" {
		t.Errorf("Lion Rock at 30%%: %s", r.Raw)
	}
	if !strings.Contains(r.Raw, `"exempt":{"members":2,"entities":[],"unknown":["Ah Kow","Lion Rock Ventures Pte. Ltd."],"decided":false`) {
		t.Errorf("the exempt test with no holder record: %s", r.Raw)
	}

	bad := map[string]any{"holder_id": customer, "from": "2099-01-01", "board_seats": 2, "observer": false, "board_majority": true, "pro_rata": true,
		"consents": []string{"Issuing new shares", "Issuing new shares"}, "agreements": " "}
	if r := e.call(http.MethodPost, "/v1/investor-rights", prep, bad); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodeRights) ||
		!strings.Contains(r.Raw, `"field":"body.from"`) || !strings.Contains(r.Raw, `"field":"body.consents[1]"`) || !strings.Contains(r.Raw, `"field":"body.agreements"`) ||
		!strings.Contains(r.Raw, `"field":"body.holder_id"`) {
		t.Errorf("rights that do not fit: %d %s", r.Status, r.Raw)
	}
	good := map[string]any{"holder_id": seedling, "from": "2026-06-18", "board_seats": 2, "observer": false, "board_majority": true, "pro_rata": true,
		"information": "Quarterly management accounts within 45 days", "consents": []string{"Issuing new shares"}, "agreements": "Shareholders' agreement of 18 Jun 2026"}
	rights := e.call(http.MethodPost, "/v1/investor-rights", prep, good)
	if rights.Status != http.StatusCreated || rights.Body["confers_control"] != true || !strings.Contains(rights.Raw, equity.CodeNoEvidence) {
		t.Fatalf("Seedling's rights drafted: %d %s", rights.Status, rights.Raw)
	}
	rpath := "/v1/investor-rights/" + rights.str("id")
	e.problemIs(e.call(http.MethodPost, rpath+"/approval", prep, nil, "If-Match", rights.Header.Get("ETag")), "requestInvestorRightsApproval", 422, equity.CodeNoEvidence)
	if f := e.upload(rpath+"/evidence", prep, map[string]string{"evidence_type": "Shareholders' agreement"}, "sha.pdf", []byte("%PDF-1.7 sha"),
		"If-Match", rights.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("the agreement: %d %s", f.Status, f.Raw)
	}
	cur := e.call(http.MethodGet, rpath, prep, nil)
	req := e.call(http.MethodPost, rpath+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || !strings.Contains(req.Raw, "Record Seedling Ventures Pte. Ltd.'s rights from Shareholders' agreement of 18 Jun 2026") {
		t.Fatalf("asking for the rights: %d %s", req.Status, req.Raw)
	}
	if a := approve(req, mei); a.Status != http.StatusOK {
		t.Fatalf("recording the rights: %d %s", a.Status, a.Raw)
	}
	if got := e.call(http.MethodGet, rpath, weiling, nil); got.str("status") != "recorded" || got.Body["recorded_by"] == nil {
		t.Errorf("the rights once recorded: %s", got.Raw)
	}
	r = tests()
	if s := standing(r, seedling); s == nil || s["standing"] != "not_assessed" || s["rights"] != true || s["shares"] != false {
		t.Errorf("Seedling's rights to weigh: %s", r.Raw)
	}
	check := e.checked("equity.controllers-registered")
	wantStatus(t, check, "due")
	if it := itemNamed(check, founder); it == nil || !strings.Contains(it["detail"].(string), "Holds 70.00% of the shares and 70.00% of the votes since 2 Jan 2025") ||
		!strings.Contains(it["detail"].(string), "by 9 Jan 2025") {
		t.Errorf("C.2 #27 for Ah Kow: %s", check.Raw)
	}
	if it := itemNamed(check, seedling+"/rights"); it == nil || it["severity"] != "warning" || !strings.Contains(it["detail"].(string), "Not assessed") {
		t.Errorf("C.2 #27 for Seedling's rights: %s", check.Raw)
	}

	if r := e.call(http.MethodPost, "/v1/controller-entries", prep, map[string]any{"holder_id": founder, "controller": true, "grounds": []string{},
		"reason": "none", "since": "2025-01-02", "central_register_on": "2025-01-07", "notices": []any{}}); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, equity.CodeController) || !strings.Contains(r.Raw, `"field":"body.grounds"`) || !strings.Contains(r.Raw, `"field":"body.reason"`) ||
		!strings.Contains(r.Raw, `"field":"body.central_register_on"`) {
		t.Errorf("an entry that does not fit: %d %s", r.Status, r.Raw)
	}
	entry := e.call(http.MethodPost, "/v1/controller-entries", prep, map[string]any{"holder_id": founder, "controller": true, "grounds": []string{"shares", "votes"},
		"since": "2025-01-02", "company_register_on": "2025-01-05", "notices": []any{}})
	if entry.Status != http.StatusCreated {
		t.Fatalf("Ah Kow's entry: %d %s", entry.Status, entry.Raw)
	}
	epath := "/v1/controller-entries/" + entry.str("id")
	if f := e.upload(epath+"/evidence", prep, map[string]string{"evidence_type": "Company secretary's confirmation"}, "cosec.pdf", []byte("%PDF-1.7 cosec"),
		"If-Match", entry.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("the confirmation: %d %s", f.Status, f.Raw)
	}
	ct := e.call(http.MethodGet, "/v1/contacts/"+founder, weiling, nil)
	if r := approve(e.call(http.MethodPut, "/v1/contacts/"+founder+"/member", weiling, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag")), mei); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}
	cur = e.call(http.MethodGet, epath, prep, nil)
	req = e.call(http.MethodPost, epath+"/approval", prep, nil, "If-Match", cur.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || !strings.Contains(req.Raw, "Record Ah Kow as a registrable controller") {
		t.Fatalf("asking for Ah Kow's entry: %d %s", req.Status, req.Raw)
	}
	if a := approve(req, kow); a.Status == http.StatusOK || !strings.Contains(a.Raw, "your own") {
		t.Errorf("Ah Kow approving his own entry: %d %s", a.Status, a.Raw)
	}
	if a := approve(req, mei); a.Status != http.StatusOK {
		t.Fatalf("recording Ah Kow's entry: %d %s", a.Status, a.Raw)
	}
	r = tests()
	if a := standing(r, founder); a == nil || a["standing"] != "to_lodge" || a["central_register_due"] != "2025-01-07" || a["entry"] == nil {
		t.Errorf("Ah Kow entered on Sunday 5 Jan, due in ACRA's register by Tuesday 7 Jan: %s", r.Raw)
	}

	notOne := e.call(http.MethodPost, "/v1/controller-entries", prep, map[string]any{"holder_id": seedling, "controller": false, "grounds": []string{},
		"reason": "The consent matters protect the investment and do not direct the company", "since": "2026-10-15", "notices": []any{}})
	if notOne.Status != http.StatusCreated {
		t.Fatalf("Seedling found not to be a controller: %d %s", notOne.Status, notOne.Raw)
	}
	posted(notOne.str("id"))
	second := e.call(http.MethodPost, "/v1/controller-entries", prep, map[string]any{"holder_id": founder, "controller": true, "grounds": []string{"shares", "votes"},
		"since": "2025-01-02", "company_register_on": "2025-01-05", "central_register_on": "2025-01-07",
		"notices": []map[string]any{{"sent_on": "2026-01-04"}}})
	if second.Status != http.StatusCreated {
		t.Fatalf("Ah Kow lodged, with this year's notice: %d %s", second.Status, second.Raw)
	}
	posted(second.str("id"))
	r = tests()
	if s := standing(r, seedling); s == nil || s["standing"] != "not_a_controller" {
		t.Errorf("Seedling concluded not a controller: %s", r.Raw)
	}
	if a := standing(r, founder); a == nil || a["standing"] != "registered" || a["notice_due"] != "2027-01-04" {
		t.Errorf("Ah Kow registered, his next notice a year after the last: %s", r.Raw)
	}
	check = e.checked("equity.controllers-registered")
	wantStatus(t, check, "due")
	if itemNamed(check, founder) != nil || itemNamed(check, seedling+"/rights") != nil || itemNamed(check, lion) == nil {
		t.Errorf("C.2 #27 once Ah Kow is registered and Seedling concluded: %s", check.Raw)
	}
	if it := itemNamed(check, founder+"/reply"); it == nil || it["severity"] != "warning" || !strings.Contains(it["detail"].(string), "No reply to the notice of 4 Jan 2026") {
		t.Errorf("C.2 #27 for the notice unanswered: %s", check.Raw)
	}

	wantStatus(t, e.checked("equity.exempt-status"), "ok")
	status := e.call(http.MethodPost, "/v1/exempt-statuses", weiling, map[string]any{"from": "2025-01-02", "exempt": true})
	if status.Status != http.StatusCreated {
		t.Fatalf("the exempt status: %d %s", status.Status, status.Raw)
	}
	posted(status.str("id"))
	for _, x := range []map[string]any{{"contact_id": founder, "kind": "individual", "id_type": "nric", "id_last4": "567A"},
		{"contact_id": lion, "kind": "entity", "id_type": "uen", "registration_number": "202312345K"}} {
		rec := e.call(http.MethodPost, "/v1/holder-records", prep, x)
		if rec.Status != http.StatusCreated {
			t.Fatalf("a holder record: %d %s", rec.Status, rec.Raw)
		}
		posted(rec.str("id"))
	}
	r = tests()
	if !strings.Contains(r.Raw, `"exempt":{"members":2,"entities":["Lion Rock Ventures Pte. Ltd."],"unknown":[],"decided":true,"exempt":false,"recorded":true,"differs":true}`) {
		t.Errorf("an entity member against the exempt status recorded: %s", r.Raw)
	}
	check = e.checked("equity.exempt-status")
	wantStatus(t, check, "warning")
	if it := itemNamed(check, status.str("id")); it == nil || !strings.Contains(it["detail"].(string), "a member is an entity (Lion Rock Ventures Pte. Ltd.)") {
		t.Errorf("C.2 #28: %s", check.Raw)
	}

	h := e.call(http.MethodGet, "/v1/holders/"+founder, weiling, nil)
	if h.Body["controller"] == nil || len(h.Body["controller_history"].([]any)) != 2 || !strings.Contains(h.Raw, `"status":"superseded"`) {
		t.Errorf("Ah Kow's holder page: %s", h.Raw)
	}
	if s := e.call(http.MethodGet, "/v1/holders/"+seedling, weiling, nil); s.Body["rights"] == nil || !strings.Contains(s.Raw, `"confers_control":true`) {
		t.Errorf("Seedling's holder page: %s", s.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table/statutory-tests", e.as(authz.Employee), nil), "getStatutoryTests", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/investor-rights", mei, good), "createInvestorRights", 403, CodeForbidden)
}

// heldFixture is the matrix's investor rights and controller entry, each a draft with a file attached.
type heldFixture struct{ holder, rights, rightsFile, entry, entryFile string }

var matrixHeldOf = map[*env]heldFixture{}

func matrixHeld(e *env) heldFixture {
	e.t.Helper()
	if f, ok := matrixHeldOf[e]; ok {
		return f
	}
	acc := e.as(authz.Accountant)
	holder := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix rights holder", "investor": true}).str("id")
	r := e.call(http.MethodPost, "/v1/investor-rights", acc, map[string]any{"holder_id": holder, "from": "2026-06-18", "board_seats": 1, "observer": false,
		"board_majority": false, "pro_rata": true, "consents": []string{}, "agreements": "Matrix side letter"})
	rf := e.upload("/v1/investor-rights/"+r.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Side letter"}, "letter.pdf",
		[]byte("%PDF-1.7 matrix side letter"), "If-Match", r.Header.Get("ETag"))
	c := e.call(http.MethodPost, "/v1/controller-entries", acc, map[string]any{"holder_id": holder, "controller": true, "grounds": []string{"shares"},
		"since": "2026-01-02", "company_register_on": "2026-01-05", "notices": []any{}})
	cf := e.upload("/v1/controller-entries/"+c.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Company secretary's confirmation"}, "cosec.pdf",
		[]byte("%PDF-1.7 matrix confirmation"), "If-Match", c.Header.Get("ETag"))
	if r.Status != http.StatusCreated || rf.Status != http.StatusCreated || c.Status != http.StatusCreated || cf.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's rights and entry: %d %d %d %d %s %s", r.Status, rf.Status, c.Status, cf.Status, r.Raw, c.Raw)
	}
	f := heldFixture{holder: holder, rights: r.str("id"), rightsFile: rf.str("id"), entry: c.str("id"), entryFile: cf.str("id")}
	matrixHeldOf[e] = f
	return f
}

// matrixHeldState is what a refused call must leave as it was: the drafts and their files.
func matrixHeldState(e *env, f heldFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/investor-rights/" + f.rights, "/v1/investor-rights/" + f.rights + "/evidence", "/v1/controller-entries/" + f.entry,
		"/v1/controller-entries/" + f.entry + "/evidence", "/v1/holders/" + f.holder} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

var (
	rightsKeys = []string{"from", "board_seats", "observer", "board_majority", "pro_rata", "consents", "agreements"}
	entryKeys  = []string{"controller", "grounds", "since", "company_register_on", "notices"}
)

// matrixHeldRights are the authorisation matrix's rows for investor rights, controller entries and the statutory tests
// (FR-EQ-22, FR-EQ-25).
var matrixHeldRights = map[string]func(e *env, tok string) (response, func() bool){
	"createInvestorRights": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		cur := e.call(http.MethodGet, "/v1/investor-rights/"+f.rights, e.as(authz.Accountant), nil)
		body := bodyOf(response{Body: cur.Body["rights"].(map[string]any)}, rightsKeys...)
		body["holder_id"] = f.holder
		r := e.call(http.MethodPost, "/v1/investor-rights", tok, body, "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"getInvestorRights": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/investor-rights/"+matrixHeld(e).rights, tok, nil), nil
	},
	"replaceInvestorRights": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		cur := e.call(http.MethodGet, "/v1/investor-rights/"+f.rights, e.as(authz.Accountant), nil)
		body := bodyOf(response{Body: cur.Body["rights"].(map[string]any)}, rightsKeys...)
		r := e.call(http.MethodPut, "/v1/investor-rights/"+f.rights, tok, body, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"requestInvestorRightsApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		r := e.call(http.MethodPost, "/v1/investor-rights/"+f.rights+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"addInvestorRightsEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		r := e.upload("/v1/investor-rights/"+f.rights+"/evidence", tok, map[string]string{"evidence_type": "Subscription agreement"}, "sub.pdf",
			[]byte("%PDF-1.7 subscription"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"listInvestorRightsEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/investor-rights/"+matrixHeld(e).rights+"/evidence", tok, nil), nil
	},
	"getInvestorRightsEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		return e.call(http.MethodGet, "/v1/investor-rights/"+f.rights+"/evidence/"+f.rightsFile, tok, nil), nil
	},
	"removeInvestorRightsEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		r := e.call(http.MethodDelete, "/v1/investor-rights/"+f.rights+"/evidence/"+f.rightsFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"createControllerEntry": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		cur := e.call(http.MethodGet, "/v1/controller-entries/"+f.entry, e.as(authz.Accountant), nil)
		body := bodyOf(response{Body: cur.Body["entry"].(map[string]any)}, entryKeys...)
		body["holder_id"] = f.holder
		r := e.call(http.MethodPost, "/v1/controller-entries", tok, body, "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"getControllerEntry": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/controller-entries/"+matrixHeld(e).entry, tok, nil), nil
	},
	"replaceControllerEntry": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		cur := e.call(http.MethodGet, "/v1/controller-entries/"+f.entry, e.as(authz.Accountant), nil)
		body := bodyOf(response{Body: cur.Body["entry"].(map[string]any)}, entryKeys...)
		r := e.call(http.MethodPut, "/v1/controller-entries/"+f.entry, tok, body, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"requestControllerEntryApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		r := e.call(http.MethodPost, "/v1/controller-entries/"+f.entry+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"addControllerEntryEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		r := e.upload("/v1/controller-entries/"+f.entry+"/evidence", tok, map[string]string{"evidence_type": "Register extract"}, "register.pdf",
			[]byte("%PDF-1.7 register"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"listControllerEntryEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/controller-entries/"+matrixHeld(e).entry+"/evidence", tok, nil), nil
	},
	"getControllerEntryEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		return e.call(http.MethodGet, "/v1/controller-entries/"+f.entry+"/evidence/"+f.entryFile, tok, nil), nil
	},
	"removeControllerEntryEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixHeld(e)
		was := matrixHeldState(e, f)
		r := e.call(http.MethodDelete, "/v1/controller-entries/"+f.entry+"/evidence/"+f.entryFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixHeldState(e, f) == was }
	},
	"getStatutoryTests": func(e *env, tok string) (response, func() bool) {
		matrixHeld(e)
		return e.call(http.MethodGet, "/v1/cap-table/statutory-tests", tok, nil), nil
	},
}
