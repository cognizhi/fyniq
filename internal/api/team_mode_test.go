//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/team"
)

// requestsOf counts the organisation's approval requests for objectType.
func requestsOf(e *env, objectType string) int {
	n := 0
	items, _ := e.call(http.MethodGet, "/v1/approvals?status=all&page_size=500", e.as(authz.Owner), nil).Body["items"].([]any)
	for _, it := range items {
		if it.(map[string]any)["object_type"] == objectType {
			n++
		}
	}
	return n
}

// The organisation starts in Founder mode, which allows the founders' conflicting roles. A switch is an approval request
// that the Administrator asks for and another Owner approves (dual control); a switch to Standard mode lists every
// member whose roles conflict and waits until each is resolved or overridden with a reason; the approval records the
// overrides, reports them, and voids every pending request. In Standard mode a role change that makes a conflict needs
// an override, an invitation never makes one, and a person who holds a conflict without an override does not post.
// req: access/team-modes Founder mode
// req: access/team-modes Standard mode
// req: access/team-modes Enterprise mode
// req: access/team-modes Mode switching
// req: SOD-6
// req: FR-ADM-04
func TestTeamModeSwitchesAreDualControlled(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben := f.as(f.ann), f.as(f.ben)
	const path = "/v1/team-mode"
	switchTo := func(who, mode, etag string, overrides ...map[string]any) response {
		body := map[string]any{"mode": mode}
		if len(overrides) > 0 {
			body["overrides"] = overrides
		}
		return e.call(http.MethodPatch, path, who, body, "If-Match", etag)
	}

	// Founder mode at first: every founder holds SOD-6, which Founder mode allows.
	g := e.call(http.MethodGet, path, ben, nil)
	if g.Status != 200 || g.Header.Get("ETag") != `"0"` || g.str("mode") != "founder" || g.Body["blocks_conflicts"] != false ||
		len(g.Body["conflicts"].([]any)) != 3 || !strings.Contains(g.Raw, `"history":[]`) || !strings.Contains(g.Raw, `"overrides":[]`) ||
		!strings.Contains(g.Raw, `"conflict":"SOD-6","member_id":"`+f.annMember+`","user_id":"`+f.ann+`","display_name":"Ann","roles":["controller","owner","administrator"],"overridden":false`) {
		t.Fatalf("the team mode at first: %d %s", g.Status, g.Raw)
	}
	if o := e.call(http.MethodGet, "/v1/organisation", ben, nil); o.str("team_mode") != "founder" {
		t.Fatalf("the organisation's team mode: %s", o.Raw)
	}

	// The refusals: no C on "Users, roles, team mode", the mode in force, Enterprise mode, unresolved conflicts, an
	// override of nobody's conflict, a stale ETag.
	e.problemIs(switchTo(f.as(f.ann, authz.Owner, authz.Controller), "standard", `"0"`), "switchTeamMode", 403, CodeForbidden)
	e.problemIs(switchTo(ann, "founder", `"0"`), "switchTeamMode", 409, team.CodeSame)
	if r := switchTo(ann, "enterprise", `"0"`); r.Status != 422 || !strings.Contains(r.Raw, `"code":"`+team.CodeUnavailable+`"`) {
		t.Fatalf("Enterprise mode: %d %s", r.Status, r.Raw)
	}
	r := switchTo(ann, "standard", `"0"`)
	e.problemIs(r, "switchTeamMode", 409, team.CodeConflicts)
	if len(r.Body["errors"].([]any)) != 3 || !strings.Contains(r.Raw, "Cat holds roles that conflict (SOD-6: controller, owner, administrator)") {
		t.Fatalf("the conflicts listed: %s", r.Raw)
	}
	if r := switchTo(ann, "standard", `"0"`, map[string]any{"member_id": uuid.NewString(), "conflict": "SOD-6", "reason": "x"}); r.Status != 422 ||
		!strings.Contains(r.Raw, `"field":"body.overrides[0].member_id"`) {
		t.Fatalf("an override of nobody: %d %s", r.Status, r.Raw)
	}
	e.problemIs(switchTo(ann, "standard", `"4"`), "switchTeamMode", 412, CodeStale)

	// Cat's conflict is resolved: her Administrator role ends, which Ben approves (role changes are dual-controlled).
	end := e.call(http.MethodDelete, "/v1/members/"+f.catMember+"/roles/administrator", ann, nil)
	if end.Status != 202 {
		t.Fatalf("ending Cat's role: %d %s", end.Status, end.Raw)
	}
	if a := f.decide(ben, end, "approve"); a.Status != 200 {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}

	// A settings change waits, asked for under Founder mode.
	waiting := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", ben, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if waiting.Status != 202 || !strings.Contains(waiting.str("envelope"), `"team_mode":"founder"`) {
		t.Fatalf("a settings change: %d %s", waiting.Status, waiting.Raw)
	}

	// Ann asks for Standard mode with an override for Ann and for Ben.
	annBen := []map[string]any{
		{"member_id": f.annMember, "conflict": "SOD-6", "reason": "  Founder who keeps the books until we hire  "},
		{"member_id": membershipOf(t, e, f.ben), "conflict": "SOD-6", "reason": "Second founder"},
	}
	r = switchTo(ann, "standard", `"0"`, annBen...)
	if r.Status != 202 || r.str("object_type") != "team_mode" || r.str("object_key") != "organisation" || r.str("operation") != "team_mode.change" ||
		r.str("policy") != "founder/team_mode@2026-10-20.1" || r.str("summary") != "Switch the team mode from founder to standard" ||
		!strings.Contains(r.str("envelope"), `"dependencies":{"conflicts":["member:`) ||
		!strings.Contains(r.str("envelope"), `"mode_version":0`) ||
		!strings.Contains(r.Raw, `{"field":"overrides","before":"","after":"Ann (SOD-6), Ben (SOD-6)"}`) {
		t.Fatalf("asking for Standard mode: %d %s", r.Status, r.Raw)
	}
	e.problemIs(switchTo(ann, "standard", `"0"`, annBen...), "switchTeamMode", 409, approval.CodePending)
	e.problemIs(f.decide(ann, r, "approve"), "approveApproval", 403, approval.CodeOwnRequest)

	// Ben approves: Standard mode, the overrides kept and reported, and the waiting settings change void.
	if a := f.decide(ben, r, "approve"); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the switch: %d %s", a.Status, a.Raw)
	}
	g = e.call(http.MethodGet, path, ben, nil)
	if g.Header.Get("ETag") != `"1"` || g.str("mode") != "standard" || g.Body["blocks_conflicts"] != true ||
		len(g.Body["overrides"].([]any)) != 2 || !strings.Contains(g.Raw, `"reason":"Founder who keeps the books until we hire","team_mode_version":1,"approval_id":"`+r.str("id")+`"`) ||
		!strings.Contains(g.Raw, `"display_name":"Ann","roles":["controller","owner","administrator"],"overridden":true`) ||
		!strings.Contains(g.Raw, `"history":[{"version":1,"mode":"standard","approval_id":"`+r.str("id")+`","requested_by":"`+f.ann+`"`) {
		t.Fatalf("Standard mode: %s", g.Raw)
	}
	if o := e.call(http.MethodGet, "/v1/organisation", ben, nil); o.str("team_mode") != "standard" {
		t.Fatalf("the organisation's team mode: %s", o.Raw)
	}
	if w := e.call(http.MethodGet, "/v1/approvals/"+waiting.str("id"), ben, nil); w.str("status") != "void" ||
		w.str("status_reason") != "the team mode changed after the request" {
		t.Fatalf("the settings change waiting when the mode changed: %s", w.Raw)
	}
	trail := auditTrail(t, e)
	for _, want := range []string{`"approval_ids":["` + r.str("id") + `"],"action":"team_mode.changed"`, `"action":"sod.overridden"`,
		`"status_reason":[null,"the team mode changed after the request"]`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}

	// In Standard mode a role change that makes a conflict needs an override, which its approval records.
	give := func(reason string) response {
		body := map[string]any{"role": "administrator"}
		if reason != "" {
			body["sod_override_reason"] = reason
		}
		return e.call(http.MethodPost, "/v1/members/"+f.catMember+"/roles", ann, body)
	}
	e.problemIs(give(""), "assignRole", 409, team.CodeConflict)
	if bad := e.call(http.MethodPost, "/v1/members/"+f.catMember+"/roles", ann, map[string]any{"role": "auditor", "sod_override_reason": "x"}); bad.Status != 422 ||
		!strings.Contains(bad.Raw, `"code":"`+team.CodeOverride+`"`) {
		t.Fatalf("an override of nothing: %d %s", bad.Status, bad.Raw)
	}
	cat := give("Third founder, until we hire an administrator")
	if cat.Status != 202 || !strings.Contains(cat.str("envelope"), `"team_mode":"standard"`) ||
		!strings.Contains(cat.str("envelope"), `"sod_override":{"conflict":"SOD-6","reason":"Third founder, until we hire an administrator","user_id":"`+f.cat+`"}`) ||
		cat.str("summary") != "Give Cat the role administrator, overriding SOD-6" {
		t.Fatalf("giving Cat the role with an override: %d %s", cat.Status, cat.Raw)
	}
	if a := f.decide(ben, cat, "approve"); a.Status != 200 {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, path, ben, nil); len(g.Body["overrides"].([]any)) != 3 ||
		!strings.Contains(g.Raw, `"display_name":"Cat","roles":["controller","owner","administrator"],"overridden":true`) {
		t.Fatalf("Cat's override: %s", g.Raw)
	}

	// An invitation never makes a conflict in Standard mode.
	inv := e.call(http.MethodPost, "/v1/members/invitations", ann, map[string]any{"display_name": "Dan", "roles": []string{"administrator", "accountant"}})
	e.problemIs(inv, "createMemberInvitation", 409, team.CodeConflict)

	// SOD-6 when acting: posting needs no conflict, or an override for the mode in force.
	id, tag := e.createDraft("10.00")
	stranger := e.principal(authn.Principal{Subject: "dan", UserID: uuid.New(), Client: "web", Class: authn.ClassUser, Organisation: e.books.Org,
		Roles: []authz.Role{authz.Administrator, authz.Accountant}})
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", stranger, nil, "If-Match", tag), "postJournal", 403, team.CodeAction)
	if p := e.call(http.MethodPost, "/v1/journals/"+id+"/post", f.as(f.ann, authz.Administrator, authz.Accountant), nil, "If-Match", tag); p.Status != 200 {
		t.Fatalf("Ann posts under her override: %d %s", p.Status, p.Raw)
	}

	// Back to Founder mode: no overrides to send, and none apply to the new version.
	e.problemIs(switchTo(ann, "founder", `"1"`, annBen[0]), "switchTeamMode", 422, CodeInvalid)
	back := switchTo(ann, "founder", `"1"`)
	if back.Status != 202 || !strings.Contains(back.str("envelope"), `"mode_version":1`) {
		t.Fatalf("asking for Founder mode: %d %s", back.Status, back.Raw)
	}
	if a := f.decide(ben, back, "approve"); a.Status != 200 {
		t.Fatalf("approving it: %d %s", a.Status, a.Raw)
	}
	g = e.call(http.MethodGet, path, ben, nil)
	if g.str("mode") != "founder" || g.Header.Get("ETag") != `"2"` || !strings.Contains(g.Raw, `"overrides":[]`) || len(g.Body["history"].([]any)) != 2 {
		t.Fatalf("Founder mode again: %s", g.Raw)
	}
}

