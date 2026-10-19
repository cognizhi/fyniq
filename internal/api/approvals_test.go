//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// founders is an organisation past the founder bootstrap: two founders, each Owner and Controller, and a third
// member with a role that expires.
type founders struct {
	e              *env
	ahkow, weiling string // their user IDs
	third          string // the third member's membership ID
}

func newFounders(t *testing.T) founders {
	e := newEnv(t)
	owner := e.as(authz.Owner)
	// Every invitation is made during the founder bootstrap, so each keeps its roles when accepted after.
	invite := func(name string, roles []string, extra ...any) string {
		body := map[string]any{"display_name": name, "roles": roles}
		if len(extra) == 2 {
			body[extra[0].(string)] = extra[1]
		}
		return e.call(http.MethodPost, "/v1/members/invitations", owner, body).str("code")
	}
	join := func(name, code string) response {
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: name, Client: "web"}),
			map[string]any{"invitation_code": code, "display_name": name})
		if j.Status != 201 {
			t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		return j
	}
	a, w := invite("Ah Kow", []string{"owner", "controller"}), invite("Wei Ling", []string{"owner", "controller"})
	x := invite("Third", []string{"tax_agent"}, "roles_expire_at", "2099-01-01T00:00:00Z")
	return founders{e: e, ahkow: join("Ah Kow", a).str("user_id"), weiling: join("Wei Ling", w).str("user_id"), third: join("Third", x).str("member_id")}
}

// as is a founder's session, stepped up now, holding roles (the Owner and the Controller by default).
func (f founders) as(user string, roles ...authz.Role) string {
	if len(roles) == 0 {
		roles = []authz.Role{authz.Owner, authz.Controller}
	}
	return f.e.principal(authn.Principal{Subject: "founder-" + user, UserID: uuid.MustParse(user), Client: "web", Class: authn.ClassUser,
		Organisation: f.e.books.Org, Roles: roles, StepUpAt: f.e.clock.Now()})
}

// decide approves or rejects a pending request, which is always at version 1.
func (f founders) decide(who, id, how string, body map[string]any) response {
	return f.e.call(http.MethodPost, "/v1/approvals/"+id+"/"+how, who, body, "If-Match", `"1"`)
}

func hashOf(r response) map[string]any {
	return map[string]any{"envelope_hash": r.str("envelope_hash")}
}

