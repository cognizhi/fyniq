//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/approvalremind"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/outbox"
)

// A delegation of approval authority (FR-APR-06): Ann, who a policy's step names, delegates to Cat for some days; Cat, who holds the approval right herself, signs
// Ann's step as herself, and the signature says on whose behalf. A delegation lends no right and no more than its days: not before it starts, not after it ends,
// not for a step it does not name Ann for; Ann cannot sign again once Cat signed for her.
// req: FR-APR-06
// req: FM-1
// asvs: V8.2.2, V8.3.1
func TestADelegateSignsTheStepOfTheDelegatorForItsDaysAndNoMore(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben, cat := f.as(f.ann), f.as(f.ben), f.as(f.cat)
	p := e.call(http.MethodPatch, "/v1/approval-policies/organisation_setting", ann, map[string]any{"steps": []map[string]any{{"quorum": 1, "users": []string{f.ann}}}}, "If-Match", `"0"`)
	if a := f.decide(ben, p, "approve"); a.Status != 200 {
		t.Fatalf("approving the policy: %d %s", a.Status, a.Raw)
	}
	path := "/v1/organisation/settings/bank.tolerance_base"
	ask := func(value, etag string) response {
		r := e.call(http.MethodPatch, path, ben, map[string]any{"value": value}, "If-Match", etag)
		if r.Status != 202 {
			t.Fatalf("asking for %s: %d %s", value, r.Status, r.Raw)
		}
		return r
	}
	today := e.clock.Now().Format("2006-01-02")
	day := func(n int) string { return e.clock.Now().AddDate(0, 0, n).Format("2006-01-02") }
	delegate := func(who string, to string, from, until string) response {
		return e.call(http.MethodPost, "/v1/approval-delegations", who,
			map[string]any{"delegate": to, "starts_on": from, "ends_on": until, "reason": "Annual leave"}, "Idempotency-Key", uuid.NewString())
	}

	// Without a delegation Cat is not the one the step names.
	r := ask("5.00", `"0"`)
	e.problemIs(f.decide(cat, r, "approve"), "approveApproval", 403, approval.CodeNotYourStep)

	// What a delegation refuses: oneself, a stranger, days the wrong way round or too many, the past, a stale step-up, and anyone but a person.
	e.problemIs(delegate(ann, f.ann, today, day(3)), "createApprovalDelegation", 422, approval.CodeDelegation)
	e.problemIs(delegate(ann, uuid.NewString(), today, day(3)), "createApprovalDelegation", 422, approval.CodeDelegation)
	e.problemIs(delegate(ann, f.cat, day(3), today), "createApprovalDelegation", 422, approval.CodeDelegation)
	e.problemIs(delegate(ann, f.cat, today, day(91)), "createApprovalDelegation", 422, approval.CodeDelegation)
	e.problemIs(delegate(ann, f.cat, day(-5), day(-2)), "createApprovalDelegation", 422, approval.CodeDelegation)
	stale := e.principal(authn.Principal{Subject: "founder-old", UserID: uuid.MustParse(f.ann), Client: "web", Organisation: e.books.Org,
		Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now().Add(-6 * time.Minute)})
	e.problemIs(delegate(stale, f.cat, today, day(3)), "createApprovalDelegation", 403, CodeStepUpRequired)
	ai := e.principal(authn.Principal{Client: "agent", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}})
	e.problemIs(delegate(ai, f.cat, today, day(3)), "createApprovalDelegation", 403, CodeForbidden)
	e.problemIs(delegate(e.as(authz.Employee), f.cat, today, day(3)), "createApprovalDelegation", 403, CodeForbidden)

	// A delegation for later days lends nothing today.
	later := delegate(ann, f.cat, day(2), day(4))
	if later.Status != 201 || later.Body["in_force"] != false || later.str("delegator") != f.ann || later.str("delegate") != f.cat {
		t.Fatalf("a delegation for later days: %d %s", later.Status, later.Raw)
	}
	e.problemIs(f.decide(cat, r, "approve"), "approveApproval", 403, approval.CodeNotYourStep)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), cat, nil); g.Body["you_may_approve"] != false {
		t.Fatalf("a delegation not yet in force: %s", g.Raw)
	}

	// A delegation for today: Cat sees she may sign, signs as herself, and the signature says whose step it was.
	d := delegate(ann, f.cat, today, day(5))
	if d.Status != 201 || d.Body["in_force"] != true || d.str("status") != "active" || d.Header.Get("ETag") != `"1"` || d.str("delegate_name") != "Cat" || d.str("delegator_name") != "Ann" {
		t.Fatalf("delegating: %d %s", d.Status, d.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/approval-delegations", cat, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Fatalf("Cat's delegations: %d %s", l.Status, l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/approval-delegations", ben, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 0 {
		t.Fatalf("a member who is neither delegator nor delegate sees none: %d %s", l.Status, l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/approval-delegations?scope=all", ben, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Fatalf("an Owner lists them all: %d %s", l.Status, l.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), cat, nil); g.Body["you_may_approve"] != true {
		t.Fatalf("the delegate may sign: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), ben, nil); g.Body["you_may_approve"] != false {
		t.Fatalf("the requester may not: %s", g.Raw)
	}
	a := f.decide(cat, r, "approve")
	if a.Status != 200 || a.str("status") != "approved" || !strings.Contains(a.Raw, `"signer":"`+f.cat+`","signer_name":"Cat","decision":"approve","method":"session_step_up"`) ||
		!strings.Contains(a.Raw, `"on_behalf_of":"`+f.ann+`","on_behalf_of_name":"Ann"`) {
		t.Fatalf("signing as the delegate: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, path, ben, nil); g.str("value") != "5.00" {
		t.Fatalf("the approved change: %s", g.Raw)
	}

	// Ann, signed for, signs no more; and the delegator or the delegate ends the delegation at once, after which Cat signs for nobody.
	r = ask("6.00", `"1"`)
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), cat, nil); g.Body["you_may_approve"] != true {
		t.Fatalf("the delegation still holds for the next request: %s", g.Raw)
	}
	end := func(who string, id, etag string) response {
		return e.call(http.MethodPost, "/v1/approval-delegations/"+id+"/end", who, nil, "If-Match", etag, "Idempotency-Key", uuid.NewString())
	}
	e.problemIs(end(ann, d.str("id"), `"7"`), "endApprovalDelegation", 412, CodeStale)
	e.problemIs(end(e.as(authz.Employee), d.str("id"), `"1"`), "endApprovalDelegation", 403, CodeForbidden)
	ended := end(ann, d.str("id"), `"1"`)
	if ended.Status != 200 || ended.str("status") != "ended" || ended.Body["in_force"] != false || ended.Header.Get("ETag") != `"2"` {
		t.Fatalf("ending: %d %s", ended.Status, ended.Raw)
	}
	e.problemIs(end(ann, d.str("id"), `"2"`), "endApprovalDelegation", 409, approval.CodeDelegationNotFor)
	e.problemIs(f.decide(cat, r, "approve"), "approveApproval", 403, approval.CodeNotYourStep)
	if a := f.decide(ann, r, "approve"); a.Status != 200 || strings.Contains(a.Raw, "on_behalf_of") {
		t.Fatalf("Ann signing as herself: %d %s", a.Status, a.Raw)
	}
}

// A delegate stands in for one delegator in a quorum: the delegator whom a delegate signed for cannot sign too, so two are never counted for one person, and
// the delegate keeps to one signature.
// req: FR-APR-06
// asvs: V8.2.2
func TestADelegatorAndTheirDelegateCountOnceInAQuorum(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben, cat := f.as(f.ann), f.as(f.ben), f.as(f.cat)
	p := e.call(http.MethodPatch, "/v1/approval-policies/organisation_setting", ben, map[string]any{"steps": []map[string]any{{"quorum": 2, "users": []string{f.ann, f.cat}}}}, "If-Match", `"0"`)
	if a := f.decide(ann, p, "approve"); a.Status != 200 {
		t.Fatalf("approving the policy: %d %s", a.Status, a.Raw)
	}
	today := e.clock.Now().Format("2006-01-02")
	if d := e.call(http.MethodPost, "/v1/approval-delegations", ann, map[string]any{"delegate": f.ben, "starts_on": today, "ends_on": today, "reason": "Leave"}, "Idempotency-Key", uuid.NewString()); d.Status != 201 {
		t.Fatalf("delegating: %d %s", d.Status, d.Raw)
	}
	// Ben asked for nothing yet: Cat asks, so the requester is Cat, whom Ann's delegate Ben may stand in beside.
	r := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", cat, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if r.Status != 202 {
		t.Fatalf("asking: %d %s", r.Status, r.Raw)
	}
	one := f.decide(ben, r, "approve")
	if one.Status != 200 || one.str("status") != "pending" || !strings.Contains(one.Raw, `"on_behalf_of":"`+f.ann+`"`) {
		t.Fatalf("Ben signing for Ann: %d %s", one.Status, one.Raw)
	}
	e.problemIs(f.decide(ann, r, "approve"), "approveApproval", 403, approval.CodeNotYourStep) // signed for already
	e.problemIs(f.decide(ben, r, "approve"), "approveApproval", 403, approval.CodeNotYourStep) // one signature each
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), ann, nil); g.Body["you_may_approve"] != false {
		t.Fatalf("Ann, signed for, may not sign: %s", g.Raw)
	}
	// Cat is the requester, Ann was signed for and Ben signed: the quorum of two needs another of the named, so it stays pending.
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), ben, nil); g.str("status") != "pending" || g.Body["current_step"] != float64(1) {
		t.Fatalf("one signature for a quorum of two: %s", g.Raw)
	}
}

