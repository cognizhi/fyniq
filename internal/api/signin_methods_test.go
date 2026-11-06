//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/signin"
)

// devZoho is a second provider on the local sign-in page, standing for Zoho.
var devZoho = signin.Provider{ID: "local-zoho", Name: "Zoho (local)", Issuer: local.DevIssuer + "/zoho", ClientID: "fyniq-web"}

// methodOf is provider's row in the Super admin's list.
func methodOf(b *browser, provider string) map[string]any {
	b.s.t.Helper()
	r := b.call(http.MethodGet, "/v1/installation/sign-in-methods", "same-origin", nil)
	if r.Status != http.StatusOK {
		b.s.t.Fatalf("listing the sign-in providers: %d %s", r.Status, r.Raw)
	}
	for _, it := range r.Body["items"].([]any) {
		if m := it.(map[string]any); m["provider"] == provider {
			return m
		}
	}
	b.s.t.Fatalf("no provider %s in %s", provider, r.Raw)
	return nil
}

// A Super admin turns a sign-in provider off, ending its sessions if they choose, and sets the email domains it accepts;
// the sign-in page offers it no more and the flow refuses it, or an account whose verified email is on no accepted
// domain. Fyniq refuses a change that leaves a Super admin no way to sign in. With two Super admins a change waits for
// the other's approval, which applies it; nobody approves their own. Each change is in the installation's trail.
// req: FR-ADM-08
// req: FR-ADM-07
// asvs: V6.8.1, V8.2.1
func TestSignInProviders(t *testing.T) {
	s := startSessionsWith(t, devZoho)
	desk, phone, _ := s.founders(t) // desk is the Super admin; phone an Owner only
	change := func(b *browser, provider, version string, body map[string]any, headers ...string) response {
		return b.call(http.MethodPatch, "/v1/installation/sign-in-methods/"+provider, "same-origin", body, append([]string{"If-Match", version}, headers...)...)
	}
	off := map[string]any{"enabled": false, "allowed_domains": []string{}, "end_sessions": true}

	l, z := methodOf(desk, "local"), methodOf(desk, "local-zoho")
	if l["enabled"] != true || l["last_for_super_admin"] != true || l["version"] != float64(0) || z["last_for_super_admin"] != false ||
		z["name"] != "Zoho (local)" {
		t.Fatalf("the providers: %v %v", l, z)
	}
	s.problemIs(phone.call(http.MethodGet, "/v1/installation/sign-in-methods", "same-origin", nil), "listSignInMethods", 403, CodeNotSuperAdmin)

	// The only way the Super admin signs in stays on, and its domains cannot shut them out; a preview says so too.
	s.problemIs(change(desk, "local", `"0"`, off), "changeSignInMethod", 409, storage.CodeSignInMethod)
	s.problemIs(change(desk, "local", `"0"`, off, "Prefer", "return=preview"), "changeSignInMethod", 409, storage.CodeSignInMethod)
	s.problemIs(change(desk, "local", `"0"`, map[string]any{"enabled": true, "allowed_domains": []string{"cognizhi.com"}}), "changeSignInMethod",
		409, storage.CodeSignInMethod)
	s.problemIs(change(desk, "local-zoho", `"0"`, map[string]any{"enabled": true, "allowed_domains": []string{"not a domain"}}), "changeSignInMethod",
		422, CodeInvalid)
	s.problemIs(change(desk, "local-zoho", `"3"`, off), "changeSignInMethod", 412, CodeStale)
	s.problemIs(change(desk, "nowhere", `"0"`, off), "changeSignInMethod", 404, CodeNotFound)

	// Turning Zoho off ends the session it started, and the sign-in page offers it no more.
	visitor := s.browser()
	visitor.signInWith("local-zoho", "visitor", true)
	if r := visitor.session(); r.Status != http.StatusOK {
		t.Fatalf("the visitor's session: %d %s", r.Status, r.Raw)
	}
	r := change(desk, "local-zoho", `"0"`, off)
	if r.Status != http.StatusOK || r.Body["enabled"] != false || r.Header.Get("ETag") != `"1"` {
		t.Fatalf("turning Zoho off: %d %s", r.Status, r.Raw)
	}
	s.problemIs(visitor.session(), "getSession", 401, CodeUnauthenticated)
	if p := desk.call(http.MethodGet, "/v1/sign-in-providers", "", nil); strings.Contains(p.Raw, "local-zoho") {
		t.Errorf("the sign-in page still offers Zoho: %s", p.Raw)
	}
	if to := s.browser().signInWith("local-zoho", "visitor", true); to != "/?signin_error=AUTH-SIGNIN-01" {
		t.Errorf("a sign-in with Zoho turned off ended at %s", to)
	}

	// Back on, the founder links a Zoho account; then only Zoho keeps them in, and the local page may narrow its domains.
	if r := change(desk, "local-zoho", `"1"`, map[string]any{"enabled": true, "allowed_domains": []string{"Cognizhi.com"}}); r.Status != http.StatusOK ||
		r.Body["version"] != float64(2) || !strings.Contains(r.Raw, `"allowed_domains":["cognizhi.com"]`) {
		t.Fatalf("turning Zoho on for cognizhi.com: %d %s", r.Status, r.Raw)
	}
	if to := desk.linkWith("local-zoho", "founder-a-zoho", "same-origin", "someone@example.com"); to != "/?signin_error=AUTH-SIGNIN-08" &&
		to != "/settings/profile?link_error=AUTH-SIGNIN-08" {
		t.Errorf("a Zoho account on another domain was linked: %s", to)
	}
	if to := desk.linkWith("local-zoho", "founder-a-zoho", "same-origin", "mei.ling@cognizhi.com"); to != "/settings/profile?linked=local-zoho" {
		t.Fatalf("linking the founder's Zoho account ended at %s", to)
	}
	if r := change(desk, "local", `"0"`, map[string]any{"enabled": true, "allowed_domains": []string{"cognizhi.com"}}); r.Status != http.StatusOK {
		t.Fatalf("narrowing the local page: %d %s", r.Status, r.Raw)
	}
	stranger := s.browser()
	stranger.email = "x@example.com"
	if to := stranger.signIn("stranger", true); to != "/?signin_error=AUTH-SIGNIN-08" {
		t.Errorf("an account on another domain signed in: %s", to)
	}
	if methodOf(desk, "local-zoho")["last_for_super_admin"] != true || methodOf(desk, "local")["last_for_super_admin"] != false {
		t.Error("the last way in moved, but the list did not say so")
	}

	// With a second Super admin, a change waits for them; the one who asked cannot approve it.
	code := desk.call(http.MethodPost, "/v1/super-admins/invitations", "same-origin", nil)
	if a := phone.call(http.MethodPost, "/v1/super-admins", "same-origin", map[string]any{"invitation_code": code.str("code"),
		"display_name": "Wei Jie Lim"}); a.Status != http.StatusCreated {
		t.Fatalf("the second Super admin: %d %s", a.Status, a.Raw)
	}
	pending := change(desk, "local", `"1"`, map[string]any{"enabled": true, "allowed_domains": []string{}})
	if pending.Status != http.StatusAccepted || pending.str("step") != "sign_in" || pending.str("connector") != "sign-in:local" {
		t.Fatalf("asking with two Super admins: %d %s", pending.Status, pending.Raw)
	}
	if methodOf(desk, "local")["pending"] == nil || methodOf(desk, "local")["version"] != float64(1) {
		t.Error("the change applied before its approval, or the list does not show it waiting")
	}
	decide := func(b *browser) response {
		return b.call(http.MethodPost, "/v1/installation/changes/"+pending.str("id")+"/approve", "same-origin",
			map[string]any{"envelope_hash": pending.str("envelope_hash")})
	}
	if r := decide(desk); r.Status < 400 {
		t.Fatalf("the Super admin who asked approved it: %d %s", r.Status, r.Raw)
	}
	if r := decide(phone); r.Status != http.StatusOK || r.Body["version"] != float64(2) || !strings.Contains(r.Raw, `"allowed_domains":[]`) {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}

	trail := s.installationTrail(t)
	if n := strings.Count(trail, `"action":"sign_in_method.changed"`); n != 4 {
		t.Errorf("sign-in changes in the trail: %d\n%s", n, trail)
	}
}
