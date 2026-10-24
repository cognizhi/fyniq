package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CustomerGroup is a group of customers and the reminder schedule they follow (FR-AR-08): the days, before (negative)
// or after an invoice's due date, on which an invoice still open is due a reminder, earliest first. A group with no
// days sends none. A group is never deleted; one no longer used is made inactive.
type CustomerGroup struct {
	ID           uuid.UUID
	Name         string
	ReminderDays []int
	Active       bool
	Version      int
}

// CustomerGroups are the organisation's customer groups. Every create and change is audited.
type CustomerGroups interface {
	// List is every group, by name.
	List(ctx context.Context) ([]CustomerGroup, error)
	Get(ctx context.Context, id uuid.UUID) (CustomerGroup, error)
	Create(ctx context.Context, name string, reminderDays []int) (CustomerGroup, error)
	// Update changes a group at its version, and returns it at its next: CodeVersion when it changed since.
	Update(ctx context.Context, id uuid.UUID, version int, name string, reminderDays []int, active bool) (CustomerGroup, error)
}

// The outcomes of a reminder (FR-AR-08).
const (
	ReminderSent    = "sent"
	ReminderSkipped = "skipped"
)

// SalesReminder is a person's decision on a reminder due to a customer (FR-AR-08): sent, with the send that carried
// it, or skipped, with a reason if given. A document's step is decided once and the decision never changes (FY203).
type SalesReminder struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	Step       int // days from the invoice's due date, or the quote's validity date
	Outcome    string
	SendID     *uuid.UUID
	Reason     string
	DecidedBy  string
	DecidedAt  time.Time
}

// NewSalesReminder is a decision to record, as the unit of work's actor.
type NewSalesReminder struct {
	DocumentID uuid.UUID
	Step       int
	Outcome    string
	SendID     *uuid.UUID
	Reason     string
}

// SalesReminders are the reminders decided. Recording one is audited.
type SalesReminders interface {
	// Record keeps a decision: CodeConflict when the document's step was decided already.
	Record(ctx context.Context, r NewSalesReminder) (SalesReminder, error)
	// Of is the decisions on each of documents, by step.
	Of(ctx context.Context, documents []uuid.UUID) (map[uuid.UUID][]SalesReminder, error)
}
