//go:build local

package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"

	"github.com/cognizhi/fyniq/internal/ports"
)

// IRASFiler is local mode's stand-in for IRAS's filing APIs (FR-TAX-03, ADR-T4): in memory and deterministic. It accepts a filing of a kind IRAS takes with a payload and a
// period, acknowledges it with "IRAS-<hash of the key>" and returns the same acknowledgement for the same Key; with ValidateOnly it takes nothing; an empty payload, a
// period that is not a day or a kind IRAS does not take is refused with its reason; SetOutage makes every call fail with ports.ErrIRASUnavailable. It never reads the
// payload's fields: IRAS's layouts are not known here [live-verify].
type IRASFiler struct {
	mu     sync.Mutex
	outage bool
	acks   map[string]ports.IRASAck
}

var _ ports.IRASFiler = (*IRASFiler)(nil)

// NewIRASFiler is a stand-in with nothing filed.
func NewIRASFiler() *IRASFiler { return &IRASFiler{acks: map[string]ports.IRASAck{}} }

// SetOutage starts or ends an outage.
func (f *IRASFiler) SetOutage(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outage = on
}

var irasKinds = []string{ports.FilingGSTF5, ports.FilingGSTF8, ports.FilingGSTF7, ports.FilingGSTListings, ports.FilingFormCS}

// File implements ports.IRASFiler.
func (f *IRASFiler) File(_ context.Context, in ports.IRASFiling) (ports.IRASAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outage {
		return ports.IRASAck{}, ports.ErrIRASUnavailable
	}
	var problems []string
	if !slices.Contains(irasKinds, in.Kind) {
		problems = append(problems, "IRAS takes no filing of the kind "+in.Kind)
	}
	if len(in.Payload) == 0 {
		problems = append(problems, "the payload is empty")
	}
	if len(in.Period) != 10 || in.Period[4] != '-' || in.Period[7] != '-' {
		problems = append(problems, "the period is a day, YYYY-MM-DD")
	}
	if len(problems) > 0 {
		return ports.IRASAck{Problems: problems}, nil
	}
	if in.ValidateOnly {
		return ports.IRASAck{Accepted: false}, nil
	}
	if a, ok := f.acks[in.Key]; ok && in.Key != "" {
		return a, nil
	}
	sum := sha256.Sum256([]byte(in.Kind + "|" + in.Period + "|" + in.Key))
	a := ports.IRASAck{Accepted: true, Reference: "IRAS-" + hex.EncodeToString(sum[:])[:10]}
	if in.Key != "" {
		f.acks[in.Key] = a
	}
	return a, nil
}
