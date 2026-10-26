//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// receipt attaches file to a claim as tok and returns the file's ID.
func (e *env) receipt(claimID, tok string, file []byte) string {
	e.t.Helper()
	r := e.upload("/v1/expense-claims/"+claimID+"/evidence", tok, map[string]string{"evidence_type": "receipt"}, "receipt.pdf", file, "If-Match", "*")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("attaching a receipt: %d %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// checksOf are the codes of a claim's checks, as tok reads it.
func (e *env) checksOf(claimID, tok string) (codes []string, tag string) {
	e.t.Helper()
	g := e.call(http.MethodGet, "/v1/expense-claims/"+claimID, tok, nil)
	for _, x := range g.Body["checks"].([]any) {
		codes = append(codes, x.(map[string]any)["code"].(string))
	}
	return codes, g.Header.Get("ETag")
}

func hasAll(got []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			return false
		}
	}
	return true
}

// FR-AP-15 (p1-expense-claims task 5.1, design D6): at submission a claim meets the expense policy. Above its category's
// thresholds a line needs its receipt (✖) and names who attended (⚠); above its limit or past the claim deadline
// finance approves too, and a late claim says why (✖ until it does); a receipt whose file is on another record blocks
// the claim until the line says why it is not a duplicate (UC-EXP-01), and a look-alike receipt on another claim shows.
// Receipts are attached by the claimant, and seen by whoever sees the claim.
// req: FR-AP-15
// req: purchases/expense-claims Claim returned for a policy breach
// req: purchases/expense-claims Out-of-pocket receipt claim
func TestTheExpensePolicyAtSubmission(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, meals := e.claimSetup()
	ahkow := c.as(c.ahkow, authz.Employee)
	mei := c.as(c.mei, authz.Employee)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	var food string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		cat, err := u.ClaimCategories().Get(ctx, uuid.MustParse(meals))
		food = cat.AccountID.String()
		return err
	})
	m := e.call(http.MethodGet, "/v1/claim-categories", weiling, nil)
	var version string
	for _, x := range m.Body["items"].([]any) {
		if x.(map[string]any)["id"] == meals {
			version = etag(int(x.(map[string]any)["version"].(float64)))
		}
	}
	if r := e.call(http.MethodPut, "/v1/claim-categories/"+meals, weiling, map[string]any{"name": "Meals", "account_id": food, "limit": sgd("50.00"),
		"receipt_above": sgd("30.00"), "attendees_above": sgd("50.00")}, "If-Match", version); r.Status != 200 {
		t.Fatalf("the meals policy: %d %s", r.Status, r.Raw)
	}

	// UC-EXP-04's S$64.80 lunch, with no receipt and no attendees: above all three thresholds.
	draft := map[string]any{"date": "2026-10-15", "lines": []any{lunch(meals, "")}}
	r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, draft)
	id := r.str("id")
	if got, _ := e.checksOf(id, ahkow); !hasAll(got, claim.CodeReceiptRequired, claim.WarnAttendees, claim.WarnOverLimit) {
		t.Fatalf("the lunch's checks: %v", got)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/expense-claims/"+id+"/submit", ahkow, nil, "If-Match", r.Header.Get("ETag")), "submitExpenseClaim", 422,
		claim.CodeReceiptRequired)

	// Its receipt attached, by him only; the line names it; it is submitted with finance's step, the attendees only shown.
	e.problemIs(e.upload("/v1/expense-claims/"+id+"/evidence", mei, map[string]string{"evidence_type": "receipt"}, "receipt.pdf", []byte("%PDF-1.7 x"), "If-Match", "*"),
		"addExpenseClaimEvidence", 404, CodeNotFound)
	e.problemIs(e.upload("/v1/expense-claims/"+id+"/evidence", c.as(c.weiling, authz.Controller), map[string]string{"evidence_type": "receipt"}, "receipt.pdf",
		[]byte("%PDF-1.7 x"), "If-Match", "*"), "addExpenseClaimEvidence", 403, CodeClaimNotYours)
	file := []byte("%PDF-1.7 Din Tai Fung, 14 Oct 2026, S$64.80")
	lunchReceipt := e.receipt(id, ahkow, file)
	if l := e.call(http.MethodGet, "/v1/expense-claims/"+id+"/evidence", weiling, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("finance sees the receipt: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/expense-claims/"+id+"/evidence", mei, nil), "listExpenseClaimEvidence", 404, CodeNotFound)
	line := lunch(meals, "")
	line["receipt_id"] = lunchReceipt
	_, tag := e.checksOf(id, ahkow)
	fixed := e.call(http.MethodPut, "/v1/expense-claims/"+id, ahkow, map[string]any{"date": "2026-10-15", "lines": []any{line}}, "If-Match", tag)
	if got, _ := e.checksOf(id, ahkow); hasAll(got, claim.CodeReceiptRequired) || !hasAll(got, claim.WarnAttendees) {
		t.Errorf("with its receipt: %v", got)
	}
	asked := e.call(http.MethodPost, "/v1/expense-claims/"+id+"/submit", ahkow, nil, "If-Match", fixed.Header.Get("ETag"))
	if asked.Status != 202 || len(asked.Body["steps"].([]any)) != 2 {
		t.Fatalf("submitting with its receipt: %d %s", asked.Status, asked.Raw)
	}

	// Mei claims the same receipt file: blocked until she says why it is not a duplicate, which the approver sees; and a
	// look-alike receipt of the same day, merchant and amount shows on her claim.
	other := e.call(http.MethodPost, "/v1/expense-claims", mei, map[string]any{"date": "2026-10-15", "lines": []any{lunch(meals, "Mei; Ah Kow")}})
	same := e.receipt(other.str("id"), mei, file)
	dup := lunch(meals, "Mei; Ah Kow")
	dup["receipt_id"] = same
	_, tag = e.checksOf(other.str("id"), mei)
	o := e.call(http.MethodPut, "/v1/expense-claims/"+other.str("id"), mei, map[string]any{"date": "2026-10-15", "lines": []any{dup}}, "If-Match", tag)
	if got, _ := e.checksOf(other.str("id"), mei); !hasAll(got, claim.CodeDuplicate, claim.WarnLookalike) {
		t.Fatalf("the same receipt: %v", got)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/expense-claims/"+other.str("id")+"/submit", mei, nil, "If-Match", o.Header.Get("ETag")), "submitExpenseClaim", 422,
		claim.CodeDuplicate)
	dup["duplicate_reason"] = "We split the bill: one receipt, half each"
	_, tag = e.checksOf(other.str("id"), mei)
	o = e.call(http.MethodPut, "/v1/expense-claims/"+other.str("id"), mei, map[string]any{"date": "2026-10-15", "lines": []any{dup}}, "If-Match", tag)
	if got, _ := e.checksOf(other.str("id"), mei); hasAll(got, claim.CodeDuplicate) || !hasAll(got, claim.WarnDuplicate) {
		t.Errorf("with its reason: %v", got)
	}
	if s := e.call(http.MethodPost, "/v1/expense-claims/"+other.str("id")+"/submit", mei, nil, "If-Match", o.Header.Get("ETag")); s.Status != 202 {
		t.Errorf("submitting with its reason: %d %s", s.Status, s.Raw)
	}

	// A taxi of 1 August is past the 60-day deadline on 15 October: it says why, and finance approves too.
	old := taxi(travel)
	old["date"] = "2026-08-01"
	late := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": []any{old}})
	if got, _ := e.checksOf(late.str("id"), ahkow); !hasAll(got, claim.WarnLate, claim.CodeLateReason) {
		t.Fatalf("a late taxi: %v", got)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/expense-claims/"+late.str("id")+"/submit", ahkow, nil, "If-Match", late.Header.Get("ETag")), "submitExpenseClaim", 422,
		claim.CodeLateReason)
	_, tag = e.checksOf(late.str("id"), ahkow)
	l2 := e.call(http.MethodPut, "/v1/expense-claims/"+late.str("id"), ahkow, map[string]any{"date": "2026-10-15", "late_reason": "Found the receipt in a jacket",
		"lines": []any{old}}, "If-Match", tag)
	if s := e.call(http.MethodPost, "/v1/expense-claims/"+late.str("id")+"/submit", ahkow, nil, "If-Match", l2.Header.Get("ETag")); s.Status != 202 ||
		len(s.Body["steps"].([]any)) != 2 || !strings.Contains(s.Raw, `"roles":["controller"]`) {
		t.Errorf("submitting it late with a reason: %d %s", s.Status, s.Raw)
	}

	// A line naming another claim's file is refused.
	stray := taxi(travel)
	stray["receipt_id"] = lunchReceipt
	s := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": []any{stray}})
	if got, _ := e.checksOf(s.str("id"), ahkow); !hasAll(got, claim.CodeReceiptFile) {
		t.Errorf("another claim's file: %v", got)
	}
}

