package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CashAdvances records money advanced to members before a trip, and what is left of it refunded (FR-AP-17, UC-EXP-03;
// the owner's decision of 26 Oct 2026, rev 0.148): each in one unit of work, the document and its entry from
// cash_advance@1 together, with a refund's allocation to the advance it refunds (ENG-02). The member's claims settle an
// advance when they post (Claims.Post). Either is undone by a void, a reversing entry (FR-GL-12); an advance that claims
// or refunds settled is not voided.
type CashAdvances struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedAdvance is an advance or refund as recording it would post it, written nowhere.
type CheckedAdvance struct {
	Prepared claim.PreparedAdvance
	Entry    ledger.Entry
	Problems ledger.Problems
}

// RecordedAdvance is a recorded advance or refund, or its void: its document, the entry it posted, and a refund's
// allocation to its advance.
type RecordedAdvance struct {
	Advance     storage.Document
	Entry       storage.Entry
	Allocations []storage.Allocation
}

func checkAdvance(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedAdvance, error) {
	p, probs, err := claim.PrepareAdvance(ctx, u, doc, s.Base)
	if err != nil {
		return CheckedAdvance{}, err
	}
	out := CheckedAdvance{Prepared: p, Problems: probs}
	entry, err := ledger.PostCashAdvance(p.Rule, s)
	if ps, ok := err.(ledger.Problems); ok {
		out.Problems = append(out.Problems, ps...)
	} else if err != nil {
		return CheckedAdvance{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry = entry
	}
	return out, nil
}

// lockAdvance locks the advance a refund names, so two refunds of it, or a refund and a claim settling it, wait for
// each other (ENG-47).
func lockAdvance(ctx context.Context, u storage.UnitOfWork, a claim.Advance) error {
	if a.Refunds == nil {
		return nil
	}
	if _, err := u.Documents().Lock(ctx, *a.Refunds); err != nil && !errors.Is(err, ports.ErrNotFound) {
		return err
	}
	return nil
}

// Check runs the rule over an advance or refund as it would be recorded, and writes nothing.
func (s CashAdvances) Check(ctx context.Context, org uuid.UUID, a claim.Advance) (CheckedAdvance, error) {
	b, err := claim.EncodeAdvance(a)
	if err != nil {
		return CheckedAdvance{}, err
	}
	var out CheckedAdvance
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		doc := storage.Document{ID: uuid.Nil, Type: storage.DocumentCashAdvance, Version: 1, Status: storage.DocumentDraft, Payload: b}
		out, err = checkAdvance(ctx, u, doc, settings)
		return err
	})
	return out, err
}

// Record records an advance or a refund and posts it in one transaction: the document, its number (ADV-, gap-free), its
// entry, a refund's allocation to its advance, and the document's change to posted. One the rule refuses writes nothing
// and answers every problem.
func (s CashAdvances) Record(ctx context.Context, org uuid.UUID, a claim.Advance) (RecordedAdvance, error) {
	var out RecordedAdvance
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = s.RecordIn(ctx, u, a)
		return err
	})
	return out, err
}

// RecordIn records an advance or a refund in u, the caller's unit of work: what Record does, for a command that writes
// more with it (ENG-02).
func (s CashAdvances) RecordIn(ctx context.Context, u storage.UnitOfWork, a claim.Advance) (RecordedAdvance, error) {
	b, err := claim.EncodeAdvance(a)
	if err != nil {
		return RecordedAdvance{}, err
	}
	if err := lockAdvance(ctx, u, a); err != nil {
		return RecordedAdvance{}, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCashAdvance, Payload: b})
	if err != nil {
		return RecordedAdvance{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedAdvance{}, err
	}
	c, err := checkAdvance(ctx, u, doc, settings)
	if err != nil {
		return RecordedAdvance{}, err
	}
	if len(c.Problems) > 0 {
		return RecordedAdvance{}, c.Problems
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentCashAdvance); err != nil {
		return RecordedAdvance{}, err
	}
	out := RecordedAdvance{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
		return RecordedAdvance{}, err
	}
	if a.Refunds != nil {
		x, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: *a.Refunds, On: a.Date,
			Currency: a.Amount.Currency().Code, Amount: storage.Decimal(a.Amount.String()), BaseCurrency: settings.Base.Code,
			BaseAmount: storage.Decimal(a.Amount.String()), RealisedFX: "0"})
		if err != nil {
			return RecordedAdvance{}, err
		}
		out.Allocations = append(out.Allocations, x)
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return RecordedAdvance{}, err
	}
	doc.Status = storage.DocumentPosted
	out.Advance = doc
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, doc, out.Entry)
}

