package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ recurring bills (FR-AP-09, FR-AUT-01, FR-AUT-02)
//
// The owner decided on 22 Oct 2026 that the scheduler makes each occurrence of a recurring bill as a draft, which a
// person completes with the supplier's number and posts under the usual approvals, as recurring journals are made
// (PRD-001 rev 0.58, 0.60). Built in rev 0.84, provisional until sign-off: the same schedule (FR-AR-07), each draft
// dated on its occurrence, and a recurring bill paused and resumed skips the occurrences it missed.

// RecurringBillMaker is the scheduler's principal for recurring bills (FR-AUT-02): the database's
// create_scheduled_bill_draft saves every draft it makes as "scheduler:bills.recurring", and its audit events name it.
var RecurringBillMaker = storage.Actor{Type: "scheduler", ID: "bills.recurring"}

// RecurringBillReason is the reason the scheduler's draft bills carry.
const RecurringBillReason = "Recurring bill"

// RecurringBillInput is what a person sets on a recurring bill: its name, its schedule, and the content of each draft:
// a bill's supplier, currency, lines and notes. A draft takes its date from its occurrence, its due date from the
// payment terms and its rate from the cascade when it posts; the supplier's number comes with each invoice.
type RecurringBillInput struct {
	Name     string
	Schedule storage.Schedule
	Content  bill.Draft
}

// EncodeBillContent is a recurring bill's content as its version stores it: the bill's payload without a date, a due
// date, the supplier's number, a typed rate or the recurring bill it came from.
func EncodeBillContent(d bill.Draft) ([]byte, error) {
	d.Date, d.DueDate, d.SupplierRef, d.Rate, d.RecurringBill = time.Time{}, time.Time{}, "", nil, nil
	return bill.Encode(d)
}

// ReadBillContent reads a recurring bill's content as a bill without a date.
func ReadBillContent(b []byte, base money.Currency) (bill.Draft, error) {
	return bill.Read(storage.Document{Type: storage.DocumentBill, Payload: b}, base)
}

// billDraftOn is a recurring bill's content as the draft of an occurrence on day: what create_scheduled_bill_draft
// writes, for checking the content as the posting rules would.
func billDraftOn(content []byte, day time.Time) (storage.Document, error) {
	var p map[string]any
	if err := json.Unmarshal(content, &p); err != nil {
		return storage.Document{}, err
	}
	p["date"] = day.Format(time.DateOnly)
	b, err := json.Marshal(p)
	return storage.Document{Type: storage.DocumentBill, Status: storage.DocumentDraft, Version: 1, Payload: b}, err
}

// CreateRecurring adds a recurring bill, active, whose first draft is for the schedule's first occurrence on or after
// its start: one in the past is made by the next run.
func (s Bills) CreateRecurring(ctx context.Context, org uuid.UUID, in RecurringBillInput) (storage.RecurringBill, error) {
	next := FirstOn(in.Schedule, in.Schedule.StartsOn)
	if next == nil {
		return storage.RecurringBill{}, noOccurrence()
	}
	b, err := EncodeBillContent(in.Content)
	if err != nil {
		return storage.RecurringBill{}, err
	}
	var out storage.RecurringBill
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		out, err = u.RecurringBills().Create(ctx, storage.RecurringBillDetails{Name: in.Name, Status: storage.RecurringActive,
			Schedule: in.Schedule, NextOn: next, Content: b})
		return err
	})
	return out, err
}

// UpdateRecurring changes a recurring bill at its version. Its content, when it changes, is a new version written by
// the actor; drafts already made keep theirs. A new schedule takes effect after the last occurrence made.
func (s Bills) UpdateRecurring(ctx context.Context, org, id uuid.UUID, version int, in RecurringBillInput) (storage.RecurringBill, error) {
	b, err := EncodeBillContent(in.Content)
	if err != nil {
		return storage.RecurringBill{}, err
	}
	var out storage.RecurringBill
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.RecurringBills().Lock(ctx, id)
		if err != nil {
			return err
		}
		content := b
		if sameJSON(cur.Content, b) {
			content = nil
		}
		next := cur.NextOn
		if !sameSchedule(cur.Schedule, in.Schedule) {
			from := in.Schedule.StartsOn
			if cur.LastOn != nil && !cur.LastOn.Before(from) {
				from = cur.LastOn.AddDate(0, 0, 1)
			}
			if next = FirstOn(in.Schedule, from); next == nil && cur.LastOn == nil {
				return noOccurrence()
			}
		}
		out, err = u.RecurringBills().Update(ctx, id, version, storage.RecurringBillDetails{Name: in.Name, Status: cur.Status,
			Schedule: in.Schedule, NextOn: next, Content: content})
		return err
	})
	return out, err
}

// PauseRecurring stops a recurring bill making drafts.
func (s Bills) PauseRecurring(ctx context.Context, org, id uuid.UUID, version int) (storage.RecurringBill, error) {
	return s.setRecurring(ctx, org, id, version, storage.RecurringPaused, time.Time{})
}

// ResumeRecurring starts a paused recurring bill again; its next draft is for its first occurrence on or after today
// (the organisation's date) and after the last one made: the occurrences it missed while paused are skipped.
func (s Bills) ResumeRecurring(ctx context.Context, org, id uuid.UUID, version int, today time.Time) (storage.RecurringBill, error) {
	return s.setRecurring(ctx, org, id, version, storage.RecurringActive, today)
}

