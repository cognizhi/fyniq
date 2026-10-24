package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ recurring invoices (FR-AR-07, FR-AUT-01, FR-AUT-02)
//
// The owner decided on 24 Oct 2026 that each recurring invoice makes a draft, the default, or issues and sends the
// invoice once a second person approved its content. Built in rev 0.135, provisional until sign-off: making drafts, as
// recurring invoices make theirs (rev 0.84), on the same monthly schedules or every week or two; each draft dated on its
// occurrence, and a recurring invoice paused and resumed skips the occurrences it missed. Issuing and sending follow.

// RecurringInvoiceMaker is the scheduler's principal for recurring invoices (FR-AUT-02): the database's
// create_scheduled_invoice_draft saves every draft it makes as "scheduler:invoices.recurring", and its audit events
// name it.
var RecurringInvoiceMaker = storage.Actor{Type: "scheduler", ID: "invoices.recurring"}

// RecurringInvoiceReason is the reason the scheduler's draft invoices carry.
const RecurringInvoiceReason = "Recurring invoice"

// RecurringInvoiceInput is what a person sets on a recurring invoice: its name, its schedule, and the content of each
// draft: an invoice's customer, reference, currency, lines and notes. A draft takes its date from its occurrence, its
// due date from the payment terms and its rate from the cascade when it is issued.
type RecurringInvoiceInput struct {
	Name     string
	Schedule storage.Schedule
	Content  invoice.Draft
	// Mode is storage.RecurringDraft, the default, or storage.RecurringIssueAndSend: the scheduler issues and sends each
	// draft once a second person approved the current content (the owner's decision of 24 Oct 2026).
	Mode string
}

// EncodeInvoiceContent is a recurring invoice's content as its version stores it: the invoice's payload without a
// date, a due date, a typed rate, a credited invoice, deposits or the recurring invoice it came from. A line naming a
// quote is refused: each occurrence would invoice the quote again.
func EncodeInvoiceContent(d invoice.Draft) ([]byte, error) {
	for i, l := range d.Lines {
		if l.Quote != nil {
			return nil, &invoice.Error{Code: invoice.CodeLine, Message: fmt.Sprintf("lines[%d] names a quote, which a recurring invoice's line does not", i+1),
				Fix: "Remove the quote from the line: invoice a quote from the quote itself"}
		}
	}
	d.Date, d.DueDate, d.Rate, d.Credits, d.Deposits, d.RecurringInvoice = time.Time{}, time.Time{}, nil, uuid.Nil, nil, nil
	return invoice.Encode(d)
}

// ReadInvoiceContent reads a recurring invoice's content as an invoice without a date.
func ReadInvoiceContent(b []byte, base money.Currency) (invoice.Draft, error) {
	return invoice.ReadContent(b, base)
}

// invoiceDraftOn is a recurring invoice's content as the draft of an occurrence on day: what
// create_scheduled_invoice_draft writes, for checking the content as the posting rules would.
func invoiceDraftOn(content []byte, day time.Time) (storage.Document, error) {
	var p map[string]any
	if err := json.Unmarshal(content, &p); err != nil {
		return storage.Document{}, err
	}
	p["date"] = day.Format(time.DateOnly)
	b, err := json.Marshal(p)
	return storage.Document{Type: storage.DocumentInvoice, Status: storage.DocumentDraft, Version: 1, Payload: b}, err
}

// CreateRecurring adds a recurring invoice, active, whose first draft is for the schedule's first occurrence on or after
// its start: one in the past is made by the next run.
func (s Invoices) CreateRecurring(ctx context.Context, org uuid.UUID, in RecurringInvoiceInput) (storage.RecurringInvoice, error) {
	next := FirstOn(in.Schedule, in.Schedule.StartsOn)
	if next == nil {
		return storage.RecurringInvoice{}, noOccurrence()
	}
	b, err := EncodeInvoiceContent(in.Content)
	if err != nil {
		return storage.RecurringInvoice{}, err
	}
	var out storage.RecurringInvoice
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		out, err = u.RecurringInvoices().Create(ctx, storage.RecurringInvoiceDetails{Name: in.Name, Status: storage.RecurringActive,
			Schedule: in.Schedule, NextOn: next, Content: b, Mode: in.Mode})
		return err
	})
	return out, err
}

