package journal

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DirectorSettlements settle what the company owes a director or shareholder (FR-EQ-05, FR-EQ-07; p1-founders-money
// design D10): a draft with its loan note, minute or deed attached is sent for another person's approval
// (director_settlement.approve), whose execution keeps the loan on its terms with no entry, or posts the waiver under
// director_settlement@1, in the approval's unit of work (ENG-02).
type DirectorSettlements struct {
	// AfterPosting runs inside the unit of work after a waiver's entry is written and the document posted.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

func checkSettlement(ctx context.Context, u storage.UnitOfWork, s director.Settlement, today time.Time) ([]byte, ledger.Settings, error) {
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return nil, settings, err
	}
	probs, err := director.CheckSettlement(ctx, u, s, settings, today)
	if err != nil {
		return nil, settings, err
	}
	if len(probs) > 0 {
		return nil, settings, probs
	}
	b, err := director.EncodeSettlement(s)
	return b, settings, err
}

// CreateIn saves a settlement as a draft in u, the caller's unit of work, dated today. One that does not fit writes
// nothing and answers every problem.
func (DirectorSettlements) CreateIn(ctx context.Context, u storage.UnitOfWork, s director.Settlement, today time.Time) (storage.Document, error) {
	s.Date, s.DeductedEarlier = today, false
	b, _, err := checkSettlement(ctx, u, s, today)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorSettlement, Payload: b})
}

// DraftSettlement reads a settlement that is still a draft, its row locked until commit (ENG-47): CodeSettled when it is
// decided.
func DraftSettlement(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, director.Settlement, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, director.Settlement{}, err
	}
	s, err := director.ReadSettlement(doc)
	if err != nil {
		return doc, s, err
	}
	if doc.Status != storage.DocumentDraft {
		return doc, s, &director.Error{Code: director.CodeSettled, Message: "the settlement is decided, so it changes no more",
			Fix: "Draft a new settlement for what is still owed"}
	}
	return doc, s, nil
}

// ReviseIn saves a new version of a draft settlement in u, dated today; the caller locked it with DraftSettlement.
func (DirectorSettlements) ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s director.Settlement, today time.Time) (storage.Document, error) {
	s.Date, s.DeductedEarlier = today, false
	b, _, err := checkSettlement(ctx, u, s, today)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// CheckedSettlement is a draft settlement as approving it would decide it: a waiver's entry as a dry run (FR-APR-02), none for a
// loan kept.
type CheckedSettlement struct {
	Settlement director.Settlement
	Entry      *ledger.Entry
}

// Impact runs a draft settlement through its checks, without its agreement, and a waiver through the rule as it would
// post today: the ledger impact a draft shows (FR-APR-02); none for a loan kept.
func (DirectorSettlements) Impact(ctx context.Context, u storage.UnitOfWork, doc storage.Document, today time.Time) (CheckedSettlement, error) {
	s, err := director.ReadSettlement(doc)
	if err != nil {
		return CheckedSettlement{}, err
	}
	_, settings, err := checkSettlement(ctx, u, s, today)
	if err != nil {
		return CheckedSettlement{}, err
	}
	out := CheckedSettlement{Settlement: s}
	if s.Route == director.RouteWaive {
		e, err := ledger.PostDirectorWaiver(s.Rule(doc, today), settings)
		if err != nil {
			return CheckedSettlement{}, err
		}
		out.Entry = &e
	}
	return out, nil
}

// Check runs a draft settlement through what asking for its approval needs: Impact, and its agreement attached (a loan
// note or minute for a loan kept, a deed or minute for a waiver: FOUNDER-30).
func (s DirectorSettlements) Check(ctx context.Context, u storage.UnitOfWork, doc storage.Document, today time.Time) (CheckedSettlement, error) {
	c, err := s.Impact(ctx, u, doc, today)
	if err != nil {
		return CheckedSettlement{}, err
	}
	n, err := director.Attached(ctx, u, doc.ID)
	if err != nil {
		return CheckedSettlement{}, err
	}
	if n == 0 {
		return CheckedSettlement{}, &director.Error{Code: director.CodeNoAgreement, Message: "the settlement has no agreement attached",
			Fix: "Attach the loan note or board minute for a loan kept, or the deed or minute of the waiver"}
	}
	return c, nil
}

// ApproveIn decides an approved draft settlement in u, the approval's unit of work, on today: numbered DS- (gap-free); a
// loan kept is posted with no entry, its terms kept; a waiver is tagged deducted earlier when it is of 2120 (pay or fees
// the company deducted), saved as its next version, and posted under director_settlement@1.
func (s DirectorSettlements) ApproveIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, today time.Time) (storage.Document, *storage.Entry, error) {
	doc, st, err := DraftSettlement(ctx, u, id)
	if err != nil {
		return doc, nil, err
	}
	if _, err := s.Check(ctx, u, doc, today); err != nil {
		return doc, nil, err
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentDirectorSettlement); err != nil {
		return doc, nil, err
	}
	var stored *storage.Entry
	if st.Route == director.RouteWaive {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return doc, nil, err
		}
		st.DeductedEarlier = settings.Accounts[st.AccountID].Role == ledger.RoleDeferredRemuneration
		b, err := director.EncodeSettlement(st)
		if err != nil {
			return doc, nil, err
		}
		number := doc.Number
		if doc, err = u.Documents().Revise(ctx, id, b); err != nil {
			return doc, nil, err
		}
		doc.Number = number
		entry, err := ledger.PostDirectorWaiver(st.Rule(doc, today), settings)
		if err != nil {
			return doc, nil, err
		}
		e, err := u.Ledger().Append(ctx, toStorage(entry, nil))
		if err != nil {
			return doc, nil, err
		}
		stored = &e
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, nil, err
	}
	doc.Status = storage.DocumentPosted
	if stored == nil || s.AfterPosting == nil {
		return doc, stored, nil
	}
	return doc, stored, s.AfterPosting(ctx, u, doc, *stored)
}

// replayDirectorWaiver posts a waiver's version again as its entry posted it (ENG-06), on the day it posted.
func replayDirectorWaiver(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	st, err := director.ReadSettlement(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayDirectorWaiver(e.RuleVersion, st.Rule(doc, e.PostingDate), s)
}