func (s Bills) setRecurring(ctx context.Context, org, id uuid.UUID, version int, to storage.RecurringStatus, today time.Time) (storage.RecurringBill, error) {
	var out storage.RecurringBill
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.RecurringBills().Lock(ctx, id)
		if err != nil {
			return err
		}
		if cur.Status == to || cur.NextOn == nil {
			state := "ended"
			if cur.NextOn != nil {
				state = string(cur.Status)
			}
			return &Error{CodeRecurringPaused, "the recurring bill is " + state, "Pause an active recurring bill, or resume a paused one"}
		}
		next := cur.NextOn
		if to == storage.RecurringActive {
			from := today
			if cur.LastOn != nil && !cur.LastOn.Before(from) {
				from = cur.LastOn.AddDate(0, 0, 1)
			}
			next = FirstOn(cur.Schedule, from)
		}
		out, err = u.RecurringBills().Update(ctx, id, version, storage.RecurringBillDetails{Name: cur.Name, Status: to,
			Schedule: cur.Schedule, NextOn: next})
		return err
	})
	return out, err
}

// RecurringBillStatuses evaluates recurring bills as recurring journals are (Appendix C.1 #28, #29; ENG-20, ENG-21):
// every problem the bill rules find in the content, dated on its next occurrence (or today), as "Templates incomplete"
// (✖), except a missing rate, which the draft takes from the cascade when it posts; then ⚠ when an occurrence before
// today has no draft yet; or OK, Paused or Ended. It reads in u and writes nothing.
func RecurringBillStatuses(ctx context.Context, u storage.UnitOfWork, rs []storage.RecurringBill, today time.Time) (map[uuid.UUID][]Status, error) {
	out := map[uuid.UUID][]Status{}
	if len(rs) == 0 {
		return out, nil
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		on := today
		if r.NextOn != nil {
			on = *r.NextOn
		}
		doc, err := billDraftOn(r.Content, on)
		if err != nil {
			return nil, err
		}
		var st []Status
		c, err := checkBill(ctx, u, doc, settings)
		var be *bill.Error
		switch {
		case errors.As(err, &be):
			st = append(st, status(SeverityError, be.Code, "", be.Message, be.Fix))
		case err != nil:
			return nil, err
		}
		for _, p := range c.Problems {
			if p.Code == ledger.CodeMissingRate || p.Code == fx.CodeNoRate {
				continue
			}
			st = append(st, status(SeverityError, p.Code, p.Field, p.Message, p.Fix))
		}
		switch {
		case r.NextOn == nil:
			st = append(st, status(SeverityOK, StatusRecurringEnded, "", "Its schedule has ended: it makes no more drafts.", ""))
		case r.Status == storage.RecurringPaused:
			st = append(st, status(SeverityOK, StatusRecurringPaused, "", "It is paused: it makes no drafts until it is resumed.", ""))
		case r.NextOn.Before(today):
			st = append(st, status(SeverityWarning, StatusRecurringDue, "next_on", "Its draft for "+r.NextOn.Format(time.DateOnly)+" is not made yet.",
				"The scheduler makes it within the hour; if it stays, the scheduler may have failed: tell an administrator"))
		case len(st) == 0:
			st = append(st, status(SeverityOK, StatusRecurringReady, "", "Its next draft is made on "+r.NextOn.Format(time.DateOnly)+".", ""))
		}
		out[r.ID] = st
	}
	return out, nil
}

// MakeDue makes the draft bill of every occurrence of org's active recurring bills on or before day (FR-AP-09,
// FR-AUT-01), each in its own transaction, under the scheduler's principal (FR-AUT-02); at most maxCatchUp occurrences
// of one recurring bill a run. A draft is dated on its occurrence; its checks say whether it can post. One that was
// paused, ended or changed meanwhile is left for the next run.
func (s Bills) MakeDue(ctx context.Context, org uuid.UUID, day time.Time) (RecurringRun, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &RecurringBillMaker, Reason: RecurringBillReason})
	run := RecurringRun{Organisation: org, Day: day}
	var due []uuid.UUID
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		due, err = u.RecurringBills().Due(ctx, day.Format(time.DateOnly), MaxRecurringDue)
		return err
	}); err != nil {
		return run, err
	}
	for _, id := range due {
		for range maxCatchUp {
			made := false
			err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				r, err := u.RecurringBills().Lock(ctx, id)
				if err != nil || r.Status != storage.RecurringActive || r.NextOn == nil || r.NextOn.After(day) {
					return err
				}
				draft, err := u.RecurringBills().MakeDraft(ctx, id, *r.NextOn, NextAfter(r.Schedule, *r.NextOn))
				if err != nil {
					return err
				}
				made = true
				if s.AfterMaking == nil {
					return nil
				}
				return s.AfterMaking(ctx, u, org, r.Name, draft, *r.NextOn)
			})
			if storage.CodeOf(err) == storage.CodeRecurring {
				break // changed meanwhile: the next run sees it as it is
			}
			if err != nil {
				return run, fmt.Errorf("recurring bill %s: %w", id, err)
			}
			if !made {
				break
			}
			run.Made++
		}
	}
	return run, nil
}

// MakeDueBillsAll runs the recurring bills of every organisation up to its own today, in its time zone, at now (the
// platform job bills.recurring). An organisation that fails does not stop the others; their errors come back together.
func MakeDueBillsAll(ctx context.Context, store storage.Store, now time.Time) ([]RecurringRun, error) {
	return Bills{Store: store}.MakeDueAll(ctx, now)
}

// MakeDueAll is MakeDueBillsAll with s's hooks.
func (s Bills) MakeDueAll(ctx context.Context, now time.Time) ([]RecurringRun, error) {
	store := s.Store
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	var runs []RecurringRun
	var errs []error
	for _, org := range orgs {
		day, err := todayIn(ctx, store, org, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		run, err := s.MakeDue(ctx, org, day)
		runs = append(runs, run)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return runs, errors.Join(errs...)
}
