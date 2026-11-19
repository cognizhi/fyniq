//go:build local

package nemura_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/identity"
	"github.com/cognizhi/fyniq/internal/passkey"
	"github.com/cognizhi/fyniq/internal/passkey/passkeytest"
)

const (
	rpOrigin = "https://people.example.com"
	rpID     = "example.com" // the registrable domain Fyniq and Nemura share (question 10)
)

type sessionFor struct {
	c       *company
	token   string
	session uuid.UUID
}

func (c *company) sessionOf(t *testing.T, member uuid.UUID, at time.Time) sessionFor {
	t.Helper()
	var token string
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		token, err = nemura.StartSession(ctx, tx, c.org, at, member)
		return err
	})
	s, err := store(t, "nemura_api").Authenticate(context.Background(), token, at)
	if err != nil {
		t.Fatal(err)
	}
	return sessionFor{c: c, token: token, session: s.ID}
}

func options(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func relyingParty(t *testing.T) *passkey.RelyingParty {
	t.Helper()
	rp, err := passkey.NewFor(rpOrigin, rpID, "Nemura")
	if err != nil {
		t.Fatal(err)
	}
	return rp
}

// register runs a whole registration for the member with a fresh authenticator.
func register(t *testing.T, c *company, rp *passkey.RelyingParty, s sessionFor, member uuid.UUID, at time.Time) *passkeytest.Authenticator {
	t.Helper()
	ctx := context.Background()
	api := store(t, "nemura_api")
	opts, err := api.BeginRegistrationFor(ctx, rp, c.org, at, s.session, member, "Pia")
	if err != nil {
		t.Fatal(err)
	}
	auth := passkeytest.New(t, rpID, rpOrigin)
	if _, err := api.CompleteRegistration(ctx, rp, c.org, at, s.session, member, "Pia", "Pia's laptop", auth.Create(passkeytest.Challenge(t, options(t, opts)))); err != nil {
		t.Fatal(err)
	}
	return auth
}

// A member registers a passkey for the shared relying party, and a session is stepped up by an assertion with it: the step-up is fresh for five minutes and
// recorded in the session and the audit trail (NM-ARC-06, SEC-01).
// req: NM-ARC-06
// asvs: V6.4.1, V7.2.2, V14.2.6
func TestAPasskeyRegistersAndStepsASessionUp(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	rp := relyingParty(t)
	if rp.ID() != rpID {
		t.Fatalf("the relying party ID is %s, want the registrable domain %s", rp.ID(), rpID)
	}
	at := today.Add(9 * time.Hour)
	s := c.sessionOf(t, c.hr, at)
	api := store(t, "nemura_api")
	// A step-up with no passkey says so.
	if _, err := api.BeginStepUpFor(ctx, rp, c.org, at, s.session, c.hr, "Hana"); !errors.Is(err, nemura.ErrNoPasskey) {
		t.Errorf("a step-up with no passkey: %v", err)
	}
	auth := register(t, c, rp, s, c.hr, at)
	opts, err := api.BeginStepUpFor(ctx, rp, c.org, at.Add(time.Minute), s.session, c.hr, "Hana")
	if err != nil {
		t.Fatal(err)
	}
	if err := api.CompleteStepUp(ctx, rp, c.org, at.Add(time.Minute), s.session, c.hr, "Hana", auth.Get(passkeytest.Challenge(t, options(t, opts)))); err != nil {
		t.Fatal(err)
	}
	got, err := api.Authenticate(ctx, s.token, at.Add(2*time.Minute))
	if err != nil || got.FreshStepUp(at.Add(2*time.Minute)) != nil || got.FreshStepUp(at.Add(7*time.Minute)) == nil {
		t.Errorf("the step-up: %+v, %v (fresh now, stale after five minutes)", got, err)
	}
	var events int
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action IN ('nemura.passkey.registered', 'nemura.passkey.used', 'nemura.session.stepped_up')", c.org).Scan(&events); err != nil || events != 3 {
		t.Errorf("audit events of the ceremonies: %d, %v", events, err)
	}
}

