package compliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// proposalsApproved (CTL-AI-02, AI-P5, AI-10; p1-ai-proposals-core design D12): every AI proposal executed in the window
// ran under a person's approval of exactly what it proposed. Its own approval request is approved and its envelope
// signs the proposal's payload hash. What was approved hashes as approved. A person's live approval signed the
// envelope, and the person it ran as is among them. No approval of it was given by the AI or a service (target 0).
func proposalsApproved(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	rows, err := u.Compliance().ExecutedProposals(ctx, since)
	if err != nil {
		return nil, "", err
	}
	return proposalFindings(rows), fmt.Sprintf("%d proposal(s) of the AI executed", len(rows)), nil
}

// proposalFindings are the executed proposals that ran without a person's approval of what they proposed.
func proposalFindings(rows []storage.ExecutedProposal) []Finding {
	var out []Finding
	for _, p := range rows {
		if why := unapproved(p); why != "" {
			out = append(out, Finding{Kind: "ai_proposal", ID: p.ID.String(), At: day(p.FinishedAt), Detail: why})
		}
	}
	return out
}

func unapproved(p storage.ExecutedProposal) string {
	var envelope struct {
		Change struct {
			PayloadSHA256 string `json:"payload_sha256"`
		} `json:"change"`
	}
	sum := sha256.Sum256([]byte(p.ApprovedPayload))
	switch {
	case p.RequestID == nil:
		return "Executed with no approval request"
	case p.RequestObject != "ai_proposal:"+p.ID.String():
		return "Executed under the approval request of another record"
	case p.RequestStatus != "approved":
		return "Executed while its approval request is " + p.RequestStatus
	case json.Unmarshal([]byte(p.Envelope), &envelope) != nil || envelope.Change.PayloadSHA256 != p.PayloadSHA256:
		return "Its approval request does not sign the proposal's payload hash"
	case p.ApprovedSHA256 == "" || hex.EncodeToString(sum[:]) != p.ApprovedSHA256:
		return "What was approved does not hash as approved"
	case len(p.Signers) == 0:
		return "Executed with no person's approval of its request"
	case p.DecidedBy == nil || !slices.Contains(p.Signers, *p.DecidedBy):
		return "Executed as someone who did not approve it"
	case p.NotByAPerson > 0:
		return "An approval of it was given by the AI or a service, not a person"
	}
	return ""
}
