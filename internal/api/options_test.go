//go:build local

package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The option pool and its grants (FR-EQ-19, design D9; p1-equity-register slice 5a): a resolution of 2 Jan 2025 sets
// aside 100,000 shares; the Accountant drafts it, every problem of a bad draft at once, and it is recorded only with the
// resolution attached on another person's approval. Priya Nair's grant of 48,000 options at S$0.10 on 15 Oct 2025, a
// 12-month cliff and 48 months' vesting, is recorded the same way, never by Priya, who signs in as Mei: on 15 Oct 2026
// 12,000 have vested. A grant beyond what the pool leaves on its grant date is refused when drafted, and again when
// approved if another grant took the room meanwhile. The cap table counts the grants and the pool fully diluted. Mei, as
// an Employee, reads her own grants and nobody else's.
// req: FR-EQ-19
// req: FR-EQ-20
// req: FM-1
// asvs: V8.2.1
func TestTheOptionPool(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	attach := func(path, etag, kind string) {
		t.Helper()
		if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": kind}, "file.pdf", []byte("%PDF-1.7 "+kind), "If-Match", etag); f.Status != http.StatusCreated {
			t.Fatalf("attaching the %s: %d %s", kind, f.Status, f.Raw)
		}
	}
	priya := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Priya Nair", "investor": true}).str("id")
	ct := e.call(http.MethodGet, "/v1/contacts/"+priya, weiling, nil)
	if r := approve(e.call(http.MethodPut, "/v1/contacts/"+priya+"/member", kow, map[string]any{"member_user_id": c.mei}, "If-Match", ct.Header.Get("ETag")), weiling); r.Status != http.StatusOK {
		t.Fatalf("linking Priya, an investor, to Mei: %d %s", r.Status, r.Raw)
	}
	ordinary := e.shareCapitalIn(e.books.Org)
	grant := func(holder, number string) map[string]any {
		return map[string]any{"holder_id": holder, "number": number, "exercise_price": "0.10", "granted_on": "2025-10-15", "cliff_months": 12,
			"vesting_months": 48, "acceleration": "double", "expires_on": "2035-10-15", "class_account_id": ordinary}
	}
	r := e.call(http.MethodPost, "/v1/option-grants", prep, grant(priya, "48000"))
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "no recorded resolution sets an option pool") {
		t.Errorf("a grant with no pool: %d %s", r.Status, r.Raw)
	}

	bad := map[string]any{"size": "0", "class_account_id": e.books.Sales.ID.String(), "resolution_ref": " ", "resolved_on": "2026-12-01"}
	r = e.call(http.MethodPost, "/v1/option-pool/resolutions", prep, bad)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodePool) {
		t.Fatalf("a bad resolution: %d %s", r.Status, r.Raw)
	}
	for _, f := range []string{"body.size", "body.class_account_id", "body.resolution_ref", "body.resolved_on"} {
		if !strings.Contains(r.Raw, `"field":"`+f+`"`) {
			t.Errorf("a bad resolution names no %s: %s", f, r.Raw)
		}
	}
	pool := map[string]any{"size": "100000", "class_account_id": ordinary, "resolution_ref": "Ordinary resolution 2 Jan 2025", "resolved_on": "2025-01-02"}
	d := e.call(http.MethodPost, "/v1/option-pool/resolutions", prep, pool)
	if d.Status != http.StatusCreated || d.str("status") != "draft" || d.str("class_code") != "3010" || d.Body["in_force"] != false ||
		!strings.Contains(d.Raw, "Attach the resolution") {
		t.Fatalf("the pool drafted: %d %s", d.Status, d.Raw)
	}
	path := "/v1/option-pool/resolutions/" + d.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", d.Header.Get("ETag")), "requestPoolResolutionApproval", 422, equity.CodeNoEvidence)
	attach(path, d.Header.Get("ETag"), "Resolution")
	req := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", d.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectOptionPool || !strings.Contains(req.Raw, "100000 shares") {
		t.Fatalf("asking for the pool: %d %s", req.Status, req.Raw)
	}
	if a := approve(req, mei); a.Status != http.StatusOK {
		t.Fatalf("approving the pool: %d %s", a.Status, a.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, prep, pool, "If-Match", `"1-posted"`), "replacePoolResolution", 409, equity.CodeRecorded)
	p := e.call(http.MethodGet, "/v1/option-pool", e.as(authz.Auditor), nil)
	if p.Status != http.StatusOK || p.str("size") != "100000" || p.str("granted") != "0" || p.str("unallocated") != "100000" || p.str("class_code") != "3010" ||
		!strings.Contains(p.Raw, `"in_force":true`) || !strings.Contains(p.Raw, `"status":"recorded"`) {
		t.Fatalf("the pool, as the Auditor reads it: %d %s", p.Status, p.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/option-pool?at=2024-12-31", weiling, nil); p.str("size") != "0" || p.Body["class_code"] != nil {
		t.Errorf("the pool before its resolution: %s", p.Raw)
	}

	// Priya's grant: every problem of a bad one at once, then drafted, and never approved by Mei, who signs in as her.
	badGrant := grant(priya, "200000")
	badGrant["cliff_months"], badGrant["expires_on"], badGrant["class_account_id"] = 60, "2025-10-15", e.books.Sales.ID.String()
	r = e.call(http.MethodPost, "/v1/option-grants", prep, badGrant)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, equity.CodeGrant) {
		t.Fatalf("a bad grant: %d %s", r.Status, r.Raw)
	}
	for _, f := range []string{"body.number", "body.cliff_months", "body.expires_on", "body.class_account_id"} {
		if !strings.Contains(r.Raw, `"field":"`+f+`"`) {
			t.Errorf("a bad grant names no %s: %s", f, r.Raw)
		}
	}
	customer := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Northwind Analytics", "customer": true}).str("id")
	if r := e.call(http.MethodPost, "/v1/option-grants", prep, grant(customer, "100")); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, `"field":"body.holder_id"`) {
		t.Errorf("a customer as holder: %d %s", r.Status, r.Raw)
	}
	g := e.call(http.MethodPost, "/v1/option-grants", prep, grant(priya, "48000"))
	if g.Status != http.StatusCreated || g.str("status") != "draft" || g.str("holder_name") != "Priya Nair" || g.str("vested") != "12000" ||
		g.str("outstanding") != "48000" || g.str("currency") != "SGD" || g.str("class_code") != "3010" {
		t.Fatalf("Priya's grant drafted: %d %s", g.Status, g.Raw)
	}
	gpath := "/v1/option-grants/" + g.str("id")
	attach(gpath, g.Header.Get("ETag"), "Grant letter")
	greq := e.call(http.MethodPost, gpath+"/approval", prep, nil, "If-Match", g.Header.Get("ETag"))
	if greq.Status != http.StatusAccepted || greq.str("object_type") != approval.ObjectOptionGrant || !strings.Contains(greq.Raw, `"after":"0.10 SGD"`) {
		t.Fatalf("asking for Priya's grant: %d %s", greq.Status, greq.Raw)
	}
	if a := approve(greq, mei); a.Status == http.StatusOK || !strings.Contains(a.Raw, "your own grant") {
		t.Errorf("Mei approving her own grant: %d %s", a.Status, a.Raw)
	}
	if a := approve(greq, kow); a.Status != http.StatusOK {
		t.Fatalf("approving Priya's grant: %d %s", a.Status, a.Raw)
	}
	got := e.call(http.MethodGet, gpath, e.as(authz.Auditor), nil)
	if got.Status != http.StatusOK || got.str("status") != "recorded" || got.Body["recorded_by"] == nil || got.Header.Get("ETag") != `"1-posted"` {
		t.Fatalf("recorded, as the Auditor reads it: %d %s", got.Status, got.Raw)
	}
	e.problemIs(e.call(http.MethodPut, gpath, prep, grant(priya, "48000"), "If-Match", `"1-posted"`), "replaceOptionGrant", 409, equity.CodeRecorded)
	if p := e.call(http.MethodGet, "/v1/option-pool", weiling, nil); p.str("granted") != "48000" || p.str("unallocated") != "52000" {
		t.Errorf("the pool after Priya's grant: %s", p.Raw)
	}

	// The room the pool leaves: 60,000 is refused when drafted; two of 30,000 each fit then, and the second is refused when approved.
	fund := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Lion Rock Ventures Pte. Ltd.", "investor": true}).str("id")
	if r := e.call(http.MethodPost, "/v1/option-grants", prep, grant(fund, "60000")); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "the pool leaves 52000 options") {
		t.Errorf("a grant beyond the pool: %d %s", r.Status, r.Raw)
	}
	asked := make([]response, 2)
	for i := range asked {
		x := e.call(http.MethodPost, "/v1/option-grants", prep, grant(fund, "30000"))
		if x.Status != http.StatusCreated {
			t.Fatalf("grant %d of 30000: %d %s", i, x.Status, x.Raw)
		}
		attach("/v1/option-grants/"+x.str("id"), x.Header.Get("ETag"), "Grant letter")
		asked[i] = e.call(http.MethodPost, "/v1/option-grants/"+x.str("id")+"/approval", prep, nil, "If-Match", x.Header.Get("ETag"))
	}
	if a := approve(asked[0], mei); a.Status != http.StatusOK {
		t.Fatalf("the first 30000: %d %s", a.Status, a.Raw)
	}
	if a := approve(asked[1], mei); a.Status != http.StatusUnprocessableEntity || !strings.Contains(a.Raw, "the pool leaves 22000 options") {
		t.Errorf("the second 30000, once the first took the room: %d %s", a.Status, a.Raw)
	}
	// Every day after counts too: a grant of 30,000 on 1 Jun 2025 fits then, but not with October's 78,000; nor does a
	// resolution of 1 Mar 2025 cutting the pool to 50,000.
	early := grant(fund, "30000")
	early["granted_on"] = "2025-06-01"
	if r := e.call(http.MethodPost, "/v1/option-grants", prep, early); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, "the 108000 options granted by 2025-10-15 are more than the pool of 100000 then") {
		t.Errorf("an earlier grant that shorts October: %d %s", r.Status, r.Raw)
	}
	smaller := map[string]any{"size": "50000", "class_account_id": ordinary, "resolution_ref": "Ordinary resolution 1 Mar 2025", "resolved_on": "2025-03-01"}
	if r := e.call(http.MethodPost, "/v1/option-pool/resolutions", prep, smaller); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, "the 78000 options granted by 2025-10-15 are more than the 50000 shares it sets aside") {
		t.Errorf("a resolution smaller than the grants: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/option-grants", weiling, nil); l.Status != http.StatusOK || len(l.Body["items"].([]any)) != 3 {
		t.Errorf("the grants: %d %s", l.Status, l.Raw)
	}

	// The cap table fully diluted (slice 5a-ii): no share is issued yet, so the 78,000 options granted and the 22,000
	// the pool leaves are the whole company fully diluted; Priya's line has her 48,000 options and no shares.
	ct = e.call(http.MethodGet, "/v1/cap-table?at=2026-10-15", weiling, nil)
	if ct.Status != http.StatusOK || ct.str("issued") != "0" || ct.str("options") != "78000" || ct.str("unallocated_pool") != "22000" ||
		ct.str("fully_diluted") != "100000" || ct.str("pool_diluted") != "22.00" ||
		!strings.Contains(ct.Raw, `"holder_name":"Priya Nair","class_name":"Ordinary shares","account_code":"3010","issued":"0","votes":"0","fully_diluted":"48000","options":"48000","diluted":"48.00"`) {
		t.Errorf("the cap table with the pool: %d %s", ct.Status, ct.Raw)
	}
	csv := e.call(http.MethodGet, "/v1/cap-table/export?at=2026-10-15&format=csv", weiling, nil)
	if rows, _ := base64.StdEncoding.DecodeString(csv.str("content_base64")); !strings.Contains(string(rows), `"Unallocated option pool","","",0,"",0,"",22000,22.00`) {
		t.Errorf("the cap table's CSV has no pool row: %s", rows)
	}

	// Mei as an Employee: her own grants, and nobody else's; a caller who signs in as no holder holds none.
	employee := c.as(c.mei, authz.Employee)
	mine := e.call(http.MethodGet, "/v1/me/option-grants", employee, nil)
	if mine.Status != http.StatusOK || mine.Body["linked"] != true || len(mine.Body["grants"].([]any)) != 1 || !strings.Contains(mine.Raw, `"vested":"12000"`) {
		t.Errorf("Mei's own grants: %d %s", mine.Status, mine.Raw)
	}
	if none := e.call(http.MethodGet, "/v1/me/option-grants", e.as(authz.Auditor), nil); none.Status != http.StatusOK || none.Body["linked"] != false ||
		len(none.Body["grants"].([]any)) != 0 {
		t.Errorf("a caller who holds no grant: %d %s", none.Status, none.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/option-grants", employee, nil), "listOptionGrants", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/option-pool", employee, nil), "getOptionPool", 403, CodeForbidden)
	if trail := auditTrail(t, e); !strings.Contains(trail, "option_grant") || !strings.Contains(trail, "option_pool") {
		t.Error("the pool and the grants were not audited")
	}
}

