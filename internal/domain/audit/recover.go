package audit

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Restore behind the anchors (AUD-09, G4-070, G4-071; p1-audit-explorer design D8). A restore from a backup can land
// behind an anchor already published: the chain then continues from the restored head, and its new events take sequence
// numbers the old anchors sign. The recovery step runs before anything else verifies the trail. It finds the restored
// head (the last event whose hash matches every anchor of the current epoch), signs and writes the recovery first, then
// records it in fyniq.audit_recovery and as audit.restored in the chain, which starts the next anchor epoch. It
// re-applies the erasure register, so no detail erased after the backup comes back, and tells the Owner and the DPO.
// Verification then explains the anchored range no longer proven; the check of the audit trail stays failed until the
// Owner acknowledges the recovery.

// RecoverySchema names a recovery's signed form.
const RecoverySchema = "fyniq-audit-recovery@1"

// Runbook is what a recovery points to.
const Runbook = "docs/runbooks/audit-restore.md"

// CodeAcknowledged refuses acknowledging a recovery acknowledged already.
const CodeAcknowledged = "AUDIT-02"

// RecoveryForm is a recovery as it is signed.
type RecoveryForm struct {
	Schema         string    `json:"schema"`
	OrganisationID uuid.UUID `json:"organisation_id"`
	Epoch          int       `json:"epoch"`
	RestoredSeq    int64     `json:"restored_seq"`
	RestoredHash   string    `json:"restored_hash"` // hex
	MissingFrom    int64     `json:"missing_from"`
	MissingTo      int64     `json:"missing_to"`
	DetectedAt     string    `json:"detected_at"`
	Runbook        string    `json:"runbook"`
	KeyID          string    `json:"key_id"`
}

// SignedRecovery is a recovery with its signature, as written to the bucket.
type SignedRecovery struct {
	Recovery  RecoveryForm `json:"recovery"`
	Signature []byte       `json:"signature"`
}

// Tell lets the Owner and the DPO know of a recovery, in its unit of work; nil tells no one.
type Tell func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, r storage.AuditRecovery) error

// EpochAnchorKey is where an anchor of an epoch is written: epoch 0 as before recoveries began, epoch n under e<n>/.
func EpochAnchorKey(org uuid.UUID, epoch int, seq int64) string {
	if epoch == 0 {
		return AnchorKey(org, seq)
	}
	return fmt.Sprintf("%se%d/%020d.json", AnchorPrefix(org), epoch, seq)
}

// RecoveryKey is where the recovery that starts epoch is written, once.
func RecoveryKey(org uuid.UUID, epoch int) string {
	return fmt.Sprintf("%srecovery/%d.json", AnchorPrefix(org), epoch)
}

// anchored is an anchor's key, its event's sequence number and its epoch.
type anchored struct {
	key   string
	seq   int64
	epoch int
}

// epochAnchors are an epoch's anchors in order of their events: its own keys, not another epoch's or a recovery's.
func epochAnchors(ctx context.Context, blobs ports.BlobStore, org uuid.UUID, epoch int) ([]anchored, error) {
	prefix := AnchorPrefix(org)
	if epoch > 0 {
		prefix += fmt.Sprintf("e%d/", epoch)
	}
	keys, err := blobs.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out []anchored
	for _, k := range keys {
		rest := strings.TrimPrefix(k, prefix)
		if strings.Contains(rest, "/") {
			continue
		}
		seq, err := strconv.ParseInt(strings.TrimSuffix(rest, ".json"), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, anchored{key: k, seq: seq, epoch: epoch})
	}
	slices.SortFunc(out, func(a, b anchored) int { return int(a.seq - b.seq) })
	return out, nil
}

// liveAnchors are the anchors verification checks: every one of the current epoch, and each earlier epoch's up to the
// head its recovery restored; the rest of an earlier epoch's are explained by its recovery (recs[e] ended epoch e).
func liveAnchors(ctx context.Context, blobs ports.BlobStore, org uuid.UUID, recs []storage.AuditRecovery) ([]anchored, int, error) {
	var out []anchored
	explained := 0
	for e := 0; e <= len(recs); e++ {
		as, err := epochAnchors(ctx, blobs, org, e)
		if err != nil {
			return nil, 0, err
		}
		for _, a := range as {
			if e < len(recs) && a.seq > recs[e].RestoredSeq {
				explained++
				continue
			}
			out = append(out, a)
		}
	}
	return out, explained, nil
}

