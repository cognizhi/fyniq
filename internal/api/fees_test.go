//go:build local

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// feesChart gives e's chart 6050 Directors' fees, and returns its ID.
func (e *env) feesChart() string {
	e.t.Helper()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6050", Name: "Directors' fees", Type: storage.Expense})
		id = a.ID.String()
		return err
	})
	return id
}

// FR-EQ-04 (PRD-001 rev 0.167; design D8, FM-9), worked by hand: the shareholders vote Ah Kow, a director linked to his
// member, a S$6,000.00 fee for 2026 on 15 Oct 2026. Wei Ling, an Owner, records it on 6050 (left out, the default); it
// cannot be sent for approval until the resolution is attached. She asks; she never approves her own request, nor does
// Ah Kow, the director paid. Mei, holding the Owner's approval right, approves it: it posts DF-0001 on 15 Oct, Dr 6050
// 6,000.00, Cr 2120 6,000.00 naming Ah Kow, a fee on his statement; posted, it changes no more.
// req: FR-EQ-04
// req: equity/founders-money A director's fee posts to the person (rev 0.167)
func TestDirectorsFees(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	_, deferred := e.directorChart()
	fees := e.feesChart()
	kow := e.directorContact("Ah Kow")
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	ct := e.call(http.MethodGet, "/v1/contacts/"+kow, e.as(authz.Accountant), nil)
	asked := e.call(http.MethodPut, "/v1/contacts/"+kow+"/member", c.as(c.ahkow, authz.Owner, authz.Controller), map[string]any{"member_user_id": c.ahkow},
		"If-Match", ct.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}

	draft := map[string]any{"contact_id": kow, "entitled_on": "2026-10-15", "period_from": "2026-01-01", "period_to": "2026-12-31",
		"amount": map[string]any{"amount": "6000.00", "currency": "SGD"}, "memo": "Directors' fee 2026"}
	e.problemIs(e.call(http.MethodPost, "/v1/director-fees", e.as(authz.Accountant), draft), "createDirectorFee", 403, CodeForbidden)
	r := e.call(http.MethodPost, "/v1/director-fees", weiling, draft)
	if r.Status != http.StatusCreated || r.str("status") != "draft" || r.str("account_id") != fees || !strings.Contains(r.Raw, director.CodeNoVote) {
		t.Fatalf("recording: %d %s", r.Status, r.Raw)
	}
	path := "/v1/director-fees/" + r.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/post-approval", weiling, nil, "If-Match", r.Header.Get("ETag")), "requestDirectorFeePost", 422, director.CodeNoVote)
	if f := e.upload(path+"/evidence", weiling, map[string]string{"evidence_type": "shareholders' resolution"}, "agm.pdf", []byte("%PDF-1.7 agm"),
		"If-Match", r.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the resolution: %d %s", f.Status, f.Raw)
	}
	req := e.call(http.MethodPost, path+"/post-approval", weiling, nil, "If-Match", r.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectDirectorFee {
		t.Fatalf("asking: %d %s", req.Status, req.Raw)
	}
	if w := e.call(http.MethodGet, path, weiling, nil); w.str("status") != "waiting" || w.str("approval_asked") != req.str("id") {
		t.Errorf("waiting: %s", w.Raw)
	}
	approve := func(token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	e.problemIs(approve(weiling), "approveApproval", 403, approval.CodeOwnRequest)
	e.problemIs(approve(c.as(c.ahkow, authz.Owner, authz.Controller)), "approveApproval", 403, director.CodeOwnFee)
	if a := approve(c.as(c.mei, authz.Owner)); a.Status != http.StatusOK {
		t.Fatalf("Mei approving: %d %s", a.Status, a.Raw)
	}

	posted := e.call(http.MethodGet, path, weiling, nil)
	if posted.str("status") != "posted" || posted.str("number") != "DF-0001" {
		t.Fatalf("posted: %s", posted.Raw)
	}
	for _, want := range []string{`"account_id":"` + fees + `","amount":{"amount":"6000.00"`, `"account_id":"` + deferred + `","amount":{"amount":"-6000.00"`,
		`"party_id":"` + kow + `"`} {
		if !strings.Contains(posted.Raw, want) {
			t.Errorf("its ledger impact lacks %s: %s", want, posted.Raw)
		}
	}
	st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-10-01&to=2026-10-31", weiling, nil)
	if !strings.Contains(st.Raw, `"origin":"`+ledger.OriginFee+`"`) {
		t.Errorf("his statement: %s", st.Raw)
	}
	e.problemIs(e.call(http.MethodPut, path, weiling, draft, "If-Match", posted.Header.Get("ETag")), "replaceDirectorFee", 409, director.CodeFeePosted)
}

// matrixFeeOf is the fee the matrix tries, drafted once per env in its organisation for the matrix's director on
// 6050, with its resolution attached: its ID and the file's.
var matrixFeeOf = map[*env][2]string{}

func matrixFee(e *env) [2]string {
	if f, ok := matrixFeeOf[e]; ok {
		return f
	}
	e.directorChart()
	fees := ""
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Accounts().List(ctx)
		for _, a := range all {
			if a.Code == "6050" {
				fees = a.ID.String()
			}
		}
		return err
	})
	if fees == "" {
		fees = e.feesChart()
	}
	owner := e.as(authz.Owner)
	c := e.call(http.MethodPost, "/v1/director-fees", owner, map[string]any{"contact_id": matrixDirector(e), "entitled_on": "2026-10-15",
		"period_from": "2026-01-01", "period_to": "2026-12-31", "amount": map[string]any{"amount": "100.00", "currency": "SGD"}, "account_id": fees})
	if c.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's fee: %d %s", c.Status, c.Raw)
	}
	r := e.upload("/v1/director-fees/"+c.str("id")+"/evidence", owner, map[string]string{"evidence_type": "shareholders' resolution"}, "agm.pdf",
		[]byte("%PDF-1.7 matrix agm"), "If-Match", c.Header.Get("ETag"))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's resolution: %d %s", r.Status, r.Raw)
	}
	f := [2]string{c.str("id"), r.str("id")}
	matrixFeeOf[e] = f
	return f
}

