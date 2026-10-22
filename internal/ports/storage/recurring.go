package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RecurringStatus is whether a recurring journal makes its drafts: active, or paused by a person. One with no next
// occurrence has ended, whatever its status.
type RecurringStatus string

// The recurring journal statuses.
const (
	RecurringActive RecurringStatus = "active"
	RecurringPaused RecurringStatus = "paused"
)

// Schedule is when a recurring journal recurs (FR-GL-02, FR-AR-07): every EveryMonths months (1, 3, 6 or 12) on
// DayOfMonth (1 to 31; the 29th to the 31st are the last day of a shorter month), from StartsOn, until EndsOn when set.
type Schedule struct {
	EveryMonths int
	DayOfMonth  int
	StartsOn    time.Time
	EndsOn      *time.Time
}

// RecurringJournal is a recurring journal at its version, with its current content (FR-GL-02; PRD-001 rev 0.60).
type RecurringJournal struct {
	ID       uuid.UUID
	Name     string
	Status   RecurringStatus
	Schedule Schedule
	// NextOn is the next occurrence the scheduler makes a draft for; nil once the schedule has ended.
	NextOn *time.Time
	// Version is the version of the whole record, for If-Match; ContentVersion the version of its content.
	Version        int
	ContentVersion int
	// Content is the current content version: a journal's memo and lines as a document version stores them, and
	// "reverses" when each draft reverses on the first day of the next month. EditedBy wrote it, at EditedAt.
	Content  []byte
	EditedBy string
	EditedAt time.Time
	// LastOn and LastJournalID are the last occurrence a draft was made for, and that draft.
	LastOn        *time.Time
	LastJournalID *uuid.UUID
}

// RecurringJournalDetails is what a person sets on a recurring journal. Content nil on an update keeps the content;
// any other content is saved as its next version, written by the unit of work's actor.
type RecurringJournalDetails struct {
	Name     string
	Status   RecurringStatus
	Schedule Schedule
	NextOn   *time.Time
	Content  []byte
}

// RecurringJournalVersion is one version of a recurring journal's content, which never changes (FY058).
type RecurringJournalVersion struct {
	Version  int
	Content  []byte
	EditedBy string
	EditedAt time.Time
}

// RecurringJournals are the organisation's recurring journals (FR-GL-02, FR-AUT-01). Every change is audited.
type RecurringJournals interface {
	// Create adds a recurring journal at version 1 with its first content version.
	Create(ctx context.Context, d RecurringJournalDetails) (RecurringJournal, error)
	Get(ctx context.Context, id uuid.UUID) (RecurringJournal, error)
	// Lock reads a recurring journal and locks it until the unit of work ends, so what is computed from it holds.
	Lock(ctx context.Context, id uuid.UUID) (RecurringJournal, error)
	// Update changes a recurring journal at its version and returns it at its next: CodeVersion when it changed since.
	Update(ctx context.Context, id uuid.UUID, version int, d RecurringJournalDetails) (RecurringJournal, error)
	// Page returns at most limit recurring journals by name, then ID, after the cursor's name and ID when afterID is
	// set.
	Page(ctx context.Context, afterName string, afterID *uuid.UUID, limit int) ([]RecurringJournal, error)
	// Version is one version of a recurring journal's content.
	Version(ctx context.Context, id uuid.UUID, version int) (RecurringJournalVersion, error)
	// Due lists the active recurring journals whose next occurrence is on or before day (YYYY-MM-DD), earliest first:
	// at most limit of them.
	Due(ctx context.Context, day string, limit int) ([]uuid.UUID, error)
	// MakeDraft makes the draft journal of a recurring journal's next occurrence, on, as the scheduler does (the
	// database's create_scheduled_draft: the current content, dated on, saved by "scheduler:journals.recurring", no
	// number until it posts; CodeRecurring when on is not its active next occurrence), and moves its next occurrence to
	// next (nil when the schedule has ended).
	MakeDraft(ctx context.Context, id uuid.UUID, on time.Time, next *time.Time) (Document, error)
}

// RecurringBill is a recurring bill at its version, with its current content (FR-AP-09; PRD-001 rev 0.84): kept as a
// recurring journal is, whose content is a bill's and whose drafts are bills.
type RecurringBill struct {
	ID       uuid.UUID
	Name     string
	Status   RecurringStatus
	Schedule Schedule
	// NextOn is the next occurrence the scheduler makes a draft for; nil once the schedule has ended.
	NextOn *time.Time
	// Version is the version of the whole record, for If-Match; ContentVersion the version of its content.
	Version        int
	ContentVersion int
	// Content is the current content version: a bill's supplier, currency, lines and notes as a document version stores
	// them, without a date, a due date or the supplier's number. EditedBy wrote it, at EditedAt.
	Content  []byte
	EditedBy string
	EditedAt time.Time
	// LastOn and LastBillID are the last occurrence a draft was made for, and that draft bill.
	LastOn     *time.Time
	LastBillID *uuid.UUID
}

// RecurringBillDetails is what a person sets on a recurring bill. Content nil on an update keeps the content; any other
// content is saved as its next version, written by the unit of work's actor.
type RecurringBillDetails struct {
	Name     string
	Status   RecurringStatus
	Schedule Schedule
	NextOn   *time.Time
	Content  []byte
}

// RecurringBillVersion is one version of a recurring bill's content, which never changes (FY058).
type RecurringBillVersion struct {
	Version  int
	Content  []byte
	EditedBy string
	EditedAt time.Time
}

// RecurringBills are the organisation's recurring bills (FR-AP-09, FR-AUT-01). Every change is audited.
type RecurringBills interface {
	// Create adds a recurring bill at version 1 with its first content version.
	Create(ctx context.Context, d RecurringBillDetails) (RecurringBill, error)
	Get(ctx context.Context, id uuid.UUID) (RecurringBill, error)
	// Lock reads a recurring bill and locks it until the unit of work ends, so what is computed from it holds.
	Lock(ctx context.Context, id uuid.UUID) (RecurringBill, error)
	// Update changes a recurring bill at its version and returns it at its next: CodeVersion when it changed since.
	Update(ctx context.Context, id uuid.UUID, version int, d RecurringBillDetails) (RecurringBill, error)
	// Page returns at most limit recurring bills by name, then ID, after the cursor's name and ID when afterID is set.
	Page(ctx context.Context, afterName string, afterID *uuid.UUID, limit int) ([]RecurringBill, error)
	// Version is one version of a recurring bill's content.
	Version(ctx context.Context, id uuid.UUID, version int) (RecurringBillVersion, error)
	// Due lists the active recurring bills whose next occurrence is on or before day (YYYY-MM-DD), earliest first: at
	// most limit of them.
	Due(ctx context.Context, day string, limit int) ([]uuid.UUID, error)
	// MakeDraft makes the draft bill of a recurring bill's next occurrence, on, as the scheduler does (the database's
	// create_scheduled_bill_draft: the current content, dated on, saved by "scheduler:bills.recurring", no number until
	// it posts; CodeRecurring when on is not its active next occurrence), and moves its next occurrence to next (nil
	// when the schedule has ended).
	MakeDraft(ctx context.Context, id uuid.UUID, on time.Time, next *time.Time) (Document, error)
}
