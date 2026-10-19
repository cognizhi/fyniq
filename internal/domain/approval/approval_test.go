package approval

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// RFC 8785 §3.2.3's sorting example: names ordered by UTF-16 code units, so the emoji (a surrogate pair from
// 0xD83D) sorts before U+FB33, unlike Go's byte order; and §3.2.2.2's string escapes.
// req: FR-APR-10
func TestCanonicalIsRFC8785(t *testing.T) {
	got, err := Canonical(map[string]any{
		"\u20ac": "Euro Sign", "\r": "Carriage Return", "\ufb33": "Hebrew Letter Dalet With Dagesh", "1": "One",
		"\U0001F600": "Emoji: Grinning Face", "\u0080": "Control", "\u00f6": "Latin Small Letter O With Diaeresis",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"\\r\":\"Carriage Return\",\"1\":\"One\",\"\u0080\":\"Control\",\"\u00f6\":\"Latin Small Letter O With Diaeresis\"," +
		"\"\u20ac\":\"Euro Sign\",\"\U0001F600\":\"Emoji: Grinning Face\",\"\ufb33\":\"Hebrew Letter Dalet With Dagesh\"}"
	if string(got) != want {
		t.Errorf("sorted:\n got %s\nwant %s", got, want)
	}
	got, err = Canonical(map[string]any{"s": "\"\\\b\f\n\r\t\x01\x1f<>&\u2028é", "n": nil, "b": true, "i": int64(-42), "l": []any{1, "x"}, "e": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"b\":true,\"e\":[],\"i\":-42,\"l\":[1,\"x\"],\"n\":null,\"s\":\"\\\"\\\\\\b\\f\\n\\r\\t\\u0001\\u001f<>&\u2028é\"}"; string(got) != want {
		t.Errorf("escapes:\n got %s\nwant %s", got, want)
	}
	for _, bad := range []any{1.5, float32(2), int64(1 << 53), map[string]any{"x": uint(1)}, "\xff"} {
		if _, err := Canonical(bad); err == nil {
			t.Errorf("%#v was serialised; an envelope holds no binary fraction, unsafe integer, other type or invalid UTF-8", bad)
		}
	}
}

func envelope() Envelope {
	return Envelope{Organisation: uuid.MustParse("01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"), ObjectType: ObjectSetting, ObjectID: "bank.tolerance_base",
		ObjectVersion: 2, Operation: OpSettingChange, Change: map[string]any{"value": "5.00", "effective_from": "2026-11-01"},
		Amounts: []Amount{{"5.00", "SGD"}}, Dependencies: map[string]any{"setting_version": 2, "value_in_effect": "1.00", "scheduled": []any{}},
		Policy: Policies[ObjectSetting].Version, TeamMode: TeamModeFounder, RequestedBy: uuid.MustParse("01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01")}
}

// The envelope's bytes and hash are pinned: the same change always signs the same bytes, and changing any part
// of it, a dependency included, changes the hash, so a rebuilt envelope that differs blocks execution.
// req: FR-APR-10
// req: FR-APR-05
func TestTheEnvelopeIsCanonicalAndCoversItsDependencies(t *testing.T) {
	b, err := envelope().Canonical()
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"amounts":[{"amount":"5.00","currency":"SGD"}],"change":{"effective_from":"2026-11-01","value":"5.00"},` +
		`"dependencies":{"scheduled":[],"setting_version":2,"value_in_effect":"1.00"},"object":{"id":"bank.tolerance_base","type":"organisation_setting","version":2},` +
		`"operation":"organisation_setting.change","organisation":"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02","policy":"founder/organisation_setting@2026-10-20.1",` +
		`"requested_by":"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01","schema":"fyniq.approval-envelope/1","team_mode":"founder"}`
	if string(b) != want {
		t.Fatalf("the envelope:\n got %s\nwant %s", b, want)
	}
	if h := hex.EncodeToString(Hash(b)); len(h) != 64 {
		t.Fatalf("the hash: %s", h)
	}
	base := Hash(b)
	changes := map[string]func(e *Envelope){
		"the record's version":  func(e *Envelope) { e.ObjectVersion++ },
		"the change":            func(e *Envelope) { e.Change["value"] = "5.01" },
		"an amount":             func(e *Envelope) { e.Amounts[0].Amount = "5.01" },
		"the policy's version":  func(e *Envelope) { e.Policy += "+1" },
		"the team mode":         func(e *Envelope) { e.TeamMode = "standard" },
		"the requester":         func(e *Envelope) { e.RequestedBy = uuid.New() },
		"the operation":         func(e *Envelope) { e.Operation = OpRoleAssign },
		"the organisation":      func(e *Envelope) { e.Organisation = uuid.New() },
		"setting_version":       func(e *Envelope) { e.Dependencies["setting_version"] = 3 },
		"value_in_effect":       func(e *Envelope) { e.Dependencies["value_in_effect"] = "2.00" },
		"scheduled":             func(e *Envelope) { e.Dependencies["scheduled"] = []any{"2027-01-01=3.00"} },
		"the object's identity": func(e *Envelope) { e.ObjectID = "bank.tolerance_percent" },
	}
	for name, change := range changes {
		e := envelope()
		change(&e)
		b, err := e.Canonical()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bytes.Equal(Hash(b), base) {
			t.Errorf("changing %s left the hash as it was", name)
		}
	}
	for _, deps := range Dependencies {
		for _, name := range deps {
			if _, ok := changes[name]; !ok && strings.HasPrefix(name, "setting") {
				t.Errorf("no change of the dependency %s is tested", name)
			}
		}
	}

	// An envelope that leaves out a declared dependency, or adds another, is refused.
	e := envelope()
	delete(e.Dependencies, "scheduled")
	if _, err := e.Canonical(); err == nil {
		t.Error("an envelope without its dependency scheduled was built")
	}
	e = envelope()
	e.Dependencies["extra"] = 1
	if _, err := e.Canonical(); err == nil {
		t.Error("an envelope with an undeclared dependency was built")
	}
	e = envelope()
	e.ObjectType = "bill"
	if _, err := e.Canonical(); err == nil {
		t.Error("an envelope of an unknown object type was built")
	}
}

// The requester never approves their own request; a decided or expired request takes no signature.
// req: FM-1
// req: FR-APR-05
func TestSignerRules(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	requester, other := uuid.New(), uuid.New()
	r := Request{Status: Pending, RequestedBy: requester, ExpiresAt: now.Add(TTL)}
	if p := CheckSigner(r, other, now); p != nil {
		t.Errorf("another member signing: %v", p)
	}
	if p := CheckSigner(r, requester, now); p == nil || p.Code != CodeOwnRequest {
		t.Errorf("the requester signing: %v", p)
	}
	if p := CheckSigner(r, other, now.Add(TTL)); p == nil || p.Code != CodeExpired || StatusAt(r, now.Add(TTL)) != Expired {
		t.Errorf("at its expiry: %v", p)
	}
	if p := CheckSigner(r, other, now.Add(TTL-time.Nanosecond)); p != nil {
		t.Errorf("just before its expiry: %v", p)
	}
	for _, s := range []Status{Approved, Rejected, Void, Expired} {
		r.Status = s
		if p := CheckSigner(r, other, now); p == nil || (p.Code != CodeNotPending && p.Code != CodeExpired && !(s == Void && p.Code == CodeChanged)) {
			t.Errorf("a %s request: %v", s, p)
		}
	}
	for typ, p := range Policies {
		if p.ObjectType != typ || p.Version == "" || p.Mode != TeamModeFounder || len(p.Steps) == 0 || p.Steps[0].Quorum < 1 {
			t.Errorf("the policy of %s: %+v", typ, p)
		}
		if _, ok := Dependencies[typ]; !ok {
			t.Errorf("%s declares no dependencies", typ)
		}
	}
}
