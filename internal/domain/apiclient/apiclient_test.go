package apiclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func jwk(t *testing.T, key any) json.RawMessage {
	t.Helper()
	b, err := (jose.JSONWebKey{Key: key}).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An integration client's definition is checked whole, every refusal at once and naming its field (FR-INT-02, SEC-07,
// FR-INT-07, ENG-23): narrow scopes only, networks as CIDR prefixes, limits within the platform's, an expiry within a
// year, and the public half of an EC P-256 or RSA (2048+) key, never a private key.
// req: FR-INT-02
// req: SEC-07
// req: FR-INT-07
// asvs: V10.4.10, V11.6.1
func TestAnIntegrationClientIsCheckedWhole(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ok := Integration{Name: "Nightly canary", Scopes: []string{"fyniq.write.sales", "fyniq.read.accounting"}, Networks: []string{"203.0.113.9/24"},
		ReadsPerMinute: 60, WritesPerMinute: 10, ExpiresAt: now.AddDate(0, 6, 0), Key: jwk(t, &ec.PublicKey)}
	c, err := CheckIntegration(ok, now)
	if err != nil {
		t.Fatalf("a good client: %v", err)
	}
	if c.Alg != "ES256" || len(c.Thumbprint) != 43 || !slices.Equal(c.Networks, []string{"203.0.113.0/24"}) ||
		!slices.Equal(c.Scopes, []string{"fyniq.read.accounting", "fyniq.write.sales"}) {
		t.Errorf("checked: %+v", c)
	}
	public, err := PublicJWK(ok.Key, c.Alg)
	if err != nil || !json.Valid(public) {
		t.Fatalf("the registered key: %s, %v", public, err)
	}
	var reg map[string]any
	_ = json.Unmarshal(public, &reg)
	if reg["use"] != "sig" || reg["alg"] != "ES256" || reg["d"] != nil {
		t.Errorf("the registered key: %v", reg)
	}
	rs, _ := rsa.GenerateKey(rand.Reader, 2048)
	if c, err := CheckIntegration(Integration{Name: "RSA", Scopes: []string{"fyniq.read.reports"}, ExpiresAt: now.Add(time.Hour), Key: jwk(t, &rs.PublicKey)}, now); err != nil || c.Alg != "RS256" {
		t.Errorf("an RSA key: %+v, %v", c, err)
	}
	bad := Integration{Name: " padded", Scopes: []string{"fyniq.write.payroll", "fyniq.read.reports", "fyniq.read.reports"}, Networks: []string{"10.0.0.300/8"},
		ReadsPerMinute: 601, WritesPerMinute: -1, ExpiresAt: now.AddDate(1, 1, 0), Key: jwk(t, ec)}
	_, err = CheckIntegration(bad, now)
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("want every refusal: %v", err)
	}
	fields := map[string]string{}
	for _, e := range errs {
		fields[e.Field] = e.Code
		if e.Message == "" || e.Fix == "" {
			t.Errorf("%s: a refusal says what is wrong and how to fix it: %+v", e.Field, e)
		}
	}
	want := map[string]string{"name": CodeName, "scopes[0]": CodeScope, "scopes[2]": CodeScope, "networks[0]": CodeNetwork, "reads_per_minute": CodeLimit,
		"writes_per_minute": CodeLimit, "expires_at": CodeExpiry, "public_key": CodeKey}
	for f, code := range want {
		if fields[f] != code {
			t.Errorf("%s: want %s, got %q (all: %v)", f, code, fields[f], fields)
		}
	}
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	for name, key := range map[string]json.RawMessage{"none": nil, "not JSON": json.RawMessage(`"x"`), "RSA 1024": jwk(t, &small.PublicKey),
		"P-384": jwk(t, &p384.PublicKey), "a symmetric key": jwk(t, []byte("0123456789abcdef0123456789abcdef"))} {
		ok.Key = key
		if _, err := CheckIntegration(ok, now); err == nil {
			t.Errorf("%s: want the key refused", name)
		}
	}
	ok.Key, ok.ExpiresAt = jwk(t, &ec.PublicKey), now
	if _, err := CheckIntegration(ok, now); err == nil {
		t.Error("an expiry that is not in the future")
	}
	ok.ExpiresAt, ok.Scopes = now.Add(time.Hour), nil
	if _, err := CheckIntegration(ok, now); err == nil {
		t.Error("a client with no scope")
	}
}
