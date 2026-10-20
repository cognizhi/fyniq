package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Opening balances (FR-GL-06; posting rule "Opening balances"; PRD-001 rev 0.49, provisional): the organisation's one
// opening balances document, saved as a draft as a journal is, dated at the books start, in the base currency, and
// posted as entered once it nets to zero. Share capital may take a line (INV-13). A change after posting is an edit
// under FR-GL-12 that needs a second person (FM-1); it arrives with task 2.3, so until then a posted document does
// not change.

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
				"Correct posted opening balances with a journal; changing them arrives with a second person's approval"}
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

func isOpening(doc storage.Document) error {
	if doc.Type != storage.DocumentOpeningBalance {
		return &Error{CodeNotOpening, fmt.Sprintf("document %s is a %s, not the opening balances", doc.ID, doc.Type), "Open it with its own module"}
	}
	return nil
}
