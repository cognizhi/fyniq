//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-PRJ-12: a project's revenue and direct costs come from posted ledger lines through the balance service, staff cost only as
// approved time at the viewer's tier, and salary tagged to the project is listed apart and never counted twice (G4-049); every
// project plus the untagged lines equals the ledger's revenue and expense totals (QA-RPT-01); a person's cost appears only for
// the payroll tier (G4-050); utilisation is billable over available hours.
// req: FR-PRJ-12
// req: QA-RPT-01
// req: G4-049
// req: G4-050
func TestProjectReportsComeFromThePostedLedgerAndApprovedTime(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Rep", "approver")
	una := e.joinedIn(e.books.Org, "Una Rep", "controller")
	priya := e.joinedIn(e.books.Org, "Priya Rep", "employee")
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "REP-1", "name": "Report", "currency": "SGD", "billing_method": "time_and_materials", "budget_minutes": 600,
		"people": []map[string]any{{"user_id": wei.String(), "manager": true}, {"user_id": priya.String()}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	// The project's value is on the Project dimension: lines tag it.
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	e.call(http.MethodPost, "/v1/billing-rates", acc, map[string]any{"project_id": pid, "effective_from": "2026-01-01", "hourly_rate": "150"}, "Idempotency-Key", uuid.NewString())
	// Priya logs 3 hours on Tuesday 13 Oct 2026; the Controller approves.
	tok := e.person(priya, authz.Employee)
	e.saveWeek(tok, "2026-10-12", map[string]any{"project_id": pid, "task_id": task, "work_date": "2026-10-13", "minutes": 180})
	_, tag := e.weekOf(tok, "2026-10-12")
	s := e.call(http.MethodPost, "/v1/timesheet/2026-10-12/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
	ctl := e.person(una, authz.Controller)
	etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), ctl, nil).Header.Get("ETag")
	if r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", ctl, map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}
	e.call(http.MethodPost, "/v1/cost-rates", e.as(authz.Owner), map[string]any{"effective_from": "2026-01-01", "hourly_rate": "70"}, "Idempotency-Key", uuid.NewString())
	e.call(http.MethodPost, "/v1/cost-rates", e.as(authz.Owner), map[string]any{"user_id": priya.String(), "role": "Consultant", "effective_from": "2026-01-01", "hourly_rate": "60"}, "Idempotency-Key", uuid.NewString())

	// Posted lines: revenue and rent tagged to the project, salary tagged to it on a payroll-cost account, and revenue tagged to none.
	var salary storage.Account
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		salary, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "6020", Name: "Salaries", Type: storage.Expense, PayrollCost: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	post := func(debit, credit, amount string, tagged bool) {
		line := func(acct string, amt string) map[string]any {
			m := map[string]any{"account_id": acct, "amount": map[string]any{"amount": amt, "currency": "SGD"}}
			if tagged {
				m["dimension_value_ids"] = []string{pid}
			}
			return m
		}
		r := e.call(http.MethodPost, "/v1/journals", acc, map[string]any{"date": "2026-10-20", "memo": "x", "lines": []map[string]any{line(debit, amount), line(credit, "-"+amount)}})
		if r.Status != 201 {
			t.Fatalf("a journal: %d %s", r.Status, r.Raw)
		}
		if x := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", acc, nil, "If-Match", r.Header.Get("ETag")); x.Status != 200 {
			t.Fatalf("posting: %d %s", x.Status, x.Raw)
		}
	}
	bank, sales, rent := e.books.Bank.ID.String(), e.books.Sales.ID.String(), e.books.Rent.ID.String()
	post(bank, sales, "1000.00", true)
	post(rent, bank, "300.00", true)
	post(salary.ID.String(), bank, "1500.00", true)
	post(bank, sales, "500.00", false)

	report := "/v1/reports/projects?from=2026-10-01&to=2026-10-31"
	row := func(r response) map[string]any {
		for _, x := range r.Body["projects"].([]any) {
			if m := x.(map[string]any); m["code"] == "REP-1" {
				return m
			}
		}
		t.Fatalf("no row for the project: %s", r.Raw)
		return nil
	}
	// The Controller: time at the standard rate (one person in the role), so 3 h × 70 = 210.00.
	c := e.call(http.MethodGet, report, e.as(authz.Controller), nil)
	r := row(c)
	if c.Status != 200 || r["revenue"] != "1000.00" || r["direct_costs"] != "300.00" || r["payroll_tagged"] != "1500.00" || r["time_cost"] != "210.00" || r["margin"] != "490.00" ||
		r["rate_basis"] != "standard" || r["minutes"] != float64(180) || r["standard_value"] != "450.00" || r["realisation"] != "222.2" || r["budget_used"] != "30.0" {
		t.Fatalf("the Controller's row: %s", c.Raw)
	}
	// G4-049: the salary tagged to the project is on its own line, and the staff cost is the time at cost once.
	// QA-RPT-01: the project plus the untagged lines is the ledger's revenue (1500.00) and expense (1800.00) totals.
	u := c.Body["untagged"].(map[string]any)
	if u["revenue"] != "500.00" || u["direct_costs"] != "0.00" || u["payroll_tagged"] != "0.00" {
		t.Errorf("the untagged lines: %v", u)
	}
	// The Owner sees each person's own rate: 3 h × 60 = 180.00, and the person's cost.
	o := e.call(http.MethodGet, report, e.as(authz.Owner), nil)
	if r := row(o); r["time_cost"] != "180.00" || r["margin"] != "520.00" || r["rate_basis"] != "person" {
		t.Errorf("the Owner's row: %v", r)
	}
	if ppl := o.Body["people"].([]any); len(ppl) != 1 || ppl[0].(map[string]any)["cost"] != "180.00" || ppl[0].(map[string]any)["utilisation"] == "" {
		t.Errorf("the Owner's people: %s", o.Raw)
	}
	if ppl := c.Body["people"].([]any); ppl[0].(map[string]any)["cost"] != nil {
		t.Errorf("the Controller is given a person's cost: %s", c.Raw)
	}
	// The manager sees their project but not the company-wide untagged lines; the AR specialist hours and revenue only.
	m := e.call(http.MethodGet, report, e.person(wei, authz.Approver), nil)
	if len(m.Body["projects"].([]any)) != 1 || m.Body["untagged"] != nil {
		t.Errorf("the manager: %s", m.Raw)
	}
	ar := e.call(http.MethodGet, report, e.as(authz.ARSpecialist), nil)
	if r := row(ar); r["revenue"] != "1000.00" || r["time_cost"] != nil || r["margin"] != nil || r["direct_costs"] != nil || r["minutes"] != float64(180) || ar.Body["costs_visible"] != false {
		t.Errorf("the AR specialist: %v", r)
	}
	e.problemIs(e.call(http.MethodGet, report, e.as(authz.Employee), nil), "getProjectReport", 403, CodeForbidden)
	// The drill: the entries behind the hours.
	d := e.call(http.MethodGet, "/v1/projects/"+pid+"/time-entries?from=2026-10-01&to=2026-10-31", e.as(authz.Controller), nil)
	if d.Status != 200 || len(d.Body["items"].([]any)) != 1 || d.Body["items"].([]any)[0].(map[string]any)["minutes"] != float64(180) {
		t.Errorf("the drill: %s", d.Raw)
	}
}
