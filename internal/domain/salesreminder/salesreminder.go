// Package salesreminder says which reminders are due to customers (PRD-001 FR-AR-08; p1-sales task 7.1; the owner's
// decision of 24 Oct 2026): an issued invoice still open is due a reminder on each day of its customer's schedule,
// counted from its due date; a sent quote about to expire, QuoteStep days before its validity date. While receipts are
// recorded by hand, nothing is sent by itself: each due reminder waits in the Inbox for a person to send or skip, and a
// document's step is decided once. Only the latest step due is offered: one decided at or after it settles the earlier
// ones.
package salesreminder

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The validation codes.
const (
	CodeDays   = "REMINDER-01" // a schedule's days are out of range, repeated, out of order or too many
	CodeNotDue = "REMINDER-02" // the reminder is not due: the document is settled, its step not reached, or decided
	CodeGroup  = "REMINDER-03" // the customer group is inactive, or its name is empty or too long
)

// Error is a refusal, in plain words, with how to fix it (ENG-23).
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Standard is the schedule of a customer in no group (the owner's decision of 24 Oct 2026): 3 days before the due date,
// on it, and 7 and 30 days after.
var Standard = []int{-3, 0, 7, 30}

// QuoteStep is when a sent quote or order is reminded of: 3 days before its validity date.
const QuoteStep = -3

// The bounds of a schedule.
const (
	MinDay  = -30
	MaxDay  = 365
	MaxDays = 6
)

// CheckDays checks a schedule: at most MaxDays days, each from MinDay to MaxDay, earliest first, none twice. No days is
// a schedule that sends no reminders.
func CheckDays(days []int) error {
	if len(days) > MaxDays {
		return &Error{CodeDays, fmt.Sprintf("the schedule has %d reminders", len(days)), fmt.Sprintf("Keep at most %d", MaxDays)}
	}
	for i, d := range days {
		if d < MinDay || d > MaxDay {
			return &Error{CodeDays, fmt.Sprintf("a reminder %d days from the due date is out of range", d),
				fmt.Sprintf("Use %d (before the due date) to %d (after it)", MinDay, MaxDay)}
		}
		if i > 0 && d <= days[i-1] {
			return &Error{CodeDays, "the schedule's days are repeated or out of order", "List each day once, earliest first, such as -3, 0, 7, 30"}
		}
	}
	return nil
}

// CheckName checks a customer group's name: 1 to 80 characters once trimmed.
func CheckName(name string) error {
	if n := len([]rune(strings.TrimSpace(name))); n < 1 || n > 80 {
		return &Error{CodeGroup, "the customer group's name is empty or longer than 80 characters", "Name the group in 1 to 80 characters"}
	}
	return nil
}

// Step is the latest day of days on which a document due on due is due a reminder by today: ok false when none is.
func Step(days []int, due, today time.Time) (int, bool) {
	step, ok := 0, false
	for _, d := range days {
		if !due.AddDate(0, 0, d).After(today) {
			step, ok = d, true
		}
	}
	return step, ok
}

// The kinds of document reminded of.
const (
	KindInvoice = "invoice"
	KindQuote   = "quote"
)

// Reminder is a reminder due to a customer as at a day.
type Reminder struct {
	Kind         string // invoice, or quote for a quote or sales order
	DocumentID   uuid.UUID
	Number       string
	CustomerID   uuid.UUID
	CustomerName string
	Recipient    string // the customer's billing email; empty when it has none
	DueOn        time.Time
	Step         int
	DaysOverdue  int         // after an invoice's due date; 0 before it and for a quote
	Amount       money.Money // what is still open on an invoice; a quote's total
}

// Overdue says whether the reminder chases an invoice past its due date.
func (r Reminder) Overdue() bool { return r.Kind == KindInvoice && r.DaysOverdue > 0 }

// Due are the reminders due to customers as at today: each issued invoice with something open whose customer's schedule
// has a step reached and not decided, and each sent quote or order within QuoteStep days of its validity date and not
// answered, not reminded of. Earliest due first.
func Due(ctx context.Context, u storage.UnitOfWork, base money.Currency, today time.Time) ([]Reminder, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := u.CustomerGroups().List(ctx)
	if err != nil {
		return nil, err
	}
	schedules := map[uuid.UUID][]int{}
	for _, g := range groups {
		schedules[g.ID] = g.ReminderDays
	}
	contacts := map[uuid.UUID]storage.Contact{}
	contact := func(id uuid.UUID) (storage.Contact, error) {
		if c, ok := contacts[id]; ok {
			return c, nil
		}
		c, err := u.Contacts().Get(ctx, id)
		if err == nil {
			contacts[id] = c
		}
		return c, err
	}
	var out []Reminder
	for _, acc := range accounts {
		if string(acc.Role) != ledger.RoleAccountsReceivable {
			continue
		}
		l, err := receivables.Open(ctx, u, base, acc.ID, today)
		if err != nil {
			return nil, err
		}
		for _, cu := range l.Customers {
			c, err := contact(cu.ID)
			if err != nil {
				return nil, err
			}
			days := Standard
			if c.CustomerGroupID != nil {
				days = schedules[*c.CustomerGroupID]
			}
			for _, it := range cu.Items {
				if it.Kind != receivables.KindInvoice || it.Open.Sign() <= 0 || it.DueDate.IsZero() {
					continue
				}
				step, ok := Step(days, it.DueDate, today)
				if !ok {
					continue
				}
				r := Reminder{Kind: KindInvoice, DocumentID: it.DocumentID, Number: it.Number, CustomerID: cu.ID, CustomerName: cu.Name,
					Recipient: strings.TrimSpace(c.BillingEmail), DueOn: it.DueDate, Step: step, Amount: it.Open}
				if late := int(today.Sub(it.DueDate).Hours() / 24); late > 0 {
					r.DaysOverdue = late
				}
				out = append(out, r)
			}
		}
	}
	quotes, err := quotesDue(ctx, u, base, today, contact)
	if err != nil {
		return nil, err
	}
	out = append(out, quotes...)
	ids := make([]uuid.UUID, 0, len(out))
	for _, r := range out {
		ids = append(ids, r.DocumentID)
	}
	decided, err := u.SalesReminders().Of(ctx, ids)
	if err != nil {
		return nil, err
	}
	out = slices.DeleteFunc(out, func(r Reminder) bool {
		return slices.ContainsFunc(decided[r.DocumentID], func(d storage.SalesReminder) bool { return d.Step >= r.Step })
	})
	slices.SortStableFunc(out, func(a, b Reminder) int {
		if c := a.DueOn.AddDate(0, 0, a.Step).Compare(b.DueOn.AddDate(0, 0, b.Step)); c != 0 {
			return c
		}
		return strings.Compare(a.Number, b.Number)
	})
	return out, nil
}

// quotesDue are the sent quotes and orders, not answered, within QuoteStep days of their validity date as at today.
func quotesDue(ctx context.Context, u storage.UnitOfWork, base money.Currency, today time.Time,
	contact func(uuid.UUID) (storage.Contact, error)) ([]Reminder, error) {
	var out []Reminder
	for _, t := range []storage.DocumentType{storage.DocumentQuote, storage.DocumentSalesOrder} {
		f := storage.DocumentFilter{Type: t, Status: storage.DocumentPosted, To: today.Format(time.DateOnly), Limit: 500}
		for {
			docs, err := u.Documents().Page(ctx, f)
			if err != nil {
				return nil, err
			}
			ids := make([]uuid.UUID, 0, len(docs))
			for _, doc := range docs {
				ids = append(ids, doc.ID)
			}
			answered, err := u.QuoteOutcomes().Of(ctx, ids)
			if err != nil {
				return nil, err
			}
			for _, doc := range docs {
				if _, ok := answered[doc.ID]; ok {
					continue
				}
				d, err := quote.Read(doc, base)
				if err != nil {
					return nil, err
				}
				if d.ValidUntil.IsZero() || d.ValidUntil.Before(today) {
					continue
				}
				if step, ok := Step([]int{QuoteStep}, d.ValidUntil, today); ok {
					qp, _, err := journal.CheckQuote(ctx, u, doc)
					if err != nil {
						return nil, err
					}
					c, err := contact(d.Content.CustomerID)
					if err != nil {
						return nil, err
					}
					out = append(out, Reminder{Kind: KindQuote, DocumentID: doc.ID, Number: doc.Number, CustomerID: c.ID, CustomerName: c.LegalName,
						Recipient: strings.TrimSpace(c.BillingEmail), DueOn: d.ValidUntil, Step: step, Amount: qp.Invoice.Totals.Total})
				}
			}
			if len(docs) < f.Limit {
				break
			}
			last := docs[len(docs)-1]
			f.AfterDate, f.AfterID = last.Date, &last.ID
		}
	}
	return out, nil
}

// Find is the reminder due on document id at step among due: CodeNotDue when there is none.
func Find(due []Reminder, id uuid.UUID, step int) (Reminder, error) {
	for _, r := range due {
		if r.DocumentID == id {
			if r.Step != step {
				return Reminder{}, &Error{CodeNotDue, fmt.Sprintf("the reminder due on %s is the one %d days from its due date, not %d", r.Number, r.Step, step),
					"Send or skip the reminder the Inbox lists"}
			}
			return r, nil
		}
	}
	return Reminder{}, &Error{CodeNotDue, "no reminder is due on that document: it is settled, answered, not yet due, or its reminder was sent or skipped",
		"Send or skip a reminder the Inbox lists"}
}
