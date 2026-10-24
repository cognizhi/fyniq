package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Invoices runs the invoice commands (FR-AR-03; p1-sales) against a store: each in one unit of work, the document and
// its entry together (ENG-02), the entry from the rule invoice@1 (ENG-01). As bills are, an invoice is drafted, issued
// (posted), voided by a reversal with a reason, or edited by reversing the entry of the version it had and posting the
// new one (FR-GL-12); posted history never changes.
type Invoices struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedInvoice is an invoice version as issuing it would post it, written nowhere: what the rules read, the entry,
// and the problems that stop it (✖). Receivable is the invoice's total in the base currency, as its entry debits
// accounts receivable: what its approval is judged on (FM-2); zero when it would not post.
type CheckedInvoice struct {
	Prepared   invoice.Prepared
	Entry      ledger.Entry
	Problems   ledger.Problems
	Receivable money.Money
}

func checkInvoice(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedInvoice, error) {
	p, probs, err := invoice.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return CheckedInvoice{}, err
	}
	out := CheckedInvoice{Prepared: p, Problems: probs}
	entry, err := ledger.PostInvoice(p.Invoice, s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate) {
				continue // the cascade found no rate: FX-01 says so, in place of the engine's LEDGER-07
			}
			if x.Code == ledger.CodeGSTRate && probs.Has(ledger.CodeGSTRate) {
				continue // said once
			}
			out.Problems = append(out.Problems, x)
		}
	} else if err != nil {
		return CheckedInvoice{}, err
	}
	out.Receivable = money.Zero(s.Base)
	if len(out.Problems) == 0 {
		out.Entry = entry
		out.Receivable = entry.Lines[0].Base // invoice@1 debits 1100 first
	}
	return out, nil
}

func isInvoice(doc storage.Document) error {
	if !invoice.IsInvoice(doc) {
		return &invoice.Error{Code: invoice.CodeNotInvoice, Message: fmt.Sprintf("document %s is a %s, not an invoice", doc.ID, doc.Type), Fix: "Open it with its own module"}
	}
	return nil
}

// InvoiceIdentityOf is the posting identity of an invoice's version under its rule.
func InvoiceIdentityOf(doc storage.Document) string {
	return ledger.InvoiceIdentity(doc.ID, doc.Version, ledger.RuleInvoice)
}

// Create saves a draft invoice (✎). It is checked when it is issued; Check shows what issuing it would do.
func (s Invoices) Create(ctx context.Context, org uuid.UUID, d invoice.Draft) (storage.Document, error) {
	b, err := invoice.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err = createNumbered(ctx, u, storage.DocumentInvoice, b)
		return err
	})
	return doc, err
}

// Revise saves a draft invoice's new content as its next version, recording who saved it.
func (s Invoices) Revise(ctx context.Context, org, id uuid.UUID, d invoice.Draft) (storage.Document, error) {
	b, err := invoice.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isInvoice(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &invoice.Error{Code: invoice.CodeNotDraft, Message: fmt.Sprintf("the invoice is %s, not a draft", doc.Status), Fix: "Only a draft changes; edit an issued invoice instead"}
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

// CheckInvoice runs the rules over doc, a version of an invoice, in u, and writes nothing.
func CheckInvoice(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (CheckedInvoice, error) {
	if err := isInvoice(doc); err != nil {
		return CheckedInvoice{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return CheckedInvoice{}, err
	}
	return checkInvoice(ctx, u, doc, settings)
}

// Check runs the rules over an invoice's current version, or its edit d when d is not nil, and writes nothing: the
// ledger impact a person sees before issuing it, the problems that would stop it, and its totals.
func (s Invoices) Check(ctx context.Context, org, id uuid.UUID, d *invoice.Draft) (CheckedInvoice, error) {
	var out CheckedInvoice
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		if err := isInvoice(doc); err != nil {
			return err
		}
		if d != nil {
			b, err := invoice.Encode(*d)
			if err != nil {
				return err
			}
			doc.Version, doc.Payload = doc.Version+1, b
		}
		out, err = CheckInvoice(ctx, u, doc)
		return err
	})
	return out, err
}

// Post issues a draft invoice: its number (gap-free, in this transaction), then the entry of its current version, with
// the document's change of status, in one transaction. An invoice the rules refuse posts nothing and answers every
// problem.
func (s Invoices) Post(ctx context.Context, org, id uuid.UUID) (storage.Entry, error) {
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isInvoice(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &invoice.Error{Code: invoice.CodeNotDraft, Message: fmt.Sprintf("the invoice is %s, not a draft", doc.Status),
				Fix: "Only a draft is issued; edit or void an issued invoice instead"}
		}
		// Numbered first, so its entry's memo names it; a refusal rolls the number back with everything else.
		if doc.Number, err = u.Sequences().Number(ctx, id, doc.Type); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		c, err := checkInvoice(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		if out, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		return s.after(ctx, u, doc, out)
	})
	return out, err
}

// Void undoes an issued invoice with a reversing entry dated date, with a reason (FR-GL-12: a void is a reversal); the
// invoice is marked reversed. Its period must be open (INV-03), and nothing may settle it.
func (s Invoices) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (storage.Entry, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isInvoice(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &invoice.Error{Code: invoice.CodeNotPosted, Message: fmt.Sprintf("the invoice is %s, not issued", doc.Status),
				Fix: "Only an issued invoice is voided; a draft is simply not issued"}
		}
		if err := unsettled(ctx, u, doc, "void"); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, InvoiceIdentityOf(doc))
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
		if out, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		return s.after(ctx, u, doc, out)
	})
	return out, err
}

