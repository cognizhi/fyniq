//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// An Administrator names the organisation's email-in address and a member their own, each over its ETag; a name is
// checked, a reserved one refused, another organisation's refused; leaving the name out retires the address. Every
// member reads the organisation's and their own; each change is audited.
// req: FR-INT-22
// req: FR-DOC-12
func TestEmailInAddressesHaveReadableNames(t *testing.T) {
	f := newFounders(t)
	e := f.e
	admin := e.as(authz.Administrator)
	got := e.call(http.MethodGet, "/v1/organisation/email/capture-addresses", e.as(authz.Employee), nil)
	if got.Status != http.StatusOK || got.str("domain") != "in.example.com" || got.Body["organisation"] != nil || got.Body["mine"] != nil {
		t.Fatalf("before any address: %d %s", got.Status, got.Raw)
	}
	const path = "/v1/organisation/email/capture-addresses/organisation"
	tag := got.str("organisation_etag")
	for name, c := range map[string]struct {
		body map[string]any
		code string
	}{
		"a capital and a space": {map[string]any{"name": "My Bills"}, CodeCaptureName},
		"two dots":              {map[string]any{"name": "bills..in"}, CodeCaptureName},
		"too short":             {map[string]any{"name": "ab"}, CodeCaptureName},
		"reserved":              {map[string]any{"name": "postmaster"}, CodeCaptureReserved},
	} {
		if r := e.call(http.MethodPut, path, admin, c.body, "If-Match", tag); r.problemCode() != c.code {
			t.Errorf("%s: %d %s; want %s", name, r.Status, r.Raw, c.code)
		}
	}
	name := "bills-" + uuid.NewString()[:6]
	set := e.call(http.MethodPut, path, admin, map[string]any{"name": strings.ToUpper(name)}, "If-Match", tag, "Idempotency-Key", uuid.NewString())
	org, _ := set.Body["organisation"].(map[string]any)
	if set.Status != http.StatusOK || org["address"] != name+"@in.example.com" || org["name"] != name || set.str("organisation_etag") == tag {
		t.Fatalf("naming it: %d %s", set.Status, set.Raw)
	}
	if r := e.call(http.MethodPut, path, admin, map[string]any{"name": "other-" + name}, "If-Match", tag); r.Status != http.StatusPreconditionFailed {
		t.Fatalf("over the stale ETag: %d %s", r.Status, r.Raw)
	}

	// Another organisation cannot take the name.
	other := inStandardMode(e)
	if r := other.call(http.MethodPut, path, other.as(authz.Administrator), map[string]any{"name": name}, "If-Match", `"none"`); r.problemCode() != CodeCaptureTaken {
		t.Fatalf("another organisation took the name: %d %s", r.Status, r.Raw)
	}

	// A member names their own; a caller who is no person has none.
	me := f.as(f.ahkow, authz.Employee)
	mine := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/mine", me, map[string]any{"name": "ahkow-" + name}, "If-Match", `"none"`)
	if m, _ := mine.Body["mine"].(map[string]any); mine.Status != http.StatusOK || m["address"] != "ahkow-"+name+"@in.example.com" || mine.Body["organisation"] == nil {
		t.Fatalf("a member's own: %d %s", mine.Status, mine.Raw)
	}
	service := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Employee}})
	if r := e.call(http.MethodPut, "/v1/organisation/email/capture-addresses/mine", service, map[string]any{"name": "x-" + name}, "If-Match", `"none"`); r.problemCode() != CodeCaptureNoUser {
		t.Fatalf("a caller without a user: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/organisation/email/capture-addresses", f.as(f.weiling, authz.Employee), nil); r.Body["mine"] != nil || r.Body["organisation"] == nil {
		t.Fatalf("another member sees an address of their own: %s", r.Raw)
	}

	// Leaving the name out retires the organisation's address.
	gone := e.call(http.MethodPut, path, admin, map[string]any{}, "If-Match", set.str("organisation_etag"))
	if gone.Status != http.StatusOK || gone.Body["organisation"] != nil || gone.str("organisation_etag") != `"none"` {
		t.Fatalf("retiring it: %d %s", gone.Status, gone.Raw)
	}
	trail := auditTrail(t, e)
	for _, action := range []string{"capture_address.created", "capture_address.retired"} {
		if !strings.Contains(trail, action) {
			t.Errorf("the audit trail lacks %s", action)
		}
	}
}
