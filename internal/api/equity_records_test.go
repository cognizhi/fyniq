//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The equity register's holders and share classes (FR-EQ-15, FR-EQ-16; p1-equity-register slice 1): a contact marked
// shareholder or investor is a holder; its record keeps an individual's identifier to its type and last 4 characters and
// never a full NRIC (the owner's decision of 7 Nov 2026); a record and a class are recorded only on another person's
// approval once their evidence is attached, never the holder's own; the DPO reads a record masked and never opens its
// identity document; an Employee lists no holders. A class belongs to an active share-capital account, a new version of
// its rights keeps it, and a share issue with no class takes its account's first recorded class.
// req: FR-EQ-15
// req: FR-EQ-16
// req: CMP-PD-07
// asvs: V8.2.1, V14.2.1
func TestHoldersAndShareClasses(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant) // Appendix D: the Accountant prepares (C/P), an Owner or the Controller approves
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}

	holder := e.directorContact("Ah Kow")
	fund := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Seed Fund I LP", "investor": true})
	if fund.Status != http.StatusCreated || fund.Body["investor"] != true {
		t.Fatalf("an investor: %d %s", fund.Status, fund.Raw)
	}
	ct := e.call(http.MethodGet, "/v1/contacts/"+holder, weiling, nil)
	link := e.call(http.MethodPut, "/v1/contacts/"+holder+"/member", kow, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag"))
	if r := approve(link, weiling); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}

	if l := e.call(http.MethodGet, "/v1/holders", weiling, nil); l.Status != http.StatusOK || !strings.Contains(l.Raw, "Seed Fund I LP") ||
		!strings.Contains(l.Raw, `"recorded":false`) {
		t.Fatalf("the holders: %d %s", l.Status, l.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/holders", e.as(authz.Employee), nil), "listHolders", 403, CodeForbidden)

	record := map[string]any{"kind": "individual", "id_type": "nric", "id_last4": "567d", "nationality": "Singapore",
		"address": "1 Raffles Place, Singapore 048616", "contact_people": []map[string]any{{"name": "Ah Kow", "email": "kow@example.com"}}}
	for name, over := range map[string]map[string]any{
		"a full NRIC as the identifier": {"id_last4": "S1234567D"},
		"a full NRIC in the note":       {"note": "NRIC S1234567D"},
		"an entity's number":            {"registration_number": "202312345K"},
	} {
		b := map[string]any{}
		for k, v := range record {
			b[k] = v
		}
		for k, v := range over {
			b[k] = v
		}
		b["contact_id"] = holder
		if r := e.call(http.MethodPost, "/v1/holder-records", prep, b); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", name, r.Status, r.Raw)
		}
	}
	record["contact_id"] = holder
	r := e.call(http.MethodPost, "/v1/holder-records", prep, record)
	if r.Status != http.StatusCreated || r.str("status") != "draft" || r.str("details", "id_last4") != "567D" || !strings.Contains(r.Raw, equity.CodeNoEvidence) {
		t.Fatalf("drafting the record: %d %s", r.Status, r.Raw)
	}
	path := "/v1/holder-records/" + r.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", r.Header.Get("ETag")), "requestHolderRecordApproval", 422, equity.CodeNoEvidence)
	f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": "Identity document"}, "nric.pdf", []byte("%PDF-1.7 identity"),
		"If-Match", r.Header.Get("ETag"))
	if f.Status != http.StatusCreated || f.str("data_class") != "identity" {
		t.Fatalf("attaching the identity document: %d %s", f.Status, f.Raw)
	}
	req := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", r.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectHolderRecord {
		t.Fatalf("asking: %d %s", req.Status, req.Raw)
	}
	e.problemIs(approve(req, weiling), "approveApproval", 403, approval.CodeOwnRequest)
	e.problemIs(approve(req, kow), "approveApproval", 403, approval.CodeNotApprover)
	if a := approve(req, mei); a.Status != http.StatusOK {
		t.Fatalf("Mei approving: %d %s", a.Status, a.Raw)
	}
	h := e.call(http.MethodGet, "/v1/holders/"+holder, weiling, nil)
	if h.Status != http.StatusOK || h.Body["recorded"] != true || h.str("identifier") != "NRIC ending 567D" || h.str("record", "status") != "recorded" {
		t.Fatalf("the holder once recorded: %d %s", h.Status, h.Raw)
	}
	delete(record, "contact_id")
	e.problemIs(e.call(http.MethodPut, path, prep, record, "If-Match", `"1-posted"`), "replaceHolderRecord", 409, equity.CodeRecorded)

	dpo := e.as(authz.DPO)
	if m := e.call(http.MethodGet, path, dpo, nil); m.Status != http.StatusOK || m.Body["masked"] != true || strings.Contains(m.Raw, "567D") ||
		strings.Contains(m.Raw, "Raffles") {
		t.Errorf("the DPO's masked read: %d %s", m.Status, m.Raw)
	}
	if file := e.call(http.MethodGet, "/v1/evidence/"+f.str("id")+"/file", dpo, nil); file.Status == http.StatusOK {
		t.Errorf("the DPO opened the identity document: %s", file.Raw)
	}
	// No one opens an identity-class file in the app until FR-DOC-09's permissions for it exist; it stays as evidence.
	e.problemIs(e.call(http.MethodGet, "/v1/evidence/"+f.str("id")+"/file", weiling, nil), "getEvidenceFile", 404, CodeNotFound)

	// A share class on 3010, recorded on approval; a share issue with no class takes it.
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
	ordinary := e.shareCapitalIn(e.books.Org)
	class := map[string]any{"name": "Ordinary shares", "kind": "ordinary", "account_id": ordinary, "votes_per_share": "1",
		"liquidation": map[string]any{}, "dividend": map[string]any{"kind": "none"}, "anti_dilution": "none"}
	bad := map[string]any{}
	for k, v := range class {
		bad[k] = v
	}
	bad["account_id"], bad["votes_per_share"] = e.books.Sales.ID.String(), "-1"
	if x := e.call(http.MethodPost, "/v1/share-classes", prep, bad); x.Status != http.StatusUnprocessableEntity || !strings.Contains(x.Raw, equity.CodeShareClass) ||
		!strings.Contains(x.Raw, "body.votes_per_share") {
		t.Errorf("a class on sales with negative votes: %d %s", x.Status, x.Raw)
	}
	sc := e.call(http.MethodPost, "/v1/share-classes", prep, class)
	if sc.Status != http.StatusCreated || sc.str("account_code") != "3010" || sc.str("status") != "draft" {
		t.Fatalf("drafting the class: %d %s", sc.Status, sc.Raw)
	}
	cpath := "/v1/share-classes/" + sc.str("version_id")
	if f := e.upload(cpath+"/evidence", prep, map[string]string{"evidence_type": "Constitution"}, "constitution.pdf", []byte("%PDF-1.7 constitution"),
		"If-Match", sc.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the constitution: %d %s", f.Status, f.Raw)
	}
	creq := e.call(http.MethodPost, cpath+"/approval", prep, nil, "If-Match", sc.Header.Get("ETag"))
	if creq.Status != http.StatusAccepted {
		t.Fatalf("asking for the class: %d %s", creq.Status, creq.Raw)
	}
	if a := approve(creq, mei); a.Status != http.StatusOK {
		t.Fatalf("approving the class: %d %s", a.Status, a.Raw)
	}
	classID := sc.str("id")
	if l := e.call(http.MethodGet, "/v1/share-classes", e.as(authz.Auditor), nil); l.Status != http.StatusOK || !strings.Contains(l.Raw, `"status":"recorded"`) {
		t.Errorf("the classes, as the Auditor reads them: %d %s", l.Status, l.Raw)
	}
	issue := e.call(http.MethodPost, "/v1/share-issues", weiling, map[string]any{"holder_id": holder, "class_account_id": ordinary, "shares": "600000",
		"price": "0.01", "consideration": "cash", "resolution_refs": "Incorporation subscription", "effective_on": "2025-01-02"})
	if issue.Status != http.StatusCreated || issue.str("class_id") != classID {
		t.Fatalf("an issue with no class: %d %s", issue.Status, issue.Raw)
	}
	if h := e.call(http.MethodGet, "/v1/holders/"+holder, weiling, nil); !strings.Contains(h.Raw, `"class_name":"Ordinary shares"`) || !strings.Contains(h.Raw, `"shares":"600000"`) {
		t.Errorf("the holder's holdings: %s", h.Raw)
	}

	// A new version of the class's rights keeps its account; another account is refused.
	again := map[string]any{}
	for k, v := range class {
		again[k] = v
	}
	again["class_id"], again["votes_per_share"] = classID, "2"
	if v := e.call(http.MethodPost, "/v1/share-classes", prep, again); v.Status != http.StatusCreated || v.str("id") != classID || v.str("version_id") == classID {
		t.Errorf("a new version of the rights: %d %s", v.Status, v.Raw)
	}
	other := e.call(http.MethodPost, "/v1/share-classes", prep, map[string]any{"class_id": classID, "name": "Ordinary shares", "kind": "ordinary",
		"account_id": e.shareCapital3020(), "votes_per_share": "1", "liquidation": map[string]any{}, "dividend": map[string]any{"kind": "none"}, "anti_dilution": "none"})
	if other.Status != http.StatusUnprocessableEntity || !strings.Contains(other.Raw, "keeps its account") {
		t.Errorf("a new version on another account: %d %s", other.Status, other.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "holder_record") || !strings.Contains(trail, "share_class") || !strings.Contains(trail, `"action":"document.posted"`) {
		t.Error("the records were not audited")
	}
}

