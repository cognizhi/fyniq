package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Evidence is the record of one scheduled check's latest run, written once per night to the evidence
// store (PRD-001 §14.1 item 3): the BlobStore under compliance/evidence/<date>/, a retention-locked
// bucket in the cloud (p0-infra-opentofu).
type Evidence struct {
	Control    string    `json:"control"`
	Frequency  string    `json:"frequency"`
	Job        string    `json:"job"`
	Slot       time.Time `json:"slot,omitzero"`
	State      string    `json:"state"`
	Detail     string    `json:"detail,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
	OK         bool      `json:"ok"`
	Problem    string    `json:"problem,omitempty"`
}

// fresh is how old a frequency's latest run may be when the evidence is recorded.
var fresh = map[string]time.Duration{"D": 26 * time.Hour, "N": 26 * time.Hour, "W": 8 * 24 * time.Hour, "M": 32 * 24 * time.Hour}

// RecordEvidence writes the evidence of every scheduled check that runs as a platform job, from the
// job's latest slot, and returns them. It fails when a check's job did not run, failed, or ran too
// long ago for its frequency: the job then misses its heartbeat, and the absence alert fires.
func RecordEvidence(ctx context.Context, p storage.Platform, blobs ports.BlobStore, now time.Time) ([]Evidence, error) {
	controls, err := Load()
	if err != nil {
		return nil, err
	}
	return RecordEvidenceFor(ctx, controls, p, blobs, now)
}

// RecordEvidenceFor is RecordEvidence for the given controls.
func RecordEvidenceFor(ctx context.Context, controls []Control, p storage.Platform, blobs ports.BlobStore, now time.Time) ([]Evidence, error) {
	var out []Evidence
	var problems []string
	for _, c := range controls {
		for _, ch := range c.Checks {
			job, ok := strings.CutPrefix(ch.How, "job ")
			if ch.Status != "running" || !ok {
				continue
			}
			e := Evidence{Control: c.ID, Frequency: ch.Frequency, Job: job, RecordedAt: now.UTC()}
			slots, err := p.Slots(ctx, job)
			if err != nil {
				return out, err
			}
			sort.Slice(slots, func(i, j int) bool { return slots[i].Slot.Before(slots[j].Slot) })
			var latest *storage.JobSlot
			for i := len(slots) - 1; i >= 0; i-- {
				if slots[i].State != "skipped" {
					latest = &slots[i]
					break
				}
			}
			switch {
			case latest == nil:
				e.State, e.Problem = "never ran", "the job has never run"
			case latest.State != "done":
				e.Slot, e.State, e.Detail, e.Problem = latest.Slot.UTC(), latest.State, latest.Detail, "the latest run did not succeed"
			case fresh[ch.Frequency] > 0 && now.Sub(latest.Slot) > fresh[ch.Frequency]:
				e.Slot, e.State, e.Problem = latest.Slot.UTC(), latest.State, fmt.Sprintf("the latest run is %s old", now.Sub(latest.Slot).Round(time.Minute))
			default:
				e.Slot, e.State, e.Detail = latest.Slot.UTC(), latest.State, latest.Detail
			}
			e.OK = e.Problem == ""
			b, err := json.MarshalIndent(e, "", " ")
			if err != nil {
				return out, err
			}
			key := fmt.Sprintf("compliance/evidence/%s/%s-%s.json", now.UTC().Format(time.DateOnly), c.ID, ch.Frequency)
			if err := blobs.Create(ctx, key, bytes.NewReader(b)); err != nil && !errors.Is(err, ports.ErrExists) {
				return out, err // a record already written tonight stays as it was (write-once)
			}
			if !e.OK {
				problems = append(problems, fmt.Sprintf("%s (%s, job %s): %s", c.ID, ch.Frequency, job, e.Problem))
			}
			out = append(out, e)
		}
	}
	if len(problems) > 0 {
		return out, fmt.Errorf("control checks failing: %s", strings.Join(problems, "; "))
	}
	return out, nil
}

// Plan lists every check of the registry: the dry run of the runner (`jobs controls`).
func Plan() ([]string, error) {
	controls, err := Load()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range controls {
		for _, ch := range c.Checks {
			what := ch.How
			if ch.Status != "running" {
				what = "arrives with " + ch.Arrives
			}
			out = append(out, fmt.Sprintf("%-12s %-3s %-8s %-14s %s", c.ID, ch.Frequency, ch.Status, c.Owner, what))
		}
	}
	return out, nil
}
