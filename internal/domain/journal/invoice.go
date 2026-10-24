package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Invoices runs the invoice commands (FR-AR-03; p1-sales) against a store: each in one unit of work, the document and
// its entry together (ENG-02), the entry from the rule invoice@1 (ENG-01), or credit_note@1 for a credit note to a
// customer (FR-AR-04), which runs through the same commands. As bills are, an invoice is drafted, issued
// (posted), voided by a reversal with a reason, or edited by reversing the entry of the version it had and posting the
// new one (FR-GL-12); posted history never changes.
type Invoices struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
	// Clock, when set, makes issuing or editing an invoice post at once the releases of its spread already due by the
	// organisation's today (FR-GL-03), so its revenue is never held on 2400 until the hourly job; nil leaves them to it.
	Clock ports.Clock
}

// releaseNow posts a just-issued invoice's releases due by the organisation's today, after its unit of work commits.
// What it cannot post the hourly job invoices.releases posts, so it never fails the issue.
func (s Invoices) releaseNow(ctx context.Context, org, id uuid.UUID) {
	if s.Clock == nil {
		return
	}
	day, err := todayIn(ctx, s.Store, org, s.Clock.Now())
	if err != nil {
		return
	}
	_, _ = Releases{Store: s.Store}.ReleaseInvoice(ctx, org, id, day)
}

// CheckedInvoice is an invoice version as issuing it would post it, written nowhere: what the rules read, the entry,
// and the problems that stop it (✖). Receivable is the invoice's total in the base currency, as its entry debits
// accounts receivable (a credit note's, as its entry credits it): what its approval is judged on (FM-2); zero when it
// would not post.
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
	post := ledger.PostInvoice
	if invoice.IsCreditNote(doc) {
		post = ledger.PostCreditNote
	}
	entry, err := post(p.Invoice, s)
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
		for _, l := range entry.Lines { // none when deposits cover the invoice whole
			if s.Accounts[l.AccountID].Role == ledger.RoleAccountsReceivable {
				out.Receivable, _ = out.Receivable.Add(l.Base)
			}
		}
		if invoice.IsCreditNote(doc) {
			out.Receivable = out.Receivable.Neg()
		}
	}
	return out, nil
}

func isInvoice(doc storage.Document) error {
	if !invoice.IsInvoice(doc) {
		return &invoice.Error{Code: invoice.CodeNotInvoice, Message: fmt.Sprintf("document %s is a %s, not an invoice", doc.ID, doc.Type), Fix: "Open it with its own module"}
	}
	return nil
}

// InvoiceIdentityOf is the posting identity of an invoice's or a credit note's version under its rule.
func InvoiceIdentityOf(doc storage.Document) string {
	if invoice.IsCreditNote(doc) {
		return ledger.CreditNoteIdentity(doc.ID, doc.Version, ledger.RuleCreditNote)
	}
	return ledger.InvoiceIdentity(doc.ID, doc.Version, ledger.RuleInvoice)
}

// Create saves a draft invoice (✎). It is checked when it is issued; Check shows what issuing it would do.
func (s Invoices) Create(ctx context.Context, org uuid.UUID, d invoice.Draft) (storage.Document, error) {
	return s.create(ctx, org, storage.DocumentInvoice, d)
}

// CreateCredit saves a draft credit note to a customer (✎, FR-AR-04): entered as an invoice is, linked to the invoice
// it credits or standalone, and issued as its reverse; numbered from the credit note sequence when it is issued.
func (s Invoices) CreateCredit(ctx context.Context, org uuid.UUID, d invoice.Draft) (storage.Document, error) {
	return s.create(ctx, org, storage.DocumentCreditNote, d)
}

func (s Invoices) create(ctx context.Context, org uuid.UUID, t storage.DocumentType, d invoice.Draft) (storage.Document, error) {
	b, err := invoice.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err = createNumbered(ctx, u, t, b)
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
		if err := lockDeposits(ctx, u, doc); err != nil {
			return err
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
		if err := useDeposits(ctx, u, doc, c.Prepared.Invoice, settings); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		return s.after(ctx, u, doc, out)
	})
	if err == nil {
		s.releaseNow(ctx, org, id)
	}
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
		if err := unuseDeposits(ctx, u, doc.ID, reversal.PostingDate); err != nil { // the deposits it deducted reopen (FR-AR-09)
			return err
		}
		// Its spread's releases go back on the void's date, or on their own when later (FR-GL-03): 2400 and the line's
		// account are left as if nothing had posted.
		if err := reverseReleases(ctx, u, doc, settings, func(e ledger.Entry) time.Time { return later(reversal.PostingDate, e.PostingDate) }, reason); err != nil {
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
		if err := lockDeposits(ctx, u, doc, next); err != nil {
			return err
		}
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
		// The releases of the version it had go back on their own dates; the new version's are posted after (FR-GL-03).
		if err := reverseReleases(ctx, u, doc, settings, func(e ledger.Entry) time.Time { return e.PostingDate }, reason); err != nil {
			return err
		}
		edited, err := u.Documents().Edit(ctx, id, b, preparer)
		if err != nil {
			return err
		}
		if out.Reversal, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return err
		}
		if err := unuseDeposits(ctx, u, doc.ID, reversal.PostingDate); err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		if err := useDeposits(ctx, u, edited, c.Prepared.Invoice, settings); err != nil {
			return err
		}
		edited.Number = doc.Number
		if err := s.after(ctx, u, edited, out.Reversal); err != nil {
			return err
		}
		return s.after(ctx, u, edited, out.Entry)
	})
	if err == nil {
		s.releaseNow(ctx, org, id)
	}
	return out, err
}