// optionFixture is the matrix's option pool: a recorded resolution of 1,000 shares, a drafted one with its resolution,
// and a drafted grant with its letter.
type optionFixture struct{ resolution, resolutionFile, grant, grantFile string }

var matrixOptionOf = map[*env]optionFixture{}

func matrixOption(e *env) optionFixture {
	e.t.Helper()
	if f, ok := matrixOptionOf[e]; ok {
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
	pool := map[string]any{"size": "1000", "class_account_id": capital, "resolution_ref": "Matrix resolution", "resolved_on": "2026-01-02"}
	in := e.call(http.MethodPost, "/v1/option-pool/resolutions", acc, pool)
	e.upload("/v1/option-pool/resolutions/"+in.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Resolution"}, "resolution.pdf",
		[]byte("%PDF-1.7 matrix pool in force"), "If-Match", in.Header.Get("ETag"))
	e.approveAsAnother(approval.ObjectOptionPool, in.str("id"), approval.OpOptionPoolRecord, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := equity.RecordIn(ctx, u, storage.DocumentOptionPool, uuid.MustParse(in.str("id")))
		return err
	})
	pool["size"] = "2000"
	d := e.call(http.MethodPost, "/v1/option-pool/resolutions", acc, pool)
	df := e.upload("/v1/option-pool/resolutions/"+d.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Resolution"}, "resolution.pdf",
		[]byte("%PDF-1.7 matrix pool"), "If-Match", d.Header.Get("ETag"))
	holder := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Matrix option holder", "investor": true}).str("id")
	g := e.call(http.MethodPost, "/v1/option-grants", acc, map[string]any{"holder_id": holder, "number": "100", "exercise_price": "0.10",
		"granted_on": "2026-10-01", "cliff_months": 12, "vesting_months": 48, "acceleration": "none", "expires_on": "2036-10-01", "class_account_id": capital})
	gf := e.upload("/v1/option-grants/"+g.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Grant letter"}, "letter.pdf",
		[]byte("%PDF-1.7 matrix grant"), "If-Match", g.Header.Get("ETag"))
	if in.Status != http.StatusCreated || d.Status != http.StatusCreated || df.Status != http.StatusCreated || g.Status != http.StatusCreated || gf.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's option pool: %d %d %d %d %d %s %s", in.Status, d.Status, df.Status, g.Status, gf.Status, d.Raw, g.Raw)
	}
	f := optionFixture{resolution: d.str("id"), resolutionFile: df.str("id"), grant: g.str("id"), grantFile: gf.str("id")}
	matrixOptionOf[e] = f
	return f
}

