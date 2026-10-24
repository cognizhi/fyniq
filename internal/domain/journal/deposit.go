package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CustomerDeposits records money customers pay ahead of their invoices (FR-AR-09; p1-sales task 3.2) against a store:
// each in one unit of work, the document and its entry from the rule customer_deposit@1 together (ENG-02). A deposit
// is recorded and posted at once, as the money has come in already; it is undone by a void, a reversing entry, once
// no invoice deducts it and no refund settles it (FR-GL-12).
type CustomerDeposits struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedDeposit is a deposit as recording it would post it, written nowhere: what the rule reads, the entry, and the
// problems that stop it (✖).
type CheckedDeposit struct {
	Prepared deposit.Prepared
	Entry    ledger.Entry
	Problems ledger.Problems
}

// RecordedDeposit is a recorded deposit, or its void: its document and the entry it posted.
type RecordedDeposit struct {
	Deposit storage.Document
	Entry   storage.Entry
}

func checkDeposit(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedDeposit, error) {
	p, probs, err := deposit.Prepare(ctx, u, doc, s)
	if err != nil {
		return CheckedDeposit{}, err
	}
	out := CheckedDeposit{Prepared: p, Problems: probs}
	entry, err := ledger.PostCustomerDeposit(p.Deposit, s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate) {
				continue // the cascade found no rate: FX-01 says so, in place of the engine's LEDGER-07
			}
			out.Problems = append(out.Problems, x)
		}
	} else if err != nil {
		return CheckedDeposit{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry = entry
	}
	return out, nil
}

// Check runs the rule over a deposit as it would be recorded, and writes nothing: the ledger impact a person sees
// before recording it, and the problems that would stop it.
func (s CustomerDeposits) Check(ctx context.Context, org uuid.UUID, d deposit.Draft) (CheckedDeposit, error) {
	b, err := deposit.Encode(d)
	if err != nil {
		return CheckedDeposit{}, err
	}
	var out CheckedDeposit
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		doc := storage.Document{ID: uuid.Nil, Type: storage.DocumentCustomerDeposit, Version: 1, Status: storage.DocumentDraft, Payload: b}
		out, err = checkDeposit(ctx, u, doc, settings)
		return err
	})
	return out, err
}

// Record records a deposit and posts it in one transaction: the document, its number (DP-, gap-free), its entry and
// the document's change to posted. A deposit the rules refuse writes nothing and answers every problem.
func (s CustomerDeposits) Record(ctx context.Context, org uuid.UUID, d deposit.Draft) (RecordedDeposit, error) {
	b, err := deposit.Encode(d)
	if err != nil {
		return RecordedDeposit{}, err
	}
	var out RecordedDeposit
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCustomerDeposit, Payload: b})
		if err != nil {
			return err
		}
		// Numbered first, so its entry's memo names it; a refusal rolls the number back with everything else.
		if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentCustomerDeposit); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		c, err := checkDeposit(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out.Deposit = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

// Void undoes a posted deposit with a reversing entry dated date, with a reason (FR-GL-12); the deposit is marked
// reversed. Nothing may use it: an invoice that deducts it or a refund of it is voided first (DEPOSIT-08). The date is
// not before the deposit's, and its period must be open (INV-03).
func (s CustomerDeposits) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (RecordedDeposit, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out RecordedDeposit
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		d, err := deposit.Read(doc, settings.Base)
		if err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &deposit.Error{Code: deposit.CodeNotPosted, Message: fmt.Sprintf("the deposit is %s, not posted", doc.Status), Fix: "Only a posted deposit is voided"}
		}
		y, m, dd := date.Date()
		date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		if date.Before(d.Date) {
			return &deposit.Error{Code: deposit.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the deposit's %s",
				date.Format(time.DateOnly), d.Date.Format(time.DateOnly)), Fix: "Date the void on or after the deposit"}
		}
		used, err := u.Allocations().Settled(ctx, []uuid.UUID{doc.ID})
		if err != nil {
			return err
		}
		if x, ok := used[doc.ID]; ok && !isZeroDecimal(string(x)) {
			return &deposit.Error{Code: deposit.CodeUsed, Message: fmt.Sprintf("%s of the deposit is deducted by invoices or refunded", x),
				Fix: "Void those invoices or refunds first, then void the deposit"}
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.CustomerDepositIdentity(doc.ID, doc.Version, ledger.RuleCustomerDeposit))
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
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out.Deposit = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

func (s CustomerDeposits) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayCustomerDeposit posts a stored deposit again under the rule that posted e (ENG-06), with what the entry kept:
// the cascade's rate as it was, and whether it bore GST, read from its output tax line, so a later record of the
// organisation's GST registration never makes an old deposit differ.
func replayCustomerDeposit(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	p, _, err := deposit.Prepare(ctx, u, doc, s)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	d := p.Deposit
	d.Memo = stored.Memo // it names the customer as they were named then
	taxed := false
	for _, l := range stored.Lines {
		switch {
		case s.Accounts[l.AccountID].Role == ledger.RoleGSTControl:
			taxed = true
		case l.AccountID == d.Account && p.Draft.Rate == nil && d.Currency != s.Base && l.Amount.Currency() == d.Currency:
			rate := l.Rate // the cascade's rate as the entry kept it
			d.Rate = &rate
		}
	}
	if !taxed {
		d.GSTRate = ""
	}
	return ledger.ReplayCustomerDeposit(e.RuleVersion, d, s)
}