// shareCapital3020 adds 3020, preference share capital.
func (e *env) shareCapital3020() string {
	e.t.Helper()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3020", Name: "Preference share capital", Type: storage.Equity, Role: storage.RoleShareCapital})
		id = a.ID.String()
		return err
	})
	return id
}

// equityFixture is the matrix's holder, its drafted record with an identity document, and a drafted share class with
// its constitution, in e's organisation.
type equityFixture struct{ holder, record, recordFile, class, classFile string }

var matrixEquityOf = map[*env]equityFixture{}

func matrixEquity(e *env) equityFixture {
	e.t.Helper()
	if f, ok := matrixEquityOf[e]; ok {
		return f
	}
	acc := e.as(authz.Accountant)
	h := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix holder", "shareholder": true})
	if h.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's holder: %d %s", h.Status, h.Raw)
	}
	r := e.call(http.MethodPost, "/v1/holder-records", acc, map[string]any{"contact_id": h.str("id"), "kind": "individual", "id_type": "nric", "id_last4": "123A"})
	rf := e.upload("/v1/holder-records/"+r.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Identity document"}, "id.pdf",
		[]byte("%PDF-1.7 matrix id"), "If-Match", r.Header.Get("ETag"))
	var account string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		for _, a := range all {
			if a.Role == storage.RoleShareCapital && a.Active && !a.Group {
				account = a.ID.String()
			}
		}
		return err
	})
	if account == "" {
		account = e.shareCapitalIn(e.books.Org)
	}
	c := e.call(http.MethodPost, "/v1/share-classes", acc, map[string]any{"name": "Matrix ordinary", "kind": "ordinary", "account_id": account,
		"votes_per_share": "1", "liquidation": map[string]any{}, "dividend": map[string]any{"kind": "none"}, "anti_dilution": "none"})
	cf := e.upload("/v1/share-classes/"+c.str("version_id")+"/evidence", acc, map[string]string{"evidence_type": "Constitution"}, "constitution.pdf",
		[]byte("%PDF-1.7 matrix constitution"), "If-Match", c.Header.Get("ETag"))
	if r.Status != http.StatusCreated || rf.Status != http.StatusCreated || c.Status != http.StatusCreated || cf.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's equity records: %d %d %d %d %s %s", r.Status, rf.Status, c.Status, cf.Status, r.Raw, c.Raw)
	}
	f := equityFixture{holder: h.str("id"), record: r.str("id"), recordFile: rf.str("id"), class: c.str("version_id"), classFile: cf.str("id")}
	matrixEquityOf[e] = f
	return f
}

