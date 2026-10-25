package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Releaser, InvoiceReleaser and BankReleaser are the scheduler's principals for the releases of spreads (FR-AUT-02,
// FR-GL-03), each its own audit identity, recorded on every release whether the hourly job posts it or the posting of
// its document: "scheduler:bills.releases" for bills' spread costs, "scheduler:invoices.releases" for invoices' spread
// revenue, "scheduler:bank.releases" for categorised bank lines' spreads (Appendix B.1).
var (
	Releaser        = storage.Actor{Type: "scheduler", ID: "bills.releases"}
	InvoiceReleaser = storage.Actor{Type: "scheduler", ID: "invoices.releases"}
	BankReleaser    = storage.Actor{Type: "scheduler", ID: "bank.releases"}
)

// ActionReleaseWaiting and ActionInvoiceReleaseWaiting are the audit events of a release that a run could not post: no
// open period covers its date, or the rule refuses it (its account was deactivated). The next run tries it again.
const (
	ActionReleaseWaiting        = "bill.release_waiting"
	ActionInvoiceReleaseWaiting = "invoice.release_waiting"
	ActionBankReleaseWaiting    = "bank_transaction.release_waiting"
)

// ReleaseReason, InvoiceReleaseReason and BankReleaseReason are the reasons the releases carry.
const (
	ReleaseReason        = "Release of a spread cost"
	InvoiceReleaseReason = "Release of spread revenue"
	BankReleaseReason    = "Release of a spread bank line"
)

// spreading is what releasing a document type's spreads needs: the principal and reason it posts with, and the audit
// event of a release that waits.
type spreading struct {
	actor   storage.Actor
	reason  string
	waiting string
}

// spreads are the document types whose lines spread (FR-GL-03): bills' costs, invoices' revenue and categorised bank
// lines' parts.
var spreads = map[storage.DocumentType]spreading{
	storage.DocumentBill:            {Releaser, ReleaseReason, ActionReleaseWaiting},
	storage.DocumentInvoice:         {InvoiceReleaser, InvoiceReleaseReason, ActionInvoiceReleaseWaiting},
	storage.DocumentBankTransaction: {BankReleaser, BankReleaseReason, ActionBankReleaseWaiting},
}

// ReleaseRun is what a run of the releases did in one organisation.
type ReleaseRun struct {
	Organisation uuid.UUID
	Day          time.Time // the organisation's day the run released up to
	Released     int
	Waiting      int
}

// Releases posts the monthly releases of bills' spread costs (FR-GL-03, ENG-05; rev 0.83) and invoices' spread revenue
// (rev 0.118) against a store: each release in its own unit of work, under the scheduler's principal, with the document
// locked so that a void or an edit of it and its release never interleave (ENG-47).
type Releases struct {
	Store storage.Store
}

// errSettled is a release that needs nothing any more: posted, or its document voided or edited, meanwhile.
var errSettled = errors.New("the release is posted, or its bill changed")

// billSchedule is the release schedule of a posted bill version (ledger.BillReleases): its spread lines as saved, and
// what the entry it posted put on 1210 for each.
func billSchedule(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) ([]ledger.Release, error) {
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

// invoiceSchedule is the release schedule of an issued invoice version (ledger.InvoiceReleases): its spread lines as
// saved, and what the entry it posted put on 2400 for each.
func invoiceSchedule(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) ([]ledger.Release, error) {
	d, err := invoice.Read(doc, s.Base)
	if err != nil {
		return nil, err
	}
	spread := false
	i := ledger.Invoice{ID: doc.ID, Version: doc.Version, Date: d.Date, Currency: d.Currency}
	for _, l := range d.Lines {
		x := l.Spread(d.Date)
		spread = spread || x != nil
		i.Lines = append(i.Lines, ledger.InvoiceLine{AccountID: l.AccountID, Description: l.Description, Spread: x})
	}
	if !spread {
		return nil, nil
	}
	stored, err := u.Ledger().ByIdentity(ctx, InvoiceIdentityOf(doc))
	if err != nil {
		return nil, err
	}
	e, err := fromStorage(stored, s.Base)
	if err != nil {
		return nil, err
	}
	i.Memo = e.Memo // the invoice as its entry named it
	return ledger.InvoiceReleases(i, e, s)
}

// scheduleOf is the release schedule of a posted document version: a bill's, an invoice's or a bank transaction's;
// nothing for another type.
func scheduleOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) ([]ledger.Release, error) {
	switch doc.Type {
	case storage.DocumentBill:
		return billSchedule(ctx, u, doc, s)
	case storage.DocumentInvoice:
		return invoiceSchedule(ctx, u, doc, s)
	case storage.DocumentBankTransaction:
		return bankSchedule(ctx, u, doc, s)
	}
	return nil, nil
}

