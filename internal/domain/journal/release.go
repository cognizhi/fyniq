package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Releaser is the scheduler's principal for the releases of spread costs (FR-AUT-02, FR-GL-03): its own audit identity,
// recorded as "scheduler:bills.releases" on every release, whether the hourly job posts it or the posting of its bill.
var Releaser = storage.Actor{Type: "scheduler", ID: "bills.releases"}

// ActionReleaseWaiting is the audit event of a release that a run could not post: no open period covers its date, or
// the rule refuses it (its account was deactivated). The next run tries it again.
const ActionReleaseWaiting = "bill.release_waiting"

// ReleaseReason is the reason the releases carry.
const ReleaseReason = "Release of a spread cost"

// ReleaseRun is what a run of the releases did in one organisation.
type ReleaseRun struct {
	Organisation uuid.UUID
	Day          time.Time // the organisation's day the run released up to
	Released     int
	Waiting      int
}

// Releases posts the monthly releases of bills' spread costs (FR-GL-03, ENG-05; rev 0.83) against a store: each release
// in its own unit of work, under the scheduler's principal, with the bill locked so that a void or an edit of it and
// its release never interleave (ENG-47).
type Releases struct {
	Store storage.Store
}

// errSettled is a release that needs nothing any more: posted, or its bill voided or edited, meanwhile.
var errSettled = errors.New("the release is posted, or its bill changed")

// billSchedule is the release schedule of a posted bill version (ledger.BillReleases): its spread lines as saved, and
// what the entry it posted put on 1210 for each.
func billSchedule(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) ([]ledger.BillRelease, error) {
	d, err := bill.Read(doc, s.Base)
	if err != nil {
		return nil, err
	}
	spread := false
	b := ledger.Bill{ID: doc.ID, Version: doc.Version, Date: d.Date, Currency: d.Currency}
	for _, l := range d.Lines {
		x := l.Spread(d.Date)
		spread = spread || x != nil
		b.Lines = append(b.Lines, ledger.BillLine{AccountID: l.AccountID, Description: l.Description, Spread: x})
	}
	if !spread || doc.Type != storage.DocumentBill {
		return nil, nil
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill))
	if err != nil {
		return nil, err
	}
	e, err := fromStorage(stored, s.Base)
	if err != nil {
		return nil, err
	}
	b.Memo = e.Memo // the bill as its entry named it
	return ledger.BillReleases(b, e, s)
}

// postedRelease is the entry a release posted under any rule version the engine has, or ports.ErrNotFound.
func postedRelease(ctx context.Context, u storage.UnitOfWork, r ledger.BillRelease) (storage.Entry, error) {
	for rule := range ledger.BillReleaseRules {
		e, err := u.Ledger().ByIdentity(ctx, r.Identity(rule))
		if !errors.Is(err, ports.ErrNotFound) {
			return e, err
		}
	}
	return storage.Entry{}, ports.ErrNotFound
}

// reverseReleases reverses in u every release a bill version posted and nothing has reversed yet, each dated by on from
// the release's entry, with a reason: what a void or an edit of the bill does with them.
func reverseReleases(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, on func(ledger.Entry) time.Time,
	reason string) error {
	rs, err := billSchedule(ctx, u, doc, s)
	if err != nil {
		return err
	}
	for _, r := range rs {
		stored, err := postedRelease(ctx, u, r)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(stored.PostingIdentity)); err == nil {
			continue
		} else if !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		original, err := fromStorage(stored, s.Base)
		if err != nil {
			return err
		}
		reversal, err := ledger.Reverse(original, on(original), reason)
		if err != nil {
			return err
		}
		if _, err := u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return err
		}
	}
	return nil
}

// ReleaseBill posts the releases of one posted bill dated on or before day (ENG-05: up to the current period, never the
// future), under the scheduler's principal: what a run does for the bill, and what posting or editing it does at once.
func (s Releases) ReleaseBill(ctx context.Context, org, id uuid.UUID, day time.Time) (ReleaseRun, error) {
	return s.release(storage.WithAudit(ctx, storage.AuditContext{Actor: &Releaser, Reason: ReleaseReason}), org, id, day)
}

func (s Releases) release(ctx context.Context, org, id uuid.UUID, day time.Time) (ReleaseRun, error) {
	run := ReleaseRun{Organisation: org, Day: day}
	var due []ledger.BillRelease
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil || doc.Type != storage.DocumentBill || doc.Status != storage.DocumentPosted {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		rs, err := billSchedule(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		for _, r := range rs {
			if r.Date.After(day) || r.Amount.IsZero() {
				continue // the future is not posted (ENG-05); a month whose part is zero releases nothing
			}
			if _, err := postedRelease(ctx, u, r); errors.Is(err, ports.ErrNotFound) {
				due = append(due, r)
			} else if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return run, err
	}
	for _, r := range due {
		err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1: a void or an edit of the bill waits for its release
			if err != nil {
				return err
			}
			if doc.Status != storage.DocumentPosted || doc.Version != r.Version {
				return errSettled
			}
			if _, err := postedRelease(ctx, u, r); err == nil {
				return errSettled
			} else if !errors.Is(err, ports.ErrNotFound) {
				return err
			}
			settings, err := loadSettings(ctx, u)
			if err != nil {
				return err
			}
			e, err := ledger.PostBillRelease(r, settings)
			if err != nil {
				return err
			}
			_, err = u.Ledger().Append(ctx, toStorage(e, nil))
			return err
		})
		var probs ledger.Problems
		switch code := storage.CodeOf(err); {
		case err == nil:
			run.Released++
		case errors.Is(err, errSettled):
		case code == storage.CodePeriodClosed || code == storage.CodeNoPeriod || errors.As(err, &probs):
			run.Waiting++
			why := code
			if why == "" {
				why = err.Error()
			}
			if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionReleaseWaiting, ObjectType: "document", ObjectID: id,
					Changes: map[string][2]any{"release": {nil, r.Identity(ledger.RuleBillRelease)}, "date": {nil, r.Date.Format(time.DateOnly)},
						"waiting_for": {nil, why}}})
			}); err != nil {
				return run, err
			}
		default:
			return run, fmt.Errorf("bill %s, release of %s: %w", id, r.Month.Format("2006-01"), err)
		}
	}
	return run, nil
}

