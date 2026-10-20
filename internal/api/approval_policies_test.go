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
)

// trio is an organisation past the founder bootstrap with three founders, each Owner, Controller and Administrator.
type trio struct {
	e                    *env
	ann, ben, cat        string // their user IDs
	annMember, catMember string // Ann's and Cat's membership IDs
}

func newTrio(t *testing.T) trio {
	e := newEnv(t)
	owner := e.as(authz.Owner)
	// Every invitation is made during the founder bootstrap, so each keeps its roles when accepted after.
	invite := func(name string) string {
		return e.call(http.MethodPost, "/v1/members/invitations", owner, map[string]any{"display_name": name,
			"roles": []string{"owner", "controller", "administrator"}}).str("code")
	}
	join := func(name, code string) response {
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: name, Client: "web"}),
			map[string]any{"invitation_code": code, "display_name": name})
		if j.Status != 201 {
			t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		return j
	}
	a, b, c := invite("Ann"), invite("Ben"), invite("Cat")
	ann, ben, cat := join("Ann", a), join("Ben", b), join("Cat", c)
	return trio{e: e, ann: ann.str("user_id"), annMember: ann.str("member_id"), ben: ben.str("user_id"), cat: cat.str("user_id"),
		catMember: cat.str("member_id")}
}

// as is a founder's session, stepped up now, with the roles they hold.
func (f trio) as(user string, roles ...authz.Role) string {
	if len(roles) == 0 {
		roles = []authz.Role{authz.Owner, authz.Controller, authz.Administrator}
	}
	return f.e.principal(authn.Principal{Subject: "founder-" + user, UserID: uuid.MustParse(user), Client: "web", Class: authn.ClassUser,
		Organisation: f.e.books.Org, Roles: roles, StepUpAt: f.e.clock.Now()})
}

func (f trio) decide(who string, r response, how string) response {
	return f.e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/"+how, who, hashOf(r), "If-Match", `"1"`)
}