// matrixApprovalDelegations are the authorisation matrix's rows of the delegation operations.
var matrixApprovalDelegations = map[string]func(e *env, tok string) (response, func() bool){
	"listApprovalDelegations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/approval-delegations", tok, nil), nil
	},
	"createApprovalDelegation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/approval-delegations", steppedUp(e, tok),
			map[string]any{"delegate": uuid.NewString(), "starts_on": "2026-10-15", "ends_on": "2026-10-16", "reason": "Matrix"}, "Idempotency-Key", uuid.NewString()), nil
	},
	"endApprovalDelegation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/approval-delegations/"+uuid.NewString()+"/end", steppedUp(e, tok), nil, "If-Match", `"1"`, "Idempotency-Key", uuid.NewString()), nil
	},
}

// A pending approval is reminded and escalated, and the job only notifies (FR-APR-08): the people who may sign its current step are reminded once the reminder time
// has passed since it was asked for, then since the last reminder, up to three times; the Owners and the requester hear of it once past the escalation time; a run
// that comes again sends nothing twice; the request is neither signed, decided nor given more time; one nobody may sign yet and one expired are not reminded.
// req: FR-APR-08
// req: FR-NTF-01
func TestAPendingApprovalIsRemindedAndEscalatedButNeverDecided(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben, cat := f.as(f.ann), f.as(f.ben), f.as(f.cat)
	r := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", ben, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if r.Status != 202 {
		t.Fatalf("asking: %d %s", r.Status, r.Raw)
	}
	drainWork(t, e)
	worker := workerStore(t)
	runner := approvalremind.Runner{Store: worker, Outbox: outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock}, Clock: e.clock}
	run := func() int {
		t.Helper()
		n, err := runner.RunOrganisation(context.Background(), e.books.Org)
		if err != nil {
			t.Fatal(err)
		}
		fanOutAll(t, e)
		return n
	}
	count := func(who, typ string) int {
		n := 0
		for _, it := range e.call(http.MethodGet, "/v1/notifications?page_size=100", who, nil).Body["items"].([]any) {
			if it.(map[string]any)["type"] == typ {
				n++
			}
		}
		return n
	}
	if n := run(); n != 0 {
		t.Fatalf("nothing is due when it was just asked for: %d", n)
	}
	e.clock.Advance(25 * time.Hour)
	if n := run(); n != 1 || count(ann, "approval.reminder") != 1 || count(cat, "approval.reminder") != 1 || count(ben, "approval.reminder") != 0 {
		t.Fatalf("the first reminder: %d; Ann %d, Cat %d, the requester Ben %d", n, count(ann, "approval.reminder"), count(cat, "approval.reminder"), count(ben, "approval.reminder"))
	}
	if n := run(); n != 0 {
		t.Fatalf("the same hour again: %d", n)
	}
	e.clock.Advance(25 * time.Hour)
	if n := run(); n != 1 || count(ann, "approval.reminder") != 2 {
		t.Fatalf("the second reminder: %d, Ann %d", n, count(ann, "approval.reminder"))
	}
	// 75 hours since it was asked for: escalated to the Owners and the requester, once.
	e.clock.Advance(25 * time.Hour)
	if n := run(); n != 1 || count(ben, "approval.escalated") != 1 || count(ann, "approval.escalated") != 1 || count(cat, "approval.escalated") != 1 {
		t.Fatalf("the escalation: %d; Ben %d, Ann %d", n, count(ben, "approval.escalated"), count(ann, "approval.escalated"))
	}
	e.clock.Advance(30 * time.Hour)
	if n := run(); n != 0 || count(ann, "approval.escalated") != 1 {
		t.Fatalf("nothing is sent twice: %d, escalations %d", n, count(ann, "approval.escalated"))
	}
	// It is neither signed, decided nor given more time.
	if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), ann, nil); g.str("status") != "pending" || g.str("expires_at") != r.str("expires_at") ||
		len(g.Body["signatures"].([]any)) != 0 {
		t.Fatalf("the request after its reminders: %s", g.Raw)
	}
	// Once expired nobody is reminded.
	e.clock.Advance(7 * 24 * time.Hour)
	if n := run(); n != 0 {
		t.Fatalf("an expired request: %d", n)
	}
	// The settings that time them are in the catalogue with their recommendations.
	for key, want := range map[string]string{"approvals.reminder_hours": "24", "approvals.escalate_hours": "72"} {
		if g := e.call(http.MethodGet, "/v1/organisation/settings/"+key, ann, nil); g.Status != 200 || g.str("value") != want {
			t.Errorf("%s: %d %s", key, g.Status, g.Raw)
		}
	}
}