// matrixEquityState is what a refused call must leave as it was: the records, their files and the lists.
func matrixEquityState(e *env, f equityFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/holder-records/" + f.record, "/v1/holder-records/" + f.record + "/evidence", "/v1/share-classes/" + f.class,
		"/v1/share-classes/" + f.class + "/evidence", "/v1/holders", "/v1/share-classes"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

// matrixEquityRecords are the authorisation matrix's rows for holders and share classes (FR-EQ-15, FR-EQ-16): writes as
// previews, so a refused one shows it changed nothing.
var matrixEquityRecords = map[string]func(e *env, tok string) (response, func() bool){
	"listHolders": func(e *env, tok string) (response, func() bool) {
		matrixEquity(e)
		return e.call(http.MethodGet, "/v1/holders", tok, nil), nil
	},
	"getHolder": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/holders/"+matrixEquity(e).holder, tok, nil), nil
	},
	"createHolderRecord": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.call(http.MethodPost, "/v1/holder-records", tok, map[string]any{"contact_id": f.holder, "kind": "individual"}, "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"getHolderRecord": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/holder-records/"+matrixEquity(e).record, tok, nil), nil
	},
	"replaceHolderRecord": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.call(http.MethodPut, "/v1/holder-records/"+f.record, tok, map[string]any{"kind": "individual", "id_type": "fin", "id_last4": "999Z"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"requestHolderRecordApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.call(http.MethodPost, "/v1/holder-records/"+f.record+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"addHolderRecordEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.upload("/v1/holder-records/"+f.record+"/evidence", tok, map[string]string{"evidence_type": "Identity document"}, "more.pdf",
			[]byte("%PDF-1.7 more"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"listHolderRecordEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/holder-records/"+matrixEquity(e).record+"/evidence", tok, nil), nil
	},
	"getHolderRecordEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		return e.call(http.MethodGet, "/v1/holder-records/"+f.record+"/evidence/"+f.recordFile, tok, nil), nil
	},
	"removeHolderRecordEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.call(http.MethodDelete, "/v1/holder-records/"+f.record+"/evidence/"+f.recordFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"listShareClasses": func(e *env, tok string) (response, func() bool) {
		matrixEquity(e)
		return e.call(http.MethodGet, "/v1/share-classes", tok, nil), nil
	},
	"getShareClass": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/share-classes/"+matrixEquity(e).class, tok, nil), nil
	},
	"createShareClass": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		cur := e.call(http.MethodGet, "/v1/share-classes/"+f.class, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/share-classes", tok, map[string]any{"name": "Matrix B", "kind": "ordinary", "account_id": cur.str("account_id"),
			"votes_per_share": "1", "liquidation": map[string]any{}, "dividend": map[string]any{"kind": "none"}, "anti_dilution": "none"}, "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"replaceShareClass": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		cur := e.call(http.MethodGet, "/v1/share-classes/"+f.class, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/share-classes/"+f.class, tok, map[string]any{"name": "Matrix ordinary", "kind": "ordinary", "account_id": cur.str("account_id"),
			"votes_per_share": "2", "liquidation": map[string]any{}, "dividend": map[string]any{"kind": "none"}, "anti_dilution": "none"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"requestShareClassApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.call(http.MethodPost, "/v1/share-classes/"+f.class+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"addShareClassEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.upload("/v1/share-classes/"+f.class+"/evidence", tok, map[string]string{"evidence_type": "Shareholders' agreement"}, "sha.pdf",
			[]byte("%PDF-1.7 sha"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
	"listShareClassEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/share-classes/"+matrixEquity(e).class+"/evidence", tok, nil), nil
	},
	"getShareClassEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		return e.call(http.MethodGet, "/v1/share-classes/"+f.class+"/evidence/"+f.classFile, tok, nil), nil
	},
	"removeShareClassEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixEquity(e)
		was := matrixEquityState(e, f)
		r := e.call(http.MethodDelete, "/v1/share-classes/"+f.class+"/evidence/"+f.classFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixEquityState(e, f) == was }
	},
}
