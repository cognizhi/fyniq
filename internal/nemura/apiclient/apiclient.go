// Package apiclient is Nemura's own API clients (PRD-002 NM-ARC-06, question 11): systems that call Nemura's API on their own with the client-credentials
// grant, signing in with a key pair they hold (private_key_jwt, RFC 7523), for the API's audience only and the scopes they were registered with. The OAuth
// server is Nemura's own instance of the platform's authorisation server, so standalone needs nothing from Fyniq. Only an Owner registers one, with a
// passkey confirmation fresh within five minutes; the second approver NM-ARC-04 asks of the Fyniq link comes with that link.
package apiclient

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Audience is the API's audience: the only one a client's tokens may name.
const Audience = "nemura-api"

// Scopes are what a client may be given. A scope joins the catalogue with the operation that needs it; the unified HR API's
// (NM-EXT-04, P2) and the link's arrive then.
var Scopes = []string{"directory.read"}

// LinkScope is the one scope Fyniq's link client holds at Nemura's own OAuth server: it reaches the link's event inbox and nothing else. It is not in Scopes, so no client
// registered through the Owner's registration (or its second-approver request) can be given it, and the link's client cannot be given another: the link registers that client itself,
// with this scope alone (p1-nemura-fyniq-link design D1).
const LinkScope = "link.events.write"

// AskScope is the one scope Fyniq's second link client holds: it reaches the read of a hold's status and nothing that writes. Fyniq asks with it at the release of a payment (PRD-001 FR-PPL-13); the client that
// sends events cannot ask, and the one that asks cannot send. Like LinkScope it is outside Scopes.
const AskScope = "link.holds.read"

// SyncScope is the second scope Fyniq's read-only link client holds (the owner's decision of 21 Nov 2026): it reaches the read of the link's event log by identity, for Fyniq's gap detection and health, and
// nothing else. The log names each event's sequence, type, object and state and never a payload, a figure, a name or a bank detail. Like AskScope it is outside Scopes, and the client that sends events holds neither.
const SyncScope = "link.sync.read"

// TokenLifespan is how long each access token lives (AI-38: at most 60 minutes).
const TokenLifespan = time.Hour

// Algorithms are the signing algorithms a client's assertions may use.
var Algorithms = []string{"ES256", "RS256", "EdDSA"}

// Refusal is a refusal with a stable code and a plain fix.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// The codes.
const (
	CodeInvalid = "NM_API_CLIENT_INVALID"
	CodeExists  = "NM_API_CLIENT_EXISTS"
)

// Check validates a registration: a name, scopes from the catalogue, a signing algorithm and a public key as a JWK that carries no private part.
func Check(name string, scopes []string, alg string, key json.RawMessage) error {
	if n := len(strings.TrimSpace(name)); n < 1 || n > 100 {
		return &Refusal{CodeInvalid, "The name is required, at most 100 characters. Name the system that will call."}
	}
	if len(scopes) == 0 {
		return &Refusal{CodeInvalid, "Give the client at least one scope. Scopes: " + strings.Join(Scopes, ", ") + "."}
	}
	for _, s := range scopes {
		if !slices.Contains(Scopes, s) {
			return &Refusal{CodeInvalid, fmt.Sprintf("The scope %q does not exist. Scopes: %s.", s, strings.Join(Scopes, ", "))}
		}
	}
	if !slices.Contains(Algorithms, alg) {
		return &Refusal{CodeInvalid, fmt.Sprintf("The algorithm %q is not accepted. Use one of %s.", alg, strings.Join(Algorithms, ", "))}
	}
	return CheckKey(key)
}

// CheckKey validates a public key given as a JWK: it is one, and carries no private part.
func CheckKey(key json.RawMessage) error {
	var jwk map[string]any
	if err := json.Unmarshal(key, &jwk); err != nil || jwk["kty"] == nil {
		return &Refusal{CodeInvalid, "The key must be a public key as a JWK (RFC 7517). Send the public part of the key pair the system holds."}
	}
	for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "k"} {
		if _, ok := jwk[private]; ok {
			return &Refusal{CodeInvalid, "The key carries a private part. Send only the public key: the private key never leaves the system that holds it."}
		}
	}
	return nil
}
