//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The controls hold together in one company's books (FR-CTL-04; PRD-001 §6, FR-GL-07, FR-GL-08). In Founder mode a
// journal above the self-approval threshold posts only on the other founder's approval, never its requester's, and a
// founder whose roles conflict still posts, as Founder mode allows. October closes on two people's acts: the one who ran
// its checks never attests them. Closed, it takes no posting, by hand or by an approval that was waiting when it closed,
// and it reopens only on another person's approval. Then the company switches to Standard mode, asked by one founder
// as Administrator and approved by the other as Owner:
// the founder whose roles conflict, with no reason accepted, no longer posts (SOD-6), and one whose roles do not conflict
// still posts at or below the threshold.
// req: FR-CTL-04
// req: controls/checks Period close and lock follow FR-GL-07/08
// req: controls/checks Approvals, SoD and team mode follow §6
// req: FR-GL-07
// req: FR-GL-08
// req: FM-1
// req: FM-2
// req: FM-4
// req: SOD-6
func TestTheControlsHoldTogether(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Accountant, authz.Controller), f.as(f.weiling, authz.Accountant, authz.Controller)
	both := f.as(f.ahkow, authz.Administrator, authz.Accountant) // SOD-6: administers users and posts
	draft := func(who, amount, day string) (string, string) {
		t.Helper()
		d := e.draft(amount)
		d["date"] = day
		r := e.call(http.MethodPost, "/v1/journals", who, d)
		if r.Status != http.StatusCreated {
			t.Fatalf("a draft of %s on %s: %d %s", amount, day, r.Status, r.Raw)
		}
		return r.str("id"), r.Header.Get("ETag")
	}
	post := func(who, id, tag string) response {
		return e.call(http.MethodPost, "/v1/journals/"+id+"/post", who, nil, "If-Match", tag)
	}
	status := func(id string) string { return e.call(http.MethodGet, "/v1/journals/"+id, weiling, nil).str("status") }

	// Approvals (FM-1, FM-2): above the threshold a journal waits for the other founder; its requester never approves it.
	id, tag := draft(ahkow, "2500.00", "2026-10-20")
	e.problemIs(post(ahkow, id, tag), "postJournal", 409, CodeNeedsApproval)
	asked := e.call(http.MethodPost, "/v1/journals/"+id+"/post-approval", ahkow, nil, "If-Match", tag)
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking to post: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != http.StatusOK || status(id) != "posted" {
		t.Fatalf("Wei Ling approving: %d %s", a.Status, a.Raw)
	}
	// Founder mode allows a conflict of roles, with its compensating controls.
	if id, tag := draft(both, "100.00", "2026-10-21"); post(both, id, tag).Status != http.StatusOK {
		t.Fatalf("Founder mode refused a founder whose roles conflict")
	}

	// The close (FR-GL-07, FM-4): two acts by two people.
	waitingID, waitingTag := draft(ahkow, "3000.00", "2026-10-22")
	waiting := e.call(http.MethodPost, "/v1/journals/"+waitingID+"/post-approval", ahkow, nil, "If-Match", waitingTag)
	if waiting.Status != http.StatusAccepted {
		t.Fatalf("asking to post the third: %d %s", waiting.Status, waiting.Raw)
	}
	oct := e.books.Oct.ID
	periodTag := func() string {
		return e.call(http.MethodGet, "/v1/periods/"+oct.String()+"/close", ahkow, nil).Header.Get("ETag")
	}
	checklistDone(t, e.store, e.books.Org, oct)
	checks := e.call(http.MethodPost, "/v1/periods/"+oct.String()+"/close-checks", ahkow, nil, "If-Match", periodTag())
	if checks.Status != http.StatusOK {
		t.Fatalf("running the close checks: %d %s", checks.Status, checks.Raw)
	}
	attest := map[string]any{"checks_id": checks.str("checks", "id"), "statement": "Reviewed."}
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+oct.String()+"/close", ahkow, attest, "If-Match", periodTag()), "closePeriod", 403, closing.CodeSelfAttest)
	if c := e.call(http.MethodPost, "/v1/periods/"+oct.String()+"/close", weiling, attest, "If-Match", periodTag()); c.Status != http.StatusOK {
		t.Fatalf("Wei Ling attesting: %d %s", c.Status, c.Raw)
	}

	// The lock (FR-GL-08, INV-03): nothing posts into October now, by hand or by the approval that waited.
	small, smallTag := draft(ahkow, "100.00", "2026-10-25")
	e.problemIs(post(ahkow, small, smallTag), "postJournal", 409, storage.CodePeriodClosed)
	e.problemIs(f.decide(weiling, waiting.str("id"), "approve", hashOf(waiting)), "approveApproval", 409, storage.CodePeriodClosed)
	if status(small) != "draft" {
		t.Errorf("the journal refused is %s", status(small))
	}
	if status(waitingID) != "draft" {
		t.Errorf("the journal that waited is %s", status(waitingID))
	}
	// It reopens only on another person's approval (FM-1).
	reopen := e.call(http.MethodPost, "/v1/periods/"+oct.String()+"/reopen", ahkow, map[string]any{"reason": "A late supplier invoice"}, "If-Match", periodTag())
	if reopen.Status != http.StatusAccepted {
		t.Fatalf("asking to reopen: %d %s", reopen.Status, reopen.Raw)
	}
	e.problemIs(f.decide(ahkow, reopen.str("id"), "approve", hashOf(reopen)), "approveApproval", 403, approval.CodeOwnRequest)

	// Standard mode (§6.4, SOD-6): the conflict no longer posts; roles that do not conflict still do, at the threshold.
	sw := e.call(http.MethodPatch, "/v1/team-mode", f.as(f.ahkow, authz.Administrator), map[string]any{"mode": "standard"}, "If-Match", `"0"`)
	if sw.Status != http.StatusAccepted {
		t.Fatalf("asking for Standard mode: %d %s", sw.Status, sw.Raw)
	}
	e.problemIs(f.decide(f.as(f.ahkow), sw.str("id"), "approve", hashOf(sw)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(f.as(f.weiling), sw.str("id"), "approve", hashOf(sw)); a.Status != http.StatusOK {
		t.Fatalf("Wei Ling approving Standard mode: %d %s", a.Status, a.Raw)
	}
	if m := e.call(http.MethodGet, "/v1/team-mode", e.as(authz.Owner), nil); m.str("mode") != "standard" {
		t.Fatalf("the team mode: %s", m.Raw)
	}
	id, tag = draft(both, "100.00", "2026-11-05")
	e.problemIs(post(both, id, tag), "postJournal", 403, team.CodeAction)
	if r := post(weiling, id, tag); r.Status != http.StatusOK {
		t.Errorf("Wei Ling posting in Standard mode: %d %s", r.Status, r.Raw)
	}
}
