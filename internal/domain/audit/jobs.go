package audit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The jobs' own events (they audit themselves).
const (
	ActionAnchorWritten = "audit.anchor.written"
	ActionVerified      = "audit.verification.completed"
)

// page is how many events verification reads at a time.
const page = 1000

// AnchorAll writes a signed anchor of every organisation's chain head to the write-once bucket
// (hourly, §11.5 L3, CTL-AUD-02) and records it in the organisation's trail. A chain with nothing
// new since its last anchor is skipped. It returns how many anchors it wrote.
func AnchorAll(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, clock ports.Clock) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list the organisations: %w", err)
	}
	written := 0
	for _, org := range orgs {
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			// Each close and reopening not yet anchored first (AUD-04); their events are anchored with the next head.
			if _, err := anchorCloses(ctx, u, blobs, signer, org, clock.Now()); err != nil {
				return err
			}
			head, err := u.Audit().Head(ctx)
			if err != nil || head.Seq == 0 || head.Action == ActionAnchorWritten {
				return err
			}
			recs, err := u.Audit().Recoveries(ctx) // each recovery starts the next epoch's anchors (design D8)
			if err != nil {
				return err
			}
			signed, err := Sign(ctx, signer, org, head.Seq, head.Hash, clock.Now())
			if err != nil {
				return err
			}
			b, err := json.Marshal(signed)
			if err != nil {
				return err
			}
			if err := blobs.Create(ctx, EpochAnchorKey(org, len(recs), head.Seq), bytes.NewReader(b)); errors.Is(err, ports.ErrExists) {
				return nil // anchored already
			} else if err != nil {
				return err
			}
			written++
			return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionAnchorWritten, ObjectType: "audit_chain", ObjectID: org,
				Changes: map[string][2]any{"seq": {nil, head.Seq}, "head_hash": {nil, hex.EncodeToString(head.Hash)}, "key_id": {nil, signer.KeyID()}}})
		})
		if err != nil {
			return written, fmt.Errorf("anchor organisation %s: %w", org, err)
		}
	}
	return written, nil
}

// Report is the outcome of verifying one organisation's trail.
type Report struct {
	Organisation   uuid.UUID
	Events         int
	Anchors        int
	Details        int // security events' request details checked against their fingerprints (AUD-06)
	Closes         int // close anchors checked against their snapshots and the chain (AUD-04)
	Explained      int // anchors of an earlier epoch past the head its recovery restored (design D8)
	Recoveries     int // the trail's recoveries from restores behind its anchors; Unacknowledged of them still waiting
	Unacknowledged int
	Unchecked      bool   // the details could not be checked: this service holds no fingerprint key
	Problem        string // empty when the chain, every anchor and every detail agree
}

// Keyed is the deployment's keyed hash (ports.Cipher.Fingerprint), which proved sensitive values and security events'
// details; nil where a service holds no key.
type Keyed func(ctx context.Context, scope, value []byte) ([]byte, error)

// OK reports whether the trail verified.
func (r Report) OK() bool { return r.Problem == "" }

// VerifyAll recomputes every organisation's chain from its first event and checks it against every
// anchor (nightly, AUD-03, INV-12), and every security event's details still kept against the fingerprint in its body
// (AUD-06), recording the outcome in each trail. It returns an error when any trail fails, so the job exits non-zero
// and alerts.
func VerifyAll(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, keyed Keyed) ([]Report, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the organisations: %w", err)
	}
	var reports []Report
	failed := 0
	for _, org := range orgs {
		r, err := verify(ctx, store, blobs, signer, keyed, org)
		if err != nil {
			return reports, err
		}
		if !r.OK() {
			failed++
		}
		reports = append(reports, r)
	}
	if failed > 0 {
		return reports, fmt.Errorf("the audit trail of %d organisation(s) failed verification (INV-12)", failed)
	}
	return reports, nil
}

// Nightly is the nightly verification (AUD-03, INV-12, design D8): the recovery step first, then every trail verified.
// It fails, so the job alerts, when a trail fails or a restore behind the anchors was recovered.
func Nightly(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, keyed Keyed, tell Tell,
	clock ports.Clock) ([]storage.AuditRecovery, []Report, error) {
	recs, err := RecoverAll(ctx, store, blobs, signer, tell, clock)
	if err != nil {
		return recs, nil, err
	}
	reports, err := VerifyAll(ctx, store, blobs, signer, keyed)
	if err == nil && len(recs) > 0 {
		err = fmt.Errorf("a restore landed behind the audit anchors of %d organisation(s): follow %s", len(recs), Runbook)
	}
	return recs, reports, err
}

