package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// AutoReverser is the scheduler's principal for automatic reversals (FR-AUT-02): its own audit identity, recorded as
// "scheduler:journals.auto-reverse" on everything it posts.
var AutoReverser = storage.Actor{Type: "scheduler", ID: "journals.auto-reverse"}

// ActionReversalWaiting is the audit event of an automatic reversal that a run could not post: its period is closed,
// or not open yet. The next run tries it again.
const ActionReversalWaiting = "journal.reversal_waiting"

// AutoReversalReason is the reason the automatic reversals carry.
const AutoReversalReason = "Automatic reversal"

// MaxDue is how many due reversals a run takes in one organisation; the rest wait for the next run.
const MaxDue = 500

// DueRun is what a run of the automatic reversals did in one organisation.
type DueRun struct {
	Organisation uuid.UUID
	Day          time.Time // the organisation's day the run reversed up to
	Reversed     int
	Waiting      int
}

// ReverseDue posts the automatic reversal of every posted journal of org whose reversal date is on or before day
// (FR-GL-02), each in its own transaction, under the scheduler's principal (FR-AUT-02). A reversal is dated on its
// reversal date. One whose period is closed, or has no period yet, waits (period locks apply): an audit event says
// why, and the next run tries it again. The reversal needs no approval of its own: it was part of the journal when the
// journal posted, under the journal's approval.
func (s Service) ReverseDue(ctx context.Context, org uuid.UUID, day time.Time) (DueRun, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &AutoReverser, Reason: AutoReversalReason})
	run := DueRun{Organisation: org, Day: day}
	var due []uuid.UUID
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		due, err = u.Documents().DueReversals(ctx, day.Format(time.DateOnly), MaxDue)
		return err
	}); err != nil {
		return run, err
	}
	for _, id := range due {
		on, err := s.reverseOn(ctx, org, id)
		if err != nil {
			return run, err
		}
		_, err = s.Reverse(ctx, org, id, on, AutoReversalReason)
		var je *Error
		switch code := storage.CodeOf(err); {
		case err == nil:
			run.Reversed++
		case errors.As(err, &je) && je.Code == CodeNotPosted:
			// Reversed or edited meanwhile: nothing is due any more.
		case code == storage.CodePeriodClosed || code == storage.CodeNoPeriod:
			run.Waiting++
			if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionReversalWaiting, ObjectType: "document", ObjectID: id,
					Changes: map[string][2]any{"reverse_on": {nil, on.Format(time.DateOnly)}, "waiting_for": {nil, code}}})
			}); err != nil {
				return run, err
			}
		default:
			return run, fmt.Errorf("journal %s: %w", id, err)
		}
	}
	return run, nil
}

// reverseOn is the automatic reversal date of a journal's current version.
func (s Service) reverseOn(ctx context.Context, org, id uuid.UUID) (time.Time, error) {
	var on time.Time
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		saved, err := ReadSaved(doc)
		if err != nil {
			return err
		}
		on, err = time.Parse(time.DateOnly, saved.ReverseOn)
		return err
	})
	return on, err
}

// ReverseDueAll runs the automatic reversals of every organisation up to its own today, in its time zone, at now. An
// organisation that fails does not stop the others; their errors come back together.
func ReverseDueAll(ctx context.Context, store storage.Store, now time.Time) ([]DueRun, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	s := Service{Store: store}
	var runs []DueRun
	var errs []error
	for _, org := range orgs {
		var day time.Time
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			loc, err := time.LoadLocation(o.Profile.TimeZone)
			if err != nil || o.Profile.TimeZone == "" {
				loc = time.UTC
			}
			y, m, d := now.In(loc).Date()
			day = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		run, err := s.ReverseDue(ctx, org, day)
		runs = append(runs, run)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return runs, errors.Join(errs...)
}
