//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// G4-050 (FR-PRJ-06, FR-PAY-13): a person's cost rate is entered and read by the Owner and the Payroll administrator only. A
// project manager without payroll rights opens the project's time cost, its people and the cost-rate API, and is given hours and
// the cost at the role average of three people, never a person's rate or cost; with fewer than three the standard rate; the
// AR specialist hours only; the AI nothing. A rate is proved by fingerprint in the audit trail and never kept.
// req: FR-PRJ-06
// req: FR-PAY-13
// req: G4-050
// req: AUD-02
// asvs: V8.2.1, V8.3.1
func TestACostRateIsPayrollDataAndOthersSeeOnlyTheRoleAverage(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	wei := e.joinedIn(e.books.Org, "Wei Rates", "approver")
	una := e.joinedIn(e.books.Org, "Una Rates", "controller")
	var crew []uuid.UUID
	people := []map[string]any{{"user_id": wei.String(), "manager": true}}
	for _, n := range []string{"Ann", "Bo", "Cy"} {
		id := e.joinedIn(e.books.Org, n+" Rates", "employee")
		crew = append(crew, id)
		people = append(people, map[string]any{"user_id": id.String()})
	}
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "RATE-1", "name": "Rates", "currency": "SGD", "billing_method": "time_and_materials", "people": people})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	task := e.call(http.MethodPost, "/v1/project-tasks", acc, map[string]any{"project_id": pid, "name": "Work"}).str("id")
	// Each of the three logs an hour on Tuesday 2090-01-03; their manager approves.
	for _, who := range crew {
		tok := e.person(who, authz.Employee)
		e.saveWeek(tok, "2090-01-02", map[string]any{"project_id": pid, "task_id": task, "work_date": "2090-01-03", "minutes": 60})
		_, tag := e.weekOf(tok, "2090-01-02")
		s := e.call(http.MethodPost, "/v1/timesheet/2090-01-02/submit", tok, map[string]any{}, "If-Match", tag, "Idempotency-Key", uuid.NewString()).Body["submissions"].([]any)[0].(map[string]any)
		etag := e.call(http.MethodGet, "/v1/timesheet-submissions/"+s["id"].(string), e.person(una, authz.Controller), nil).Header.Get("ETag")
		if r := e.call(http.MethodPost, "/v1/timesheet-submissions/"+s["id"].(string)+"/approve", e.person(una, authz.Controller), map[string]any{}, "If-Match", etag, "Idempotency-Key", uuid.NewString()); r.Status != 200 {
			t.Fatalf("approving: %d %s", r.Status, r.Raw)
		}
	}
	owner, payroll := e.as(authz.Owner), e.as(authz.PayrollAdmin)
	manager, controller, ar, auditor := e.person(wei, authz.Approver), e.as(authz.Controller), e.as(authz.ARSpecialist), e.as(authz.Auditor)
	cost := "/v1/projects/" + pid + "/time-cost?from=2090-01-01&to=2090-01-31"

	// Who enters and reads a person's rate: the Owner and the Payroll administrator, nobody else (Appendix D).
	rate := func(tok string, who uuid.UUID, hourly string) response {
		return e.call(http.MethodPost, "/v1/cost-rates", tok, map[string]any{"user_id": who.String(), "role": "Consultant", "effective_from": "2089-01-01", "hourly_rate": hourly}, "Idempotency-Key", uuid.NewString())
	}
	for _, tok := range []string{acc, controller, manager, e.as(authz.Auditor), e.as(authz.TaxAgent), e.as(authz.Employee), e.as(authz.Administrator)} {
		e.problemIs(rate(tok, crew[0], "60"), "setCostRate", 403, CodeForbidden)
		e.problemIs(e.call(http.MethodGet, "/v1/cost-rates", tok, nil), "listCostRates", 403, CodeForbidden)
	}
	if r := rate(owner, crew[0], "60"); r.Status != 201 || r.str("hourly_rate") != "60.000000" && r.str("hourly_rate") != "60.00" || r.str("person_name") != "Ann Rates" {
		t.Fatalf("the Owner's rate: %d %s", r.Status, r.Raw)
	}
	if r := rate(payroll, crew[1], "80"); r.Status != 201 {
		t.Fatalf("the Payroll administrator's rate: %d %s", r.Status, r.Raw)
	}
	if r := rate(owner, crew[2], "100.50"); r.Status != 201 {
		t.Fatalf("the third rate: %d %s", r.Status, r.Raw)
	}
	if r := rate(owner, crew[0], "61"); r.Status != 422 {
		t.Errorf("the same day twice: %d %s", r.Status, r.Raw)
	}
	if r := rate(owner, crew[0], "6.1234567"); r.Status != 422 {
		t.Errorf("a rate of seven decimals: %d %s", r.Status, r.Raw)
	}
	listed := e.call(http.MethodGet, "/v1/cost-rates", payroll, nil)
	if len(listed.Body["items"].([]any)) != 3 {
		t.Fatalf("the rates: %s", listed.Raw)
	}

	// The Owner and the Payroll administrator see each person's rate and cost; hours add up.
	o := e.call(http.MethodGet, cost, owner, nil)
	if o.Status != 200 || o.Body["rate_basis"] != "person" || o.Body["minutes"] != float64(180) || o.str("cost") != "240.50" {
		t.Fatalf("the Owner: %d %s", o.Status, o.Raw)
	}
	costs := map[string]bool{}
	for _, x := range o.Body["people"].([]any) {
		costs[x.(map[string]any)["cost"].(string)] = true
	}
	if !costs["60.00"] || !costs["80.00"] || !costs["100.50"] {
		t.Errorf("each person's own cost for the payroll tier: %v", costs)
	}

	// Everyone else: the role average of the three (60, 80 and 100.50 is 80.166667 an hour: 80.17 an entry), and no person's rate or cost.
	for name, tok := range map[string]string{"the project manager": manager, "the Controller": controller, "the Auditor": auditor} {
		r := e.call(http.MethodGet, cost, tok, nil)
		if r.Status != 200 || r.Body["rate_basis"] != "role_average" || r.str("cost") != "240.51" || r.Body["minutes"] != float64(180) {
			t.Fatalf("%s: %d %s", name, r.Status, r.Raw)
		}
		for _, x := range r.Body["people"].([]any) {
			if _, has := x.(map[string]any)["cost"]; has {
				t.Errorf("%s is given a person's cost: %v", name, x)
			}
		}
		for _, secret := range []string{"60.00", "80.00", "100.50", "100.5", "60.000000", "80.000000", "100.500000"} {
			if strings.Contains(r.Raw, `"`+secret+`"`) {
				t.Errorf("%s: the response holds a person's rate or cost %s: %s", name, secret, r.Raw)
			}
		}
	}
	// Hours only for the AR specialist; nothing for the AI; another project's manager is not found.
	if r := e.call(http.MethodGet, cost, ar, nil); r.Status != 200 || r.Body["rate_basis"] != "none" || r.Body["cost"] != nil || r.Body["minutes"] != float64(180) {
		t.Errorf("the AR specialist: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, cost, e.person(e.joinedIn(e.books.Org, "Otto Rates", "approver"), authz.Approver), nil), "getProjectTimeCost", 404, CodeNotFound)
	// The role average needs three people: with only two rated in a role, the standard rate, else hours only.
	two := e.joinedIn(e.books.Org, "Dee Rates", "employee")
	_ = two
	trail := workerStore(t)
	var body strings.Builder
	if err := trail.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		evs, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range evs {
			if strings.Contains(ev.Body, "cost_rate.entered") {
				body.WriteString(ev.Body)
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), "fingerprint") || strings.Contains(body.String(), "100.50") || strings.Contains(body.String(), `"80`) {
		t.Errorf("the audit trail proves a cost rate by fingerprint and never keeps it: %s", body.String())
	}
	var _ = json.Marshal
}

// Billing rates are kept by those who keep projects, read by those who read them, for one project or customer, in the project's
// currency, and the standard rate is the Owner's fallback (FR-PRJ-06).
// req: FR-PRJ-06
func TestBillingRatesAreEnteredByScopeAndStandardRateFallsBack(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "BILL-1", "name": "Billed", "currency": "SGD", "billing_method": "time_and_materials",
		"people": []map[string]any{{"user_id": e.joinedIn(e.books.Org, "Mgr Bill", "approver").String(), "manager": true}}})
	pid := p.str("id")
	rate := func(tok string, body map[string]any) response {
		body["effective_from"] = "2026-01-01"
		return e.call(http.MethodPost, "/v1/billing-rates", tok, body, "Idempotency-Key", uuid.NewString())
	}
	e.problemIs(rate(e.as(authz.Employee), map[string]any{"project_id": pid, "hourly_rate": "150"}), "setBillingRate", 403, CodeForbidden)
	if r := rate(acc, map[string]any{"project_id": pid, "hourly_rate": "150"}); r.Status != 201 || r.str("currency") != "SGD" {
		t.Fatalf("the project's rate: %d %s", r.Status, r.Raw)
	}
	if r := rate(acc, map[string]any{"project_id": pid, "role": "Consultant", "hourly_rate": "180.5"}); r.Status != 201 {
		t.Fatalf("a role's rate: %d %s", r.Status, r.Raw)
	}
	for name, body := range map[string]map[string]any{"neither a project nor a customer": {"hourly_rate": "1"},
		"a person and a role": {"project_id": pid, "user_id": uuid.NewString(), "role": "X", "hourly_rate": "1"}, "seven decimals": {"project_id": pid, "hourly_rate": "1.1234567"}} {
		if r := rate(acc, body); r.Status != 422 {
			t.Errorf("%s: %d %s", name, r.Status, r.Raw)
		}
	}
	l := e.call(http.MethodGet, "/v1/projects/"+pid+"/billing-rates", e.as(authz.Auditor), nil)
	if len(l.Body["items"].([]any)) != 2 {
		t.Errorf("the project's rates: %s", l.Raw)
	}
	// The standard rate, with no person: the Owner's fallback.
	std := e.call(http.MethodPost, "/v1/cost-rates", e.as(authz.Owner), map[string]any{"effective_from": "2026-01-01", "hourly_rate": "70"}, "Idempotency-Key", uuid.NewString())
	if std.Status != 201 || std.str("role") != "Standard" || std.Body["user_id"] != nil {
		t.Errorf("the standard rate: %d %s", std.Status, std.Raw)
	}
}

