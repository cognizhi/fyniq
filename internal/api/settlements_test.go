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

// capitalContribution gives e's chart 3300 Capital contribution, and returns its ID.
func (e *env) capitalContribution() string {
	e.t.Helper()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "3300", Name: "Capital contribution", Type: storage.Equity})
		id = a.ID.String()
		return err
	})
	return id
}

// FR-EQ-05, FR-EQ-07 and FM-9 (PRD-001 rev 0.167; design D10), worked by hand: Ah Kow, linked to his contact, lent the
// company S$1,000.00 on 5 Oct 2026. Wei Ling, an Owner, drafts keeping S$600.00 as an interest-free loan on demand (the
// default route, left out); more than is owed is refused, and nothing is asked until the loan note is attached. She asks;
// Ah Kow, the person settled, never approves it (APR-15), nor she her own request; Mei, holding the Owner's approval
// right, approves it: decided DS-0001 with no entry. Then he waives S$400.00 as a shareholder: the request shows Dr 2500,
// Cr 3300; approved, it posts on 15 Oct 2026 naming him, waived, and nothing remains owed.
// req: FR-EQ-05
// req: FR-EQ-07
// req: FM-9
// req: equity/founders-money Keep as a loan
// req: equity/founders-money Waive
// req: equity/founders-money Ledger impact before approval
// req: access/team-modes Another founder approves
func TestDirectorSettlements(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	loan, _ := e.directorChart()
	capital := e.capitalContribution()
	kow := e.directorContact("Ah Kow")
	if r := e.postFounderJournal("2026-10-05", founderLine(e.books.Bank.ID.String(), "1000.00", "", ""), founderLine(loan, "-1000.00", kow, ledger.OriginLent)); r.Status != http.StatusOK {
		t.Fatalf("Ah Kow lending: %d %s", r.Status, r.Raw)
	}
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	ct := e.call(http.MethodGet, "/v1/contacts/"+kow, e.as(authz.Accountant), nil)
	asked := e.call(http.MethodPut, "/v1/contacts/"+kow+"/member", c.as(c.ahkow, authz.Owner, authz.Controller), map[string]any{"member_user_id": c.ahkow},
		"If-Match", ct.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}
	sgd := func(a string) map[string]any { return map[string]any{"amount": a, "currency": "SGD"} }
	attach := func(path, etag string) {
		t.Helper()
		if f := e.upload(path+"/evidence", weiling, map[string]string{"evidence_type": "loan note"}, "note.pdf", []byte("%PDF-1.7 "+path), "If-Match", etag); f.Status != http.StatusCreated {
			t.Fatalf("attaching to %s: %d %s", path, f.Status, f.Raw)
		}
	}
	ask := func(path, etag string) response {
		return e.call(http.MethodPost, path+"/approval", weiling, nil, "If-Match", etag)
	}
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}

	// Keep, the default route.
	e.problemIs(e.call(http.MethodPost, "/v1/director-settlements", weiling, map[string]any{"contact_id": kow, "account_id": loan, "amount": sgd("1200.00")}),
		"createDirectorSettlement", 422, director.CodeSettlement)
	keep := e.call(http.MethodPost, "/v1/director-settlements", weiling, map[string]any{"contact_id": kow, "account_id": loan, "amount": sgd("600.00")})
	if keep.Status != http.StatusCreated || keep.str("route") != director.RouteKeep || keep.str("terms", "interest_rate") != "0" || keep.str("owed", "amount") != "1000.00" {
		t.Fatalf("drafting keep: %d %s", keep.Status, keep.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/director-settlements?contact_id="+kow, weiling, nil); l.str("default_route") != director.RouteKeep || !strings.Contains(l.Raw, keep.str("id")) {
		t.Errorf("the list, with the route a new settlement starts with: %s", l.Raw)
	}
	keepPath := "/v1/director-settlements/" + keep.str("id")
	e.problemIs(ask(keepPath, keep.Header.Get("ETag")), "requestDirectorSettlementApproval", 422, director.CodeNoAgreement)
	attach(keepPath, keep.Header.Get("ETag"))
	req := ask(keepPath, keep.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || req.str("object_type") != approval.ObjectDirectorSettlement {
		t.Fatalf("asking: %d %s", req.Status, req.Raw)
	}
	if w := e.call(http.MethodGet, keepPath, weiling, nil); w.str("status") != "waiting" || w.Body["accountant_review"] != false {
		t.Errorf("waiting: %s", w.Raw)
	}
	e.problemIs(approve(req, c.as(c.ahkow, authz.Owner, authz.Controller)), "approveApproval", 403, approval.CodeBeneficiary)
	e.problemIs(approve(req, weiling), "approveApproval", 403, approval.CodeOwnRequest)
	if a := approve(req, c.as(c.mei, authz.Owner)); a.Status != http.StatusOK {
		t.Fatalf("Mei approving the loan kept: %d %s", a.Status, a.Raw)
	}
	kept := e.call(http.MethodGet, keepPath, weiling, nil)
	if kept.str("status") != "decided" || kept.str("number") != "DS-0001" || strings.Contains(kept.Raw, `"ledger_impact":[{`) {
		t.Errorf("the loan kept: %s", kept.Raw)
	}

	// Waive, as a shareholder.
	waive := e.call(http.MethodPost, "/v1/director-settlements", weiling, map[string]any{"contact_id": kow, "account_id": loan, "amount": sgd("400.00"), "route": "waive",
		"waiver": map[string]any{"credit_account_id": capital, "capacity": "shareholder", "reason": "Waived ahead of the seed round"}})
	if waive.Status != http.StatusCreated || !strings.Contains(waive.Raw, `"account_id":"`+capital+`","amount":{"amount":"-400.00"`) {
		t.Fatalf("drafting the waiver, with its dry run: %d %s", waive.Status, waive.Raw)
	}
	waivePath := "/v1/director-settlements/" + waive.str("id")
	attach(waivePath, waive.Header.Get("ETag"))
	wreq := ask(waivePath, waive.Header.Get("ETag"))
	if wreq.Status != http.StatusAccepted || !strings.Contains(wreq.Raw, `"debit 2500"`) || !strings.Contains(wreq.Raw, `"credit 3300"`) {
		t.Fatalf("asking for the waiver, its legs in the envelope: %d %s", wreq.Status, wreq.Raw)
	}
	if a := approve(wreq, c.as(c.mei, authz.Owner)); a.Status != http.StatusOK {
		t.Fatalf("Mei approving the waiver: %d %s", a.Status, a.Raw)
	}
	waived := e.call(http.MethodGet, waivePath, weiling, nil)
	for _, want := range []string{`"status":"decided"`, `"account_id":"` + loan + `","amount":{"amount":"400.00"`, `"party_id":"` + kow + `"`, `"deducted_earlier":false`} {
		if !strings.Contains(waived.Raw, want) {
			t.Errorf("the waiver lacks %s: %s", want, waived.Raw)
		}
	}
	st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-10-01&to=2026-10-31", weiling, nil)
	if !strings.Contains(st.Raw, `"origin":"`+ledger.OriginWaived+`"`) || !strings.Contains(st.Raw, `"closing":{"amount":"-600.00"`) {
		t.Errorf("his statement: %s", st.Raw)
	}
	e.problemIs(e.call(http.MethodPut, waivePath, weiling, map[string]any{"contact_id": kow, "account_id": loan, "amount": sgd("1.00")}, "If-Match", waived.Header.Get("ETag")),
		"replaceDirectorSettlement", 409, director.CodeSettled)
}

// matrixSettlementOf is the settlement the matrix tries, drafted once per env in its organisation: S$100.00 of the S$500.00
// the matrix's director lent, kept as a loan, with its loan note attached; its ID and the file's.
var matrixSettlementOf = map[*env][2]string{}

func matrixSettlement(e *env) [2]string {
	if f, ok := matrixSettlementOf[e]; ok {
		return f
	}
	loan, _ := e.directorChart()
	person := matrixDirector(e)
	if r := e.postFounderJournal("2026-10-06", founderLine(e.books.Bank.ID.String(), "500.00", "", ""), founderLine(loan, "-500.00", person, ledger.OriginLent)); r.Status != http.StatusOK {
		e.t.Fatalf("the matrix director lending: %d %s", r.Status, r.Raw)
	}
	owner := e.as(authz.Owner)
	c := e.call(http.MethodPost, "/v1/director-settlements", owner, map[string]any{"contact_id": person, "account_id": loan,
		"amount": map[string]any{"amount": "100.00", "currency": "SGD"}, "route": "keep"})
	if c.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's settlement: %d %s", c.Status, c.Raw)
	}
	r := e.upload("/v1/director-settlements/"+c.str("id")+"/evidence", owner, map[string]string{"evidence_type": "loan note"}, "note.pdf",
		[]byte("%PDF-1.7 matrix note"), "If-Match", c.Header.Get("ETag"))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's loan note: %d %s", r.Status, r.Raw)
	}
	f := [2]string{c.str("id"), r.str("id")}
	matrixSettlementOf[e] = f
	return f
}

