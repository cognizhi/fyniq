package link

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const fixtures = "../../../contracts/peoplelink/"

// testKey is the key of the fixtures: derived from a phrase at test time, so no private key is committed (the public key is).
func testKey() ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("fyniq people link test vector 1"))
	return ed25519.NewKeyFromSeed(seed[:])
}

type canonicalFixtures struct {
	Canonical []struct{ Name, Input, Canonical, SHA256 string }
	Refused   []struct{ Name, Input string }
}

// part of NM-INT-10 (its envelope; the requirement is tagged when its inbox and lifecycle are built)
func TestCanonicalFixtures(t *testing.T) {
	var f canonicalFixtures
	raw, err := os.ReadFile(fixtures + "canonical.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Canonical {
		got, err := Canonical([]byte(c.Input))
		if err != nil || string(got) != c.Canonical {
			t.Errorf("%s: %s, %v; want %s", c.Name, got, err, c.Canonical)
		}
		if d, err := Digest([]byte(c.Input)); err != nil || d != c.SHA256 {
			t.Errorf("%s: digest %s, %v; want %s", c.Name, d, err, c.SHA256)
		}
	}
	for _, c := range f.Refused {
		if _, err := Canonical([]byte(c.Input)); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: want a refusal, got %v", c.Name, err)
		}
	}
}

type signedFixture struct {
	Note      string
	PublicKey string `json:"public_key"`
	Now       string
	Epoch     int
	Envelope  Envelope
}

func fixtureHeader() Header {
	return Header{EventID: "0192a000-0000-7000-8000-000000000001", Type: "payroll.run.approved", SchemaVersion: 1, SourceSystem: "nemura",
		SourceOrganisation: "0192a000-0000-7000-8000-0000000000a1", TargetOrganisation: "0192a000-0000-7000-8000-0000000000b2", Epoch: 3, Sequence: 41,
		ObjectType: "payroll_run", ObjectID: "0192a000-0000-7000-8000-0000000000c3", ObjectVersion: 2, EffectiveAt: "2026-10-31T00:00:00Z",
		RecordedAt: "2026-11-02T03:00:00Z", SignedAt: "2026-11-02T03:00:01Z", CorrelationID: "0192a000-0000-7000-8000-0000000000d4"}
}

const fixturePayload = `{"wage_month":"2026-10","totals":[{"department":"G&A","pay_item":"basic","amount":"10200.0000"}],"currency":"SGD"}`

// part of NM-INT-10 (its envelope; the requirement is tagged when its inbox and lifecycle are built)
func TestSignedFixtureVerifiesAndIsReproducible(t *testing.T) {
	key := testKey()
	want, err := Sign(key, fixtureHeader(), []byte(fixturePayload))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(fixtures + "envelope.json")
	if err != nil {
		t.Fatal(err)
	}
	var got signedFixture
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Envelope.Signature != want.Signature || got.Envelope.Header != want.Header {
		t.Fatalf("the fixture is not what the rule signs; Fyniq's copy owns the fixture (internal/domain/peoplelink, go test -update):\n%+v\n%+v", got.Envelope, want)
	}
	pub, _ := hex.DecodeString(got.PublicKey)
	now, _ := time.Parse(time.RFC3339, got.Now)
	if err := got.Envelope.Verify(linkOf(pub), now); err != nil {
		t.Fatalf("the fixture does not verify: %v", err)
	}
}

func linkOf(pub []byte) Link {
	h := fixtureHeader()
	return Link{Key: pub, Epoch: h.Epoch, SourceOrganisation: h.SourceOrganisation, TargetOrganisation: h.TargetOrganisation, SourceSystem: "nemura"}
}

