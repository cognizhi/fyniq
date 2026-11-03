//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-EQ-05 and FR-BNK-12 (PRD-001 rev 0.167; design D10), worked by hand: Ah Kow, linked to his contact, lent the company
// S$1,000.00 on 5 Oct 2026. Repaying him is refused while he has no account in force; once his own DBS account is
// approved, Wei Ling drafts repaying S$400.00, whose request shows the account; he never approves it (APR-15), Mei does:
// decided DS-0001 with no entry, payable. A second settlement for S$700.00 is refused, as only S$600.00 is not spoken for.
// The batch for 20 Oct suggests it; released, it posts DR-0001: Dr 2500 400.00 naming him (paid out), Cr the bank, and
// the settlement shows S$400.00 repaid. The bank's line naming the batch matches it by itself and the batch is paid.
// Then S$300.00 more, in a second batch the bank rejects: its void is asked by Mei as the Accountant, refused to Ah Kow,
// and approved by Wei Ling: reversed, the settlement open again and suggested for the next batch.
// req: FR-EQ-05
// req: FR-BNK-12
// req: FM-9
// req: equity/founders-money Repay
// req: ledger/posting-rules Repaying a director
// req: banking/bank-transactions A batch's payment matches by itself (rev 0.160)
func TestADirectorIsRepaidThroughABatch(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	loan, _ := e.directorChart()
	kow := e.directorContact("Ah Kow")
	if r := e.postFounderJournal("2026-10-05", founderLine(e.books.Bank.ID.String(), "1000.00", "", ""), founderLine(loan, "-1000.00", kow, ledger.OriginLent)); r.Status != http.StatusOK {
		t.Fatalf("Ah Kow lending: %d %s", r.Status, r.Raw)
	}
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	ahkow := c.as(c.ahkow, authz.Owner, authz.Controller)
	ct := e.call(http.MethodGet, "/v1/contacts/"+kow, e.as(authz.Accountant), nil)
	linked := e.call(http.MethodPut, "/v1/contacts/"+kow+"/member", ahkow, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+linked.str("id")+"/approve", weiling, hashOf(linked), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}
	sgd := func(a string) map[string]any { return map[string]any{"amount": a, "currency": "SGD"} }
	repay := func(amount string) response {
		return e.call(http.MethodPost, "/v1/director-settlements", weiling, map[string]any{"contact_id": kow, "account_id": loan, "amount": sgd(amount), "route": "repay"})
	}
	decide := func(amount string) string {
		t.Helper()
		d := repay(amount)
		path := "/v1/director-settlements/" + d.str("id")
		if f := e.upload(path+"/evidence", weiling, map[string]string{"evidence_type": "board minute"}, "minute.pdf", []byte("%PDF-1.7 "+amount), "If-Match", d.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("attaching the minute: %d %s", f.Status, f.Raw)
		}
		req := e.call(http.MethodPost, path+"/approval", weiling, nil, "If-Match", d.Header.Get("ETag"))
		if req.Status != http.StatusAccepted || !strings.Contains(req.Raw, `"repay_to"`) || !strings.Contains(req.Raw, "••••4321") {
			t.Fatalf("asking to repay, into his account: %d %s", req.Status, req.Raw)
		}
		e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", ahkow, hashOf(req), "If-Match", `"1"`), "approveApproval", 403, approval.CodeBeneficiary)
		if a := e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", c.as(c.mei, authz.Owner), hashOf(req), "If-Match", `"1"`); a.Status != http.StatusOK {
			t.Fatalf("Mei approving the repayment: %d %s", a.Status, a.Raw)
		}
		return d.str("id")
	}
	suggested := func(id string) bool {
		s := e.call(http.MethodGet, "/v1/payment-batches/suggestion?payment_date=2026-10-20", e.as(authz.Accountant), nil)
		return strings.Contains(s.Raw, `{"kind":"director","document_id":"`+id+`"}`)
	}

	// No account to repay him into yet; then his own, approved by another.
	e.problemIs(repay("400.00"), "createDirectorSettlement", 422, director.CodeNoRepayTo)
	me := c.as(c.ahkow, authz.Employee)
	change, _ := e.myBankChange(me, myAccount("5550004321"))
	e.myScreenshot(change, me)
	e.approveAsAnother(approval.ObjectClaimantBankChange, change, approval.OpClaimantBankChange, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := payee.Approve(ctx, u, uuid.MustParse(change), e.clock.Now())
		return err
	})
	first := decide("400.00")
	got := e.call(http.MethodGet, "/v1/director-settlements/"+first, weiling, nil)
	if got.str("status") != "decided" || got.str("number") != "DS-0001" || got.str("repaid", "amount") != "0.00" || got.str("repay_to", "account_number_last4") != "4321" ||
		strings.Contains(got.Raw, `"ledger_impact":[{`) {
		t.Fatalf("decided, payable: %s", got.Raw)
	}
	e.problemIs(repay("700.00"), "createDirectorSettlement", 422, director.CodeSettlement)
	if !suggested(first) {
		t.Fatal("the settlement is not suggested for the next batch")
	}

	// Released: DR-0001 posts Dr 2500 naming him, Cr the bank; the settlement is repaid and no longer suggested.
	rel, m := e.releasedBatch(paymentbatch.KindDirector + ":" + first)
	pay := paymentOf(t, rel, "Ah Kow")
	if pay["kind"] != "director" || pay["amount"].(map[string]any)["amount"] != "400.00" || pay["payee"].(map[string]any)["kind"] != "person" {
		t.Fatalf("the batch's payment to Ah Kow: %v", pay)
	}
	rp := e.call(http.MethodGet, "/v1/director-repayments/"+pay["payment_id"].(string), e.as(authz.Accountant), nil)
	for _, want := range []string{`"number":"DR-0001"`, `"settlement_number":"DS-0001"`, `"account_id":"` + loan + `","amount":{"amount":"400.00"`, `"party_id":"` + kow + `"`,
		`"amount":{"amount":"-400.00"`, `"batch_id":"` + rel.str("id") + `"`, `"date":"2026-10-20"`} {
		if !strings.Contains(rp.Raw, want) {
			t.Errorf("the repayment lacks %s: %s", want, rp.Raw)
		}
	}
	if got := e.call(http.MethodGet, "/v1/director-settlements/"+first, weiling, nil); got.str("repaid", "amount") != "400.00" || suggested(first) {
		t.Errorf("repaid: %s", got.Raw)
	}
	st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-10-01&to=2026-10-31", weiling, nil)
	if !strings.Contains(st.Raw, `"origin":"`+ledger.OriginPaidOut+`"`) || !strings.Contains(st.Raw, `"closing":{"amount":"-600.00"`) {
		t.Errorf("his statement: %s", st.Raw)
	}

	// The bank's line naming the batch matches the repayment by itself; the batch is paid.
	line := e.namedLineOn(m, "-400.00", "FAST "+rel.str("number")+" TAN AH KOW")
	if r := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", e.as(authz.Accountant), map[string]any{"money_account_id": m.ID.String()}); r.Status != http.StatusOK ||
		r.Body["reconciled"].(float64) != 1 || e.bankLineNow(line.ID).Status == storage.LineUnreconciled {
		t.Fatalf("matching the repayment = %d: %s", r.Status, r.Raw)
	}
	if b := e.call(http.MethodGet, "/v1/payment-batches/"+rel.str("id"), e.as(authz.Accountant), nil); b.str("status") != "paid" {
		t.Errorf("the batch: %s", b.Raw)
	}
	if rp := e.call(http.MethodGet, "/v1/director-repayments/"+pay["payment_id"].(string), e.as(authz.Accountant), nil); rp.Body["paid"] != true {
		t.Errorf("the repayment is not paid: %s", rp.Raw)
	}

	// S$300.00 more, in a batch the bank rejects: voided on Wei Ling's approval, never Ah Kow's, and open again.
	second := decide("300.00")
	rel2, _ := e.releasedBatch(paymentbatch.KindDirector + ":" + second)
	rejected := paymentOf(t, rel2, "Ah Kow")["payment_id"].(string)
	g := e.call(http.MethodGet, "/v1/director-repayments/"+rejected, e.as(authz.Accountant), nil)
	asked := e.call(http.MethodPost, "/v1/director-repayments/"+rejected+"/void-approval", c.as(c.mei, authz.Accountant), map[string]any{"date": "2026-10-22", "reason": "The bank rejected it"},
		"If-Match", g.Header.Get("ETag"))
	if asked.Status != http.StatusAccepted || asked.str("operation") != approval.OpRepaymentVoid {
		t.Fatalf("asking to void = %d: %s", asked.Status, asked.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", ahkow, hashOf(asked), "If-Match", `"1"`), "approveApproval", 403, approval.CodeBeneficiary)
	if a := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); a.Status != http.StatusOK {
		t.Fatalf("Wei Ling approving the void: %d %s", a.Status, a.Raw)
	}
	if v := e.call(http.MethodGet, "/v1/director-repayments/"+rejected, e.as(authz.Accountant), nil); v.str("status") != "reversed" {
		t.Errorf("voided: %s", v.Raw)
	}
	if b := e.call(http.MethodGet, "/v1/payment-batches/"+rel2.str("id"), e.as(authz.Accountant), nil); paymentOf(t, b, "Ah Kow")["rejected"] != true {
		t.Errorf("the second batch's payment is not rejected: %s", b.Raw)
	}
	if got := e.call(http.MethodGet, "/v1/director-settlements/"+second, weiling, nil); got.str("repaid", "amount") != "0.00" || !suggested(second) {
		t.Errorf("open again: %s", got.Raw)
	}
}

