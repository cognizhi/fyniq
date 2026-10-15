package gcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// fakeToolkit is Identity Platform's REST API as this adapter uses it, with Google's certificate map:
// it checks the API key header and binds signInWithIdp to createAuthUri's session ID, as the service
// does.
type fakeToolkit struct {
	t       *testing.T
	key     *rsa.PrivateKey
	project string
	now     time.Time

	mu        sync.Mutex
	sessionID string
	result    map[string]any // signInWithIdp's answer; tokens filled in by the test
	status    int            // signInWithIdp's status, 200 by default
	certHits  int
	urlKeys   []string // any API key that reached a URL
}

func newFake(t *testing.T, now time.Time) (*fakeToolkit, *httptest.Server) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeToolkit{t: t, key: k, project: "fyniq-staging", now: now, status: http.StatusOK}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeToolkit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Query().Get("key") != "" {
		f.urlKeys = append(f.urlKeys, r.URL.Query().Get("key"))
	}
	if r.URL.Path == "/certs" {
		f.certHits++
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "securetoken"},
			NotBefore: f.now.Add(-time.Hour), NotAfter: f.now.Add(24 * time.Hour)}
		der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &f.key.PublicKey, f.key)
		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		_ = json.NewEncoder(w).Encode(map[string]string{"k1": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))})
		return
	}
	if r.Header.Get("X-Goog-Api-Key") != "test-api-key" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch r.URL.Path {
	case "/v1/accounts:createAuthUri":
		f.sessionID, _ = body["sessionId"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"authUri": "https://accounts.zoho.com/oauth/v2/auth?state=idp-state&redirect_uri=" +
			url.QueryEscape(body["continueUri"].(string)), "sessionId": f.sessionID, "providerId": body["providerId"]})
	case "/v1/accounts:signInWithIdp":
		if body["sessionId"] != f.sessionID || !strings.Contains(body["requestUri"].(string), "code=") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"INVALID_IDP_RESPONSE : the session ID does not match"}}`))
			return
		}
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(f.result)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// platformToken is Identity Platform's ID token, signed with kid k1.
func (f *fakeToolkit) platformToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(claims)
	jws, _ := s.Sign(b)
	out, _ := jws.CompactSerialize()
	return out
}

func (f *fakeToolkit) platformClaims() map[string]any {
	return map[string]any{"iss": "https://securetoken.google.com/" + f.project, "aud": f.project, "sub": "ip-uid-1",
		"iat": f.now.Add(-time.Minute).Unix(), "exp": f.now.Add(time.Hour).Unix(), "auth_time": f.now.Add(-time.Minute).Unix(),
		"email": "mei@example.test", "email_verified": true, "firebase": map[string]any{"sign_in_provider": "oidc.zoho"}}
}

// upstreamToken is the provider's ID token as Identity Platform passes it on (its signature is
// Identity Platform's to check, so this one carries none).
func upstreamToken(claims map[string]any) string {
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	return enc(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." + enc(claims) + ".sig"
}

var zohoUS = IdentityProvider{ID: "zoho", Name: "Zoho", IdP: "oidc.zoho", Issuer: "https://accounts.zoho.com", ClientID: "1000.FYNIQ"}

func newPlatform(f *fakeToolkit, srv *httptest.Server, c ports.Clock) *IdentityPlatform {
	return &IdentityPlatform{Endpoint: srv.URL, APIKey: "test-api-key", Project: f.project, Federated: []IdentityProvider{zohoUS},
		Keys: &CertKeys{URL: srv.URL + "/certs", Clock: c}, Clock: c}
}

func (f *fakeToolkit) answer(t *testing.T, platform, upstream map[string]any, extra map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result = map[string]any{"providerId": "oidc.zoho", "localId": "ip-uid-1", "federatedId": "zoho-sub-1",
		"idToken": f.platformToken(t, platform), "oauthIdToken": upstreamToken(upstream), "email": "mei@example.test", "emailVerified": true}
	for k, v := range extra {
		if v == nil {
			delete(f.result, k)
		} else {
			f.result[k] = v
		}
	}
}

func zohoClaims() map[string]any {
	return map[string]any{"iss": "https://accounts.zoho.com", "aud": "1000.FYNIQ", "sub": "zoho-sub-1"}
}

func with(m map[string]any, k string, v any) map[string]any {
	out := map[string]any{}
	for x, y := range m {
		out[x] = y
	}
	out[k] = v
	return out
}

var secrets = ports.SignInSecrets{State: "state-1", Nonce: "session-secret-1", CodeVerifier: strings.Repeat("v", 43)}

const callback = "https://fyniq.example.test/api/auth/callback"

func back(t *testing.T) *url.URL {
	u, err := url.Parse(callback + "?code=zoho-code&state=idp-state")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// A Zoho sign-in through Identity Platform: the server starts it with the browser's session ID, which
// never appears in a URL, and the identity is the provider's (issuer, subject) after both tokens pass
// their checks. A Zoho token from another data centre, another client, a platform token for another
// project or signed by an unknown key, and an answer for another provider are refused (FR-ADM-08).
// req: FR-ADM-08
// req: SEC-08
// asvs: V10.5.3, V10.5.4, V10.2.1
func TestIdentityPlatformSignIn(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	f, srv := newFake(t, now)
	c := clock.NewFrozen(now)
	p := newPlatform(f, srv, c)

	to, err := p.Start(ctx, "zoho", callback, secrets)
	if err != nil || !strings.HasPrefix(to, "https://accounts.zoho.com/") || strings.Contains(to, secrets.Nonce) {
		t.Fatalf("Start = %q, %v", to, err)
	}
	f.answer(t, f.platformClaims(), zohoClaims(), nil)
	who, err := p.Finish(ctx, "zoho", back(t), secrets)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if who.Provider != "zoho" || who.Issuer != "https://accounts.zoho.com" || who.Subject != "zoho-sub-1" || who.Email != "mei@example.test" ||
		!who.EmailVerified || who.SecondFactor || !who.AuthTime.Equal(now.Add(-time.Minute)) {
		t.Fatalf("signed in as %+v", who)
	}

	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	forged := func() string {
		s, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: other}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		b, _ := json.Marshal(f.platformClaims())
		jws, _ := s.Sign(b)
		out, _ := jws.CompactSerialize()
		return out
	}()
	for name, tc := range map[string]struct {
		platform, upstream, extra map[string]any
	}{
		"a Zoho token from the EU data centre":   {f.platformClaims(), with(zohoClaims(), "iss", "https://accounts.zoho.eu"), nil},
		"a Zoho token with a trailing slash":     {f.platformClaims(), with(zohoClaims(), "iss", "https://accounts.zoho.com/"), nil},
		"a Zoho token for another client":        {f.platformClaims(), with(zohoClaims(), "aud", "1000.SOMEONE"), nil},
		"a Zoho token without a subject":         {f.platformClaims(), with(zohoClaims(), "sub", ""), nil},
		"a platform token for another project":   {with(f.platformClaims(), "aud", "someone-else"), zohoClaims(), nil},
		"a platform token from another issuer":   {with(f.platformClaims(), "iss", "https://securetoken.google.com/someone-else"), zohoClaims(), nil},
		"an expired platform token":              {with(f.platformClaims(), "exp", now.Add(-time.Second).Unix()), zohoClaims(), nil},
		"a platform token signed by another key": {f.platformClaims(), zohoClaims(), map[string]any{"idToken": forged}},
		"an answer for Google":                   {f.platformClaims(), zohoClaims(), map[string]any{"providerId": "google.com"}},
		"no provider token to check":             {f.platformClaims(), zohoClaims(), map[string]any{"oauthIdToken": nil}},
		"a second factor pending":                {f.platformClaims(), zohoClaims(), map[string]any{"mfaPendingCredential": "pending"}},
		"an account link needed":                 {f.platformClaims(), zohoClaims(), map[string]any{"needConfirmation": true}},
	} {
		f.answer(t, tc.platform, tc.upstream, tc.extra)
		if _, err := p.Finish(ctx, "zoho", back(t), secrets); !errors.Is(err, ports.ErrSignInRefused) {
			t.Errorf("%s: not refused (%v)", name, err)
		}
	}

	f.answer(t, f.platformClaims(), zohoClaims(), nil)
	stolen := secrets
	stolen.Nonce = "another-browser"
	if _, err := p.Finish(ctx, "zoho", back(t), stolen); !errors.Is(err, ports.ErrSignInRefused) {
		t.Errorf("a return to another browser's sign-in: %v", err)
	}
	f.status = http.StatusServiceUnavailable
	_, err = p.Finish(ctx, "zoho", back(t), secrets)
	if err == nil || errors.Is(err, ports.ErrSignInRefused) || strings.Contains(err.Error(), "test-api-key") {
		t.Errorf("an outage is a failure, not a refusal, and names no key: %v", err)
	}
	if len(f.urlKeys) != 0 {
		t.Errorf("the API key reached a URL: %v", f.urlKeys)
	}
	if _, err := p.Start(ctx, "github", callback, secrets); !errors.Is(err, ports.ErrUnknownProvider) {
		t.Errorf("an unknown provider: %v", err)
	}
}

// The signing keys are cached for the max-age Google sends, and an unknown key ID fetches them again
// at most once a minute, so forged tokens cannot make Fyniq hammer the key endpoint.
// req: FR-ADM-08
func TestCertKeysCacheByMaxAge(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	f, srv := newFake(t, now)
	c := clock.NewFrozen(now)
	keys := &CertKeys{URL: srv.URL + "/certs", Clock: c}
	good := f.platformToken(t, f.platformClaims())
	for range 3 {
		if _, err := keys.VerifySignature(ctx, good); err != nil {
			t.Fatal(err)
		}
	}
	if f.certHits != 1 {
		t.Errorf("fetched %d times within max-age; want 1", f.certHits)
	}
	s, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithHeader("kid", "k9"))
	jws, _ := s.Sign([]byte(`{}`))
	unknown, _ := jws.CompactSerialize()
	c.Advance(2 * time.Minute)
	for range 5 {
		if _, err := keys.VerifySignature(ctx, unknown); err == nil {
			t.Fatal("a token with an unknown key ID verified")
		}
	}
	if f.certHits != 2 {
		t.Errorf("fetched %d times for unknown key IDs; want 2 (one more, once a minute)", f.certHits)
	}
	c.Advance(time.Hour)
	if _, err := keys.VerifySignature(ctx, good); err != nil || f.certHits != 3 {
		t.Errorf("after max-age the keys are fetched again: %d hits, %v", f.certHits, err)
	}
	hs, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte("0123456789abcdef0123456789abcdef")}, nil)
	jws, _ = hs.Sign([]byte(`{}`))
	hmacToken, _ := jws.CompactSerialize()
	if _, err := keys.VerifySignature(ctx, hmacToken); err == nil {
		t.Error("an HS256 token was accepted")
	}
}
