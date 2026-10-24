package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CustomerReceipts runs the commands of receipts recorded by hand (FR-AR-18; the owner's decision of 24 Oct 2026) against
// a store, as BillPayments does payments: each in one unit of work, the document, its entry from the rule
// customer_receipt@1 and its allocations together (ENG-02). A receipt is recorded and posted at once, as the money has
// come in already; it is undone by a void, a reversing entry and reversing allocations, which reopens the invoices
// (FR-GL-12, FR-AR-18).
type CustomerReceipts struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedReceipt is a receipt as recording it would post it, written nowhere: what the rule reads, the entry, the
// settlements, and the problems that stop it.
type CheckedReceipt struct {
	Prepared    receipt.Prepared
	Entry       ledger.Entry
	Settlements []ledger.Settlement
	Problems    ledger.Problems
}

// RecordedReceipt is a recorded receipt: its document, its entry and its allocations.
type RecordedReceipt struct {
	Receipt     storage.Document
	Entry       storage.Entry
	Allocations []storage.Allocation
}

func checkReceipt(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedReceipt, error) {
	p, probs, err := receipt.Prepare(ctx, u, doc, s)
	if err != nil {
		return CheckedReceipt{}, err
	}
	out := CheckedReceipt{Prepared: p, Problems: probs}
	entry, settled, err := ledger.PostCustomerReceipt(p.Receipt, s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate) {
				continue // the cascade found no rate: FX-01 says so, in place of the engine's LEDGER-07
			}
			if x.Code == ledger.CodePaymentAmount && x.Field == "invoices" && (probs.Has(receipt.CodeInvoice) || probs.Has(receipt.CodeOverpaid)) {
				continue // the invoices were refused: their own problems say so, in place of "it settles nothing"
			}
			out.Problems = append(out.Problems, x)
		}
	} else if err != nil {
		return CheckedReceipt{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry, out.Settlements = entry, settled
	}
	return out, nil
}

func invoiceIDs(d receipt.Draft) []uuid.UUID {
	var out []uuid.UUID
	for _, x := range d.Invoices {
		out = append(out, x.InvoiceID)
	}
	return out
}

// Check runs the rule over a receipt as it would be recorded, and writes nothing: the ledger impact a person sees before
// recording it, and the problems that would stop it.
func (s CustomerReceipts) Check(ctx context.Context, org uuid.UUID, d receipt.Draft) (CheckedReceipt, error) {
	b, err := receipt.Encode(d)
	if err != nil {
		return CheckedReceipt{}, err
	}
	var out CheckedReceipt
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		doc := storage.Document{ID: uuid.Nil, Type: storage.DocumentCustomerReceipt, Version: 1, Status: storage.DocumentDraft, Payload: b}
		out, err = checkReceipt(ctx, u, doc, settings)
		return err
	})
	return out, err
}

// Record records a receipt and posts it in one transaction: the document, its number (RC-, gap-free), its entry, an
// allocation for each invoice it settles, and the document's change to posted. The invoices are locked first. A receipt
// the rules refuse writes nothing and answers every problem.
func (s CustomerReceipts) Record(ctx context.Context, org uuid.UUID, d receipt.Draft) (RecordedReceipt, error) {
	b, err := receipt.Encode(d)
	if err != nil {
		return RecordedReceipt{}, err
	}
	var out RecordedReceipt
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := lockBills(ctx, u, invoiceIDs(d)); err != nil {
			return err
		}
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCustomerReceipt, Payload: b})
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		c, err := checkReceipt(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentCustomerReceipt); err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		for _, x := range c.Settlements {
			a, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: x.DocumentID, On: d.Date,
				Currency: x.Amount.Currency().Code, Amount: storage.Decimal(x.Amount.String()), BaseCurrency: settings.Base.Code,
				BaseAmount: storage.Decimal(x.Base.String()), RealisedFX: storage.Decimal(x.RealisedFX.String()),
				Tolerance: storage.Decimal(x.Tolerance.String())})
			if err != nil {
				return err
			}
			out.Allocations = append(out.Allocations, a)
		}
		if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out.Receipt = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

