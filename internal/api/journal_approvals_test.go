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
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// standardMode switches e's organisation to Standard mode as its members would (rev 0.42): an Administrator asks and
// an Owner approves, neither holding a conflict. The tests of reversals use it: in Founder mode voiding a posted journal
// needs a second person (FM-1).
func standardMode(e *env) {
	e.t.Helper()
	owner := e.as(authz.Owner)
	users := map[string]string{}
	codes := map[string]string{}
	for name, role := range map[string]string{"Ada": "administrator", "Bo": "owner"} {
		codes[name] = e.call(http.MethodPost, "/v1/members/invitations", owner, map[string]any{"display_name": name, "roles": []string{role}}).str("code")
	}
	for name, code := range codes {
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: name, Client: "web"}),
			map[string]any{"invitation_code": code, "display_name": name})
		if j.Status != 201 {
			e.t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		users[name] = j.str("user_id")
	}
	as := func(name string, role authz.Role) string {
		return e.principal(authn.Principal{Subject: "member-" + name, UserID: uuid.MustParse(users[name]), Client: "web", Class: authn.ClassUser,
			Organisation: e.books.Org, Roles: []authz.Role{role}, StepUpAt: e.clock.Now()})
	}
	r := e.call(http.MethodPatch, "/v1/team-mode", as("Ada", authz.Administrator), map[string]any{"mode": "standard"}, "If-Match", `"0"`)
	if r.Status != 202 {
		e.t.Fatalf("asking for Standard mode: %d %s", r.Status, r.Raw)
	}
	if a := e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/approve", as("Bo", authz.Owner), hashOf(r), "If-Match", `"1"`); a.Status != 200 {
		e.t.Fatalf("approving Standard mode: %d %s", a.Status, a.Raw)
	}
}

// inStandardMode is e with a second organisation of its database, switched to Standard mode once: the same server and
// sessions, its own books. The matrix's organisation stays in Founder mode, where a void is asked for instead (FM-1).
func inStandardMode(e *env) *env {
	if e.standard == nil {
		s := *e
		s.books = porttest.NewBooks(e.t, e.store, "Standard "+uuid.NewString()[:8]+" Pte Ltd")
		standardMode(&s)
		e.standard = &s
	}
	return e.standard
}

