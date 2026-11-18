//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
)

// G4-057's pack half (FR-APR-10): a GST-bearing document's approval envelope holds the GST pack its codes and rates are
// read from. Activating a version while the approval waits changes it, so the old approval cannot execute: the request
// is void, and asked again it restarts under the version now in force.
// req: G4-057
// req: FR-APR-10
func TestAPackActivatedWhileAnApprovalWaitsRestartsIt(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	r := e.call(http.MethodPost, "/v1/invoices", ahkow, e.invoiceDraft("4", "1000.00"))
	if r.Status != http.StatusCreated {
		t.Fatalf("the draft: %d %s", r.Status, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	asked := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue-approval", ahkow, nil, "If-Match", tag)
	if asked.Status != 202 || !strings.Contains(asked.str("envelope"), `"gst_pack":"activated: none; carried: sg-gst@2026.1`) {
		t.Fatalf("asking to issue: %d %s", asked.Status, asked.Raw)
	}

	// While it waits, the organisation activates sg-gst@2026.1: a different person approves the activation (SOD-10).
	act := e.call(http.MethodPost, "/v1/statutory-packs/sg-gst@2026.1/activate", ahkow, nil, "If-Match", `"0"`)
	if a := f.decide(weiling, act.str("id"), "approve", hashOf(act)); a.Status != http.StatusOK || a.str("status") != "approved" {
		t.Fatalf("activating the pack: %d %s", a.Status, a.Raw)
	}

	// The old approval cannot execute: its envelope names the pack as it was.
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 409, approval.CodeChanged)
	if g := e.call(http.MethodGet, "/v1/approvals/"+asked.str("id"), weiling, nil); g.str("status") != "void" {
		t.Fatalf("the old request: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+id, weiling, nil); g.str("status") != "draft" {
		t.Fatalf("the invoice issued on the old approval: %s", g.Raw)
	}

	// Asked again, it restarts under the version now in force, and that approval executes.
	tag = e.call(http.MethodGet, "/v1/invoices/"+id, ahkow, nil).Header.Get("ETag")
	again := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue-approval", ahkow, nil, "If-Match", tag)
	if again.Status != 202 || !strings.Contains(again.str("envelope"), `"gst_pack":"activated: sg-gst@2026.1; carried: sg-gst@2026.1`) {
		t.Fatalf("asking again: %d %s", again.Status, again.Raw)
	}
	if a := f.decide(weiling, again.str("id"), "approve", hashOf(again)); a.Status != http.StatusOK || a.str("status") != "approved" {
		t.Fatalf("approving again: %d %s", a.Status, a.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/invoices/"+id, weiling, nil); g.str("status") != "posted" {
		t.Fatalf("after the new approval: %s", g.Raw)
	}
}