// part of NM-INT-10 (its envelope; the requirement is tagged when its inbox and lifecycle are built)
// part of NM-INT-06 (its envelope; the requirement is tagged when its inbox and lifecycle are built)
func TestAnEnvelopeThatIsForgedReplayedOrStaleIsRefused(t *testing.T) {
	key := testKey()
	pub := key.Public().(ed25519.PublicKey)
	now, _ := time.Parse(time.RFC3339, "2026-11-02T03:00:30Z")
	good, err := Sign(key, fixtureHeader(), []byte(fixturePayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := good.Verify(linkOf(pub), now); err != nil {
		t.Fatal(err)
	}
	other := ed25519.NewKeyFromSeed(make([]byte, 32))
	cases := []struct {
		name string
		edit func(e *Envelope, l *Link, at *time.Time)
		code string
	}{
		{"altered totals under the earlier approval's digest", func(e *Envelope, _ *Link, _ *time.Time) {
			e.Payload = json.RawMessage(strings.Replace(fixturePayload, "10200.0000", "20200.0000", 1))
		}, CodeDigest},
		{"an altered header", func(e *Envelope, _ *Link, _ *time.Time) { e.Header.ObjectVersion = 1 }, CodeSignature},
		{"a digest rewritten to match altered totals", func(e *Envelope, _ *Link, _ *time.Time) {
			e.Payload = json.RawMessage(strings.Replace(fixturePayload, "10200.0000", "20200.0000", 1))
			e.Header.PayloadSHA256, _ = Digest(e.Payload)
		}, CodeSignature},
		{"signed with another key", func(e *Envelope, _ *Link, _ *time.Time) { *e, _ = Sign(other, fixtureHeader(), []byte(fixturePayload)) }, CodeSignature},
		{"a link key that is not the signer's", func(_ *Envelope, l *Link, _ *time.Time) { l.Key = other.Public().(ed25519.PublicKey) }, CodeSignature},
		{"an earlier epoch", func(_ *Envelope, l *Link, _ *time.Time) { l.Epoch = 4 }, CodeEpoch},
		{"an epoch not linked yet", func(_ *Envelope, l *Link, _ *time.Time) { l.Epoch = 2 }, CodeEpoch},
		{"another target organisation", func(_ *Envelope, l *Link, _ *time.Time) {
			l.TargetOrganisation = "0192a000-0000-7000-8000-0000000000ff"
		}, CodeOrg},
		{"another source system", func(_ *Envelope, l *Link, _ *time.Time) { l.SourceSystem = "other" }, CodeOrg},
		{"a replay six minutes later", func(_ *Envelope, _ *Link, at *time.Time) { *at = at.Add(6 * time.Minute) }, CodeReplay},
		{"a clock six minutes ahead", func(_ *Envelope, _ *Link, at *time.Time) { *at = at.Add(-7 * time.Minute) }, CodeReplay},
		{"no signature", func(e *Envelope, _ *Link, _ *time.Time) { e.Signature = "" }, CodeMalformed},
		{"no payload", func(e *Envelope, _ *Link, _ *time.Time) { e.Payload = nil }, CodeMalformed},
	}
	for _, c := range cases {
		e, l, at := good, linkOf(pub), now
		c.edit(&e, &l, &at)
		var r *Refusal
		if err := e.Verify(l, at); !errors.As(err, &r) || r.Code != c.code {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
		}
	}
	// the edge of the window is accepted
	if err := good.Verify(linkOf(pub), now.Add(4*time.Minute)); err != nil {
		t.Errorf("4 minutes after signing: %v", err)
	}
}

// part of NM-INT-10 (its envelope; the requirement is tagged when its inbox and lifecycle are built)
func TestARefusalNeverCarriesThePayload(t *testing.T) {
	key := testKey()
	e, _ := Sign(key, fixtureHeader(), []byte(fixturePayload))
	e.Payload = json.RawMessage(strings.Replace(fixturePayload, "10200.0000", "88888.8888", 1))
	err := e.Verify(linkOf(key.Public().(ed25519.PublicKey)), time.Date(2026, 11, 2, 3, 0, 30, 0, time.UTC))
	if err == nil || strings.Contains(err.Error(), "88888") || strings.Contains(err.Error(), "10200") {
		t.Fatalf("a refusal must not repeat the payload: %v", err)
	}
}