// A challenge is used once, whatever the answer; a wrong answer, a replayed one, an expired challenge, another session's and another
// member's passkey step nobody up; a signature counter that goes back is refused as a cloned authenticator; and the origin must be the app's.
// req: NM-ARC-06
// asvs: V6.4.2, V7.2.2, V8.3.1
func TestAPasskeyCeremonyIsOnceAndRefusesWhatIsNotTheMembers(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	rp := relyingParty(t)
	at := today.Add(10 * time.Hour)
	s := c.sessionOf(t, c.hr, at)
	api := store(t, "nemura_api")
	auth := register(t, c, rp, s, c.hr, at)
	begin := func(when time.Time, s sessionFor) map[string]any {
		o, err := api.BeginStepUpFor(ctx, rp, c.org, when, s.session, c.hr, "Hana")
		if err != nil {
			t.Fatal(err)
		}
		return options(t, o)
	}
	// A wrong answer (a challenge of another ceremony) is refused, and uses the challenge: the right answer to it no longer works.
	opts := begin(at, s)
	if err := api.CompleteStepUp(ctx, rp, c.org, at, s.session, c.hr, "Hana", auth.Get(make([]byte, 32))); !errors.Is(err, passkey.ErrRefused) {
		t.Errorf("an answer to another challenge: %v", err)
	}
	if err := api.CompleteStepUp(ctx, rp, c.org, at, s.session, c.hr, "Hana", auth.Get(passkeytest.Challenge(t, opts))); !errors.Is(err, nemura.ErrNoChallenge) {
		t.Errorf("the right answer after a wrong one used the challenge: %v", err)
	}
	// A good answer works once; replayed it finds no challenge.
	opts = begin(at, s)
	answer := auth.Get(passkeytest.Challenge(t, opts))
	if err := api.CompleteStepUp(ctx, rp, c.org, at, s.session, c.hr, "Hana", answer); err != nil {
		t.Fatal(err)
	}
	if err := api.CompleteStepUp(ctx, rp, c.org, at, s.session, c.hr, "Hana", answer); !errors.Is(err, nemura.ErrNoChallenge) {
		t.Errorf("a replayed answer: %v", err)
	}
	// An expired challenge finds none.
	opts = begin(at, s)
	late := at.Add(nemura.ChallengeTTL + time.Second)
	if err := api.CompleteStepUp(ctx, rp, c.org, late, s.session, c.hr, "Hana", auth.Get(passkeytest.Challenge(t, opts))); !errors.Is(err, nemura.ErrNoChallenge) {
		t.Errorf("an expired challenge: %v", err)
	}
	// Another session cannot answer this session's challenge.
	other := c.sessionOf(t, c.hr, at)
	opts = begin(at, s)
	if err := api.CompleteStepUp(ctx, rp, c.org, at, other.session, c.hr, "Hana", auth.Get(passkeytest.Challenge(t, opts))); !errors.Is(err, nemura.ErrNoChallenge) {
		t.Errorf("another session's answer: %v", err)
	}
	// A passkey the member did not register, and one from the wrong origin, are refused.
	stranger := passkeytest.New(t, rpID, rpOrigin)
	if err := api.CompleteStepUp(ctx, rp, c.org, at, s.session, c.hr, "Hana", stranger.Get(passkeytest.Challenge(t, opts))); !errors.Is(err, passkey.ErrRefused) {
		t.Errorf("an unregistered passkey: %v", err)
	}
	opts = begin(at, s)
	auth.Origin = "https://evil.example"
	if err := api.CompleteStepUp(ctx, rp, c.org, at, s.session, c.hr, "Hana", auth.Get(passkeytest.Challenge(t, opts))); !errors.Is(err, passkey.ErrRefused) {
		t.Errorf("an answer from another origin: %v", err)
	}
	auth.Origin = rpOrigin
	// The counter never goes back: an answer with an older counter is refused (a cloned authenticator).
	opts = begin(at, s)
	if err := api.CompleteStepUp(ctx, rp, c.org, at, s.session, c.hr, "Hana", auth.Sign(passkeytest.Challenge(t, opts), 1)); !errors.Is(err, passkey.ErrRefused) {
		t.Errorf("a counter that went back: %v", err)
	}
	// The database says the same, below the code.
	if _, err := connect(t, db.SuperURL()).Exec(ctx, "UPDATE nemura.passkey SET sign_count = sign_count - 1 WHERE member_id = $1 AND sign_count > 1", c.hr); err == nil || !strings.Contains(err.Error(), "NM103") {
		t.Errorf("a counter that goes back in the database: %v", err)
	}
	// The member's passkey is the member's: another member's session cannot register on their behalf, and the identity of a member
	// who is deactivated no longer opens a session, so no step-up either.
	if _, err := store(t, "nemura_api").Authenticate(ctx, "x."+strings.Repeat("a", 30), at); err == nil || !strings.Contains(err.Error(), identity.CodeSignInRefused) {
		t.Errorf("a made-up token: %v", err)
	}
}
