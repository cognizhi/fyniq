package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Opening balances (FR-GL-06; posting rule "Opening balances"; PRD-001 rev 0.49, provisional): the organisation's one
// opening balances document, saved as a draft as a journal is, dated at the books start, in the base currency, and
// posted as entered once it nets to zero. Share capital may take a line (INV-13). A change after posting is an edit
// under FR-GL-12 that needs a second person in every team mode (FM-1; PRD-001 rev 0.66, provisional): EditOpening runs
// it on the approval.

// The stable error codes of the opening balances (ENG-23).
const (
	CodeNotOpening   = "JOURNAL-05" // the document is not the opening balances
	CodeNoBooksStart = "JOURNAL-06" // the books start is not set, so the opening balances have no date
)

// Opening is the organisation's opening balances document at its current version, or ports.ErrNotFound when none is
// saved yet.
func (s Service) Opening(ctx context.Context, org uuid.UUID) (storage.Document, error) {
	var doc storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		doc, err = u.Documents().OpeningBalance(ctx)
		return err
	})
	return doc, err
}

// SaveOpening saves the opening balances as a draft dated at the books start: the first save creates the document,
// each later one its next version. Like a journal's draft, it is checked only for its amounts; posting checks the rest.
func (s Service) SaveOpening(ctx context.Context, org uuid.UUID, memo string, lines []ledger.JournalLine) (storage.Document, error) {
	var out storage.Document
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if o.Profile.BooksStart == nil {
			return &Error{CodeNoBooksStart, "the books start is not set, so the opening balances have no date",
				"Enter when the books start in Settings › Organisation"}
		}
		b, err := encode(Draft{Date: *o.Profile.BooksStart, Memo: memo, Lines: lines})
		if err != nil {
			return err
		}
		cur, err := u.Documents().OpeningBalance(ctx)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			out, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOpeningBalance, Payload: b})
			return err
		case err != nil:
			return err
		}
		if cur, err = u.Documents().Lock(ctx, cur.ID); err != nil { // ENG-47 step 1
			return err
		}
		if cur.Status != storage.DocumentDraft {
			return &Error{CodeNotDraft, fmt.Sprintf("the opening balances are %s, not a draft", cur.Status),
				"Ask for a second person's approval to change posted opening balances"}
		}
		out, err = u.Documents().Revise(ctx, cur.ID, b)
		return err
	})
	return out, err
}

// PostOpening posts the opening balances' current version at the books start (FR-GL-06), with the document's change
// to posted in one transaction (ENG-02).
func (s Service) PostOpening(ctx context.Context, org uuid.UUID) (storage.Entry, error) {
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		found, err := u.Documents().OpeningBalance(ctx)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Lock(ctx, found.ID) // ENG-47 step 1
		if err != nil {
			return err
		}
		if doc.Status != storage.DocumentDraft {
			return &Error{CodeNotDraft, fmt.Sprintf("the opening balances are %s, not a draft", doc.Status), "They post once"}
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if o.Profile.BooksStart == nil {
			return &Error{CodeNoBooksStart, "the books start is not set, so the opening balances have no date",
				"Enter when the books start in Settings › Organisation"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		j, err := decode(doc, settings.Base)
		if err != nil {
			return err
		}
		j.Date = *o.Profile.BooksStart // always the books start, whatever the draft was dated when saved
		entry, err := ledger.PostOpening(j, settings)
		if err != nil {
			return err
		}
		if out, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		return s.after(ctx, u, doc, out)
	})
	return out, err
}

// openingEdit is the posted opening balances' next version, at the books start, and the entries an edit to it writes:
// the reversal of the entry they have, on that entry's date, and the new version's entry. It refuses anything but
// posted opening balances, and content the posting rules refuse.
func openingEdit(ctx context.Context, u storage.UnitOfWork, memo string, lines []ledger.JournalLine, reason string) (doc storage.Document, payload []byte,
	reversal ledger.Entry, entry ledger.Entry, stored storage.Entry, err error) {
	found, err := u.Documents().OpeningBalance(ctx)
	if err != nil {
		return
	}
	if doc, err = u.Documents().Lock(ctx, found.ID); err != nil { // ENG-47 step 1
		return
	}
	if doc.Status != storage.DocumentPosted {
		err = &Error{CodeNotPosted, fmt.Sprintf("the opening balances are %s, not posted", doc.Status), "Change a draft by saving it again"}
		return
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return
	}
	if o.Profile.BooksStart == nil {
		err = &Error{CodeNoBooksStart, "the books start is not set, so the opening balances have no date", "Enter when the books start in Settings › Organisation"}
		return
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return
	}
	if payload, err = encode(Draft{Date: *o.Profile.BooksStart, Memo: memo, Lines: lines}); err != nil {
		return
	}
	next := doc
	next.Version, next.Payload = doc.Version+1, payload
	j, err := decode(next, settings.Base)
	if err != nil {
		return
	}
	if entry, err = ledger.PostOpening(j, settings); err != nil { // the new version's entry first: content the rules refuse writes nothing
		return
	}
	if stored, err = u.Ledger().ByIdentity(ctx, ledger.OpeningIdentity(doc.ID, doc.Version, ledger.RuleOpeningBalance)); err != nil {
		return
	}
	original, err := fromStorage(stored, settings.Base)
	if err != nil {
		return
	}
	reversal, err = ledger.Reverse(original, original.PostingDate, reason)
	return
}

// OpeningEditDebits are the debits, in the base currency, of what an edit of the posted opening balances would post,
// after checking it against the posting rules, as a request for the edit shows them; it saves nothing.
func (s Service) OpeningEditDebits(ctx context.Context, org uuid.UUID, memo string, lines []ledger.JournalLine) (money.Money, error) {
	var out money.Money
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, _, entry, _, err := openingEdit(ctx, u, memo, lines, "")
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		out, err = debitsOf(entry, settings.Base)
		return err
	})
	return out, err
}

// EditOpening changes posted opening balances (FR-GL-06, FR-GL-12, FM-1; rev 0.66): in one transaction it saves their
// next version at the books start, reverses the entry they had on that entry's own date, and posts the new version's
// entry, so the month the books start in must be open. It runs on an approved request, as the approver, with the
// requester as the one who prepared the new version (preparer); the database refuses an edit without both postings
// (FY055).
func (s Service) EditOpening(ctx context.Context, org uuid.UUID, memo string, lines []ledger.JournalLine, reason, preparer string) (Edited, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out Edited
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, payload, reversal, entry, stored, err := openingEdit(ctx, u, memo, lines, reason)
		if err != nil {
			return err
		}
		edited, err := u.Documents().Edit(ctx, doc.ID, payload, preparer)
		if err != nil {
			return err
		}
		if out.Reversal, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
			return err
		}
		if err := s.after(ctx, u, edited, out.Reversal); err != nil {
			return err
		}
		return s.after(ctx, u, edited, out.Entry)
	})
	return out, err
}

func isOpening(doc storage.Document) error {
	if doc.Type != storage.DocumentOpeningBalance {
		return &Error{CodeNotOpening, fmt.Sprintf("document %s is a %s, not the opening balances", doc.ID, doc.Type), "Open it with its own module"}
	}
	return nil
}