// An approval email links to the request in the web app, which asks for sign-in and returns to it (FR-APR-07): the link carries no token, opening it (a GET, as a mail
// scanner does) decides and changes nothing, and no GET path approves or rejects.
// req: FR-APR-07
// asvs: V8.2.2, V4.2.2
func TestAnApprovalEmailLinksToTheRequestAndOpeningItDecidesNothing(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben := f.as(f.ann), f.as(f.ben)
	r := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", ben, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if r.Status != 202 {
		t.Fatalf("asking: %d %s", r.Status, r.Raw)
	}
	fanOutAll(t, e)
	var link string
	for _, it := range e.call(http.MethodGet, "/v1/notifications", ann, nil).Body["items"].([]any) {
		if n := it.(map[string]any); n["type"] == "approval.requested" {
			link, _ = n["link"].(string)
		}
	}
	if link != "/approvals/"+r.str("id") || strings.Contains(link, "?") || strings.Contains(link, "token") {
		t.Fatalf("the link in the approval notification: %q", link)
	}
	for range 2 { // a scanner opens it twice
		if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id"), ann, nil); g.str("status") != "pending" || g.Header.Get("ETag") != `"1"` || len(g.Body["signatures"].([]any)) != 0 {
			t.Fatalf("opening the request changed it: %s", g.Raw)
		}
	}
	for _, how := range []string{"approve", "reject"} {
		if g := e.call(http.MethodGet, "/v1/approvals/"+r.str("id")+"/"+how, ann, nil); g.Status != http.StatusMethodNotAllowed && g.Status != http.StatusNotFound {
			t.Errorf("a GET of %s answered %d: no GET decides", how, g.Status)
		}
	}
	if g := e.call(http.MethodGet, "/v1/organisation/settings/bank.tolerance_base", ann, nil); g.str("value") != "1.00" {
		t.Fatalf("the setting changed: %s", g.Raw)
	}
}
