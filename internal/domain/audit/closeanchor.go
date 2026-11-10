package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Close anchors (AUD-04, PRD-001 rev 0.234; p1-audit-explorer design D4): every close's snapshot, and every reopening's
// snapshot after, is signed with the anchors' key and written once to the retention-locked bucket, with the ledger
// revision it was taken at and the audit chain's head it recorded. The hourly anchoring job writes each one not yet
// written, so a close is anchored within the hour; verification checks each against the stored snapshot and the chain.

// CloseAnchorSchema names a close anchor's format.
const CloseAnchorSchema = "fyniq-close-anchor@1"

// ActionCloseAnchored is the event of a close anchor written.
const ActionCloseAnchored = "audit.close_anchored"

// CloseAnchor is a signed statement of a close's or a reopening's snapshot.
type CloseAnchor struct {
	Schema         string    `json:"schema"`
	OrganisationID uuid.UUID `json:"organisation_id"`
	Kind           string    `json:"kind"` // close or reopen
	RecordID       uuid.UUID `json:"record_id"`
	PeriodID       uuid.UUID `json:"period_id"`
	Revision       int64     `json:"ledger_revision"`
	SnapshotHash   string    `json:"snapshot_sha256"`
	ChainSeq       int64     `json:"chain_seq"`  // the chain's head the snapshot recorded; 0 for a version 1 snapshot
	ChainHash      string    `json:"chain_hash"` // its hash, in hexadecimal
	AnchoredAt     string    `json:"anchored_at"`
	KeyID          string    `json:"key_id"`
}

// SignedCloseAnchor is a close anchor with its signature, as written to the bucket.
type SignedCloseAnchor struct {
	Anchor    CloseAnchor `json:"anchor"`
	Signature []byte      `json:"signature"`
}

// CloseAnchorPrefix lists an organisation's close anchors; CloseAnchorKey is where one is written, once.
func CloseAnchorPrefix(org uuid.UUID) string { return fmt.Sprintf("audit-close-anchors/%s/", org) }

func CloseAnchorKey(org uuid.UUID, kind string, record uuid.UUID) string {
	return fmt.Sprintf("%s%s-%s.json", CloseAnchorPrefix(org), kind, record)
}

// snapshotChain is the chain's head a version 2 snapshot recorded.
func snapshotChain(snapshot []byte) (int64, string) {
	var s struct {
		Chain struct {
			Seq  int64  `json:"seq"`
			Hash string `json:"hash"`
		} `json:"chain"`
	}
	_ = json.Unmarshal(snapshot, &s) // a version 1 snapshot has none: zero
	return s.Chain.Seq, s.Chain.Hash
}

// anchorCloses writes the close anchors of org's closes and reopenings not yet anchored, each with its event, and
// returns how many it wrote.
func anchorCloses(ctx context.Context, u storage.UnitOfWork, blobs ports.BlobStore, signer ports.Signer, org uuid.UUID, now time.Time) (int, error) {
	snaps, err := u.Closes().Snapshots(ctx)
	if err != nil || len(snaps) == 0 {
		return 0, err
	}
	have, err := blobs.List(ctx, CloseAnchorPrefix(org))
	if err != nil {
		return 0, err
	}
	done := map[string]bool{}
	for _, k := range have {
		done[k] = true
	}
	written := 0
	for _, s := range snaps {
		key := CloseAnchorKey(org, s.Kind, s.ID)
		if done[key] {
			continue
		}
		seq, chain := snapshotChain(s.Snapshot)
		a := CloseAnchor{Schema: CloseAnchorSchema, OrganisationID: org, Kind: s.Kind, RecordID: s.ID, PeriodID: s.PeriodID, Revision: s.Revision,
			SnapshotHash: hex.EncodeToString(s.Hash), ChainSeq: seq, ChainHash: chain, AnchoredAt: now.UTC().Format(time.RFC3339Nano), KeyID: signer.KeyID()}
		msg, err := json.Marshal(a)
		if err != nil {
			return written, err
		}
		sig, err := signer.Sign(ctx, msg)
		if err != nil {
			return written, err
		}
		b, err := json.Marshal(SignedCloseAnchor{Anchor: a, Signature: sig})
		if err != nil {
			return written, err
		}
		if err := blobs.Create(ctx, key, bytes.NewReader(b)); errors.Is(err, ports.ErrExists) {
			continue
		} else if err != nil {
			return written, err
		}
		written++
		if err := u.Audit().Record(ctx, storage.AuditRecord{Action: ActionCloseAnchored, ObjectType: "period", ObjectID: s.PeriodID,
			Changes: map[string][2]any{"kind": {nil, s.Kind}, "record_id": {nil, s.ID.String()}, "ledger_revision": {nil, s.Revision},
				"snapshot_sha256": {nil, a.SnapshotHash}, "key_id": {nil, a.KeyID}}}); err != nil {
			return written, err
		}
	}
	return written, nil
}

// checkCloses checks org's close anchors (AUD-04): each signature, the stored snapshot it names and that snapshot's own
// hash, and the chain hash at the sequence the snapshot recorded. It returns how many it checked and the first problem.
func checkCloses(ctx context.Context, u storage.UnitOfWork, blobs ports.BlobStore, signer ports.Signer, org uuid.UUID,
	hashes map[int64][]byte, in func(chainSeq int64) bool) (int, string, error) {
	keys, err := blobs.List(ctx, CloseAnchorPrefix(org))
	if err != nil || len(keys) == 0 {
		return 0, "", err
	}
	snaps, err := u.Closes().Snapshots(ctx)
	if err != nil {
		return 0, "", err
	}
	byKey := map[string]storage.CloseSnapshot{}
	for _, s := range snaps {
		byKey[CloseAnchorKey(org, s.Kind, s.ID)] = s
	}
	checked := 0
	for _, k := range keys {
		var sa SignedCloseAnchor
		if err := readJSON(ctx, blobs, k, &sa); err != nil {
			return checked, fmt.Sprintf("close anchor %s cannot be read: %v", k, err), nil
		}
		if in != nil && !in(sa.Anchor.ChainSeq) {
			continue
		}
		i := checked
		checked++
		msg, err := json.Marshal(sa.Anchor)
		if err != nil {
			return i, "", err
		}
		if err := signer.Verify(sa.Anchor.KeyID, msg, sa.Signature); err != nil || sa.Anchor.OrganisationID != org {
			return i, fmt.Sprintf("close anchor %s is not signed for this organisation", k), nil
		}
		s, ok := byKey[k]
		if !ok {
			return i, fmt.Sprintf("the %s anchored at %s is missing from the books", sa.Anchor.Kind, k), nil
		}
		sum := sha256.Sum256(s.Snapshot)
		if !bytes.Equal(sum[:], s.Hash) || hex.EncodeToString(s.Hash) != sa.Anchor.SnapshotHash || s.Revision != sa.Anchor.Revision {
			return i, fmt.Sprintf("the %s of period %s differs from its anchor", s.Kind, s.PeriodID), nil
		}
		if sa.Anchor.ChainSeq > 0 {
			if h, ok := hashes[sa.Anchor.ChainSeq]; ok && hex.EncodeToString(h) != sa.Anchor.ChainHash {
				return i, fmt.Sprintf("the %s of period %s recorded chain event %d, which differs now", s.Kind, s.PeriodID, sa.Anchor.ChainSeq), nil
			}
		}
	}
	return checked, "", nil
}

// readJSON reads the blob at key into v.
func readJSON(ctx context.Context, blobs ports.BlobStore, key string, v any) error {
	rc, err := blobs.Get(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
