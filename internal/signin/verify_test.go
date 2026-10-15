package signin_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/signin"
)

var (
	now    = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	google = signin.Provider{ID: "google", Name: "Google", Issuer: "https://accounts.google.com", ClientID: "fyniq-google"}
	zoho   = signin.Provider{ID: "zoho", Name: "Zoho", Issuer: "https://accounts.zoho.com", ClientID: "fyniq-zoho"}
)

type keyPair struct{ key *ecdsa.PrivateKey }

func newKey(t *testing.T) keyPair {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return keyPair{k}
}

func (k keyPair) set() oidc.KeySet {
	return &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&k.key.PublicKey}}
}

// sign returns an ID token with claims (alg ES256 unless overridden by signing with another algorithm).
func (k keyPair) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: k.key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(claims)
	jws, err := s.Sign(b)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := jws.CompactSerialize()
	return out
}

// claims are a valid ID token from p for the nonce "n-1".
func claims(p signin.Provider) map[string]any {
	return map[string]any{"iss": p.Issuer, "sub": "110169484474386276334", "aud": p.ClientID, "exp": now.Add(5 * time.Minute).Unix(),
		"iat": now.Add(-time.Minute).Unix(), "auth_time": now.Add(-time.Minute).Unix(), "nonce": "n-1", "name": "Mei Ling Tan",
		"email": "mei@example.test", "email_verified": true, "amr": []string{"pwd"}}
}

func with(c map[string]any, k string, v any) map[string]any {
	out := maps.Clone(c)
	if v == nil {
		delete(out, k)
	} else {
		out[k] = v
	}
	return out
}

// The configured issuer per provider is stored, and an ID token's iss must equal it exactly: a token
// from another provider's issuer, or one that differs by a slash, a scheme or a case, is refused, as is
// one for another audience, expired, from another sign-in, or signed by another key (FR-ADM-08).
// req: FR-ADM-08
// asvs: V10.5.1, V10.5.3, V10.5.4
func TestAnIDTokenFromAnotherIssuerIsRefused(t *testing.T) {
	ctx := context.Background()
	k := newKey(t)
	who, err := signin.Verify(ctx, google, k.set(), k.sign(t, claims(google)), "n-1", now)
	if err != nil {
		t.Fatalf("a valid token was refused: %v", err)
	}
	if who.Issuer != google.Issuer || who.Subject != "110169484474386276334" || who.Provider != "google" || !who.EmailVerified || who.SecondFactor ||
		!who.AuthTime.Equal(now.Add(-time.Minute)) {
		t.Fatalf("Verify = %+v", who)
	}

	other := newKey(t)
	for name, tc := range map[string]struct {
		p     signin.Provider
		token string
		nonce string
	}{
		"a Zoho token offered as Google's":         {google, k.sign(t, claims(zoho)), "n-1"},
		"Google's token offered as Zoho's":         {zoho, k.sign(t, claims(google)), "n-1"},
		"the issuer with a trailing slash":         {google, k.sign(t, with(claims(google), "iss", "https://accounts.google.com/")), "n-1"},
		"the issuer without its scheme":            {google, k.sign(t, with(claims(google), "iss", "accounts.google.com")), "n-1"},
		"the issuer in another case":               {google, k.sign(t, with(claims(google), "iss", "https://Accounts.Google.com")), "n-1"},
		"another Zoho data centre":                 {zoho, k.sign(t, with(claims(zoho), "iss", "https://accounts.zoho.eu")), "n-1"},
		"no issuer":                                {google, k.sign(t, with(claims(google), "iss", nil)), "n-1"},
		"another audience":                         {google, k.sign(t, with(claims(google), "aud", "someone-else")), "n-1"},
		"several audiences, another party":         {google, k.sign(t, with(with(claims(google), "aud", []string{"fyniq-google", "x"}), "azp", "x")), "n-1"},
		"expired":                                  {google, k.sign(t, with(claims(google), "exp", now.Add(-time.Second).Unix())), "n-1"},
		"issued in the future":                     {google, k.sign(t, with(claims(google), "iat", now.Add(time.Hour).Unix())), "n-1"},
		"authenticated in the future":              {google, k.sign(t, with(claims(google), "auth_time", now.Add(time.Hour).Unix())), "n-1"},
		"another sign-in's nonce":                  {google, k.sign(t, claims(google)), "n-2"},
		"no nonce in the token":                    {google, k.sign(t, with(claims(google), "nonce", nil)), "n-1"},
		"no subject":                               {google, k.sign(t, with(claims(google), "sub", nil)), "n-1"},
		"signed by another key":                    {google, other.sign(t, claims(google)), "n-1"},
		"not a token":                              {google, "not.a.token", "n-1"},
		"a provider without an issuer configured":  {signin.Provider{ID: "x", ClientID: "c"}, k.sign(t, with(claims(google), "aud", "c")), "n-1"},
		"a provider without a client configured":   {signin.Provider{ID: "x", Issuer: google.Issuer}, k.sign(t, claims(google)), "n-1"},
		"a sign-in without a nonce":                {google, k.sign(t, with(claims(google), "nonce", "")), ""},
		"unsigned (alg none), with a valid header": {google, unsigned(t, claims(google)), "n-1"},
	} {
		if _, err := signin.Verify(ctx, tc.p, k.set(), tc.token, tc.nonce, now); !errors.Is(err, ports.ErrSignInRefused) {
			t.Errorf("%s: not refused (%v)", name, err)
		}
	}
}

// unsigned is a token with "alg": "none" and an empty signature.
func unsigned(t *testing.T, c map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	b, _ := json.Marshal(c)
	enc := base64.RawURLEncoding.EncodeToString
	return enc(h) + "." + enc(b) + "."
}

// Only a sign-in that passed a second factor is a step-up (FR-ADM-07, SEC-01): the token says so with
// "mfa" among its authentication methods (RFC 8176). Some providers send email_verified as a string.
// req: FR-ADM-07
func TestASecondFactorIsReadFromTheToken(t *testing.T) {
	k := newKey(t)
	c := with(with(claims(zoho), "amr", []string{"pwd", "otp", "mfa"}), "email_verified", "true")
	who, err := signin.Verify(context.Background(), zoho, k.set(), k.sign(t, c), "n-1", now)
	if err != nil || !who.SecondFactor || !who.EmailVerified {
		t.Fatalf("Verify = %+v, %v", who, err)
	}
	c = with(with(claims(zoho), "amr", []string{"otp"}), "email_verified", "false")
	if who, err := signin.Verify(context.Background(), zoho, k.set(), k.sign(t, c), "n-1", now); err != nil || who.SecondFactor || who.EmailVerified {
		t.Fatalf("one-time code alone, unverified email: %+v, %v", who, err)
	}
}
