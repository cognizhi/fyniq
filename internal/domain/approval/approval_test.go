package approval

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
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
	got, err = Canonical(map[string]any{"s": "\"\\\b\f\n\r\t\x01\x1f<>&\u2028é", "n": nil, "b": true, "i": int64(-42), "l": []any{1, "x", json.Number("7")}, "e": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"b\":true,\"e\":[],\"i\":-42,\"l\":[1,\"x\",7],\"n\":null,\"s\":\"\\\"\\\\\\b\\f\\n\\r\\t\\u0001\\u001f<>&\u2028é\"}"; string(got) != want {
		t.Errorf("escapes:\n got %s\nwant %s", got, want)
	}
	for _, bad := range []any{1.5, float32(2), int64(1 << 53), map[string]any{"x": uint(1)}, "\xff", json.Number("1.5"), json.Number("9007199254740993")} {
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
		if p.ObjectType != typ || p.Version == "" || p.Number != 0 || len(p.Steps) != 1 || p.Steps[0].Quorum != 1 || p.Fixed != (typ == ObjectPolicy) {
			t.Errorf("the built-in policy of %s: %+v", typ, p)
		}
		if _, ok := Dependencies[typ]; !ok {
			t.Errorf("%s declares no dependencies", typ)
		}
		if Rows[typ] == "" {
			t.Errorf("%s names no Appendix D row", typ)
		}
	}
}

// A step is signed by anyone who holds A on the object's row unless it names roles or members; the steps are
// signed in order, each to its quorum.
// req: FR-APR-01
func TestStepsAreSignedInOrderToTheirQuorum(t *testing.T) {
	ann, ben, cat := uuid.New(), uuid.New(), uuid.New()
	anyone, controllers, named := Step{Quorum: 1}, Step{Quorum: 2, Roles: []string{"controller"}}, Step{Quorum: 1, Users: []uuid.UUID{cat}}
	if !Eligible(anyone, ann, nil) || !Eligible(controllers, ben, []string{"owner", "controller"}) || Eligible(controllers, ben, []string{"owner"}) ||
		!Eligible(named, cat, nil) || Eligible(named, ann, []string{"controller"}) {
		t.Fatal("who may sign a step")
	}
	steps := []Step{controllers, named}
	for _, c := range []struct {
		sigs []Signature
		want int
	}{
		{nil, 1},
		{[]Signature{{Step: 1, Signer: ann, Approve: true}}, 1},
		{[]Signature{{Step: 1, Signer: ann, Approve: true}, {Step: 1, Signer: ben, Approve: false}}, 1},
		{[]Signature{{Step: 1, Signer: ann, Approve: true}, {Step: 1, Signer: ben, Approve: true}}, 2},
		{[]Signature{{Step: 1, Signer: ann, Approve: true}, {Step: 1, Signer: ben, Approve: true}, {Step: 2, Signer: cat, Approve: true}}, 0},
	} {
		if got := CurrentStep(steps, c.sigs); got != c.want {
			t.Errorf("after %v: step %d; want %d", c.sigs, got, c.want)
		}
	}
}

// A policy's steps can always be signed: 1 to 5 steps, quorums of 1 to 5, roles that approve the object's row and
// members whose roles do, never both in one step, and enough members for every quorum, a person signing once.
// req: FR-APR-01
// req: FR-APR-09
func TestAPolicysStepsCanBeSigned(t *testing.T) {
	const row = "Organisation profile, financial year, defaults (rev 0.29)"
	approves := func(r string) bool { return r == "owner" || r == "controller" }
	ann, ben, cat, dan := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	members := []Member{{ann, []string{"owner"}, true}, {ben, []string{"owner", "controller"}, true}, {cat, []string{"controller"}, true},
		{dan, []string{"bookkeeper"}, false}}
	for name, c := range map[string]struct {
		steps []Step
		want  string // the first problem's field; "" for none
	}{
		"the built-in":                 {[]Step{{Quorum: 1}}, ""},
		"two owners, then a member":    {[]Step{{Quorum: 2, Roles: []string{"owner"}}, {Quorum: 1, Users: []uuid.UUID{cat}}}, ""},
		"no steps":                     {nil, "body.steps"},
		"six steps":                    {[]Step{{Quorum: 1}, {Quorum: 1}, {Quorum: 1}, {Quorum: 1}, {Quorum: 1}, {Quorum: 1}}, "body.steps"},
		"a quorum of none":             {[]Step{{Quorum: 0}}, "body.steps[0].quorum"},
		"a quorum of six":              {[]Step{{Quorum: 6}}, "body.steps[0].quorum"},
		"roles and members":            {[]Step{{Quorum: 1, Roles: []string{"owner"}, Users: []uuid.UUID{ann}}}, "body.steps[0]"},
		"a role that never approves":   {[]Step{{Quorum: 1, Roles: []string{"bookkeeper"}}}, "body.steps[0].roles[0]"},
		"a role twice":                 {[]Step{{Quorum: 1, Roles: []string{"owner", "owner"}}}, "body.steps[0].roles[1]"},
		"a member who never approves":  {[]Step{{Quorum: 1, Users: []uuid.UUID{dan}}}, "body.steps[0].users[0]"},
		"someone who is not a member":  {[]Step{{Quorum: 1, Users: []uuid.UUID{uuid.New()}}}, "body.steps[0].users[0]"},
		"a member twice":               {[]Step{{Quorum: 1, Users: []uuid.UUID{ann, ann}}}, "body.steps[0].users[1]"},
		"more controllers than exist":  {[]Step{{Quorum: 3, Roles: []string{"controller"}}}, "body.steps[0].quorum"},
		"more approvals than signers":  {[]Step{{Quorum: 2, Roles: []string{"controller"}}, {Quorum: 2, Roles: []string{"controller"}}}, "body.steps"},
		"every approver, in two steps": {[]Step{{Quorum: 2, Roles: []string{"owner"}}, {Quorum: 1, Roles: []string{"controller"}}}, ""},
	} {
		got := CheckSteps(row, c.steps, approves, members)
		switch {
		case c.want == "" && len(got) > 0:
			t.Errorf("%s: %+v", name, got)
		case c.want != "" && (len(got) == 0 || got[0].Field != c.want || got[0].Code != CodeSteps || got[0].Fix == ""):
			t.Errorf("%s: %+v; want a problem with %s", name, got, c.want)
		}
	}
}
