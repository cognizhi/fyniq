//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// A person sees the browsers they are signed in on, the current one marked, and signs any of them out, or all but the
// current one; nobody else's. Each sign-in sends them an alert by email, to the address their provider verified.
// req: FR-ADM-03
// req: SEC-05
// asvs: V7.4.2, V7.5.2
func TestYourDevices(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	desk, laptop, phone := s.browser(), s.browser(), s.browser()
	desk.signIn("founder-a", true) // before setup: no trail to queue an alert in
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	if r := desk.call(http.MethodPost, "/v1/setup", "same-origin", map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}}); r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}
	laptop.email = "mei.ling@example.com"
	laptop.signIn("founder-a", true)

	l := desk.call(http.MethodGet, "/v1/session/devices", "same-origin", nil)
	items, _ := l.Body["items"].([]any)
	if l.Status != 200 || len(items) != 2 || !strings.Contains(l.Raw, `"browser":"Go-http-client/1.1"`) || !strings.Contains(l.Raw, `"current":true`) ||
		!strings.Contains(l.Raw, `"current":false`) || !strings.Contains(l.Raw, `"step_up_method":"sign_in"`) {
		t.Fatalf("the devices: %d %s", l.Status, l.Raw)
	}
	var deskID string
	for _, it := range items {
		if d := it.(map[string]any); d["current"] == true {
			deskID = d["id"].(string)
		}
	}
	if strings.Contains(l.Raw, string(s.sessionHash(desk))) {
		t.Error("a device's ID shows its session")
	}

	// The laptop signs the desk out; an unknown device is nobody's.
	s.problemIs(laptop.call(http.MethodDelete, "/v1/session/devices/00000000000000000000000000000000", "same-origin", nil), "signOutDevice", 404, CodeNotFound)
	if r := laptop.call(http.MethodDelete, "/v1/session/devices/"+deskID, "same-origin", nil); r.Status != 200 || r.str("id") != deskID {
		t.Fatalf("signing the desk out: %d %s", r.Status, r.Raw)
	}
	s.problemIs(desk.session(), "getSession", 401, CodeUnauthenticated)

	// The phone signs in; the laptop signs out everywhere else.
	phone.signIn("founder-a", true)
	if r := laptop.call(http.MethodPost, "/v1/session/devices/sign-out-others", "same-origin", nil); r.Status != 200 || r.Body["ended"] != float64(1) {
		t.Fatalf("signing out everywhere else: %d %s", r.Status, r.Raw)
	}
	s.problemIs(phone.session(), "getSession", 401, CodeUnauthenticated)
	if r := laptop.session(); r.Status != 200 {
		t.Fatalf("the laptop after signing out everywhere else: %d %s", r.Status, r.Raw)
	}
	if !strings.Contains(s.log.String(), `"msg":"api.session.ended"`) {
		t.Error("the sign-outs were not logged")
	}

	// One alert, for the laptop's sign-in with a verified address, queued in the installation's trail.
	trail := s.installationTrail(t)
	if n := strings.Count(trail, `"sign_in_alert"`); n != 1 || !strings.Contains(trail, `"mei.ling@example.com"`) {
		t.Errorf("sign-in alerts in the trail: %d\n%s", n, trail)
	}
}
