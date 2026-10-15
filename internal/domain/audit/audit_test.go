package audit

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
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