// Recover runs the recovery step for org at now: nil when its chain agrees with every anchor of its current epoch.
func Recover(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, tell Tell, now time.Time,
	org uuid.UUID) (*storage.AuditRecovery, error) {
	var form *RecoveryForm
	var restored []byte
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		recs, err := u.Audit().Recoveries(ctx)
		if err != nil {
			return err
		}
		epoch := len(recs)
		anchors, err := epochAnchors(ctx, blobs, org, epoch)
		if err != nil || len(anchors) == 0 {
			return err
		}
		events, err := allEvents(ctx, u)
		if err != nil {
			return err
		}
		prev, first, problem, err := fromBoundary(ctx, u, blobs, signer, org)
		if err != nil || problem != "" {
			return err // a boundary that does not agree is verification's to report
		}
		hashes, _, err := ChainAfter(prev, first, events)
		if err != nil {
			return nil // a chain that does not link is verification's to report
		}
		restoredSeq, restoredHash := first-1, prev
		failedAt := -1
		for i, a := range anchors {
			if a.seq < first {
				continue // behind a purge boundary
			}
			signed, err := readAnchor(ctx, blobs, a.key)
			if err != nil {
				return nil // an anchor that cannot be read is verification's to report
			}
			msg, err := json.Marshal(signed.Anchor)
			if err != nil || signer.Verify(signed.Anchor.KeyID, msg, signed.Signature) != nil || signed.Anchor.OrganisationID != org {
				return nil // nor is one whose signature does not verify: a restore never changes a signature
			}
			got, ok := hashes[a.seq]
			if !ok || hex.EncodeToString(got) != signed.Anchor.HeadHash {
				failedAt = i
				break
			}
			restoredSeq, restoredHash = a.seq, got
		}
		if failedAt < 0 {
			return nil
		}
		form = &RecoveryForm{Schema: RecoverySchema, OrganisationID: org, Epoch: epoch + 1, RestoredSeq: restoredSeq,
			RestoredHash: hex.EncodeToString(restoredHash), MissingFrom: restoredSeq + 1, MissingTo: anchors[len(anchors)-1].seq,
			DetectedAt: now.UTC().Format(time.RFC3339Nano), Runbook: Runbook, KeyID: signer.KeyID()}
		restored = restoredHash
		return nil
	})
	if err != nil || form == nil {
		return nil, err
	}
	// The signed recovery first: a step that then fails leaves a recovery that the next run records again.
	msg, err := json.Marshal(form)
	if err != nil {
		return nil, err
	}
	sig, err := signer.Sign(ctx, msg)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(SignedRecovery{Recovery: *form, Signature: sig})
	if err != nil {
		return nil, err
	}
	key := RecoveryKey(org, form.Epoch)
	if err := blobs.Create(ctx, key, bytes.NewReader(raw)); err != nil && !errors.Is(err, ports.ErrExists) {
		return nil, err
	}
	var rec storage.AuditRecovery
	err = store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if rec, err = u.Audit().RecordRecovery(ctx, storage.NewAuditRecovery{Epoch: form.Epoch, RestoredSeq: form.RestoredSeq,
			RestoredHash: restored, MissingFrom: form.MissingFrom, MissingTo: form.MissingTo, DetectedAt: now, AnchorKey: key}); err != nil {
			return err
		}
		if tell == nil {
			return nil
		}
		return tell(ctx, u, org, rec)
	})
	if err != nil {
		return nil, err
	}
	if _, err := Reapply(ctx, store, blobs, signer, org); err != nil {
		return &rec, fmt.Errorf("re-apply the erasure register after the recovery: %w", err)
	}
	// §11.8, §11.9 (rev 0.244): keys a backup brought back are shredded again, and contacts anonymised since, again.
	if _, err := ReapplyPDPA(ctx, store, blobs, signer, org); err != nil {
		return &rec, fmt.Errorf("re-apply the shredding and anonymisation registers after the recovery: %w", err)
	}
	return &rec, nil
}

// Reapply re-applies org's erasure register (G4-070's audit part, design D2 and D8): every security event's details
// erased since the register began are erased again, so a restore from a backup taken before an erasure brings none
// back. Each entry's signature is checked first. It returns how many details it erased.
func Reapply(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, org uuid.UUID) (int64, error) {
	keys, err := blobs.List(ctx, ErasurePrefix(org))
	if err != nil {
		return 0, err
	}
	var total int64
	for _, k := range keys {
		var se SignedErasure
		if err := readJSON(ctx, blobs, k, &se); err != nil {
			return total, fmt.Errorf("erasure register %s: %w", k, err)
		}
		if err := se.Check(signer); err != nil || se.Erasure.OrganisationID != org {
			return total, fmt.Errorf("erasure register %s is not signed for this organisation", k)
		}
		cutoff, err := time.Parse(time.RFC3339Nano, se.Erasure.Cutoff)
		if err != nil {
			return total, err
		}
		err = store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			done, err := u.Audit().EraseDetails(ctx, cutoff)
			if err != nil || done.Erased == 0 {
				return err
			}
			total += done.Erased
			return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionDetailsErased, ObjectType: "audit_chain", ObjectID: org,
				Changes: map[string][2]any{"cutoff": {nil, se.Erasure.Cutoff}, "erased": {nil, done.Erased}, "first_seq": {nil, done.FirstSeq},
					"last_seq": {nil, done.LastSeq}, "register": {nil, k}, "reapplied": {nil, true}}})
		})
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// RecoverAll runs the recovery step in every organisation, and returns the recoveries it recorded.
func RecoverAll(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, tell Tell, clock ports.Clock) ([]storage.AuditRecovery, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the organisations: %w", err)
	}
	var out []storage.AuditRecovery
	for _, org := range orgs {
		r, err := Recover(ctx, store, blobs, signer, tell, clock.Now(), org)
		if err != nil {
			return out, fmt.Errorf("recover the audit trail of organisation %s: %w", org, err)
		}
		if r != nil {
			out = append(out, *r)
		}
	}
	return out, nil
}

// allEvents are the trail's events, in order.
func allEvents(ctx context.Context, u storage.UnitOfWork) ([]Stored, error) {
	var events []Stored
	for after := int64(0); ; {
		batch, err := u.Audit().Events(ctx, after, page)
		if err != nil {
			return nil, err
		}
		for _, e := range batch {
			events = append(events, Stored{ID: e.ID, Seq: e.Seq, Body: e.Body, PrevHash: e.PrevHash, Hash: e.Hash})
		}
		if len(batch) < page {
			return events, nil
		}
		after = batch[len(batch)-1].Seq
	}
}
