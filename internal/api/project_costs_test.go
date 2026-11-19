//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

// FR-PRJ-08: a bill's or a claim's line that carries a project can be billable with a markup; a line without a project is refused with a
// plain message, a markup needs a billable line and a percentage Fyniq reads; a purchase order's project is carried to the bill made from
// it as a project value on each line.
// req: FR-PRJ-08
// req: RND-MARKUP
func TestBillableCostsNeedAProjectAndAPurchaseOrderCarriesItsProject(t *testing.T) {
	e := newEnv(t)
	e.projectsOn()
	acc := e.as(authz.Accountant)
	p := e.call(http.MethodPost, "/v1/projects", acc, map[string]any{"code": "COST-1", "name": "Costs", "currency": "SGD", "billing_method": "time_and_materials",
		"people": []map[string]any{{"user_id": e.joinedIn(e.books.Org, "Wei Cost", "approver").String(), "manager": true}}})
	if p.Status != 201 {
		t.Fatalf("the project: %d %s", p.Status, p.Raw)
	}
	pid := p.str("id")
	line := func(extra map[string]any) map[string]any {
		d := e.billDraft("420.00", "0.00")
		l := d["lines"].([]map[string]any)[0]
		for k, v := range extra {
			l[k] = v
		}
		return d
	}
	post := func(body map[string]any) response { return e.call(http.MethodPost, "/v1/bills", acc, body) }
	// A billable line with no project, and a markup on a line that is not billable, are refused, naming the line and the fix.
	r := post(line(map[string]any{"billable": true, "markup_percent": "10"}))
	if r.Status != 422 || !strings.Contains(r.Raw, "PRJ-11") || !strings.Contains(r.Raw, "body.lines[0].billable") {
		t.Fatalf("a billable line with no project: %d %s", r.Status, r.Raw)
	}
	if r := post(line(map[string]any{"markup_percent": "10", "dimension_value_ids": []string{pid}})); r.Status != 422 || !strings.Contains(r.Raw, "PRJ-10") {
		t.Errorf("a markup on a line that is not billable: %d %s", r.Status, r.Raw)
	}
	if r := post(line(map[string]any{"billable": true, "markup_percent": "10%", "dimension_value_ids": []string{pid}})); r.Status != 422 {
		t.Errorf("a markup that is not a percentage: %d %s", r.Status, r.Raw)
	}
	// With the project it is saved, and the line comes back as entered.
	ok := post(line(map[string]any{"billable": true, "markup_percent": "10", "dimension_value_ids": []string{pid}}))
	if ok.Status != 201 {
		t.Fatalf("a billable line with a project: %d %s", ok.Status, ok.Raw)
	}
	l := ok.Body["lines"].([]any)[0].(map[string]any)
	if l["billable"] != true || l["markup_percent"] != "10" {
		t.Errorf("the line as saved: %v", l)
	}

	// A purchase order for the project: each bill line made from it carries the project.
	od := e.orderDraft("3", "33.333333")
	od["project_id"] = pid
	o := e.call(http.MethodPost, "/v1/purchase-orders", acc, od)
	if o.Status != 201 || o.str("project_id") != pid {
		t.Fatalf("the order: %d %s", o.Status, o.Raw)
	}
	if bad := e.orderDraft("3", "33.333333"); true {
		bad["project_id"] = uuid.NewString()
		if r := e.call(http.MethodPost, "/v1/purchase-orders", acc, bad); r.Status != 422 || !strings.Contains(r.Raw, "PRJ-11") {
			t.Errorf("an order for a project that is not one: %d %s", r.Status, r.Raw)
		}
	}
	ap := e.call(http.MethodPost, "/v1/purchase-orders/"+o.str("id")+"/approve", acc, nil, "If-Match", o.Header.Get("ETag"))
	if ap.Status != 200 {
		t.Fatalf("approving: %d %s", ap.Status, ap.Raw)
	}
	b := e.call(http.MethodPost, "/v1/purchase-orders/"+o.str("id")+"/bill", acc, nil, "If-Match", ap.Header.Get("ETag"), "Idempotency-Key", uuid.NewString())
	if b.Status != 201 {
		t.Fatalf("the bill from the order: %d %s", b.Status, b.Raw)
	}
	for _, x := range b.Body["lines"].([]any) {
		v, _ := x.(map[string]any)["dimension_value_ids"].([]any)
		if len(v) != 1 || v[0] != pid {
			t.Errorf("a line of the bill from the order: %v", x)
		}
	}

	// The same for a claim's lines.
	c := newClaimants(t)
	c.e.projectsOn()
	travel, _ := c.e.claimSetup()
	proj := c.e.call(http.MethodPost, "/v1/projects", c.e.as(authz.Accountant), map[string]any{"code": "COST-2", "name": "Claims", "currency": "SGD", "billing_method": "time_and_materials",
		"people": []map[string]any{{"user_id": c.mei, "manager": true}}})
	if proj.Status != 201 {
		t.Fatalf("the claims' project: %d %s", proj.Status, proj.Raw)
	}
	ahkow := c.as(c.ahkow, authz.Employee)
	tx := taxi(travel)
	tx["billable"] = true
	if r := c.e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": []any{tx}}); r.Status != 422 || !strings.Contains(r.Raw, "PRJ-11") {
		t.Errorf("a billable receipt with no project: %d %s", r.Status, r.Raw)
	}
	tx["dimension_value_ids"] = []string{proj.str("id")}
	tx["markup_percent"] = "15"
	if r := c.e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": []any{tx}}); r.Status != 201 ||
		r.Body["lines"].([]any)[0].(map[string]any)["markup_percent"] != "15" {
		t.Errorf("a billable receipt with a project: %d %s", r.Status, r.Raw)
	}
}
