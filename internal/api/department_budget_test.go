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
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// narrowApprover gives the member whose membership is membership an Approver role narrowed to values, as the role change does
// once approved, and returns their user ID.
func (e *env) narrowApprover(membership string, values ...string) string {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var user string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ms, err := u.Members().List(ctx, e.clock.Now())
		if err != nil {
			return err
		}
		for _, m := range ms {
			if m.ID.String() != membership {
				continue
			}
			user = m.UserID.String()
			a, err := u.Members().Assign(ctx, m.ID, "approver", nil)
			if err != nil {
				return err
			}
			var ids []uuid.UUID
			for _, v := range values {
				ids = append(ids, uuid.MustParse(v))
			}
			return u.Members().Narrow(ctx, a.ID, ids)
		}
		e.t.Fatal("no such member")
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return user
}

// A department's budget holder (Appendix D's Approver, "C dept" on Budgets) reads and plans the lines of their department's values and
// nothing else of the company's budget (FR-ADM-01, rev 0.381): the draft they read has their department's lines and their totals
// alone; a save of theirs replaces only those lines and keeps every other line as it was; a line outside their departments, a
// company line, or a changed name is refused whole; creating, copying, importing or asking for approval, the forecasts and the
// company's planning summary are refused; and Budget vs Actual adds up their department's budget and actuals only. An Approver
// role with no department reaches none of it.
// req: FR-ADM-01
// req: FR-PLN-01
// req: FR-RPT-03
// asvs: V8.2.1, V8.3.1, V8.2.2
func TestADepartmentsBudgetHolderPlansTheirDepartmentsLines(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.booksFrom()
	e.clock.Set(time.Date(2026, 12, 5, 9, 0, 0, 0, time.UTC))
	c := e.budgetChart()
	accountant := f.as(f.ahkow, authz.Accountant)
	owner := f.as(f.weiling, authz.Owner)

	body := func(salesAds string) map[string]any {
		return map[string]any{"name": "FY2026 budget", "kind": "original", "financial_year": "2026-10-01", "lines": []any{
			map[string]any{"account_id": c.revenue, "amounts": []string{"10000.00", "10000.00", "10000.00"}},
			map[string]any{"account_id": c.ads, "dimension_value_ids": []string{c.sales}, "amounts": []string{salesAds, "2000.00", "2000.00"}},
			map[string]any{"account_id": c.rent, "dimension_value_ids": []string{c.eng}, "amounts": []string{"3000.00", "3000.00", "3000.00"}},
			map[string]any{"account_id": c.ads, "amounts": []string{"500.00", "500.00", "500.00"}},
		}}
	}
	d := e.call(http.MethodPost, "/v1/budgets", accountant, body("2000.00"))
	if d.Status != http.StatusCreated {
		t.Fatalf("the draft: %d %s", d.Status, d.Raw)
	}
	id := d.str("id")
	path := "/v1/budgets/" + id
	lines := func(r response) []any { return r.Body["budget"].(map[string]any)["lines"].([]any) }

	// Third holds an Approver role with no department: it reaches none of the budget.
	user := e.narrowApprover(f.third) // narrowed to nothing: the role is given with no scope
	holder := f.as(user, authz.Approver)
	if r := e.call(http.MethodGet, path, holder, nil); r.Status != 200 || len(lines(r)) != 0 {
		t.Fatalf("an Approver role with no department reads %d %s", r.Status, r.Raw)
	}
	if err := e.store.Do(storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}}), e.books.Org,
		func(ctx context.Context, u storage.UnitOfWork) error {
			ms, err := u.Members().List(ctx, e.clock.Now())
			if err != nil {
				return err
			}
			for _, m := range ms {
				if m.UserID.String() != user {
					continue
				}
				for _, r := range m.Roles {
					if r.Role == "approver" {
						return u.Members().Revoke(ctx, r.ID)
					}
				}
			}
			return nil
		}); err != nil {
		t.Fatal(err)
	}
	// The same person, narrowed to Sales by a role given again.
	user = e.narrowApprover(f.third, c.sales)
	holder = f.as(user, authz.Approver)

	// They read their department's lines and the totals of those lines.
	g := e.call(http.MethodGet, path, holder, nil)
	if g.Status != 200 || len(lines(g)) != 1 || lines(g)[0].(map[string]any)["dimension_value_ids"].([]any)[0] != c.sales {
		t.Fatalf("the budget a department's holder reads: %d %s", g.Status, g.Raw)
	}
	if strings.Contains(g.Raw, c.revenue) || strings.Contains(g.Raw, c.eng) || !strings.Contains(g.Raw, "6000.00") || strings.Contains(g.Raw, "9500.00") {
		t.Errorf("the budget leaks another line or totals the company: %s", g.Raw)
	}
	l := e.call(http.MethodGet, "/v1/budgets", holder, nil)
	if l.Status != 200 || len(l.Body["items"].([]any)) != 1 || len(lines(response{Body: l.Body["items"].([]any)[0].(map[string]any)})) != 1 {
		t.Errorf("the list a department's holder reads: %d %s", l.Status, l.Raw)
	}

	// They plan their lines: only those are replaced; the company's, Engineering's and every other line stay.
	tag := g.Header.Get("ETag")
	mine := map[string]any{"name": "FY2026 budget", "kind": "original", "financial_year": "2026-10-01", "lines": []any{
		map[string]any{"account_id": c.ads, "dimension_value_ids": []string{c.sales}, "amounts": []string{"2500.00", "2500.00", "2500.00"}},
	}}
	if r := e.call(http.MethodPut, path, holder, mine, "If-Match", tag); r.Status != 200 || len(lines(r)) != 1 {
		t.Fatalf("a department's holder saving their lines: %d %s", r.Status, r.Raw)
	}
	full := e.call(http.MethodGet, path, accountant, nil)
	if len(lines(full)) != 4 || !strings.Contains(full.Raw, "2500.00") || !strings.Contains(full.Raw, c.revenue) || !strings.Contains(full.Raw, c.eng) {
		t.Fatalf("the Accountant reads after the holder saved: %s", full.Raw)
	}
	tag = full.Header.Get("ETag")
	// Outside their departments, a company line, another name: each refused, and nothing is saved.
	for what, bad := range map[string]map[string]any{
		"an Engineering line": {"name": "FY2026 budget", "kind": "original", "financial_year": "2026-10-01", "lines": []any{
			map[string]any{"account_id": c.rent, "dimension_value_ids": []string{c.eng}, "amounts": []string{"1.00", "1.00", "1.00"}}}},
		"a company line": {"name": "FY2026 budget", "kind": "original", "financial_year": "2026-10-01", "lines": []any{
			map[string]any{"account_id": c.revenue, "amounts": []string{"1.00", "1.00", "1.00"}}}},
		"another name": {"name": "Mine", "kind": "original", "financial_year": "2026-10-01", "lines": []any{
			map[string]any{"account_id": c.ads, "dimension_value_ids": []string{c.sales}, "amounts": []string{"1.00", "1.00", "1.00"}}}},
	} {
		if r := e.call(http.MethodPut, path, holder, bad, "If-Match", tag); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "BUDGET-10") {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	if after := e.call(http.MethodGet, path, accountant, nil); after.Header.Get("ETag") != tag {
		t.Errorf("a refused save changed the budget")
	}
	// What a holder does not do: draft, copy, import, ask for approval, plan forecasts, and read the company's planning summary.
	for what, r := range map[string]response{
		"drafting":          e.call(http.MethodPost, "/v1/budgets", holder, body("2000.00")),
		"asking approval":   e.call(http.MethodPost, path+"/approval", holder, nil, "If-Match", tag),
		"forecasts":         e.call(http.MethodGet, "/v1/forecasts", holder, nil),
		"planning summary":  e.call(http.MethodGet, "/v1/planning/summary?month=2026-11", holder, nil),
		"cash projection":   e.call(http.MethodGet, "/v1/cash-projection", holder, nil),
		"the budget import": e.call(http.MethodGet, "/v1/imports/budgets/template", holder, nil),
	} {
		if r.Status != http.StatusForbidden && r.Status != http.StatusNotFound {
			t.Errorf("%s as a department's budget holder: %d %s", what, r.Status, r.Raw)
		}
	}

	// Approved, the budget is the year's: Budget vs Actual adds up their department's budget and actuals alone.
	asked := e.call(http.MethodPost, path+"/approval", accountant, nil, "If-Match", tag)
	if r := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); r.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}
	e.plannedBooks(c)
	vs := e.call(http.MethodGet, "/v1/reports/budget-vs-actual?from=2026-10&to=2026-11&by=dimension", holder, nil)
	if vs.Status != 200 {
		t.Fatalf("Budget vs Actual: %d %s", vs.Status, vs.Raw)
	}
	rows := vs.Body["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["code"] != "6110" || strings.Contains(vs.Raw, "3000.00") || strings.Contains(vs.Raw, "12000.00") {
		t.Fatalf("a department's Budget vs Actual: %s", vs.Raw)
	}
	costs := vs.Body["costs"].(map[string]any)
	if costs["actual"] != "4000.00" || costs["budget"] != "5000.00" {
		t.Errorf("their costs %v, want 4000.00 against 5000.00 (Sales advertising only)", costs)
	}
	if rev := vs.Body["income"].(map[string]any); rev["actual"] != "0.00" || rev["budget"] != "0.00" {
		t.Errorf("their income %v, want none", rev)
	}
	// The Accountant's is the company's.
	if all := e.call(http.MethodGet, "/v1/reports/budget-vs-actual?from=2026-10&to=2026-11", accountant, nil); !strings.Contains(all.Raw, "18000.00") {
		t.Errorf("the Accountant's Budget vs Actual: %s", all.Raw)
	}
}
