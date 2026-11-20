//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixSandbox is the authorisation matrix's rows for the sandbox organisation's two operations: previews and refusals, so nothing is made.
var matrixSandbox = map[string]func(e *env, tok string) (response, func() bool){
	"createSandboxOrganisation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/sandbox-organisations", tok, map[string]any{"name": "Matrix"}, "Prefer", "return=preview"), nil
	},
	"loadSandboxSampleCompany": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/installation/sandbox-organisations/"+e.books.Org.String()+"/sample-company", tok, nil), nil
	},
}

// A Super admin makes a sandbox organisation where the deployment enables them (FR-INT-18; the owner's decision of 21 Nov 2026): refused until the
// operator enables it and for anyone who is no Super admin; a new organisation labelled "Sandbox: " that holds the sample company, is flagged, and
// is the creator's to work in, apart from the real one; it takes no practice organisation and no webhook to a host the operator has not allowed;
// loading the sample company again is refused; with two Super admins the other approves it, never the one who asked, and it is made once.
// req: FR-INT-18
// req: FR-ADM-07
// asvs: V8.2.1, V4.2.1, V8.3.1
func TestASuperAdminMakesASandboxOrganisationWhereTheDeploymentAllowsIt(t *testing.T) {
	s := startSessionsWith(t)
	desk, phone, realOrg := s.founders(t) // desk is the Super admin; phone an Owner only
	make := func(b *browser, name string, headers ...string) response {
		return b.call(http.MethodPost, "/v1/installation/sandbox-organisations", "same-origin", map[string]any{"name": name}, headers...)
	}
	s.problemIs(make(desk, "Integration tests"), "createSandboxOrganisation", 409, CodeSandboxOff)
	s.env.api.deps.Sandbox = SandboxConfig{Enabled: true}
	s.problemIs(make(phone, "Integration tests"), "createSandboxOrganisation", 403, CodeNotSuperAdmin)
	s.problemIs(make(desk, "   "), "createSandboxOrganisation", 422, CodeInvalid)
	if r := make(desk, "Integration tests", "Prefer", "return=preview"); r.Status != http.StatusOK || r.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", r.Status, r.Raw)
	}
	if orgs := desk.call(http.MethodGet, "/v1/session/organisations", "same-origin", nil); strings.Contains(orgs.Raw, "Sandbox: ") {
		t.Fatalf("a preview made an organisation: %s", orgs.Raw)
	}

	made := make(desk, "Integration tests")
	if made.Status != http.StatusCreated || made.str("name") != "Sandbox: Integration tests" || made.Body["sandbox"] != true || made.str("base_currency") != "SGD" {
		t.Fatalf("making one with one Super admin: %d %s", made.Status, made.Raw)
	}
	sandbox := made.str("id")
	if sandbox == realOrg {
		t.Fatal("the sandbox is the real organisation")
	}
	// It is the creator's to work in and says what it is; its books hold the sample company; the real organisation does not say it.
	orgs := desk.call(http.MethodGet, "/v1/session/organisations", "same-origin", nil)
	if !strings.Contains(orgs.Raw, `"sandbox":true`) || strings.Count(orgs.Raw, `"sandbox":true`) != 1 {
		t.Fatalf("the creator's organisations: %s", orgs.Raw)
	}
	if r := desk.call(http.MethodGet, "/v1/organisation", "same-origin", nil); r.Body["sandbox"] == true {
		t.Fatalf("the real organisation says it is a sandbox: %s", r.Raw)
	}
	if r := desk.call(http.MethodPost, "/v1/session/organisation", "same-origin", map[string]any{"organisation_id": sandbox}); r.Status != http.StatusOK || r.Body["sandbox"] != true {
		t.Fatalf("switching into the sandbox: %d %s", r.Status, r.Raw)
	}
	if r := desk.call(http.MethodGet, "/v1/organisation", "same-origin", nil); r.Status != 200 || r.Body["sandbox"] != true || !strings.HasPrefix(r.str("name"), "Sandbox: ") {
		t.Fatalf("the sandbox organisation: %d %s", r.Status, r.Raw)
	}
	accounts := desk.call(http.MethodGet, "/v1/accounts", "same-origin", nil)
	if accounts.Status != 200 || len(accounts.Body["items"].([]any)) < 20 {
		t.Fatalf("the sandbox holds no sample company: %d %.200s", accounts.Status, accounts.Raw)
	}
	// Inside it: no practice organisation; loading again is refused.
	s.problemIs(desk.call(http.MethodPost, "/v1/organisation/practice", "same-origin", map[string]any{}), "createPracticeOrganisation", 409, CodeSandboxRefused)
	s.problemIs(desk.call(http.MethodPost, "/v1/installation/sandbox-organisations/"+sandbox+"/sample-company", "same-origin", nil), "loadSandboxSampleCompany", 409, CodeSandboxRefused)
	// The real organisation takes no sample company either.
	s.problemIs(desk.call(http.MethodPost, "/v1/installation/sandbox-organisations/"+realOrg+"/sample-company", "same-origin", nil), "loadSandboxSampleCompany", 409, CodeSandboxRefused)
	if r := desk.call(http.MethodPost, "/v1/session/organisation", "same-origin", map[string]any{"organisation_id": realOrg}); r.Status != http.StatusOK || r.Body["sandbox"] == true {
		t.Fatalf("switching back: %d %s", r.Status, r.Raw)
	}
	// Another person, an Owner of the real organisation only, reads nothing of the sandbox (tenancy).
	s.problemIs(phone.call(http.MethodPost, "/v1/session/organisation", "same-origin", map[string]any{"organisation_id": sandbox}), "switchMyOrganisation", 404, CodeNotFound)

	// With a second Super admin, a sandbox waits for them; the one who asked cannot approve it; it is made once.
	code := desk.call(http.MethodPost, "/v1/super-admins/invitations", "same-origin", nil)
	if a := phone.call(http.MethodPost, "/v1/super-admins", "same-origin", map[string]any{"invitation_code": code.str("code"), "display_name": "Wei Jie Lim"}); a.Status != http.StatusCreated {
		t.Fatalf("the second Super admin: %d %s", a.Status, a.Raw)
	}
	pending := make(desk, "Second try")
	if pending.Status != http.StatusAccepted || pending.str("step") != "sandbox" || !strings.HasPrefix(pending.str("connector"), "sandbox:") {
		t.Fatalf("asking with two Super admins: %d %s", pending.Status, pending.Raw)
	}
	if r := desk.call(http.MethodGet, "/v1/session/organisations", "same-origin", nil); strings.Contains(r.Raw, "Second try") {
		t.Fatal("the sandbox was made before its approval")
	}
	decide := func(b *browser) response {
		return b.call(http.MethodPost, "/v1/installation/changes/"+pending.str("id")+"/approve", "same-origin", map[string]any{"envelope_hash": pending.str("envelope_hash")})
	}
	if r := decide(desk); r.Status < 400 {
		t.Fatalf("the Super admin who asked approved it: %d %s", r.Status, r.Raw)
	}
	approved := decide(phone)
	if approved.Status != http.StatusOK || approved.str("name") != "Sandbox: Second try" || approved.Body["sandbox"] != true {
		t.Fatalf("approving: %d %s", approved.Status, approved.Raw)
	}
	if r := decide(phone); r.Status < 400 {
		t.Fatalf("the approval applied twice: %d %s", r.Status, r.Raw)
	}
	if r := phone.call(http.MethodPost, "/v1/session/organisation", "same-origin", map[string]any{"organisation_id": approved.str("id")}); r.Status != http.StatusOK {
		t.Fatalf("the approver works in the sandbox: %d %s", r.Status, r.Raw)
	}
	if n := strings.Count(s.installationTrail(t), `"action":"sandbox_organisation.created"`); n != 2 {
		t.Errorf("sandbox_organisation.created in the installation's trail: %d, want 2", n)
	}
	_ = storage.CodeSandbox
}