// A member's conflicts change after a switch is asked for: the switch's envelope differs, so it never executes.
// req: access/team-modes Mode switching
// req: FR-APR-10
func TestASwitchBlocksWhenTheConflictsChange(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben := f.as(f.ann), f.as(f.ben)
	overrides := []map[string]any{}
	for _, u := range []string{f.ann, f.ben, f.cat} {
		overrides = append(overrides, map[string]any{"member_id": membershipOf(t, e, u), "conflict": "SOD-6", "reason": "Founder"})
	}
	r := e.call(http.MethodPatch, "/v1/team-mode", ann, map[string]any{"mode": "standard", "overrides": overrides}, "If-Match", `"0"`)
	if r.Status != 202 {
		t.Fatalf("asking for Standard mode: %d %s", r.Status, r.Raw)
	}
	// Ben withdraws nothing; Cat's roles change through an approved role change meanwhile, which needs no override in
	// Founder mode.
	end := e.call(http.MethodDelete, "/v1/members/"+f.catMember+"/roles/administrator", ben, nil)
	if a := f.decide(ann, end, "approve"); a.Status != 200 {
		t.Fatalf("ending Cat's role: %d %s", a.Status, a.Raw)
	}
	e.problemIs(f.decide(ben, r, "approve"), "approveApproval", 409, approval.CodeChanged)
	if g := e.call(http.MethodGet, "/v1/team-mode", ben, nil); g.str("mode") != "founder" {
		t.Fatalf("the switch executed: %s", g.Raw)
	}
}

// membershipOf is the membership ID of user.
func membershipOf(t *testing.T, e *env, user string) string {
	t.Helper()
	l := e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil)
	for _, it := range l.Body["items"].([]any) {
		if m := it.(map[string]any); m["user_id"] == user {
			return m["id"].(string)
		}
	}
	t.Fatalf("no member with user %s", user)
	return ""
}
