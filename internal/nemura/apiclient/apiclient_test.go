package apiclient

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// A registration needs a name, scopes from the catalogue, an accepted algorithm and a public key with no private part: the private key never
// reaches Nemura (NM-ARC-06).
// req: NM-ARC-06
// asvs: V6.4.1, V13.2.1
func TestARegistrationNeedsAPublicKeyAndKnownScopes(t *testing.T) {
	pub := json.RawMessage(`{"kty":"EC","crv":"P-256","x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU","y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"}`)
	if err := Check("Payroll bureau", []string{"directory.read"}, "ES256", pub); err != nil {
		t.Fatalf("a valid registration: %v", err)
	}
	long := strings.Repeat("x", 101)
	for name, err := range map[string]error{
		"no name":                 Check("  ", []string{"directory.read"}, "ES256", pub),
		"a name over 100":         Check(long, []string{"directory.read"}, "ES256", pub),
		"no scope":                Check("x", nil, "ES256", pub),
		"an unknown scope":        Check("x", []string{"payroll.write"}, "ES256", pub),
		"an unaccepted algorithm": Check("x", []string{"directory.read"}, "HS256", pub),
		"a key that is not a JWK": Check("x", []string{"directory.read"}, "ES256", json.RawMessage(`"abc"`)),
		"a key with no type":      Check("x", []string{"directory.read"}, "ES256", json.RawMessage(`{"x":"1"}`)),
		"a private EC key":        Check("x", []string{"directory.read"}, "ES256", json.RawMessage(`{"kty":"EC","crv":"P-256","x":"a","y":"b","d":"secret"}`)),
		"a private RSA key":       Check("x", []string{"directory.read"}, "RS256", json.RawMessage(`{"kty":"RSA","n":"a","e":"AQAB","p":"p"}`)),
		"a symmetric key":         Check("x", []string{"directory.read"}, "ES256", json.RawMessage(`{"kty":"oct","k":"abc"}`)),
	} {
		var r *Refusal
		if !errors.As(err, &r) || r.Code != CodeInvalid || r.Message == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
