package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeSalaryBatch is the database refusing a change to a salary batch: a line, event or release changed or deleted, a batch moved backwards, approved without a second person
// who is neither the run's preparer nor its approver, released unapproved or by the run's preparer or approver, or paid with no bank line or evidence behind it (PRD-001 FR-PAY-11,
// FR-PPL-10, FR-PPL-12, FR-PPL-14; p1-payroll-link tasks 4.2, 6.1, 6.2).
const CodeSalaryBatch = "STORAGE-SALARY-BATCH-01"

// The statuses of a salary batch as stored. What a person reads (released, paid in part, paid) is worked out from its lines' states.
const (
	SalaryOpen      = "open"
	SalaryApproved  = "approved"
	SalaryCancelled = "cancelled"
)

// The states of a salary payment line (FR-PPL-12, PRD-002 NM-PAY-10).
const (
	LineScheduled = "scheduled"
	LineReleased  = "released"
	LineSent      = "sent"
	LineAccepted  = "accepted"
	LinePaid      = "paid"
	LineRejected  = "rejected"
	LineReturned  = "returned"
	LineUnknown   = "unknown"
)

// Where a line's state came from: it arrived, a release made it, the bank feed matched the debit, or a person recorded it with evidence.
const (
	LineByArrival  = "arrival"
	LineByRelease  = "release"
	LineByBankFeed = "bank_feed"
	LineByManual   = "manual"
)

// SalaryBatch is the payment lines of a posted pay run, as Fyniq holds them (its lines are SalaryLine, sealed).
type SalaryBatch struct {
	ID, LinkID, RunID, ReceiptID uuid.UUID
	Seq                          int
	SourceSystem                 string
	RunVersion                   int
	WageMonth                    time.Time
	PayDate                      *time.Time // from preparation
	MoneyAccountID               uuid.UUID  // uuid.Nil until prepared
	Method                       string
	PreparedBy                   string // who prepared it in Fyniq: an actor of the audit trail, "" before
	Status                       string
	Version, LinesVersion        int
	CancelReason                 string
	CreatedAt, ChangedAt         time.Time
}

// SalaryLine is a version of one person's payment line. The payee's name, bank, account name and number are sealed (SEC-11); the rest is what the approval's envelope binds.
type SalaryLine struct {
	ID, BatchID, LineRef, PersonID, BankDetailID, EventID uuid.UUID
	LineVersion, BankDetailVersion                        int
	Amount                                                Decimal
	BankDetailDigest                                      string
	Sealed                                                []byte
	CreatedAt                                             time.Time
}

// SalaryLineEvent is a state of a line: append-only. Paid, rejected, returned and unknown come from the bank feed (BankLineID) or a person with evidence (EvidenceID).
type SalaryLineEvent struct {
	ID, BatchID, LineRef, PersonID, RunID uuid.UUID
	State, Source, InstructionID          string
	Amount                                *Decimal
	OnDate                                *time.Time
	BankLineID, EvidenceID                uuid.UUID
	RecordedBy                            string
	At                                    time.Time
}

// SalaryRelease is what one release of a salary batch wrote: the file sealed, its SHA-256 and which lines went in it.
type SalaryRelease struct {
	ID, BatchID, ApprovalID, ReleasedBy uuid.UUID
	BatchVersion, Lines                 int
	ReleasedAt                          time.Time
	Writer, FileName, MediaType         string
	Sealed, SHA256                      []byte
	Total                               Decimal
}

// SalaryReleaseLine is a line in a release, with the instruction ID the bank's instruction carries (FR-PPL-12).
type SalaryReleaseLine struct {
	ID, ReleaseID, BatchID, LineRef, LineID uuid.UUID
	InstructionID                           string
}

// SalaryBatches are the salary payment batches, their sealed lines, the states of each line and their releases. Every change is audited by the batch, with counts and IDs and never a name,
// an account or a person's amount (FR-PPL-17).
type SalaryBatches interface {
	// Create records a batch for a run's version, audited as salary_batch.received. NextSeq gives its number; AddLines gives it its lines.
	Create(ctx context.Context, b SalaryBatch) (SalaryBatch, error)
	// AddLines records a new batch's lines, each with its first state, scheduled, audited as salary_batch.lines_added with the count.
	AddLines(ctx context.Context, batch uuid.UUID, lines []SalaryLine, at time.Time) error
	// NextSeq is the number the next batch takes.
	NextSeq(ctx context.Context) (int, error)
	// Get is a batch, or ports.ErrNotFound; ForUpdate locks it for the change that follows.
	Get(ctx context.Context, id uuid.UUID, forUpdate bool) (SalaryBatch, error)
	// OfRun is the batch of a run's version, locked, or ports.ErrNotFound.
	OfRun(ctx context.Context, source string, run uuid.UUID, version int) (SalaryBatch, error)
	// List are the batches, newest first.
	List(ctx context.Context) ([]SalaryBatch, error)
	// Lines are the current version of each line of a batch, in the order they arrived.
	Lines(ctx context.Context, batch uuid.UUID) ([]SalaryLine, error)
	// Revise adds new versions of lines of a batch and raises its version and lines version, which makes an approval of the old one stale (FR-PPL-14), audited as salary_batch.lines_revised.
	Revise(ctx context.Context, batch uuid.UUID, linesVersion int, lines []SalaryLine, at time.Time) (SalaryBatch, error)
	// Prepare names the money account, payment date and method at the batch's version and raises it, audited as salary_batch.prepared. CodeVersion when it moved on.
	Prepare(ctx context.Context, batch uuid.UUID, version int, account uuid.UUID, on time.Time, method, by string, at time.Time) (SalaryBatch, error)
	// Approve marks a batch approved (the database refuses it without a second person's approval), audited as salary_batch.approved.
	Approve(ctx context.Context, batch uuid.UUID, approval uuid.UUID, at time.Time) (SalaryBatch, error)
	// Cancel cancels a batch with nothing released, with a reason, audited as salary_batch.cancelled.
	Cancel(ctx context.Context, batch uuid.UUID, reason string, at time.Time) (SalaryBatch, error)
	// Events are the states of the batch's lines, oldest first.
	Events(ctx context.Context, batch uuid.UUID) ([]SalaryLineEvent, error)
	// AddEvent records a state of a line, audited as salary_batch.line_state (the state, its source and the IDs, never an amount).
	AddEvent(ctx context.Context, e SalaryLineEvent) error
	// StatesOfRunPerson are the latest state of each of a person's lines of a run, across its batches: an unknown outcome blocks a new instruction for the person in the run.
	StatesOfRunPerson(ctx context.Context, run, person uuid.UUID) ([]string, error)
	// Release writes a release and its lines and records each line as released with its instruction ID, audited as salary_batch.released (the file's name and hash, counts, never a name or account).
	Release(ctx context.Context, r SalaryRelease, lines []SalaryReleaseLine, at time.Time) (SalaryRelease, error)
	// Releases are a batch's releases, oldest first; ReleaseLines the lines that went out in them all.
	Releases(ctx context.Context, batch uuid.UUID) ([]SalaryRelease, error)
	ReleaseLines(ctx context.Context, batch uuid.UUID) ([]SalaryReleaseLine, error)
	// IsRunParty reports whether a Fyniq user is, by sign-in issuer and subject, the preparer or approver of the batch's run in Nemura (SOD-4): they neither approve nor release its salary batch.
	IsRunParty(ctx context.Context, batch, user uuid.UUID) (bool, error)
	// ReleaseOf is one release, or ports.ErrNotFound.
	ReleaseOf(ctx context.Context, id uuid.UUID) (SalaryRelease, error)
}
