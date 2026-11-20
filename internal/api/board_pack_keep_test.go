package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
)

// A published board pack's PDF is kept as evidence on another person's approval (FR-RPT-06, FR-DOC-05, FR-DOC-06; rev 0.607): the Controller asks, with the file attached as generated evidence
// to a document of its own and its hash in the request; the requester cannot sign it, and the Owner does; kept, the file is a final evidence file found under Board packs in the evidence
// search, a second PDF for the pack is refused, a draft has none, and an Accountant, who builds a pack, does not ask.
// req: FR-RPT-06
// req: FR-DOC-06
func TestABoardPacksPDFIsKeptAsEvidenceOnAnotherPersonsApproval(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	kpiBooks(e)
	controller := c.as(c.weiling, authz.Controller) // people with users of their own, who passed step-up now
	owner := c.as(c.ahkow, authz.Owner)
	board := e.person(uuid.New(), authz.BoardViewer)
	made := e.call(http.MethodPost, "/v1/board-packs", controller, map[string]any{"month": "2026-11"})
	id := made.str("id")
	keep := "/v1/board-packs/" + id + "/pdf/keeping"

	// A draft has no PDF to keep.
	e.problemIs(e.call(http.MethodGet, keep, controller, nil), "getBoardPackKeeping", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodPost, keep, controller, nil), "requestBoardPackKeeping", 404, CodeNotFound)
	if r := e.call(http.MethodPost, "/v1/board-packs/"+id+"/publish", controller, nil, "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("publishing = %d: %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, keep, board, nil); r.Status != http.StatusOK || r.str("state") != "not_kept" {
		t.Fatalf("before asking, a Board viewer reads %d: %s", r.Status, r.Raw)
	}
	// An Accountant builds a pack and does not ask to keep it (BPACK-01); a Board viewer reads and does not ask.
	e.problemIs(e.call(http.MethodPost, keep, c.as(c.mei, authz.Accountant), nil), "requestBoardPackKeeping", 403, CodePackRole)
	if r := e.call(http.MethodPost, keep, board, nil); r.Status != http.StatusForbidden {
		t.Errorf("a Board viewer asked to keep a PDF: %d %s", r.Status, r.Raw)
	}

	pdf := e.call(http.MethodGet, "/v1/board-packs/"+id+"/pdf", controller, nil)
	asked := e.call(http.MethodPost, keep, controller, nil)
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectBoardPackPDF || asked.str("operation") != approval.OpBoardPackPDFKeep ||
		!strings.Contains(asked.Raw, pdf.str("sha256")) {
		t.Fatalf("asking to keep it: %d %s", asked.Status, asked.Raw)
	}
	waiting := e.call(http.MethodGet, keep, board, nil)
	if waiting.str("state") != "waiting" || waiting.str("sha256") != pdf.str("sha256") || waiting.str("file_state") != "attached" || waiting.str("file_name") != "board-pack-2026-11-v1.pdf" {
		t.Errorf("waiting: %s", waiting.Raw)
	}
	// The requester never signs it; a request waiting is not asked twice.
	if r := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", controller, hashOf(asked), "If-Match", `"1"`); r.Status == http.StatusOK {
		t.Errorf("the requester approved their own request: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, keep, controller, nil); r.Status != http.StatusConflict || !strings.Contains(r.Raw, approval.CodePending) {
		t.Errorf("asking again while waiting: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", owner, hashOf(asked), "If-Match", `"1"`); r.Status != http.StatusOK || r.str("status") != "approved" {
		t.Fatalf("the Owner approving: %d %s", r.Status, r.Raw)
	}
	kept := e.call(http.MethodGet, keep, board, nil)
	if kept.str("state") != "kept" || kept.str("sha256") != pdf.str("sha256") || kept.str("kept_at") == "" || kept.str("evidence_id") == "" {
		t.Errorf("kept: %s", kept.Raw)
	}
	// Found in the evidence search under Board packs, by those who view the Reports row, and not by a Board viewer, whose right is the pack's alone.
	found := e.call(http.MethodGet, "/v1/evidence?activity=board_pack", controller, nil)
	if found.Status != http.StatusOK || !strings.Contains(found.Raw, "board pack PDF") || !strings.Contains(found.Raw, kept.str("evidence_id")) {
		t.Errorf("the evidence search: %d %.400s", found.Status, found.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/evidence?activity=board_pack", board, nil); strings.Contains(r.Raw, kept.str("evidence_id")) {
		t.Errorf("a Board viewer found the kept file in the evidence search: %.300s", r.Raw)
	}
	// A pack keeps one PDF.
	e.problemIs(e.call(http.MethodPost, keep, controller, nil), "requestBoardPackKeeping", 409, CodePackKept)
	if trail := auditTrail(t, e); !strings.Contains(trail, "board_pack.pdf_kept") || !strings.Contains(trail, pdf.str("sha256")) {
		t.Errorf("keeping the PDF was not audited with its hash")
	}
}
