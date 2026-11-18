// Package apiclient checks what an organisation's API clients are asked to be (PRD-001 FR-INT-02, SEC-07, AI-34;
// p1-public-api task 3.1): an integration client's name, narrow scopes, networks, own rate limits, expiry and the public
// key its private_key_jwt assertions are signed with. Every field is checked and every refusal returned at once, each
// naming the field and the fix (ENG-23, FR-INT-07).
package apiclient

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The refusals of an API client's definition and steps.
const (
	CodeName     = "API-CLIENT-01" // the name is empty, too long or has spaces at either end
	CodeScope    = "API-CLIENT-02" // a scope is not one an API client may be granted, or none is given
	CodeNetwork  = "API-CLIENT-03" // a network is not a CIDR prefix, or too many are given
	CodeLimit    = "API-CLIENT-04" // a rate limit is above the platform's or below one
	CodeExpiry   = "API-CLIENT-05" // the expiry is not in the future, or too far
	CodeKey      = "API-CLIENT-06" // the public key is not an EC P-256 or RSA (2048 bits or more) public key
	CodeOwner    = "API-CLIENT-07" // the member behind it is not an active member of the organisation
	CodeState    = "API-CLIENT-08" // the client is revoked already
	CodeDisabled = "API-CLIENT-09" // personal access tokens are off for the organisation (security.personal_access_tokens)
)

// The limits of an integration client (SEC-07, FR-INT-16).
const (
	MaxIntegrationLife = 366 * 24 * time.Hour // an integration client expires within a year
	MaxScopes          = 16
	MaxNetworks        = 32
	MaxReadsPerMinute  = 600 // the platform's defaults: a client's own limits only lower them
	MaxWritesPerMinute = 120
)

// Integration is what a person asks an integration client to be.
type Integration struct {
	Name            string
	Scopes          []string
	Networks        []string
	ReadsPerMinute  int // 0: the platform's
	WritesPerMinute int
	ExpiresAt       time.Time
	Key             json.RawMessage // the public JWK
}

// Error is one refusal, naming the field.
type Error struct {
	Code, Field, Message, Fix string
}

func (e *Error) Error() string { return e.Field + " " + e.Message }

// Errors are every refusal of a definition, at once.
type Errors []*Error

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, x := range e {
		parts[i] = x.Error()
	}
	return strings.Join(parts, "; ")
}

// Checked is a definition as it will be registered: the networks as canonical prefixes, the scopes sorted, the key's
// algorithm and RFC 7638 thumbprint.
type Checked struct {
	Integration
	Alg, Thumbprint string
}

// definition is the part of an API client's definition that integration clients and personal access tokens share: the
// name, scopes, networks, own limits and expiry, which must fall within life of now. It adds every refusal to errs and
// returns the scopes sorted and the networks as canonical prefixes.
func definition(errs *Errors, name string, scopes, networks []string, reads, writes int, expires, now time.Time, life time.Duration, within string) ([]string, []string) {
	add := func(code, field, message, fix string) {
		*errs = append(*errs, &Error{Code: code, Field: field, Message: message, Fix: fix})
	}
	if n := len([]rune(name)); n == 0 || n > 80 || name != strings.TrimSpace(name) {
		add(CodeName, "name", "is empty, longer than 80 characters or has spaces at either end", "Give the client a name of 1 to 80 characters")
	}
	var outScopes []string
	for i, s := range scopes {
		switch {
		case !authz.ClientScope(s):
			add(CodeScope, fmt.Sprintf("scopes[%d]", i), "is not a scope an API client may be granted",
				"Choose fyniq.read.<area> or fyniq.write.<area> from "+strings.Join(authz.ClientScopes(), ", "))
		case slices.Contains(outScopes, s):
			add(CodeScope, fmt.Sprintf("scopes[%d]", i), "is given twice", "Give each scope once")
		default:
			outScopes = append(outScopes, s)
		}
	}
	if len(scopes) == 0 || len(scopes) > MaxScopes {
		add(CodeScope, "scopes", fmt.Sprintf("has %d scopes", len(scopes)), fmt.Sprintf("Grant 1 to %d scopes, only what the integration needs", MaxScopes))
	}
	slices.Sort(outScopes)
	outNetworks := []string{}
	for i, n := range networks {
		p, err := netip.ParsePrefix(strings.TrimSpace(n))
		if err != nil {
			add(CodeNetwork, fmt.Sprintf("networks[%d]", i), "is not a network in CIDR form", "Write it as 203.0.113.0/24 or 2001:db8::/32")
			continue
		}
		outNetworks = append(outNetworks, p.Masked().String())
	}
	if len(networks) > MaxNetworks {
		add(CodeNetwork, "networks", fmt.Sprintf("has %d networks", len(networks)), fmt.Sprintf("Give at most %d networks", MaxNetworks))
	}
	if reads < 0 || reads > MaxReadsPerMinute {
		add(CodeLimit, "reads_per_minute", "is outside 1 to the platform's 600", "Give 1 to 600, or leave it out for the platform's")
	}
	if writes < 0 || writes > MaxWritesPerMinute {
		add(CodeLimit, "writes_per_minute", "is outside 1 to the platform's 120", "Give 1 to 120, or leave it out for the platform's")
	}
	switch {
	case !expires.After(now):
		add(CodeExpiry, "expires_at", "is not in the future", "Give a time within "+within)
	case expires.Sub(now) > life-time.Minute: // the database checks the life from when it is registered
		add(CodeExpiry, "expires_at", "is more than "+within+" away", "Give a time within "+within+"; register a new one before it expires")
	}
	return outScopes, outNetworks
}

