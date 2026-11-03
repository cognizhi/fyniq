package journal

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DirectorRepayments records what a payment batch's release pays a director or shareholder of the settlements approved
// to be repaid (FR-EQ-05, FR-BNK-12; p1-founders-money design D10): in the release's unit of work, the document, its entry
// from director_repayment@1 and an allocation for each settlement it repays (ENG-02). Only a release records one (FY254).
// A repayment the bank rejected is voided, on another person's approval: a reversing entry with reversing allocations,
// which leaves the settlements open again (FR-GL-12).
type DirectorRepayments struct {
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// RecordedRepayment is a recorded repayment, or its void: its document, the entry it posted, and its allocations.
type RecordedRepayment struct {
	Repayment   storage.Document
	Entry       storage.Entry
	Allocations []storage.Allocation
}

// lockSettlements locks the settlements a repayment pays, in the order of their IDs, so two repayments of one settlement
// wait for each other (ENG-47).
func lockSettlements(ctx context.Context, u storage.UnitOfWork, r director.Repayment) error {
	ids := make([]uuid.UUID, 0, len(r.Settlements))
	for _, x := range r.Settlements {
		ids = append(ids, x.SettlementID)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range slices.Compact(ids) {
		if _, err := u.Documents().Lock(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// RecordIn records a repayment in u, the release's unit of work: each settlement locked and checked to have at least
// what it repays open (CodeNotRepaid), the document numbered DR- (gap-free), its entry, an allocation for each settlement,
// and the document posted. The release links it in the same transaction, or the database refuses it at commit (FY254).
func (s DirectorRepayments) RecordIn(ctx context.Context, u storage.UnitOfWork, r director.Repayment) (RecordedRepayment, error) {
	b, err := director.EncodeRepayment(r)
	if err != nil {
		return RecordedRepayment{}, err
	}
	if err := lockSettlements(ctx, u, r); err != nil {
		return RecordedRepayment{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedRepayment{}, err
	}
	for _, x := range r.Settlements {
		p, err := director.PayableOf(ctx, u, settings.Base, x.SettlementID)
		if err != nil {
			return RecordedRepayment{}, err
		}
		if c, _ := x.Amount.Cmp(p.Open); c > 0 {
			return RecordedRepayment{}, &director.Error{Code: director.CodeNotRepaid, Message: fmt.Sprintf("%s has %s open, less than the %s repaid", p.Doc.Number, p.Open, x.Amount),
				Fix: "Prepare the batch again: it pays what is open now"}
		}
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorRepayment, Payload: b})
	if err != nil {
		return RecordedRepayment{}, err
	}
	rule, err := director.PrepareRepayment(ctx, u, doc)
	if err != nil {
		return RecordedRepayment{}, err
	}
	entry, settled, err := ledger.PostDirectorRepayment(rule, settings)
	if err != nil {
		return RecordedRepayment{}, err
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentDirectorRepayment); err != nil {
		return RecordedRepayment{}, err
	}
	out := RecordedRepayment{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
		return RecordedRepayment{}, err
	}
	for _, x := range settled {
		a, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: x.DocumentID, On: r.Date,
			Currency: x.Amount.Currency().Code, Amount: storage.Decimal(x.Amount.String()), BaseCurrency: settings.Base.Code,
			BaseAmount: storage.Decimal(x.Base.String()), RealisedFX: storage.Decimal(x.RealisedFX.String())})
		if err != nil {
			return RecordedRepayment{}, err
		}
		out.Allocations = append(out.Allocations, a)
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return RecordedRepayment{}, err
	}
	doc.Status = storage.DocumentPosted
	out.Repayment = doc
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, doc, out.Entry)
}

// VoidIn voids a posted repayment in u, the approval's unit of work, with a reversing entry dated date and a reason
// (FR-GL-12), and a reversing allocation for each of its allocations, which leaves the settlements open again. The date
// is not before the repayment's, and its period must be open (INV-03).
func (s DirectorRepayments) VoidIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, date time.Time, reason string) (RecordedRepayment, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return RecordedRepayment{}, err
	}
	r, err := director.ReadRepayment(doc)
	if err != nil {
		return RecordedRepayment{}, err
	}
	if doc.Status != storage.DocumentPosted {
		return RecordedRepayment{}, &director.Error{Code: director.CodeNotPosted, Message: fmt.Sprintf("the repayment is %s, not posted", doc.Status),
			Fix: "Only a posted repayment is voided"}
	}
	y, m, dd := date.Date()
	date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
	if date.Before(r.Date) {
		return RecordedRepayment{}, &director.Error{Code: director.CodeNotPosted, Message: fmt.Sprintf("the void is dated %s, before the repayment's %s",
			date.Format(time.DateOnly), r.Date.Format(time.DateOnly)), Fix: "Date the void on or after the repayment"}
	}
	if err := lockSettlements(ctx, u, r); err != nil {
		return RecordedRepayment{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedRepayment{}, err
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.DirectorRepaymentIdentity(doc.ID, doc.Version, ledger.RuleDirectorRepayment))
	if err != nil {
		return RecordedRepayment{}, err
	}
	original, err := fromStorage(stored, settings.Base)
	if err != nil {
		return RecordedRepayment{}, err
	}
	reversal, err := ledger.Reverse(original, date, reason)
	if err != nil {
		return RecordedRepayment{}, err
	}
	out := RecordedRepayment{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
		return RecordedRepayment{}, err
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return RecordedRepayment{}, err
	}
	for _, a := range allocs {
		if a.ReversesID != nil {
			continue
		}
		reverses := a.ID
		x, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID, On: date,
			Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount),
			RealisedFX: negated(a.RealisedFX), Tolerance: negated(a.Tolerance), ReversesID: &reverses})
		if err != nil {
			return RecordedRepayment{}, err
		}
		out.Allocations = append(out.Allocations, x)
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
		return RecordedRepayment{}, err
	}
	doc.Status = storage.DocumentReversed
	out.Repayment = doc
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, doc, out.Entry)
}

// replayDirectorRepayment posts a repayment's version again as its entry posted it (ENG-06).
func replayDirectorRepayment(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	r, err := director.PrepareRepayment(ctx, u, doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayDirectorRepayment(e.RuleVersion, r, s)
}
