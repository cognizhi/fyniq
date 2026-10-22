package journal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
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
	// Clock, when set, makes posting or editing a bill post at once the releases of its spread already due by the
	// organisation's today (FR-GL-03), so its cost is never held on 1210 until the hourly job; nil leaves them to it.
	Clock ports.Clock
}

// releaseNow posts a just-posted bill's releases due by the organisation's today, after its unit of work commits. What
// it cannot post the hourly job bills.releases posts, so it never fails the posting.
func (s Bills) releaseNow(ctx context.Context, org, id uuid.UUID) {
	if s.Clock == nil {
		return
	}
	day, err := todayIn(ctx, s.Store, org, s.Clock.Now())
	if err != nil {
		return
	}
	_, _ = Releases{Store: s.Store}.ReleaseBill(ctx, org, id, day)
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
	post := ledger.PostBill
	if doc.Type == storage.DocumentVendorCredit {
		post = ledger.PostVendorCredit
	}
	entry, err := post(p.Bill, s)
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
	if !bill.IsBill(doc) {
		return &bill.Error{Code: bill.CodeNotBill, Message: fmt.Sprintf("document %s is a %s, not a bill", doc.ID, doc.Type), Fix: "Open it with its own module"}
	}
	return nil
}

// BillIdentityOf is the posting identity of a bill's or a vendor credit's version under its rule.
func BillIdentityOf(doc storage.Document) string {
	if doc.Type == storage.DocumentVendorCredit {
		return ledger.VendorCreditIdentity(doc.ID, doc.Version, ledger.RuleVendorCredit)
	}
	return ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill)
}

func billDraft(doc storage.Document, what string) error {
	if doc.Status != storage.DocumentDraft {
		return &bill.Error{Code: bill.CodeNotDraft, Message: fmt.Sprintf("the bill is %s, not a draft", doc.Status), Fix: what}
	}
	return nil
}

// Create saves a draft bill (✎). It is checked when it posts; Check shows what posting it would do.
func (s Bills) Create(ctx context.Context, org uuid.UUID, d bill.Draft) (storage.Document, error) {
	return s.create(ctx, org, storage.DocumentBill, d)
}

// CreateCredit saves a draft vendor credit (✎, FR-AP-03): a supplier's credit note, entered as a bill is and posted as
// its reverse.
func (s Bills) CreateCredit(ctx context.Context, org uuid.UUID, d bill.Draft) (storage.Document, error) {
	return s.create(ctx, org, storage.DocumentVendorCredit, d)
}

func (s Bills) create(ctx context.Context, org uuid.UUID, t storage.DocumentType, d bill.Draft) (storage.Document, error) {
	b, err := bill.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if doc, err = u.Documents().Create(ctx, storage.NewDocument{Type: t, Payload: b}); err != nil {
			return err
		}
		// A sequence that is not gap-free numbers a document when it is saved (FR-ORG-04).
		seq, err := u.Sequences().Get(ctx, t)
		if err != nil || seq.GapFree {
			return err
		}
		doc.Number, err = u.Sequences().Number(ctx, doc.ID, t)
		return err
	})
	return doc, err
}

