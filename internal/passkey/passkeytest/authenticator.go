// Package passkeytest is a software authenticator for the tests: it makes a P-256 passkey and answers registration
// and assertion challenges as a browser and a platform authenticator would (W3C WebAuthn Level 3: clientDataJSON,
// authenticator data, a "none" attestation object, an ES256 signature over authenticator data ‖ SHA-256(clientDataJSON)).
// Each field the relying party checks can be set wrong, so every refusal has its test.
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// The authenticator data flags (WebAuthn §6.1).
const (
	FlagUP byte = 0x01 // user present
	FlagUV byte = 0x04 // user verified
	FlagBE byte = 0x08 // backup eligible
	FlagBS byte = 0x10 // backed up
	FlagAT byte = 0x40 // attested credential data included
)

// Authenticator holds one passkey.
type Authenticator struct {
	t            testing.TB
	Key          *ecdsa.PrivateKey
	CredentialID []byte
	AAGUID       []byte
	// RPID and Origin are what it signs for; a test sets either wrong to see the refusal.
	RPID, Origin string
	// Count is the signature counter; each assertion adds one first, unless a test set it.
	Count uint32
	// Flags are the authenticator data flags of every answer: UP|UV by default.
	Flags byte
	// UserHandle is returned with assertions.
	UserHandle []byte
}

// New makes a passkey for rpID, used from origin.
func New(t testing.TB, rpID, origin string) *Authenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	aaguid := make([]byte, 16)
	_, _ = rand.Read(id)
	_, _ = rand.Read(aaguid)
	return &Authenticator{t: t, Key: key, CredentialID: id, AAGUID: aaguid, RPID: rpID, Origin: origin, Flags: FlagUP | FlagUV}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// Challenge reads the challenge of a creation or request options document, as the browser does.
func Challenge(t testing.TB, options map[string]any) []byte {
	t.Helper()
	pk, _ := options["publicKey"].(map[string]any)
	s, _ := pk["challenge"].(string)
	c, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(c) == 0 {
		t.Fatalf("no challenge in %v", options)
	}
	return c
}

func (a *Authenticator) clientData(kind string, challenge []byte) []byte {
	b, err := json.Marshal(map[string]any{"type": kind, "challenge": b64(challenge), "origin": a.Origin, "crossOrigin": false})
	if err != nil {
		a.t.Fatal(err)
	}
	return b
}

func (a *Authenticator) authData(flags byte, count uint32, attested []byte) []byte {
	rp := sha256.Sum256([]byte(a.RPID))
	out := append(rp[:], flags)
	out = binary.BigEndian.AppendUint32(out, count)
	return append(out, attested...)
}

// coseKey is the passkey's public key as a COSE_Key (RFC 9053): EC2, ES256, P-256.
func (a *Authenticator) coseKey() []byte {
	point, err := a.Key.PublicKey.Bytes() // 0x04 ‖ x ‖ y
	if err != nil {
		a.t.Fatal(err)
	}
	b, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: point[1:33], -3: point[33:65]})
	if err != nil {
		a.t.Fatal(err)
	}
	return b
}

// Create answers a registration challenge: the JSON of the PublicKeyCredential the browser returns.
func (a *Authenticator) Create(challenge []byte) []byte {
	n := len(a.CredentialID)
	if n < 0 || n > math.MaxUint16 {
		a.t.Fatalf("a credential ID of %d bytes", n)
		return nil
	}
	attested := append([]byte{}, a.AAGUID...)
	attested = binary.BigEndian.AppendUint16(attested, uint16(n))
	attested = append(attested, a.CredentialID...)
	attested = append(attested, a.coseKey()...)
	obj, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(a.Flags|FlagAT, a.Count, attested)})
	if err != nil {
		a.t.Fatal(err)
	}
	return a.marshal(map[string]any{"clientDataJSON": b64(a.clientData("webauthn.create", challenge)), "attestationObject": b64(obj),
		"transports": []string{"internal", "hybrid"}})
}

// Get answers an assertion challenge with the next counter: the JSON of the PublicKeyCredential the browser returns.
func (a *Authenticator) Get(challenge []byte) []byte {
	a.Count++
	return a.Sign(challenge, a.Count)
}

// Sign answers an assertion challenge with the counter given.
func (a *Authenticator) Sign(challenge []byte, count uint32) []byte {
	cd := a.clientData("webauthn.get", challenge)
	ad := a.authData(a.Flags, count, nil)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.Key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	r := map[string]any{"clientDataJSON": b64(cd), "authenticatorData": b64(ad), "signature": b64(sig)}
	if len(a.UserHandle) > 0 {
		r["userHandle"] = b64(a.UserHandle)
	}
	return a.marshal(r)
}

func (a *Authenticator) marshal(response map[string]any) []byte {
	b, err := json.Marshal(map[string]any{"id": b64(a.CredentialID), "rawId": b64(a.CredentialID), "type": "public-key",
		"response": response, "clientExtensionResults": map[string]any{}, "authenticatorAttachment": "platform"})
	if err != nil {
		a.t.Fatal(err)
	}
	return b
}