// postedRelease is the entry a release posted under any rule version the engine has, or ports.ErrNotFound.
func postedRelease(ctx context.Context, u storage.UnitOfWork, r ledger.Release) (storage.Entry, error) {
	for rule := range ledger.ReleaseRules(r.Source) {
		e, err := u.Ledger().ByIdentity(ctx, r.Identity(rule))
		if !errors.Is(err, ports.ErrNotFound) {
			return e, err
		}
	}
	return storage.Entry{}, ports.ErrNotFound
}

// reverseReleases reverses in u every release a document version posted and nothing has reversed yet, each dated by on
// from the release's entry, with a reason: what a void or an edit of the bill or invoice does with them.
func reverseReleases(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, on func(ledger.Entry) time.Time,
	reason string) error {
	rs, err := scheduleOf(ctx, u, doc, s)
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
	return s.release(withSpreading(ctx, storage.DocumentBill), org, id, day, storage.DocumentBill)
}

// ReleaseInvoice posts the releases of one issued invoice dated on or before day (ENG-05), under the scheduler's
// principal for invoices: what a run does for the invoice, and what issuing or editing it does at once.
func (s Releases) ReleaseInvoice(ctx context.Context, org, id uuid.UUID, day time.Time) (ReleaseRun, error) {
	return s.release(withSpreading(ctx, storage.DocumentInvoice), org, id, day, storage.DocumentInvoice)
}

// ReleaseBankTransaction posts the releases of one categorised bank line dated on or before day (ENG-05), under the
// scheduler's principal for bank lines: what a run does for it, and what categorising the line does at once.
func (s Releases) ReleaseBankTransaction(ctx context.Context, org, id uuid.UUID, day time.Time) (ReleaseRun, error) {
	return s.release(withSpreading(ctx, storage.DocumentBankTransaction), org, id, day, storage.DocumentBankTransaction)
}

// withSpreading is ctx acting as the scheduler's principal for releasing a document type's spreads.
func withSpreading(ctx context.Context, t storage.DocumentType) context.Context {
	k := spreads[t]
	return storage.WithAudit(ctx, storage.AuditContext{Actor: &k.actor, Reason: k.reason})
}

func (s Releases) release(ctx context.Context, org, id uuid.UUID, day time.Time, t storage.DocumentType) (ReleaseRun, error) {
	run := ReleaseRun{Organisation: org, Day: day}
	var due []ledger.Release
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil || doc.Type != t || doc.Status != storage.DocumentPosted {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		rs, err := scheduleOf(ctx, u, doc, settings)
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
			doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1: a void or an edit of the document waits for its release
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
			e, err := ledger.ReleaseRules(r.Source)[ledger.CurrentRelease(r.Source)](r, settings)
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
				return u.Audit().Record(ctx, storage.AuditRecord{Action: spreads[t].waiting, ObjectType: "document", ObjectID: id,
					Changes: map[string][2]any{"release": {nil, r.Identity(ledger.CurrentRelease(r.Source))}, "date": {nil, r.Date.Format(time.DateOnly)},
						"waiting_for": {nil, why}}})
			}); err != nil {
				return run, err
			}
		default:
			return run, fmt.Errorf("%s %s, release of %s: %w", t, id, r.Month.Format("2006-01"), err)
		}
	}
	return run, nil
}

// ReleaseDue posts every release dated on or before day of the organisation's posted bills (FR-GL-03, ENG-05): the
// hourly job bills.releases. It looks only at bills with a release a period not closed can take; one that cannot post
// waits, with an audit event that says why, and the next run tries it again.
func (s Releases) ReleaseDue(ctx context.Context, org uuid.UUID, day time.Time) (ReleaseRun, error) {
	return s.releaseDue(ctx, org, day, storage.DocumentBill)
}

// ReleaseInvoicesDue posts every release dated on or before day of the organisation's issued invoices (FR-GL-03,
// ENG-05; rev 0.118): the hourly job invoices.releases, as ReleaseDue is for bills.
func (s Releases) ReleaseInvoicesDue(ctx context.Context, org uuid.UUID, day time.Time) (ReleaseRun, error) {
	return s.releaseDue(ctx, org, day, storage.DocumentInvoice)
}

