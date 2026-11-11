package audit

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The purge after retention (AUD-09, open question 38, the owner's decision of PRD-001 rev 0.230; p1-audit-explorer
// design D7a). With the switch on, the trail's leading run of events whose records' retention has passed is purged
// behind a boundary: signed and written once to the anchors' bucket first, then recorded, then the events go, through
// the one database function that may delete them. Verification starts from the latest boundary.

// PurgeSetting is the purge after retention's switch: the trail's purge and the ledger's (AUD-10).
const PurgeSetting = "retention.purge_after_retention"

// ActionPurged is a purge's event.
const ActionPurged = "audit.purged"

// BoundarySchema names a boundary's signed form.
const BoundarySchema = "fyniq-audit-boundary@1"

// Boundary is a purge boundary as it is signed.
type Boundary struct {
	Schema         string           `json:"schema"`
	OrganisationID uuid.UUID        `json:"organisation_id"`
	LastSeq        int64            `json:"last_seq"`
	LastHash       string           `json:"last_hash"`
	LastOccurredAt string           `json:"last_occurred_at"`
	Approval       string           `json:"approval"`
	Counts         map[string]int64 `json:"counts"`
	WrittenAt      string           `json:"written_at"`
	KeyID          string           `json:"key_id"`
}

// SignedBoundary is a boundary with its signature, as written to the bucket.
type SignedBoundary struct {
	Boundary  Boundary `json:"boundary"`
	Signature []byte   `json:"signature"`
}

// BoundaryKey is where a boundary is written, once.
func BoundaryKey(org uuid.UUID, seq int64) string { return fmt.Sprintf("audit-boundaries/%s/%020d.json", org, seq) }

// Check verifies a boundary's signature and that it is the one recorded.
func (sb SignedBoundary) Check(s ports.Signer, org uuid.UUID, b storage.AuditBoundary) error {
	msg, err := json.Marshal(sb.Boundary)
	if err != nil {
		return err
	}
	if err := s.Verify(sb.Boundary.KeyID, msg, sb.Signature); err != nil || sb.Boundary.OrganisationID != org {
		return fmt.Errorf("the purge boundary at event %d is not signed for this organisation", b.LastSeq)
	}
	if sb.Boundary.LastSeq != b.LastSeq || sb.Boundary.LastHash != hex.EncodeToString(b.LastHash) {
		return fmt.Errorf("the purge boundary at event %d differs from its signed copy", b.LastSeq)
	}
	return nil
}

// PurgeOn reports whether the purge after retention is switched on, and which setting change switched it on.
func PurgeOn(ctx context.Context, u storage.UnitOfWork, day time.Time) (bool, string, error) {
	def, _ := settings.Lookup(PurgeSetting)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return false, "", err
	}
	v := settings.On(def, changes, day)
	if v.Value != "true" {
		return false, "", nil
	}
	approval := def.Key
	for _, c := range changes {
		if !c.EffectiveFrom.After(day) && c.Value == "true" {
			approval = fmt.Sprintf("%s v%d", def.Key, c.Version)
		}
	}
	return true, approval, nil
}

// RecordsYears is the organisation's records' retention on a day, in years after the financial year (§11.9): it only
// lengthens, and is never less than 7.
func RecordsYears(ctx context.Context, u storage.UnitOfWork, day time.Time) (int, error) {
	def, _ := settings.Lookup("retention.records_years")
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(settings.On(def, changes, day).Value)
	return max(n, 7), err
}