func verify(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, keyed Keyed, org uuid.UUID) (Report, error) {
	r := Report{Organisation: org}
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var events []Stored
		for after := int64(0); ; {
			batch, err := u.Audit().Events(ctx, after, page)
			if err != nil {
				return err
			}
			for _, e := range batch {
				events = append(events, Stored{ID: e.ID, Seq: e.Seq, Body: e.Body, PrevHash: e.PrevHash, Hash: e.Hash})
			}
			if len(batch) < page {
				break
			}
			after = batch[len(batch)-1].Seq
		}
		r.Events = len(events)
		// From the latest purge boundary, whose signed copy agrees, when the trail was purged (AUD-09).
		prev, first, problem, err := fromBoundary(ctx, u, blobs, signer, org)
		if err != nil {
			return err
		}
		r.Problem = problem
		hashes, _, err := ChainAfter(prev, first, events)
		if err != nil && r.Problem == "" {
			r.Problem = err.Error()
		}
		recs, err := u.Audit().Recoveries(ctx)
		if err != nil {
			return err
		}
		r.Recoveries = len(recs)
		for _, x := range recs {
			if x.AcknowledgedBy == nil {
				r.Unacknowledged++
			}
		}
		anchors, explained, err := liveAnchors(ctx, blobs, org, recs)
		if err != nil {
			return err
		}
		r.Explained = explained
		for _, a := range anchors {
			if r.Problem != "" {
				break
			}
			if a.seq < first {
				continue // behind the boundary: its events were purged
			}
			r.Anchors++
			signed, err := readAnchor(ctx, blobs, a.key)
			if err != nil {
				r.Problem = fmt.Sprintf("anchor %s cannot be read: %v", a.key, err)
				break
			}
			if err := signed.Check(signer, org, hashes); err != nil {
				r.Problem = err.Error()
			}
		}
		if r.Problem == "" {
			if keyed == nil {
				r.Unchecked = true
			} else if r.Details, r.Problem, err = checkDetails(ctx, u, org, events, keyed); err != nil {
				return err
			}
		}
		if r.Problem == "" {
			if r.Closes, r.Problem, err = checkCloses(ctx, u, blobs, signer, org, hashes, nil); err != nil {
				return err
			}
		}
		status := "passed"
		if r.Problem != "" {
			status = "failed"
		}
		return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionVerified, ObjectType: "audit_chain", ObjectID: org,
			Changes: map[string][2]any{"status": {nil, status}, "events": {nil, r.Events}, "anchors": {nil, r.Anchors}, "details": {nil, r.Details}, "closes": {nil, r.Closes},
				"details_unchecked": {nil, r.Unchecked}, "problem": {nil, r.Problem}, "explained_anchors": {nil, r.Explained},
				"recoveries": {nil, r.Recoveries}, "unacknowledged_recoveries": {nil, r.Unacknowledged}}})
	})
	return r, err
}

func readAnchor(ctx context.Context, blobs ports.BlobStore, key string) (Signed, error) {
	rc, err := blobs.Get(ctx, key)
	if err != nil {
		return Signed{}, err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		return Signed{}, err
	}
	var s Signed
	err = json.Unmarshal(b, &s)
	return s, err
}

// checkDetails checks each security event's details still kept against the fingerprint its body carries (AUD-06):
// details changed beside the chain are caught while they exist; erased ones are not missed. It returns how many it
// checked and the first problem.
func checkDetails(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, events []Stored, keyed Keyed) (int, string, error) {
	type body struct {
		Request struct {
			Detail string `json:"detail"`
		} `json:"request"`
	}
	want := map[uuid.UUID]string{}
	seqs := map[uuid.UUID]int64{}
	var ids []uuid.UUID
	for _, e := range events {
		var b body
		if err := json.Unmarshal([]byte(e.Body), &b); err != nil || b.Request.Detail == "" {
			continue
		}
		want[e.ID], seqs[e.ID] = b.Request.Detail, e.Seq
		ids = append(ids, e.ID)
	}
	scope := []byte("audit:" + org.String())
	checked := 0
	for i := 0; i < len(ids); i += page {
		got, err := u.Audit().Details(ctx, ids[i:min(i+page, len(ids))])
		if err != nil {
			return checked, "", err
		}
		for id, d := range got {
			mac, err := keyed(ctx, scope, DetailValue(d.IP, d.UserAgent, d.Country, d.Region))
			if err != nil {
				return checked, "", err
			}
			if FingerprintPrefix+base64.RawURLEncoding.EncodeToString(mac) != want[id] {
				return checked, fmt.Sprintf("event %d's request details differ from the fingerprint in its body", seqs[id]), nil
			}
			checked++
		}
	}
	return checked, "", nil
}
