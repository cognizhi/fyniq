//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// reconcilable is a money account with October's statement: opening at nothing, one line of S$10.00 out on 20 October
// waiting, closing at −10.00.
func (e *env) reconcilable() storage.MoneyAccount {
	e.t.Helper()
	e.bankBooks()
	m := e.newMoneyAccount()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		opening, closing := storage.Decimal("0.00"), storage.Decimal("-10.00")
		_, _, err := u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: m.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 1), PeriodTo: porttest.Day(10, 31), Currency: "SGD", Opening: &opening, Closing: &closing,
			At: e.clock.Now(), Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 20), Amount: "-10.00", Description: "AWS EMEA",
				Fingerprint: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")}}})
		return err
	})
	return m
}

// preparedReconciliation is a reconcilable account's October reconciliation, prepared by a bookkeeper.
func (e *env) preparedReconciliation() storage.BankReconciliation {
	e.t.Helper()
	m := e.reconcilable()
	var r banking.ReconciledPeriod
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = banking.Prepare(ctx, u, banking.ReconcileRequest{MoneyAccountID: m.ID, PeriodID: e.books.Oct.ID, At: e.clock.Now()})
		return err
	})
	return r.BankReconciliation
}

func (e *env) reconciliationNow(r storage.BankReconciliation) storage.BankReconciliation {
	e.t.Helper()
	var out storage.BankReconciliation
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = u.BankMatching().Reconciliation(ctx, r.MoneyAccountID, r.PeriodID)
		return err
	})
	return out
}

func reconciliationURL(r storage.BankReconciliation) string {
	return "/v1/money-accounts/" + r.MoneyAccountID.String() + "/reconciliations/" + r.PeriodID.String()
}

func (e *env) ruleDraft(name string) map[string]any {
	return map[string]any{"name": name, "position": 1, "active": true, "when": map[string]any{"description_contains": "aws", "direction": "out"},
		"then": map[string]any{"account_id": e.books.Rent.ID.String(), "gst_code": "TX"}}
}

// matrixBankRules are the matrix's rows of the rules, the Auto-reconciled list, reconciliations and cash (QA-SEC-03).
var matrixBankRules = map[string]func(e *env, tok string) (response, func() bool){
	"listBankRules": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bank-rules", tok, nil), nil
	},
	"createBankRule": func(e *env, tok string) (response, func() bool) {
		name := "Matrix " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/bank-rules", tok, e.ruleDraft(name))
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/bank-rules", e.as(authz.Accountant), nil).Raw, name)
		}
	},
	"getBankRule": func(e *env, tok string) (response, func() bool) {
		r := e.bankRule("Matrix " + uuid.NewString()[:8])
		return e.call(http.MethodGet, "/v1/bank-rules/"+r.ID.String(), tok, nil), nil
	},
	"replaceBankRule": func(e *env, tok string) (response, func() bool) {
		r := e.bankRule("Matrix " + uuid.NewString()[:8])
		resp := e.call(http.MethodPut, "/v1/bank-rules/"+r.ID.String(), tok, e.ruleDraft("Matrix "+uuid.NewString()[:8]), "If-Match", ruleETag(r))
		return resp, func() bool { return e.rule(r.ID).Version == r.Version }
	},
	"backTestBankRule": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/bank-rules:back-test", tok, map[string]any{"description_contains": "aws"}), nil
	},
	"listBankRuleOffers": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bank-rule-offers", tok, nil), nil
	},
	"listAutoReconciliations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/auto-reconciliations", tok, nil), nil
	},
	"getCashPosition": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/cash-position?date=2026-10-31", tok, nil), nil
	},
	"getBankReconciliation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, reconciliationURL(e.preparedReconciliation()), tok, nil), nil
	},
	"listBankReconciliations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/bank-reconciliations?period_id="+e.books.Oct.ID.String(), tok, nil), nil
	},
	"prepareBankReconciliation": func(e *env, tok string) (response, func() bool) {
		m := e.reconcilable()
		r := e.call(http.MethodPost, "/v1/bank-reconciliations", tok, map[string]any{"money_account_id": m.ID.String(), "period_id": e.books.Oct.ID.String()})
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String()+"/reconciliations/"+e.books.Oct.ID.String(), e.as(authz.Controller), nil).Status == 404
		}
	},
	"signBankReconciliation": func(e *env, tok string) (response, func() bool) {
		rec := e.preparedReconciliation()
		r := e.call(http.MethodPost, reconciliationURL(rec)+"/sign", steppedUp(e, tok), nil, "If-Match", etag(rec.Version))
		return r, func() bool { return e.reconciliationNow(rec).SignedBy == "" }
	},
}

