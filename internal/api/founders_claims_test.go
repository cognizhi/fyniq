//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
)

// FR-EQ-02 (PRD-001 rev 0.167; p1-founders-money design D6), worked by hand: Ah Kow, a director whose contact is linked
// to him, keeps his S$27.30 taxi as a loan to the company. Another founder approves it as any claim; it posts under
// expense_claim@2, Dr Rent (the taxi's category) 27.30, Cr 2500 27.30 naming him; his director account says the company
// owes him S$27.30, paid personally; and nothing reimburses it. He never approves it himself. Mei, who signs in as no director, cannot keep a claim as
// a loan, nor can a claim that settles a cash advance.
// req: FR-EQ-02
// req: equity/founders-money Reimburse or keep as a loan
// req: ledger/posting-rules Cost a founder paid personally, kept as a loan
func TestAClaimKeptAsALoanToTheCompany(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	loan, _ := e.directorChart()
	kow := e.directorContact("Ah Kow")
	ahkow := c.as(c.ahkow, authz.Employee)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Approver, authz.Employee)

	// He asks for his contact's link to him; Wei Ling approves it.
	ct := e.call(http.MethodGet, "/v1/contacts/"+kow, e.as(authz.Accountant), nil)
	asked := e.call(http.MethodPut, "/v1/contacts/"+kow+"/member", c.as(c.ahkow, authz.Owner, authz.Controller), map[string]any{"member_user_id": c.ahkow},
		"If-Match", ct.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s / %s", r.Status, r.Raw, asked.Raw)
	}

	draft := map[string]any{"date": "2026-10-15", "title": "Taxi", "settle_to": "loan", "lines": []any{taxi(travel)}}
	r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, draft)
	if r.Status != http.StatusCreated || r.str("settle_to") != "loan" || r.str("total", "amount") != "27.30" {
		t.Fatalf("drafting a claim kept as a loan: %d %s", r.Status, r.Raw)
	}
	path := "/v1/expense-claims/" + r.str("id")
	impact := e.call(http.MethodGet, path, weiling, nil)
	if !strings.Contains(impact.Raw, `"account_id":"`+loan+`"`) || !strings.Contains(impact.Raw, `"party_id":"`+kow+`"`) {
		t.Errorf("its ledger impact names 2500 and Ah Kow: %s", impact.Raw)
	}
	sub := e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", r.Header.Get("ETag"))
	if sub.Status != http.StatusAccepted {
		t.Fatalf("submitting: %d %s", sub.Status, sub.Raw)
	}
	// He never approves his own claim, even as an Owner and the Controller (FR-AP-17).
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+sub.str("id")+"/approve", c.as(c.ahkow, authz.Owner, authz.Controller), hashOf(sub),
		"If-Match", `"1"`), "approveApproval", 403, approval.CodeOwnRequest)
	if a := e.call(http.MethodPost, "/v1/approvals/"+sub.str("id")+"/approve", weiling, hashOf(sub), "If-Match", `"1"`); a.Status != http.StatusOK {
		t.Fatalf("Wei Ling approving: %d %s", a.Status, a.Raw)
	}
	posted := e.call(http.MethodGet, path, weiling, nil)
	if posted.str("status") != "posted" || posted.str("stage") != "approved" || posted.str("total", "amount") != "27.30" {
		t.Errorf("posted: %s", posted.Raw)
	}
	mine := e.call(http.MethodGet, "/v1/me/director-account", c.as(c.ahkow, authz.Owner), nil)
	if mine.str("account", "net", "amount") != "-27.30" {
		t.Errorf("his director account: %s", mine.Raw)
	}
	st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-10-01&to=2026-10-31", weiling, nil)
	if !strings.Contains(st.Raw, `"origin":"`+ledger.OriginPaidPersonally+`"`) {
		t.Errorf("his statement: %s", st.Raw)
	}
	// Nothing reimburses a claim kept as a loan: it owes nothing on 2150.
	acc := e.chartMoneyAccount()
	e.problemIs(e.call(http.MethodPost, "/v1/claim-reimbursements", e.as(authz.Accountant), map[string]any{"claimant_id": c.ahkow, "date": "2026-10-20",
		"money_account_id": acc.ID.String(), "claims": []any{map[string]any{"claim_id": r.str("id"), "amount": sgd("27.30")}}}),
		"recordClaimReimbursement", 422, claim.CodeNotOwed)

	// Mei signs in as no director: her claim kept as a loan does not submit.
	m := e.call(http.MethodPost, "/v1/expense-claims", mei, draft)
	if m.Status != http.StatusCreated {
		t.Fatalf("Mei drafting: %d %s", m.Status, m.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/expense-claims/"+m.str("id"), mei, nil); !strings.Contains(g.Raw, claim.CodeSettleTo) {
		t.Errorf("Mei's claim's checks: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/expense-claims/"+m.str("id")+"/submit", mei, nil, "If-Match", m.Header.Get("ETag")), "submitExpenseClaim", 422, claim.CodeSettleTo)
}
