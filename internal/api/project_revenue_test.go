//go:build local

package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/projectrevenue"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// revenueBooks gives the books the three accounts revenue over time posts to, at the settings' recommended codes.
func (e *env) revenueBooks() {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range []storage.NewAccount{{Code: "1150", Name: "Contract asset", Type: storage.Asset}, {Code: "2400", Name: "Deferred revenue", Type: storage.Liability},
			{Code: "4030", Name: "Project revenue", Type: storage.Revenue}} {
			have, err := u.Accounts().List(ctx)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(have, func(x storage.Account) bool { return x.Code == a.Code }) {
				continue
			}
			if _, err := u.Accounts().Create(ctx, a); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

// FR-PRJ-11 through the API: a fixed-fee project elected for revenue over time by cost-to-cost progress. The month's figures are read from the posted ledger (the fee is the
// milestones' total, the cost is the project's tagged costs, the revenue held is what was invoiced): 1,000.00 fee, 600.00 estimated, 300.00 spent and 400.00 invoiced earn
// 500.00, so the journal books 100.00 and leaves a contract asset of 100.00. The journal is proposed as a draft with every line carrying the project, once a month; it posts
// nothing until a person posts it; and a project that is not fixed-fee, not elected or without its accounts is refused in plain words.
// req: FR-PRJ-11
// asvs: V4.2.1
func TestAFixedFeeProjectRecognisesRevenueOverTimeByAProposedJournal(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	customer := e.customer() // first: it sets up accounts of its own
	e.revenueBooks()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Rev", "approver")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "REV-1", "name": "Fixed over time", "currency": "SGD", "billing_method": "fixed_fee", "customer_id": customer,
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	for _, m := range []struct{ name, amount string }{{"Design", "600.00"}, {"Build", "400.00"}} {
		if r := e.call(http.MethodPost, "/v1/project-milestones", acc, map[string]any{"project_id": pid, "name": m.name, "amount": m.amount}, "Idempotency-Key", uuid.NewString()); r.Status != 201 {
			t.Fatalf("a milestone: %d %s", r.Status, r.Raw)
		}
	}
	url := "/v1/projects/" + pid + "/revenue"
	elect := func(who string, body map[string]any, tag ...string) response {
		h := []string{"Idempotency-Key", uuid.NewString()}
		if len(tag) > 0 {
			h = append(h, "If-Match", tag[0])
		}
		return e.call(http.MethodPut, url+"-election", who, body, h...)
	}
	// Nothing is elected: milestones, and no figures.
	if g := e.call(http.MethodGet, url, acc, nil); g.Status != 200 || g.str("election", "method") != "milestone" || g.Body["position"] != nil {
		t.Fatalf("the default: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, url+"-journals", acc, map[string]any{"month": "2026-10-01"}, "Idempotency-Key", uuid.NewString()), "proposeProjectRevenueJournal", 422, projectrevenue.CodeProject)
	// Only those who post journals elect; an estimate is needed and above nothing.
	if r := elect(e.as(authz.Employee), map[string]any{"method": "over_time", "estimated_cost": "600.00"}); r.Status != 403 {
		t.Errorf("an employee electing: %d %s", r.Status, r.Raw)
	}
	for _, body := range []map[string]any{{"method": "over_time"}, {"method": "over_time", "estimated_cost": "0"}, {"method": "milestone", "estimated_cost": "5.00"}} {
		if r := elect(acc, body); r.Status != 422 || !strings.Contains(r.Raw, projectrevenue.CodeEstimate) {
			t.Errorf("electing with %v: %d %s", body, r.Status, r.Raw)
		}
	}
	el := elect(acc, map[string]any{"method": "over_time", "estimated_cost": "600.00"})
	if el.Status != 200 || el.str("election", "estimated_cost", "amount") != "600.00" || el.Header.Get("ETag") != `"1"` {
		t.Fatalf("electing: %d %s", el.Status, el.Raw)
	}
	e.problemIs(elect(acc, map[string]any{"method": "over_time", "estimated_cost": "700.00"}), "setProjectRevenueElection", 428, CodeIfMatchRequired)

	// 400.00 invoiced (revenue tagged to the project) and 300.00 spent on it, posted on 20 Oct.
	bank, sales, rent := e.books.Bank.ID.String(), e.books.Sales.ID.String(), e.books.Rent.ID.String()
	post := func(debit, credit, amount string) {
		line := func(acct, amt string) map[string]any {
			return map[string]any{"account_id": acct, "amount": map[string]any{"amount": amt, "currency": "SGD"}, "dimension_value_ids": []string{pid}}
		}
		r := e.call(http.MethodPost, "/v1/journals", acc, map[string]any{"date": "2026-10-14", "memo": "x", "lines": []map[string]any{line(debit, amount), line(credit, "-"+amount)}})
		if r.Status != 201 {
			t.Fatalf("a journal: %d %s", r.Status, r.Raw)
		}
		if x := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", acc, nil, "If-Match", r.Header.Get("ETag")); x.Status != 200 {
			t.Fatalf("posting: %d %s", x.Status, x.Raw)
		}
	}
	post(bank, sales, "400.00")
	post(rent, bank, "300.00")
	g := e.call(http.MethodGet, url+"?month=2026-10-01", acc, nil)
	pos, _ := g.Body["position"].(map[string]any)
	amount := func(k string) any { return pos[k].(map[string]any)["amount"] }
	if g.Status != 200 || pos == nil || pos["progress"] != "50.0" || amount("fee") != "1000.00" || amount("cost_to_date") != "300.00" || amount("earned") != "500.00" ||
		amount("invoiced") != "400.00" || amount("held") != "400.00" || amount("adjustment") != "100.00" || amount("contract_asset") != "100.00" || amount("contract_liability") != "0.00" {
		t.Fatalf("the month's figures: %d %s", g.Status, g.Raw)
	}
	// Costs that do not show progress are left out with their reason: 100.00 of the 300.00 posted leaves 200.00, a third of the estimate, so 333.33 is earned and the journal would book
	// minus 66.67 against the 400.00 held. Without a reason, or with more than was posted, they are refused; and leaving none out puts the figures back.
	tag := g.Header.Get("ETag")
	reelect := func(body map[string]any) response { return elect(acc, body, tag) }
	if r := reelect(map[string]any{"method": "over_time", "estimated_cost": "600.00", "excluded_cost": "100.00"}); r.Status != 422 || !strings.Contains(r.Raw, "excluded_reason") {
		t.Errorf("costs left out with no reason: %d %s", r.Status, r.Raw)
	}
	left := reelect(map[string]any{"method": "over_time", "estimated_cost": "600.00", "excluded_cost": "100.00", "excluded_reason": "Rework of a drawing the customer had approved"})
	if left.Status != 200 || left.str("election", "excluded_cost", "amount") != "100.00" {
		t.Fatalf("leaving costs out: %d %s", left.Status, left.Raw)
	}
	cut := e.call(http.MethodGet, url+"?month=2026-10-01", acc, nil).Body["position"].(map[string]any)
	cutAmount := func(k string) any { return cut[k].(map[string]any)["amount"] }
	if cut["progress"] != "33.3" || cutAmount("costs_posted") != "300.00" || cutAmount("costs_left_out") != "100.00" || cutAmount("cost_to_date") != "200.00" || cutAmount("earned") != "333.33" || cutAmount("adjustment") != "-66.67" {
		t.Errorf("with costs left out: %v", cut)
	}
	if r := elect(acc, map[string]any{"method": "over_time", "estimated_cost": "600.00", "excluded_cost": "400.00", "excluded_reason": "More than was spent"}, left.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("electing with too much left out: %d %s", r.Status, r.Raw)
	}
	if bad := e.call(http.MethodGet, url+"?month=2026-10-01", acc, nil); !strings.Contains(bad.str("problem"), "are more than") || bad.Body["position"] != nil {
		t.Errorf("more left out than posted: %s", bad.Raw)
	}
	back := elect(acc, map[string]any{"method": "over_time", "estimated_cost": "600.00"}, e.call(http.MethodGet, url, acc, nil).Header.Get("ETag"))
	if back.Status != 200 || back.Body["election"].(map[string]any)["excluded_cost"] != nil {
		t.Fatalf("leaving none out again: %d %s", back.Status, back.Raw)
	}
	el = back
	// A month that has not begun, and one that is not a first day, are refused with their reason.
	if f := e.call(http.MethodGet, url+"?month=2026-12-01", acc, nil); f.str("problem") == "" || f.Body["position"] != nil {
		t.Errorf("a month that has not begun: %s", f.Raw)
	}
	propose := func(month string) response {
		return e.call(http.MethodPost, url+"-journals", acc, map[string]any{"month": month}, "Idempotency-Key", uuid.NewString())
	}
	e.problemIs(propose("2026-10-02"), "proposeProjectRevenueJournal", 422, projectrevenue.CodeMonth)
	// The journal's accounts must be set up: without 4030 it is refused and nothing is made.
	made := propose("2026-10-01")
	if made.Status != 201 || made.str("run", "adjustment", "amount") != "100.00" || made.str("figures", "contract_asset", "amount") != "100.00" {
		t.Fatalf("proposing: %d %s", made.Status, made.Raw)
	}
	e.problemIs(propose("2026-10-01"), "proposeProjectRevenueJournal", 422, projectrevenue.CodeMonth)
	j := e.call(http.MethodGet, "/v1/journals/"+made.str("run", "journal_id"), acc, nil)
	if j.Status != 200 || j.str("status") != "draft" || len(j.Body["lines"].([]any)) != 2 {
		t.Fatalf("the proposed journal: %d %s", j.Status, j.Raw)
	}
	for _, l := range j.Body["lines"].([]any) {
		if v, _ := l.(map[string]any)["dimension_value_ids"].([]any); len(v) != 1 || v[0] != pid {
			t.Errorf("a line without the project: %v", l)
		}
	}
	// Nothing is posted until a person posts it: the ledger still holds 400.00, and a later read shows the run.
	if g := e.call(http.MethodGet, url+"?month=2026-10-01", acc, nil); len(g.Body["runs"].([]any)) != 1 || g.Body["position"].(map[string]any)["held"].(map[string]any)["amount"] != "400.00" {
		t.Errorf("after proposing: %s", g.Raw)
	}
	if x := e.call(http.MethodPost, "/v1/journals/"+made.str("run", "journal_id")+"/post", acc, nil, "If-Match", j.Header.Get("ETag")); x.Status != 200 {
		t.Fatalf("posting the proposal: %d %s", x.Status, x.Raw)
	}
	// Posted, the month is worked out again from the ledger: 500.00 held, 400.00 invoiced, nothing more to book.
	after := e.call(http.MethodGet, url+"?month=2026-10-01", acc, nil)
	ap := after.Body["position"].(map[string]any)
	if ap["held"].(map[string]any)["amount"] != "500.00" || ap["invoiced"].(map[string]any)["amount"] != "400.00" || ap["adjustment"].(map[string]any)["amount"] != "0.00" || ap["nothing_to_propose"] != true {
		t.Errorf("after posting: %s", after.Raw)
	}
	// A project that is not elected, or whose election is back to milestones, has no figures.
	if back := elect(acc, map[string]any{"method": "milestone"}, el.Header.Get("ETag")); back.Status != 200 || back.str("election", "method") != "milestone" {
		t.Errorf("back to milestones: %d %s", back.Status, back.Raw)
	}
}

// matrixProjectRevenue are the authorisation matrix's rows for revenue over time.
var matrixProjectRevenue = map[string]func(e *env, tok string) (response, func() bool){
	"getProjectRevenue": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		return e.call(http.MethodGet, "/v1/projects/"+uuid.NewString()+"/revenue", tok, nil), nil
	},
	"setProjectRevenueElection": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		return e.call(http.MethodPut, "/v1/projects/"+uuid.NewString()+"/revenue-election", tok, map[string]any{"method": "milestone"}, "Idempotency-Key", uuid.NewString()), nil
	},
	"proposeProjectRevenueJournal": func(e *env, tok string) (response, func() bool) {
		e.projectsOn()
		return e.call(http.MethodPost, "/v1/projects/"+uuid.NewString()+"/revenue-journals", tok, map[string]any{"month": "2026-10-01"}, "Idempotency-Key", uuid.NewString()), nil
	},
}

// FR-PRJ-11: a fixed-fee project in another currency is worked in the base currency at the rate of the month's day: its fee (the milestones) and its estimate convert under RND-FX, the costs and
// the revenue held are already in the base currency, so progress is one ratio. Without a rate for the day the month is refused in plain words.
// req: FR-PRJ-11
func TestAForeignCurrencyProjectIsWorkedAtTheRateOfTheMonth(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	customer := e.customer()
	e.revenueBooks()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Usd", "approver")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "REV-USD", "name": "Fixed in dollars", "currency": "USD", "billing_method": "fixed_fee", "customer_id": customer,
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	for _, m := range []struct{ name, amount string }{{"Design", "480.00"}, {"Build", "320.00"}} {
		if r := e.call(http.MethodPost, "/v1/project-milestones", acc, map[string]any{"project_id": pid, "name": m.name, "amount": m.amount}, "Idempotency-Key", uuid.NewString()); r.Status != 201 {
			t.Fatalf("a milestone: %d %s", r.Status, r.Raw)
		}
	}
	url := "/v1/projects/" + pid + "/revenue"
	if r := e.call(http.MethodPut, url+"-election", acc, map[string]any{"method": "over_time", "estimated_cost": "480.00"}, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("electing: %d %s", r.Status, r.Raw)
	}
	bank, sales, rent := e.books.Bank.ID.String(), e.books.Sales.ID.String(), e.books.Rent.ID.String()
	post := func(debit, credit, amount string) {
		line := func(acct, amt string) map[string]any {
			return map[string]any{"account_id": acct, "amount": map[string]any{"amount": amt, "currency": "SGD"}, "dimension_value_ids": []string{pid}}
		}
		r := e.call(http.MethodPost, "/v1/journals", acc, map[string]any{"date": "2026-10-14", "memo": "x", "lines": []map[string]any{line(debit, amount), line(credit, "-"+amount)}})
		if x := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", acc, nil, "If-Match", r.Header.Get("ETag")); r.Status != 201 || x.Status != 200 {
			t.Fatalf("a journal: %d %s / %d %s", r.Status, r.Raw, x.Status, x.Raw)
		}
	}
	post(bank, sales, "400.00")
	post(rent, bank, "300.00")
	// No USD rate: the month is refused with what to do.
	if g := e.call(http.MethodGet, url+"?month=2026-10-01", acc, nil); g.Body["position"] != nil || !strings.Contains(g.str("problem"), "no USD exchange rate") {
		t.Fatalf("with no rate: %s", g.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/fx/rates", acc, map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-15", "quote": "0.8", "quote_form": "foreign-per-base", "note": "Bank's rate"}); r.Status != http.StatusCreated {
		t.Fatalf("the USD rate: %d %s", r.Status, r.Raw)
	}
	g := e.call(http.MethodGet, url+"?month=2026-10-01", acc, nil)
	pos, _ := g.Body["position"].(map[string]any)
	amount := func(k string) any { return pos[k].(map[string]any)["amount"] }
	conv, _ := pos["converted_at"].(map[string]any)
	if g.Status != 200 || pos == nil || conv["currency"] != "USD" || conv["rate"] != "1.25" || amount("fee") != "1000.00" || amount("estimated_cost") != "600.00" || pos["progress"] != "50.0" ||
		amount("earned") != "500.00" || amount("adjustment") != "100.00" || amount("contract_asset") != "100.00" {
		t.Fatalf("the month at the day's rate: %d %s", g.Status, g.Raw)
	}
	if made := e.call(http.MethodPost, url+"-journals", acc, map[string]any{"month": "2026-10-01"}, "Idempotency-Key", uuid.NewString()); made.Status != 201 {
		t.Errorf("proposing: %d %s", made.Status, made.Raw)
	}
}

