// Package passkey is Fyniq as a WebAuthn relying party (PRD-001 FR-ADM-02, SEC-01, FR-APR-04): the ceremonies that
// register a person's passkey and that step a session up with one. go-webauthn checks each response: the challenge,
// the origin (the web app's, exactly), the relying party ID (its host), user presence and user verification, the
// signature with the passkey's public key, and the backup flags; this package refuses a signature counter that goes
// back (a cloned authenticator). Attestation is not asked for ("none"), so any authenticator the person trusts is
// accepted (rev 0.43, provisional). The challenges are the caller's: kept server-side, bound to the session, used once.
package passkey

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ErrRefused is a response that failed a check: nothing is registered and nobody is stepped up.
var ErrRefused = errors.New("the passkey's answer was refused")

// algorithms are the public keys Fyniq accepts, preferred first: ES256, EdDSA, RS256.
var algorithms = []protocol.CredentialParameter{
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgES256},
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgEdDSA},
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgRS256},
}

// RelyingParty is Fyniq to a person's authenticator.
type RelyingParty struct {
	w  *webauthn.WebAuthn
	id string
}

// New returns the relying party of the web app at origin: its ID is the origin's host.
func New(origin string) (*RelyingParty, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("passkey: %q is not the web app's origin", origin)
	}
	w, err := webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "Fyniq", RPOrigins: []string{u.Scheme + "://" + u.Host},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationRequired}})
	if err != nil {
		return nil, err
	}
	return &RelyingParty{w: w, id: u.Hostname()}, nil
}

// ID is the relying party ID: the web app's host.
func (rp *RelyingParty) ID() string { return rp.id }

// User is a person to their authenticator: their Fyniq user and their live passkeys.
type User struct {
	ID       uuid.UUID
	Name     string
	Passkeys []storage.Passkey
}

func (u User) WebAuthnID() []byte          { return u.ID[:] }
func (u User) WebAuthnName() string        { return u.Name }
func (u User) WebAuthnDisplayName() string { return u.Name }

func (u User) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.Passkeys))
	for _, k := range u.Passkeys {
		if k.RevokedAt != nil {
			continue
		}
		c := webauthn.Credential{ID: k.CredentialID, PublicKey: k.PublicKey, AttestationType: "none", AttestationFormat: "none",
			Flags:         webauthn.CredentialFlags{UserPresent: true, UserVerified: true, BackupEligible: k.BackupEligible, BackupState: k.BackupState},
			Authenticator: webauthn.Authenticator{AAGUID: k.AAGUID, SignCount: k.SignCount}}
		for _, t := range k.Transports {
			c.Transport = append(c.Transport, protocol.AuthenticatorTransport(t))
		}
		out = append(out, c)
	}
	return out
}

// session is the ceremony's state, rebuilt from the challenge Fyniq kept: no expiry here, since the store refuses a
// challenge taken late by the app's clock.
func (rp *RelyingParty) session(u User, challenge []byte) webauthn.SessionData {
	return webauthn.SessionData{Challenge: base64.RawURLEncoding.EncodeToString(challenge), RelyingPartyID: rp.id, UserID: u.WebAuthnID(),
		UserVerification: protocol.VerificationRequired, CredParams: algorithms}
}

// Creation returns what the browser passes to navigator.credentials.create for u, and its challenge (32 random bytes)
// to keep: u's passkeys are excluded, so an authenticator registers once.
func (rp *RelyingParty) Creation(u User) (*protocol.CredentialCreation, []byte, error) {
	var exclude []protocol.CredentialDescriptor
	for _, c := range u.WebAuthnCredentials() {
		exclude = append(exclude, c.Descriptor())
	}
	creation, s, err := rp.w.BeginRegistration(u, webauthn.WithCredentialParameters(algorithms), webauthn.WithExclusions(exclude),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation))
	if err != nil {
		return nil, nil, err
	}
	challenge, err := base64.RawURLEncoding.DecodeString(s.Challenge)
	return creation, challenge, err
}

// Registered is a passkey a registration response proved.
type Registered struct {
	CredentialID   []byte
	PublicKey      []byte
	SignCount      uint32
	AAGUID         []byte
	Transports     []string
	BackupEligible bool
	BackupState    bool
}

// Register checks the browser's answer (the JSON of the PublicKeyCredential) to the registration challenge for u.
func (rp *RelyingParty) Register(u User, challenge, response []byte) (Registered, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return Registered{}, errors.Join(ErrRefused, err)
	}
	c, err := rp.w.CreateCredential(u, rp.session(u, challenge), parsed)
	if err != nil {
		return Registered{}, errors.Join(ErrRefused, err)
	}
	r := Registered{CredentialID: c.ID, PublicKey: c.PublicKey, SignCount: c.Authenticator.SignCount, AAGUID: c.Authenticator.AAGUID,
		BackupEligible: c.Flags.BackupEligible, BackupState: c.Flags.BackupState, Transports: []string{}}
	if len(r.AAGUID) != 16 {
		r.AAGUID = make([]byte, 16) // an authenticator that names no model
	}
	for _, t := range c.Transport {
		r.Transports = append(r.Transports, string(t))
	}
	return r, nil
}

// Assertion returns what the browser passes to navigator.credentials.get for u to sign challenge with one of their
// passkeys, with user verification required.
func (rp *RelyingParty) Assertion(u User, challenge []byte) (*protocol.CredentialAssertion, error) {
	a, _, err := rp.w.BeginLogin(u, webauthn.WithChallenge(challenge), webauthn.WithUserVerification(protocol.VerificationRequired))
	return a, err
}

// Asserted is the passkey that answered an assertion, with what the answer carried.
type Asserted struct {
	Passkey     storage.Passkey
	SignCount   uint32
	BackupState bool
}

// Verify checks the browser's answer to an assertion challenge for u: one of u's live passkeys signed exactly that
// challenge, for this origin and relying party, with the person verified; its counter did not go back.
func (rp *RelyingParty) Verify(u User, challenge, response []byte) (Asserted, error) {
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return Asserted{}, errors.Join(ErrRefused, err)
	}
	c, err := rp.w.ValidateLogin(u, rp.session(u, challenge), parsed)
	if err != nil {
		return Asserted{}, errors.Join(ErrRefused, err)
	}
	if c.Authenticator.CloneWarning {
		return Asserted{}, errors.Join(ErrRefused, errors.New("the signature counter went back: the authenticator may be cloned"))
	}
	for _, k := range u.Passkeys {
		if k.RevokedAt == nil && string(k.CredentialID) == string(c.ID) {
			return Asserted{Passkey: k, SignCount: c.Authenticator.SignCount, BackupState: c.Flags.BackupState}, nil
		}
	}
	return Asserted{}, errors.Join(ErrRefused, errors.New("no live passkey of the user answered"))
}
