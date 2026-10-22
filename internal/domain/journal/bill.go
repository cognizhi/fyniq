package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Bills runs the bill commands (FR-AP-02; p1-purchases) against a store: each in one unit of work, the document and
// its entry together (ENG-02), the entry from the rule bill@1 (ENG-01). As journals are, a bill is drafted, posted,
// voided by a reversal with a reason, or edited by reversing the entry of the version it had and posting the new one
// (FR-GL-12); posted history never changes.
type Bills struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// Checked is a bill version as posting it would post it, written nowhere: what the rules read, the entry, and the
// problems that stop it posting (✖) beside its warnings (⚠, on Prepared). Payable is the bill's total in the base
// currency, as its entry credits accounts payable: what its approval is judged on (FM-2); zero when it would not post.
type Checked struct {
	Prepared bill.Prepared
	Entry    ledger.Entry
	Problems ledger.Problems
	Payable  money.Money
}

// check runs the rules over doc, a version of a bill, and gathers every problem together (ENG-22): the bill's own and
// the engine's.
func checkBill(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (Checked, error) {
	p, probs, err := bill.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return Checked{}, err
	}
	out := Checked{Prepared: p, Problems: probs}
	entry, err := ledger.PostBill(p.Bill, s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate) {
				continue // the cascade found no rate: FX-01 says so, in place of the engine's LEDGER-07
			}
			out.Problems = append(out.Problems, x)
		}
	} else if err != nil {
		return Checked{}, err
	}
	out.Payable = money.Zero(s.Base)
	if len(out.Problems) == 0 {
		out.Entry = entry
		for _, l := range entry.Lines {
			if s.Accounts[l.AccountID].Role == ledger.RoleAccountsPayable {
				out.Payable = l.Base.Neg()
			}
		}
	}
	return out, nil
}

func isBill(doc storage.Document) error {
	if doc.Type != storage.DocumentBill {
		return &bill.Error{Code: bill.CodeNotBill, Message: fmt.Sprintf("document %s is a %s, not a bill", doc.ID, doc.Type), Fix: "Open it with its own module"}
	}
	return nil
}

func billDraft(doc storage.Document, what string) error {
	if doc.Status != storage.DocumentDraft {
		return &bill.Error{Code: bill.CodeNotDraft, Message: fmt.Sprintf("the bill is %s, not a draft", doc.Status), Fix: what}
	}
	return nil
}

// Create saves a draft bill (✎). It is checked when it posts; Check shows what posting it would do.
func (s Bills) Create(ctx context.Context, org uuid.UUID, d bill.Draft) (storage.Document, error) {
	b, err := bill.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if doc, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBill, Payload: b}); err != nil {
			return err
		}
		// A sequence that is not gap-free numbers a bill when it is saved (FR-ORG-04).
		seq, err := u.Sequences().Get(ctx, storage.DocumentBill)
		if err != nil || seq.GapFree {
			return err
		}
		doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentBill)
		return err
	})
	return doc, err
}