// Void undoes a posted receipt with a reversing entry dated date, with a reason (FR-GL-12), and a reversing allocation
// for each of its allocations on that date, which reopens the invoices (FR-AR-18); the receipt is marked reversed. The
// date is not before the receipt's, and its period must be open (INV-03).
func (s CustomerReceipts) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (RecordedReceipt, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out RecordedReceipt
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if doc.Type != storage.DocumentCustomerReceipt {
			return &receipt.Error{Code: receipt.CodeNotReceipt, Message: fmt.Sprintf("document %s is a %s, not a customer receipt", doc.ID, doc.Type), Fix: "Open it with its own module"}
		}
		if doc.Status != storage.DocumentPosted {
			return &receipt.Error{Code: receipt.CodeNotPosted, Message: fmt.Sprintf("the receipt is %s, not posted", doc.Status), Fix: "Only a posted receipt is voided"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		d, err := receipt.Read(doc, settings.Base)
		if err != nil {
			return err
		}
		y, m, dd := date.Date()
		date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		if date.Before(d.Date) {
			return &receipt.Error{Code: receipt.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the receipt's %s", date.Format(time.DateOnly), d.Date.Format(time.DateOnly)),
				Fix: "Date the void on or after the receipt"}
		}
		if err := lockBills(ctx, u, invoiceIDs(d)); err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.CustomerReceiptIdentity(doc.ID, doc.Version, ledger.RuleCustomerReceipt))
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
		reversed := map[uuid.UUID]bool{}
		for _, a := range allocs {
			if a.ReversesID != nil {
				reversed[*a.ReversesID] = true
			}
		}
		for _, a := range allocs {
			if a.ReversesID != nil || reversed[a.ID] {
				continue
			}
			id := a.ID
			r, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID, On: date,
				Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount),
				RealisedFX: negated(a.RealisedFX), Tolerance: negated(a.Tolerance), ReversesID: &id})
			if err != nil {
				return err
			}
			out.Allocations = append(out.Allocations, r)
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out.Receipt = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

func (s CustomerReceipts) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayCustomerReceipt posts a stored receipt version again under the rule that posted e (ENG-06), with what the entry
// and the allocations kept: the rate the cascade gave it (ENG-14), each invoice's stored rate from its line, and each
// invoice's carrying amount the allocation cleared, so a later receipt or void of the same invoices never makes an old
// receipt differ.
func replayCustomerReceipt(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	d, err := receipt.Read(doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	cleared := map[uuid.UUID]storage.Allocation{}
	for _, a := range allocs {
		if a.ReversesID == nil && a.SourceVersion == doc.Version {
			cleared[a.DocumentID] = a
		}
	}
	r := ledger.CustomerReceipt{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: stored.Memo, Account: d.AccountID, Currency: d.Currency, Rate: d.Rate,
		Received: d.Received, Unapplied: d.Unapplied}
	var parts []ledger.Line // each invoice's line, in order: on 1100, with an amount
	for _, l := range stored.Lines {
		switch {
		case s.Accounts[l.AccountID].Role == ledger.RoleAccountsReceivable && !l.Amount.IsZero():
			parts = append(parts, l)
		case l.AccountID == d.AccountID && d.Rate == nil && d.Currency != s.Base && l.Amount.Currency() == d.Currency:
			rate := l.Rate // the cascade's rate as the entry kept it
			r.Rate = &rate
		}
	}
	for i, x := range d.Invoices {
		part := ledger.ReceivedInvoice{InvoiceID: x.InvoiceID, Amount: x.Amount, StoredRate: money.Identity(s.Base)}
		if i < len(parts) {
			part.StoredRate = parts[i].Rate
		}
		if a, ok := cleared[x.InvoiceID]; ok {
			base, err := money.ParseStored(string(a.BaseAmount), s.Base)
			if err != nil {
				return ledger.Entry{}, err
			}
			part.Clears = &base
			// What it settled beyond what it paid was a settlement difference within tolerance (FR-BNK-08).
			settled, err := money.ParseStored(string(a.Amount), x.Amount.Currency())
			if err != nil {
				return ledger.Entry{}, err
			}
			if short, err := settled.Sub(x.Amount); err == nil && short.Sign() > 0 {
				part.Short = &short
			}
		}
		r.Invoices = append(r.Invoices, part)
	}
	entry, _, err := ledger.ReplayCustomerReceipt(e.RuleVersion, r, s)
	return entry, err
}