// FM-1: a setting under dual control changes only when another member who holds the approval right approves the
// envelope its requester asked for; both pass step-up; the requester never approves their own; the change runs in
// the approving transaction and its audit event names the approval.
// req: FM-1
// req: FR-APR-03
// req: FR-APR-04
// req: FR-APR-10
// req: compliance/singapore-statutory Electronic signatures on approvals
// asvs: V8.2.2, V8.3.1
func TestTwoPersonRuleForASetting(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	path := "/v1/organisation/settings/bank.tolerance_base"

	// Asking needs step-up; with it the answer is 202 with the request, and the setting is unchanged.
	stale := e.principal(authn.Principal{Subject: "founder-old", UserID: uuid.MustParse(f.ahkow), Client: "web", Organisation: e.books.Org,
		Roles: []authz.Role{authz.Controller}, StepUpAt: e.clock.Now().Add(-6 * time.Minute)})
	e.problemIs(e.call(http.MethodPatch, path, stale, map[string]any{"value": "5.00"}, "If-Match", `"0"`), "updateSetting", 403, CodeStepUpRequired)
	r := e.call(http.MethodPatch, path, ahkow, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if r.Status != 202 || r.str("status") != "pending" || r.str("requested_by") != f.ahkow || r.str("requested_by_name") != "Ah Kow" ||
		len(r.str("envelope_hash")) != 64 || r.Body["you_may_approve"] != false || r.str("expires_at") != "2026-10-22T09:00:00Z" ||
		!strings.Contains(r.Raw, `"diff":[{"field":"bank.tolerance_base","before":"1.00","after":"5.00"}`) || r.Header.Get("ETag") != "" {
		t.Fatalf("asking for a change under dual control: %d %s", r.Status, r.Raw)
	}
	sum := sha256.Sum256([]byte(r.str("envelope")))
	if hex.EncodeToString(sum[:]) != r.str("envelope_hash") || !strings.Contains(r.str("envelope"), `"dependencies":{"scheduled":[],"setting_version":0,"value_in_effect":"1.00"}`) ||
		!strings.Contains(r.str("envelope"), `"amounts":[{"amount":"5.00","currency":"SGD"}]`) {
		t.Fatalf("the envelope and its hash: %s", r.Raw)
	}
	id := r.str("id")
	if g := e.call(http.MethodGet, path, ahkow, nil); g.Body["version"] != float64(0) || g.str("value") != "1.00" {
		t.Fatalf("the setting changed before its approval: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, path, ahkow, map[string]any{"value": "6.00"}, "If-Match", `"0"`), "updateSetting", 409, approval.CodePending)

	// The requester never approves their own; nobody without the approval right, without step-up, or with another
	// envelope's hash signs; the AI and an Employee never reach it.
	e.problemIs(f.decide(ahkow, id, "approve", hashOf(r)), "approveApproval", 403, approval.CodeOwnRequest)
	e.problemIs(f.decide(f.as(f.weiling, authz.Controller, authz.Accountant), id, "approve", hashOf(r)), "approveApproval", 403, CodeForbidden)
	e.problemIs(f.decide(e.as(authz.Owner), id, "approve", hashOf(r)), "approveApproval", 403, CodeStepUpRequired)
	e.problemIs(f.decide(weiling, id, "approve", map[string]any{"envelope_hash": strings.Repeat("ab", 32)}), "approveApproval", 409, approval.CodeChanged)
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+id+"/approve", weiling, hashOf(r)), "approveApproval", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+id+"/approve", weiling, hashOf(r), "If-Match", `"2"`), "approveApproval", 412, CodeStale)
	e.problemIs(e.call(http.MethodGet, "/v1/approvals/"+id, e.as(authz.Employee), nil), "getApproval", 403, CodeForbidden)
	ai := e.principal(authn.Principal{Client: "agent", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}})
	e.problemIs(f.decide(ai, id, "approve", hashOf(r)), "approveApproval", 403, CodeForbidden)
	if g := e.call(http.MethodGet, "/v1/approvals/"+id, weiling, nil); g.str("status") != "pending" || g.Body["you_may_approve"] != true ||
		g.str("envelope_hash") != r.str("envelope_hash") || g.Header.Get("ETag") != `"1"` {
		t.Fatalf("the request after the refusals: %s", g.Raw)
	}

	// The other founder approves: the signature, then the change, in one transaction.
	a := f.decide(weiling, id, "approve", hashOf(r))
	if a.Status != 200 || a.str("status") != "approved" || a.Body["you_may_approve"] != false ||
		!strings.Contains(a.Raw, `"signer":"`+f.weiling+`","signer_name":"Wei Ling","decision":"approve","method":"session_step_up"`) {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, path, ahkow, nil); g.Body["version"] != float64(1) || g.str("value") != "5.00" {
		t.Fatalf("the approved change: %s", g.Raw)
	}
	e.problemIs(f.decide(weiling, id, "approve", hashOf(r)), "approveApproval", 409, approval.CodeNotPending)

	// The trail has the request, the signature and the change, which names the approval (AUD-01).
	trail := auditTrail(t, e)
	for _, want := range []string{`"action":"approval.requested"`, `"action":"approval.given"`, `"action":"approval.approved"`,
		`"approval_ids":["` + id + `"],"action":"organisation_setting.changed"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}
}

// A request dies when what it depends on changes: the database voids it when its record gets a new version, and
// a dependency that changes without a new version (a role that expires) makes the rebuilt envelope differ, which
// blocks the change and voids the request. A rejection needs its reason; after 7 days a request expires.
// req: FR-APR-05
// req: FR-APR-10
// req: FM-1
func TestApprovalsBlockOnChangeAndExpire(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)

	// The record changes after the request: the database voids it, audited, and approving it is refused.
	r := e.call(http.MethodPatch, "/v1/organisation/settings/retention.records_years", ahkow, map[string]any{"value": "8"}, "If-Match", `"0"`)
	if r.Status != 202 {
		t.Fatalf("asking: %d %s", r.Status, r.Raw)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "retention.records_years", "7", "9", porttest.Day(10, 15), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), weiling, nil); g.str("status") != "void" || g.str("status_reason") == "" {
		t.Fatalf("after its record changed: %s", g.Raw)
	}
	e.problemIs(f.decide(weiling, r.str("id"), "approve", hashOf(r)), "approveApproval", 409, approval.CodeChanged)
	if !strings.Contains(auditTrail(t, e), `"action":"approval.void"`) {
		t.Error("the void is not in the trail")
	}

	// A role that expires changes the member's live roles with no new version: the rebuilt envelope differs.
	e.clock.Set(time.Date(2098, 12, 31, 9, 0, 0, 0, time.UTC))
	ahkow = f.as(f.ahkow)
	r = e.call(http.MethodPost, "/v1/members/"+f.third+"/roles", ahkow, map[string]any{"role": "auditor"})
	if r.Status != 202 || !strings.Contains(r.Raw, `"before":"tax_agent","after":"tax_agent, auditor"`) {
		t.Fatalf("asking for a role: %d %s", r.Status, r.Raw)
	}
	e.clock.Set(time.Date(2099, 1, 1, 9, 0, 0, 0, time.UTC)) // the third member's tax_agent role has expired
	weiling = f.as(f.weiling)
	e.problemIs(f.decide(weiling, r.str("id"), "approve", hashOf(r)), "approveApproval", 409, approval.CodeChanged)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), weiling, nil); g.str("status") != "void" {
		t.Fatalf("after the envelope differed: %s", g.Raw)
	}
	if strings.Contains(e.call(http.MethodGet, "/v1/members", weiling, nil).Raw, `"auditor"`) {
		t.Fatal("a blocked role change was made")
	}

	// A rejection needs its reason, and the change never happens.
	ahkow = f.as(f.ahkow)
	r = e.call(http.MethodPatch, "/v1/organisation/settings/features.ai_assistant", ahkow, map[string]any{"value": "true"}, "If-Match", `"0"`)
	e.problemIs(f.decide(weiling, r.str("id"), "reject", hashOf(r)), "rejectApproval", 422, CodeInvalid)
	x := f.decide(weiling, r.str("id"), "reject", map[string]any{"envelope_hash": r.str("envelope_hash"), "reason": "Not before the DPIA"})
	if x.Status != 200 || x.str("status") != "rejected" || x.str("status_reason") != "Not before the DPIA" ||
		!strings.Contains(x.Raw, `"decision":"reject"`) {
		t.Fatalf("rejecting: %d %s", x.Status, x.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/organisation/settings/features.ai_assistant", ahkow, nil); g.str("value") != "false" {
		t.Fatalf("a rejected change was made: %s", g.Raw)
	}

	// After 7 days a request expires and takes no signature; a new one may then be asked for.
	r = e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_cap_month", ahkow, map[string]any{"value": "300.00"}, "If-Match", `"0"`)
	e.clock.Advance(approval.TTL)
	weiling, ahkow = f.as(f.weiling), f.as(f.ahkow)
	e.problemIs(f.decide(weiling, r.str("id"), "approve", hashOf(r)), "approveApproval", 409, approval.CodeExpired)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), weiling, nil); g.str("status") != "expired" {
		t.Fatalf("after 7 days: %s", g.Raw)
	}
	if again := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_cap_month", ahkow, map[string]any{"value": "300.00"}, "If-Match", `"0"`); again.Status != 202 {
		t.Fatalf("asking again after the expiry: %d %s", again.Status, again.Raw)
	}

	// The lists: pending by default, every status on request, and only what the caller may see.
	l := e.call(http.MethodGet, "/v1/approvals?status=all", weiling, nil)
	if l.Status != 200 || len(l.Body["items"].([]any)) != 5 {
		t.Fatalf("every request: %d %s", l.Status, l.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/approvals", weiling, nil); len(p.Body["items"].([]any)) != 1 {
		t.Fatalf("the pending requests: %s", p.Raw)
	}
	if b := e.call(http.MethodGet, "/v1/approvals?status=all", e.as(authz.Bookkeeper), nil); b.Status != 200 ||
		len(b.Body["items"].([]any)) != 4 || strings.Contains(b.Raw, `"object_type":"membership"`) {
		t.Fatalf("a Bookkeeper sees the settings' requests only: %s", b.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/approvals", e.as(authz.Employee), nil), "listApprovals", 403, CodeForbidden)
}

// After the founder bootstrap, role changes and invitations wait for another Owner: approved, the change is made
// in the approving transaction, and an invitation's code works only once it is approved.
// req: FR-ADM-01
// req: FM-1
func TestRoleChangesAfterTheBootstrapWaitForApproval(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)

	r := e.call(http.MethodPost, "/v1/members/"+f.third+"/roles", ahkow, map[string]any{"role": "accountant"}, "Idempotency-Key", "assign-1")
	if r.Status != 202 || r.str("operation") != "role.assign" {
		t.Fatalf("asking for a role: %d %s", r.Status, r.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/members/"+f.third+"/roles", ahkow, map[string]any{"role": "accountant"}, "Idempotency-Key", "assign-1"); again.Status != 202 ||
		again.Header.Get("Idempotent-Replayed") != "true" || again.str("id") != r.str("id") {
		t.Fatalf("the same request again: %d %s", again.Status, again.Raw)
	}
	if a := f.decide(weiling, r.str("id"), "approve", hashOf(r)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the role: %d %s", a.Status, a.Raw)
	}
	if m := e.call(http.MethodGet, "/v1/members", weiling, nil); !strings.Contains(m.Raw, `"display_name":"Third","roles":[{"role":"accountant"}`) {
		t.Fatalf("the approved role: %s", m.Raw)
	}

	rv := e.call(http.MethodDelete, "/v1/members/"+f.third+"/roles/accountant", weiling, nil)
	if rv.Status != 202 || rv.str("operation") != "role.revoke" {
		t.Fatalf("asking to end a role: %d %s", rv.Status, rv.Raw)
	}
	if a := f.decide(ahkow, rv.str("id"), "approve", hashOf(rv)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the end of a role: %d %s", a.Status, a.Raw)
	}
	if m := e.call(http.MethodGet, "/v1/members", weiling, nil); strings.Contains(m.Raw, `"accountant"`) {
		t.Fatalf("the ended role: %s", m.Raw)
	}

	inv := e.call(http.MethodPost, "/v1/members/invitations", ahkow, map[string]any{"display_name": "Auditor", "roles": []string{"auditor"}})
	code := inv.str("invitation_code")
	if inv.Status != 202 || len(code) < 20 || inv.str("operation") != "member_invitation.create" {
		t.Fatalf("asking to invite: %d %s", inv.Status, inv.Raw)
	}
	join := func() response {
		return e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "auditor", Client: "web"}),
			map[string]any{"invitation_code": code, "display_name": "Auditor"})
	}
	e.problemIs(join(), "acceptMemberInvitation", 409, CodeCodeRefused)
	if a := f.decide(weiling, inv.str("id"), "approve", hashOf(inv)); a.Status != 200 || strings.Contains(a.Raw, code) {
		t.Fatalf("approving the invitation: %d %s", a.Status, a.Raw)
	}
	if j := join(); j.Status != 201 || !strings.Contains(j.Raw, `"roles":["auditor"]`) {
		t.Fatalf("joining with the approved invitation: %d %s", j.Status, j.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/approvals/"+inv.str("id"), weiling, nil); strings.Contains(g.Raw, code) {
		t.Error("the invitation's code is shown again")
	}
	trail := auditTrail(t, e)
	if strings.Contains(trail, code) || strings.Contains(trail, strings.ReplaceAll(code, "-", "")) {
		t.Error("an invitation code reached the trail (SEC-15)")
	}
}

// Every approval policy names an Appendix D row the API grants from, and declares its dependencies.
// req: FR-APR-01
func TestApprovalPoliciesNameAppendixDRows(t *testing.T) {
	for typ, p := range approval.Policies {
		for _, s := range p.Steps {
			if _, ok := authz.Grants[authz.Capability(s.Row)]; !ok {
				t.Errorf("%s: the step's row %q is not a capability authz grants", typ, s.Row)
			}
		}
		if _, ok := approval.Dependencies[typ]; !ok {
			t.Errorf("%s declares no dependencies", typ)
		}
	}
	for _, d := range settings.Catalogue {
		if d.DualControl && d.Fixed {
			t.Errorf("%s is both set by the platform and under dual control", d.Key)
		}
	}
}

// auditTrail is the organisation's trail, read as the read-only role.
func auditTrail(t *testing.T, e *env) string {
	t.Helper()
	reader, err := postgres.Open(context.Background(), db.URL("fyniq_readonly"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var trail strings.Builder
	if err := reader.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return trail.String()
}