// lockDeposits locks the deposits the invoice versions deduct, in one order, so no other invoice or refund uses them
// meanwhile (FR-AR-09).
func lockDeposits(ctx context.Context, u storage.UnitOfWork, docs ...storage.Document) error {
	var ids []uuid.UUID
	for _, doc := range docs {
		if invoice.IsCreditNote(doc) {
			continue
		}
		var p struct {
			Deposits []struct {
				DepositID uuid.UUID `json:"deposit_id"`
			} `json:"deposits"`
		}
		if err := json.Unmarshal(doc.Payload, &p); err != nil {
			continue // the rules say why it cannot be read
		}
		for _, x := range p.Deposits {
			ids = append(ids, x.DepositID)
		}
	}
	return lockBills(ctx, u, ids)
}

// useDeposits records how an invoice version deducts its deposits (FR-AR-09): an allocation from the invoice to each
// deposit with the amount it takes, its part of the deposit's base amount and of its tax (RND-DEPOSIT), and its realised
// FX: its base part less what it takes at the invoice's rate, the last deposit taking what is left of the entry's, so
// the allocations add up to the entry's realised FX line.
func useDeposits(ctx context.Context, u storage.UnitOfWork, doc storage.Document, i ledger.Invoice, s ledger.Settings) error {
	if len(i.Deposits) == 0 {
		return nil
	}
	rate := money.Identity(s.Base)
	if i.Rate != nil {
		rate = *i.Rate
	}
	totals, err := ledger.TotalsOf(i)
	if err != nil {
		return err
	}
	deducted := money.Zero(i.Currency)
	for _, d := range i.Deposits {
		deducted, _ = deducted.Add(d.Amount)
	}
	receivable, _ := totals.Total.Sub(deducted)
	full, err := rounding.FX(totals.Total, rate)
	if err != nil {
		return err
	}
	owed, err := rounding.FX(receivable, rate)
	if err != nil {
		return err
	}
	atRate, _ := full.Sub(owed) // what the deposits take at the invoice's rate, together
	for k, d := range i.Deposits {
		at := atRate
		if k < len(i.Deposits)-1 {
			if at, err = rounding.FX(d.Amount, rate); err != nil {
				return err
			}
		}
		atRate, _ = atRate.Sub(at)
		gain, _ := d.Base.Sub(at)
		if _, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: d.DepositID, On: i.Date,
			Currency: d.Amount.Currency().Code, Amount: storage.Decimal(d.Amount.String()), BaseCurrency: s.Base.Code,
			BaseAmount: storage.Decimal(d.Base.String()), RealisedFX: storage.Decimal(gain.String()), Tax: storage.Decimal(d.Tax.String())}); err != nil {
			return err
		}
	}
	return nil
}

// unuseDeposits reverses on date every use of a deposit an invoice still stands by: a void or an edit reopens them.
func unuseDeposits(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, date time.Time) error {
	allocs, err := u.Allocations().OfSource(ctx, id)
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
		reverses := a.ID
		if _, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID, On: date,
			Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount), RealisedFX: negated(a.RealisedFX),
			Tax: negated(a.Tax), ReversesID: &reverses}); err != nil {
			return err
		}
	}
	return nil
}

// unsettled refuses to void or edit an invoice that receipts or credits settle (FR-AR-18): they are voided first, which
// reopens it, so a void never leaves a receipt settling nothing and an edit never changes what a receipt carried.
func unsettled(ctx context.Context, u storage.UnitOfWork, doc storage.Document, what string) error {
	settled, err := u.Allocations().Settled(ctx, []uuid.UUID{doc.ID})
	if err != nil {
		return err
	}
	if x, ok := settled[doc.ID]; ok && !isZeroDecimal(string(x)) {
		if invoice.IsCreditNote(doc) {
			return &invoice.Error{Code: invoice.CodeSettled, Message: fmt.Sprintf("%s of the credit note is applied or refunded", x),
				Fix: fmt.Sprintf("Void its applications and refunds first, then %s it", what)}
		}
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
	// What each deposit's use took when it posted, from the allocations of this version, whatever has become of the
	// deposits since (FR-AR-09).
	if len(p.Draft.Deposits) > 0 {
		allocs, err := u.Allocations().OfSource(ctx, doc.ID)
		if err != nil {
			return ledger.Entry{}, err
		}
		i.Deposits = nil
		for _, x := range p.Draft.Deposits {
			use := ledger.InvoiceDeposit{DepositID: x.DepositID, Amount: x.Amount, Base: money.Zero(s.Base), Tax: money.Zero(s.Base)}
			if dep, err := u.Documents().Get(ctx, x.DepositID); err == nil {
				use.Number = dep.Number
			}
			for _, a := range allocs {
				if a.ReversesID != nil || a.SourceVersion != doc.Version || a.DocumentID != x.DepositID {
					continue
				}
				if use.Base, err = money.ParseStored(string(a.BaseAmount), s.Base); err != nil {
					return ledger.Entry{}, err
				}
				if use.Tax, err = money.ParseStored(string(cmpDecimal(a.Tax)), s.Base); err != nil {
					return ledger.Entry{}, err
				}
			}
			i.Deposits = append(i.Deposits, use)
		}
	}
	if p.Draft.Rate == nil && i.Currency != s.Base {
		for _, l := range stored.Lines { // a line in the invoice's currency, at the rate the cascade gave it
			if l.Amount.Currency() == i.Currency {
				r := l.Rate
				i.Rate = &r
				break
			}
		}
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
	if invoice.IsCreditNote(doc) {
		return ledger.ReplayCreditNote(e.RuleVersion, i, s)
	}
	return ledger.ReplayInvoice(e.RuleVersion, i, s)
}

// cmpDecimal is a stored decimal, zero when it is empty.
func cmpDecimal(d storage.Decimal) storage.Decimal {
	if d == "" {
		return "0"
	}
	return d
}
