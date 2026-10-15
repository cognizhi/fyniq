package audit

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// AnchorSchema names the anchor format.
const AnchorSchema = "fyniq-audit-anchor@1"

// Anchor is a signed statement of an organisation's chain head at a moment (§11.5 L3, CTL-AUD-02).
// Written to a retention-locked bucket, it lets verification catch a chain rewritten from the start.
type Anchor struct {
	Schema         string    `json:"schema"`
	OrganisationID uuid.UUID `json:"organisation_id"`
	Seq            int64     `json:"seq"`
	HeadHash       string    `json:"head_hash"` // hex
	AnchoredAt     string    `json:"anchored_at"`
	KeyID          string    `json:"key_id"`
}

// Signed is an anchor with its signature, as written to the bucket.
type Signed struct {
	Anchor    Anchor `json:"anchor"`
	Signature []byte `json:"signature"`
}

// AnchorKey is where an anchor is written: one object per organisation and sequence number.
func AnchorKey(org uuid.UUID, seq int64) string {
	return fmt.Sprintf("audit-anchors/%s/%020d.json", org, seq)
}

// AnchorPrefix lists an organisation's anchors.
func AnchorPrefix(org uuid.UUID) string { return fmt.Sprintf("audit-anchors/%s/", org) }

// Sign builds and signs the anchor of a chain head.
func Sign(ctx context.Context, s ports.Signer, org uuid.UUID, seq int64, head []byte, at time.Time) (Signed, error) {
	a := Anchor{Schema: AnchorSchema, OrganisationID: org, Seq: seq, HeadHash: hex.EncodeToString(head),
		AnchoredAt: at.UTC().Format(time.RFC3339Nano), KeyID: s.KeyID()}
	msg, err := json.Marshal(a)
	if err != nil {
		return Signed{}, err
	}
	sig, err := s.Sign(ctx, msg)
	if err != nil {
		return Signed{}, err
	}
	return Signed{Anchor: a, Signature: sig}, nil
}

// Check verifies an anchor's signature and that the recomputed chain agrees with it.
func (sa Signed) Check(s ports.Signer, org uuid.UUID, hashes map[int64][]byte) error {
	msg, err := json.Marshal(sa.Anchor)
	if err != nil {
		return err
	}
	if err := s.Verify(sa.Anchor.KeyID, msg, sa.Signature); err != nil {
		return fmt.Errorf("anchor at event %d: the signature does not verify: %w", sa.Anchor.Seq, err)
	}
	if sa.Anchor.OrganisationID != org {
		return fmt.Errorf("anchor at event %d belongs to another organisation", sa.Anchor.Seq)
	}
	got, ok := hashes[sa.Anchor.Seq]
	if !ok {
		return fmt.Errorf("anchor at event %d: the chain no longer reaches that event", sa.Anchor.Seq)
	}
	if hex.EncodeToString(got) != sa.Anchor.HeadHash {
		return fmt.Errorf("anchor at event %d: the chain was rewritten since it was anchored on %s", sa.Anchor.Seq, sa.Anchor.AnchoredAt)
	}
	return nil
}