// FR-PRJ-11 (rev 0.587): the platform's month-end job proposes the previous month's journal of a project elected for revenue over time as a draft only, through the database's narrow function (the
// worker holds no right to create documents and posts nothing): the draft carries the project on every line and the run is recorded; the next day it proposes nothing more; a project whose month has
// nothing to move or whose election is for milestones is left alone.
// req: FR-PRJ-11
// asvs: V4.2.1
func TestTheMonthEndJobProposesTheDraftAndOnlyTheDraft(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	customer := e.customer()
	e.revenueBooks()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Job", "approver")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "REV-JOB", "name": "Fixed by the job", "currency": "SGD", "billing_method": "fixed_fee", "customer_id": customer,
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	if r := e.call(http.MethodPost, "/v1/project-milestones", acc, map[string]any{"project_id": pid, "name": "All", "amount": "1000.00"}, "Idempotency-Key", uuid.NewString()); r.Status != 201 {
		t.Fatalf("a milestone: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPut, "/v1/projects/"+pid+"/revenue-election", acc, map[string]any{"method": "over_time", "estimated_cost": "600.00"}, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("electing: %d %s", r.Status, r.Raw)
	}
	bank, sales, rent := e.books.Bank.ID.String(), e.books.Sales.ID.String(), e.books.Rent.ID.String()
	for _, j := range []struct{ debit, credit, amount string }{{bank, sales, "400.00"}, {rent, bank, "300.00"}} {
		line := func(acct, amt string) map[string]any {
			return map[string]any{"account_id": acct, "amount": map[string]any{"amount": amt, "currency": "SGD"}, "dimension_value_ids": []string{pid}}
		}
		r := e.call(http.MethodPost, "/v1/journals", acc, map[string]any{"date": "2026-10-14", "memo": "x", "lines": []map[string]any{line(j.debit, j.amount), line(j.credit, "-"+j.amount)}})
		if x := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", acc, nil, "If-Match", r.Header.Get("ETag")); r.Status != 201 || x.Status != 200 {
			t.Fatalf("a journal: %d %s / %d %s", r.Status, r.Raw, x.Status, x.Raw)
		}
	}
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	job := func(on time.Time) int {
		t.Helper()
		n, err := projectrevenue.MonthEndIn(context.Background(), worker, clock.NewFrozen(on), e.books.Org)
		if err != nil {
			t.Fatalf("the job on %s: %v", on.Format(time.DateOnly), err)
		}
		return n
	}
	// Within October nothing has ended; on 2 November October is proposed once.
	if n := job(porttest.Day(10, 20)); n != 0 {
		t.Errorf("in the month: %d drafts", n)
	}
	if n := job(porttest.Day(11, 2)); n != 1 {
		t.Fatalf("on 2 November: %d drafts, want 1", n)
	}
	if n := job(porttest.Day(11, 3)); n != 0 {
		t.Errorf("the next day: %d drafts", n)
	}
	g := e.call(http.MethodGet, "/v1/projects/"+pid+"/revenue?month=2026-10-01", acc, nil)
	runs, _ := g.Body["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["adjustment"].(map[string]any)["amount"] != "100.00" {
		t.Fatalf("the run: %s", g.Raw)
	}
	j := e.call(http.MethodGet, "/v1/journals/"+runs[0].(map[string]any)["journal_id"].(string), acc, nil)
	if j.Status != 200 || j.str("status") != "draft" || len(j.Body["lines"].([]any)) != 2 {
		t.Fatalf("the draft: %d %s", j.Status, j.Raw)
	}
	for _, l := range j.Body["lines"].([]any) {
		if v, _ := l.(map[string]any)["dimension_value_ids"].([]any); len(v) != 1 || v[0] != pid {
			t.Errorf("a line without the project: %v", l)
		}
	}
}
