package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The organisation's settings with effective dates (FR-ORG-03; data/data-model "Effective-dated reference
// data"): each version is valid from a date, the database refuses overlapping versions, and every change
// is recorded with the time it was recorded (system time) and an audit event.

// The GST return frequencies and accounting bases (FR-ORG-03).
const (
	ReturnQuarterly = "quarterly"
	ReturnMonthly   = "monthly"
	BasisInvoice    = "invoice" // cash basis only once the Cash Accounting Scheme's rules are pinned (ENG-45)
)

// GSTRegistration is one GST registration: valid from RegisteredFrom until the day before
// DeregisteredFrom (nil while it lasts).
type GSTRegistration struct {
	ID                uuid.UUID
	RegisteredFrom    time.Time
	DeregisteredFrom  *time.Time
	GSTNumber         string
	ReturnFrequency   string // ReturnQuarterly or ReturnMonthly
	QuarterStartMonth int    // quarters start in this month and every third after it: 1, 2 or 3
	AccountingBasis   string // BasisInvoice
	Version           int
	RecordedAt        time.Time // system time: when this version was recorded
}

// NewGSTRegistration is a registration to record, or what a registration becomes.
type NewGSTRegistration struct {
	RegisteredFrom    time.Time
	DeregisteredFrom  *time.Time
	GSTNumber         string
	ReturnFrequency   string
	QuarterStartMonth int
	AccountingBasis   string
	RecordedAt        time.Time
}

// GSTRegistrations are the organisation's GST registrations. Overlapping validity is refused with
// CodeOverlap; every create and update is audited.
type GSTRegistrations interface {
	// List returns every registration, by RegisteredFrom.
	List(ctx context.Context) ([]GSTRegistration, error)
	Get(ctx context.Context, id uuid.UUID) (GSTRegistration, error)
	Create(ctx context.Context, r NewGSTRegistration) (GSTRegistration, error)
	// Update replaces a registration at its version, and returns it at its next; CodeVersion when it
	// changed since.
	Update(ctx context.Context, id uuid.UUID, version int, r NewGSTRegistration) (GSTRegistration, error)
}

// Sequence is a document type's number sequence (FR-ORG-04, rev 0.30).
type Sequence struct {
	ID           uuid.UUID
	DocumentType DocumentType
	Prefix       string
	NextNumber   int64
	Digits       int  // the number is padded with zeros to this many digits
	GapFree      bool // numbered when the document posts, under the sequence's lock; otherwise when it is saved
	Version      int  // 0 for a sequence not set up yet, which has its defaults
}

// SequenceSettings are what may be set on a sequence.
type SequenceSettings struct {
	Prefix     string
	NextNumber int64
	Digits     int
	GapFree    bool
}

// DefaultSequences are each document type's sequence before it is set up: gap-free, from 1, four digits; but an expense
// claim is numbered when its claimant saves it, so it is known by its number while it waits for approval.
var DefaultSequences = map[DocumentType]SequenceSettings{
	DocumentJournal:            {Prefix: "JNL-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentBill:               {Prefix: "BILL-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentBillPayment:        {Prefix: "PAY-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentVendorCredit:       {Prefix: "VC-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentSupplierRefund:     {Prefix: "RF-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentPurchaseOrder:      {Prefix: "PO-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentInvoice:            {Prefix: "INV-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentCreditNote:         {Prefix: "CN-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentCustomerReceipt:    {Prefix: "RC-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentWriteOff:           {Prefix: "WO-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentCustomerRefund:     {Prefix: "CR-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentCustomerDeposit:    {Prefix: "DP-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentQuote:              {Prefix: "QT-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentSalesOrder:         {Prefix: "SO-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentStatement:          {Prefix: "ST-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentExpenseClaim:       {Prefix: "CLM-", NextNumber: 1, Digits: 4, GapFree: false},
	DocumentClaimReimbursement: {Prefix: "RB-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentCashAdvance:        {Prefix: "ADV-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentFixedAsset:         {Prefix: "FA-", NextNumber: 1, Digits: 4, GapFree: true},
	DocumentAssetDisposal:      {Prefix: "DSP-", NextNumber: 1, Digits: 4, GapFree: true},
}

// FormatNumber is a document number as the sequence s prints it: its prefix and the number, zero-padded.
func FormatNumber(prefix string, n int64, digits int) string {
	return fmt.Sprintf("%s%0*d", prefix, digits, n)
}

// Sequences are the organisation's document number sequences. A change to a sequence's settings is
// audited; taking a number is audited on its document.
type Sequences interface {
	// Get returns a document type's sequence, or its defaults at version 0 when it is not set up yet.
	Get(ctx context.Context, t DocumentType) (Sequence, error)
	// Update sets a sequence up, or changes it, at its version (0 to set it up): CodeVersion when it changed
	// since, CodeSequence when the next number would go back.
	Update(ctx context.Context, t DocumentType, version int, s SequenceSettings) (Sequence, error)
	// Number gives document id the next number of its type's sequence, under the sequence's lock (ENG-47
	// step 1a, after the document's own lock), and returns it; a document numbered already keeps its number.
	Number(ctx context.Context, id uuid.UUID, t DocumentType) (string, error)
}
