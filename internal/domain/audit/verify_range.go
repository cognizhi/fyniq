package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Verify integrity for a range (AUD-03, PRD-001 rev 0.235; p1-audit-explorer design D5): a person with audit access
// verifies the events that occurred in a range of days. The range's links are recomputed from the event before it, and
// every anchor, close anchor and security event's details in it are checked. The nightly job verifies the whole chain.

// The outcomes of a verification.
const (
	Passed    = "passed"
	Failed    = "failed"
	Explained = "explained" // a purge boundary or a restore's recovery, each with its record (AUD-09)
)

// ActionVerifiedByMember is a person's Verify integrity.
const ActionVerifiedByMember = "audit.verified_by_member"

// CodeRange refuses a range: from after to, or more than MaxRangeDays days.
const CodeRange = "AUDIT-01"

// MaxRangeDays is the longest range a person verifies at once.
const MaxRangeDays = 366

// RangeError is a range refused.
type RangeError struct{ Message, Fix string }

func (e *RangeError) Error() string { return CodeRange + ": " + e.Message }

// RangeReport is a verification of the events that occurred from From to the end of To.
type RangeReport struct {
	From, To          time.Time
	FirstSeq, LastSeq int64 // 0 when no event occurred in the range
	Events            int
	Anchors           int
	Closes            int
	Details           int
	DetailsUnchecked  bool  // no fingerprint key here: the details were not checked
	AnchoredThrough   int64 // the range's last anchored event; later ones are anchored within the hour
	Outcome           string
	FailedSeq         int64
	Problem           string
}

// VerifyRange verifies org's events that occurred on the days from from to to, in loc, and records the verification in
// the trail.
func VerifyRange(ctx context.Context, u storage.UnitOfWork, blobs ports.BlobStore, signer ports.Signer, keyed Keyed, org uuid.UUID,
	from, to time.Time, loc *time.Location) (RangeReport, error) {
	if to.Before(from) {
		return RangeReport{}, &RangeError{"the range ends before it starts", "Send a from day no later than the to day"}
	}
	if to.Sub(from) >= MaxRangeDays*24*time.Hour {
		return RangeReport{}, &RangeError{fmt.Sprintf("the range runs %d days", int(to.Sub(from).Hours()/24)+1),
			fmt.Sprintf("Verify at most %d days at a time; the nightly verification checks the whole trail", MaxRangeDays)}
	}
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	r := RangeReport{From: from, To: to, Outcome: Passed}
	var events []Stored
	for after := int64(0); ; {
		batch, err := u.Audit().Events(ctx, after, page)
		if err != nil {
			return r, err
		}
		for _, e := range batch {
			events = append(events, Stored{ID: e.ID, Seq: e.Seq, Body: e.Body, PrevHash: e.PrevHash, Hash: e.Hash})
		}
		if len(batch) < page {
			break
		}
		after = batch[len(batch)-1].Seq
	}
	a, b := -1, -1
	for i, e := range events {
		var body struct {
			OccurredAt time.Time `json:"occurred_at"`
		}
		if err := json.Unmarshal([]byte(e.Body), &body); err != nil {
			r.Outcome, r.FailedSeq, r.Problem = Failed, e.Seq, fmt.Sprintf("event %d cannot be read", e.Seq)
			return r, record(ctx, u, org, r)
		}
		if !body.OccurredAt.Before(start) && body.OccurredAt.Before(end) {
			if a < 0 {
				a = i
			}
			b = i
		}
	}
	fail := func(seq int64, problem string) (RangeReport, error) {
		r.Outcome, r.FailedSeq, r.Problem = Failed, seq, problem
		return r, record(ctx, u, org, r)
	}
	// A purged trail starts at its latest boundary (AUD-09): a range reaching back into the purged days is explained by
	// it, never clean and never failed.
	bprev, bfirst, bproblem, err := fromBoundary(ctx, u, blobs, signer, org)
	if err != nil {
		return r, err
	}
	if bproblem != "" {
		return fail(bfirst-1, bproblem)
	}
	explained := ""
	if bfirst > 1 {
		bounds, err := u.Audit().Boundaries(ctx)
		if err != nil {
			return r, err
		}
		latest := bounds[len(bounds)-1]
		if !latest.LastOccurredAt.Before(start) {
			explained = fmt.Sprintf("the events up to %d, which occurred until %s, were purged after their retention behind the boundary written on %s (%s)",
				latest.LastSeq, latest.LastOccurredAt.UTC().Format(time.DateOnly), latest.WrittenAt.UTC().Format(time.DateOnly), latest.AnchorKey)
		}
	}
	if a < 0 {
		if explained != "" {
			r.Outcome, r.Problem = Explained, explained
		}
		return r, record(ctx, u, org, r)
	}
	r.FirstSeq, r.LastSeq, r.Events = events[a].Seq, events[b].Seq, b-a+1
	r.AnchoredThrough = r.FirstSeq - 1
	prev := bprev
	if a > 0 {
		prev = events[a-1].Hash
	}
	hashes, at, err := ChainAfter(prev, r.FirstSeq, events[a:b+1])
	if err != nil {
		return fail(at, err.Error())
	}
	keys, err := blobs.List(ctx, AnchorPrefix(org))
	if err != nil {
		return r, err
	}
	for _, k := range keys {
		seq, err := strconv.ParseInt(strings.TrimSuffix(path.Base(k), ".json"), 10, 64)
		if err != nil || seq < r.FirstSeq || seq > r.LastSeq {
			continue // another kind of object, or an anchor outside the range
		}
		signed, err := readAnchor(ctx, blobs, k)
		if err != nil {
			return fail(seq, fmt.Sprintf("anchor %s cannot be read: %v", k, err))
		}
		if err := signed.Check(signer, org, hashes); err != nil {
			return fail(seq, err.Error())
		}
		r.Anchors++
		r.AnchoredThrough = max(r.AnchoredThrough, seq)
	}
	if keyed == nil {
		r.DetailsUnchecked = true
	} else {
		n, problem, err := checkDetails(ctx, u, org, events[a:b+1], keyed)
		if err != nil {
			return r, err
		}
		if problem != "" {
			return fail(0, problem)
		}
		r.Details = n
	}
	n, problem, err := checkCloses(ctx, u, blobs, signer, org, hashes, func(seq int64) bool { return seq >= r.FirstSeq && seq <= r.LastSeq })
	if err != nil {
		return r, err
	}
	if problem != "" {
		return fail(0, problem)
	}
	r.Closes = n
	if explained != "" {
		r.Outcome, r.Problem = Explained, explained
	}
	return r, record(ctx, u, org, r)
}

// record writes the verification's event.
func record(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, r RangeReport) error {
	return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionVerifiedByMember, ObjectType: "audit_chain", ObjectID: org,
		Changes: map[string][2]any{"from": {nil, r.From.Format(time.DateOnly)}, "to": {nil, r.To.Format(time.DateOnly)},
			"first_seq": {nil, r.FirstSeq}, "last_seq": {nil, r.LastSeq}, "outcome": {nil, r.Outcome}, "problem": {nil, r.Problem}}})
}
