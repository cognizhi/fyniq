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

// narrowExisting narrows the live Approver role of user to values, as it is when given with a scope.
func (e *env) narrowExisting(user string, values ...string) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
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
					var ids []uuid.UUID
					for _, v := range values {
						ids = append(ids, uuid.MustParse(v))
					}
					return u.Members().Narrow(ctx, r.ID, ids)
				}
			}
		}
		e.t.Fatal("the member holds no Approver role")
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

// An Approver narrowed to departments (FR-ADM-01, rev 0.381) reaches the expense claims, bills and requests of their departments
// only: the claims they list and read, the files of a claim, the bills they list and read, and the requests they see and sign
// are those whose lines carry only their department's values, and a record in no department is in none of theirs; a claim of
// their own is theirs still; the company's payables and a bill's files are the Inbox's, not theirs to browse; and a person
// whose other role reaches the row, or whose Approver role has no scope, reaches what they did. A refused sign leaves the claim
// unposted.
// req: FR-ADM-01
// req: FR-AP-17
// req: SEC-34
// asvs: V8.2.1, V8.3.1, V8.2.2
func TestADepartmentsApproverReachesTheirDepartmentsRecords(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"})
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "ENG", "name": "Engineering"}).str("id")
	ops := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "OPS", "name": "Operations"}).str("id")
	ahkow := c.as(c.ahkow, authz.Employee)
	mei := c.as(c.mei, authz.Approver, authz.Employee)
	meiAsAccountant := c.as(c.mei, authz.Approver, authz.Accountant)

	// Three claims asked for: Engineering's, Operations' and one in no department.
	type claimed struct{ id, request string }
	claimIn := func(value string) claimed {
		t.Helper()
		line := taxi(travel)
		if value != "" {
			line["dimension_value_ids"] = []string{value}
		}
		r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "title": "Trip", "lines": []any{line}})
		if r.Status != http.StatusCreated {
			t.Fatalf("drafting: %d %s", r.Status, r.Raw)
		}
		asked := e.call(http.MethodPost, "/v1/expense-claims/"+r.str("id")+"/submit", ahkow, nil, "If-Match", r.Header.Get("ETag"))
		if asked.Status != http.StatusAccepted {
			t.Fatalf("submitting: %d %s", asked.Status, asked.Raw)
		}
		return claimed{r.str("id"), asked.str("id")}
	}
	inEng, inOps, inNone := claimIn(eng), claimIn(ops), claimIn("")
	count := func(who, path string) int {
		t.Helper()
		r := e.call(http.MethodGet, path, who, nil)
		if r.Status != 200 {
			t.Fatalf("%s: %d %s", path, r.Status, r.Raw)
		}
		return len(r.Body["items"].([]any))
	}

	// With no scope the Approver reaches what they did: all three.
	if n := count(mei, "/v1/expense-claims?whose=everyone"); n != 3 {
		t.Fatalf("an Approver with no scope lists %d claims, want 3", n)
	}
	e.narrowExisting(c.mei, eng)

	// Narrowed to Engineering: its claim alone.
	if n := count(mei, "/v1/expense-claims?whose=everyone"); n != 1 {
		t.Errorf("a narrowed Approver lists %d claims, want 1", n)
	}
	if n := count(mei, "/v1/expense-claims?whose=waiting_for_me"); n != 1 {
		t.Errorf("a narrowed Approver has %d claims waiting for them, want 1", n)
	}
	if r := e.call(http.MethodGet, "/v1/expense-claims/"+inEng.id, mei, nil); r.Status != 200 {
		t.Errorf("their department's claim: %d %s", r.Status, r.Raw)
	}
	for what, id := range map[string]string{"another department's claim": inOps.id, "a claim in no department": inNone.id} {
		e.problemIs(e.call(http.MethodGet, "/v1/expense-claims/"+id, mei, nil), "getExpenseClaim", 404, CodeNotFound)
		if r := e.call(http.MethodGet, "/v1/expense-claims/"+id+"/evidence", mei, nil); r.Status != http.StatusNotFound {
			t.Errorf("the files of %s: %d %s", what, r.Status, r.Raw)
		}
	}
	// The requests: they see and sign Engineering's, and are told nothing of the others.
	if n := count(mei, "/v1/approvals"); n != 1 {
		t.Errorf("a narrowed Approver lists %d requests, want 1", n)
	}
	for what, id := range map[string]string{"another department's": inOps.request, "no department's": inNone.request} {
		if r := e.call(http.MethodGet, "/v1/approvals/"+id, mei, nil); r.Status != http.StatusNotFound {
			t.Errorf("reading %s request: %d %s", what, r.Status, r.Raw)
		}
		if r := e.call(http.MethodPost, "/v1/approvals/"+id+"/approve", mei, map[string]any{"envelope_hash": strings.Repeat("0", 64)}, "If-Match", `"1"`); r.Status != http.StatusNotFound &&
			r.Status != http.StatusForbidden {
			t.Errorf("signing %s request: %d %s", what, r.Status, r.Raw)
		}
	}
	if g := e.call(http.MethodGet, "/v1/expense-claims/"+inOps.id, c.as(c.weiling, authz.Owner, authz.Controller), nil); g.str("stage") != "submitted" {
		t.Errorf("a refused signature moved the claim: %s", g.Raw)
	}
	asked := e.call(http.MethodGet, "/v1/approvals/"+inEng.request, mei, nil)
	if asked.Status != 200 || asked.Body["you_may_approve"] != true {
		t.Fatalf("their department's request: %d %s", asked.Status, asked.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+inEng.request+"/approve", mei, hashOf(asked), "If-Match", `"1"`); d.Status != 200 || d.str("status") != "approved" {
		t.Fatalf("approving their department's claim: %d %s", d.Status, d.Raw)
	}
	// Her own claim is hers whatever its department.
	mine := e.call(http.MethodPost, "/v1/expense-claims", mei, map[string]any{"date": "2026-10-15", "title": "Mine", "lines": []any{taxi(travel)}})
	if mine.Status != 201 {
		t.Fatalf("her own claim: %d %s", mine.Status, mine.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/expense-claims/"+mine.str("id"), mei, nil); r.Status != 200 {
		t.Errorf("her own claim: %d %s", r.Status, r.Raw)
	}
	// Her other role that reaches the row is not narrowed.
	if n := count(meiAsAccountant, "/v1/expense-claims?whose=everyone"); n != 4 {
		t.Errorf("an Approver who is also an Accountant lists %d claims, want all 4", n)
	}

	// Bills: the Accountant drafts one in each; a narrowed Approver lists and reads Engineering's, not the company's payables.
	bill := func(net, value string) string {
		t.Helper()
		d := e.billDraft(net, "0.00")
		if value != "" {
			d["lines"].([]map[string]any)[0]["dimension_value_ids"] = []string{value}
		}
		r := e.call(http.MethodPost, "/v1/bills", accountant, d)
		if r.Status != http.StatusCreated {
			t.Fatalf("a bill: %d %s", r.Status, r.Raw)
		}
		return r.str("id")
	}
	billEng, billOps, billNone := bill("100.00", eng), bill("200.00", ops), bill("300.00", "")
	if n := count(mei, "/v1/bills"); n != 1 {
		t.Errorf("a narrowed Approver lists %d bills, want 1", n)
	}
	if r := e.call(http.MethodGet, "/v1/bills/"+billEng, mei, nil); r.Status != 200 {
		t.Errorf("their department's bill: %d %s", r.Status, r.Raw)
	}
	for _, id := range []string{billOps, billNone} {
		e.problemIs(e.call(http.MethodGet, "/v1/bills/"+id, mei, nil), "getBill", 404, CodeNotFound)
	}
	for _, path := range []string{"/v1/payables", "/v1/receivables", "/v1/bills/" + billEng + "/evidence", "/v1/bill-reminders"} {
		if r := e.call(http.MethodGet, path, mei, nil); r.Status != http.StatusForbidden {
			t.Errorf("%s as a narrowed Approver: %d %s", path, r.Status, r.Raw)
		}
	}
	if n := count(meiAsAccountant, "/v1/bills"); n != 3 {
		t.Errorf("an Approver who is also an Accountant lists %d bills, want 3", n)
	}
}
