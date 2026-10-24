package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/customercredit"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CustomerRefunds records money paid back to customers against their credits (FR-AR-04, FR-AR-18; p1-sales task 3.1)
// against a store: each in one unit of work, the document, its entry from the rule customer_refund@1 and its allocation
// together (ENG-02). A refund is recorded and posted at once, as the money has left already; it is undone by a void, a
// reversing entry and a reversing allocation, which reopens the credit (FR-GL-12, FR-AR-18).
type CustomerRefunds struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// Record records a refund and posts it in one transaction: the credit locked first, the document, its number (CR-,
// gap-free), its entry, the allocation that settles the credit, and the document's change to posted. A refund the
// rules refuse writes nothing and answers every problem.
func (s CustomerRefunds) Record(ctx context.Context, org uuid.UUID, d customercredit.RefundDraft) (Refunded, error) {
	b, err := customercredit.EncodeRefund(d)
	if err != nil {
		return Refunded{}, err
	}
	var out Refunded
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := lockBills(ctx, u, []uuid.UUID{d.CreditID}); err != nil {
			return err
		}
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCustomerRefund, Payload: b})
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		p, probs, err := customercredit.PrepareRefund(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		entry, settled := ledger.Entry{}, ledger.Settlement{}
		if len(probs) == 0 || p.Credit.Doc.ID != uuid.Nil {
			var perr error
			entry, settled, perr = ledger.PostCustomerRefund(p.Refund, settings)
			if ps, ok := perr.(ledger.Problems); ok {
				probs = append(probs, ps...)
			} else if perr != nil {
				return perr
			}
		}
		if len(probs) > 0 {
			return probs
		}
		if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentCustomerRefund); err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
			return err
		}
		if out.Allocation, err = u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: d.CreditID,
			On: d.Date, Currency: settled.Amount.Currency().Code, Amount: storage.Decimal(settled.Amount.String()), BaseCurrency: settings.Base.Code,
			BaseAmount: storage.Decimal(settled.Base.String()), RealisedFX: storage.Decimal(settled.RealisedFX.String())}); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out.Refund = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

// Void undoes a posted refund on date, with a reason: a reversing entry and a reversing allocation, which reopens the
// credit by what it refunded. The date is not before the refund's, and its period must be open (INV-03).
func (s CustomerRefunds) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (Refunded, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out Refunded
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		d, err := customercredit.ReadRefund(doc, settings.Base)
		if err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &customercredit.Error{Code: customercredit.CodeNotPosted, Message: fmt.Sprintf("the refund is %s, not posted", doc.Status), Fix: "Only a posted refund is voided"}
		}
		y, m, dd := date.Date()
		date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		if date.Before(d.Date) {
			return &customercredit.Error{Code: customercredit.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the refund's %s",
				date.Format(time.DateOnly), d.Date.Format(time.DateOnly)), Fix: "Date the void on or after the refund"}
		}
		if err := lockBills(ctx, u, []uuid.UUID{d.CreditID}); err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.CustomerRefundIdentity(doc.ID, doc.Version, ledger.RuleCustomerRefund))
		if err != nil {
			return err
		}
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
		allocs, err := u.Allocations().OfSource(ctx, doc.ID)
		if err != nil {
			return err
		}
		for _, a := range allocs {
			if a.ReversesID != nil {
				continue
			}
			reverses := a.ID
			if out.Allocation, err = u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID,
				On: date, Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount),
				RealisedFX: negated(a.RealisedFX), ReversesID: &reverses}); err != nil {
				return err
			}
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out.Refund = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

func (s CustomerRefunds) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayCustomerRefund posts a stored refund again under the rule that posted e (ENG-06), with what the entry and the
// allocation kept: the credit's rate from its line, the cascade's rate as it was, and the carrying amount it cleared.
func replayCustomerRefund(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	d, err := customercredit.ReadRefund(doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	cur := d.Amount.Currency()
	r := ledger.CustomerRefund{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: stored.Memo, Account: d.AccountID, Currency: cur, Rate: d.Rate,
		Paid: d.Paid, Credit: ledger.PaidBill{BillID: d.CreditID, Amount: d.Amount, StoredRate: money.Identity(s.Base)}}
	for _, l := range stored.Lines {
		switch {
		case s.Accounts[l.AccountID].Role == ledger.RoleAccountsReceivable && !l.Amount.IsZero():
			r.Credit.StoredRate = l.Rate
		case l.AccountID == d.AccountID && d.Rate == nil && cur != s.Base && l.Amount.Currency() == cur:
			rate := l.Rate // the cascade's rate as the entry kept it
			r.Rate = &rate
		}
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, a := range allocs {
		if a.ReversesID == nil && a.SourceVersion == doc.Version {
			base, err := money.ParseStored(string(a.BaseAmount), s.Base)
			if err != nil {
				return ledger.Entry{}, err
			}
			r.Credit.Clears = &base
		}
	}
	entry, _, err := ledger.ReplayCustomerRefund(e.RuleVersion, r, s)
	return entry, err
}
