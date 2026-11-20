package ports

import (
	"context"
	"errors"
)

// IRAS filings through its APIs (PRD-001 FR-TAX-03, FR-TAX-06; p2-tax-filings design D5). IRAS lists CorpPass-authorised APIs for the GST return (Submission of GST Return
// (F5), File GST Return (F5 and F8), Edit Past GST Return (F7), Submission of GST Transaction Listings) and Submission of Corporate Tax Records (Form C-S and pre-fill) on its
// developer portal; the payload of each is given to a registered developer behind IRAS's sign-in, so no layout is written here: the caller gives the bytes its adapter
// maps, and the real adapter is [live-verify] once IRAS grants access.

// The filings IRAS takes through its APIs.
const (
	FilingGSTF5       = "gst_f5"
	FilingGSTF8       = "gst_f8"
	FilingGSTF7       = "gst_f7"
	FilingGSTListings = "gst_transaction_listings"
	FilingFormCS      = "form_cs"
)

// IRASFiling is one submission to IRAS.
type IRASFiling struct {
	Kind string
	// Period names what the filing is for: a GST return's last day, or a Year of Assessment's end, as YYYY-MM-DD.
	Period string
	// Payload is the filing as the adapter maps it for IRAS.
	Payload []byte
	// Key makes a retry of the same filing a no-op: the same Key returns the same acknowledgement.
	Key string
	// ValidateOnly asks IRAS to check the filing and take nothing, where its API allows.
	ValidateOnly bool
}

// IRASAck is IRAS's answer to a filing.
type IRASAck struct {
	Accepted bool
	// Reference is IRAS's acknowledgement of an accepted filing; empty for one only validated or refused.
	Reference string
	Problems  []string
}

// ErrIRASUnavailable is an outage of IRAS or its API gateway: the call may be retried and nothing was filed.
var ErrIRASUnavailable = errors.New("IRAS's API cannot be reached")

// IRASFiler files with IRAS. Production: an adapter over IRAS's APIs behind Corppass [live-verify]. Local: a stand-in that behaves as
// contracts/connectors/iras-filing.contract.json says.
type IRASFiler interface {
	File(ctx context.Context, f IRASFiling) (IRASAck, error)
}
