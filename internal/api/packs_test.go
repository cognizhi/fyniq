//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// The loaded pack versions are read with their sources and pinned tests; activating one waits for a second person
// who holds A on "Statutory parameters", never the proposer, and the pinned tests run again before it is recorded.
// req: ENG-30
// req: ENG-32
// req: SOD-10
// req: FR-ORG-05
// req: FR-ADM-04
func TestPackActivationWaitsForASecondApprover(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ann, ben := f.as(f.ann), f.as(f.ben)
	const path = "/v1/statutory-packs/sg-gst@2026.1"

	l := e.call(http.MethodGet, "/v1/statutory-packs", ben, nil)
	if l.Status != 200 || !strings.Contains(l.Raw, `"id":"sg-gst@2026.1","pack":"sg-gst"`) || strings.Contains(l.Raw, `"activation"`) ||
		!strings.Contains(l.Raw, `"readable":true`) {
		t.Fatalf("the loaded versions: %d %s", l.Status, l.Raw)
	}
	g := e.call(http.MethodGet, path, ben, nil)
	if g.Status != 200 || g.Body["pinned_total"] == float64(0) || g.Body["pinned_passed"] != g.Body["pinned_total"] ||
		len(g.Body["sources"].([]any)) == 0 || strings.Contains(g.Raw, `"pass":false`) {
		t.Fatalf("the version: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/statutory-packs/sg-gst@1999.1", ben, nil), "getStatutoryPack", 404, CodeNotFound)

	// Only C on the row asks: not a Bookkeeper, not an Owner alone; and a person who passed step-up.
	e.problemIs(e.call(http.MethodPost, path+"/activate", f.as(f.ann, authz.Bookkeeper), nil, "If-Match", `"0"`), "activateStatutoryPack", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, path+"/activate", f.as(f.ann, authz.Owner), nil, "If-Match", `"0"`), "activateStatutoryPack", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, path+"/activate", e.as(authz.Controller), nil, "If-Match", `"0"`), "activateStatutoryPack", 403, CodeStepUpRequired)

	if g.Header.Get("ETag") != `"0"` {
		t.Fatalf("the version's ETag before activation: %q", g.Header.Get("ETag"))
	}
	e.problemIs(e.call(http.MethodPost, path+"/activate", ann, nil), "activateStatutoryPack", 428, CodeIfMatchRequired)
	r := e.call(http.MethodPost, path+"/activate", ann, nil, "Idempotency-Key", "activate-1", "If-Match", `"0"`)
	if r.Status != 202 || r.str("object_type") != "statutory_pack" || r.str("object_key") != "sg-gst@2026.1" ||
		r.str("operation") != "statutory_pack.activate" || r.str("policy") != "founder/statutory_pack@2026-10-20.1" ||
		!strings.Contains(r.str("envelope"), `"activated":false,"digest":"`+g.str("digest")+`"`) ||
		!strings.Contains(r.Raw, `{"field":"activated","before":"no","after":"yes"}`) {
		t.Fatalf("asking to activate: %d %s", r.Status, r.Raw)
	}
	if again := e.call(http.MethodPost, path+"/activate", ann, nil, "Idempotency-Key", "activate-1", "If-Match", `"0"`); again.Status != 202 || again.str("id") != r.str("id") {
		t.Fatalf("the same request again: %d %s", again.Status, again.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path+"/activate", ann, nil, "If-Match", `"0"`), "activateStatutoryPack", 409, approval.CodePending)
	e.problemIs(f.decide(ann, r, "approve"), "approveApproval", 403, approval.CodeOwnRequest)
	if g := e.call(http.MethodGet, path, ben, nil); strings.Contains(g.Raw, `"activation"`) {
		t.Fatalf("activated before its approval: %s", g.Raw)
	}

	// Another founder approves: the activation names both and the request, and it is audited with the approval.
	if a := f.decide(ben, r, "approve"); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving the activation: %d %s", a.Status, a.Raw)
	}
	g = e.call(http.MethodGet, path, ann, nil)
	if !strings.Contains(g.Raw, `"activation":{"proposed_by":"`+f.ann+`","approved_by":"`+f.ben+`","approval_id":"`+r.str("id")+`"`) {
		t.Fatalf("the activated version: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path+"/activate", ann, nil, "If-Match", `"0"`), "activateStatutoryPack", 412, CodeStale)
	e.problemIs(e.call(http.MethodPost, path+"/activate", ann, nil, "If-Match", `"1"`), "activateStatutoryPack", 409, pack.CodeActive)
	if trail := auditTrail(t, e); !strings.Contains(trail, `"approval_ids":["`+r.str("id")+`"],"action":"statutory_pack.activated"`) {
		t.Error("the activation is not in the trail with its approval")
	}
}
