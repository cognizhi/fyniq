package passkey_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/passkey"
	"github.com/cognizhi/fyniq/internal/passkey/passkeytest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

const origin = "https://fyniq.example"

// register makes a passkey for u through the relying party, as the API does.
func register(t *testing.T, rp *passkey.RelyingParty, u passkey.User, a *passkeytest.Authenticator) storage.Passkey {
	t.Helper()
	_, challenge, err := rp.Creation(u)
	if err != nil || len(challenge) != 32 {
		t.Fatalf("creation: %v (%d bytes)", err, len(challenge))
	}
	r, err := rp.Register(u, challenge, a.Create(challenge))
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	if string(r.CredentialID) != string(a.CredentialID) || len(r.PublicKey) == 0 || string(r.AAGUID) != string(a.AAGUID) {
		t.Fatalf("the passkey registered: %+v", r)
	}
	return storage.Passkey{ID: uuid.New(), UserID: u.ID, CredentialID: r.CredentialID, PublicKey: r.PublicKey, SignCount: r.SignCount,
		AAGUID: r.AAGUID, Transports: r.Transports, BackupEligible: r.BackupEligible, BackupState: r.BackupState, Name: "Laptop"}
}

// A passkey registers once, for this relying party and origin, with the person verified; an assertion is accepted only
// for its own challenge, origin and relying party, by a passkey of the user, verified, with a counter that went up.
// req: FR-ADM-02
// req: SEC-01
// req: FR-APR-04
// asvs: V6.5.1, V6.5.4
func TestPasskeyCeremonies(t *testing.T) {
	rp, err := passkey.New(origin + "/")
	if err != nil || rp.ID() != "fyniq.example" {
		t.Fatalf("the relying party: %v %v", rp, err)
	}
	u := passkey.User{ID: uuid.New(), Name: "Ann"}
	a := passkeytest.New(t, rp.ID(), origin)

	// Registration refusals.
	_, challenge, _ := rp.Creation(u)
	for name, bad := range map[string]func(*passkeytest.Authenticator){
		"another origin":          func(x *passkeytest.Authenticator) { x.Origin = "https://evil.example" },
		"another relying party":   func(x *passkeytest.Authenticator) { x.RPID = "evil.example" },
		"the person not verified": func(x *passkeytest.Authenticator) { x.Flags = passkeytest.FlagUP },
		"backed up, not eligible": func(x *passkeytest.Authenticator) { x.Flags |= passkeytest.FlagBS },
	} {
		x := passkeytest.New(t, rp.ID(), origin)
		bad(x)
		if _, err := rp.Register(u, challenge, x.Create(challenge)); !errors.Is(err, passkey.ErrRefused) {
			t.Errorf("registering with %s: %v", name, err)
		}
	}
	if _, err := rp.Register(u, []byte("another challenge of 32 bytes!!!"), a.Create(challenge)); !errors.Is(err, passkey.ErrRefused) {
		t.Errorf("registering against another challenge: %v", err)
	}

	k := register(t, rp, u, a)
	u.Passkeys = []storage.Passkey{k}
	if creation, _, err := rp.Creation(u); err != nil || len(creation.Response.CredentialExcludeList) != 1 {
		t.Errorf("a second registration excludes the first passkey: %v", err)
	}

	// Assertions.
	challenge = []byte("0123456789abcdef0123456789abcdef")
	if opts, err := rp.Assertion(u, challenge); err != nil || len(opts.Response.AllowedCredentials) != 1 ||
		string(opts.Response.Challenge) != string(challenge) || opts.Response.UserVerification != "required" {
		t.Fatalf("the assertion options: %+v %v", opts, err)
	}
	got, err := rp.Verify(u, challenge, a.Get(challenge))
	if err != nil || got.Passkey.ID != k.ID || got.SignCount != 1 {
		t.Fatalf("a good assertion: %+v %v", got, err)
	}
	u.Passkeys[0].SignCount = got.SignCount
	for name, response := range map[string][]byte{
		"replayed (the same counter)": a.Sign(challenge, 1),
		"an older counter":            a.Sign(challenge, 0),
		"another challenge":           a.Get([]byte("fedcba9876543210fedcba9876543210")),
	} {
		if _, err := rp.Verify(u, challenge, response); !errors.Is(err, passkey.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, bad := range map[string]func(*passkeytest.Authenticator){
		"another origin":          func(x *passkeytest.Authenticator) { x.Origin = "https://evil.example" },
		"another relying party":   func(x *passkeytest.Authenticator) { x.RPID = "evil.example" },
		"the person not verified": func(x *passkeytest.Authenticator) { x.Flags = passkeytest.FlagUP },
	} {
		x := *a
		bad(&x)
		if _, err := rp.Verify(u, challenge, x.Get(challenge)); !errors.Is(err, passkey.ErrRefused) {
			t.Errorf("an assertion with %s: %v", name, err)
		}
	}
	other := passkeytest.New(t, rp.ID(), origin)
	if _, err := rp.Verify(u, challenge, other.Get(challenge)); !errors.Is(err, passkey.ErrRefused) {
		t.Errorf("another person's passkey: %v", err)
	}
	revoked := u
	revoked.Passkeys = []storage.Passkey{u.Passkeys[0]}
	now := revoked.Passkeys[0].CreatedAt
	revoked.Passkeys[0].RevokedAt = &now
	if _, err := rp.Verify(revoked, challenge, a.Get(challenge)); !errors.Is(err, passkey.ErrRefused) {
		t.Errorf("a revoked passkey: %v", err)
	}
	if _, err := passkey.New("not an origin"); err == nil {
		t.Error("a relying party without an origin")
	}
}
