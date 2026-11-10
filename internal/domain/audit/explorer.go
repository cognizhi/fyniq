package audit

import (
	"encoding/json"
	"sort"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The audit trail explorer (FR-CTL-03, AUD-07; p1-audit-explorer design D6) shows an event as its body records it, each
// field's before and after, and for a sensitive value only whether it changed (AUD-02): the trail keeps its proof, never
// the value.

// What a sensitive value's proofs show.
const (
	ProofChanged     = "changed"
	ProofChangedBack = "changed_back" // to what it was before the field's previous change
	ProofUnchanged   = "unchanged"
)

// Entry is an event as the explorer reads it.
type Entry struct {
	Seq         int64
	ID          uuid.UUID
	OccurredAt  string
	Actor       Actor
	OnBehalfOf  *Actor
	Originator  *string
	ApprovalIDs []string
	Action      string
	Object      Object
	Reason      string
	Request     Request
	Changes     []FieldChange
	PrevHash    []byte
	Hash        []byte
}

// FieldChange is one field's before and after, as the body keeps them. Proof is set for a sensitive value only.
type FieldChange struct {
	Field  string
	Before any
	After  any
	Proof  string
	Hint   string
}

// Read reads a stored event for the explorer, its fields by name.
func Read(e storage.AuditEvent) (Entry, error) {
	var b Event
	if err := json.Unmarshal([]byte(e.Body), &b); err != nil {
		return Entry{}, err
	}
	out := Entry{Seq: e.Seq, ID: e.ID, OccurredAt: b.OccurredAt, Actor: b.Actor, OnBehalfOf: b.OnBehalfOf, Originator: b.Originator,
		ApprovalIDs: b.ApprovalIDs, Action: b.Action, Object: b.Object, Reason: b.Reason, Request: b.Request, PrevHash: e.PrevHash, Hash: e.Hash}
	fields := make([]string, 0, len(b.Changes))
	for f := range b.Changes {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	for _, f := range fields {
		c := FieldChange{Field: f, Before: b.Changes[f][0], After: b.Changes[f][1]}
		before, hb := proofOf(c.Before)
		after, ha := proofOf(c.After)
		if before != "" || after != "" {
			c.Proof = ProofChanged
			if before == after {
				c.Proof = ProofUnchanged
			}
			c.Hint = ha
			if c.Hint == "" {
				c.Hint = hb
			}
		}
		out.Changes = append(out.Changes, c)
	}
	return out, nil
}

// proofOf is a value's fingerprint and hint when it is a proof (AUD-02).
func proofOf(v any) (string, string) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", ""
	}
	f, _ := m["fingerprint"].(string)
	h, _ := m["hint"].(string)
	return f, h
}

// ChangedBack marks a sensitive field whose after is what it was before the field's previous change: earlier is that
// previous change's before.
func (c *FieldChange) ChangedBack(earlier any) {
	if c.Proof != ProofChanged {
		return
	}
	after, _ := proofOf(c.After)
	if prev, _ := proofOf(earlier); prev != "" && prev == after {
		c.Proof = ProofChangedBack
	}
}

// Fields are the names of the fields an event changed, in order.
func (e Entry) Fields() []string {
	out := make([]string, len(e.Changes))
	for i, c := range e.Changes {
		out[i] = c.Field
	}
	return out
}
