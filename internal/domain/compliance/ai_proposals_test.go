package compliance

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The nightly reconciliation of CTL-AI-02 (AI-P5, AI-10; p1-ai-proposals-core design D12): an executed proposal is an
// exception unless its own approval request is approved and signs its payload hash, what was approved hashes as
// approved, a person's live approval signed it, it ran as one of them, and no approval of it was the AI's.
// req: CTL-AI-02
// asvs: V8.2.1, V16.3.3
func TestEveryExecutedProposalHasAPersonsApproval(t *testing.T) {
	approver := uuid.New()
	approved := `{"note":"Book the room"}`
	sum := sha256.Sum256([]byte(approved))
	right := func() storage.ExecutedProposal {
		id, request := uuid.New(), uuid.New()
		hash := "ab" + hex.EncodeToString(sum[1:])
		return storage.ExecutedProposal{ID: id, FinishedAt: time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC), PayloadSHA256: hash,
			ApprovedPayload: approved, ApprovedSHA256: hex.EncodeToString(sum[:]), DecidedBy: &approver, RequestID: &request,
			RequestObject: "ai_proposal:" + id.String(), RequestStatus: "approved", Signers: []uuid.UUID{uuid.New(), approver},
			Envelope: `{"object":{"type":"ai_proposal"},"change":{"payload_sha256":"` + hash + `"}}`}
	}
	someone := uuid.New()
	cases := map[string]func(p *storage.ExecutedProposal){
		"Executed with no approval request":                     func(p *storage.ExecutedProposal) { p.RequestID = nil },
		"Executed under the approval request of another record": func(p *storage.ExecutedProposal) { p.RequestObject = "ai_proposal:" + uuid.NewString() },
		"Executed while its approval request is pending":        func(p *storage.ExecutedProposal) { p.RequestStatus = "pending" },
		"Its approval request does not sign the proposal's payload hash": func(p *storage.ExecutedProposal) {
			p.Envelope = strings.Replace(p.Envelope, p.PayloadSHA256, "cd"+p.PayloadSHA256[2:], 1)
		},
		"What was approved does not hash as approved":                      func(p *storage.ExecutedProposal) { p.ApprovedPayload = `{"note":"Book the hall"}` },
		"Executed with no person's approval of its request":                func(p *storage.ExecutedProposal) { p.Signers = nil },
		"Executed as someone who did not approve it":                       func(p *storage.ExecutedProposal) { p.DecidedBy = &someone },
		"An approval of it was given by the AI or a service, not a person": func(p *storage.ExecutedProposal) { p.NotByAPerson = 1 },
	}
	if found := proposalFindings([]storage.ExecutedProposal{right()}); len(found) != 0 {
		t.Fatalf("a proposal a person approved is an exception: %+v", found)
	}
	for want, breach := range cases {
		p := right()
		breach(&p)
		found := proposalFindings([]storage.ExecutedProposal{right(), p})
		if len(found) != 1 || found[0].Detail != want || found[0].ID != p.ID.String() || found[0].Kind != "ai_proposal" {
			t.Errorf("%s: %+v", want, found)
		}
	}
	unsigned := right()
	unsigned.Envelope = `not json`
	if found := proposalFindings([]storage.ExecutedProposal{unsigned}); len(found) != 1 || found[0].Detail != "Its approval request does not sign the proposal's payload hash" {
		t.Errorf("an envelope that cannot be read: %+v", found)
	}
}
