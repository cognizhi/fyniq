//go:build local

package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/passkey/passkeytest"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// installationTrail is the installation's audit trail, as the read-only role sees it.
func (s *sessionStack) installationTrail(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	reader, err := postgres.Open(ctx, s.readonly)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	org, err := s.store.InstallationOrganisation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var trail strings.Builder
	if err := reader.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return trail.String()
}

func options(t *testing.T, r response) map[string]any {
	t.Helper()
	o, _ := r.Body["options"].(map[string]any)
	if r.Status != http.StatusOK || o == nil || r.str("challenge_id") == "" || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("a challenge: %d %s", r.Status, r.Raw)
	}
	return o
}

func credential(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Every user signs in with a second factor: a session that passed none only reads itself and steps up (SEC-01). A
// person whose session passed one in the last 5 minutes registers a passkey for this site, which then steps any of
// their sessions up: the challenge is the session's own, works once and only within 5 minutes; the answer must come
// from this origin and relying party, with the person verified, from a live passkey whose counter went up. Registering
// and revoking are in the installation's trail.
// req: FR-ADM-02
// req: SEC-01
// req: SEC-05
// req: FR-APR-04
// asvs: V6.3.3, V6.5.1, V6.7.2
func TestPasskeys(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	origin := strings.Replace(s.srv.URL, "127.0.0.1", "localhost", 1) // the web app's origin, as startSessions gives it
	host, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	desk, laptop := s.browser(), s.browser()
	desk.signIn("founder-a", true)
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	if r := desk.call(http.MethodPost, "/v1/setup", "same-origin", map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}}); r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}

	// The laptop signed in without a second factor: it reads its session and asks to step up, nothing else.
	laptop.signIn("founder-a", false)
	s.problemIs(laptop.call(http.MethodGet, "/v1/members", "same-origin", nil), "listMembers", 403, CodeSecondFactor)
	s.problemIs(laptop.call(http.MethodGet, "/v1/session/passkeys", "same-origin", nil), "listPasskeys", 403, CodeSecondFactor)
	if r := laptop.session(); r.Status != 200 || r.Body["step_up_method"] != nil || r.Body["stepped_up_until"] != nil {
		t.Fatalf("a session without a second factor: %d %s", r.Status, r.Raw)
	}
	s.problemIs(laptop.call(http.MethodPost, "/v1/session/step-up/challenge", "same-origin", nil), "startStepUp", 409, CodeNoPasskey)
	if r := desk.session(); r.str("step_up_method") != "sign_in" {
		t.Fatalf("the desk's session: %s", r.Raw)
	}

	// The desk registers a passkey; a wrong answer spends its challenge.
	a := passkeytest.New(t, host.Hostname(), origin)
	start := func(b *browser) (string, []byte) {
		r := b.call(http.MethodPost, "/v1/session/passkeys/challenge", "same-origin", nil)
		return r.str("challenge_id"), passkeytest.Challenge(t, options(t, r))
	}
	id, challenge := start(desk)
	evil := *a
	evil.Origin = "https://evil.example"
	register := func(id string, response []byte) response {
		return desk.call(http.MethodPost, "/v1/session/passkeys", "same-origin", map[string]any{"challenge_id": id, "name": " Work laptop ",
			"credential": credential(t, response)})
	}
	s.problemIs(register(id, evil.Create(challenge)), "registerPasskey", 422, CodePasskeyRefused)
	s.problemIs(register(id, a.Create(challenge)), "registerPasskey", 409, CodeChallenge)
	id, challenge = start(desk)
	r := register(id, a.Create(challenge))
	if r.Status != http.StatusCreated || r.str("name") != "Work laptop" || r.Body["backup_eligible"] != false || r.str("id") == "" {
		t.Fatalf("registering: %d %s", r.Status, r.Raw)
	}
	keyID := r.str("id")
	s.problemIs(register(id, a.Create(challenge)), "registerPasskey", 409, CodeChallenge)
	s.problemIs(laptop.call(http.MethodPost, "/v1/session/passkeys/challenge", "same-origin", nil), "startPasskeyRegistration", 403, CodeSecondFactor)
	if l := desk.call(http.MethodGet, "/v1/session/passkeys", "same-origin", nil); l.Status != 200 || len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("the passkeys: %d %s", l.Status, l.Raw)
	}

	// The laptop steps up with it; the challenge is its own and works once.
	stepUp := func(b *browser) (string, []byte) {
		r := b.call(http.MethodPost, "/v1/session/step-up/challenge", "same-origin", nil)
		o := options(t, r)
		if !strings.Contains(r.Raw, `"userVerification":"required"`) || !strings.Contains(r.Raw, `"allowCredentials":[{`) {
			t.Fatalf("the step-up options: %v", o)
		}
		return r.str("challenge_id"), passkeytest.Challenge(t, o)
	}
	answer := func(b *browser, id string, response []byte) response {
		return b.call(http.MethodPost, "/v1/session/step-up", "same-origin", map[string]any{"challenge_id": id, "credential": credential(t, response)})
	}
	id, challenge = stepUp(laptop)
	deskID, _ := stepUp(desk)
	s.problemIs(answer(desk, id, a.Get(challenge)), "stepUp", 409, CodeChallenge) // another session's challenge
	id, challenge = stepUp(laptop)
	first := a.Get(challenge)
	r = answer(laptop, id, first)
	if r.Status != 200 || r.str("step_up_method") != "passkey" || r.str("passkey_id") != keyID || r.str("stepped_up_until") == "" {
		t.Fatalf("stepping up: %d %s", r.Status, r.Raw)
	}
	if r := laptop.session(); r.str("step_up_method") != "passkey" || r.Body["stepped_up_until"] == nil {
		t.Fatalf("the laptop's session after the step-up: %s", r.Raw)
	}
	if r := laptop.call(http.MethodGet, "/v1/members", "same-origin", nil); r.Status != 200 {
		t.Fatalf("the laptop after its step-up: %d %s", r.Status, r.Raw)
	}
	s.problemIs(answer(laptop, id, first), "stepUp", 409, CodeChallenge) // replayed
	id, challenge = stepUp(laptop)
	s.problemIs(answer(laptop, id, first), "stepUp", 422, CodePasskeyRefused) // an old answer to a new challenge
	for name, response := range map[string]func([]byte) []byte{
		"the same counter": func(c []byte) []byte { return a.Sign(c, a.Count) },
		"another origin":   func(c []byte) []byte { x := *a; x.Origin = "https://evil.example"; return x.Get(c) },
		"another site":     func(c []byte) []byte { x := *a; x.RPID = "evil.example"; return x.Get(c) },
		"not verified":     func(c []byte) []byte { x := *a; x.Flags = passkeytest.FlagUP; return x.Get(c) },
		"another passkey":  func(c []byte) []byte { return passkeytest.New(t, host.Hostname(), origin).Get(c) },
	} {
		id, challenge = stepUp(laptop)
		if r := answer(laptop, id, response(challenge)); r.Status != 422 || r.problemCode() != CodePasskeyRefused {
			t.Errorf("a step-up with %s: %d %s", name, r.Status, r.Raw)
		}
	}
	if !strings.Contains(s.log.String(), `"msg":"api.passkey.refused"`) {
		t.Error("the refusals were not logged")
	}

	// The step-up lasts 5 minutes; a challenge too. Revoking needs a fresh one; revoked, the passkey steps nobody up.
	s.clock.Advance(authn.StepUpWindow + time.Minute)
	s.problemIs(laptop.call(http.MethodDelete, "/v1/session/passkeys/"+keyID, "same-origin", nil), "revokePasskey", 403, CodeStepUpRequired)
	s.problemIs(answer(desk, deskID, a.Get(challenge)), "stepUp", 409, CodeChallenge) // expired
	id, challenge = stepUp(laptop)
	if r := answer(laptop, id, a.Get(challenge)); r.Status != 200 {
		t.Fatalf("stepping up again: %d %s", r.Status, r.Raw)
	}
	if r := laptop.call(http.MethodDelete, "/v1/session/passkeys/"+keyID, "same-origin", nil); r.Status != 200 || r.str("revoked_at") == "" ||
		!strings.HasSuffix(r.str("revoked_at"), "Z") {
		t.Fatalf("revoking: %d %s", r.Status, r.Raw)
	}
	s.problemIs(laptop.call(http.MethodDelete, "/v1/session/passkeys/"+keyID, "same-origin", nil), "revokePasskey", 404, CodeNotFound)
	phone := s.browser()
	phone.signIn("founder-a", false)
	s.problemIs(phone.call(http.MethodPost, "/v1/session/step-up/challenge", "same-origin", nil), "startStepUp", 409, CodeNoPasskey)

	trail := s.installationTrail(t)
	for _, want := range []string{`"action":"passkey.registered"`, `"action":"passkey.revoked"`, `"object":{"type":"webauthn_credential","id":"` + keyID} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the installation's trail", want)
		}
	}
	for _, secret := range []string{string(challenge), "BEGIN", "\"public_key\""} {
		if strings.Contains(s.log.String(), secret) {
			t.Errorf("the log holds %q", secret)
		}
	}
}