// matrixFeeState is the fee's ETag and files and the requests to post fees, as an Owner reads them.
func matrixFeeState(e *env, f [2]string) string {
	owner := e.as(authz.Owner)
	c := e.call(http.MethodGet, "/v1/director-fees/"+f[0], owner, nil)
	ev := e.call(http.MethodGet, "/v1/director-fees/"+f[0]+"/evidence", owner, nil)
	l := e.call(http.MethodGet, "/v1/director-fees", owner, nil)
	return c.Header.Get("ETag") + "|" + ev.Raw + "|" + l.Raw + "|" + strconv.Itoa(requestsOf(e, approval.ObjectDirectorFee))
}

var matrixFees = map[string]func(e *env, tok string) (response, func() bool){
	"listDirectorFees": func(e *env, tok string) (response, func() bool) {
		matrixFee(e)
		return e.call(http.MethodGet, "/v1/director-fees", tok, nil), nil
	},
	"getDirectorFee": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		return e.call(http.MethodGet, "/v1/director-fees/"+f[0], tok, nil), nil
	},
	"listDirectorFeeEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		return e.call(http.MethodGet, "/v1/director-fees/"+f[0]+"/evidence", tok, nil), nil
	},
	"getDirectorFeeEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		return e.call(http.MethodGet, "/v1/director-fees/"+f[0]+"/evidence/"+f[1], tok, nil), nil
	},
	// The writes: an allowed role previews, which saves nothing; a refused one changes nothing.
	"createDirectorFee": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		was := matrixFeeState(e, f)
		r := e.call(http.MethodPost, "/v1/director-fees", tok, map[string]any{"contact_id": matrixDirector(e), "entitled_on": "2026-10-16",
			"period_from": "2026-01-01", "period_to": "2026-12-31", "amount": map[string]any{"amount": "50.00", "currency": "SGD"}}, "Prefer", "return=preview")
		return r, func() bool { return matrixFeeState(e, f) == was }
	},
	"replaceDirectorFee": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		was := matrixFeeState(e, f)
		r := e.call(http.MethodPut, "/v1/director-fees/"+f[0], tok, map[string]any{"contact_id": matrixDirector(e), "entitled_on": "2026-10-15",
			"period_from": "2026-01-01", "period_to": "2026-12-31", "amount": map[string]any{"amount": "90.00", "currency": "SGD"}},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFeeState(e, f) == was }
	},
	// Always a request for another person's approval: an allowed role, stepped up, previews the request it would make.
	"requestDirectorFeePost": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		was := matrixFeeState(e, f)
		r := e.call(http.MethodPost, "/v1/director-fees/"+f[0]+"/post-approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFeeState(e, f) == was }
	},
	"addDirectorFeeEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		was := matrixFeeState(e, f)
		r := e.upload("/v1/director-fees/"+f[0]+"/evidence", tok, map[string]string{"evidence_type": "minutes"}, "more.pdf", []byte("%PDF-1.7 more"),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFeeState(e, f) == was }
	},
	"removeDirectorFeeEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixFee(e)
		was := matrixFeeState(e, f)
		r := e.call(http.MethodDelete, "/v1/director-fees/"+f[0]+"/evidence/"+f[1], tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFeeState(e, f) == was }
	},
}
