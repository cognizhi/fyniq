package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// BankTransactions categorises bank lines (FR-BNK-06, FR-BNK-11; p1-bank-import-reconcile task 4.1, design D4)
// against a store: in one unit of work, the bank transaction, its entry from bank_transaction@1, the line's match and
// the line's status together (ENG-02). A line with a part on the suspense account (2999) stays in the queue as
// suspense until it is classified, which undoes its suspense posting and posts the new one in the same unit of work.
// Undoing a categorisation reverses its entry and the releases it posted, on their own dates, marks the transaction
// reversed, undoes the match and puts the line back in the queue (FR-GL-12); its period must be open.
type BankTransactions struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
	// Clock, when set, makes categorising a line with a spread post at once the releases already due by the
	// organisation's today (FR-GL-03); nil leaves them to the hourly job bank.releases.
	Clock ports.Clock
}

// The stable codes of categorising (ENG-23).
const (
	CodeLineDone    = "BANKTXN-08" // the line is not waiting to be categorised: categorised, matched, a possible duplicate or excluded
	CodeNotCategory = "BANKTXN-09" // the line's live match is not a categorisation, so there is nothing of it to undo here
)

// Categorisation is a categorisation as it would be made: the draft, how it is made (a person's, or under an
// auto-apply rule) and when.
type Categorisation struct {
	Draft  banktransaction.Draft
	Basis  string     // storage.BasisPerson or storage.BasisRule
	RuleID *uuid.UUID // the auto-apply rule, for storage.BasisRule
	At     time.Time
}

// CheckedCategorisation is a categorisation as it would post, written nowhere: what the rule reads, the entry, and the
// problems that stop it (✖).
type CheckedCategorisation struct {
	Saved    banktransaction.Saved
	Entry    ledger.Entry
	Problems ledger.Problems
}

// Categorised is a categorised line: its bank transaction, the entry it posted, its match and the line as it now is.
type Categorised struct {
	Transaction storage.Document
	Entry       storage.Entry
	Match       storage.BankMatch
	Line        storage.BankLine
}

// checkCategorisation runs the rule over a draft for doc, written nowhere.
func checkCategorisation(ctx context.Context, u storage.UnitOfWork, doc storage.Document, d banktransaction.Draft, s ledger.Settings) (CheckedCategorisation, error) {
	saved, probs, err := banktransaction.Prepare(ctx, u, d, s)
	if err != nil {
		return CheckedCategorisation{}, err
	}
	out := CheckedCategorisation{Saved: saved, Problems: probs}
	entry, err := ledger.PostBankTransaction(banktransaction.Transaction(doc, saved), s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate) {
				continue // the cascade found no rate: FX-01 says so, in place of the engine's LEDGER-07
			}
			out.Problems = append(out.Problems, x)
		}
	} else if err != nil {
		return CheckedCategorisation{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry = entry
	}
	return out, nil
}

// Check runs the rule over a categorisation as it would be made, and writes nothing: the ledger impact a person sees
// before accepting it (FR-INT-14), and the problems that would stop it.
func (s BankTransactions) Check(ctx context.Context, org uuid.UUID, d banktransaction.Draft) (CheckedCategorisation, error) {
	var out CheckedCategorisation
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		doc := storage.Document{ID: uuid.Nil, Type: storage.DocumentBankTransaction, Version: 1, Status: storage.DocumentDraft}
		out, err = checkCategorisation(ctx, u, doc, d, settings)
		return err
	})
	return out, err
}

// Categorise categorises a line waiting in the queue, unreconciled or in suspense, and posts it in one transaction. A
// categorisation the rules refuse writes nothing and answers every problem.
func (s BankTransactions) Categorise(ctx context.Context, org uuid.UUID, c Categorisation) (Categorised, error) {
	var out Categorised
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = s.categorise(ctx, u, c)
		return err
	})
	if err == nil {
		s.releaseNow(ctx, org, out.Transaction.ID)
	}
	return out, err
}

func (s BankTransactions) categorise(ctx context.Context, u storage.UnitOfWork, c Categorisation) (Categorised, error) {
	line, err := u.Banking().Line(ctx, c.Draft.LineID)
	if errors.Is(err, ports.ErrNotFound) {
		return Categorised{}, &banktransaction.Error{Code: banktransaction.CodeLine, Message: "the bank line does not exist in this organisation",
			Fix: "Choose a line from the Categorise queue"}
	}
	if err != nil {
		return Categorised{}, err
	}
	switch line.Status {
	case storage.LineUnreconciled:
	case storage.LineSuspense: // classified: its suspense posting is reversed first, in this unit of work
		if err := s.reverse(ctx, u, line, "Classified from suspense", c.At); err != nil {
			return Categorised{}, err
		}
	default:
		return Categorised{}, &banktransaction.Error{Code: CodeLineDone, Message: fmt.Sprintf("the line is %s, not waiting to be categorised", line.Status),
			Fix: "Undo what it is matched to first, or resolve the possible duplicate"}
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return Categorised{}, err
	}
	// Checked first with a document of no ID, so a refusal writes nothing at all.
	checked, err := checkCategorisation(ctx, u, storage.Document{Type: storage.DocumentBankTransaction, Version: 1}, c.Draft, settings)
	if err != nil {
		return Categorised{}, err
	}
	if len(checked.Problems) > 0 {
		return Categorised{}, checked.Problems
	}
	b, err := banktransaction.Encode(checked.Saved)
	if err != nil {
		return Categorised{}, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBankTransaction, Payload: b})
	if err != nil {
		return Categorised{}, err
	}
	entry, err := ledger.PostBankTransaction(banktransaction.Transaction(doc, checked.Saved), settings)
	if err != nil {
		return Categorised{}, err
	}
	out := Categorised{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
		return Categorised{}, err
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return Categorised{}, err
	}
	doc.Status = storage.DocumentPosted
	out.Transaction = doc
	kind, status := storage.MatchCategorised, storage.LineCategorised
	for _, p := range checked.Saved.Parts {
		if settings.Accounts[p.AccountID].Role == ledger.RoleSuspense {
			kind, status = storage.MatchSuspense, storage.LineSuspense
		}
	}
	basis := c.Basis
	if basis == "" {
		basis = storage.BasisPerson
	}
	if out.Match, err = u.BankMatching().MakeMatch(ctx, storage.NewBankMatch{LineID: line.ID, Kind: kind, DocumentID: &doc.ID, EntryID: &out.Entry.ID,
		Basis: basis, RuleID: c.RuleID, At: c.At}); err != nil {
		return Categorised{}, err
	}
	if out.Line, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, status, c.At); err != nil {
		return Categorised{}, err
	}
	return out, s.after(ctx, u, doc, out.Entry)
}