// A journal whose debits are above the self-approval threshold (S$2,000 by default, a dual-controlled setting) posts
// only on another person's approval, and whoever prepared it never approves it; at or below the threshold a
// Founder-mode post is logged as self-approved for the weekly review. In Founder mode a void needs a second person
// too: it is asked for, then approved.
// req: FR-GL-01
// req: FM-1
// req: FM-2
// req: FM-3
// req: SOD-3
func TestJournalApprovals(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	draft := func(who, amount string) (string, string) {
		r := e.call(http.MethodPost, "/v1/journals", who, e.draft(amount))
		if r.Status != http.StatusCreated {
			t.Fatalf("a draft of %s: %d %s", amount, r.Status, r.Raw)
		}
		return r.str("id"), r.Header.Get("ETag")
	}

	// At the threshold: Ah Kow posts it alone, and the review log has it for Wei Ling to review.
	id, tag := draft(ahkow, "2000.00")
	r := e.call(http.MethodPost, "/v1/journals/"+id+"/post", ahkow, nil, "If-Match", tag)
	if r.Status != http.StatusOK {
		t.Fatalf("posting S$2,000.00 alone: %d %s", r.Status, r.Raw)
	}
	l := e.call(http.MethodGet, "/v1/review-log", weiling, nil)
	if l.Status != 200 || !strings.Contains(l.Raw, `"kind":"self_approved","object_type":"journal","object_key":"`+id+`"`) ||
		!strings.Contains(l.Raw, `alone, at or below the self-approval threshold`) {
		t.Fatalf("the self-approval is not in the review log: %d %s", l.Status, l.Raw)
	}

	// Above it: refused, and asked for instead.
	id, tag = draft(ahkow, "2000.01")
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", ahkow, nil, "If-Match", tag), "postJournal", 409, CodeNeedsApproval)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", e.as(authz.Accountant), nil, "If-Match", tag), "requestJournalPost", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", ahkow, nil), "requestJournalPost", 428, CodeIfMatchRequired)
	asked := e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", ahkow, nil, "If-Match", tag)
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectJournal || asked.str("operation") != approval.OpJournalPost ||
		asked.str("policy") != "founder/journal@2026-10-21.1" || asked.str("summary") != "Post the draft journal (October rent), debits 2000.01" ||
		!strings.Contains(asked.str("envelope"), `"debits":"2000.01"`) || !strings.Contains(asked.str("envelope"), `"status":"draft"`) {
		t.Fatalf("asking to post: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", ahkow, nil, "If-Match", tag), "requestJournalPost", 409, approval.CodePending)
	if g := e.call(http.MethodGet, "/v1/journals/"+id, weiling, nil); g.str("approval_asked") != asked.str("id") {
		t.Errorf("the journal does not name the request that waits: %s", g.Raw)
	}
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)

	// SOD-3: Wei Ling prepared this one and Ah Kow asks; she still may not approve it.
	own, ownTag := draft(weiling, "5000.00")
	mine := e.call(http.MethodPost, "/v1/journals/"+own+"/post-approval", ahkow, nil, "If-Match", ownTag)
	if mine.Status != 202 {
		t.Fatalf("asking to post Wei Ling's journal: %d %s", mine.Status, mine.Raw)
	}
	e.problemIs(f.decide(weiling, mine.str("id"), "approve", hashOf(mine)), "approveApproval", 403, CodeOwnJournal)

	// Wei Ling approves Ah Kow's: it posts, as her.
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the post: %d %s", a.Status, a.Raw)
	}
	g := e.call(http.MethodGet, "/v1/journals/"+id, weiling, nil)
	if g.str("status") != "posted" || g.str("posted_by", "id") != f.weiling || g.Body["approval_asked"] != nil {
		t.Fatalf("after the approval: %s", g.Raw)
	}

	// A change to the draft voids the request that held its version.
	e.call(http.MethodPost, "/v1/approvals/"+mine.str("id")+"/withdraw", ahkow, nil, "If-Match", `"1"`)
	other, otherTag := draft(ahkow, "3000.00")
	pending := e.call(http.MethodPost, "/v1/journals/"+other+"/post-approval", ahkow, nil, "If-Match", otherTag)
	if pending.Status != 202 {
		t.Fatalf("asking to post a third: %d %s", pending.Status, pending.Raw)
	}
	changed := e.draft("3500.00")
	if r := e.call(http.MethodPut, "/v1/journals/"+other, ahkow, changed, "If-Match", otherTag); r.Status != 200 {
		t.Fatalf("changing the draft: %d %s", r.Status, r.Raw)
	}
	e.problemIs(f.decide(weiling, pending.str("id"), "approve", hashOf(pending)), "approveApproval", 409, approval.CodeChanged)
	if g := e.call(http.MethodGet, "/v1/journals/"+other, ahkow, nil); g.str("status") != "draft" {
		t.Fatalf("the changed draft posted: %s", g.Raw)
	}

	// Founder mode: a void is refused, then asked for and approved.
	reversal := map[string]any{"date": "2026-10-25", "reason": "Posted to the wrong account"}
	posted := e.call(http.MethodGet, "/v1/journals/"+id, ahkow, nil).Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", ahkow, reversal, "If-Match", posted), "reverseJournal", 409, CodeVoidApproval)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+other+"/reverse-approval", ahkow, reversal, "If-Match", "*"), "requestJournalReversal", 409, journal.CodeNotPosted)
	void := e.call(http.MethodPost, "/v1/journals/"+id+"/reverse-approval", ahkow, reversal, "If-Match", posted)
	if void.Status != 202 || void.str("operation") != approval.OpJournalReverse || !strings.Contains(void.str("summary"), "on 2026-10-25: Posted to the wrong account") {
		t.Fatalf("asking to reverse: %d %s", void.Status, void.Raw)
	}
	if a := f.decide(weiling, void.str("id"), "approve", hashOf(void)); a.Status != 200 {
		t.Fatalf("approving the reversal: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/journals/"+id, ahkow, nil); g.str("status") != "reversed" {
		t.Fatalf("after the reversal's approval: %s", g.Raw)
	}
	trail := auditTrail(t, e)
	for _, want := range []string{`"approval_ids":["` + asked.str("id") + `"],"action":"document.posted"`,
		`"approval_ids":["` + void.str("id") + `"],"action":"document.reversed"`, `"kind":[null,"self_approved"]`} {
		if !strings.Contains(trail, want) {
			t.Errorf("the audit trail lacks %s", want)
		}
	}
}

// The threshold holds in Standard mode too; at or below it nothing is logged for review, since only Founder mode
// self-approves (FM-2), and a person who holds P reverses a posted journal directly.
// req: FM-2
// req: FR-GL-12
func TestJournalThresholdInStandardMode(t *testing.T) {
	e := newEnv(t)
	standardMode(e)
	accountant := e.as(authz.Accountant)
	id, tag := e.createDraft("2000.01")
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag), "postJournal", 409, CodeNeedsApproval)
	id, tag = e.postedJournal("2000.00")
	if l := e.call(http.MethodGet, "/v1/review-log", e.as(authz.Owner), nil); strings.Contains(l.Raw, `"self_approved"`) {
		t.Errorf("a post in Standard mode was logged as self-approved: %s", l.Raw)
	}
	r := e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", accountant, map[string]any{"date": "2026-10-21", "reason": "Duplicate"}, "If-Match", tag)
	if r.Status != http.StatusOK {
		t.Fatalf("a reversal in Standard mode: %d %s", r.Status, r.Raw)
	}
}