// UpdateRecurring changes a recurring invoice at its version. Its content, when it changes, is a new version written by
// the actor; drafts already made keep theirs. A new schedule takes effect after the last occurrence made.
func (s Invoices) UpdateRecurring(ctx context.Context, org, id uuid.UUID, version int, in RecurringInvoiceInput) (storage.RecurringInvoice, error) {
	b, err := EncodeInvoiceContent(in.Content)
	if err != nil {
		return storage.RecurringInvoice{}, err
	}
	var out storage.RecurringInvoice
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.RecurringInvoices().Lock(ctx, id)
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
		out, err = u.RecurringInvoices().Update(ctx, id, version, storage.RecurringInvoiceDetails{Name: in.Name, Status: cur.Status,
			Schedule: in.Schedule, NextOn: next, Content: content, Mode: in.Mode})
		return err
	})
	return out, err
}

// PauseRecurring stops a recurring invoice making drafts.
func (s Invoices) PauseRecurring(ctx context.Context, org, id uuid.UUID, version int) (storage.RecurringInvoice, error) {
	return s.setRecurring(ctx, org, id, version, storage.RecurringPaused, time.Time{})
}

// ResumeRecurring starts a paused recurring invoice again; its next draft is for its first occurrence on or after today
// (the organisation's date) and after the last one made: the occurrences it missed while paused are skipped.
func (s Invoices) ResumeRecurring(ctx context.Context, org, id uuid.UUID, version int, today time.Time) (storage.RecurringInvoice, error) {
	return s.setRecurring(ctx, org, id, version, storage.RecurringActive, today)
}

func (s Invoices) setRecurring(ctx context.Context, org, id uuid.UUID, version int, to storage.RecurringStatus, today time.Time) (storage.RecurringInvoice, error) {
	var out storage.RecurringInvoice
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.RecurringInvoices().Lock(ctx, id)
		if err != nil {
			return err
		}
		if cur.Status == to || cur.NextOn == nil {
			state := "ended"
			if cur.NextOn != nil {
				state = string(cur.Status)
			}
			return &Error{CodeRecurringPaused, "the recurring invoice is " + state, "Pause an active recurring invoice, or resume a paused one"}
		}
		next := cur.NextOn
		if to == storage.RecurringActive {
			from := today
			if cur.LastOn != nil && !cur.LastOn.Before(from) {
				from = cur.LastOn.AddDate(0, 0, 1)
			}
			next = FirstOn(cur.Schedule, from)
		}
		out, err = u.RecurringInvoices().Update(ctx, id, version, storage.RecurringInvoiceDetails{Name: cur.Name, Status: to,
			Schedule: cur.Schedule, NextOn: next, Mode: cur.Mode})
		return err
	})
	return out, err
}

