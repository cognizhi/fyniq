//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A department-scoped Approver reads the profit and loss as the totals of their department values (FR-ADM-01, rev 0.371): the
// database adds up only the lines of the values their role is narrowed to; with no scope they add up none; the balance
// sheet, the cash flow, the trial balance and a budget comparative, which the department filter does not reach, are refused,
// and so is an export; the whole company's view is the Accountant's.
// req: FR-ADM-01
// req: SEC-34
// asvs: V8.2.1, V8.3.1, V8.2.2
func TestADepartmentsApproverReadsTheirDepartmentsProfitAndLoss(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"})
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "ENG", "name": "Engineering"})
	ops := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "OPS", "name": "Operations"})
	post := func(amount string, value string) {
		t.Helper()
		d := e.draft(amount)
		d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{value}
		r := e.call(http.MethodPost, "/v1/journals", accountant, d)
		if r.Status != http.StatusCreated {
			t.Fatalf("drafting: %d %s", r.Status, r.Raw)
		}
		if p := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag")); p.Status != http.StatusOK {
			t.Fatalf("posting: %d %s", p.Status, p.Raw)
		}
	}
	post("100.00", eng.str("id"))
	post("40.00", ops.str("id"))

	const pl = "/v1/reports/statements/pl?from=2026-10&to=2026-10"
	mei := c.as(c.mei, authz.Approver, authz.Employee)
	full := e.call(http.MethodGet, pl, accountant, nil)
	if full.Status != 200 || !strings.Contains(full.Raw, "140.00") {
		t.Fatalf("the Accountant's P&L: %d %s", full.Status, full.Raw)
	}
	// No scope reaches none: the Approver's role was given with no department.
	none := e.call(http.MethodGet, pl, mei, nil)
	if none.Status != 200 || strings.Contains(none.Raw, "100.00") || strings.Contains(none.Raw, "140.00") || strings.Contains(none.Raw, "40.00") {
		t.Fatalf("a department's view with no scope: %d %s", none.Status, none.Raw)
	}
	// Narrow the role to Engineering: only its lines add up.
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ms, err := u.Members().List(ctx, e.clock.Now())
		if err != nil {
			return err
		}
		for _, m := range ms {
			if m.UserID.String() != c.mei {
				continue
			}
			for _, r := range m.Roles {
				if r.Role == "approver" {
					return u.Members().Narrow(ctx, r.ID, []uuid.UUID{uuid.MustParse(eng.str("id"))})
				}
			}
		}
		t.Fatal("Mei holds no Approver role")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scoped := e.call(http.MethodGet, pl, mei, nil)
	if scoped.Status != 200 || !strings.Contains(scoped.Raw, "100.00") || strings.Contains(scoped.Raw, "140.00") || strings.Contains(scoped.Raw, `"40.00"`) {
		t.Fatalf("a department's view narrowed to Engineering: %d %s", scoped.Status, scoped.Raw)
	}
	// What the department filter does not reach is refused.
	for _, path := range []string{"/v1/reports/statements/bs?to=2026-10", "/v1/reports/statements/cf?from=2026-10&to=2026-10", "/v1/reports/statements/tb?to=2026-10",
		pl + "&compare=budget", "/v1/reports/statements/pl/export?from=2026-10&to=2026-10&format=csv"} {
		if r := e.call(http.MethodGet, path, mei, nil); r.Status != http.StatusForbidden {
			t.Errorf("%s as a department's Approver: %d %s", path, r.Status, r.Raw)
		}
	}
	if r := e.call(http.MethodGet, pl+"&compare=prior_period", mei, nil); r.Status != 200 {
		t.Errorf("a prior-period comparative: %d %s", r.Status, r.Raw)
	}
}