func matrixSettlementState(e *env, f [2]string) string {
	owner := e.as(authz.Owner)
	c := e.call(http.MethodGet, "/v1/director-settlements/"+f[0], owner, nil)
	ev := e.call(http.MethodGet, "/v1/director-settlements/"+f[0]+"/evidence", owner, nil)
	l := e.call(http.MethodGet, "/v1/director-settlements", owner, nil)
	return c.Header.Get("ETag") + "|" + ev.Raw + "|" + l.Raw + "|" + strconv.Itoa(requestsOf(e, approval.ObjectDirectorSettlement))
}

var matrixSettlements = map[string]func(e *env, tok string) (response, func() bool){
	"listDirectorSettlements": func(e *env, tok string) (response, func() bool) {
		matrixSettlement(e)
		return e.call(http.MethodGet, "/v1/director-settlements", tok, nil), nil
	},
	"getDirectorSettlement": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		return e.call(http.MethodGet, "/v1/director-settlements/"+f[0], tok, nil), nil
	},
	"listDirectorSettlementEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		return e.call(http.MethodGet, "/v1/director-settlements/"+f[0]+"/evidence", tok, nil), nil
	},
	"getDirectorSettlementEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		return e.call(http.MethodGet, "/v1/director-settlements/"+f[0]+"/evidence/"+f[1], tok, nil), nil
	},
	// The writes: an allowed role previews, which saves nothing; a refused one changes nothing.
	"createDirectorSettlement": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		loan, _ := e.directorChart()
		was := matrixSettlementState(e, f)
		r := e.call(http.MethodPost, "/v1/director-settlements", tok, map[string]any{"contact_id": matrixDirector(e), "account_id": loan,
			"amount": map[string]any{"amount": "50.00", "currency": "SGD"}}, "Prefer", "return=preview")
		return r, func() bool { return matrixSettlementState(e, f) == was }
	},
	"replaceDirectorSettlement": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		loan, _ := e.directorChart()
		was := matrixSettlementState(e, f)
		r := e.call(http.MethodPut, "/v1/director-settlements/"+f[0], tok, map[string]any{"contact_id": matrixDirector(e), "account_id": loan,
			"amount": map[string]any{"amount": "90.00", "currency": "SGD"}, "route": "keep"}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixSettlementState(e, f) == was }
	},
	// Always a request for another person's approval: an allowed role, stepped up, previews the request it would make.
	"requestDirectorSettlementApproval": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		was := matrixSettlementState(e, f)
		r := e.call(http.MethodPost, "/v1/director-settlements/"+f[0]+"/approval", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixSettlementState(e, f) == was }
	},
	"addDirectorSettlementEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		was := matrixSettlementState(e, f)
		r := e.upload("/v1/director-settlements/"+f[0]+"/evidence", tok, map[string]string{"evidence_type": "board minute"}, "more.pdf", []byte("%PDF-1.7 more"),
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixSettlementState(e, f) == was }
	},
	"removeDirectorSettlementEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixSettlement(e)
		was := matrixSettlementState(e, f)
		r := e.call(http.MethodDelete, "/v1/director-settlements/"+f[0]+"/evidence/"+f[1], tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixSettlementState(e, f) == was }
	},
}