// A policy change is itself an approval request (SOD-8): the Administrator asks, another member who holds A on
// "Approval policies, AI catalogue" approves, and the version is added then and kept; every pending request of that
// kind restarts. The policy for approval policies is fixed, and a policy whose steps the members cannot sign, or that
// names a role or member who never approves, is refused.
// req: FR-APR-01
// req: FR-APR-09
// req: FR-APR-10
// req: SOD-8
func TestApprovalPolicyChangesAreApprovedAndKept(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben := f.as(f.ann), f.as(f.ben)
	const path = "/v1/approval-policies/organisation_setting"

	l := e.call(http.MethodGet, "/v1/approval-policies", ben, nil)
	if l.Status != 200 || len(l.Body["items"].([]any)) != 6 ||
		!strings.Contains(l.Raw, `{"object_type":"approval_policy","row":"Approval policies, AI catalogue","version":"founder/approval_policy@2026-10-20.1","number":0,"built_in":true,"fixed":true,"steps":[{"quorum":1,"roles":[],"users":[]}]}`) {
		t.Fatalf("the policies in force: %d %s", l.Status, l.Raw)
	}
	if g := e.call(http.MethodGet, path, ben, nil); g.Status != 200 || g.Header.Get("ETag") != `"0"` || g.Body["built_in"] != true ||
		!strings.Contains(g.Raw, `"history":[]`) {
		t.Fatalf("the built-in settings policy: %d %s", g.Status, g.Raw)
	}

	// The refusals: no C on the row, the fixed policy, steps nobody can sign, a stale ETag.
	change := func(who string, steps []map[string]any, etag string) response {
		return e.call(http.MethodPatch, path, who, map[string]any{"steps": steps}, "If-Match", etag)
	}
	e.problemIs(change(f.as(f.ann, authz.Owner, authz.Controller), []map[string]any{{"quorum": 1}}, `"0"`), "changeApprovalPolicy", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPatch, "/v1/approval-policies/approval_policy", ann, map[string]any{"steps": []map[string]any{{"quorum": 2}}},
		"If-Match", `"0"`), "changeApprovalPolicy", 409, approval.CodeFixed)
	e.problemIs(change(ann, []map[string]any{{"quorum": 1, "roles": []string{"bookkeeper"}}}, `"0"`), "changeApprovalPolicy", 422, CodeInvalid)
	e.problemIs(change(ann, []map[string]any{{"quorum": 4, "roles": []string{"owner"}}}, `"0"`), "changeApprovalPolicy", 422, CodeInvalid)
	e.problemIs(change(ann, []map[string]any{{"quorum": 1, "users": []string{uuid.NewString()}}}, `"0"`), "changeApprovalPolicy", 422, CodeInvalid)
	e.problemIs(change(ann, []map[string]any{{"quorum": 1}}, `"3"`), "changeApprovalPolicy", 412, CodeStale)

	// A settings change waits under the built-in policy.
	waiting := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", ben, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if waiting.Status != 202 || waiting.str("policy") != "founder/organisation_setting@2026-10-20.1" ||
		!strings.Contains(waiting.Raw, `"steps":[{"quorum":1,"roles":[],"users":[],"approvals":0}],"current_step":1`) {
		t.Fatalf("a settings change: %d %s", waiting.Status, waiting.Raw)
	}

	// Ann asks for two steps: one of Ann herself, then one of Cat.
	steps := []map[string]any{{"quorum": 1, "users": []string{f.ann}}, {"quorum": 1, "users": []string{f.cat}}}
	r := change(ann, steps, `"0"`)
	if r.Status != 202 || r.str("object_type") != "approval_policy" || r.str("object_key") != "organisation_setting" ||
		r.str("operation") != "approval_policy.change" || r.str("policy") != "founder/approval_policy@2026-10-20.1" ||
		!strings.Contains(r.str("envelope"), `"dependencies":{"policy_version":"founder/organisation_setting@2026-10-20.1"}`) ||
		!strings.Contains(r.Raw, `"diff":[{"field":"steps","before":"step 1: 1 of anyone who holds the approval right","after":"step 1: 1 of members `+f.ann+`; step 2: 1 of members `+f.cat+`"}]`) {
		t.Fatalf("asking for a policy: %d %s", r.Status, r.Raw)
	}
	e.problemIs(change(ann, steps, `"0"`), "changeApprovalPolicy", 409, approval.CodePending)
	e.problemIs(f.decide(ann, r, "approve"), "approveApproval", 403, approval.CodeOwnRequest)
	if g := e.call(http.MethodGet, path, ben, nil); g.Body["built_in"] != true {
		t.Fatalf("the policy changed before its approval: %s", g.Raw)
	}

	// Ben approves: the version is added, kept, and the waiting settings change restarts.
	if a := f.decide(ben, r, "approve"); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the policy: %d %s", a.Status, a.Raw)
	}
	g := e.call(http.MethodGet, path, ben, nil)
	if g.Header.Get("ETag") != `"1"` || g.str("version") != "organisation_setting/v1" || g.Body["built_in"] != false ||
		!strings.Contains(g.Raw, `"steps":[{"quorum":1,"roles":[],"users":["`+f.ann+`"]},{"quorum":1,"roles":[],"users":["`+f.cat+`"]}]`) ||
		!strings.Contains(g.Raw, `"request_id":"`+r.str("id")+`","changed_by":"`+f.ann+`"`) {
		t.Fatalf("the approved policy: %s", g.Raw)
	}
	if w := e.call(http.MethodGet, "/v1/approvals/"+waiting.str("id"), ben, nil); w.str("status") != "void" ||
		w.str("status_reason") != "the approval policy changed after the request" {
		t.Fatalf("the settings change waiting when the policy changed: %s", w.Raw)
	}
	e.problemIs(f.decide(ann, waiting, "approve"), "approveApproval", 409, approval.CodeChanged)
	trail := auditTrail(t, e)
	for _, want := range []string{`"approval_ids":["` + r.str("id") + `"],"action":"approval_policy.changed"`, `"action":"approval.void"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}

	// The members change before a policy change executes: steps nobody can sign any more are never added.
	r = change(ann, []map[string]any{{"quorum": 1, "users": []string{f.cat}}}, `"1"`)
	if r.Status != 202 {
		t.Fatalf("asking for a policy of Cat: %d %s", r.Status, r.Raw)
	}
	rv := e.call(http.MethodDelete, "/v1/members/"+f.catMember+"/roles/owner", ben, nil)
	if a := f.decide(ann, rv, "approve"); a.Status != 200 {
		t.Fatalf("ending Cat's owner role: %d %s", a.Status, a.Raw)
	}
	e.problemIs(f.decide(ben, r, "approve"), "approveApproval", 422, approval.CodeSteps)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), ben, nil); g.str("status") != "void" ||
		g.str("status_reason") != "the policy's steps can no longer be signed" {
		t.Fatalf("a policy change whose steps can no longer be signed: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, path, ben, nil); g.Header.Get("ETag") != `"1"` {
		t.Fatalf("a policy nobody can sign was added: %s", g.Raw)
	}
}

// A request under a policy with steps is signed step by step, in order, each by someone the step names, and a person
// signs it once; the change executes on the last step, after every earlier approver is checked to hold their right
// still. Its requester may withdraw it, which voids it and its signatures.
// req: FR-APR-01
// req: FR-APR-10
// req: FM-1
func TestStepsAreSignedInOrderAndRechecked(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben, cat := f.as(f.ann), f.as(f.ben), f.as(f.cat)
	p := e.call(http.MethodPatch, "/v1/approval-policies/organisation_setting", ann, map[string]any{"steps": []map[string]any{
		{"quorum": 1, "users": []string{f.ann}}, {"quorum": 1, "users": []string{f.cat}}}}, "If-Match", `"0"`)
	if a := f.decide(ben, p, "approve"); a.Status != 200 {
		t.Fatalf("approving the policy: %d %s", a.Status, a.Raw)
	}
	path := "/v1/organisation/settings/bank.tolerance_base"
	ask := func(value, etag string) response {
		r := e.call(http.MethodPatch, path, ben, map[string]any{"value": value}, "If-Match", etag)
		if r.Status != 202 || r.str("policy") != "organisation_setting/v1" {
			t.Fatalf("asking for %s: %d %s", value, r.Status, r.Raw)
		}
		return r
	}

	// Step 1 is Ann's, step 2 Cat's.
	r := ask("5.00", `"0"`)
	e.problemIs(f.decide(cat, r, "approve"), "approveApproval", 403, approval.CodeNotYourStep)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), cat, nil); g.Body["you_may_approve"] != false || g.Body["current_step"] != float64(1) {
		t.Fatalf("the request before step 1: %s", g.Raw)
	}
	one := f.decide(ann, r, "approve")
	if one.Status != 200 || one.str("status") != "pending" || one.Body["current_step"] != float64(2) ||
		!strings.Contains(one.Raw, `"approvals":1},{"quorum":1,"roles":[],"users":["`+f.cat+`"],"approvals":0}]`) {
		t.Fatalf("signing step 1: %d %s", one.Status, one.Raw)
	}
	if g := e.call(http.MethodGet, path, ben, nil); g.str("value") != "1.00" {
		t.Fatalf("the setting changed after step 1: %s", g.Raw)
	}
	e.problemIs(f.decide(ann, r, "approve"), "approveApproval", 403, approval.CodeNotYourStep)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), cat, nil); g.Body["you_may_approve"] != true {
		t.Fatalf("the request before step 2: %s", g.Raw)
	}
	if two := f.decide(cat, r, "approve"); two.Status != 200 || two.str("status") != "approved" || two.Body["current_step"] != float64(0) {
		t.Fatalf("signing step 2: %d %s", two.Status, two.Raw)
	}
	if g := e.call(http.MethodGet, path, ben, nil); g.str("value") != "5.00" {
		t.Fatalf("the approved setting: %s", g.Raw)
	}

	// The requester withdraws a request with a signature: it is void, and so is the signature.
	r = ask("6.00", `"1"`)
	if a := f.decide(ann, r, "approve"); a.Status != 200 {
		t.Fatalf("signing step 1: %d %s", a.Status, a.Raw)
	}
	withdraw := func(who, etag string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", who, nil, "If-Match", etag)
	}
	e.problemIs(withdraw(cat, `"1"`), "withdrawApproval", 403, approval.CodeNotYours)
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", ben, nil), "withdrawApproval", 428, CodeIfMatchRequired)
	w := withdraw(ben, `"1"`)
	if w.Status != 200 || w.str("status") != "void" || w.str("status_reason") != "withdrawn by its requester" || !strings.Contains(w.Raw, `"voided":true`) {
		t.Fatalf("withdrawing: %d %s", w.Status, w.Raw)
	}
	e.problemIs(withdraw(ben, `"2"`), "withdrawApproval", 409, approval.CodeChanged)
	e.problemIs(f.decide(cat, r, "approve"), "approveApproval", 409, approval.CodeChanged)

	// An earlier approver who loses their right before the last step: nothing executes, and the request is void.
	r = ask("7.00", `"1"`)
	if a := f.decide(ann, r, "approve"); a.Status != 200 {
		t.Fatalf("signing step 1: %d %s", a.Status, a.Raw)
	}
	rv := e.call(http.MethodDelete, "/v1/members/"+f.annMember+"/roles/owner", ben, nil)
	if a := f.decide(cat, rv, "approve"); a.Status != 200 {
		t.Fatalf("ending Ann's owner role: %d %s", a.Status, a.Raw)
	}
	e.problemIs(f.decide(cat, r, "approve"), "approveApproval", 409, approval.CodeAuthority)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), ben, nil); g.str("status") != "void" ||
		g.str("status_reason") != "an earlier approver no longer holds the right their step needs" {
		t.Fatalf("after the earlier approver lost their right: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, path, ben, nil); g.str("value") != "5.00" {
		t.Fatalf("a blocked change was made: %s", g.Raw)
	}
}

// matrixApprover makes the matrix's one member (matrixMember) an Owner as well, so the members can sign a policy's
// steps, and returns their user ID. The organisation keeps one member, so the founder bootstrap the other rows rely
// on holds.
func matrixApprover(e *env) string {
	id := matrixMember(e)
	l := e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil)
	m := l.Body["items"].([]any)[0].(map[string]any)
	if !strings.Contains(l.Raw, `"owner"`) {
		if r := e.call(http.MethodPost, "/v1/members/"+id+"/roles", e.as(authz.Administrator), map[string]any{"role": "owner"}); r.Status != 201 {
			e.t.Fatalf("making the matrix member an Owner: %d %s", r.Status, r.Raw)
		}
	}
	return m["user_id"].(string)
}

// steppedUp is tok's principal as the matrix's member, a person with a Fyniq user who passed step-up now, so a call
// the role is allowed reaches its approval request. Other principals (the AI, the Super admin) are left as they are.
func steppedUp(e *env, tok string) string {
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	if p.Class != authn.ClassUser || p.Organisation != e.books.Org {
		return tok
	}
	p.UserID, p.StepUpAt = uuid.MustParse(matrixApprover(e)), e.clock.Now()
	return e.principal(p)
}

// policyRequests counts the organisation's requests to change an approval policy.
func policyRequests(e *env) int {
	n := 0
	items, _ := e.call(http.MethodGet, "/v1/approvals?status=all&page_size=500", e.as(authz.Owner), nil).Body["items"].([]any)
	for _, it := range items {
		if it.(map[string]any)["object_type"] == "approval_policy" {
			n++
		}
	}
	return n
}

// packRequests counts the organisation's requests to activate a statutory pack version.
func packRequests(e *env) int {
	n := 0
	items, _ := e.call(http.MethodGet, "/v1/approvals?status=all&page_size=500", e.as(authz.Owner), nil).Body["items"].([]any)
	for _, it := range items {
		if it.(map[string]any)["object_type"] == "statutory_pack" {
			n++
		}
	}
	return n
}
