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
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// book posts amount to account against the bank on date, straight to the ledger: the API's journals stop at the
// self-approval threshold, and the variances need larger totals.
func (e *env) book(account storage.Account, date time.Time, amount string) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	other := "-" + amount
	if strings.HasPrefix(amount, "-") {
		other = amount[1:]
	}
	id := uuid.NewString()
	entry := storage.NewEntry{PostingDate: date, PostingIdentity: id, RuleVersion: "test@1", Source: storage.Source{Type: "manual_journal", ID: id, Version: 1},
		Lines: []storage.NewLine{
			{AccountID: account.ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
			{AccountID: e.books.Bank.ID, Currency: "SGD", Amount: storage.Decimal(other), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(other)},
		}}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, entry)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
}

// Flux analysis in the close: each income or expense account's month against the previous one; a variance above both
// thresholds needs an explanation of its totals before the checks let the close go on; the explanation goes stale when
// a total changes, and none is written for figures its writer did not read, a balance-sheet account or a closed month.
// req: FR-GL-14
// req: C.2 #57
// req: RND-PERCENT
func TestFluxAnalysisInTheClose(t *testing.T) {
	e := newEnv(t)
	oct, nov := e.books.Oct.ID.String(), e.books.Nov.ID.String()
	accountant, controller := e.as(authz.Accountant), e.as(authz.Controller)
	e.book(e.books.Rent, porttest.Day(10, 5), "5000.00")
	e.book(e.books.Sales, porttest.Day(10, 9), "-500.00")
	e.book(e.books.Rent, porttest.Day(11, 5), "8000.00")
	list := func(period string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/periods/"+period+"/variances", accountant, nil)
		if r.Status != 200 {
			t.Fatalf("listPeriodVariances: %d %s", r.Status, r.Raw)
		}
		return r
	}
	item := func(r response, code string) map[string]any {
		for _, x := range r.Body["items"].([]any) {
			if x.(map[string]any)["code"] == code {
				return x.(map[string]any)
			}
		}
		return nil
	}
	severity := func(r response) string {
		for _, c := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if c.(map[string]any)["key"] == "variances" {
				return c.(map[string]any)["severity"].(string)
			}
		}
		return ""
	}
	checks := func(period string) response {
		t.Helper()
		s := e.call(http.MethodGet, "/v1/periods/"+period+"/close", controller, nil)
		r := e.call(http.MethodPost, "/v1/periods/"+period+"/close-checks", controller, nil, "If-Match", s.Header.Get("ETag"))
		if r.Status != 200 {
			t.Fatalf("runCloseChecks: %d %s", r.Status, r.Raw)
		}
		return r
	}
	explain := func(tok, period, account string, body map[string]any, headers ...string) response {
		return e.call(http.MethodPost, "/v1/periods/"+period+"/variances/"+account+"/explanation", tok, body, headers...)
	}
	sgd := func(amount string) map[string]any { return map[string]any{"amount": amount, "currency": "SGD"} }
	rent := e.books.Rent.ID.String()

	// October has no earlier month: nothing to compare, and its check passes.
	if r := list(oct); r.Body["previous_period"] != nil || len(r.Body["items"].([]any)) != 0 || r.Body["unexplained"] != float64(0) {
		t.Fatalf("October: %s", r.Raw)
	}
	if r := checks(oct); severity(r) != "ok" {
		t.Errorf("October's variances check: %s", r.Raw)
	}

	// November against October, by code, in the trial balance's signs; the thresholds are the recommended settings.
	r := list(nov)
	if r.str("previous_period", "id") != oct || r.str("threshold_amount", "amount") != "1000.00" || r.str("threshold_percent") != "10" ||
		r.Body["unexplained"] != float64(1) || len(r.Body["items"].([]any)) != 2 || r.Body["items"].([]any)[0].(map[string]any)["code"] != "4000" {
		t.Fatalf("November: %s", r.Raw)
	}
	sales, rentRow := item(r, "4000"), item(r, "6100")
	if sales["current"].(map[string]any)["amount"] != "0.00" || sales["previous"].(map[string]any)["amount"] != "-500.00" ||
		sales["change"].(map[string]any)["amount"] != "500.00" || sales["change_percent"] != "100.0" || sales["needs_explanation"] != false ||
		sales["type"] != "revenue" {
		t.Errorf("sales: %v", sales)
	}
	if rentRow["current"].(map[string]any)["amount"] != "8000.00" || rentRow["previous"].(map[string]any)["amount"] != "5000.00" ||
		rentRow["change"].(map[string]any)["amount"] != "3000.00" || rentRow["change_percent"] != "60.0" || rentRow["needs_explanation"] != true ||
		rentRow["explained"] != false || rentRow["explanation"] != nil {
		t.Errorf("rent: %v", rentRow)
	}
	if p := e.call(http.MethodGet, "/v1/periods/"+nov+"/variances?page_size=1", accountant, nil); len(p.Body["items"].([]any)) != 1 ||
		p.str("next_cursor") == "" || p.Body["unexplained"] != float64(1) {
		t.Errorf("a page of one: %s", p.Raw)
	}

	// The checks refuse the close while rent's variance has no explanation.
	if r := checks(nov); severity(r) != "error" || !strings.Contains(r.Raw, "1 income or expense account(s) moved by more than 1000.00 SGD and 10% against October 2026") {
		t.Fatalf("November's checks: %s", r.Raw)
	}

	// Refused: totals other than the account's now, an account with no variance, a month with nothing to compare, no words,
	// totals in another currency, and the AI or an Owner, who do not prepare the close.
	words := "The office moved to a larger unit in November."
	body := map[string]any{"current": sgd("8000.00"), "previous": sgd("5000.00"), "explanation": words}
	e.problemIs(explain(accountant, nov, rent, map[string]any{"current": sgd("7000.00"), "previous": sgd("5000.00"), "explanation": words}),
		"explainVariance", 409, closing.CodeMoved)
	e.problemIs(explain(accountant, nov, e.books.Bank.ID.String(), map[string]any{"current": sgd("-8000.00"), "previous": sgd("-4500.00"), "explanation": words}),
		"explainVariance", 409, closing.CodeNoVariance)
	e.problemIs(explain(accountant, oct, rent, map[string]any{"current": sgd("5000.00"), "previous": sgd("0.00"), "explanation": words}),
		"explainVariance", 409, closing.CodeNoVariance)
	e.problemIs(explain(accountant, nov, rent, map[string]any{"current": sgd("8000.00"), "previous": sgd("5000.00"), "explanation": "   "}),
		"explainVariance", 422, CodeInvalid)
	e.problemIs(explain(accountant, nov, rent, map[string]any{"current": map[string]any{"amount": "8000.00", "currency": "USD"}, "previous": sgd("5000.00"),
		"explanation": words}), "explainVariance", 422, CodeInvalid)
	e.problemIs(explain(e.as(authz.Owner), nov, rent, body), "explainVariance", 403, CodeForbidden)

	// A preview records nothing.
	pv := explain(accountant, nov, rent, body, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || pv.Body["explained"] != true || list(nov).Body["unexplained"] != float64(1) {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}

	// The accountant explains it, once for the same key.
	key := uuid.NewString()
	x := explain(accountant, nov, rent, body, "Idempotency-Key", key)
	if x.Status != 200 || x.Body["explained"] != true || x.str("explanation", "explanation") != words || x.str("explanation", "current", "amount") != "8000.00" ||
		x.str("explanation", "previous", "amount") != "5000.00" || x.str("explanation", "written_by", "kind") != "user" {
		t.Fatalf("the explanation: %d %s", x.Status, x.Raw)
	}
	if again := explain(accountant, nov, rent, body, "Idempotency-Key", key); again.Header.Get("Idempotent-Replayed") != "true" ||
		again.str("explanation", "id") != x.str("explanation", "id") {
		t.Errorf("the repeat: %s", again.Raw)
	}
	if r := list(nov); r.Body["unexplained"] != float64(0) || item(r, "6100")["explained"] != true {
		t.Errorf("after the explanation: %s", r.Raw)
	}
	if r := checks(nov); severity(r) != "ok" {
		t.Errorf("November's checks after the explanation: %s", r.Raw)
	}

	// A later posting changes the totals: the explanation is stale, kept, and the close asks for a new one.
	e.book(e.books.Rent, porttest.Day(11, 6), "10.00")
	r = list(nov)
	if rentRow := item(r, "6100"); r.Body["unexplained"] != float64(1) || rentRow["explained"] != false ||
		rentRow["explanation"].(map[string]any)["current"].(map[string]any)["amount"] != "8000.00" {
		t.Errorf("a stale explanation: %s", r.Raw)
	}
	if x := explain(controller, nov, rent, map[string]any{"current": sgd("8010.00"), "previous": sgd("5000.00"), "explanation": words + " Plus a late repair."}); x.Status != 200 ||
		list(nov).Body["unexplained"] != float64(0) {
		t.Errorf("the new explanation: %d %s", x.Status, x.Raw)
	}

	// A closed month's variances are not explained.
	ctx := context.Background()
	as := func(who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	checklistDone(t, e.store, e.books.Org, e.books.Oct.ID)
	c, err := e.api.closing.RunChecks(as("preparer"), e.books.Org, e.books.Oct.ID)
	if err == nil {
		_, _, err = e.api.closing.Close(as("attester"), e.books.Org, e.books.Oct.ID, c.ID, "Reviewed.")
	}
	if err != nil {
		t.Fatal(err)
	}
	e.problemIs(explain(accountant, oct, rent, map[string]any{"current": sgd("5000.00"), "previous": sgd("0.00"), "explanation": words}),
		"explainVariance", 409, closing.CodeNotOpen)

	if trail := auditTrail(t, e); !strings.Contains(trail, `"action":"period.variance_explained"`) {
		t.Error("no period.variance_explained in the trail")
	}
}