// Revise saves a draft bill's new content as its next version, recording who saved it (FR-GL-01's preparer).
func (s Bills) Revise(ctx context.Context, org, id uuid.UUID, d bill.Draft) (storage.Document, error) {
	b, err := bill.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isBill(doc); err != nil {
			return err
		}
		if err := billDraft(doc, "Only a draft changes; edit a posted bill instead"); err != nil {
			return err
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

// Check runs the rules over a bill's current version, or its edit d when d is not nil, and writes nothing: the
// ledger impact a person sees before posting, the problems that would stop it, its warnings, and its total.
func (s Bills) Check(ctx context.Context, org, id uuid.UUID, d *bill.Draft) (Checked, error) {
	var out Checked
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		if err := isBill(doc); err != nil {
			return err
		}
		if d != nil {
			b, err := bill.Encode(*d)
			if err != nil {
				return err
			}
			doc.Version, doc.Payload = doc.Version+1, b
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		out, err = checkBill(ctx, u, doc, settings)
		return err
	})
	return out, err
}

// Post posts a draft bill: its number (gap-free, in this transaction), then the entry of its current version, with the
// document's change of status, in one transaction. A bill the rules refuse posts nothing and answers every problem.
func (s Bills) Post(ctx context.Context, org, id uuid.UUID) (storage.Entry, error) {
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isBill(doc); err != nil {
			return err
		}
		if err := billDraft(doc, "Only a draft is posted; edit or void a posted bill instead"); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		c, err := checkBill(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		if doc.Number, err = u.Sequences().Number(ctx, id, storage.DocumentBill); err != nil {
			return err
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

// postedEntry is the entry of a posted bill's current version.
func postedEntry(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (storage.Entry, error) {
	return u.Ledger().ByIdentity(ctx, ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill))
}

// Void undoes a posted bill with a reversing entry dated date, with a reason (FR-GL-12: a void is a reversal); the bill
// is marked reversed. Its period must be open (INV-03).
func (s Bills) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (storage.Entry, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isBill(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &bill.Error{Code: bill.CodeNotPosted, Message: fmt.Sprintf("the bill is %s, not posted", doc.Status), Fix: "Only a posted bill is voided; a draft is simply not posted"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		stored, err := postedEntry(ctx, u, doc)
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

// Edit corrects a posted bill (FR-GL-12, posting rule "Edits of posted documents"): it saves d as the bill's next
// version, reverses the entry of the version it had on that entry's own date, and posts the new version's entry, in one
// transaction; the bill stays posted, with its number, and the actor as who posted the edit. preparer is who prepared
// the new version ("type:id"), empty for the actor. An edit the rules refuse writes nothing.
func (s Bills) Edit(ctx context.Context, org, id uuid.UUID, d bill.Draft, reason, preparer string) (Edited, error) {
	b, err := bill.Encode(d)
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
		if err := isBill(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &bill.Error{Code: bill.CodeNotPosted, Message: fmt.Sprintf("the bill is %s, not posted", doc.Status), Fix: "Only a posted bill is edited this way; change a draft as it is"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		next := doc
		next.Version, next.Payload = doc.Version+1, b
		c, err := checkBill(ctx, u, next, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		stored, err := postedEntry(ctx, u, doc)
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

func (s Bills) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayBill posts a stored bill version again under the rule that posted e (ENG-06), with the rate and the claimable
// lines the entry kept: the rate the cascade gave it (ENG-14), and each line's input tax as it was decided then, read
// from the line's cost (its net when claimed, net and GST when not), so a later change of the organisation's GST
// registration never makes an old bill differ.
func replayBill(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	p, _, err := bill.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	b := p.Bill
	b.Memo = stored.Memo // it names the supplier as it was named then
	if p.Draft.Rate == nil && b.Currency != s.Base {
		b.Rate = nil
		for _, x := range stored.Lines { // the cascade's rate as the entry kept it, on every line in the bill's currency
			if x.Amount.Currency() == b.Currency {
				r := x.Rate
				b.Rate = &r
				break
			}
		}
	}
	k := 0 // the next stored line that is a bill line's cost
	for i := range b.Lines {
		l := &b.Lines[i]
		gross, _ := l.Net.Add(l.GST)
		switch {
		case k < len(stored.Lines) && stored.Lines[k].AccountID == l.AccountID && stored.Lines[k].Amount.Equal(gross) && !l.GST.IsZero():
			l.Claimable = false
			k++
		case k < len(stored.Lines) && stored.Lines[k].AccountID == l.AccountID && stored.Lines[k].Amount.Equal(l.Net):
			l.Claimable = !l.GST.IsZero()
			k++
		case l.Net.IsZero():
			l.Claimable = true // a claimable line of GST alone posts no cost
		}
	}
	return ledger.ReplayBill(e.RuleVersion, b, s)
}
