// Package journal is the manual journal, P0's posting source (FR-GL-01 in P0 form): a document
// that is drafted, posted and reversed. Each command runs in one unit of work: the document change
// and its entry commit together or not at all (ENG-02), and the entry is produced by the pure
// engine in package ledger (ENG-01).
package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable error codes of the journal commands (ENG-23).
const (
	CodeNotJournal = "JOURNAL-01" // the document is not a manual journal
	CodeNotDraft   = "JOURNAL-02" // only a draft can be posted
	CodeNotPosted  = "JOURNAL-03" // only a posted journal can be reversed
	CodeBadDraft   = "JOURNAL-04" // the saved draft cannot be read
	CodeReverseOn  = "JOURNAL-11" // the automatic reversal is not dated after the journal (FR-GL-02)
)

// Error is a journal command failure with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is a manual journal as entered: debits positive, credits negative.
type Draft struct {
	Date  time.Time
	Memo  string
	Lines []ledger.JournalLine
	// ReverseOn is when the scheduler reverses the posted journal by itself (FR-GL-02): after Date, often the first day
	// of the next period; zero for a journal that does not reverse by itself.
	ReverseOn time.Time
	// RecurringJournal is the recurring journal version the scheduler made it from (FR-GL-02; rev 0.60), nil for one a
	// person made. Saving a draft or an edit keeps it, so the journal still says where it came from.
	RecurringJournal *RecurringRef
}

// RecurringRef names the recurring journal, and the version of its content, that a journal was made from.
type RecurringRef struct {
	ID      uuid.UUID `json:"id"`
	Version int       `json:"version"`
}

// payload is a draft as the document version stores it: dates and amounts as text.
type payload struct {
	Date      string        `json:"date"`
	Memo      string        `json:"memo,omitempty"`
	Lines     []payloadLine `json:"lines"`
	ReverseOn string        `json:"reverse_on,omitempty"`
	// RecurringJournal is written by the database's create_scheduled_draft, and kept by every later version.
	RecurringJournal *RecurringRef `json:"recurring_journal,omitempty"`
}

type payloadLine struct {
	AccountID    uuid.UUID   `json:"account_id"`
	Amount       money.Money `json:"amount"`
	FXRate       string      `json:"fx_rate,omitempty"`
	FXRateSource string      `json:"fx_rate_source,omitempty"`
	Description  string      `json:"description,omitempty"`
}

// Service runs the journal commands against a store.
type Service struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the
	// document updated, for work that must commit with the posting. An error rolls everything back.
	// (The audit events need no hook: the unit of work records every change it makes.)
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
	// AfterMaking runs inside the unit of work that made a recurring journal's draft (Made); nil does nothing.
	AfterMaking Made
}

// Made runs inside the unit of work that made a draft dated on from the recurring journal or bill named name, for work
// that must commit with it, such as telling those who post it (notify.RecurringMade). An error rolls the draft back.
type Made func(ctx context.Context, uow storage.UnitOfWork, org uuid.UUID, name string, draft storage.Document, on time.Time) error

// encode is a draft as a document version stores it.
func encode(d Draft) ([]byte, error) {
	p := payload{Date: d.Date.Format(time.DateOnly), Memo: d.Memo, RecurringJournal: d.RecurringJournal}
	if d.Date.IsZero() {
		p.Date = ""
	}
	if !d.ReverseOn.IsZero() {
		p.ReverseOn = d.ReverseOn.Format(time.DateOnly)
	}
	p.Lines = linesOf(d.Lines)
	return json.Marshal(p)
}

// linesOf are journal lines as a document version stores them.
func linesOf(lines []ledger.JournalLine) []payloadLine {
	var out []payloadLine
	for _, l := range lines {
		pl := payloadLine{AccountID: l.AccountID, Amount: l.Amount, Description: l.Description}
		if l.Rate != nil {
			pl.FXRate, pl.FXRateSource = l.Rate.String(), l.Rate.Source
		}
		out = append(out, pl)
	}
	return out
}

// Create saves a draft (✎). Drafts are not validated beyond their amounts: Post validates.
func (s Service) Create(ctx context.Context, org uuid.UUID, d Draft) (storage.Document, error) {
	b, err := encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if doc, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: b}); err != nil {
			return err
		}
		// A sequence that is not gap-free numbers a journal when it is saved (FR-ORG-04).
		seq, err := u.Sequences().Get(ctx, storage.DocumentJournal)
		if err != nil || seq.GapFree {
			return err
		}
		doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentJournal)
		return err
	})
	return doc, err
}