// Void undoes a posted advance or refund with a reversing entry dated date, with a reason (FR-GL-12); a refund's
// allocation is reversed on that date too, which reopens the advance. An advance that claims or refunds settled is not
// voided: void the refunds first; a claim stays posted. The date is not before the document's, and its period must be
// open (INV-03).
func (s CashAdvances) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (RecordedAdvance, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out RecordedAdvance
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = s.VoidIn(ctx, u, id, date, reason)
		return err
	})
	return out, err
}

// VoidIn voids a posted advance or refund in u, the caller's unit of work: what Void does.
func (s CashAdvances) VoidIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, date time.Time, reason string) (RecordedAdvance, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return RecordedAdvance{}, err
	}
	a, err := claim.ReadAdvance(doc)
	if err != nil {
		return RecordedAdvance{}, err
	}
	if err := lockAdvance(ctx, u, a); err != nil { // the advance before its refund, as recording one does
		return RecordedAdvance{}, err
	}
	if doc, err = u.Documents().Lock(ctx, id); err != nil {
		return RecordedAdvance{}, err
	}
	refuse := func(msg, fix string) (RecordedAdvance, error) {
		return RecordedAdvance{}, &claim.Error{Code: claim.CodeAdvanceSettled, Message: msg, Fix: fix}
	}
	if doc.Status != storage.DocumentPosted {
		return refuse(fmt.Sprintf("the advance is %s, not posted", doc.Status), "Only a posted advance or refund is voided")
	}
	y, m, dd := date.Date()
	date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
	if date.Before(a.Date) {
		return refuse(fmt.Sprintf("the void is dated %s, before the advance's %s", date.Format(time.DateOnly), a.Date.Format(time.DateOnly)),
			"Date the void on or after the advance")
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedAdvance{}, err
	}
	if a.Refunds == nil {
		b, err := claim.AdvanceBalances(ctx, u, []storage.Document{doc}, settings.Base, time.Time{})
		if err != nil {
			return RecordedAdvance{}, err
		}
		if x := b[doc.ID]; !x.Settled.IsZero() || !x.Refunded.IsZero() {
			return refuse(fmt.Sprintf("claims settled %s %s and refunds %s %s of the advance", x.Settled, settings.Base, x.Refunded, settings.Base),
				"Void its refunds first; an advance that claims settled stays, and what is left of it is refunded")
		}
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.CashAdvanceIdentity(doc.ID, doc.Version, ledger.RuleCashAdvance))
	if err != nil {
		return RecordedAdvance{}, err
	}
	original, err := fromStorage(stored, settings.Base)
	if err != nil {
		return RecordedAdvance{}, err
	}
	reversal, err := ledger.Reverse(original, date, reason)
	if err != nil {
		return RecordedAdvance{}, err
	}
	out := RecordedAdvance{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
		return RecordedAdvance{}, err
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return RecordedAdvance{}, err
	}
	for _, x := range allocs {
		if x.ReversesID != nil {
			continue
		}
		rid := x.ID
		r, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: x.SourceID, SourceVersion: x.SourceVersion, DocumentID: x.DocumentID, On: date,
			Currency: x.Currency, Amount: negated(x.Amount), BaseCurrency: x.BaseCurrency, BaseAmount: negated(x.BaseAmount),
			RealisedFX: negated(x.RealisedFX), Tolerance: negated(x.Tolerance), ReversesID: &rid})
		if err != nil {
			return RecordedAdvance{}, err
		}
		out.Allocations = append(out.Allocations, r)
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
		return RecordedAdvance{}, err
	}
	doc.Status = storage.DocumentReversed
	out.Advance = doc
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, doc, out.Entry)
}

// replayCashAdvance posts an advance or refund version again as its entry posted it (ENG-06), to the advances account
// the entry kept, so a later change of the setting does not change what it posted.
func replayCashAdvance(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	p, _, err := claim.PrepareAdvance(ctx, u, doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, l := range e.Lines {
		if l.AccountID != p.Rule.Account {
			p.Rule.Advances = l.AccountID
		}
	}
	return ledger.ReplayCashAdvance(e.RuleVersion, p.Rule, s)
}
