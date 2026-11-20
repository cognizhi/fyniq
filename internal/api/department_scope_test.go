//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// An Approver's role is narrowed to department values when it is given (FR-ADM-01, rev 0.253): after the bootstrap the change
// waits for another Owner and the scope goes with it; the members list shows the scope; another role, an ID that is no
// department's value, a repeated one and a malformed one are refused; the scope is audited.
// req: FR-ADM-01
// asvs: V8.2.1, V8.3.1
func TestAnApproverRoleIsNarrowedToDepartments(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"})
	region := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Region"})
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "ENG", "name": "Engineering"})
	apac := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": region.str("id"), "code": "APAC", "name": "Asia Pacific"})
	if eng.Status != 201 || apac.Status != 201 {
		t.Fatalf("the values: %d %s / %d %s", eng.Status, eng.Raw, apac.Status, apac.Raw)
	}
	roles := "/v1/members/" + f.third + "/roles"
	for what, body := range map[string]map[string]any{
		"another role":                 {"role": "accountant", "department_value_ids": []string{eng.str("id")}},
		"a value of another dimension": {"role": "approver", "department_value_ids": []string{apac.str("id")}},
		"a repeated value":             {"role": "approver", "department_value_ids": []string{eng.str("id"), eng.str("id")}},
		"a malformed ID":               {"role": "approver", "department_value_ids": []string{"engineering"}},
		"an ID that is nothing":        {"role": "approver", "department_value_ids": []string{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e99"}},
	} {
		if r := e.call(http.MethodPost, roles, ahkow, body); r.Status != 422 {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	r := e.call(http.MethodPost, roles, ahkow, map[string]any{"role": "approver", "department_value_ids": []string{eng.str("id")}})
	if r.Status != 202 || r.str("operation") != "role.assign" {
		t.Fatalf("asking for the narrowed role: %d %s", r.Status, r.Raw)
	}
	if a := f.decide(weiling, r.str("id"), "approve", hashOf(r)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}
	m := e.call(http.MethodGet, "/v1/members", weiling, nil)
	for _, it := range m.Body["items"].([]any) {
		item := it.(map[string]any)
		if item["display_name"] != "Third" {
			continue
		}
		if _, ok := item["passkeys"]; !ok {
			t.Fatalf("the member's passkey count is missing: %v", item) // SEC-06: the list says whether they hold a passkey
		}
		role := item["roles"].([]any)[0].(map[string]any)
		ids, _ := role["department_value_ids"].([]any)
		if role["role"] != "approver" || len(ids) != 1 || ids[0] != eng.str("id") {
			t.Fatalf("the member's narrowed role: %v", role)
		}
		return
	}
	t.Fatalf("Third is not listed: %s", m.Raw)
}

// An invitation that gives the Approver role narrows it to department values from the first minute (FR-ADM-01, rev 0.381): the
// scope is part of what the second Owner signs; the list of invitations shows it; redeemed, the member's role carries it. Another
// set of roles, a value that is not a department's and a repeated one are refused, and no scope on an invitation is no scope.
// req: FR-ADM-01
// asvs: V8.2.1, V8.3.1
func TestAnInvitationNarrowsTheApproverRoleItGives(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	accountant := e.as(authz.Accountant)
	dept := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "department", "name": "Department"})
	region := e.call(http.MethodPost, "/v1/dimensions", accountant, map[string]any{"kind": "custom", "name": "Region"})
	eng := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": dept.str("id"), "code": "ENG", "name": "Engineering"})
	apac := e.call(http.MethodPost, "/v1/dimension-values", accountant, map[string]any{"dimension_id": region.str("id"), "code": "APAC", "name": "Asia Pacific"})
	for what, body := range map[string]map[string]any{
		"another role":                 {"display_name": "Mei", "roles": []string{"accountant"}, "department_value_ids": []string{eng.str("id")}},
		"a value of another dimension": {"display_name": "Mei", "roles": []string{"approver"}, "department_value_ids": []string{apac.str("id")}},
		"a repeated value":             {"display_name": "Mei", "roles": []string{"approver"}, "department_value_ids": []string{eng.str("id"), eng.str("id")}},
		"an ID that is nothing":        {"display_name": "Mei", "roles": []string{"approver"}, "department_value_ids": []string{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e99"}},
	} {
		if r := e.call(http.MethodPost, "/v1/members/invitations", ahkow, body); r.Status != 422 {
			t.Errorf("%s: %d %s", what, r.Status, r.Raw)
		}
	}
	// Two members now: the invitation waits for another Owner, scope and all.
	asked := e.call(http.MethodPost, "/v1/members/invitations", ahkow, map[string]any{"display_name": "Mei", "roles": []string{"approver", "employee"},
		"department_value_ids": []string{eng.str("id")}})
	if asked.Status != 202 || !strings.Contains(asked.Raw, eng.str("id")) {
		t.Fatalf("asking for the narrowed invitation: %d %s", asked.Status, asked.Raw)
	}
	code := asked.str("invitation_code")
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}
	pending := e.call(http.MethodGet, "/v1/members/invitations", weiling, nil)
	if pending.Status != 200 || !strings.Contains(pending.Raw, `"department_value_ids":["`+eng.str("id")+`"]`) {
		t.Fatalf("the pending invitation does not show its scope: %d %s", pending.Status, pending.Raw)
	}
	j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "Mei", Client: "web"}), map[string]any{"invitation_code": code, "display_name": "Mei"})
	if j.Status != 201 {
		t.Fatalf("Mei joining: %d %s", j.Status, j.Raw)
	}
	for _, it := range e.call(http.MethodGet, "/v1/members", weiling, nil).Body["items"].([]any) {
		item := it.(map[string]any)
		if item["display_name"] != "Mei" {
			continue
		}
		for _, r := range item["roles"].([]any) {
			role := r.(map[string]any)
			ids, _ := role["department_value_ids"].([]any)
			switch role["role"] {
			case "approver":
				if len(ids) != 1 || ids[0] != eng.str("id") {
					t.Fatalf("Mei's Approver role after joining: %v", role)
				}
			default:
				if len(ids) != 0 {
					t.Errorf("a role besides the Approver is narrowed: %v", role)
				}
			}
		}
		return
	}
	t.Fatal("Mei is not listed")
}