// CheckIntegration checks d at now and returns it as it will be registered, or every refusal at once.
func CheckIntegration(d Integration, now time.Time) (Checked, error) {
	var errs Errors
	out := Checked{Integration: d}
	out.Scopes, out.Networks = definition(&errs, d.Name, d.Scopes, d.Networks, d.ReadsPerMinute, d.WritesPerMinute, d.ExpiresAt, now, MaxIntegrationLife, "the next year")
	alg, thumb, err := publicKey(d.Key)
	if err != nil {
		errs = append(errs, &Error{Code: CodeKey, Field: "public_key", Message: err.Error(),
			Fix: "Give the public half of an EC P-256 key (ES256) or an RSA key of 2048 bits or more (RS256), as a JWK"})
	}
	out.Alg, out.Thumbprint = alg, thumb
	if len(errs) > 0 {
		return Checked{}, errs
	}
	return out, nil
}

// Personal is what a developer asks a personal access token to be (FR-INT-02): it carries no key of its own, since the
// OAuth server issues its secret; it expires within 90 days.
type Personal struct {
	Name            string
	Scopes          []string
	Networks        []string
	ReadsPerMinute  int // 0: the platform's
	WritesPerMinute int
	ExpiresAt       time.Time
}

// MaxPersonalLife is the longest a personal access token lives (FR-INT-02).
const MaxPersonalLife = 90 * 24 * time.Hour

// CheckPersonal checks d at now and returns it as it will be registered, or every refusal at once.
func CheckPersonal(d Personal, now time.Time) (Personal, error) {
	var errs Errors
	out := d
	out.Scopes, out.Networks = definition(&errs, d.Name, d.Scopes, d.Networks, d.ReadsPerMinute, d.WritesPerMinute, d.ExpiresAt, now, MaxPersonalLife, "the next 90 days")
	if len(errs) > 0 {
		return Personal{}, errs
	}
	return out, nil
}

// publicKey reads a public JWK fit to sign private_key_jwt assertions, and returns its algorithm and RFC 7638 thumbprint.
func publicKey(raw json.RawMessage) (alg, thumbprint string, err error) {
	if len(raw) == 0 {
		return "", "", fmt.Errorf("is missing")
	}
	var k jose.JSONWebKey
	if err := k.UnmarshalJSON(raw); err != nil {
		return "", "", fmt.Errorf("is not a JWK")
	}
	if !k.IsPublic() || !k.Valid() {
		return "", "", fmt.Errorf("is not a valid public key: never send the private key")
	}
	if k.Use != "" && k.Use != "sig" {
		return "", "", fmt.Errorf("is for %q, not for signing", k.Use)
	}
	switch pk := k.Key.(type) {
	case *ecdsa.PublicKey:
		if pk.Curve != elliptic.P256() {
			return "", "", fmt.Errorf("is an EC key on a curve other than P-256")
		}
		alg = string(jose.ES256)
	case *rsa.PublicKey:
		if pk.N.BitLen() < 2048 {
			return "", "", fmt.Errorf("is an RSA key shorter than 2048 bits")
		}
		alg = string(jose.RS256)
	default:
		return "", "", fmt.Errorf("is neither an EC P-256 nor an RSA key")
	}
	if k.Algorithm != "" && k.Algorithm != alg {
		return "", "", fmt.Errorf("names the algorithm %s; its key signs with %s", k.Algorithm, alg)
	}
	sum, err := k.Thumbprint(crypto.SHA256)
	if err != nil {
		return "", "", fmt.Errorf("has no thumbprint")
	}
	return alg, base64.RawURLEncoding.EncodeToString(sum), nil
}

// PublicJWK is the key as the OAuth server registers it: the public half only, with its algorithm and use.
func PublicJWK(raw json.RawMessage, alg string) (json.RawMessage, error) {
	var k jose.JSONWebKey
	if err := k.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	pub := k.Public()
	pub.Algorithm, pub.Use = alg, "sig"
	return pub.MarshalJSON()
}