// matrixRepaymentOf is the repayment the matrix tries, made once per env in its organisation: the matrix's director lent
// S$300.00, a settlement of S$100.00 to repay into the contact's account was approved by another, and a batch repaid it.
var matrixRepaymentOf = map[*env]string{}

func matrixRepayment(e *env) string {
	if id, ok := matrixRepaymentOf[e]; ok {
		return id
	}
	loan, _ := e.directorChart()
	person := matrixDirector(e)
	if r := e.postFounderJournal("2026-10-07", founderLine(e.books.Bank.ID.String(), "300.00", "", ""), founderLine(loan, "-300.00", person, ledger.OriginLent)); r.Status != http.StatusOK {
		e.t.Fatalf("the matrix director lending: %d %s", r.Status, r.Raw)
	}
	e.vendorAccount(person, "7770001234")
	owner := e.as(authz.Owner)
	c := e.call(http.MethodPost, "/v1/director-settlements", owner, map[string]any{"contact_id": person, "account_id": loan,
		"amount": map[string]any{"amount": "100.00", "currency": "SGD"}, "route": "repay"})
	if c.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's settlement to repay: %d %s", c.Status, c.Raw)
	}
	if r := e.upload("/v1/director-settlements/"+c.str("id")+"/evidence", owner, map[string]string{"evidence_type": "board minute"}, "minute.pdf",
		[]byte("%PDF-1.7 matrix minute"), "If-Match", c.Header.Get("ETag")); r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's minute: %d %s", r.Status, r.Raw)
	}
	e.approveAsAnother(approval.ObjectDirectorSettlement, c.str("id"), approval.OpSettlementApprove, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := e.api.settlements.ApproveIn(ctx, u, uuid.MustParse(c.str("id")), e.clock.Now())
		return err
	})
	rel, _ := e.releasedBatch(paymentbatch.KindDirector + ":" + c.str("id"))
	id := rel.Body["payments"].([]any)[0].(map[string]any)["payment_id"].(string)
	matrixRepaymentOf[e] = id
	return id
}

var matrixRepayments = map[string]func(e *env, tok string) (response, func() bool){
	"getDirectorRepayment": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/director-repayments/"+matrixRepayment(e), tok, nil), nil
	},
	// Always a request for another person's approval: an allowed role, stepped up, previews the request it would make.
	"requestDirectorRepaymentVoid": func(e *env, tok string) (response, func() bool) {
		id := matrixRepayment(e)
		state := func() string {
			return e.call(http.MethodGet, "/v1/director-repayments/"+id, e.as(authz.Accountant), nil).Header.Get("ETag") + "|" +
				strings.Repeat("r", requestsOf(e, approval.ObjectDirectorRepayment))
		}
		was := state()
		r := e.call(http.MethodPost, "/v1/director-repayments/"+id+"/void-approval", steppedUp(e, tok), map[string]any{"date": "2026-10-25", "reason": "Rejected"},
			"If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return state() == was }
	},
}
