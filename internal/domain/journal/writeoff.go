package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// WriteOffs runs the commands of write-offs (FR-AR-17; p1-sales task 2.3) against a store: a write-off is drafted and
// changed while a draft, posted only when another person approves it (FR-APR-01, SOD-9: the API's approval engine calls
// Post), and voided by a reversal and a reversing allocation, which reopens the invoice (FR-GL-12, FR-AR-18). Each
// command is one unit of work, the document, its entry from the rule write_off@1 and its allocation together (ENG-02).
type WriteOffs struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedWriteOff is a write-off version as posting it would post it, written nowhere.
type CheckedWriteOff struct {
	Prepared   writeoff.Prepared
	Entry      ledger.Entry
	Settlement ledger.Settlement
	Problems   ledger.Problems
}

// PostedWriteOff is a posted or voided write-off: its document, its entry and its allocation.
type PostedWriteOff struct {
	WriteOff   storage.Document
	Entry      storage.Entry
	Allocation storage.Allocation
}

func isWriteOff(doc storage.Document) error {
	if !writeoff.IsWriteOff(doc) {
		return &writeoff.Error{Code: writeoff.CodeNotWriteOff, Message: fmt.Sprintf("document %s is a %s, not a write-off", doc.ID, doc.Type), Fix: "Open it with its own module"}
	}
	return nil
}

func checkWriteOff(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedWriteOff, error) {
	p, probs, err := writeoff.Prepare(ctx, u, doc, s)
	if err != nil {
		return CheckedWriteOff{}, err
	}
	out := CheckedWriteOff{Prepared: p, Problems: probs}
	if probs.Has(writeoff.CodeInvoice) || probs.Has(writeoff.CodeAccount) || probs.Has(writeoff.CodeReason) {
		return out, nil // the rule would only repeat them
	}
	entry, settled, err := ledger.PostWriteOff(p.WriteOff, s)
	if ps, ok := err.(ledger.Problems); ok {
		out.Problems = append(out.Problems, ps...)
	} else if err != nil {
		return CheckedWriteOff{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry, out.Settlement = entry, settled
	}
	return out, nil
}

// CheckWriteOff runs the rules over doc, a version of a write-off, in u, and writes nothing.
func CheckWriteOff(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (CheckedWriteOff, error) {
	if err := isWriteOff(doc); err != nil {
		return CheckedWriteOff{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return CheckedWriteOff{}, err
	}
	return checkWriteOff(ctx, u, doc, settings)
}

// Create saves a draft write-off; it posts only on another person's approval.
func (s WriteOffs) Create(ctx context.Context, org uuid.UUID, d writeoff.Draft) (storage.Document, error) {
	b, err := writeoff.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err = createNumbered(ctx, u, storage.DocumentWriteOff, b)
		return err
	})
	return doc, err
}

// Revise saves a draft write-off's new content as its next version, recording who saved it.
func (s WriteOffs) Revise(ctx context.Context, org, id uuid.UUID, d writeoff.Draft) (storage.Document, error) {
	b, err := writeoff.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isWriteOff(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &writeoff.Error{Code: writeoff.CodeNotDraft, Message: fmt.Sprintf("the write-off is %s, not a draft", doc.Status), Fix: "Only a draft changes; void a posted one"}
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

// Post posts an approved draft write-off: its number, its entry, the allocation that settles the invoice, and the
// document's change to posted, in one transaction, the invoice locked first. A write-off the rules refuse posts nothing.
func (s WriteOffs) Post(ctx context.Context, org, id uuid.UUID) (PostedWriteOff, error) {
	var out PostedWriteOff
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isWriteOff(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &writeoff.Error{Code: writeoff.CodeNotDraft, Message: fmt.Sprintf("the write-off is %s, not a draft", doc.Status), Fix: "Only a draft is posted"}
		}
		d, err := writeoff.Read(doc)
		if err != nil {
			return err
		}
		if err := lockBills(ctx, u, []uuid.UUID{d.InvoiceID}); err != nil {
			return err
		}
		if doc.Number, err = u.Sequences().Number(ctx, id, doc.Type); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		c, err := checkWriteOff(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		x := c.Settlement
		if out.Allocation, err = u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: x.DocumentID,
			On: d.Date, Currency: x.Amount.Currency().Code, Amount: storage.Decimal(x.Amount.String()), BaseCurrency: settings.Base.Code,
			BaseAmount: storage.Decimal(x.Base.String()), RealisedFX: storage.Decimal(x.RealisedFX.String())}); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out.WriteOff = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

// Void undoes a posted write-off with a reversing entry dated date, with a reason (FR-GL-12), and a reversing allocation
// on that date, which reopens the invoice (FR-AR-18); the write-off is marked reversed.
func (s WriteOffs) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (PostedWriteOff, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out PostedWriteOff
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isWriteOff(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &writeoff.Error{Code: writeoff.CodeNotPosted, Message: fmt.Sprintf("the write-off is %s, not posted", doc.Status), Fix: "Only a posted write-off is voided"}
		}
		d, err := writeoff.Read(doc)
		if err != nil {
			return err
		}
		y, m, dd := date.Date()
		date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		if date.Before(d.Date) {
			return &writeoff.Error{Code: writeoff.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the write-off's %s", date.Format(time.DateOnly), d.Date.Format(time.DateOnly)),
				Fix: "Date the void on or after the write-off"}
		}
		if err := lockBills(ctx, u, []uuid.UUID{d.InvoiceID}); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.WriteOffIdentity(doc.ID, doc.Version, ledger.RuleWriteOff))
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
			aid := a.ID
			if out.Allocation, err = u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID,
				On: date, Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount),
				RealisedFX: negated(a.RealisedFX), ReversesID: &aid}); err != nil {
				return err
			}
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out.WriteOff = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

func (s WriteOffs) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayWriteOff posts a stored write-off version again under the rule that posted e (ENG-06), with what the entry and
// the allocation kept: the invoice's stored rate from its 1100 line, and the carrying amount the allocation cleared.
func replayWriteOff(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	d, err := writeoff.Read(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	w := ledger.WriteOff{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: stored.Memo, InvoiceID: d.InvoiceID, Amount: d.Amount,
		StoredRate: money.Identity(s.Base)}
	for _, l := range stored.Lines {
		switch {
		case s.Accounts[l.AccountID].Role == ledger.RoleAccountsReceivable && !l.Amount.IsZero():
			w.StoredRate = l.Rate
		case s.Accounts[l.AccountID].Role != ledger.RoleAccountsReceivable:
			w.Account = l.AccountID
		}
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, a := range allocs {
		if a.ReversesID == nil && a.SourceVersion == doc.Version {
			// The amount at its stored rate differs from what the allocation cleared only when the write-off took all
			// that was open: the residual line says so.
			base, err := money.ParseStored(string(a.BaseAmount), s.Base)
			if err != nil {
				return ledger.Entry{}, err
			}
			for _, l := range stored.Lines {
				if s.Accounts[l.AccountID].Role == ledger.RoleAccountsReceivable && l.Amount.IsZero() {
					w.Clears = &base
				}
			}
		}
	}
	entry, _, err := ledger.ReplayWriteOff(e.RuleVersion, w, s)
	return entry, err
}
