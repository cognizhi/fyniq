package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CreditApplications applies supplier credits to bills (FR-AP-03, FR-AR-18) against a store: each in one unit of work,
// the document, its entry when the two carrying amounts differ (credit_application@1) and its two allocations together
// (ENG-02). An application is undone by a void: a reversing entry and reversing allocations, which reopen the bill and
// the credit (FR-GL-12).
type CreditApplications struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// Applied is a written application: its document, its entry (empty when it posted nothing) and its two allocations.
type Applied struct {
	Application storage.Document
	Entry       storage.Entry
	Allocations []storage.Allocation
}

// Apply applies a credit to a bill in one transaction: both locked first, the document, the entry when the carrying
// amounts differ, the allocation that settles the bill (with the realised FX) and the one that settles the credit, and
// the document's change to posted. An application the rules refuse writes nothing and answers every problem.
func (s CreditApplications) Apply(ctx context.Context, org uuid.UUID, d credits.Draft) (Applied, error) {
	b, err := credits.Encode(d)
	if err != nil {
		return Applied{}, err
	}
	var out Applied
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := lockBills(ctx, u, []uuid.UUID{d.BillID, d.CreditID}); err != nil {
			return err
		}
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCreditApplication, Payload: b})
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		p, probs, err := credits.Prepare(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		entry, applied, err := ledger.PostCreditApplication(p.Application, settings)
		if ps, ok := err.(ledger.Problems); ok {
			probs = append(probs, ps...)
		} else if err != nil {
			return err
		}
		if len(probs) > 0 {
			return probs
		}
		if len(entry.Lines) > 0 {
			if out.Entry, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
				return err
			}
		}
		cur := d.Amount.Currency().Code
		for _, x := range []struct {
			document uuid.UUID
			base, fx money.Money
		}{{d.BillID, applied.Bill, applied.RealisedFX}, {d.CreditID, applied.Credit, money.Zero(settings.Base)}} {
			a, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: x.document, On: d.Date,
				Currency: cur, Amount: storage.Decimal(d.Amount.String()), BaseCurrency: settings.Base.Code, BaseAmount: storage.Decimal(x.base.String()),
				RealisedFX: storage.Decimal(x.fx.String())})
			if err != nil {
				return err
			}
			out.Allocations = append(out.Allocations, a)
		}
		if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out.Application = doc
		if len(entry.Lines) > 0 {
			return s.after(ctx, u, doc, out.Entry)
		}
		return nil
	})
	return out, err
}

// Void undoes a posted application on date, with a reason: its entry, if it posted one, is reversed, and each of its
// allocations is reversed on that date, which reopens the bill and the credit by the amount applied. The date is not
// before the application's, and its period must be open (INV-03).
func (s CreditApplications) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (Applied, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out Applied
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		d, err := credits.Read(doc)
		if err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &credits.Error{Code: credits.CodeNotPosted, Message: fmt.Sprintf("the application is %s, not posted", doc.Status), Fix: "Only a posted application is voided"}
		}
		y, m, dd := date.Date()
		date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		if date.Before(d.Date) {
			return &credits.Error{Code: credits.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the application's %s", date.Format(time.DateOnly), d.Date.Format(time.DateOnly)),
				Fix: "Date the void on or after the application"}
		}
		if err := lockBills(ctx, u, []uuid.UUID{d.BillID, d.CreditID}); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.CreditApplicationIdentity(doc.ID, doc.Version, ledger.RuleCreditApplication))
		switch {
		case errors.Is(err, ports.ErrNotFound): // it posted nothing: there is nothing to reverse
		case err != nil:
			return err
		default:
			original, err := fromStorage(stored, settings.Base)
			if err != nil {
				return err
			}
			reversal, err := ledger.Reverse(original, date, reason)
			if err != nil {
				return err
			}
			if out.Entry, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
				return err
			}
		}
		allocs, err := u.Allocations().OfSource(ctx, doc.ID)
		if err != nil {
			return err
		}
		for _, a := range allocs {
			if a.ReversesID != nil {
				continue
			}
			reverses := a.ID
			r, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID, On: date,
				Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount), RealisedFX: negated(a.RealisedFX),
				ReversesID: &reverses})
			if err != nil {
				return err
			}
			out.Allocations = append(out.Allocations, r)
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out.Application = doc
		if out.Entry.ID != uuid.Nil {
			return s.after(ctx, u, doc, out.Entry)
		}
		return nil
	})
	return out, err
}

func (s CreditApplications) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayCreditApplication posts a stored application again under the rule that posted e (ENG-06), with the rates its
// entry kept and the carrying amounts its allocations cleared, so a later application or void never makes it differ.
func replayCreditApplication(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	d, err := credits.Read(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	a := ledger.CreditApplication{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: stored.Memo, Amount: d.Amount}
	var rates []money.Rate // the bill's line, then the credit's: on 2010, with an amount
	for _, l := range stored.Lines {
		if s.Accounts[l.AccountID].Role == ledger.RoleAccountsPayable && !l.Amount.IsZero() {
			rates = append(rates, l.Rate)
		}
	}
	if len(rates) != 2 {
		return ledger.Entry{}, fmt.Errorf("the application's entry has %d lines with an amount on accounts payable, not 2", len(rates))
	}
	a.BillRate, a.CreditRate = rates[0], rates[1]
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, x := range allocs {
		if x.ReversesID != nil || x.SourceVersion != doc.Version {
			continue
		}
		base, err := money.ParseStored(string(x.BaseAmount), s.Base)
		if err != nil {
			return ledger.Entry{}, err
		}
		if x.DocumentID == d.BillID {
			a.BillClears = &base
		} else {
			a.CreditClears = &base
		}
	}
	entry, _, err := ledger.ReplayCreditApplication(e.RuleVersion, a, s)
	return entry, err
}