// FR-BNK-05, FR-BNK-10, FR-BNK-13 and FR-BNK-14 through the API (p1-bank-import-reconcile task 9.1): a rule created,
// refused when it does not fit, read and changed at its ETag; conditions back-tested; a rule offered from five lines
// coded alike; what the system reconciled listed with its basis; a reconciliation prepared from the imported statement
// (a typed balance refused in Founder mode, FM-5), read with its waiting lines, refused to the preparer's row and without
// step-up, and signed off by a Controller who stepped up; the period's reconciliations; the cash position.
// req: FR-BNK-05
// req: FR-BNK-10
// req: FR-BNK-13
// req: FR-BNK-14
// req: FM-5
// req: FR-INT-01
func TestBankRulesAndReconciliationThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	bk := e.as(authz.Bookkeeper)
	e.bankBooks()

	// Rules.
	c := e.call(http.MethodPost, "/v1/bank-rules", bk, e.ruleDraft("AWS to rent"))
	if c.Status != 201 || c.str("when", "description_contains") != "aws" || c.Body["auto_apply"] != false || c.Header.Get("ETag") == "" {
		t.Fatalf("creating a rule: %d %s", c.Status, c.Raw)
	}
	bad := e.ruleDraft("Bad")
	bad["when"] = map[string]any{"description_regex": "("}
	e.problemIs(e.call(http.MethodPost, "/v1/bank-rules", bk, bad), "createBankRule", 422, banking.CodeRule)
	id, tag := c.str("id"), c.Header.Get("ETag")
	if g := e.call(http.MethodGet, "/v1/bank-rules/"+id, e.as(authz.Auditor), nil); g.Status != 200 || g.Header.Get("ETag") != tag {
		t.Fatalf("reading it: %d %s", g.Status, g.Raw)
	}
	changed := e.ruleDraft("AWS hosting")
	changed["position"] = 2
	if r := e.call(http.MethodPut, "/v1/bank-rules/"+id, bk, changed, "If-Match", tag); r.Status != 200 || r.str("name") != "AWS hosting" || r.Body["position"] != 2.0 {
		t.Fatalf("changing it: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/bank-rules/"+id, bk, changed, "If-Match", tag), "replaceBankRule", 412, CodeStale)
	if l := e.call(http.MethodGet, "/v1/bank-rules", bk, nil); !strings.Contains(l.Raw, `"name":"AWS hosting"`) {
		t.Errorf("the rules: %s", l.Raw)
	}

	// Back-test; offers from five lines coded alike.
	for range 5 {
		e.categorisedLine()
	}
	bt := e.call(http.MethodPost, "/v1/bank-rules:back-test", bk, map[string]any{"description_contains": "line", "direction": "out"})
	if bt.Status != 200 || bt.Body["matched"].(float64) < 5 || bt.Body["by_status"].(map[string]any)["categorised"].(float64) < 5 {
		t.Fatalf("the back-test: %d %s", bt.Status, bt.Raw)
	}
	of := e.call(http.MethodGet, "/v1/bank-rule-offers", bk, nil)
	if items, _ := of.Body["items"].([]any); of.Status != 200 || len(items) == 0 || !strings.Contains(of.Raw, `"lines":5`) {
		t.Fatalf("the offers: %d %s", of.Status, of.Raw)
	}

	// What the system reconciled: a transfer's other side.
	tr, to := e.inTransit()
	line := e.lineOn(to, "10.00", nil)
	if _, err := banking.AutoReconcile(e.bankCtx(), e.api.bank, e.books.Org, []uuid.UUID{line.ID}, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	ar := e.call(http.MethodGet, "/v1/auto-reconciliations?since=2026-10-01", bk, nil)
	if ar.Status != 200 || !strings.Contains(ar.Raw, line.ID.String()) || !strings.Contains(ar.Raw, `"basis":"transfer"`) || !strings.Contains(ar.Raw, tr.ID.String()) {
		t.Fatalf("the Auto-reconciled list: %d %s", ar.Status, ar.Raw)
	}

	// A reconciliation.
	m := e.reconcilable()
	body := map[string]any{"money_account_id": m.ID.String(), "period_id": e.books.Oct.ID.String()}
	typed := map[string]any{"money_account_id": m.ID.String(), "period_id": e.books.Oct.ID.String(), "statement_balance": sgd("-10.00")}
	e.problemIs(e.call(http.MethodPost, "/v1/bank-reconciliations", bk, typed), "prepareBankReconciliation", 422, banking.CodeTyped)
	p := e.call(http.MethodPost, "/v1/bank-reconciliations", bk, body)
	if p.Status != 200 || p.str("difference", "amount") != "0.00" || p.Body["unreconciled_count"] != 1.0 || p.str("statement_date") != "2026-10-31" ||
		len(p.Body["unreconciled_lines"].([]any)) != 1 {
		t.Fatalf("preparing it: %d %s", p.Status, p.Raw)
	}
	url := "/v1/money-accounts/" + m.ID.String() + "/reconciliations/" + e.books.Oct.ID.String()
	g := e.call(http.MethodGet, url, e.as(authz.Auditor), nil)
	if g.Status != 200 || g.Header.Get("ETag") != p.Header.Get("ETag") || len(g.Body["unreconciled_lines"].([]any)) != 1 {
		t.Fatalf("reading it: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, url+"/sign", steppedUp(e, bk), nil, "If-Match", g.Header.Get("ETag")), "signBankReconciliation", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, url+"/sign", e.as(authz.Controller), nil, "If-Match", g.Header.Get("ETag")), "signBankReconciliation", 403, CodeSessionRequired)
	s := e.call(http.MethodPost, url+"/sign", steppedUp(e, e.as(authz.Controller)), nil, "If-Match", g.Header.Get("ETag"))
	if s.Status != 200 || s.str("signed_by") == "" || s.str("signed_by") == s.str("prepared_by") {
		t.Fatalf("signing it off: %d %s", s.Status, s.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/bank-reconciliations", bk, body), "prepareBankReconciliation", 409, storage.CodeBanking)
	if l := e.call(http.MethodGet, "/v1/bank-reconciliations?period_id="+e.books.Oct.ID.String(), bk, nil); !strings.Contains(l.Raw, s.str("id")) {
		t.Errorf("the period's reconciliations: %s", l.Raw)
	}

	// The cash position.
	cash := e.call(http.MethodGet, "/v1/cash-position?date=2026-10-31", e.as(authz.Owner), nil)
	if cash.Status != 200 || cash.str("date") != "2026-10-31" || cash.str("total", "currency") != "SGD" || !strings.Contains(cash.Raw, m.ID.String()) {
		t.Fatalf("the cash position: %d %s", cash.Status, cash.Raw)
	}
}