// matrixOptionState is what a refused call must leave as it was: the pool, the drafts and their files.
func matrixOptionState(e *env, f optionFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/option-pool", "/v1/option-pool/resolutions/" + f.resolution, "/v1/option-pool/resolutions/" + f.resolution + "/evidence",
		"/v1/option-grants", "/v1/option-grants/" + f.grant + "/evidence"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

// bodyOf is a record's fields a draft takes, from its view.
func bodyOf(r response, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := r.Body[k]; ok {
			out[k] = v
		}
	}
	return out
}

var (
	poolKeys  = []string{"size", "class_account_id", "resolution_ref", "resolved_on"}
	grantKeys = []string{"holder_id", "number", "exercise_price", "granted_on", "cliff_months", "vesting_months", "acceleration", "expires_on", "class_account_id"}
)

// matrixOptions are the authorisation matrix's rows for the option pool and its grants (FR-EQ-19): writes as previews,
// so a refused one shows it changed nothing.
var matrixOptions = map[string]func(e *env, tok string) (response, func() bool){
	"getOptionPool": func(e *env, tok string) (response, func() bool) {
		matrixOption(e)
		return e.call(http.MethodGet, "/v1/option-pool", tok, nil), nil
	},
	"createPoolResolution": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		cur := e.call(http.MethodGet, "/v1/option-pool/resolutions/"+f.resolution, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/option-pool/resolutions", tok, bodyOf(cur, poolKeys...), "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"getPoolResolution": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/option-pool/resolutions/"+matrixOption(e).resolution, tok, nil), nil
	},
	"replacePoolResolution": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		cur := e.call(http.MethodGet, "/v1/option-pool/resolutions/"+f.resolution, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/option-pool/resolutions/"+f.resolution, tok, bodyOf(cur, poolKeys...), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"requestPoolResolutionApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		r := e.call(http.MethodPost, "/v1/option-pool/resolutions/"+f.resolution+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"addPoolResolutionEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		r := e.upload("/v1/option-pool/resolutions/"+f.resolution+"/evidence", tok, map[string]string{"evidence_type": "Plan rules"}, "rules.pdf",
			[]byte("%PDF-1.7 plan rules"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"listPoolResolutionEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/option-pool/resolutions/"+matrixOption(e).resolution+"/evidence", tok, nil), nil
	},
	"getPoolResolutionEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		return e.call(http.MethodGet, "/v1/option-pool/resolutions/"+f.resolution+"/evidence/"+f.resolutionFile, tok, nil), nil
	},
	"removePoolResolutionEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		r := e.call(http.MethodDelete, "/v1/option-pool/resolutions/"+f.resolution+"/evidence/"+f.resolutionFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"listOptionGrants": func(e *env, tok string) (response, func() bool) {
		matrixOption(e)
		return e.call(http.MethodGet, "/v1/option-grants", tok, nil), nil
	},
	"createOptionGrant": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		cur := e.call(http.MethodGet, "/v1/option-grants/"+f.grant, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPost, "/v1/option-grants", tok, bodyOf(cur, grantKeys...), "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"getOptionGrant": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/option-grants/"+matrixOption(e).grant, tok, nil), nil
	},
	"replaceOptionGrant": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		cur := e.call(http.MethodGet, "/v1/option-grants/"+f.grant, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/option-grants/"+f.grant, tok, bodyOf(cur, grantKeys...), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"requestOptionGrantApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		r := e.call(http.MethodPost, "/v1/option-grants/"+f.grant+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"addOptionGrantEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		r := e.upload("/v1/option-grants/"+f.grant+"/evidence", tok, map[string]string{"evidence_type": "Signed acceptance"}, "acceptance.pdf",
			[]byte("%PDF-1.7 acceptance"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"listOptionGrantEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/option-grants/"+matrixOption(e).grant+"/evidence", tok, nil), nil
	},
	"getOptionGrantEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		return e.call(http.MethodGet, "/v1/option-grants/"+f.grant+"/evidence/"+f.grantFile, tok, nil), nil
	},
	"removeOptionGrantEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOption(e)
		was := matrixOptionState(e, f)
		r := e.call(http.MethodDelete, "/v1/option-grants/"+f.grant+"/evidence/"+f.grantFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionState(e, f) == was }
	},
	"getMyOptionGrants": func(e *env, tok string) (response, func() bool) {
		matrixOption(e)
		return e.call(http.MethodGet, "/v1/me/option-grants", tok, nil), nil
	},
}