// RecurringInvoiceStatuses evaluates recurring invoices as recurring journals are (Appendix C.1 #28, #29; ENG-20, ENG-21):
// every problem the invoice rules find in the content, dated on its next occurrence (or today), as "Templates incomplete"
// (✖), except a missing rate, which the draft takes from the cascade when it is issued; then ⚠ when an occurrence before
// today has no draft yet; or OK, Paused or Ended. It reads in u and writes nothing.
func RecurringInvoiceStatuses(ctx context.Context, u storage.UnitOfWork, rs []storage.RecurringInvoice, today time.Time) (map[uuid.UUID][]Status, error) {
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
		doc, err := invoiceDraftOn(r.Content, on)
		if err != nil {
			return nil, err
		}
		var st []Status
		c, err := checkInvoice(ctx, u, doc, settings)
		var be *invoice.Error
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
		if r.Mode == storage.RecurringIssueAndSend && !r.IssuesAndSends() {
			st = append(st, status(SeverityWarning, StatusRecurringUnapproved, "mode",
				"It is set to issue and send, but its current content is not approved: it makes drafts until it is.",
				"Ask another person who holds the approval right on invoices to approve it"))
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

// MakeDue makes the draft invoice of every occurrence of org's active recurring invoices on or before day (FR-AR-07,
// FR-AUT-01), each in its own transaction, under the scheduler's principal (FR-AUT-02); at most maxCatchUp occurrences
// of one recurring invoice a run. A draft is dated on its occurrence; its checks say whether it can be issued. One that was
// paused, ended or changed meanwhile is left for the next run.
func (s Invoices) MakeDue(ctx context.Context, org uuid.UUID, day time.Time) (RecurringRun, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &RecurringInvoiceMaker, Reason: RecurringInvoiceReason})
	run := RecurringRun{Organisation: org, Day: day}
	var due []uuid.UUID
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		due, err = u.RecurringInvoices().Due(ctx, day.Format(time.DateOnly), MaxRecurringDue)
		return err
	}); err != nil {
		return run, err
	}
	for _, id := range due {
		for range maxCatchUp {
			made := false
			var r storage.RecurringInvoice
			var draft storage.Document
			var on time.Time
			err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				r, err = u.RecurringInvoices().Lock(ctx, id)
				if err != nil || r.Status != storage.RecurringActive || r.NextOn == nil || r.NextOn.After(day) {
					return err
				}
				on = *r.NextOn
				if draft, err = u.RecurringInvoices().MakeDraft(ctx, id, on, NextAfter(r.Schedule, on)); err != nil {
					return err
				}
				made = true
				if s.AfterMaking == nil || r.IssuesAndSends() {
					return nil // one the scheduler issues is listed for the weekly review instead
				}
				return s.AfterMaking(ctx, u, org, r.Name, draft, on)
			})
			if err == nil && made && r.IssuesAndSends() {
				issued, ierr := s.issueScheduled(ctx, org, r, draft, on)
				if ierr != nil {
					return run, fmt.Errorf("recurring invoice %s: %w", id, ierr)
				}
				if issued {
					run.Issued++
				}
			}
			if storage.CodeOf(err) == storage.CodeRecurring {
				break // changed meanwhile: the next run sees it as it is
			}
			if err != nil {
				return run, fmt.Errorf("recurring invoice %s: %w", id, err)
			}
			if !made {
				break
			}
			run.Made++
		}
	}
	return run, nil
}

// MakeDueInvoicesAll runs the recurring invoices of every organisation up to its own today, in its time zone, at now (the
// platform job invoices.recurring). An organisation that fails does not stop the others; their errors come back together.
func MakeDueInvoicesAll(ctx context.Context, store storage.Store, now time.Time) ([]RecurringRun, error) {
	return Invoices{Store: store}.MakeDueAll(ctx, now)
}

// MakeDueAll is MakeDueInvoicesAll with s's hooks.
func (s Invoices) MakeDueAll(ctx context.Context, now time.Time) ([]RecurringRun, error) {
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

// issueScheduled issues draft, made on on from recurring invoice r set to issue and send with its current content
// approved (FR-AR-07; the owner's decisions of 24 Oct 2026), as the scheduler, and lists it for the weekly review (FM-3),
// in one unit of work: the database lets the scheduler post only such a draft (FY201). A draft the invoice rules refuse
// stays a draft, and those who issue invoices are told, as for a draft made to be issued by a person; it answers false.
func (s Invoices) issueScheduled(ctx context.Context, org uuid.UUID, r storage.RecurringInvoice, draft storage.Document, on time.Time) (bool, error) {
	logged := on
	if s.Clock != nil {
		logged = s.Clock.Now()
	}
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := s.Post(ctx, org, draft.ID); err != nil {
			return err
		}
		doc, err := u.Documents().Get(ctx, draft.ID)
		if err != nil {
			return err
		}
		_, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewScheduledIssue, ObjectType: "invoice", ObjectKey: draft.ID.String(),
			Summary:  fmt.Sprintf("Issued %s of %s from the recurring invoice %q, content version %d", doc.Number, on.Format("2 Jan 2006"), r.Name, r.ContentVersion),
			LoggedAt: logged})
		return err
	})
	var probs ledger.Problems
	var ie *invoice.Error
	if !errors.As(err, &probs) && !errors.As(err, &ie) {
		return err == nil, err
	}
	if s.AfterMaking == nil {
		return false, nil
	}
	return false, s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		return s.AfterMaking(ctx, u, org, r.Name, draft, on)
	})
}
