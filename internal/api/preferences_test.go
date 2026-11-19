//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// A person's preferences are kept on the server, so they are the same on every device (FR-UX-23): a save sets what it
// names and takes back what it resets, a stale one is refused, each save is the state and its audit event and nothing
// else, the session carries them, and nobody sets another person's. The mode a person starts in follows the roles'
// starting modes the Administrator sets, the Owner's winning (FR-UX-27).
// req: FR-UX-23
// req: FR-UX-27
// req: NFR-L10N-01
func TestPreferencesAreKeptOnTheServer(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	weiling := f.as(f.weiling, authz.Owner, authz.Controller)
	drainWork(t, e)

	// Before any choice: the defaults, and the mode the roles start in (an Owner starts in Basic, though a Controller starts in Expert).
	got := e.call(http.MethodGet, "/v1/preferences", ahkow, nil)
	if got.Status != 200 || got.Body["default_mode"] != "basic" || got.Body["mode"] != nil || got.Header.Get("ETag") != got.str("etag") {
		t.Fatalf("before a choice: %d %s", got.Status, got.Raw)
	}
	_, seq := auditActionsAfter(t, e, 0)

	saved := e.call(http.MethodPatch, "/v1/preferences", ahkow, map[string]any{"mode": "expert", "number_format": "dot_comma", "date_format": "iso",
		"shortcuts_on": false, "shortcut_keys": map[string]any{"go.home": "g h"}}, "If-Match", got.str("etag"))
	if saved.Status != 200 || saved.Body["mode"] != "expert" || saved.Body["number_format"] != "dot_comma" || saved.Body["date_format"] != "iso" || saved.Body["shortcuts_on"] != false {
		t.Fatalf("saving: %d %s", saved.Status, saved.Raw)
	}
	// A save names only what it changes: the rest stays.
	more := e.call(http.MethodPatch, "/v1/preferences", ahkow, map[string]any{"theme": "dark", "reset": []string{"date_format"}}, "If-Match", saved.Header.Get("ETag"))
	if more.Status != 200 || more.Body["theme"] != "dark" || more.Body["mode"] != "expert" || more.Body["date_format"] != nil || more.Body["number_format"] != "dot_comma" {
		t.Fatalf("a second save: %d %s", more.Status, more.Raw)
	}
	if keys, _ := more.Body["shortcut_keys"].(map[string]any); keys["go.home"] != "g h" {
		t.Fatalf("the keys stay: %s", more.Raw)
	}
	if stale := e.call(http.MethodPatch, "/v1/preferences", ahkow, map[string]any{"mode": "basic"}, "If-Match", saved.Header.Get("ETag")); stale.Status != 412 {
		t.Fatalf("a stale save was kept: %d %s", stale.Status, stale.Raw)
	}
	if r := e.call(http.MethodPatch, "/v1/preferences", ahkow, map[string]any{"mode": "basic"}); r.Status != 428 {
		t.Fatalf("a save without If-Match: %d %s", r.Status, r.Raw)
	}
	// The preview checks and keeps nothing.
	if prev := e.call(http.MethodPatch, "/v1/preferences", ahkow, map[string]any{"mode": "basic"}, "If-Match", more.Header.Get("ETag"), "Prefer", "return=preview"); prev.Status != 200 || prev.Body["mode"] != "basic" {
		t.Fatalf("a preview: %d %s", prev.Status, prev.Raw)
	}
	if again := e.call(http.MethodGet, "/v1/preferences", ahkow, nil); again.Body["mode"] != "expert" {
		t.Fatalf("a preview saved: %s", again.Raw)
	}
	// What is not allowed is refused with its code and the field.
	for _, field := range []string{"mode", "number_format", "date_format", "theme"} {
		r := e.call(http.MethodPatch, "/v1/preferences", ahkow, map[string]any{field: "bogus"}, "If-Match", more.Header.Get("ETag"))
		if r.Status != 422 {
			t.Fatalf("%s: %d %s", field, r.Status, r.Raw)
		}
	}
	if r := e.call(http.MethodPatch, "/v1/preferences", ahkow, map[string]any{"reset": []string{"password"}}, "If-Match", more.Header.Get("ETag")); r.Status != 422 {
		t.Fatalf("a reset of an unknown field: %d %s", r.Status, r.Raw)
	}

	// The session carries them, and the organisation's time zone, so the first page is drawn as chosen.
	session := e.call(http.MethodGet, "/v1/session", ahkow, nil)
	prefs, _ := session.Body["preferences"].(map[string]any)
	if session.Status != 200 || prefs["mode"] != "expert" || prefs["theme"] != "dark" || session.str("time_zone") == "" {
		t.Fatalf("the session: %d %s", session.Status, session.Raw)
	}
	// Wei Ling's are her own.
	theirs := e.call(http.MethodGet, "/v1/preferences", weiling, nil)
	if theirs.Body["mode"] != nil || theirs.Body["theme"] != nil {
		t.Fatalf("another person's preferences: %s", theirs.Raw)
	}

	// Each save wrote its state and its audit event, and nothing else did; a preview wrote nothing.
	actions, _ := auditActionsAfter(t, e, seq)
	if len(actions) != 2 {
		t.Fatalf("the saves' writes: %v", actions)
	}
	for _, a := range actions {
		if a != "user_preference.set" {
			t.Fatalf("a save wrote %q: %v", a, actions)
		}
	}

	// A caller that is not a person has none to set; reading gives the defaults.
	client := e.principal(authn.Principal{Subject: "client-1", Client: "integration", Class: authn.ClassIntegration, Organisation: e.books.Org,
		Roles: []authz.Role{authz.Owner}})
	if r := e.call(http.MethodPatch, "/v1/preferences", client, map[string]any{"mode": "expert"}, "If-Match", "*"); r.Status != 422 || r.str("code") != CodePreferenceNoUser {
		t.Fatalf("a client's save: %d %s", r.Status, r.Raw)
	}
}
