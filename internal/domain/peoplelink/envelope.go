// Package peoplelink is Fyniq's side of the link to Nemura (PRD-001 §8.26, PRD-002 §16): the envelope every event and call
// between the apps carries (NM-INT-10), its canonical bytes, digest and signature, and the checks that refuse a forged,
// replayed or stale one (FR-PPL-09, FR-PPL-15). It is pure: no storage, no clock, no network. Nemura implements the same rule in
// its own code (the architecture test forbids sharing it) and both pass the fixtures in contracts/peoplelink.
package peoplelink

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The refusals of an envelope, each with a stable code. A refusal never carries the payload.
const (
	CodeMalformed = "PPL-01" // the envelope is not well formed, or a field is missing or not in its shape
	CodeSignature = "PPL-02" // the signature does not verify with the link key
	CodeDigest    = "PPL-03" // the payload's SHA-256 is not the one the header carries
	CodeReplay    = "PPL-04" // the signing time is more than 5 minutes from now
	CodeEpoch     = "PPL-05" // the event is of another epoch of the link
	CodeOrg       = "PPL-06" // the event is for or from another organisation than the link's
)

// Refusal is a refused envelope: a stable code and a plain message.
type Refusal struct {
	Code, Message string
	cause         error
}

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }
func (r *Refusal) Unwrap() error { return r.cause }

// ErrMalformed is the cause of a document that cannot be canonicalised.
var ErrMalformed = errors.New("malformed")

// Window is how far a signing time may be from now: the replay window (NM-INT-10).
const Window = 5 * time.Minute

// SigningPrefix starts every signed message, so a signature made here cannot be taken for one made for another purpose.
const SigningPrefix = "fyniq-link-v1\n"

// Header is everything an envelope says about its payload (NM-INT-10). Its times are RFC 3339 in UTC with a Z.
type Header struct {
	EventID            string `json:"event_id"`
	Type               string `json:"type"`
	SchemaVersion      int    `json:"schema_version"`
	SourceSystem       string `json:"source_system"`
	SourceOrganisation string `json:"source_organisation"`
	TargetOrganisation string `json:"target_organisation"`
	Epoch              int    `json:"epoch"`
	Sequence           int64  `json:"sequence"`
	ObjectType         string `json:"object_type"`
	ObjectID           string `json:"object_id"`
	ObjectVersion      int    `json:"object_version"`
	EffectiveAt        string `json:"effective_at"`
	RecordedAt         string `json:"recorded_at"`
	SignedAt           string `json:"signed_at"`
	CorrelationID      string `json:"correlation_id"`
	PayloadSHA256      string `json:"payload_sha256"`
}

// Envelope is an event or call: its header, the link key's signature over the header, and the payload.
type Envelope struct {
	Header    Header          `json:"header"`
	Signature string          `json:"signature"`
	Payload   json.RawMessage `json:"payload"`
}

// Digest is the SHA-256, in lower-case hex, of a payload's canonical bytes.
func Digest(payload []byte) (string, error) {
	c, err := Canonical(payload)
	if err != nil {
		return "", &Refusal{CodeMalformed, "the payload cannot be read as canonical JSON: " + err.Error(), err}
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:]), nil
}

// SigningInput is the message the link key signs: the prefix and the header's canonical bytes.
func SigningInput(h Header) ([]byte, error) {
	raw, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	c, err := Canonical(raw)
	if err != nil {
		return nil, err
	}
	return append([]byte(SigningPrefix), c...), nil
}

// Sign fills the header's payload digest and signs it with the link's private key.
func Sign(key ed25519.PrivateKey, h Header, payload []byte) (Envelope, error) {
	d, err := Digest(payload)
	if err != nil {
		return Envelope{}, err
	}
	h.PayloadSHA256 = d
	in, err := SigningInput(h)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Header: h, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, in)), Payload: append(json.RawMessage(nil), payload...)}, nil
}

// Link is what the receiving side knows of the link an envelope claims to belong to.
type Link struct {
	Key                ed25519.PublicKey // the link key given at the approved link, never one from the message
	Epoch              int
	SourceOrganisation string // the sending side's organisation
	TargetOrganisation string // the receiving side's organisation
	SourceSystem       string
}

// Verify checks an envelope against the link, in the order a forger is most likely to fail: the shape, the organisations,
// the epoch, the signing time, the signature over the header and last the payload's digest. It reads no clock itself.
func (e Envelope) Verify(l Link, now time.Time) error {
	h := e.Header
	if h.EventID == "" || h.Type == "" || h.SchemaVersion < 1 || h.ObjectType == "" || h.ObjectID == "" || h.CorrelationID == "" || len(h.PayloadSHA256) != 64 || e.Signature == "" || len(e.Payload) == 0 {
		return &Refusal{CodeMalformed, "the envelope lacks an event ID, a type, a schema version, an object, a correlation ID, a digest, a signature or a payload", nil}
	}
	if h.SourceOrganisation != l.SourceOrganisation || h.TargetOrganisation != l.TargetOrganisation || h.SourceSystem != l.SourceSystem {
		return &Refusal{CodeOrg, "the event is not from the linked organisation to this one", nil}
	}
	switch {
	case h.Epoch < l.Epoch:
		return &Refusal{CodeEpoch, fmt.Sprintf("the event is of epoch %d; the link is at epoch %d, and earlier epochs are refused", h.Epoch, l.Epoch), nil}
	case h.Epoch > l.Epoch:
		return &Refusal{CodeEpoch, fmt.Sprintf("the event is of epoch %d, which this side has not linked yet (the link is at epoch %d)", h.Epoch, l.Epoch), nil}
	}
	signed, err := time.Parse(time.RFC3339, h.SignedAt)
	if err != nil {
		return &Refusal{CodeMalformed, "signed_at is not an RFC 3339 time", err}
	}
	if d := now.Sub(signed); d > Window || d < -Window {
		return &Refusal{CodeReplay, "the event was signed more than 5 minutes from now", nil}
	}
	in, err := SigningInput(h)
	if err != nil {
		return &Refusal{CodeMalformed, "the header cannot be read as canonical JSON", err}
	}
	sig, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil || len(l.Key) != ed25519.PublicKeySize || !ed25519.Verify(l.Key, in, sig) {
		return &Refusal{CodeSignature, "the signature does not verify with the link key", nil}
	}
	d, err := Digest(e.Payload)
	if err != nil {
		return err
	}
	if d != h.PayloadSHA256 {
		return &Refusal{CodeDigest, "the payload is not the one the signed header names", nil}
	}
	return nil
}
