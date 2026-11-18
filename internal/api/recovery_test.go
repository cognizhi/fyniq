//go:build local

package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// memberIDOf is the membership ID of the member named name, as b's organisation lists it.
func memberIDOf(b *browser, name string) string {
	b.s.t.Helper()
	for _, x := range b.call(http.MethodGet, "/v1/members", "same-origin", nil).Body["items"].([]any) {
		if m := x.(map[string]any); m["display_name"] == name {
			return m["id"].(string)
		}
	}
	b.s.t.Fatalf("no member %s", name)
	return ""
}

// G4-060: a founder who lost every authenticator, their provider's second factor among them, recovers with two
// independent proofs: one of their recovery codes and another Owner's confirmation. Email or a support request alone
// never does. The recovery ends their other sessions and tokens, replaces their codes, notifies them and every Owner,
// enters the weekly review, and starts a 24-hour cooling-off in which approving, bank-detail, role and sign-in changes
// and confirming others' recoveries wait, while adding a new passkey does not.
// req: SEC-35
// req: G4-060
// asvs: V6.4.1, V6.4.2, V7.4.2
func TestAccountRecovery(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	desk, phone, org := s.founders(t) // two Owners: desk is Mei Ling Tan, phone Wei Jie Lim

	// Wei Jie makes their recovery codes: ten of 10 characters, shown once, kept as keyed hashes.
	if r := phone.call(http.MethodGet, "/v1/session/recovery-codes", "same-origin", nil); r.Status != 200 || r.Body["unused"] != float64(0) {
		t.Fatalf("before any codes: %d %s", r.Status, r.Raw)
	}
	made := phone.call(http.MethodPost, "/v1/session/recovery-codes", "same-origin", nil)
	codes, _ := made.Body["recovery_codes"].([]any)
	if made.Status != 200 || len(codes) != 10 || !regexp.MustCompile(`^[A-Z2-7]{5}-[A-Z2-7]{5}$`).MatchString(codes[0].(string)) {
		t.Fatalf("making the codes: %d %s", made.Status, made.Raw)
	}
	if r := phone.call(http.MethodGet, "/v1/session/recovery-codes", "same-origin", nil); r.Body["unused"] != float64(10) {
		t.Fatalf("after making them: %s", r.Raw)
	}
	if strings.Contains(s.installationTrail(t), strings.ReplaceAll(codes[0].(string), "-", "")) {
		t.Error("the trail holds a recovery code")
	}

	// They lose every authenticator: a new sign-in passes no second factor, and does nothing but read itself and recover.
	lost := s.browser()
	lost.signIn("founder-b", false)
	s.problemIs(lost.call(http.MethodGet, "/v1/members", "same-origin", nil), "listMembers", 403, CodeSecondFactor)
	recover := func(code, confirmation string) response {
		return lost.call(http.MethodPost, "/v1/session/recovery", "same-origin", map[string]any{"recovery_code": code, "confirmation_code": confirmation})
	}
	s.problemIs(recover(codes[0].(string), "NOT-A-CONFIRMATION-OF-ANYONE-0"), "recoverAccount", 409, storage.CodeRecovery)

	// The other Owner confirms it after speaking to them; nobody confirms their own.
	wei := memberIDOf(desk, "Wei Jie Lim")
	s.problemIs(phone.call(http.MethodPost, "/v1/members/"+wei+"/recovery-confirmations", "same-origin", nil), "confirmAccountRecovery", 409,
		storage.CodeRecovery)
	confirmed := desk.call(http.MethodPost, "/v1/members/"+wei+"/recovery-confirmations", "same-origin", nil)
	if confirmed.Status != http.StatusCreated || confirmed.str("code") == "" {
		t.Fatalf("confirming: %d %s", confirmed.Status, confirmed.Raw)
	}
	s.problemIs(recover("AAAAA-AAAAA", confirmed.str("code")), "recoverAccount", 409, storage.CodeRecovery) // a wrong code

	// Both proofs: the session passes its second factor by recovery; the old session ends; new codes; cooling-off.
	done := recover(codes[0].(string), confirmed.str("code"))
	newCodes, _ := done.Body["recovery_codes"].([]any)
	if done.Status != 200 || len(newCodes) != 10 || done.str("cooling_off_until") != s.clock.Now().Add(CoolingOff).UTC().Format(time.RFC3339) {
		t.Fatalf("recovering: %d %s", done.Status, done.Raw)
	}
	if me := lost.session(); me.str("step_up_method") != "recovery" || me.str("cooling_off_until") == "" {
		t.Fatalf("the recovered session: %s", me.Raw)
	}
	s.problemIs(phone.session(), "getSession", 401, CodeUnauthenticated)
	s.problemIs(recover(codes[0].(string), confirmed.str("code")), "recoverAccount", 409, storage.CodeRecovery) // used once
	again := desk.call(http.MethodPost, "/v1/members/"+wei+"/recovery-confirmations", "same-origin", nil)
	s.problemIs(recover(codes[1].(string), again.str("code")), "recoverAccount", 409, storage.CodeRecovery) // the old codes were replaced

	// The cooling-off holds changes of roles, sign-ins and others' recoveries; a new passkey may be added.
	s.problemIs(lost.call(http.MethodPost, "/v1/members/invitations", "same-origin", map[string]any{"display_name": "Someone", "roles": []string{"bookkeeper"}}),
		"createMemberInvitation", 403, CodeCoolingOff)
	s.problemIs(lost.call(http.MethodPost, "/v1/members/"+memberIDOf(lost, "Mei Ling Tan")+"/recovery-confirmations", "same-origin", nil),
		"confirmAccountRecovery", 403, CodeCoolingOff)
	if r := lost.call(http.MethodPost, "/v1/session/passkeys/challenge", "same-origin", nil); r.Status != 200 {
		t.Errorf("adding a passkey during the cooling-off: %d %s", r.Status, r.Raw)
	}
	if to := lost.link("founder-b-google", "same-origin"); to != "/settings/profile?link_error=AUTH-SIGNIN-09" {
		t.Errorf("a link during the cooling-off ended at %s", to)
	}

	// Recorded: the installation's trail, the confirmation in the organisation's, the weekly review, the notices.
	trail := s.installationTrail(t)
	if !strings.Contains(trail, `"action":"account.recovered"`) || !strings.Contains(trail, `"action":"recovery_codes.replaced"`) {
		t.Errorf("the installation's trail:\n%s", trail)
	}
	if o := s.trailOf(t, uuid.MustParse(org)); !strings.Contains(o, `"action":"account_recovery.confirmed"`) {
		t.Errorf("the organisation's trail:\n%s", o)
	}
	if r := desk.call(http.MethodGet, "/v1/review-log", "same-origin", nil); !strings.Contains(r.Raw, "account_recovery") {
		t.Errorf("the weekly review: %d %s", r.Status, r.Raw)
	}
	if err := s.env.store.Do(ctx, uuid.MustParse(org), func(ctx context.Context, u storage.UnitOfWork) error {
		told, err := u.Notifications().Emitted(ctx, notify.RecoveryType, uuid.MustParse(wei))
		if err == nil && !told {
			t.Error("nobody was told of the confirmation")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Once the cooling-off ends, the hold lifts.
	s.clock.Advance(CoolingOff + time.Minute)
	later := s.browser()
	later.signIn("founder-b", true)
	if me := later.session(); me.str("cooling_off_until") != "" {
		t.Errorf("still cooling off a day later: %s", me.Raw)
	}
	if r := later.call(http.MethodPost, "/v1/members/invitations", "same-origin", map[string]any{"display_name": "Someone", "roles": []string{"bookkeeper"}}); r.Status == http.StatusForbidden {
		t.Errorf("an invitation after the cooling-off: %d %s", r.Status, r.Raw)
	}
}

// The hold after an account recovery is the organisation's setting (SEC-35; rev 0.253, the owner's decision of 12 Nov
// 2026): with "Hold after an account recovery" set to 48 hours, a recovered founder cools off for 48 hours, not the
// default 24.
// req: SEC-35
// asvs: V6.4.1, V6.4.2
func TestTheHoldAfterARecoveryIsTheOrganisationsSetting(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	s := startSessions(t)
	desk, phone, org := s.founders(t)
	if err := s.env.store.Do(ctx, uuid.MustParse(org), func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "security.recovery_cooling_off_hours", "", "48", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	codes, _ := phone.call(http.MethodPost, "/v1/session/recovery-codes", "same-origin", nil).Body["recovery_codes"].([]any)
	lost := s.browser()
	lost.signIn("founder-b", false)
	confirmed := desk.call(http.MethodPost, "/v1/members/"+memberIDOf(desk, "Wei Jie Lim")+"/recovery-confirmations", "same-origin", nil)
	done := lost.call(http.MethodPost, "/v1/session/recovery", "same-origin", map[string]any{"recovery_code": codes[0], "confirmation_code": confirmed.str("code")})
	if want := s.clock.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339); done.Status != 200 || done.str("cooling_off_until") != want {
		t.Fatalf("recovering under a 48-hour hold: %d %s; want until %s", done.Status, done.Raw, want)
	}
}
