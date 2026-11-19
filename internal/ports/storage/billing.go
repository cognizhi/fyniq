package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// What a project has billed to its customer (PRD-001 FR-PRJ-10; p1-projects task 5.2). The marks are written by the database when an
// invoice made from the project's unbilled work is posted and released when it is voided (FY606): this port only reads them.

// The kinds of work a project bills.
const (
	BilledTime      = "time"      // an approved time entry
	BilledExpense   = "expense"   // a line of a posted bill or claim
	BilledMilestone = "milestone" // a fixed-fee milestone or a retainer's period
)

// CodeBilled: the work an invoice bills is not that of an approved, billable entry or posted bill or claim of the project, or an invoice already
// bills it (FY606).
const CodeBilled = "STORAGE-BILL-01"

// BilledItem is a piece of work an invoice billed, and the invoice.
type BilledItem struct {
	Kind      string
	RefID     uuid.UUID // the time entry, the bill or claim document, or the milestone
	Line      int       // the line of a bill or claim; 0 otherwise
	InvoiceID uuid.UUID
}

// Billing reads what has been billed.
type Billing interface {
	// Billed lists what a project's posted, not voided invoices bill.
	Billed(ctx context.Context, project uuid.UUID) ([]BilledItem, error)
	// BillableDocuments lists, by ID after a cursor, at most limit posted bills and claims that have a billable line carrying the project as a
	// dimension value (FR-PRJ-08).
	BillableDocuments(ctx context.Context, project uuid.UUID, after *uuid.UUID, limit int) ([]uuid.UUID, error)
	// Milestones lists a project's milestones or retainer periods, oldest first, each with the invoice that billed it, if one did.
	Milestones(ctx context.Context, project uuid.UUID) ([]Milestone, error)
	// GetMilestone is one, ports.ErrNotFound for none.
	GetMilestone(ctx context.Context, id uuid.UUID) (Milestone, error)
	// CreateMilestone saves one under its project; the database refuses one on a project of another billing method, or a closed one (CodeMilestone).
	CreateMilestone(ctx context.Context, m NewMilestone) (Milestone, error)
	// AcceptMilestone records the customer's acceptance, once, at its version: CodeVersion when it changed since.
	AcceptMilestone(ctx context.Context, id uuid.UUID, version int, on time.Time) (Milestone, error)
}

// The kinds of milestone.
const (
	MilestoneFixedFee = "milestone" // a fixed-fee project's milestone: invoiced once it is accepted
	MilestoneRetainer = "retainer"  // a retainer's period: invoiced without acceptance, and time is drawn down against it
)

// CodeMilestone: a milestone on a project of another billing method or a closed one, one changed after it was saved, or accepted twice (FY607).
const CodeMilestone = "STORAGE-MILESTONE-01"

// Milestone is a part of a fixed fee, or a retainer's period, and what became of it.
type Milestone struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	Kind       string
	Name       string
	Amount     Decimal // in the project's currency
	DueOn      *time.Time
	AcceptedOn *time.Time
	Version    int
	InvoiceID  *uuid.UUID // the posted invoice that billed it
	CreatedAt  time.Time
}

// NewMilestone is a milestone as entered.
type NewMilestone struct {
	ProjectID uuid.UUID
	Kind      string
	Name      string
	Amount    Decimal
	DueOn     *time.Time
}
