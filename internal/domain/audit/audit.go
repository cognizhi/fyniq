// Package audit is Fyniq's audit trail (PRD-001 §11.5): the AUD-01 event, its canonical form, the
// masking of sensitive values (SEC-15), the per-organisation hash chain (INV-12), and the signed
// anchors and verification that make tampering detectable (AUD-03, CTL-AUD-02).
//
// The unit of work writes an event for every saved change in the same transaction, and the database
// chains them: hash = SHA-256(prev_hash ‖ body), body being the canonical JSON as stored and the first
// prev_hash 32 zero bytes. contracts/audit/ documents the format so the chain can be verified outside
// Fyniq.
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Schema names the event format; contracts/audit/audit-event.v1.schema.json describes it.
const Schema = "fyniq-audit-event@1"

// Actor is who made a change: a user, an AI proposal's executor, an integration, a service or the
// scheduler (AUD-01).
type Actor struct {
	Type string `json:"type"` // user, service, integration, scheduler, ai
	ID   string `json:"id"`
}

// Object is what changed.
type Object struct {
	Type    string    `json:"type"`
	ID      uuid.UUID `json:"id"`
	Version *int      `json:"version"`
}

// Request is where a change came from.
type Request struct {
	ID        string `json:"id"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Session   string `json:"session"`
}

// Change is one field's value before and after; nil means absent.
type Change [2]any

// Event is one audit event as AUD-01 defines it, without the chain fields the database adds
// (sequence, prev_hash and hash).
type Event struct {
	Schema         string            `json:"schema"`
	EventID        uuid.UUID         `json:"event_id"`
	OrganisationID uuid.UUID         `json:"organisation_id"`
	OccurredAt     string            `json:"occurred_at"`
	Actor          Actor             `json:"actor"`
	OnBehalfOf     *Actor            `json:"on_behalf_of"`
	Originator     *string           `json:"originator"`
	ApprovalIDs    []string          `json:"approval_ids"`
	Action         string            `json:"action"`
	Object         Object            `json:"object"`
	Changes        map[string]Change `json:"changes"`
	Reason         string            `json:"reason"`
	Request        Request           `json:"request"`
}

// New starts an event with the fields every event has, and its sensitive values masked.
func New(org uuid.UUID, at time.Time, actor Actor, action string, object Object, changes map[string]Change) (Event, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Event{}, err
	}
	if changes == nil {
		changes = map[string]Change{}
	}
	return Event{
		Schema: Schema, EventID: id, OrganisationID: org, OccurredAt: at.UTC().Format(time.RFC3339Nano),
		Actor: actor, ApprovalIDs: []string{}, Action: action, Object: object, Changes: MaskChanges(changes),
	}, nil
}

// Canonical is the event's body as stored and hashed: compact JSON, struct fields in their declared
// order and map keys sorted, no HTML escaping. The same event always gives the same bytes.
func (e Event) Canonical() ([]byte, error) {
	e.Reason = RedactText(e.Reason)
	e.Changes = MaskChanges(e.Changes)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Genesis is the prev_hash of an organisation's first event.
var Genesis = make([]byte, sha256.Size)

// Link is the hash of an event: SHA-256(prev_hash ‖ body).
func Link(prev []byte, body string) []byte {
	h := sha256.New()
	h.Write(prev)
	h.Write([]byte(body))
	return h.Sum(nil)
}

// Stored is an event as the database keeps it.
type Stored struct {
	ID       uuid.UUID
	Seq      int64
	Body     string
	PrevHash []byte
	Hash     []byte
}

// Chain checks stored events, in sequence order, from the first: every sequence number follows the
// last, every prev_hash is the previous hash, and every hash is recomputed from its body. It returns
// the recomputed hash at each sequence number, for the anchors.
func Chain(events []Stored) (map[int64][]byte, error) {
	prev := Genesis
	hashes := make(map[int64][]byte, len(events))
	for i, e := range events {
		if want := int64(i + 1); e.Seq != want {
			return hashes, fmt.Errorf("event %s has sequence %d, where %d was expected: an event is missing or out of order", e.ID, e.Seq, want)
		}
		if !bytes.Equal(e.PrevHash, prev) {
			return hashes, fmt.Errorf("event %d (%s) does not link to the event before it", e.Seq, e.ID)
		}
		got := Link(prev, e.Body)
		if !bytes.Equal(got, e.Hash) {
			return hashes, fmt.Errorf("event %d (%s) was altered: its body no longer gives its stored hash", e.Seq, e.ID)
		}
		var body struct {
			EventID uuid.UUID `json:"event_id"`
		}
		if err := json.Unmarshal([]byte(e.Body), &body); err != nil || body.EventID != e.ID {
			return hashes, fmt.Errorf("event %d does not carry its own id", e.Seq)
		}
		hashes[e.Seq] = got
		prev = got
	}
	return hashes, nil
}
