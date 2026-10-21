//go:build local

package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/accounting"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-ORG-10, G4-017 (the founders' decision of rev 0.62): the Owner, the Controller or the Accountant records the
// framework and each policy from a financial year; a member who holds the Accountant role confirms or approves each
// version, never its writer, never twice and never a replaced one. A year's statements are "Management statements",
// naming no framework, until everything in force for it is signed; then they show the framework and edition, and a year
// from 1 Jan 2027 under SFRS for Small Entities needs the Third Edition. While accounting.signing_step_up is on, as it is
// by default, signing needs step-up in the last 5 minutes (the founders' decision of rev 0.63); off, it does not.
// req: FR-ORG-10
func TestAccountingFrameworkAndPolicies(t *testing.T) {
	e := newEnv(t)
	owner, writer := e.as(authz.Owner), e.as(authz.Accountant)
	checker := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Accountant},
		StepUpAt: e.clock.Now().Add(-time.Minute)})
	tag := e.call(http.MethodGet, "/v1/organisation/profile", e.as(authz.Controller), nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Controller),
		map[string]any{"fy_start_month": 1, "books_start": "2026-01-01", "first_year_end": "2026-12-31"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the years: %d %s", p.Status, p.Raw)
	}
	year := func(on string) response {
		r := e.call(http.MethodGet, "/v1/organisation/accounting?on="+on, owner, nil)
		if r.Status != 200 {
			t.Fatalf("the year of %s: %d %s", on, r.Status, r.Raw)
		}
		return r
	}

	// Nothing recorded: management statements, naming no framework; SFRS for Small Entities recommended.
	y := year("2026-10-15")
	if y.str("starts_on") != "2026-01-01" || y.str("status") != "not_approved" || y.str("statement_title") != "Management statements" ||
		y.Body["statement_basis"] != nil || y.str("recommended_framework") != "sfrs_se" || y.str("recommended_edition") != accounting.EditionOf2024 ||
		len(y.Body["checks"].([]any)) != 1+len(accounting.Areas) || len(y.Body["policies"].([]any)) != len(accounting.Areas) {
		t.Fatalf("nothing recorded: %s", y.Raw)
	}

	// The Owner records the framework; a preview saves nothing; a year's first day only.
	framework := map[string]any{"framework": "sfrs_se", "edition": accounting.EditionOf2024, "from_on": "2026-01-01"}
	if pv := e.call(http.MethodPost, "/v1/organisation/accounting/framework", owner, framework, "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" || year("2026-10-15").Body["framework"] != nil {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	bad := e.call(http.MethodPost, "/v1/organisation/accounting/framework", owner, map[string]any{"framework": "sfrs_se", "edition": "x", "from_on": "2026-02-01"})
	e.problemIs(bad, "recordAccountingFramework", 422, CodeInvalid)
	if bad.Body["errors"].([]any)[0].(map[string]any)["code"] != accounting.CodeInput {
		t.Errorf("not a year's first day: %s", bad.Raw)
	}
	f := e.call(http.MethodPost, "/v1/organisation/accounting/framework", owner, framework, "Idempotency-Key", "framework-1")
	if f.Status != 201 || f.Body["version"] != 1.0 || f.str("name") != "SFRS for Small Entities" || f.Body["written_by"] == nil ||
		f.Body["confirmed_by"] != nil || f.Body["you_may_confirm"] != false {
		t.Fatalf("the framework: %d %s", f.Status, f.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/accounting/framework/1/confirm", owner, nil), "confirmAccountingFramework", 403, CodeForbidden)

	// An Accountant writes the revenue policy and cannot approve it; another Accountant approves it, once.
	rev := e.call(http.MethodPost, "/v1/organisation/accounting/policies/revenue", writer, map[string]any{"wording": "Revenue as the accountant decided.", "from_on": "2026-01-01"})
	if rev.Status != 201 || rev.Body["version"] != 1.0 || rev.Body["you_may_approve"] != false {
		t.Fatalf("the revenue policy: %d %s", rev.Status, rev.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/accounting/policies/revenue/1/approve", writer, nil), "approveAccountingPolicy", 403, accounting.CodeOwn)
	// Another accountant who has not stepped up in the last 5 minutes is asked to.
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/accounting/policies/revenue/1/approve", e.as(authz.Accountant), nil), "approveAccountingPolicy", 403, CodeStepUpRequired)
	if a := e.call(http.MethodPost, "/v1/organisation/accounting/policies/revenue/1/approve", checker, nil); a.Status != 200 || a.Body["approved_by"] == nil || a.Body["approved_at"] == nil {
		t.Fatalf("approved by another accountant: %d %s", a.Status, a.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/accounting/policies/revenue/1/approve", checker, nil), "approveAccountingPolicy", 409, accounting.CodeSigned)
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/accounting/policies/revenue/9/approve", checker, nil), "approveAccountingPolicy", 404, CodeNotFound)
	if c := e.call(http.MethodPost, "/v1/organisation/accounting/framework/1/confirm", checker, nil); c.Status != 200 || c.Body["confirmed_by"] == nil {
		t.Fatalf("confirmed: %d %s", c.Status, c.Raw)
	}

	// Every other policy, written by the Owner and approved: the year is approved.
	for _, a := range accounting.Areas[1:] {
		p := e.call(http.MethodPost, "/v1/organisation/accounting/policies/"+a.Key, owner, map[string]any{"wording": a.Title + " as decided.", "from_on": "2026-01-01"})
		if p.Status != 201 {
			t.Fatalf("%s: %d %s", a.Key, p.Status, p.Raw)
		}
		if ap := e.call(http.MethodPost, "/v1/organisation/accounting/policies/"+a.Key+"/1/approve", checker, nil); ap.Status != 200 {
			t.Fatalf("approve %s: %d %s", a.Key, ap.Status, ap.Raw)
		}
	}
	y = year("2026-10-15")
	if y.str("status") != "approved" || y.str("statement_title") != "Financial statements" || len(y.Body["checks"].([]any)) != 0 ||
		y.str("statement_basis") != "Prepared under SFRS for Small Entities (effective for periods beginning on or after 1 January 2024)" {
		t.Fatalf("all signed: %s", y.Raw)
	}

	// 2027 carries the 2026 framework, but the Third Edition applies; recorded and confirmed, 2027 is approved too.
	y = year("2027-03-01")
	checks := y.Body["checks"].([]any)
	if y.str("status") != "not_approved" || y.str("recommended_edition") != accounting.ThirdEdition || len(checks) != 1 ||
		checks[0].(map[string]any)["code"] != accounting.CodeEdition {
		t.Fatalf("2027 under the 2024 edition: %s", y.Raw)
	}
	third := e.call(http.MethodPost, "/v1/organisation/accounting/framework", owner, map[string]any{"framework": "sfrs_se", "edition": "Third Edition", "from_on": "2027-01-01"})
	if c := e.call(http.MethodPost, fmt.Sprintf("/v1/organisation/accounting/framework/%d/confirm", int(third.Body["version"].(float64))), checker, nil); c.Status != 200 {
		t.Fatalf("the Third Edition: %d %s", c.Status, c.Raw)
	}
	if y = year("2027-03-01"); y.str("status") != "approved" || y.str("statement_basis") != "Prepared under SFRS for Small Entities (Third Edition)" {
		t.Fatalf("2027 under the Third Edition: %s", y.Raw)
	}
	if y = year("2026-10-15"); y.str("status") != "approved" || y.Body["framework"].(map[string]any)["version"] != 1.0 {
		t.Fatalf("2026 keeps its framework: %s", y.Raw)
	}

	// A new version of a policy waits for approval, and the one it replaces can no longer be approved.
	for range 2 {
		e.call(http.MethodPost, "/v1/organisation/accounting/policies/leases", owner, map[string]any{"wording": "Leases, revised.", "from_on": "2026-01-01"})
	}
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/accounting/policies/leases/2/approve", checker, nil), "approveAccountingPolicy", 409, accounting.CodeSigned)
	if y = year("2026-10-15"); y.str("status") != "not_approved" || y.str("statement_title") != "Management statements" || y.Body["statement_basis"] != nil {
		t.Fatalf("a revised policy: %s", y.Raw)
	}

	// With the setting off, signing asks for no step-up.
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "accounting.signing_step_up", "true", "false", time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if a := e.call(http.MethodPost, "/v1/organisation/accounting/policies/leases/3/approve", e.as(authz.Accountant), nil); a.Status != 200 {
		t.Fatalf("approved without step-up, the setting off: %d %s", a.Status, a.Raw)
	}
}
