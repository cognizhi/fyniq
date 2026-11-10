//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A Super admin registers a private endpoint for one organisation and credential, changes it as a new version with
// If-Match, and retires it, which is final; anyone else is refused, as are a public or too wide address, a pin that is
// not one and an organisation that does not exist. With two Super admins a registration waits for the other's approval.
// Each version is in the installation's trail.
// req: SEC-37
// req: FR-ADM-07
// asvs: V8.2.1, V1.3.6
func TestPrivateEndpoints(t *testing.T) {
	s := startSessionsWith(t)
	desk, phone, org := s.founders(t) // desk is the Super admin; phone an Owner only
	const credential = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"
	pin := egress.PinOf([]byte("the endpoint's SubjectPublicKeyInfo"))
	body := func(over map[string]any) map[string]any {
		b := map[string]any{"address": "10.20.0.0/28", "port": 8443, "tls_pin": pin, "service": "connector", "organisation_id": org,
			"credential": credential, "reason": "Acme's ledger API inside its own network"}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	register := func(b *browser, req map[string]any, headers ...string) response {
		return b.call(http.MethodPost, "/v1/installation/private-endpoints", "same-origin", req, headers...)
	}

	s.problemIs(phone.call(http.MethodGet, "/v1/installation/private-endpoints", "same-origin", nil), "listPrivateEndpoints", 403, CodeNotSuperAdmin)
	s.problemIs(register(phone, body(nil)), "registerPrivateEndpoint", 403, CodeNotSuperAdmin)
	for field, bad := range map[string]map[string]any{
		"body.address":  {"address": "8.8.8.8"},
		"body.tls_pin":  {"tls_pin": "not-a-pin"},
		"body.retired":  {"retired": true},
		"body.replaces": {"replaces": "nope"},
	} {
		r := register(desk, body(bad))
		s.problemIs(r, "registerPrivateEndpoint", 422, CodeInvalid)
		if !strings.Contains(r.Raw, `"field":"`+field+`"`) {
			t.Errorf("%v: the problem does not name %s: %s", bad, field, r.Raw)
		}
	}
	s.problemIs(register(desk, body(map[string]any{"address": "10.20.0.0/16"})), "registerPrivateEndpoint", 422, CodeInvalid)
	s.problemIs(register(desk, body(map[string]any{"organisation_id": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4eff"})), "registerPrivateEndpoint", 422,
		storage.CodePrivateEndpoint)
	if r := register(desk, body(nil), "Prefer", "return=preview"); r.Status != http.StatusOK || r.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", r.Status, r.Raw)
	}
	if r := desk.call(http.MethodGet, "/v1/installation/private-endpoints", "same-origin", nil); !strings.Contains(r.Raw, `"items":[]`) {
		t.Fatalf("a preview registered something: %s", r.Raw)
	}

	r := register(desk, body(map[string]any{"address": "10.20.0.5"}))
	if r.Status != http.StatusCreated || r.Header.Get("ETag") != `"1"` || r.str("address") != "10.20.0.5/32" || r.str("organisation_id") != org ||
		r.str("credential") != credential || r.Body["retired"] != false {
		t.Fatalf("registering: %d %s", r.Status, r.Raw)
	}
	id := r.str("id")
	moved := body(map[string]any{"replaces": id, "address": "10.20.0.8/29", "reason": "Acme moved the API to a small range"})
	s.problemIs(register(desk, moved), "registerPrivateEndpoint", 428, CodeIfMatchRequired)
	if r := register(desk, moved, "If-Match", `"1"`); r.Status != http.StatusOK || r.Body["version"] != float64(2) || r.str("address") != "10.20.0.8/29" {
		t.Fatalf("a new version: %d %s", r.Status, r.Raw)
	}
	s.problemIs(register(desk, moved, "If-Match", `"1"`), "registerPrivateEndpoint", 412, CodeStale)
	s.problemIs(register(desk, body(map[string]any{"replaces": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e99"}), "If-Match", `"1"`), "registerPrivateEndpoint",
		404, CodeNotFound)
	retire := body(map[string]any{"replaces": id, "address": "10.20.0.8/29", "retired": true, "reason": "Acme left"})
	if r := register(desk, retire, "If-Match", `"2"`); r.Status != http.StatusOK || r.Body["retired"] != true || r.Body["version"] != float64(3) {
		t.Fatalf("retiring: %d %s", r.Status, r.Raw)
	}
	s.problemIs(register(desk, moved, "If-Match", `"3"`), "registerPrivateEndpoint", 422, storage.CodePrivateEndpoint)

	// With a second Super admin, a registration waits for them; the one who asked cannot approve it.
	code := desk.call(http.MethodPost, "/v1/super-admins/invitations", "same-origin", nil)
	if a := phone.call(http.MethodPost, "/v1/super-admins", "same-origin", map[string]any{"invitation_code": code.str("code"),
		"display_name": "Wei Jie Lim"}); a.Status != http.StatusCreated {
		t.Fatalf("the second Super admin: %d %s", a.Status, a.Raw)
	}
	pending := register(desk, body(map[string]any{"address": "192.168.10.0/24", "port": 443}))
	if pending.Status != http.StatusAccepted || pending.str("step") != "private_endpoint" || !strings.HasPrefix(pending.str("connector"), "private-endpoint:") {
		t.Fatalf("asking with two Super admins: %d %s", pending.Status, pending.Raw)
	}
	if r := desk.call(http.MethodGet, "/v1/installation/private-endpoints", "same-origin", nil); strings.Contains(r.Raw, "192.168.10.0/24") {
		t.Fatal("the registration applied before its approval")
	}
	decide := func(b *browser) response {
		return b.call(http.MethodPost, "/v1/installation/changes/"+pending.str("id")+"/approve", "same-origin",
			map[string]any{"envelope_hash": pending.str("envelope_hash")})
	}
	if r := decide(desk); r.Status < 400 {
		t.Fatalf("the Super admin who asked approved it: %d %s", r.Status, r.Raw)
	}
	if r := decide(phone); r.Status != http.StatusOK || r.str("address") != "192.168.10.0/24" || r.str("change_id") != pending.str("id") {
		t.Fatalf("approving: %d %s", r.Status, r.Raw)
	}

	trail := s.installationTrail(t)
	for action, want := range map[string]int{"private_endpoint.registered": 2, "private_endpoint.changed": 1, "private_endpoint.retired": 1} {
		if n := strings.Count(trail, `"action":"`+action+`"`); n != want {
			t.Errorf("%s in the trail: %d, want %d", action, n, want)
		}
	}
}
