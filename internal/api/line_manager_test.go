//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A line manager approves their reports' claims first, from the people projection, with no role (PRD-001 FR-AP-17, FR-PPL-06, §6.2 "line manager is a relationship, not a role"; p1-payroll-link
// task 3.4): Ah Kow's person names Mei's person as his line manager, each mapped to their Fyniq user explicitly, so his claim's approval has the manager's step first and finance's after; Mei holds only the
// Employee role and so reaches that one request and signs its first step; she cannot sign twice, nor a claim of someone she does not manage, nor read other approvals; finance signs the second step and the claim
// posts. With no mapping, no line manager, or a manager the projection has no Fyniq user for, the policy's own steps stand.
// req: FR-AP-17
// part of FR-PPL-06 (the Approver's department figures follow the role's own scope, not the projection)
// asvs: V8.2.1, V8.2.2, V8.3.1
func TestALineManagerApprovesTheirReportsClaimFirstWithNoRole(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	ahkow := c.as(c.ahkow, authz.Employee)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	meiAsEmployee := c.as(c.mei, authz.Employee) // the relationship, no role of approval
	draft := map[string]any{"date": "2026-10-15", "title": "Acme visit", "lines": []any{taxi(travel)}}
	r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, draft)
	if r.Status != 201 {
		t.Fatalf("drafting: %d %s", r.Status, r.Raw)
	}
	path := "/v1/expense-claims/" + r.str("id")
	// With no projection the policy's own step stands: one.
	asked := e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", r.Header.Get("ETag"))
	if asked.Status != 202 || len(asked.Body["steps"].([]any)) != 1 {
		t.Fatalf("with no projection: %d %s", asked.Status, asked.Raw)
	}
	w := e.call(http.MethodPost, path+"/withdraw", ahkow, nil, "If-Match", r.Header.Get("ETag"))
	if w.Status != 200 {
		t.Fatalf("withdrawing: %d %s", w.Status, w.Raw)
	}
	// The projection: Ah Kow's person, whose line manager is Mei's person; each mapped to their user.
	p1, p2 := uuid.New(), uuid.New()
	save := func(p peoplelink.Person) {
		t.Helper()
		if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.PeopleLinks().SavePerson(ctx, p) }); err != nil {
			t.Fatal(err)
		}
	}
	now := e.clock.Now()
	save(peoplelink.Person{ID: p2, Version: 1, DisplayName: "Mei", Status: "active", UserID: uuid.MustParse(c.mei), UpdatedAt: now})
	save(peoplelink.Person{ID: p1, Version: 1, DisplayName: "Ah Kow", Status: "active", LineManager: p2, UserID: uuid.MustParse(c.ahkow), UpdatedAt: now})
	g := e.call(http.MethodGet, path, ahkow, nil)
	again := e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", g.Header.Get("ETag"))
	steps, _ := again.Body["steps"].([]any)
	if again.Status != 202 || len(steps) != 2 || steps[0].(map[string]any)["users"].([]any)[0] != c.mei {
		t.Fatalf("with the line manager: %d %s", again.Status, again.Raw)
	}
	// Mei, with the Employee role alone, reaches this request, and no one else's; another Employee neither.
	if r := e.call(http.MethodGet, "/v1/approvals/"+again.str("id"), meiAsEmployee, nil); r.Status != http.StatusOK {
		t.Fatalf("the line manager reading it: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/approvals/"+again.str("id"), c.as(c.weiling, authz.Employee), nil), "getApproval", 403, CodeForbidden)
	// Finance cannot go before the manager; the manager signs the first step; she cannot sign twice; finance's step posts it.
	if d := e.call(http.MethodPost, "/v1/approvals/"+again.str("id")+"/approve", weiling, hashOf(again), "If-Match", `"1"`); d.Status == http.StatusOK && d.str("status") == "approved" {
		t.Fatalf("finance approved ahead of the line manager: %s", d.Raw)
	}
	step1 := e.call(http.MethodPost, "/v1/approvals/"+again.str("id")+"/approve", meiAsEmployee, hashOf(again), "If-Match", `"1"`)
	if step1.Status != http.StatusOK || step1.str("status") != "pending" {
		t.Fatalf("the line manager's step: %d %s", step1.Status, step1.Raw)
	}
	cur := e.call(http.MethodGet, "/v1/approvals/"+again.str("id"), weiling, nil)
	if d := e.call(http.MethodPost, "/v1/approvals/"+again.str("id")+"/approve", meiAsEmployee, hashOf(cur), "If-Match", cur.Header.Get("ETag")); d.Status == http.StatusOK && d.str("status") == "approved" {
		t.Fatalf("the line manager signed twice: %s", d.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+again.str("id")+"/approve", weiling, hashOf(cur), "If-Match", cur.Header.Get("ETag")); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("finance's step: %d %s", d.Status, d.Raw)
	}
	if s := e.call(http.MethodGet, path, ahkow, nil); s.str("stage") == "submitted" || s.str("status") != "posted" {
		t.Errorf("the claim after both steps: %s", s.Raw)
	}
	// A claim of someone Mei does not manage is not hers: no step names her, so she neither reads nor approves it.
	other := e.call(http.MethodPost, "/v1/expense-claims", c.as(c.weiling, authz.Employee), draft)
	oa := e.call(http.MethodPost, "/v1/expense-claims/"+other.str("id")+"/submit", c.as(c.weiling, authz.Employee), nil, "If-Match", other.Header.Get("ETag"))
	if oa.Status != 202 {
		t.Fatalf("Wei Ling's claim: %d %s", oa.Status, oa.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+oa.str("id")+"/approve", meiAsEmployee, hashOf(oa), "If-Match", `"1"`); d.Status == http.StatusOK {
		t.Errorf("a line manager approved a claim of someone they do not manage: %s", d.Raw)
	}
	_ = approval.ObjectClaim
}