// Revise saves a draft bill's new content as its next version, recording who saved it (FR-GL-01's preparer).
func (s Bills) Revise(ctx context.Context, org, id uuid.UUID, d bill.Draft) (storage.Document, error) {
	if _, err := bill.Encode(d); err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
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
		b, err := keepRecurring(doc, d)
		if err != nil {
			return err
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

// keepRecurring is d as a new version of doc stores it: a bill the scheduler made from a recurring bill keeps naming
// that recurring bill's version in every later version (SOD-3; FR-AP-09).
func keepRecurring(doc storage.Document, d bill.Draft) ([]byte, error) {
	if d.RecurringBill == nil {
		d.RecurringBill = bill.RecurringOf(doc)
	}
	return bill.Encode(d)
}

// CheckBill runs the rules over doc, a version of a bill, in u, and writes nothing: what Check answers, inside a unit of
// work already open.
func CheckBill(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (Checked, error) {
	if err := isBill(doc); err != nil {
		return Checked{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return Checked{}, err
	}
	return checkBill(ctx, u, doc, settings)
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
		if doc.Number, err = u.Sequences().Number(ctx, id, doc.Type); err != nil {
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
	if err == nil {
		s.releaseNow(ctx, org, id)
	}
	return out, err
}

// unpaid refuses to void or edit a bill a payment settles (FR-AR-18): its payments are voided first, which reopens it,
// so a void never leaves a payment settling nothing and an edit never changes what a payment carried.
func unpaid(ctx context.Context, u storage.UnitOfWork, doc storage.Document, what string) error {
	settled, err := u.Allocations().Settled(ctx, []uuid.UUID{doc.ID})
	if err != nil {
		return err
	}
	if x, ok := settled[doc.ID]; ok && !isZeroDecimal(string(x)) {
		if doc.Type == storage.DocumentVendorCredit {
			return &credits.Error{Code: credits.CodeApplied, Message: fmt.Sprintf("%s of the credit note is applied to bills", x),
				Fix: fmt.Sprintf("Void its applications first, then %s it", what)}
		}
		return &bill.Error{Code: bill.CodePaid, Message: fmt.Sprintf("payments settle %s of the bill", x),
			Fix: fmt.Sprintf("Void its payments first, then %s it", what)}
	}
	return nil
}

func isZeroDecimal(s string) bool {
	return isZero(strings.TrimPrefix(s, "-"))
}

// postedEntry is the entry of a posted bill's current version.
func postedEntry(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (storage.Entry, error) {
	return u.Ledger().ByIdentity(ctx, BillIdentityOf(doc))
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
		if err := unpaid(ctx, u, doc, "void"); err != nil {
			return err
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
		// Its spread's releases go back on the void's date, or on their own when later (FR-GL-03): 1210 and the line's
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
		if err := unpaid(ctx, u, doc, "edit"); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		if b, err = keepRecurring(doc, d); err != nil {
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
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
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

// later is the later of two dates.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (s Bills) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// baseOf is x in the base currency at rate (RND-FX), x itself when it is in the base currency, and x when there is no
// rate to convert it at (it then matches no stored base amount).
func baseOf(x money.Money, rate *money.Rate, base money.Currency) money.Money {
	if x.Currency() == base || rate == nil {
		return x
	}
	b, err := rounding.FX(x, *rate)
	if err != nil {
		return x
	}
	return b
}

// replayBill posts a stored bill version again under the rule that posted e (ENG-06), with the rate and the claimable
// lines the entry kept: the rate the cascade gave it (ENG-14), and each line's input tax as it was decided then, read
// from the line's cost (its net when claimed, net and GST when not), so a later change of the organisation's GST
// registration never makes an old bill differ. A spread line's cost is read from 1210, at its base amount.
func replayBill(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	p, _, err := bill.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	credit := doc.Type == storage.DocumentVendorCredit
	if credit { // a credit's lines are a bill's negated: read them as the bill's
		for i := range stored.Lines {
			stored.Lines[i].Amount, stored.Lines[i].Base = stored.Lines[i].Amount.Neg(), stored.Lines[i].Base.Neg()
		}
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
	var prepay uuid.UUID
	for _, a := range s.Accounts {
		if a.Role == ledger.RolePrepayments {
			prepay = a.ID
		}
	}
	k := 0 // the next stored line that is a bill line's cost
	for i := range b.Lines {
		l := &b.Lines[i]
		gross, _ := l.Net.Add(l.GST)
		acc, cost := l.AccountID, func(x money.Money) money.Money { return x }
		if l.Spread != nil { // a spread line's cost is on 1210 at its base amount (FR-GL-03; rev 0.83)
			acc, cost = prepay, func(x money.Money) money.Money { return baseOf(x, b.Rate, s.Base) }
		}
		next := k < len(stored.Lines) && stored.Lines[k].AccountID == acc
		switch {
		case next && l.Spread != nil && cost(gross).Equal(cost(l.Net)) && stored.Lines[k].Amount.Equal(cost(l.Net)):
			k++ // GST under a cent of the base currency: the cost cannot tell, and the reading of the bill's date stands
		case next && stored.Lines[k].Amount.Equal(cost(gross)) && !l.GST.IsZero():
			l.Claimable = false
			k++
		case next && stored.Lines[k].Amount.Equal(cost(l.Net)):
			l.Claimable = !l.GST.IsZero()
			k++
		case l.Net.IsZero():
			l.Claimable = true // a claimable line of GST alone posts no cost
		}
	}
	if credit {
		return ledger.ReplayVendorCredit(e.RuleVersion, b, s)
	}
	return ledger.ReplayBill(e.RuleVersion, b, s)
}
