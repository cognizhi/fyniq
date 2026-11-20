//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// An Administrator pins a role to Basic mode (FR-UX-28): a person whose every role is pinned is shown Basic whatever they saved, is told they are pinned so the web app draws no
// switch, and has a save of Expert refused; a person with a role that is not pinned keeps the switch; the Owner and the Administrator cannot be pinned; and the pin only changes what is
// shown, so what the person may do is as it was.
// req: FR-UX-28
// asvs: V8.2.2, V4.2.1
func TestAnAdministratorPinsARoleToBasicAndOnlyThatRole(t *testing.T) {
	f := newFounders(t)
	e := f.e
	admin := f.as(f.ahkow, authz.Administrator)
	emp := f.as(f.weiling, authz.Employee)
	both := f.as(f.weiling, authz.Employee, authz.Accountant)

	// Before the pin Wei Ling chooses Expert, as Employees may.
	got := e.call(http.MethodGet, "/v1/preferences", emp, nil)
	if got.Status != 200 || got.Body["pinned_basic"] != nil {
		t.Fatalf("before the pin: %d %s", got.Status, got.Raw)
	}
	if s := e.call(http.MethodPatch, "/v1/preferences", emp, map[string]any{"mode": "expert"}, "If-Match", got.str("etag")); s.Status != 200 || s.Body["mode"] != "expert" {
		t.Fatalf("choosing Expert before the pin: %d %s", s.Status, s.Raw)
	}

	// The Administrator pins the Employee role; the Owner and the Administrator cannot be pinned.
	pin := e.call(http.MethodPatch, "/v1/organisation/settings/experience.pinned_basic.employee", admin, map[string]any{"value": "true"}, "If-Match", `"0"`)
	if pin.Status != 200 {
		t.Fatalf("pinning: %d %s", pin.Status, pin.Raw)
	}
	for _, role := range []string{"owner", "administrator"} {
		if r := e.call(http.MethodPatch, "/v1/organisation/settings/experience.pinned_basic."+role, admin, map[string]any{"value": "true"}, "If-Match", `"0"`); r.Status < 400 {
			t.Fatalf("pinning the %s: %d %s", role, r.Status, r.Raw)
		}
	}

	// Pinned: Basic is shown whatever they saved, the app is told, and Expert is refused with its code.
	now := e.call(http.MethodGet, "/v1/preferences", emp, nil)
	if now.Body["pinned_basic"] != true || now.Body["mode"] != "basic" || now.Body["default_mode"] != "basic" {
		t.Fatalf("pinned: %s", now.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, "/v1/preferences", emp, map[string]any{"mode": "expert"}, "If-Match", now.str("etag")), "setMyPreferences", 409, CodePinnedBasic)
	if ok := e.call(http.MethodPatch, "/v1/preferences", emp, map[string]any{"theme": "dark"}, "If-Match", now.str("etag")); ok.Status != 200 || ok.Body["theme"] != "dark" || ok.Body["mode"] != "basic" {
		t.Fatalf("another choice while pinned: %d %s", ok.Status, ok.Raw)
	}
	if s := e.call(http.MethodGet, "/v1/session", emp, nil); s.Status != 200 || s.Body["preferences"].(map[string]any)["pinned_basic"] != true {
		t.Fatalf("the session says so too: %d %s", s.Status, s.Raw)
	}

	// A person who also holds a role that is not pinned keeps the switch; the pin changes no right.
	mixed := e.call(http.MethodGet, "/v1/preferences", both, nil)
	if mixed.Body["pinned_basic"] != nil {
		t.Fatalf("an Employee who is also an Accountant is pinned: %s", mixed.Raw)
	}
	if s := e.call(http.MethodPatch, "/v1/preferences", both, map[string]any{"mode": "expert"}, "If-Match", mixed.str("etag")); s.Status != 200 {
		t.Fatalf("choosing Expert with an unpinned role: %d %s", s.Status, s.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/periods", emp, nil); r.Status != http.StatusForbidden {
		t.Fatalf("an Employee reads the periods whatever the mode: %d", r.Status)
	}
}