// PurgeTrail purges org's leading run of events past their retention, if the purge is on (weekly), and returns how
// many events went.
func PurgeTrail(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, now time.Time, org uuid.UUID) (int64, error) {
	var b Boundary
	var last Stored
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		on, approval, err := PurgeOn(ctx, u, now)
		if err != nil || !on {
			return err
		}
		years, err := RecordsYears(ctx, u, now)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Profile.TimeZone)
		if err != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		bounds, err := u.Audit().Boundaries(ctx)
		if err != nil {
			return err
		}
		after := int64(0)
		if len(bounds) > 0 {
			after = bounds[len(bounds)-1].LastSeq
		}
		counts := map[string]int64{"events": 0, "security_events": 0}
		var at time.Time
	scan:
		for {
			batch, err := u.Audit().Events(ctx, after, page)
			if err != nil {
				return err
			}
			for _, e := range batch {
				var body struct {
					Action     string    `json:"action"`
					OccurredAt time.Time `json:"occurred_at"`
				}
				if err := json.Unmarshal([]byte(e.Body), &body); err != nil {
					return err
				}
				if !evidence.RetainUntil(o.Profile.FYStartMonth, years, body.OccurredAt, loc).Before(now) {
					break scan
				}
				last, at = Stored{ID: e.ID, Seq: e.Seq, Hash: e.Hash}, body.OccurredAt
				counts["events"]++
				if IsSecurity(body.Action) {
					counts["security_events"]++
				}
			}
			if len(batch) < page {
				break
			}
			after = batch[len(batch)-1].Seq
		}
		if counts["events"] == 0 {
			return nil
		}
		b = Boundary{Schema: BoundarySchema, OrganisationID: org, LastSeq: last.Seq, LastHash: hex.EncodeToString(last.Hash),
			LastOccurredAt: at.UTC().Format(time.RFC3339Nano), Approval: approval, Counts: counts, WrittenAt: now.UTC().Format(time.RFC3339Nano),
			KeyID: signer.KeyID()}
		return nil
	})
	if err != nil || b.LastSeq == 0 {
		return 0, err
	}
	// The signed boundary first: a purge that then fails leaves a boundary that verification reads, never a gap.
	msg, err := json.Marshal(b)
	if err != nil {
		return 0, err
	}
	sig, err := signer.Sign(ctx, msg)
	if err != nil {
		return 0, err
	}
	raw, err := json.Marshal(SignedBoundary{Boundary: b, Signature: sig})
	if err != nil {
		return 0, err
	}
	key := BoundaryKey(org, b.LastSeq)
	if err := blobs.Create(ctx, key, bytes.NewReader(raw)); err != nil && !errors.Is(err, ports.ErrExists) {
		return 0, err
	}
	at, err := time.Parse(time.RFC3339Nano, b.LastOccurredAt)
	if err != nil {
		return 0, err
	}
	var purged int64
	err = store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Audit().RecordBoundary(ctx, storage.NewAuditBoundary{LastSeq: b.LastSeq, LastHash: last.Hash, LastOccurredAt: at,
			Approval: b.Approval, Counts: b.Counts, AnchorKey: key, WrittenAt: now}); err != nil {
			return err
		}
		if purged, err = u.Audit().Purge(ctx, b.LastSeq); err != nil {
			return err
		}
		return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionPurged, ObjectType: "audit_chain", ObjectID: org,
			Changes: map[string][2]any{"last_seq": {nil, b.LastSeq}, "purged": {nil, purged}, "boundary": {nil, key}}})
	})
	return purged, err
}

// PurgeAll runs the purge after retention in every organisation that switched it on (weekly).
func PurgeAll(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, clock ports.Clock) (int64, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list the organisations: %w", err)
	}
	var total int64
	for _, org := range orgs {
		n, err := PurgeTrail(ctx, store, blobs, signer, clock.Now(), org)
		if err != nil {
			return total, fmt.Errorf("purge the audit trail of organisation %s: %w", org, err)
		}
		total += n
	}
	return total, nil
}

// fromBoundary is where a purged trail's chain starts: the latest boundary's hash and the sequence number after it;
// Genesis and 1 when the trail was never purged. A boundary whose signed copy does not agree is a problem, and the
// chain is still checked from it.
func fromBoundary(ctx context.Context, u storage.UnitOfWork, blobs ports.BlobStore, signer ports.Signer, org uuid.UUID) ([]byte, int64, string, error) {
	bounds, err := u.Audit().Boundaries(ctx)
	if err != nil || len(bounds) == 0 {
		return Genesis, 1, "", err
	}
	b := bounds[len(bounds)-1]
	var sb SignedBoundary
	if err := readJSON(ctx, blobs, b.AnchorKey, &sb); err != nil {
		return b.LastHash, b.LastSeq + 1, fmt.Sprintf("the purge boundary at event %d has no signed copy: %v", b.LastSeq, err), nil
	}
	if err := sb.Check(signer, org, b); err != nil {
		return b.LastHash, b.LastSeq + 1, err.Error(), nil
	}
	return b.LastHash, b.LastSeq + 1, "", nil
}

// anchorSeq is the sequence number a chain anchor's key names.
func anchorSeq(key string) (int64, error) {
	return strconv.ParseInt(strings.TrimSuffix(path.Base(key), ".json"), 10, 64)
}
