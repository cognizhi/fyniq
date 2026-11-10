//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// The close's budget variances (FR-GL-14; p1-planning design D6), worked by hand on TestBudgetVsActualAndHome's company
// at the recommended thresholds, more than 1,000.00 and 10%. Its budget is subscriptions of 10,000.00 a month,
// advertising of 2,000.00 and rent of 3,000.00; November's books hold subscriptions of 6,000.00, advertising of 2,500.00
// and rent of 3,500.00. Against the budget, in the trial balance's signs: subscriptions −6,000.00 against −10,000.00, a
// change of 4,000.00 or 40.0% of the budget, to explain; advertising and rent 500.00 each, within the thresholds.
// Against October, subscriptions moved from −12,000.00 by 6,000.00 or 50.0%, to explain apart. October has no earlier
// month, yet its subscriptions are 2,000.00 or 20.0% above the budget.
// req: FR-GL-14
// req: C.2 #57
func TestTheBudgetFlux(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.booksFrom()
	e.clock.Set(time.Date(2026, 12, 5, 9, 0, 0, 0, time.UTC))
	c := e.budgetChart()
	e.plannedBooks(c)
	accountant, controller := f.as(f.ahkow, authz.Accountant), e.as(authz.Controller)
	oct, nov := e.books.Oct.ID.String(), e.books.Nov.ID.String()
	list := func(period, against string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/periods/"+period+"/variances?against="+against, accountant, nil)
		if r.Status != http.StatusOK {
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
	amount := func(x map[string]any, field string) string { return x[field].(map[string]any)["amount"].(string) }
	check := func(period string) map[string]any {
		t.Helper()
		s := e.call(http.MethodGet, "/v1/periods/"+period+"/close", controller, nil)
		r := e.call(http.MethodPost, "/v1/periods/"+period+"/close-checks", controller, nil, "If-Match", s.Header.Get("ETag"))
		if r.Status != http.StatusOK {
			t.Fatalf("runCloseChecks: %d %s", r.Status, r.Raw)
		}
		for _, x := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if x.(map[string]any)["key"] == "variances" {
				return x.(map[string]any)
			}
		}
		t.Fatalf("no variances check: %s", r.Raw)
		return nil
	}
	explain := func(period, account, against, current, previous string) response {
		body := map[string]any{"current": map[string]any{"amount": current, "currency": "SGD"},
			"previous": map[string]any{"amount": previous, "currency": "SGD"}, "explanation": "A large customer moved its renewal to January."}
		if against != "" {
			body["against"] = against
		}
		return e.call(http.MethodPost, "/v1/periods/"+period+"/variances/"+account+"/explanation", accountant, body)
	}

	// Before a budget is approved, there is nothing to compare with, and nothing to explain against it.
	if r := list(nov, "budget"); r.Body["budgeted"] != false || len(r.Body["items"].([]any)) != 0 || r.Body["unexplained"] != float64(0) {
		t.Fatalf("November against no budget: %s", r.Raw)
	}
	e.problemIs(explain(nov, c.revenue, "budget", "-6000.00", "-10000.00"), "explainVariance", 409, closing.CodeNoVariance)
	if x := check(oct); x["severity"] != "ok" || x["title"] != "No earlier month to compare" {
		t.Errorf("October's check before the budget: %v", x)
	}

	f.approvedBudget(c)
	r := list(nov, "budget")
	if r.Body["against"] != "budget" || r.Body["budgeted"] != true || r.Body["unexplained"] != float64(1) || len(r.Body["items"].([]any)) != 3 {
		t.Fatalf("November against its budget: %s", r.Raw)
	}
	subs := item(r, "4010")
	if amount(subs, "current") != "-6000.00" || amount(subs, "previous") != "-10000.00" || amount(subs, "change") != "4000.00" ||
		subs["change_percent"] != "40.0" || subs["needs_explanation"] != true || subs["explained"] != false {
		t.Errorf("subscriptions against the budget: %v", subs)
	}
	for _, code := range []string{"6110", "6200"} {
		if x := item(r, code); amount(x, "change") != "500.00" || x["needs_explanation"] != false {
			t.Errorf("%s against the budget: %v", code, x)
		}
	}
	m := list(nov, "previous")
	if m.Body["against"] != "previous" || m.Body["budgeted"] != true || m.Body["unexplained"] != float64(1) ||
		item(m, "4010")["change_percent"] != "50.0" {
		t.Fatalf("November against October: %s", m.Raw)
	}
	x := check(nov)
	if x["severity"] != "error" || !strings.Contains(x["detail"].(string),
		"1 income or expense account(s) moved by more than 1000.00 SGD and 10% against October 2026, and 1 differ from November 2026's budget by more than 1000.00 SGD and 10%") {
		t.Fatalf("November's check: %v", x)
	}
	// October has no earlier month, but it has a budget.
	if x := check(oct); x["severity"] != "error" || !strings.Contains(x["detail"].(string), "1 differ from October 2026's budget") {
		t.Errorf("October's check with the budget: %v", x)
	}

	// The budget variance is explained apart: the month's explanation does not cover it, nor it the month's.
	e.problemIs(explain(nov, c.revenue, "budget", "-6000.00", "-12000.00"), "explainVariance", 409, closing.CodeMoved)
	if x := explain(nov, c.revenue, "budget", "-6000.00", "-10000.00"); x.Status != http.StatusOK || x.Body["explained"] != true ||
		x.str("explanation", "previous", "amount") != "-10000.00" {
		t.Fatalf("explaining the budget variance: %d %s", x.Status, x.Raw)
	}
	if r := list(nov, "budget"); r.Body["unexplained"] != float64(0) || item(r, "4010")["explained"] != true {
		t.Errorf("after explaining the budget variance: %s", r.Raw)
	}
	if m := list(nov, "previous"); m.Body["unexplained"] != float64(1) || item(m, "4010")["explanation"] != nil {
		t.Errorf("the month's variance after the budget's explanation: %s", m.Raw)
	}
	if x := explain(nov, c.revenue, "", "-6000.00", "-12000.00"); x.Status != http.StatusOK {
		t.Fatalf("explaining the month's variance: %d %s", x.Status, x.Raw)
	}
	if x := check(nov); x["severity"] != "ok" {
		t.Errorf("November's check once both are explained: %v", x)
	}

	// Another subscription in November moves the total: both explanations are stale, and each is asked for again.
	e.plannedLine(c.cash, c.revenue, porttest.Day(11, 25), "500.00")
	if r := list(nov, "budget"); r.Body["unexplained"] != float64(1) || item(r, "4010")["explained"] != false {
		t.Errorf("a stale budget explanation: %s", r.Raw)
	}
	if x := check(nov); x["severity"] != "error" {
		t.Errorf("November's check with stale explanations: %v", x)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `"kind":[null,"budget"]`) {
		t.Error("no budget explanation in the trail")
	}
}