// Revise saves a draft's new content as its next version, recording who saved it (FR-GL-01). Only a draft
// changes this way; a posted journal is corrected by reversal (FR-GL-12).
func (s Service) Revise(ctx context.Context, org, id uuid.UUID, d Draft) (storage.Document, error) {
	if _, err := encode(d); err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isJournal(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &Error{CodeNotDraft, fmt.Sprintf("the journal is %s, not a draft", doc.Status), "Only a draft changes; reverse a posted journal instead"}
		}
		b, err := keepOrigin(doc, d)
		if err != nil {
			return err
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

// keepOrigin encodes d with the recurring journal the current version of doc came from, unless d names one.
func keepOrigin(doc storage.Document, d Draft) ([]byte, error) {
	if d.RecurringJournal == nil {
		if saved, err := readSaved(doc); err == nil {
			d.RecurringJournal = saved.RecurringJournal
		}
	}
	return encode(d)
}

// Post posts a draft: the engine turns its current version into an entry, which is written with
// the document's change of status in one transaction.
func (s Service) Post(ctx context.Context, org, id uuid.UUID) (storage.Entry, error) {
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isJournal(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &Error{CodeNotDraft, fmt.Sprintf("the journal is %s, not a draft", doc.Status), "Only a draft can be posted; reverse a posted journal instead"}
		}
		// ENG-47 step 1a: the journal's number, in this transaction under the sequence's lock, so a posting
		// that fails takes no number (FR-ORG-04); a journal numbered when it was saved keeps its number.
		if doc.Number, err = u.Sequences().Number(ctx, id, storage.DocumentJournal); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		j, err := decode(doc, settings.Base)
		if err != nil {
			return err
		}
		entry, err := postRated(ctx, u, j, settings)
		if err != nil {
			return err
		}
		if out, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
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

// Reverse undoes a posted journal with a reversing entry dated date (FR-GL-12); posted history is
// never changed.
func (s Service) Reverse(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (storage.Entry, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason) // the audit events record why (FR-GL-12: void = reversal with reason)
	}
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if err := isJournal(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentPosted {
			return &Error{CodeNotPosted, fmt.Sprintf("the journal is %s, not posted", doc.Status), "Only a posted journal can be reversed"}
		}
		org, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(org.BaseCurrency)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.JournalIdentity(id, doc.Version, ledger.RuleManualJournal))
		if err != nil {
			return err
		}
		original, err := fromStorage(stored, base)
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

// Edited is what an edit of a posted journal posts: the reversal of the entry it had, and the entry of its new version.
type Edited struct {
	Reversal, Entry storage.Entry
}

// Edit corrects a posted journal in place of a void and a new journal (FR-GL-12, posting rule "Edits of posted
// documents"): it saves d as the journal's next version, reverses the entry of the version it had on that entry's own
// date, and posts the new version's entry on its date, in one transaction; the journal stays posted, with the actor as
// who posted the edit, and its number. Both periods must be open (INV-03); the database refuses an edit without both
// postings (FY055). preparer is who prepared the new version ("type:id"), empty for the actor: an approved edit names
// its requester. The reason goes with every audit event.
func (s Service) Edit(ctx context.Context, org, id uuid.UUID, d Draft, reason, preparer string) (Edited, error) {
	b, err := encode(d)
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
		if err := editable(doc); err != nil {
			return err
		}
		if b, err = keepOrigin(doc, d); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		// The new version's entry first: an edit the rules refuse writes nothing.
		next := doc
		next.Version, next.Payload = doc.Version+1, b
		j, err := decode(next, settings.Base)
		if err != nil {
			return err
		}
		entry, err := postRated(ctx, u, j, settings)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.JournalIdentity(id, doc.Version, ledger.RuleManualJournal))
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
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
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

// editable refuses an edit of anything but a posted manual journal.
func editable(doc storage.Document) error {
	if err := isJournal(doc); err != nil {
		return err
	}
	if doc.Status != storage.DocumentPosted {
		return &Error{CodeNotPosted, fmt.Sprintf("the journal is %s, not posted", doc.Status), "Only a posted journal is edited this way; change a draft as it is"}
	}
	return nil
}

func (s Service) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

func isJournal(doc storage.Document) error {
	if doc.Type != storage.DocumentJournal {
		return &Error{CodeNotJournal, fmt.Sprintf("document %s is a %s, not a manual journal", doc.ID, doc.Type), "Open it with its own module"}
	}
	return nil
}

// LoadSettings reads what the engine needs, for a reader that finds a document's amounts the way the rules do, such as
// what is left of a customer deposit.
func LoadSettings(ctx context.Context, u storage.UnitOfWork) (ledger.Settings, error) {
	return loadSettings(ctx, u)
}

// loadSettings reads what the engine needs: the base currency and the chart of accounts.
func loadSettings(ctx context.Context, u storage.UnitOfWork) (ledger.Settings, error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return ledger.Settings{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return ledger.Settings{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return ledger.Settings{}, err
	}
	s := ledger.Settings{Base: base, Accounts: make(map[uuid.UUID]ledger.Account, len(accounts))}
	for _, a := range accounts {
		s.Accounts[a.ID] = ledger.Account{ID: a.ID, Code: a.Code, Group: a.Group, Active: a.Active, ShareCapital: a.Role == storage.RoleShareCapital,
			Revalue: a.Revalue, UnrealisedFX: a.Role == storage.RoleUnrealisedFX, Role: string(a.Role), Money: a.MoneyAccount, Currency: a.Currency, Type: string(a.Type)}
	}
	return s, nil
}

// Saved is a journal version as it was saved, for reading it back: the date (empty when none was
// entered), the memo and the lines, with amounts and rates as entered.
type Saved struct {
	Date      string
	Memo      string
	Lines     []SavedLine
	ReverseOn string // empty for a journal that does not reverse by itself
	// RecurringJournal is the recurring journal version it was made from, nil for one a person made (rev 0.60).
	RecurringJournal *RecurringRef
}

// SavedLine is one line of a saved journal.
type SavedLine struct {
	AccountID    uuid.UUID
	Amount       money.Money
	FXRate       string
	FXRateSource string
	Description  string
}

// ReadSaved reads a journal version as it was saved.
func ReadSaved(doc storage.Document) (Saved, error) {
	if err := isJournal(doc); err != nil {
		return Saved{}, err
	}
	return readSaved(doc)
}

// ReadOpening reads a version of the opening balances, which are saved as a journal is.
func ReadOpening(doc storage.Document) (Saved, error) {
	if err := isOpening(doc); err != nil {
		return Saved{}, err
	}
	return readSaved(doc)
}

func readSaved(doc storage.Document) (Saved, error) {
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Saved{}, &Error{CodeBadDraft, fmt.Sprintf("the saved journal cannot be read: %v", err), "Save the draft again"}
	}
	out := Saved{Date: p.Date, Memo: p.Memo, Lines: make([]SavedLine, 0, len(p.Lines)), ReverseOn: p.ReverseOn, RecurringJournal: p.RecurringJournal}
	for _, l := range p.Lines {
		out.Lines = append(out.Lines, SavedLine{AccountID: l.AccountID, Amount: l.Amount, FXRate: l.FXRate,
			FXRateSource: l.FXRateSource, Description: l.Description})
	}
	return out, nil
}

// decode reads a stored journal version.
func decode(doc storage.Document, base money.Currency) (ledger.Journal, error) {
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return ledger.Journal{}, &Error{CodeBadDraft, fmt.Sprintf("the saved journal cannot be read: %v", err), "Save the draft again"}
	}
	j := ledger.Journal{ID: doc.ID, Version: doc.Version, Memo: p.Memo}
	if p.Date != "" {
		d, err := time.Parse(time.DateOnly, p.Date)
		if err != nil {
			return ledger.Journal{}, &Error{CodeBadDraft, fmt.Sprintf("the saved date %q is not a date", p.Date), "Save the draft again"}
		}
		j.Date = d
	}
	// The automatic reversal is after the journal (FR-GL-02): checked wherever the rules run, so it never posts otherwise.
	if p.ReverseOn != "" {
		on, err := time.Parse(time.DateOnly, p.ReverseOn)
		if err != nil {
			return ledger.Journal{}, &Error{CodeBadDraft, fmt.Sprintf("the saved reversal date %q is not a date", p.ReverseOn), "Save the draft again"}
		}
		if !j.Date.IsZero() && !on.After(j.Date) {
			return ledger.Journal{}, &Error{CodeReverseOn, fmt.Sprintf("the automatic reversal is dated %s, not after the journal's date %s", p.ReverseOn, p.Date),
				"Date the automatic reversal after the journal, such as the first day of the next month"}
		}
	}
	for i, l := range p.Lines {
		line := ledger.JournalLine{AccountID: l.AccountID, Amount: l.Amount, Description: l.Description}
		if l.FXRate != "" {
			r, err := money.ParseRate(l.FXRate, l.Amount.Currency(), base, l.FXRateSource)
			if err != nil {
				return ledger.Journal{}, &Error{CodeBadDraft, fmt.Sprintf("line %d: %v", i+1, err), "Correct the rate and save the draft again"}
			}
			line.Rate = &r
		}
		j.Lines = append(j.Lines, line)
	}
	return j, nil
}

// toStorage turns the engine's entry into the port's, as exact decimal text.
func toStorage(e ledger.Entry, reverses *uuid.UUID) storage.NewEntry {
	out := storage.NewEntry{
		PostingDate: e.PostingDate, PostingIdentity: e.PostingIdentity, RuleVersion: e.RuleVersion, Memo: e.Memo,
		Source:          storage.Source{Type: e.Source.Type, ID: e.Source.ID, Version: e.Source.Version},
		ReversesEntryID: reverses,
	}
	for _, l := range e.Lines {
		out.Lines = append(out.Lines, storage.NewLine{
			AccountID: l.AccountID, Currency: l.Amount.Currency().Code, Amount: storage.Decimal(l.Amount.String()),
			FXRate: storage.Decimal(l.Rate.String()), FXRateSource: l.Rate.Source, BaseAmount: storage.Decimal(l.Base.String()),
			Description: l.Description,
		})
	}
	return out
}

// fromStorage reads a posted entry back into the engine's form, for its reversal.
func fromStorage(e storage.Entry, base money.Currency) (ledger.Entry, error) {
	out := ledger.Entry{
		PostingDate: e.PostingDate, PostingIdentity: e.PostingIdentity, RuleVersion: e.RuleVersion, Memo: e.Memo,
		Source: ledger.Source{Type: e.Source.Type, ID: e.Source.ID, Version: e.Source.Version},
	}
	for _, l := range e.Lines {
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return ledger.Entry{}, err
		}
		amount, err := money.ParseStored(string(l.Amount), cur)
		if err != nil {
			return ledger.Entry{}, err
		}
		baseAmount, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return ledger.Entry{}, err
		}
		rate, err := money.ParseRate(string(l.FXRate), cur, base, l.FXRateSource)
		if err != nil {
			return ledger.Entry{}, err
		}
		out.Lines = append(out.Lines, ledger.Line{AccountID: l.AccountID, Amount: amount, Rate: rate, Base: baseAmount, Description: l.Description})
	}
	return out, nil
}

