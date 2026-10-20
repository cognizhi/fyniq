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

// FM-3: every high-risk action that executes is logged, the founder bootstrap's changes and each approved change, and
// another founder reviews it, never the one who acted, once; in Founder mode an entry unreviewed 14 days after it
// executed is overdue. The AI never sees the log.
// req: FM-3
// req: FM-1
// req: FR-ADM-01
func TestTheReviewLog(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben, cat := f.as(f.ann), f.as(f.ben), f.as(f.cat)
	list := func(who, status string) response {
		r := e.call(http.MethodGet, "/v1/review-log?status="+status, who, nil)
		if r.Status != 200 {
			t.Fatalf("the review log: %d %s", r.Status, r.Raw)
		}
		return r
	}
	review := func(who string, note string, ids ...string) response {
		return e.call(http.MethodPost, "/v1/review-log/reviews", who, map[string]any{"entry_ids": ids, "note": note})
	}

	// The founder bootstrap made three invitations alone.
	l := list(ann, "unreviewed")
	if items := l.Body["items"].([]any); len(items) != 3 || l.Body["unreviewed"] != float64(3) || l.Body["overdue"] != float64(0) ||
		!strings.Contains(l.Raw, `"kind":"founder_bootstrap","object_type":"member_invitation"`) || !strings.Contains(l.Raw, `"summary":"Invite Ann as owner, controller, administrator"`) {
		t.Fatalf("the bootstrap's entries: %s", l.Raw)
	}

	// Ben asks for a setting, Cat approves it: an approved change, Ben's.
	r := e.call(http.MethodPatch, "/v1/organisation/settings/bank.tolerance_base", ben, map[string]any{"value": "5.00"}, "If-Match", `"0"`)
	if a := f.decide(cat, r, "approve"); a.Status != 200 {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	l = list(ann, "unreviewed")
	var changeID string
	for _, it := range l.Body["items"].([]any) {
		if m := it.(map[string]any); m["kind"] == "approved_change" {
			changeID = m["id"].(string)
			if m["actor"] != f.ben || m["approval_id"] != r.str("id") || m["you_may_review"] != true || m["object_key"] != "bank.tolerance_base" {
				t.Fatalf("the approved change's entry: %v", m)
			}
		}
	}
	if changeID == "" {
		t.Fatalf("no approved change in the log: %s", l.Raw)
	}

	// Ben does not review his own; Ann does, once; nothing is recorded when one entry fails.
	e.problemIs(review(ben, "", changeID), "reviewLogEntries", 403, approval.CodeReviewOwn)
	e.problemIs(review(ann, "", changeID, uuid.NewString()), "reviewLogEntries", 404, CodeNotFound)
	if p := e.call(http.MethodPost, "/v1/review-log/reviews", ann, map[string]any{"entry_ids": []string{changeID}}, "Prefer", "return=preview"); p.Status != 200 {
		t.Fatalf("a preview: %d %s", p.Status, p.Raw)
	}
	ok := review(ann, "  Agreed at Monday's meeting  ", changeID)
	if ok.Status != 200 || !strings.Contains(ok.Raw, `"reviewed_by":"`+f.ann+`"`) || !strings.Contains(ok.Raw, `"note":"Agreed at Monday's meeting"`) {
		t.Fatalf("Ann's review: %d %s", ok.Status, ok.Raw)
	}
	e.problemIs(review(cat, "", changeID), "reviewLogEntries", 409, approval.CodeReviewed)
	if l := list(ann, "reviewed"); len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("the reviewed entries: %s", l.Raw)
	}

	// Fourteen days on, the bootstrap's entries are overdue in Founder mode.
	e.clock.Advance(approval.ReviewOverdue + 1)
	if l := list(ann, "all"); l.Body["overdue"] != float64(3) || l.Body["unreviewed"] != float64(3) || len(l.Body["items"].([]any)) != 4 {
		t.Fatalf("the log after 14 days: %s", l.Raw)
	}

	// The AI never sees it; an Employee holds no right on what the entries changed.
	ai := e.principal(authnAgent(e))
	e.problemIs(e.call(http.MethodGet, "/v1/review-log", ai, nil), "listReviewLog", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/review-log", e.as(authz.Employee), nil), "listReviewLog", 403, CodeForbidden)
	trail := auditTrail(t, e)
	for _, want := range []string{`"action":"review_log.logged"`, `"action":"review_log.reviewed"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}
}

// authnAgent is the AI acting for an Owner of e's organisation.
func authnAgent(e *env) authn.Principal {
	return authn.Principal{Client: "assistant", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}}
}
