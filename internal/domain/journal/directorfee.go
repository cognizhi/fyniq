package journal

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DirectorFees records the directors' fees the shareholders voted, and posts each once another person approves it
// (FR-EQ-04, §10.2; p1-founders-money design D8): a draft takes the shareholders' resolution as its evidence; the
// approval of director_fee.post posts it under director_fee@1, Dr 6050 · Cr 2120 naming the director, on the day they
// became entitled to it, in the approval's unit of work (ENG-02).
type DirectorFees struct {
	// AfterPosting runs inside the unit of work after the entry is written and the document posted.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

func checkFee(ctx context.Context, u storage.UnitOfWork, f director.Fee) ([]byte, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return nil, err
	}
	probs, err := director.CheckFee(ctx, u, f, base)
	if err != nil {
		return nil, err
	}
	if len(probs) > 0 {
		return nil, probs
	}
	return director.EncodeFee(f)
}

// CreateIn saves a fee as a draft in u, the caller's unit of work. One that does not fit writes nothing and answers
// every problem.
func (s DirectorFees) CreateIn(ctx context.Context, u storage.UnitOfWork, f director.Fee) (storage.Document, error) {
	b, err := checkFee(ctx, u, f)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorFee, Payload: b})
}

// DraftFee reads a fee that is still a draft, its row locked until commit (ENG-47): CodeFeePosted when it is posted.
func DraftFee(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, director.Fee, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, director.Fee{}, err
	}
	f, err := director.ReadFee(doc)
	if err != nil {
		return doc, f, err
	}
	if doc.Status != storage.DocumentDraft {
		return doc, f, &director.Error{Code: director.CodeFeePosted, Message: "the fee is posted, so it changes no more",
			Fix: "Correct a posted fee with a journal to the director's account on 2120"}
	}
	return doc, f, nil
}

// ReviseIn saves a new version of a draft fee in u; the caller locked it with DraftFee.
func (s DirectorFees) ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, f director.Fee) (storage.Document, error) {
	b, err := checkFee(ctx, u, f)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// Check runs a draft fee through its checks and the rule as it would post, writing nothing: what asking for its approval
// needs (the shareholders' resolution attached: FOUNDER-17), and the entry the approver sees.
func (s DirectorFees) Check(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (ledger.Entry, error) {
	f, err := director.ReadFee(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	if _, err := checkFee(ctx, u, f); err != nil {
		return ledger.Entry{}, err
	}
	n, err := director.Attached(ctx, u, doc.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	if n == 0 {
		return ledger.Entry{}, &director.Error{Code: director.CodeNoVote, Message: "the fee has no shareholders' resolution attached",
			Fix: "Attach the resolution or the minutes of the general meeting that voted it"}
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.PostDirectorFee(f.Rule(doc), settings)
}

// PostIn posts an approved draft fee in u, the approval's unit of work: its number (DF-, gap-free), its entry under
// director_fee@1, and the fee posted.
func (s DirectorFees) PostIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, storage.Entry, error) {
	doc, _, err := DraftFee(ctx, u, id)
	if err != nil {
		return doc, storage.Entry{}, err
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentDirectorFee); err != nil {
		return doc, storage.Entry{}, err
	}
	entry, err := s.Check(ctx, u, doc)
	if err != nil {
		return doc, storage.Entry{}, err
	}
	stored, err := u.Ledger().Append(ctx, toStorage(entry, nil))
	if err != nil {
		return doc, storage.Entry{}, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, storage.Entry{}, err
	}
	doc.Status = storage.DocumentPosted
	if s.AfterPosting == nil {
		return doc, stored, nil
	}
	return doc, stored, s.AfterPosting(ctx, u, doc, stored)
}

// replayDirectorFee posts a fee's version again as its entry posted it (ENG-06).
func replayDirectorFee(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	f, err := director.ReadFee(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayDirectorFee(e.RuleVersion, f.Rule(doc), s)
}
