package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The month-end revaluation's posting (FR-GL-04; PRD-001 rev 0.73): fx works out what a period's revaluation is, and
// the posting service posts it, as it posts every document (the one posting path).

// RevaluationReverser is the scheduler's principal for the revaluations' reversals that wait (FR-AUT-02), recorded as
// "scheduler:fx.reversals" on everything it posts.
var RevaluationReverser = storage.Actor{Type: "scheduler", ID: "fx.reversals"}

// RevaluationReversalReason is the reason a revaluation's reversal carries.
const RevaluationReversalReason = "FX revaluation reversed on the next period's first day"

// PostRevaluation revalues a period (FR-GL-04) in u: under the ledger lock it works out the plan, records it as a
// revaluation document, posts its entry dated the period's last day, and the entry's reversal on the next period's first
// day when that period is open; otherwise the reversal waits for the scheduler (ReverseRevaluations). A run with nothing
// to change posts no entry and still records that the period was revalued at the revision it read. Refused as
// fx.Plan.Refusal says.
func PostRevaluation(ctx context.Context, u storage.UnitOfWork, periodID uuid.UUID, today time.Time) (fx.Run, error) {
	if _, err := u.Ledger().Lock(ctx); err != nil { // ENG-47: nothing posts while the balances are read and revalued
		return fx.Run{}, err
	}
	plan, err := fx.Prepare(ctx, u, periodID)
	if err != nil {
		return fx.Run{}, err
	}
	if err := plan.Refusal(today); err != nil {
		return fx.Run{}, err
	}
	payload, err := plan.Record()
	if err != nil {
		return fx.Run{}, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: fx.DocumentFXRevaluation, Payload: payload})
	if err != nil {
		return fx.Run{}, err
	}
	run := fx.Run{Document: doc, Revision: plan.Revision, ReverseOn: plan.ReverseOn, Current: true}
	if len(plan.Entry.Lines) > 0 {
		r, settings, err := fx.ReadRevaluation(ctx, u, doc)
		if err != nil {
			return fx.Run{}, err
		}
		entry, err := ledger.Revalue(r, settings)
		if err != nil {
			return fx.Run{}, err
		}
		stored, err := u.Ledger().Append(ctx, toStorage(entry, nil))
		if err != nil {
			return fx.Run{}, err
		}
		run.Entry = &stored
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return fx.Run{}, err
	}
	run.Document.Status = storage.DocumentPosted
	if run.Entry == nil {
		return run, nil
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return fx.Run{}, err
	}
	for _, x := range all {
		if x.StartsOn.Equal(plan.ReverseOn) && x.Status == storage.PeriodOpen {
			if run.Reversal, err = reverseRevaluation(ctx, u, doc, *run.Entry, plan.ReverseOn); err != nil {
				return fx.Run{}, err
			}
			run.Document.Status = storage.DocumentReversed
		}
	}
	return run, nil
}

// reverseRevaluation posts the reversal of a revaluation's entry on on, and marks the document reversed.
func reverseRevaluation(ctx context.Context, u storage.UnitOfWork, doc storage.Document, stored storage.Entry, on time.Time) (*storage.Entry, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return nil, err
	}
	e, err := fromStorage(stored, base)
	if err != nil {
		return nil, err
	}
	back, err := ledger.Reverse(e, on, RevaluationReversalReason)
	if err != nil {
		return nil, err
	}
	out, err := u.Ledger().Append(ctx, toStorage(back, &stored.ID))
	if err != nil {
		return nil, err
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentReversed); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevaluationReversals is what a run of the waiting revaluation reversals did in one organisation.
type RevaluationReversals struct {
	Organisation uuid.UUID
	Reversed     int
	Waiting      int
}

// ReverseRevaluations posts the reversal of every posted revaluation of org whose reverse-on date is on or before day
// and whose reversal waited, each in its own transaction, under the scheduler's principal (FR-AUT-02). One whose period
// is not open yet keeps waiting, with an audit event that says so.
func ReverseRevaluations(ctx context.Context, store storage.Store, org uuid.UUID, day time.Time) (RevaluationReversals, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &RevaluationReverser, Reason: RevaluationReversalReason})
	run := RevaluationReversals{Organisation: org}
	var due []uuid.UUID
	if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: fx.DocumentFXRevaluation, Status: storage.DocumentPosted,
			To: day.AddDate(0, 0, -1).Format(time.DateOnly), Limit: 500})
		for _, d := range docs {
			due = append(due, d.ID)
		}
		return err
	}); err != nil {
		return run, err
	}
	for _, id := range due {
		reversed := false
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil || doc.Status != storage.DocumentPosted {
				return err
			}
			r, err := fx.RunOf(ctx, u, doc)
			if err != nil || r.Entry == nil || r.Reversal != nil {
				return err // nothing changed, so nothing to reverse
			}
			_, err = reverseRevaluation(ctx, u, doc, *r.Entry, r.ReverseOn)
			reversed = err == nil
			return err
		})
		switch code := storage.CodeOf(err); {
		case err == nil:
			if reversed {
				run.Reversed++
			}
		case code == storage.CodePeriodClosed || code == storage.CodeNoPeriod:
			run.Waiting++
			if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Audit().Record(ctx, storage.AuditRecord{Action: "fx_revaluation.reversal_waiting", ObjectType: "document", ObjectID: id,
					Changes: map[string][2]any{"waiting_for": {nil, code}}})
			}); err != nil {
				return run, err
			}
		default:
			return run, fmt.Errorf("revaluation %s: %w", id, err)
		}
	}
	return run, nil
}

// ReverseRevaluationsAll runs the waiting revaluation reversals of every organisation up to its own today, in its time
// zone, at now (the platform job fx.reversals). An organisation that fails does not stop the others; their errors come
// back together.
func ReverseRevaluationsAll(ctx context.Context, store storage.Store, now time.Time) ([]RevaluationReversals, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	var runs []RevaluationReversals
	var errs []error
	for _, org := range orgs {
		day, err := todayIn(ctx, store, org, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		r, err := ReverseRevaluations(ctx, store, org, day)
		runs = append(runs, r)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return runs, errors.Join(errs...)
}