// ReleaseDue posts every release dated on or before day of the organisation's posted bills (FR-GL-03, ENG-05): the
// hourly job bills.releases. It looks only at bills with a release a period not closed can take; one that cannot post
// waits, with an audit event that says why, and the next run tries it again.
func (s Releases) ReleaseDue(ctx context.Context, org uuid.UUID, day time.Time) (ReleaseRun, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Releaser, Reason: ReleaseReason})
	run := ReleaseRun{Organisation: org, Day: day}
	from := ""
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		periods, err := u.Periods().List(ctx)
		for _, p := range periods {
			if p.Status != storage.PeriodClosed {
				from = p.StartsOn.Format(time.DateOnly)
				break
			}
		}
		return err
	}); err != nil || from == "" {
		return run, err // no period that is not closed: nothing can post
	}
	var after *uuid.UUID
	for {
		var ids []uuid.UUID
		if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			ids, err = u.Documents().SpreadBills(ctx, from, after, MaxDue)
			return err
		}); err != nil {
			return run, err
		}
		for _, id := range ids {
			r, err := s.release(ctx, org, id, day)
			run.Released += r.Released
			run.Waiting += r.Waiting
			if err != nil {
				return run, err
			}
		}
		if len(ids) < MaxDue {
			return run, nil
		}
		after = &ids[len(ids)-1]
	}
}

// ReleaseDueAll runs the releases of every organisation up to its own today, in its time zone, at now. An organisation
// that fails does not stop the others; their errors come back together.
func ReleaseDueAll(ctx context.Context, store storage.Store, now time.Time) ([]ReleaseRun, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	s := Releases{Store: store}
	var runs []ReleaseRun
	var errs []error
	for _, org := range orgs {
		day, err := todayIn(ctx, store, org, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		run, err := s.ReleaseDue(ctx, org, day)
		runs = append(runs, run)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return runs, errors.Join(errs...)
}

// replayBillRelease posts a stored release again under the rule that posted e (ENG-06): the release of the bill
// version's schedule that has e's identity.
func replayBillRelease(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	rs, err := billSchedule(ctx, u, doc, s)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, r := range rs {
		if r.Identity(e.RuleVersion) == e.PostingIdentity {
			return ledger.ReplayBillRelease(e.RuleVersion, r, s)
		}
	}
	return ledger.Entry{}, fmt.Errorf("the schedule of %s version %d has no release %s", doc.Number, doc.Version, e.PostingIdentity)
}

// The states of a release, as a bill shows it (ENG-05: the future is a projection, never mixed with actuals).
const (
	ReleasePosted    = "posted"    // in the ledger
	ReleaseReversed  = "reversed"  // posted, and reversed by a void or an edit of its bill
	ReleaseWaiting   = "waiting"   // due by the organisation's today, not posted yet: the hourly job posts it, or its period is not open
	ReleaseProjected = "projected" // in the future, or of a bill not posted: not in the ledger
)

// ReleaseState is one release of a bill's schedule and its state, with its entry once posted.
type ReleaseState struct {
	Release ledger.BillRelease
	Status  string
	EntryID uuid.UUID
}

// ReleasesOf is the release schedule of a bill's current version and each release's state on today, leaving out the
// months whose part is zero: for a draft, what posting it would release, all projected; nothing for a vendor credit, a
// bill without a spread, or a draft that cannot post.
func ReleasesOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, today time.Time) ([]ReleaseState, error) {
	if doc.Type != storage.DocumentBill {
		return nil, nil
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	var rs []ledger.BillRelease
	if doc.Status == storage.DocumentDraft {
		c, err := checkBill(ctx, u, doc, settings)
		if err != nil || len(c.Problems) > 0 {
			return nil, err
		}
		if rs, err = ledger.BillReleases(c.Prepared.Bill, c.Entry, settings); err != nil {
			return nil, err
		}
	} else if rs, err = billSchedule(ctx, u, doc, settings); err != nil {
		return nil, err
	}
	out := make([]ReleaseState, 0, len(rs))
	for _, r := range rs {
		if r.Amount.IsZero() {
			continue // a month whose part is zero releases nothing
		}
		x := ReleaseState{Release: r, Status: ReleaseProjected}
		if doc.Status != storage.DocumentDraft {
			e, err := postedRelease(ctx, u, r)
			switch {
			case err == nil:
				x.Status, x.EntryID = ReleasePosted, e.ID
				if _, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(e.PostingIdentity)); err == nil {
					x.Status = ReleaseReversed
				} else if !errors.Is(err, ports.ErrNotFound) {
					return nil, err
				}
			case !errors.Is(err, ports.ErrNotFound):
				return nil, err
			case doc.Status == storage.DocumentPosted && !r.Date.After(today):
				x.Status = ReleaseWaiting
			}
		}
		out = append(out, x)
	}
	return out, nil
}