// A grant's forfeiture and expiry (FR-EQ-19, design D9; slice 5b-i): Priya's 48,000 options from 15 Oct 2025, 12,000
// vested on 15 Oct 2026, the day she leaves. The 36,000 not vested are forfeited, never 36,001; then the 12,000 vested
// lapse. Each is recorded on another person's approval with its notice, never by Mei, who signs in as Priya, and the
// options are back in the pool from the day: 88,000 unallocated, then 100,000. A grant still a draft has no events.
// req: FR-EQ-19
// req: FM-1
// asvs: V8.2.1
func TestAGrantsOptionsAreForfeitedAndLapse(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	recorded := func(path string, created response, kind string) response {
		t.Helper()
		if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": kind}, "file.pdf", []byte("%PDF-1.7 "+kind), "If-Match", created.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("attaching the %s: %d %s", kind, f.Status, f.Raw)
		}
		return e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", created.Header.Get("ETag"))
	}
	priya := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Priya Nair", "investor": true}).str("id")
	ct := e.call(http.MethodGet, "/v1/contacts/"+priya, weiling, nil)
	if r := approve(e.call(http.MethodPut, "/v1/contacts/"+priya+"/member", kow, map[string]any{"member_user_id": c.mei}, "If-Match", ct.Header.Get("ETag")), weiling); r.Status != http.StatusOK {
		t.Fatalf("linking Priya to Mei: %d %s", r.Status, r.Raw)
	}
	ordinary := e.shareCapitalIn(e.books.Org)
	pool := e.call(http.MethodPost, "/v1/option-pool/resolutions", prep, map[string]any{"size": "100000", "class_account_id": ordinary,
		"resolution_ref": "Ordinary resolution 2 Jan 2025", "resolved_on": "2025-01-02"})
	if a := approve(recorded("/v1/option-pool/resolutions/"+pool.str("id"), pool, "Resolution"), mei); a.Status != http.StatusOK {
		t.Fatalf("the pool: %d %s", a.Status, a.Raw)
	}
	terms := map[string]any{"holder_id": priya, "number": "48000", "exercise_price": "0.10", "granted_on": "2025-10-15", "cliff_months": 12,
		"vesting_months": 48, "acceleration": "none", "expires_on": "2035-10-15", "class_account_id": ordinary}
	draft := e.call(http.MethodPost, "/v1/option-grants", prep, terms)
	if r := e.call(http.MethodPost, "/v1/option-grants/"+draft.str("id")+"/events", prep, map[string]any{"kind": "expiry", "number": "1", "on": "2026-10-15"},
		"If-Match", draft.Header.Get("ETag")); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "the grant is not recorded") {
		t.Errorf("an event of a drafted grant: %d %s", r.Status, r.Raw)
	}
	g := draft
	gpath := "/v1/option-grants/" + g.str("id")
	if a := approve(recorded(gpath, g, "Grant letter"), kow); a.Status != http.StatusOK {
		t.Fatalf("Priya's grant: %d %s", a.Status, a.Raw)
	}
	grantTag := e.call(http.MethodGet, gpath, weiling, nil).Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, gpath+"/events", prep, map[string]any{"kind": "forfeiture", "number": "36000", "on": "2026-10-15"}),
		"createOptionEvent", 428, CodeIfMatchRequired)
	if r := e.call(http.MethodPost, gpath+"/events", prep, map[string]any{"kind": "forfeiture", "number": "36001", "on": "2026-10-15"}, "If-Match", grantTag); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, "36000 options are not vested on 2026-10-15") {
		t.Errorf("forfeiting a vested option: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, gpath+"/events", prep, map[string]any{"kind": "expiry", "number": "1", "on": "2025-10-14"}, "If-Match", grantTag); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, `"field":"body.on"`) {
		t.Errorf("an event before the grant: %d %s", r.Status, r.Raw)
	}
	f := e.call(http.MethodPost, gpath+"/events", prep, map[string]any{"kind": "forfeiture", "number": "36000", "on": "2026-10-15", "note": "Left on 15 Oct 2026"},
		"If-Match", grantTag)
	if f.Status != http.StatusCreated || f.str("status") != "draft" || f.str("holder_name") != "Priya Nair" || !strings.Contains(f.Raw, "Attach the notice") {
		t.Fatalf("the forfeiture drafted: %d %s", f.Status, f.Raw)
	}
	fpath := "/v1/option-events/" + f.str("id")
	e.problemIs(e.call(http.MethodPost, fpath+"/approval", prep, nil, "If-Match", f.Header.Get("ETag")), "requestOptionEventApproval", 422, equity.CodeNoEvidence)
	freq := recorded(fpath, f, "Leaver's letter")
	if freq.Status != http.StatusAccepted || freq.str("object_type") != approval.ObjectOptionEvent || !strings.Contains(freq.Raw, "Forfeit 36000 of Priya Nair's options") {
		t.Fatalf("asking for the forfeiture: %d %s", freq.Status, freq.Raw)
	}
	if a := approve(freq, mei); a.Status == http.StatusOK || !strings.Contains(a.Raw, "your own options") {
		t.Errorf("Mei approving her own forfeiture: %d %s", a.Status, a.Raw)
	}
	if a := approve(freq, kow); a.Status != http.StatusOK {
		t.Fatalf("approving the forfeiture: %d %s", a.Status, a.Raw)
	}
	e.problemIs(e.call(http.MethodPut, fpath, prep, map[string]any{"kind": "forfeiture", "number": "1", "on": "2026-10-15"}, "If-Match", `"1-posted"`),
		"replaceOptionEvent", 409, equity.CodeRecorded)
	got := e.call(http.MethodGet, gpath, e.as(authz.Auditor), nil)
	if got.str("forfeited") != "36000" || got.str("outstanding") != "12000" || got.str("vested") != "12000" || len(got.Body["events"].([]any)) != 1 {
		t.Errorf("the grant after its forfeiture: %s", got.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/option-pool", weiling, nil); p.str("returned") != "36000" || p.str("unallocated") != "88000" {
		t.Errorf("the pool after the forfeiture: %s", p.Raw)
	}

	// The vested 12,000 lapse: never 12,001, which the grant does not have.
	if r := e.call(http.MethodPost, gpath+"/events", prep, map[string]any{"kind": "expiry", "number": "12001", "on": "2026-10-15"}, "If-Match", grantTag); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, "leave 12000 options") {
		t.Errorf("an expiry of more than is left: %d %s", r.Status, r.Raw)
	}
	x := e.call(http.MethodPost, gpath+"/events", prep, map[string]any{"kind": "expiry", "number": "12000", "on": "2026-10-15"}, "If-Match", grantTag)
	if a := approve(recorded("/v1/option-events/"+x.str("id"), x, "Notice of lapse"), kow); a.Status != http.StatusOK {
		t.Fatalf("the expiry: %d %s", a.Status, a.Raw)
	}
	if got := e.call(http.MethodGet, gpath, weiling, nil); got.str("expired") != "12000" || got.str("outstanding") != "0" || got.str("vested") != "0" {
		t.Errorf("the grant after its expiry: %s", got.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/option-pool", weiling, nil); p.str("unallocated") != "100000" {
		t.Errorf("the pool after the expiry: %s", p.Raw)
	}
	if ct := e.call(http.MethodGet, "/v1/cap-table?at=2026-10-15", weiling, nil); ct.str("options") != "0" || ct.str("unallocated_pool") != "100000" ||
		strings.Contains(ct.Raw, "Priya Nair") {
		t.Errorf("the cap table once the options are gone: %s", ct.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "option_event") {
		t.Error("the events were not audited")
	}
}

// optionEventFixture is the matrix's grant events: a recorded grant and a drafted forfeiture of it with its notice.
type optionEventFixture struct{ grant, event, eventFile string }

var matrixOptionEventOf = map[*env]optionEventFixture{}

func matrixOptionEvent(e *env) optionEventFixture {
	e.t.Helper()
	if f, ok := matrixOptionEventOf[e]; ok {
		return f
	}
	matrixOption(e) // its recorded pool of 1,000 from 2 Jan 2026
	acc := e.as(authz.Accountant)
	cur := e.call(http.MethodGet, "/v1/option-grants/"+matrixOption(e).grant, acc, nil)
	terms := bodyOf(cur, grantKeys...)
	terms["number"] = "200"
	g := e.call(http.MethodPost, "/v1/option-grants", acc, terms)
	e.upload("/v1/option-grants/"+g.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Grant letter"}, "letter.pdf",
		[]byte("%PDF-1.7 matrix recorded grant"), "If-Match", g.Header.Get("ETag"))
	e.approveAsAnother(approval.ObjectOptionGrant, g.str("id"), approval.OpOptionGrantRecord, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := equity.RecordIn(ctx, u, storage.DocumentOptionGrant, uuid.MustParse(g.str("id")))
		return err
	})
	ev := e.call(http.MethodPost, "/v1/option-grants/"+g.str("id")+"/events", acc, map[string]any{"kind": "expiry", "number": "10", "on": "2026-10-15"},
		"If-Match", `"1-posted"`)
	ef := e.upload("/v1/option-events/"+ev.str("id")+"/evidence", acc, map[string]string{"evidence_type": "Notice of lapse"}, "notice.pdf",
		[]byte("%PDF-1.7 matrix notice"), "If-Match", ev.Header.Get("ETag"))
	if g.Status != http.StatusCreated || ev.Status != http.StatusCreated || ef.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's grant events: %d %d %d %s %s", g.Status, ev.Status, ef.Status, g.Raw, ev.Raw)
	}
	f := optionEventFixture{grant: g.str("id"), event: ev.str("id"), eventFile: ef.str("id")}
	matrixOptionEventOf[e] = f
	return f
}

