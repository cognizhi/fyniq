package audit

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// testSigner is an in-memory Ed25519 signer.
type testSigner struct{ key ed25519.PrivateKey }

func (s testSigner) KeyID() string { return "test" }
func (s testSigner) Sign(_ context.Context, m []byte) ([]byte, error) {
	return ed25519.Sign(s.key, m), nil
}
func (s testSigner) Verify(id string, m, sig []byte) error {
	if id != "test" || !ed25519.Verify(s.key.Public().(ed25519.PublicKey), m, sig) {
		return ports.ErrBadSignature
	}
	return nil
}

var (
	org    = uuid.MustParse("0192f1a0-0000-7000-8000-0000000000aa")
	at     = time.Date(2026, 10, 15, 1, 2, 3, 0, time.UTC)
	signer = testSigner{ed25519.NewKeyFromSeed(make([]byte, 32))}
)

// req: SEC-15
// asvs: V16.2.5
func TestSensitiveValuesAreMasked(t *testing.T) {
	e, err := New(org, at, Actor{"user", "u1"}, "employee.updated", Object{Type: "employee", ID: org}, map[string]Change{
		"bank_account_number": {"012-345678-9", "123-456789-001"},
		"nric":                {nil, "S1234567D"},
		"client_secret":       {nil, "abc123-very-secret"},
		"access_token":        {nil, "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefgh"},
		"note":                {nil, "Paid S7654321A by Bearer abc.def.ghi; key sk-live_1234567890abcdef"},
		"definition":          {nil, "a fine word"},
		"amount":              {"100.00", "1000000000.00"},
		"nested":              {nil, map[string]any{"password": "hunter2", "list": []any{"T0123456G"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.Reason = "customer S2222222B asked"
	body, err := e.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"345678", "456789", "S1234567D", "abc123-very-secret", "eyJhbGciOiJIUzI1NiJ9",
		"S7654321A", "abc.def.ghi", "sk-live_1234567890abcdef", "hunter2", "T0123456G", "S2222222B"} {
		if strings.Contains(string(body), leaked) {
			t.Errorf("the event body contains %q:\n%s", leaked, body)
		}
	}
	for _, kept := range []string{"*-001", "567D", "a fine word", "1000000000.00", "[redacted]"} {
		if !strings.Contains(string(body), kept) {
			t.Errorf("the event body lost %q, which is not sensitive:\n%s", kept, body)
		}
	}
	again, _ := e.Canonical()
	if string(again) != string(body) {
		t.Error("masking an event twice changed it")
	}
}

func chain(t *testing.T, n int) []Stored {
	t.Helper()
	prev := Genesis
	var out []Stored
	for i := range n {
		e, err := New(org, at.Add(time.Duration(i)*time.Second), Actor{"service", "test"}, "account.created", Object{Type: "account", ID: org}, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := e.Canonical()
		h := Link(prev, string(body))
		out = append(out, Stored{ID: e.EventID, Seq: int64(i + 1), Body: string(body), PrevHash: prev, Hash: h})
		prev = h
	}
	return out
}

// req: INV-12
func TestTheChainCatchesEveryKindOfTampering(t *testing.T) {
	good := chain(t, 5)
	hashes, err := Chain(good)
	if err != nil || len(hashes) != 5 {
		t.Fatalf("a good chain: %v", err)
	}
	for name, tamper := range map[string]func([]Stored) []Stored{
		"a changed body": func(c []Stored) []Stored {
			c[2].Body = strings.Replace(c[2].Body, "account.created", "account.deleted", 1)
			return c
		},
		"a deleted event": func(c []Stored) []Stored { return append(c[:2], c[3:]...) },
		"swapped events":  func(c []Stored) []Stored { c[1], c[2] = c[2], c[1]; return c },
		"a forged hash": func(c []Stored) []Stored {
			c[4].Hash = Link(c[4].PrevHash, c[4].Body+" ")
			return c
		},
		"another event's id": func(c []Stored) []Stored { c[0].ID = c[1].ID; return c },
	} {
		events := append([]Stored(nil), chain(t, 0)...)
		events = append(events, good...)
		copied := make([]Stored, len(events))
		copy(copied, events)
		if _, err := Chain(tamper(copied)); err == nil {
			t.Errorf("%s went unnoticed", name)
		}
	}
}

// req: CTL-AUD-02
// req: INV-12
func TestAnchorsCatchARewrittenChain(t *testing.T) {
	original := chain(t, 3)
	hashes, _ := Chain(original)
	signed, err := Sign(context.Background(), signer, org, 3, hashes[3], at)
	if err != nil {
		t.Fatal(err)
	}
	if err := signed.Check(signer, org, hashes); err != nil {
		t.Fatalf("a good anchor: %v", err)
	}
	// Rewriting the whole chain consistently fools the chain check but not the anchor.
	rewritten := chain(t, 3)
	rehashed, err := Chain(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if err := signed.Check(signer, org, rehashed); err == nil {
		t.Fatal("a chain rewritten after its anchor went unnoticed")
	}
	// A forged anchor fails its signature.
	b, _ := json.Marshal(signed)
	var forged Signed
	_ = json.Unmarshal(b, &forged)
	forged.Anchor.HeadHash = strings.Repeat("0", 64)
	if err := forged.Check(signer, org, hashes); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("a forged anchor: %v", err)
	}
	if err := signed.Check(signer, uuid.New(), hashes); err == nil {
		t.Fatal("an anchor of another organisation was accepted")
	}
	if err := signed.Check(signer, org, map[int64][]byte{}); err == nil {
		t.Fatal("an anchor beyond a truncated chain was accepted")
	}
}

// fakeFingerprint is a keyed hash for tests: HMAC-SHA256 under a fixed key.
func fakeFingerprint(_ context.Context, v []byte) ([]byte, error) {
	m := hmac.New(sha256.New, []byte("test fingerprint key"))
	m.Write(v)
	return m.Sum(nil), nil
}

// AUD-02, proof only (PRD-001 rev 0.230): an identifier or a person's pay, at any depth, is replaced by its proof, the
// organisation's keyed fingerprint with an identifier's last four characters as its hint. The same value proves the
// same, so a change back shows; nothing of the value but the hint reaches the body. A forecast's salary_increase is a
// rate, not pay, and other values are untouched.
// req: AUD-02
// req: SEC-15
// asvs: V16.2.5
func TestSensitiveValuesAreProvedNeverKept(t *testing.T) {
	ctx := context.Background()
	in := map[string]Change{
		"account_number":      {"0123456789", "9876543210"},
		"bank_account_number": {"9876543210", "0123456789"},
		"salary":              {"8500.00", "8500.00"},
		"salary_increase":     {"0.04", "0.05"},
		"payee":               {nil, map[string]any{"name": "Northwind", "nric": "S1234567D", "accounts": []any{map[string]any{"iban": "SG12DBSS0123456789"}}}},
		"memo":                {"", "rent"},
	}
	out, err := Prove(ctx, in, fakeFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	proof := func(v any) map[string]any {
		t.Helper()
		p, ok := v.(map[string]any)
		if !ok || !strings.HasPrefix(p["fingerprint"].(string), FingerprintPrefix) || len(p["fingerprint"].(string)) != len(FingerprintPrefix)+43 {
			t.Fatalf("not a proof: %#v", v)
		}
		return p
	}
	a0, a1 := proof(out["account_number"][0]), proof(out["account_number"][1])
	b0, b1 := proof(out["bank_account_number"][0]), proof(out["bank_account_number"][1])
	if a0["fingerprint"] == a1["fingerprint"] || a0["fingerprint"] != b1["fingerprint"] || a1["fingerprint"] != b0["fingerprint"] {
		t.Errorf("equal values must prove equal and different ones differently: %v %v %v %v", a0, a1, b0, b1)
	}
	if a0["hint"] != "******6789" || a1["hint"] != "******3210" {
		t.Errorf("an identifier's hint is its last four characters: %v %v", a0, a1)
	}
	s0, s1 := proof(out["salary"][0]), proof(out["salary"][1])
	if s0["fingerprint"] != s1["fingerprint"] || s0["hint"] != nil {
		t.Errorf("an unchanged salary proves unchanged, with no hint: %v %v", s0, s1)
	}
	if out["salary_increase"] != in["salary_increase"] || out["memo"] != in["memo"] || out["payee"][0] != nil {
		t.Errorf("other values are untouched: %v", out)
	}
	payee := out["payee"][1].(map[string]any)
	if payee["name"] != "Northwind" || proof(payee["nric"])["hint"] != "*****567D" {
		t.Errorf("a nested NRIC is proved: %v", payee)
	}
	proof(payee["accounts"].([]any)[0].(map[string]any)["iban"])
	again, err := Prove(ctx, out, fakeFingerprint)
	if err != nil || again["account_number"][0].(map[string]any)["fingerprint"] != a0["fingerprint"] {
		t.Errorf("a proof is not proved again: %v %v", again["account_number"], err)
	}
	e, err := New(org, at, Actor{"user", "u1"}, "payee_bank_account.changed", Object{Type: "document", ID: org}, out)
	if err != nil {
		t.Fatal(err)
	}
	body, err := e.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, clear := range []string{"0123456789", "9876543210", "8500.00", "S1234567D", "SG12DBSS0123456789"} {
		if strings.Contains(string(body), clear) {
			t.Errorf("the body holds %s: %s", clear, body)
		}
	}
	if !strings.Contains(string(body), `"schema":"`+Schema+`"`) || !strings.Contains(string(body), a0["fingerprint"].(string)) {
		t.Errorf("the body is in the current format and keeps the proofs as written: %s", body)
	}
	if _, err := Prove(ctx, in, func(context.Context, []byte) ([]byte, error) { return nil, errors.New("kms down") }); err == nil {
		t.Error("a fingerprint that fails must fail the event, never keep the value")
	}
}
