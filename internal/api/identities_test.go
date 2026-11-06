//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// link links a sign-in as subject to b's account through the sign-in page, sent from site ("" for none), and returns
// where the browser ended.
func (b *browser) link(subject, site string) string {
	b.s.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, b.s.srv.URL+"/auth/signin?provider=local&link=1", nil)
	if site != "" {
		req.Header.Set("Sec-Fetch-Site", site)
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.Request.URL.Path != "/auth/dev/authorize" {
		return resp.Request.URL.RequestURI() // refused before the provider
	}
	form := resp.Request.URL.Query()
	form.Set("subject", subject)
	form.Set("name", "Mei Ling Tan")
	form.Set("decision", "sign-in")
	form.Set("email", "mei.ling@example.com") // the same email as the account's: it links nothing by itself
	resp, err = b.c.PostForm(b.s.srv.URL+"/auth/dev/authorize", form)
	if err != nil {
		b.s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.Request.URL.RequestURI()
}

// identities are b's sign-ins, by provider subject order as listed.
func (b *browser) identities() []map[string]any {
	b.s.t.Helper()
	r := b.call(http.MethodGet, "/v1/session/identities", "same-origin", nil)
	if r.Status != http.StatusOK {
		b.s.t.Fatalf("listing the sign-ins: %d %s", r.Status, r.Raw)
	}
	var out []map[string]any
	for _, it := range r.Body["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

// A person links a sign-in to their account by signing in with it, from Fyniq, in a session that confirmed it is them
// in the last 5 minutes; lists their sign-ins; and removes one, never the one in use nor their last, which ends its
// sessions. An account at a provider signs one person in: another's is refused, a matching email links nothing, and the
// database refuses a link or a removal from a session without a recent second factor whatever the API checks.
// req: FR-ADM-08
// asvs: V6.8.1, V7.5.1
func TestYourSignIns(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	desk, phone, org := s.founders(t)
	told := func() bool { // whether the founder was sent a security notice of a sign-in linked or removed
		t.Helper()
		var yes bool
		if err := s.env.store.Do(ctx, uuid.MustParse(org), func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			yes, err = u.Notifications().Emitted(ctx, notify.SignInMethodType, uuid.MustParse(desk.session().str("user_id")))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return yes
	}

	mine := desk.identities()
	if len(mine) != 1 || mine[0]["current"] != true || mine[0]["provider"] != "local" || mine[0]["linked_at"] != nil {
		t.Fatalf("the founder's sign-ins: %v", mine)
	}
	if told() {
		t.Fatal("a notice of a sign-in linked before any was")
	}
	// The last sign-in, which is also the one in use, stays.
	s.problemIs(desk.call(http.MethodDelete, "/v1/session/identities/"+mine[0]["id"].(string), "same-origin", nil), "unlinkIdentity", 409,
		storage.CodeIdentity)

	// From another site, or without a recent second factor, nothing is linked; the browser goes back to the profile.
	if to := desk.link("founder-a-zoho", ""); to != "/settings/profile?link_error=AUTH-SIGNIN-05" {
		t.Errorf("a link from another site ended at %s", to)
	}
	laptop := s.browser()
	laptop.signIn("founder-a", false)
	if to := laptop.link("founder-a-zoho", "same-origin"); to != "/settings/profile?link_error=AUTH-SIGNIN-06" {
		t.Errorf("a link without a second factor ended at %s", to)
	}
	// The database refuses it too, whatever the API checks.
	if _, err := s.env.store.LinkIdentity(ctx, storage.IdentityLink{SessionHash: s.sessionHash(laptop), Issuer: "urn:test", Subject: "x",
		Provider: "local", Now: s.env.clock.Now()}); storage.CodeOf(err) != storage.CodeIdentityStepUp {
		t.Errorf("the store linked from a session without a second factor: %v", err)
	}

	// The founder links a second sign-in; their session stays as it was.
	before := s.sessionHash(desk)
	if to := desk.link("founder-a-zoho", "same-origin"); to != "/settings/profile?linked=local" {
		t.Fatalf("linking ended at %s", to)
	}
	if string(s.sessionHash(desk)) != string(before) {
		t.Error("linking replaced the session")
	}
	if !told() {
		t.Error("the founder was not told a sign-in was linked")
	}
	mine = desk.identities()
	if len(mine) != 2 || mine[1]["current"] != false || mine[1]["linked_at"] == nil {
		t.Fatalf("after linking: %v", mine)
	}
	// It signs the founder in: the same user, in the same organisation.
	tablet := s.browser()
	tablet.signIn("founder-a-zoho", true)
	if a, b := desk.session(), tablet.session(); a.str("user_id") == "" || a.str("user_id") != b.str("user_id") {
		t.Fatalf("the linked sign-in is someone else: %s / %s", a.Raw, b.Raw)
	}
	// Another account whose verified email is the same as the linked one's links nothing: it signs nobody in.
	stranger := s.browser()
	stranger.email = "mei.ling@example.com"
	stranger.signIn("founder-a-google", true)
	if r := stranger.session(); r.Status != http.StatusOK || r.str("user_id") != "" {
		t.Errorf("a matching email signed the founder in: %d %s", r.Status, r.Raw)
	}
	// Linked already, to them or to anyone else: the second founder's sign-in stays theirs, whatever its email.
	if to := desk.link("founder-a-zoho", "same-origin"); to != "/settings/profile?link_error=AUTH-SIGNIN-07" {
		t.Errorf("linking again ended at %s", to)
	}
	if to := desk.link("founder-b", "same-origin"); to != "/settings/profile?link_error=AUTH-SIGNIN-07" {
		t.Errorf("linking the other founder's sign-in ended at %s", to)
	}
	if len(phone.identities()) != 1 || len(desk.identities()) != 2 {
		t.Error("a refused link moved a sign-in")
	}

	// The tablet cannot remove the sign-in it uses; another's is not found; a session without a second factor uses no API
	// (SEC-01), and the database refuses its removal too.
	zoho := mine[1]["id"].(string)
	s.problemIs(tablet.call(http.MethodDelete, "/v1/session/identities/"+zoho, "same-origin", nil), "unlinkIdentity", 409, storage.CodeIdentity)
	s.problemIs(phone.call(http.MethodDelete, "/v1/session/identities/"+zoho, "same-origin", nil), "unlinkIdentity", 404, CodeNotFound)
	s.problemIs(laptop.call(http.MethodDelete, "/v1/session/identities/"+zoho, "same-origin", nil), "unlinkIdentity", 403, CodeSecondFactor)
	if _, err := s.env.store.UnlinkIdentity(ctx, s.sessionHash(laptop), "urn:test", "x", s.env.clock.Now()); storage.CodeOf(err) != storage.CodeIdentityStepUp {
		t.Errorf("the store removed from a session without a second factor: %v", err)
	}
	// A second factor more than 5 minutes old removes nothing, nor links anything, in the API or the database.
	s.clock.Advance(authn.StepUpWindow + time.Minute)
	s.problemIs(desk.call(http.MethodDelete, "/v1/session/identities/"+zoho, "same-origin", nil), "unlinkIdentity", 403, CodeStepUpRequired)
	if _, err := s.env.store.UnlinkIdentity(ctx, s.sessionHash(desk), "urn:test", "x", s.env.clock.Now()); storage.CodeOf(err) != storage.CodeIdentityStepUp {
		t.Errorf("the store removed from a session whose second factor is 6 minutes old: %v", err)
	}
	if to := desk.link("founder-a-github", "same-origin"); to != "/settings/profile?link_error=AUTH-SIGNIN-06" {
		t.Errorf("a link 6 minutes after the second factor ended at %s", to)
	}
	desk.signIn("founder-a", true)

	// The desk removes it: the tablet's session ends with it.
	if r := desk.call(http.MethodDelete, "/v1/session/identities/"+zoho, "same-origin", nil); r.Status != 200 || r.str("id") != zoho {
		t.Fatalf("removing the sign-in: %d %s", r.Status, r.Raw)
	}
	if r := tablet.session(); r.Status == http.StatusOK && r.str("user_id") != "" {
		t.Errorf("the removed sign-in's session still signs the founder in: %s", r.Raw)
	}
	if len(desk.identities()) != 1 {
		t.Error("the sign-in was not removed")
	}

	// The installation's trail records both, without the account at the provider.
	trail := s.installationTrail(t)
	if !strings.Contains(trail, `"action":"identity.linked"`) || !strings.Contains(trail, `"action":"identity.unlinked"`) ||
		strings.Contains(trail, "founder-a-zoho") {
		t.Errorf("the trail:\n%s", trail)
	}
}
