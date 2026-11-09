//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/organisation"
	"github.com/cognizhi/fyniq/internal/email"
)

// The Data Protection Officer (CMP-PD-01, rev 0.244): the profile names a member as the DPO with the business contact
// the company publishes, audited; a member without the Compliance officer role is a warning, not a refusal; someone who
// is not a member is refused, and so is a member without the contact (ORG-11); an outsourced DPO has the contact and no
// member. Every email Fyniq sends then ends with the contact.
// req: CMP-PD-01
// req: FR-ORG-01
func TestTheDataProtectionOfficer(t *testing.T) {
	e := newEnv(t)
	controller := e.as(authz.Controller)
	grace, hal := e.joined("Grace", "dpo"), e.joined("Hal", "bookkeeper")
	patch := func(body map[string]any) response {
		t.Helper()
		return e.call(http.MethodPatch, "/v1/organisation/profile", controller, body, "If-Match", "*")
	}

	r := patch(map[string]any{"dpo_user_id": grace.String(), "dpo_name": "Grace Lim", "dpo_email": "dpo@northwind.example", "email": "accounts@example.com"})
	if r.Status != 200 || r.str("dpo_user_id") != grace.String() || r.str("dpo_name") != "Grace Lim" || r.Body["dpo_holds_role"] != true {
		t.Fatalf("naming Grace: %d %s", r.Status, r.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/organisation/profile", controller, nil); g.str("dpo_email") != "dpo@northwind.example" || g.Body["dpo_holds_role"] != true {
		t.Fatalf("reading it: %s", g.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, "dpo_user_id") || !strings.Contains(trail, "dpo@northwind.example") {
		t.Error("naming the DPO is not in the audit trail")
	}

	// A member without the Compliance officer role is named, with a warning.
	if r := patch(map[string]any{"dpo_user_id": hal.String()}); r.Status != 200 || r.Body["dpo_holds_role"] != false {
		t.Errorf("naming Hal: %d %s", r.Status, r.Raw)
	}
	// Someone who is not a member is refused.
	r = patch(map[string]any{"dpo_user_id": uuid.NewString()})
	e.problemIs(r, "updateOrganisationProfile", 422, CodeInvalid)
	if f := fields(r); f["body.dpo_user_id"] != CodeInvalid {
		t.Errorf("a stranger named: %v", f)
	}
	// A member without the contact is refused.
	r = patch(map[string]any{"dpo_user_id": grace.String(), "dpo_email": ""})
	e.problemIs(r, "updateOrganisationProfile", 422, CodeInvalid)
	if f := fields(r); f["body.dpo_email"] != organisation.CodeDPO {
		t.Errorf("a member without the contact: %v", f)
	}
	// An outsourced DPO: the contact, and no member.
	r = patch(map[string]any{"dpo_user_id": "", "dpo_name": "Privacy Partners LLP", "dpo_email": "dpo@privacy.example"})
	if r.Status != 200 || r.Body["dpo_user_id"] != nil || r.Body["dpo_holds_role"] != nil || r.str("dpo_name") != "Privacy Partners LLP" {
		t.Fatalf("an outsourced DPO: %d %s", r.Status, r.Raw)
	}

	// Every email ends with the contact.
	if s := e.call(http.MethodPost, "/v1/organisation/email/test-messages", e.as(authz.Administrator), nil); s.Status != http.StatusCreated {
		t.Fatalf("a test message: %d %s", s.Status, s.Raw)
	}
	k, row := nextWorkRow(t, e)
	if k != email.Kind || !strings.Contains(string(row.Payload), "Questions about your personal data: Privacy Partners LLP, Data Protection Officer, dpo@privacy.example") {
		t.Errorf("the message's footer: %s %s", k, row.Payload)
	}
}
