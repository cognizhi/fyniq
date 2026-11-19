package storage

import (
	"context"

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
}
