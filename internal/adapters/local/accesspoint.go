//go:build local

package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"

	"github.com/cognizhi/fyniq/internal/ports"
)

// AccessPoint is local mode's stand-in for an InvoiceNow access point (FR-AR-12, ADR-T4): in memory, deterministic and
// the same for every test, with synthetic participants only. It behaves as contracts/connectors/peppol-access-point.contract.json
// says:
//   - a participant 0195:SGUEN<UEN> whose UEN starts 9999 is not on the network; any other is;
//   - a send is accepted when its payload names PINT-SG's specification identifier and is not empty, else it fails with
//     its reason; a second send with the same Key returns the same transmission ID and sends nothing again;
//   - a transmission reads queued, then delivered, then acknowledged with IRAS's acknowledgement ID "IRAS-<ID>", one
//     step for each read of its status;
//   - Receive gives the next Poll a document, as a supplier's access point would;
//   - SetOutage makes every call fail with ports.ErrAccessPointUnavailable (G4-033).
type AccessPoint struct {
	mu      sync.Mutex
	outage  bool
	sent    map[string]*transmission // by Key
	byID    map[string]*transmission
	inbound []ports.AccessPointInbound
}

type transmission struct {
	id     string
	reads  int
	failed []string
}

var _ ports.AccessPoint = (*AccessPoint)(nil)

// NewAccessPoint is an access point with nothing sent or received.
func NewAccessPoint() *AccessPoint {
	return &AccessPoint{sent: map[string]*transmission{}, byID: map[string]*transmission{}}
}

// SetOutage starts or ends an outage.
func (a *AccessPoint) SetOutage(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.outage = on
}

// Receive queues a document from sender for the next Poll.
func (a *AccessPoint) Receive(from string, payload []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inbound = append(a.inbound, ports.AccessPointInbound{MessageID: "msg-" + strconv.Itoa(len(a.inbound)+1), From: from, Payload: payload})
}

// Lookup implements ports.AccessPoint.
func (a *AccessPoint) Lookup(_ context.Context, id string) (ports.AccessPointParticipant, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.outage {
		return ports.AccessPointParticipant{}, ports.ErrAccessPointUnavailable
	}
	scheme, value, ok := strings.Cut(id, ":")
	value, prefixed := strings.CutPrefix(value, "SGUEN")
	if !ok || scheme != "0195" || !prefixed || len(value) < 9 || len(value) > 10 || strings.HasPrefix(value, "9999") {
		return ports.AccessPointParticipant{ID: id}, nil
	}
	return ports.AccessPointParticipant{ID: id, Registered: true, Accepts: []string{"invoice", "credit_note", "debit_note"}}, nil
}

// Send implements ports.AccessPoint.
func (a *AccessPoint) Send(_ context.Context, s ports.AccessPointSend) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.outage {
		return "", ports.ErrAccessPointUnavailable
	}
	if t, ok := a.sent[s.Key]; ok {
		return t.id, nil
	}
	sum := sha256.Sum256([]byte(s.Key))
	t := &transmission{id: "tx-" + hex.EncodeToString(sum[:])[:12]}
	switch {
	case len(s.Payload) == 0:
		t.failed = []string{"the document is empty"}
	case !bytes.Contains(s.Payload, []byte("urn:peppol:pint:billing-1@sg-1")):
		t.failed = []string{"the specification identifier is not urn:peppol:pint:billing-1@sg-1"}
	}
	a.sent[s.Key], a.byID[t.id] = t, t
	return t.id, nil
}

// Status implements ports.AccessPoint.
func (a *AccessPoint) Status(_ context.Context, id string) (ports.AccessPointStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.outage {
		return ports.AccessPointStatus{}, ports.ErrAccessPointUnavailable
	}
	t, ok := a.byID[id]
	if !ok {
		return ports.AccessPointStatus{}, ports.ErrNotFound
	}
	if t.failed != nil {
		return ports.AccessPointStatus{State: ports.TransmissionFailed, Reasons: t.failed}, nil
	}
	t.reads++
	switch t.reads {
	case 1:
		return ports.AccessPointStatus{State: ports.TransmissionQueued}, nil
	case 2:
		return ports.AccessPointStatus{State: ports.TransmissionDelivered}, nil
	}
	return ports.AccessPointStatus{State: ports.TransmissionAcknowledged, IRASAck: "IRAS-" + strings.TrimPrefix(id, "tx-")}, nil
}

// Poll implements ports.AccessPoint: the cursor is the number of documents already read.
func (a *AccessPoint) Poll(_ context.Context, cursor string) ([]ports.AccessPointInbound, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.outage {
		return nil, cursor, ports.ErrAccessPointUnavailable
	}
	n, _ := strconv.Atoi(cursor)
	if n < 0 || n > len(a.inbound) {
		n = 0
	}
	out := append([]ports.AccessPointInbound(nil), a.inbound[n:]...)
	return out, strconv.Itoa(len(a.inbound)), nil
}