// The members' list shows each member's verified sign-in email to an Owner and an Administrator, who invite and remove people, and to
// no one else who may read the list: the Auditor, the DPO and the Controller see the names and roles (FR-ADM-01, SEC-06, rev 0.381).
// req: FR-ADM-01
// req: SEC-06
// asvs: V8.2.1, V8.3.1
func TestOnlyAnOwnerAndAnAdministratorSeeMembersEmails(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	ctx := context.Background()
	if err := e.store.RecordIdentityEmail(ctx, "urn:fyniq:test", "Mei", "mei@example.sg", e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	emailOf := func(who string) any {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/members", who, nil)
		if r.Status != 200 {
			t.Fatalf("listing: %d %s", r.Status, r.Raw)
		}
		for _, it := range r.Body["items"].([]any) {
			if m := it.(map[string]any); m["display_name"] == "Mei" {
				return m["email"]
			}
		}
		t.Fatal("Mei is not listed")
		return nil
	}
	for role, want := range map[authz.Role]any{authz.Owner: "mei@example.sg", authz.Administrator: "mei@example.sg", authz.Auditor: nil, authz.DPO: nil, authz.Controller: nil} {
		if got := emailOf(e.as(role)); got != want {
			t.Errorf("the %s reads Mei's email %v, want %v", role, got, want)
		}
	}
}

// An invitation made with an email address queues one message to it, in the founder bootstrap and once approved, that names the organisation and
// says to ask for the code: the code is in no email, and the address is kept nowhere but in that message (FR-ADM-01, rev 0.421).
// req: FR-ADM-01
// asvs: V8.2.1, V8.3.1
func TestAnInvitationEmailNeverCarriesTheCode(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	bad := e.call(http.MethodPost, "/v1/members/invitations", ahkow, map[string]any{"display_name": "Mei", "roles": []string{"employee"}, "email": "not an address"})
	if bad.Status != 422 {
		t.Fatalf("an address that is none: %d %s", bad.Status, bad.Raw)
	}
	asked := e.call(http.MethodPost, "/v1/members/invitations", ahkow, map[string]any{"display_name": "Mei", "roles": []string{"employee"}, "email": "mei@example.sg"})
	if asked.Status != 202 {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	code := asked.str("invitation_code")
	messages := func() response { return e.call(http.MethodGet, "/v1/organisation/email/messages?purpose=invitation", weiling, nil) }
	if m := messages(); len(m.Body["items"].([]any)) != 0 {
		t.Fatalf("an email went before the invitation was approved: %s", m.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	m := messages()
	items, _ := m.Body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["recipient"] != "mei@example.sg" || strings.Contains(m.Raw, code) {
		t.Fatalf("the invitation's email: %s", m.Raw)
	}
	one := e.call(http.MethodGet, "/v1/organisation/email/messages/"+items[0].(map[string]any)["id"].(string), weiling, nil)
	if one.Status != 200 || strings.Contains(one.Raw, code) {
		t.Fatalf("the message: %d %s", one.Status, one.Raw)
	}
}