func (s Releases) releaseDue(ctx context.Context, org uuid.UUID, day time.Time, t storage.DocumentType) (ReleaseRun, error) {
	ctx = withSpreading(ctx, t)
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
			ids, err = u.Documents().Spread(ctx, t, from, after, MaxDue)
			return err
		}); err != nil {
			return run, err
		}
		for _, id := range ids {
			r, err := s.release(ctx, org, id, day, t)
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

// ReleaseBankTransactionsDue posts every release dated on or before day of the organisation's categorised bank lines
// (FR-GL-03, ENG-05; Appendix B.1): the hourly job bank.releases, as ReleaseDue is for bills.
func (s Releases) ReleaseBankTransactionsDue(ctx context.Context, org uuid.UUID, day time.Time) (ReleaseRun, error) {
	return s.releaseDue(ctx, org, day, storage.DocumentBankTransaction)
}

// ReleaseDueAll runs the bills' releases of every organisation up to its own today, in its time zone, at now. An
// organisation that fails does not stop the others; their errors come back together.
func ReleaseDueAll(ctx context.Context, store storage.Store, now time.Time) ([]ReleaseRun, error) {
	return releaseDueAll(ctx, store, now, Releases.ReleaseDue)
}

// ReleaseInvoicesDueAll runs the invoices' releases of every organisation up to its own today, as ReleaseDueAll does
// the bills'.
func ReleaseInvoicesDueAll(ctx context.Context, store storage.Store, now time.Time) ([]ReleaseRun, error) {
	return releaseDueAll(ctx, store, now, Releases.ReleaseInvoicesDue)
}

// ReleaseBankTransactionsDueAll runs the categorised bank lines' releases of every organisation up to its own today, as
// ReleaseDueAll does the bills'.
func ReleaseBankTransactionsDueAll(ctx context.Context, store storage.Store, now time.Time) ([]ReleaseRun, error) {
	return releaseDueAll(ctx, store, now, Releases.ReleaseBankTransactionsDue)
}

func releaseDueAll(ctx context.Context, store storage.Store, now time.Time,
	due func(Releases, context.Context, uuid.UUID, time.Time) (ReleaseRun, error)) ([]ReleaseRun, error) {
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
		run, err := due(s, ctx, org, day)
		runs = append(runs, run)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return runs, errors.Join(errs...)
}

// replayRelease posts a stored release again under the rule that posted e (ENG-06): the release of the document
// version's schedule that has e's identity.
func replayRelease(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	rs, err := scheduleOf(ctx, u, doc, s)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, r := range rs {
		if r.Identity(e.RuleVersion) == e.PostingIdentity {
			return ledger.ReplayRelease(e.RuleVersion, r, s)
		}
	}
	return ledger.Entry{}, fmt.Errorf("the schedule of %s version %d has no release %s", doc.Number, doc.Version, e.PostingIdentity)
}

// The states of a release, as a bill or an invoice shows it (ENG-05: the future is a projection, never mixed with
// actuals).
const (
	ReleasePosted    = "posted"    // in the ledger
	ReleaseReversed  = "reversed"  // posted, and reversed by a void or an edit of its document
	ReleaseWaiting   = "waiting"   // due by the organisation's today, not posted yet: the hourly job posts it, or its period is not open
	ReleaseProjected = "projected" // in the future, or of a bill not posted: not in the ledger
)

// ReleaseState is one release of a document's schedule and its state, with its entry once posted.
type ReleaseState struct {
	Release ledger.Release
	Status  string
	EntryID uuid.UUID
}

// ReleasesOf is the release schedule of a bill's or an invoice's current version and each release's state on today,
// leaving out the months whose part is zero: for a draft, what posting it would release, all projected; nothing for
// another type, a document without a spread, or a draft that cannot post.
func ReleasesOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, today time.Time) ([]ReleaseState, error) {
	if _, ok := spreads[doc.Type]; !ok {
		return nil, nil
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	var rs []ledger.Release
	switch {
	case doc.Status == storage.DocumentDraft && doc.Type == storage.DocumentBill:
		c, err := checkBill(ctx, u, doc, settings)
		if err != nil || len(c.Problems) > 0 {
			return nil, err
		}
		if rs, err = ledger.BillReleases(c.Prepared.Bill, c.Entry, settings); err != nil {
			return nil, err
		}
	case doc.Status == storage.DocumentDraft && doc.Type == storage.DocumentInvoice:
		c, err := checkInvoice(ctx, u, doc, settings)
		if err != nil || len(c.Problems) > 0 {
			return nil, err
		}
		if rs, err = ledger.InvoiceReleases(c.Prepared.Invoice, c.Entry, settings); err != nil {
			return nil, err
		}
	case doc.Status == storage.DocumentDraft: // a bank transaction is posted as it is made
	default:
		if rs, err = scheduleOf(ctx, u, doc, settings); err != nil {
			return nil, err
		}
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