// matrixOptionEventState is what a refused call must leave as it was: the grant, its event and the event's files.
func matrixOptionEventState(e *env, f optionEventFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/option-grants/" + f.grant, "/v1/option-events/" + f.event, "/v1/option-events/" + f.event + "/evidence"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

var eventKeys = []string{"kind", "number", "on"}

// matrixOptionEvents are the authorisation matrix's rows for a grant's forfeitures and expiries (FR-EQ-19).
var matrixOptionEvents = map[string]func(e *env, tok string) (response, func() bool){
	"createOptionEvent": func(e *env, tok string) (response, func() bool) {
		f := matrixOptionEvent(e)
		was := matrixOptionEventState(e, f)
		r := e.call(http.MethodPost, "/v1/option-grants/"+f.grant+"/events", tok, map[string]any{"kind": "expiry", "number": "5", "on": "2026-10-15"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionEventState(e, f) == was }
	},
	"getOptionEvent": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/option-events/"+matrixOptionEvent(e).event, tok, nil), nil
	},
	"replaceOptionEvent": func(e *env, tok string) (response, func() bool) {
		f := matrixOptionEvent(e)
		was := matrixOptionEventState(e, f)
		cur := e.call(http.MethodGet, "/v1/option-events/"+f.event, e.as(authz.Accountant), nil)
		r := e.call(http.MethodPut, "/v1/option-events/"+f.event, tok, bodyOf(cur, eventKeys...), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionEventState(e, f) == was }
	},
	"requestOptionEventApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixOptionEvent(e)
		was := matrixOptionEventState(e, f)
		r := e.call(http.MethodPost, "/v1/option-events/"+f.event+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionEventState(e, f) == was }
	},
	"addOptionEventEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOptionEvent(e)
		was := matrixOptionEventState(e, f)
		r := e.upload("/v1/option-events/"+f.event+"/evidence", tok, map[string]string{"evidence_type": "Board minute"}, "minute.pdf",
			[]byte("%PDF-1.7 minute"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionEventState(e, f) == was }
	},
	"listOptionEventEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/option-events/"+matrixOptionEvent(e).event+"/evidence", tok, nil), nil
	},
	"getOptionEventEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOptionEvent(e)
		return e.call(http.MethodGet, "/v1/option-events/"+f.event+"/evidence/"+f.eventFile, tok, nil), nil
	},
	"removeOptionEventEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixOptionEvent(e)
		was := matrixOptionEventState(e, f)
		r := e.call(http.MethodDelete, "/v1/option-events/"+f.event+"/evidence/"+f.eventFile, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixOptionEventState(e, f) == was }
	},
}