// matrixClaimEvidence are the authorisation matrix's rows for a claim's receipts, as the matrix's member, its claimant.
var matrixClaimEvidence = map[string]func(e *env, tok string) (response, func() bool){
	"addExpenseClaimEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		r := e.upload("/v1/expense-claims/"+id+"/evidence", steppedUp(e, tok), map[string]string{"evidence_type": "receipt"}, "receipt.pdf",
			[]byte("%PDF-1.7 matrix "+uuid.NewString()), "If-Match", "*")
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/expense-claims/"+id+"/evidence", e.as(authz.Owner), nil).Body["items"].([]any)) == 0
		}
	},
	"listExpenseClaimEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		return e.call(http.MethodGet, "/v1/expense-claims/"+id+"/evidence", steppedUp(e, tok), nil), nil
	},
	"getExpenseClaimEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		file := e.receipt(id, steppedUp(e, e.as(authz.Employee)), []byte("%PDF-1.7 matrix "+uuid.NewString()))
		return e.call(http.MethodGet, "/v1/expense-claims/"+id+"/evidence/"+file, steppedUp(e, tok), nil), nil
	},
	"removeExpenseClaimEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.ownClaimOf(uuid.MustParse(matrixApprover(e)))
		file := e.receipt(id, steppedUp(e, e.as(authz.Employee)), []byte("%PDF-1.7 matrix "+uuid.NewString()))
		r := e.call(http.MethodDelete, "/v1/expense-claims/"+id+"/evidence/"+file, steppedUp(e, tok), nil, "If-Match", etag(1))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/expense-claims/"+id+"/evidence/"+file, e.as(authz.Owner), nil).str("state") == "attached"
		}
	},
}