// Undo undoes the categorisation of a line with a reason: its entry and the releases it posted are reversed on their
// own dates, the bank transaction is marked reversed, the match is undone and the line goes back to the queue
// unreconciled. The line's period must be open (FY220, FY224, INV-03). A line in suspense is classified instead.
func (s BankTransactions) Undo(ctx context.Context, org, lineID uuid.UUID, reason string, at time.Time) (storage.BankLine, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out storage.BankLine
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		line, err := u.Banking().Line(ctx, lineID)
		if err != nil {
			return err
		}
		if line.Status == storage.LineSuspense {
			return &banktransaction.Error{Code: CodeNotCategory, Message: "the line is in suspense, waiting to be classified",
				Fix: "Categorise it into the account it belongs to: that replaces its posting to suspense"}
		}
		if err := s.reverse(ctx, u, line, reason, at); err != nil {
			return err
		}
		out, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineUnreconciled, at)
		return err
	})
	return out, err
}

// reverse reverses the line's live categorisation or posting to suspense: its entry and the releases it posted, on
// their own dates; the bank transaction is marked reversed and the match undone. The line's status is the caller's.
func (s BankTransactions) reverse(ctx context.Context, u storage.UnitOfWork, line storage.BankLine, reason string, at time.Time) error {
	matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &line.ID})
	if err != nil {
		return err
	}
	var m *storage.BankMatch
	for i := range matches {
		if matches[i].UndoneAt == nil {
			m = &matches[i]
		}
	}
	if m == nil || (m.Kind != storage.MatchCategorised && m.Kind != storage.MatchSuspense) || m.DocumentID == nil {
		return &banktransaction.Error{Code: CodeNotCategory, Message: fmt.Sprintf("the line is %s, not categorised", line.Status),
			Fix: "Undo it where it was matched: its receipt, bill payment or transfer"}
	}
	doc, err := u.Documents().Lock(ctx, *m.DocumentID) // ENG-47 step 1: a release of it waits for its undo
	if err != nil {
		return err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return err
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.BankTransactionIdentity(doc.ID, doc.Version, ledger.RuleBankTransaction))
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
	if err := reverseReleases(ctx, u, doc, settings, func(e ledger.Entry) time.Time { return e.PostingDate }, reason); err != nil {
		return err
	}
	e, err := u.Ledger().Append(ctx, toStorage(reversal, &stored.ID))
	if err != nil {
		return err
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentReversed); err != nil {
		return err
	}
	doc.Status = storage.DocumentReversed
	if _, err := u.BankMatching().UndoMatch(ctx, m.ID, reason, at); err != nil {
		return err
	}
	return s.after(ctx, u, doc, e)
}

// releaseNow posts a just-categorised line's releases due by the organisation's today, after its unit of work commits.
// What it cannot post the hourly job bank.releases posts, so it never fails the categorisation.
func (s BankTransactions) releaseNow(ctx context.Context, org, id uuid.UUID) {
	if s.Clock == nil {
		return
	}
	day, err := todayIn(ctx, s.Store, org, s.Clock.Now())
	if err != nil {
		return
	}
	_, _ = Releases{Store: s.Store}.ReleaseBankTransaction(ctx, org, id, day)
}

func (s BankTransactions) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// bankSchedule is the release schedule of a posted bank transaction version (ledger.BankTransactionReleases): its
// spread parts as saved, and what the entry it posted put on 1210 or 2400 for each.
func bankSchedule(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) ([]ledger.Release, error) {
	saved, err := banktransaction.Read(doc, s.Base)
	if err != nil {
		return nil, err
	}
	t := banktransaction.Transaction(doc, saved)
	spread := false
	for _, p := range t.Parts {
		spread = spread || p.Months != 0
	}
	if !spread {
		return nil, nil
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.BankTransactionIdentity(doc.ID, doc.Version, ledger.RuleBankTransaction))
	if err != nil {
		return nil, err
	}
	e, err := fromStorage(stored, s.Base)
	if err != nil {
		return nil, err
	}
	return ledger.BankTransactionReleases(t, e, s)
}

// replayBankTransaction posts a stored bank transaction again under the rule that posted e (ENG-06), from what its
// version kept: the line's amount and date, the rate and each part's GST rate as they were decided.
func replayBankTransaction(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	saved, err := banktransaction.Read(doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayBankTransaction(e.RuleVersion, banktransaction.Transaction(doc, saved), s)
}
