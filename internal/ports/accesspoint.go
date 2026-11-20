package ports

import (
	"context"
	"errors"
)

// AccessPointParticipant is what the network's directory says about a Peppol participant ID, written scheme:value (a
// Singapore UEN is 0195:<UEN>).
type AccessPointParticipant struct {
	ID         string
	Registered bool
	// Accepts are the document types it receives, such as "invoice" and "credit_note".
	Accepts []string
}

// AccessPointSend is a document for the access point to send: PINT-SG bytes, made and checked by invoicenow.Build.
type AccessPointSend struct {
	To           string // the buyer's participant ID
	DocumentUUID string
	Version      int
	Kind         string
	Payload      []byte
	// Key makes a retry of the same send a no-op at the access point (the outbox's key).
	Key string
}

// The states an access point reports for a transmission.
const (
	TransmissionQueued       = "queued"
	TransmissionDelivered    = "delivered"
	TransmissionAcknowledged = "acknowledged" // IRAS's acknowledgement ID is in AccessPointStatus.IRASAck
	TransmissionFailed       = "failed"
)

// AccessPointStatus is where a transmission stands.
type AccessPointStatus struct {
	State   string
	IRASAck string
	Reasons []string
}

// AccessPointInbound is a document received for the organisation.
type AccessPointInbound struct {
	MessageID string
	From      string // the sender's participant ID
	Payload   []byte
}

// ErrAccessPointUnavailable is an outage: the call may be retried; nothing was sent or read (G4-033).
var ErrAccessPointUnavailable = errors.New("the InvoiceNow access point cannot be reached")

// AccessPoint is an IMDA-accredited Peppol access point (FR-AR-12, FR-AP-05). Production: the provider the founders
// choose (Q2), [live-verify] against its sandbox. Local: an in-memory stand-in that behaves as the contract in
// connectors/peppol-access-point/contract.json says, so every adapter answers the same fixtures.
type AccessPoint interface {
	// Lookup asks the directory whether a participant is on the network and what it accepts (the customer's Peppol ID
	// is validated before a document is sent).
	Lookup(ctx context.Context, participantID string) (AccessPointParticipant, error)
	// Send hands a document over and returns its transmission ID; the same Key returns the same ID.
	Send(ctx context.Context, s AccessPointSend) (string, error)
	// Status reads a transmission, up to IRAS's acknowledgement ID.
	Status(ctx context.Context, transmissionID string) (AccessPointStatus, error)
	// Poll reads the documents received after cursor and the cursor to read from next time.
	Poll(ctx context.Context, cursor string) ([]AccessPointInbound, string, error)
}