// sessionHash is the SHA-256 of b's session token, as the store keeps it.
func (s *sessionStack) sessionHash(b *browser) []byte {
	s.t.Helper()
	u, _ := url.Parse(s.srv.URL)
	for _, c := range b.c.Jar.Cookies(u) {
		if c.Name == authn.SessionCookie(false) {
			return authn.HashToken(c.Value)
		}
	}
	s.t.Fatal("the browser has no session")
	return nil
}

// The database is the backstop for passkeys: a registration challenge goes only to a session that passed a second
// factor in the last 5 minutes; a challenge is taken once, by its own session, for its purpose, before it expires,
// and answered once; a passkey exists only as the answer to a registration challenge, and steps a session up only as
// the answer to that session's step-up challenge, while live, with a counter that goes up. The owner cannot change a
// passkey's key or a challenge, nor delete either; the API reaches neither table but through the functions.
// req: FR-ADM-02
// req: SEC-01
// req: FR-APR-04
// asvs: V6.5.1, V6.7.2, V8.2.2
func TestAttackPasskeysAreAnsweredChallenges(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	desk, laptop := s.browser(), s.browser()
	desk.signIn("founder-a", true)
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	r := desk.call(http.MethodPost, "/v1/setup", "same-origin", map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}})
	if r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}
	user := uuid.MustParse(r.str("user_id"))
	laptop.signIn("founder-a", false)
	deskHash, laptopHash := s.sessionHash(desk), s.sessionHash(laptop)
	inst, err := s.store.InstallationOrganisation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := s.clock.Now()
	keys := func(f func(ctx context.Context, k storage.Passkeys) error) error {
		return s.store.Do(ctx, inst, func(ctx context.Context, u storage.UnitOfWork) error { return f(ctx, u.Passkeys()) })
	}
	random := func() []byte {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		return b
	}
	issue := func(session []byte, purpose string) (uuid.UUID, error) {
		var id uuid.UUID
		err := keys(func(ctx context.Context, k storage.Passkeys) (err error) {
			id, err = k.Challenge(ctx, storage.NewPasskeyChallenge{SessionHash: session, Purpose: purpose, Challenge: random(), Now: now,
				Expires: now.Add(ChallengeTTL)})
			return err
		})
		return id, err
	}
	take := func(id uuid.UUID, session []byte, purpose string) error {
		return keys(func(ctx context.Context, k storage.Passkeys) error {
			_, err := k.Take(ctx, id, session, purpose, now)
			return err
		})
	}
	register := func(ch uuid.UUID, session []byte) (storage.Passkey, error) {
		var p storage.Passkey
		err := keys(func(ctx context.Context, k storage.Passkeys) (err error) {
			p, err = k.Register(ctx, storage.NewPasskey{ChallengeID: ch, SessionHash: session, UserID: user, CredentialID: random(), PublicKey: random(),
				AAGUID: make([]byte, 16), Transports: []string{}, Name: "Key", Now: now})
			return err
		})
		return p, err
	}
	stepUp := func(ch uuid.UUID, session []byte, key uuid.UUID, count uint32) error {
		return keys(func(ctx context.Context, k storage.Passkeys) error {
			return k.StepUp(ctx, storage.PasskeyStepUp{ChallengeID: ch, SessionHash: session, PasskeyID: key, SignCount: count, Now: now})
		})
	}
	refused := func(what string, err error) {
		t.Helper()
		if storage.CodeOf(err) != storage.CodePasskey {
			t.Errorf("%s: want %s, got %v", what, storage.CodePasskey, err)
		}
	}
	gone := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("%s: want no challenge, got %v", what, err)
		}
	}

	_, err = issue(laptopHash, storage.PurposeRegister)
	refused("a registration challenge to a session without a recent second factor", err)
	ch, err := issue(deskHash, storage.PurposeRegister)
	if err != nil {
		t.Fatal(err)
	}
	_, err = register(ch, deskHash)
	refused("a passkey answering a challenge nobody took", err)
	gone("another session taking the challenge", take(ch, laptopHash, storage.PurposeRegister))
	gone("taking it for another purpose", take(ch, deskHash, storage.PurposeStepUp))
	if err := take(ch, deskHash, storage.PurposeRegister); err != nil {
		t.Fatal(err)
	}
	gone("taking it twice", take(ch, deskHash, storage.PurposeRegister))
	_, err = register(ch, laptopHash)
	refused("a passkey for another session's challenge", err)
	key, err := register(ch, deskHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = register(ch, deskHash)
	refused("a second passkey for one challenge", err)

	sc, err := issue(laptopHash, storage.PurposeStepUp)
	if err != nil {
		t.Fatal(err)
	}
	refused("a step-up answering a challenge nobody took", stepUp(sc, laptopHash, key.ID, 5))
	if err := take(sc, laptopHash, storage.PurposeStepUp); err != nil {
		t.Fatal(err)
	}
	refused("stepping up another session with the challenge", stepUp(sc, deskHash, key.ID, 5))
	if err := stepUp(sc, laptopHash, key.ID, 5); err != nil {
		t.Fatalf("a step-up: %v", err)
	}
	if got, err := s.store.Session(ctx, laptopHash, now, authn.SessionIdle); err != nil || got.StepUpMethod != "passkey" || !got.StepUpAt.Equal(now.Truncate(time.Second)) {
		t.Errorf("the stepped-up session: %+v %v", got, err)
	}
	refused("answering a step-up challenge twice", stepUp(sc, laptopHash, key.ID, 6))
	for name, count := range map[string]uint32{"the same counter": 5, "a counter that went back": 3} {
		c, err := issue(laptopHash, storage.PurposeStepUp)
		if err == nil {
			err = take(c, laptopHash, storage.PurposeStepUp)
		}
		if err != nil {
			t.Fatal(err)
		}
		refused(name, stepUp(c, laptopHash, key.ID, count))
	}
	if err := keys(func(ctx context.Context, k storage.Passkeys) error { return k.Revoke(ctx, user, key.ID, now) }); err != nil {
		t.Fatal(err)
	}
	c, _ := issue(laptopHash, storage.PurposeStepUp)
	_ = take(c, laptopHash, storage.PurposeStepUp)
	refused("a step-up by a revoked passkey", stepUp(c, laptopHash, key.ID, 9))
	gone("revoking it twice", keys(func(ctx context.Context, k storage.Passkeys) error { return k.Revoke(ctx, user, key.ID, now) }))
	late, _ := issue(laptopHash, storage.PurposeStepUp)
	s.clock.Advance(ChallengeTTL)
	now = s.clock.Now()
	gone("taking a challenge when it expired", take(late, laptopHash, storage.PurposeStepUp))

	sqlstate := func(err error) string {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return pg.Code
		}
		return ""
	}
	owner, err := pgx.Connect(ctx, s.migrator)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	api, err := pgx.Connect(ctx, s.api)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close(ctx)
	for _, c := range []struct {
		conn      *pgx.Conn
		what, sql string
		want      string
	}{
		{owner, "changing a passkey's key", "UPDATE fyniq.webauthn_credential SET public_key = '\\x00112233445566778899aabbccddeeff'", "FY140"},
		{owner, "un-revoking a passkey", "UPDATE fyniq.webauthn_credential SET revoked_at = NULL", "FY140"},
		{owner, "deleting a passkey", "DELETE FROM fyniq.webauthn_credential", "FY140"},
		{owner, "taking a challenge again", "UPDATE fyniq.webauthn_challenge SET taken_at = taken_at + interval '1 second' WHERE taken_at IS NOT NULL", "FY141"},
		{owner, "deleting a challenge", "DELETE FROM fyniq.webauthn_challenge", "FY141"},
		{api, "the API reading passkeys", "SELECT count(*) FROM fyniq.webauthn_credential", "42501"},
		{api, "the API reading challenges", "SELECT count(*) FROM fyniq.webauthn_challenge", "42501"},
	} {
		if _, err := c.conn.Exec(ctx, c.sql); sqlstate(err) != c.want {
			t.Errorf("%s: want %s, got %v", c.what, c.want, err)
		}
	}
}
