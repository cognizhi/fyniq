package kpi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Where the customer counts of a month came from (FR-KPI-04): the source a KPI that uses them names.
const (
	CountsEntered   = "entered"                     // a person entered the month's counts (rev 0.161)
	CountsRecurring = "recurring invoice templates" // counted from the recurring invoices that were active at the month's end
)

// CustomerCount is a month's customer counts from one source: the paying customers at the month's end, those who started paying in the month and those who stopped.
type CustomerCount struct {
	Paying, New, Lost int64
	Source            string
}

// CustomerCounter answers a month's customer counts (FR-KPI-04, p2-reporting-planning design D10): the subscription schedules (FR-AR-15) and a CRM or billing
// integration answer through it as they arrive. ok is false when the source has nothing to say for the month, and the next source, then the entered counts,
// are used.
type CustomerCounter interface {
	Count(ctx context.Context, u storage.UnitOfWork, month time.Time) (c CustomerCount, ok bool, err error)
}

// Counters are the sources of customer counts, in order: the first that answers for a month wins, ahead of a person's entry (the owner's decision of
// 29 Oct 2026: the entries give way to the system's counts when those arrive).
var Counters = []CustomerCounter{RecurringInvoiceCounter{}}

// RecurringInvoiceCounter counts customers by their recurring invoices, until the subscription schedules exist (rev 0.540). The rule is conservative, and
// `[owner to confirm]`: a recurring invoice counts for a month only while it is active now, its schedule has started by the month's last day and has not ended
// before it; a paused or ended one counts for no month, because the books do not say when it was paused. A customer is paying at a month's end when any
// counted invoice names them; a customer new in a month is paying at its end and was not at the end of the month before, and a customer lost the other way
// round, so a customer's second invoice, or one that replaces another, is neither. The counter answers only from the first month a counted invoice
// starts in, and says nothing when the organisation has no active recurring invoice.
type RecurringInvoiceCounter struct{}

// Count implements CustomerCounter.
func (RecurringInvoiceCounter) Count(ctx context.Context, u storage.UnitOfWork, month time.Time) (CustomerCount, bool, error) {
	end := time.Date(month.Year(), month.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	before := time.Date(month.Year(), month.Month(), 0, 0, 0, 0, 0, time.UTC)
	now, prev := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	any, started := false, false
	var after *uuid.UUID
	afterName := ""
	for {
		page, err := u.RecurringInvoices().Page(ctx, afterName, after, 200)
		if err != nil {
			return CustomerCount{}, false, err
		}
		for _, r := range page {
			if r.Status != storage.RecurringActive {
				continue
			}
			var c struct {
				CustomerID uuid.UUID `json:"customer_id"`
			}
			if json.Unmarshal(r.Content, &c) != nil || c.CustomerID == uuid.Nil {
				continue
			}
			any = true
			if !r.Schedule.StartsOn.After(end) {
				started = true
			}
			at := func(day time.Time) bool {
				return !r.Schedule.StartsOn.After(day) && (r.Schedule.EndsOn == nil || !r.Schedule.EndsOn.Before(day))
			}
			if at(end) {
				now[c.CustomerID] = true
			}
			if at(before) {
				prev[c.CustomerID] = true
			}
		}
		if len(page) < 200 {
			break
		}
		last := page[len(page)-1]
		after, afterName = &last.ID, last.Name
	}
	if !any || !started {
		return CustomerCount{}, false, nil
	}
	out := CustomerCount{Paying: int64(len(now)), Source: CountsRecurring}
	for c := range now {
		if !prev[c] {
			out.New++
		}
	}
	for c := range prev {
		if !now[c] {
			out.Lost++
		}
	}
	return out, true, nil
}
