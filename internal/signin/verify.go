// Package signin is the web app's way in and out (SEC-05, SEC-08, FR-ADM-08): the checks every
// provider's ID token passes (Verify), and the browser flow (Flow) that sends a person to a provider
// through the SignIn port and turns who came back into a first-party session.
package signin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Provider is a configured sign-in provider: what the sign-in page offers, and what its ID tokens must
// carry (FR-ADM-08).
type Provider struct {
	ID       string // e.g. google, zoho
	Name     string // as the sign-in page names it
	Issuer   string // the ID token's iss must equal it exactly
	ClientID string // the ID token's aud must include it
}

// algorithms are the signatures an ID token may carry: RSA and ECDSA with SHA-256. Never "none" or an
// HMAC, which a public key cannot check.
var algorithms = []string{oidc.RS256, oidc.ES256}

// skew is how far a provider's clock may run ahead of ours for the issue and authentication times.
const skew = time.Minute

// Verify checks an ID token from p and returns who signed in. The token must be signed by one of keys
// with an accepted algorithm; its issuer must be exactly p.Issuer, with no exception for any provider
// (FR-ADM-08); its audience must include p.ClientID, and with several audiences its authorised party
// must be p.ClientID (OpenID Connect Core §3.1.3.7); it must be unexpired at now, not issued or
// authenticated in the future, and carry nonce. Every failure is ports.ErrSignInRefused with the reason;
// the reason never quotes the token.
func Verify(ctx context.Context, p Provider, keys oidc.KeySet, raw, nonce string, now time.Time) (ports.SignedIn, error) {
	refused := func(format string, a ...any) (ports.SignedIn, error) {
		return ports.SignedIn{}, errors.Join(ports.ErrSignInRefused, fmt.Errorf("%s: "+format, append([]any{p.ID}, a...)...))
	}
	if p.Issuer == "" || p.ClientID == "" || nonce == "" {
		return refused("the provider has no issuer or client ID configured, or the sign-in no nonce")
	}
	v := oidc.NewVerifier(p.Issuer, keys, &oidc.Config{ClientID: p.ClientID, SupportedSigningAlgs: algorithms,
		Now: func() time.Time { return now }})
	t, err := v.Verify(ctx, raw)
	if err != nil {
		return refused("the ID token does not verify: %v", err)
	}
	// go-oidc lets a Google token say "accounts.google.com" for "https://accounts.google.com"; FR-ADM-08
	// wants the configured issuer exactly, so this check is ours.
	if t.Issuer != p.Issuer {
		return refused("the ID token was issued by %q, not the configured %q", t.Issuer, p.Issuer)
	}
	var c struct {
		AZP           string   `json:"azp"`
		AuthTime      float64  `json:"auth_time"`
		AMR           []string `json:"amr"`
		Name          string   `json:"name"`
		Email         string   `json:"email"`
		EmailVerified any      `json:"email_verified"` // a boolean; a string "true" from some providers
	}
	if err := t.Claims(&c); err != nil {
		return refused("the ID token's claims cannot be read: %v", err)
	}
	switch {
	case t.Subject == "":
		return refused("the ID token has no subject")
	case t.Nonce != nonce:
		return refused("the ID token belongs to another sign-in (nonce)")
	case len(t.Audience) > 1 && c.AZP != p.ClientID:
		return refused("the ID token has several audiences and another authorised party")
	case t.IssuedAt.After(now.Add(skew)):
		return refused("the ID token was issued in the future")
	}
	authTime := t.IssuedAt
	if c.AuthTime > 0 {
		authTime = time.Unix(int64(c.AuthTime), 0).UTC()
		if authTime.After(now.Add(skew)) {
			return refused("the ID token says the person authenticated in the future")
		}
	}
	verified := c.EmailVerified == true || c.EmailVerified == "true"
	return ports.SignedIn{Provider: p.ID, Issuer: t.Issuer, Subject: t.Subject, Name: c.Name, Email: c.Email, EmailVerified: verified,
		AuthTime: authTime, SecondFactor: slices.Contains(c.AMR, "mfa")}, nil
}