// Debits runs the posting rules over a draft's current version without writing anything, and answers its debits in
// the base currency: the amount its approval is judged on (FM-2, SOD-3). A draft the rules refuse answers their
// problems, as posting it would.
func (s Service) Debits(ctx context.Context, org, id uuid.UUID) (money.Money, error) {
	var out money.Money
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		if err := isJournal(doc); err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &Error{CodeNotDraft, fmt.Sprintf("the journal is %s, not a draft", doc.Status), "Only a draft is posted"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		j, err := decode(doc, settings.Base)
		if err != nil {
			return err
		}
		entry, err := postRated(ctx, u, j, settings)
		if err != nil {
			return err
		}
		out, err = debitsOf(entry, settings.Base)
		return err
	})
	return out, err
}

// EditDebits runs the posting rules over the edit d of a posted journal without writing anything, and answers its
// debits in the base currency, as Debits does for a draft (FM-2): an edit the rules refuse answers their problems.
func (s Service) EditDebits(ctx context.Context, org, id uuid.UUID, d Draft) (money.Money, error) {
	b, err := encode(d)
	if err != nil {
		return money.Money{}, err
	}
	var out money.Money
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		if err := editable(doc); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		doc.Version, doc.Payload = doc.Version+1, b
		j, err := decode(doc, settings.Base)
		if err != nil {
			return err
		}
		entry, err := postRated(ctx, u, j, settings)
		if err != nil {
			return err
		}
		out, err = debitsOf(entry, settings.Base)
		return err
	})
	return out, err
}

// debitsOf adds up an entry's debits in the base currency.
func debitsOf(e ledger.Entry, base money.Currency) (money.Money, error) {
	out := money.Zero(base)
	for _, l := range e.Lines {
		if l.Base.Sign() > 0 {
			var err error
			if out, err = out.Add(l.Base); err != nil {
				return money.Money{}, err
			}
		}
	}
	return out, nil
}