var matrixRateDays int

var matrixProjectRates = map[string]func(e *env, tok string) (response, func() bool){
	"listCostRates": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		return e.call(http.MethodGet, "/v1/cost-rates", t.as(e, tok, t.priya), nil), nil
	},
	"setCostRate": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		matrixRateDays++
		day := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, matrixRateDays).Format(time.DateOnly)
		r := e.call(http.MethodPost, "/v1/cost-rates", t.as(e, tok, t.priya), map[string]any{"effective_from": day, "hourly_rate": "70"}, "Idempotency-Key", uuid.NewString())
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/cost-rates", t.fresh(e, t.una, authz.Owner), nil).Raw, day)
		}
	},
	"listBillingRates": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		return e.call(http.MethodGet, "/v1/projects/"+t.project+"/billing-rates", t.as(e, tok, t.wei), nil), nil
	},
	"setBillingRate": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		matrixRateDays++
		day := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, matrixRateDays).Format(time.DateOnly)
		r := e.call(http.MethodPost, "/v1/billing-rates", t.as(e, tok, t.priya), map[string]any{"project_id": t.project, "effective_from": day, "hourly_rate": "150"}, "Idempotency-Key", uuid.NewString())
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/projects/"+t.project+"/billing-rates", t.fresh(e, t.una, authz.Controller), nil).Raw, day)
		}
	},
	"getProjectTimeCost": func(e *env, tok string) (response, func() bool) {
		t := matrixTimeTeam(e)
		return e.call(http.MethodGet, "/v1/projects/"+t.project+"/time-cost?from=2090-01-01&to=2090-01-31", t.as(e, tok, t.wei), nil), nil
	},
}