// An exercise of options (FR-EQ-19, design D9; slice 5b-ii): Priya's 48,000 options at S$0.10 from 15 Oct 2025, 12,000
// vested on 15 Oct 2026. An exercise of 12,001 is refused; one of 10,000 is an issue to her of 3010 at S$0.10, S$1,000.00,
// approved by another person, never Mei, who signs in as her (FM-9), and made effective only once her S$1,000.00 is in
// share application monies: then Dr 2040 · Cr 3010, her 10,000 shares, and 38,000 options outstanding, 2,000
// exercisable; fully diluted she still counts 48,000. Its reversal makes the 10,000 outstanding again. A grant at no
// exercise price is not exercised for cash.
// req: FR-EQ-19
// req: FM-9
// asvs: V8.2.1
func TestAGrantsOptionsAreExercised(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	asked := func(path string, created response, kind string) response {
		t.Helper()
		if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": kind}, "file.pdf", []byte("%PDF-1.7 "+kind), "If-Match", created.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("attaching the %s: %d %s", kind, f.Status, f.Raw)
		}
		return e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", created.Header.Get("ETag"))
	}
	priya := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Priya Nair", "investor": true}).str("id")
	ct := e.call(http.MethodGet, "/v1/contacts/"+priya, weiling, nil)
	if r := approve(e.call(http.MethodPut, "/v1/contacts/"+priya+"/member", kow, map[string]any{"member_user_id": c.mei}, "If-Match", ct.Header.Get("ETag")), weiling); r.Status != http.StatusOK {
		t.Fatalf("linking Priya to Mei: %d %s", r.Status, r.Raw)
	}
	ordinary := e.shareCapitalIn(e.books.Org)
	var monies string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2040", Name: "Share application monies", Type: storage.Liability, Role: "share-application-monies"})
		monies = m.ID.String()
		return err
	})
	pool := e.call(http.MethodPost, "/v1/option-pool/resolutions", prep, map[string]any{"size": "100000", "class_account_id": ordinary,
		"resolution_ref": "Ordinary resolution 2 Jan 2025", "resolved_on": "2025-01-02"})
	if a := approve(asked("/v1/option-pool/resolutions/"+pool.str("id"), pool, "Resolution"), mei); a.Status != http.StatusOK {
		t.Fatalf("the pool: %d %s", a.Status, a.Raw)
	}
	grantOf := func(price, number string) string {
		t.Helper()
		g := e.call(http.MethodPost, "/v1/option-grants", prep, map[string]any{"holder_id": priya, "number": number, "exercise_price": price,
			"granted_on": "2025-10-15", "cliff_months": 12, "vesting_months": 48, "acceleration": "none", "expires_on": "2035-10-15", "class_account_id": ordinary})
		if a := approve(asked("/v1/option-grants/"+g.str("id"), g, "Grant letter"), kow); a.Status != http.StatusOK {
			t.Fatalf("a grant at %s: %d %s", price, a.Status, a.Raw)
		}
		return g.str("id")
	}
	grant := grantOf("0.10", "48000")
	free := grantOf("0", "4800")
	exercise := func(g, shares string, extra map[string]any) response {
		body := map[string]any{"kind": "exercise", "grant_id": g, "shares": shares, "resolution_refs": "Board resolution 15 Oct 2026"}
		for k, v := range extra {
			body[k] = v
		}
		return e.call(http.MethodPost, "/v1/equity-transactions", prep, body)
	}
	if r := exercise(grant, "12001", nil); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "12000 options are vested and not exercised on 2026-10-15") {
		t.Errorf("exercising one option not vested: %d %s", r.Status, r.Raw)
	}
	if r := exercise(grant, "100", map[string]any{"price": "0.20", "holder_id": c.ahkow}); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, `"field":"body.price"`) || !strings.Contains(r.Raw, `"field":"body.holder_id"`) {
		t.Errorf("another price and holder: %d %s", r.Status, r.Raw)
	}
	if r := exercise(free, "100", nil); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "exercise price is 0") {
		t.Errorf("a grant at no price: %d %s", r.Status, r.Raw)
	}
	x := exercise(grant, "10000", nil)
	if x.Status != http.StatusCreated || !strings.Contains(x.Raw, `"price":"0.10"`) || !strings.Contains(x.Raw, `"holder_id":"`+priya+`"`) ||
		!strings.Contains(x.Raw, `"amount":"1000.00"`) {
		t.Fatalf("the exercise drafted: %d %s", x.Status, x.Raw)
	}
	path := "/v1/equity-transactions/" + x.str("id")
	req := asked(path, x, "Exercise notice")
	if req.Status != http.StatusAccepted || !strings.Contains(req.Raw, "Issue 10000 shares to Priya Nair (subscriber) on an exercise of options") {
		t.Fatalf("asking for the exercise: %d %s", req.Status, req.Raw)
	}
	if a := approve(req, mei); a.Status == http.StatusOK {
		t.Errorf("Mei approved her own exercise: %s", a.Raw)
	}
	if a := approve(req, kow); a.Status != http.StatusOK {
		t.Fatalf("approving the exercise: %d %s", a.Status, a.Raw)
	}
	approved := e.call(http.MethodGet, path, weiling, nil)
	effective := func(tx response) response {
		return e.call(http.MethodPost, "/v1/equity-transactions/"+tx.str("id")+"/effective", weiling,
			map[string]any{"effective_on": "2026-10-15", "lodgement_ref": "AA2026101500789"}, "If-Match", tx.Header.Get("ETag"))
	}
	e.problemIs(effective(approved), "recordEquityTransactionEffective", 409, equity.CodeEffective) // not paid yet
	if r := e.postFounderJournal("2026-10-14", founderLine(e.books.Bank.ID.String(), "1000.00", "", ""), founderLine(monies, "-1000.00", priya, "")); r.Status != http.StatusOK {
		t.Fatalf("Priya's S$1,000.00 into share application monies: %d %s", r.Status, r.Raw)
	}
	done := effective(approved)
	if done.Status != http.StatusOK || done.str("status") != "effective" || len(done.Body["entries"].([]any)) != 1 {
		t.Fatalf("the exercise made effective: %d %s", done.Status, done.Raw)
	}
	g := e.call(http.MethodGet, "/v1/option-grants/"+grant, weiling, nil)
	if g.str("exercised") != "10000" || g.str("outstanding") != "38000" || g.str("exercisable") != "2000" || g.str("vested") != "12000" {
		t.Errorf("the grant after its exercise: %s", g.Raw)
	}
	// The pool's grants: 48,000 + 4,800 granted, 10,000 exercised; vested and not exercised 2,000 + 1,200 (4,800 × 12 ÷ 48).
	if p := e.call(http.MethodGet, "/v1/option-pool?at=2026-10-15", weiling, nil); p.str("exercised") != "10000" || p.str("outstanding") != "42800" ||
		p.str("exercisable") != "3200" || p.str("forfeited") != "0" || p.str("unallocated") != "47200" {
		t.Errorf("the pool's grants once 10,000 are exercised: %s", p.Raw)
	}
	cap := e.call(http.MethodGet, "/v1/cap-table?at=2026-10-15", weiling, nil)
	if !strings.Contains(cap.Raw, `"holder_name":"Priya Nair","class_name":"Ordinary shares","account_code":"3010","issued":"10000","votes":"10000","fully_diluted":"52800","options":"42800"`) {
		t.Errorf("Priya's line: 10,000 shares and the 38,000 + 4,800 options outstanding: %s", cap.Raw)
	}

	// The exercise reversed: its 10,000 options outstanding again.
	rev := e.call(http.MethodPost, "/v1/equity-transactions", prep, map[string]any{"kind": "reversal", "reverses_id": x.str("id"),
		"reason": "The exercise notice was withdrawn"})
	rreq := asked("/v1/equity-transactions/"+rev.str("id"), rev, "Withdrawal")
	if a := approve(rreq, kow); a.Status != http.StatusOK {
		t.Fatalf("approving the reversal: %d %s", a.Status, a.Raw)
	}
	if r := effective(e.call(http.MethodGet, "/v1/equity-transactions/"+rev.str("id"), weiling, nil)); r.Status != http.StatusOK {
		t.Fatalf("the reversal made effective: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/option-grants/"+grant, weiling, nil); g.str("exercised") != "0" || g.str("outstanding") != "48000" || g.str("exercisable") != "12000" {
		t.Errorf("the grant once its exercise is reversed: %s", g.Raw)
	}
	if g.str("next_vests_on") != "2026-11-15" || g.str("next_vests") != "1000" {
		t.Errorf("the grant's next vesting: %s", g.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/option-pool?at=2026-10-15", weiling, nil); p.str("exercised") != "0" || p.str("outstanding") != "52800" ||
		p.str("exercisable") != "13200" {
		t.Errorf("the pool's grants once the exercise is reversed: %s", p.Raw)
	}
}