// Edit corrects an issued invoice (FR-GL-12, posting rule "Edits of posted documents"): it saves d as the invoice's next
// version, reverses the entry of the version it had on that entry's own date, and posts the new version's entry, in one
// transaction; the invoice stays issued, with its number. preparer is who prepared the new version ("type:id"), empty
// for the actor. An edit the rules refuse writes nothing.
func (s Invoices) Edit(ctx context.Context, org, id uuid.UUID, d invoice.Draft, reason, preparer string) (Edited, error) {
	b, err := invoice.Encode(d)
	if err != nil {
		return Edited{}, err
	}
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out Edited
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isInvoice(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &invoice.Error{Code: invoice.CodeNotPosted, Message: fmt.Sprintf("the invoice is %s, not issued", doc.Status),
				Fix: "Only an issued invoice is edited this way; change a draft as it is"}
		}
		if err := unsettled(ctx, u, doc, "edit"); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		next := doc
		next.Version, next.Payload = doc.Version+1, b
		c, err := checkInvoice(ctx, u, next, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		stored, err := u.Ledger().ByIdentity(ctx, InvoiceIdentityOf(doc))
		if err != nil {
			return err
		}
		original, err := fromStorage(stored, settings.Base)
		if err != nil {
			return err
		}
		reversal, err := ledger.Reverse(original, original.PostingDate, reason)
		if err != nil {
			return err
		}
		edited, err := u.Documents().Edit(ctx, id, b, preparer)
		if err != nil {
			return err
		}
		if out.Reversal, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		edited.Number = doc.Number
		if err := s.after(ctx, u, edited, out.Reversal); err != nil {
			return err
		}
		return s.after(ctx, u, edited, out.Entry)
	})
	return out, err
}

// unsettled refuses to void or edit an invoice that receipts or credits settle (FR-AR-18): they are voided first, which
// reopens it, so a void never leaves a receipt settling nothing and an edit never changes what a receipt carried.
func unsettled(ctx context.Context, u storage.UnitOfWork, doc storage.Document, what string) error {
	settled, err := u.Allocations().Settled(ctx, []uuid.UUID{doc.ID})
	if err != nil {
		return err
	}
	if x, ok := settled[doc.ID]; ok && !isZeroDecimal(string(x)) {
		return &invoice.Error{Code: invoice.CodeSettled, Message: fmt.Sprintf("receipts or credits settle %s of the invoice", x),
			Fix: fmt.Sprintf("Void them first, then %s it", what)}
	}
	return nil
}

func (s Invoices) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayInvoice posts a stored invoice version again under the rule that posted e (ENG-06), with what the entry kept:
// the rate the cascade gave it (ENG-14), and whether it bore GST, read from its output tax line, so a later record of
// the organisation's GST registration never makes an old invoice differ.
func replayInvoice(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	p, _, err := invoice.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	i := p.Invoice
	i.Memo = stored.Memo // it names the customer as they were named then
	if p.Draft.Rate == nil && i.Currency != s.Base && len(stored.Lines) > 0 {
		r := stored.Lines[0].Rate // 1100, in the invoice's currency, at the rate the cascade gave it
		i.Rate = &r
	}
	taxed := false
	for _, l := range stored.Lines {
		taxed = taxed || s.Accounts[l.AccountID].Role == ledger.RoleGSTControl
	}
	if !taxed {
		for k := range i.Lines {
			i.Lines[k].GSTRate = ""
		}
	}
	return ledger.ReplayInvoice(e.RuleVersion, i, s)
}
