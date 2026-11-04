package journal

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// PreCosts records the costs directors and shareholders paid before the books start, and the accountant's decision on
// each (FR-EQ-03; p1-founders-money design D7). A cost is recorded with its original date and receipt and needs a
// decision (a draft, which posts nothing); the decision posts it under pre_incorporation_cost@1 in the same unit of work
// (ENG-02): Dr the account the accountant chose · Cr 2500, naming the person, on the books start, or on the day of the
// decision when the books start's period is closed.
type PreCosts struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after the entry is written and the document posted.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// RecordedPreCost is a cost with its document, and once decided, the entry it posted.
type RecordedPreCost struct {
	Doc   storage.Document
	Cost  director.PreCost
	Entry *storage.Entry
}

// checkPreCost reads what checking a cost needs and checks it.
func checkPreCost(ctx context.Context, u storage.UnitOfWork, c director.PreCost) error {
	org, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return err
	}
	probs, err := director.CheckPreCost(ctx, u, c, org.Profile.BooksStart, base)
	if err != nil {
		return err
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// RecordIn records a cost that needs the accountant's decision in u, the caller's unit of work, numbered PIC- (gap-free),
// and posts nothing. One that does not fit writes nothing and answers every problem.
func (s PreCosts) RecordIn(ctx context.Context, u storage.UnitOfWork, c director.PreCost) (RecordedPreCost, error) {
	c.Decision = nil
	b, err := director.EncodePreCost(c)
	if err != nil {
		return RecordedPreCost{}, err
	}
	if err := checkPreCost(ctx, u, c); err != nil {
		return RecordedPreCost{}, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentPreIncorporationCost, Payload: b})
	if err != nil {
		return RecordedPreCost{}, err
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentPreIncorporationCost); err != nil {
		return RecordedPreCost{}, err
	}
	return RecordedPreCost{Doc: doc, Cost: c}, nil
}

// Undecided reads a cost that still needs a decision, its row locked until commit (ENG-47): CodePreCostDone when it is
// decided.
func Undecided(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, director.PreCost, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, director.PreCost{}, err
	}
	c, err := director.ReadPreCost(doc)
	if err != nil {
		return doc, c, err
	}
	if doc.Status != storage.DocumentDraft || c.Decision != nil {
		return doc, c, &director.Error{Code: director.CodePreCostDone, Message: "the cost is decided and posted, so it changes no more",
			Fix: "Correct a posted cost with a journal to the person's account"}
	}
	return doc, c, nil
}

// ReviseIn saves a new version of a cost that still needs a decision, in u; the caller locked it with Undecided.
func (s PreCosts) ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, c director.PreCost) (RecordedPreCost, error) {
	c.Decision = nil
	b, err := director.EncodePreCost(c)
	if err != nil {
		return RecordedPreCost{}, err
	}
	if err := checkPreCost(ctx, u, c); err != nil {
		return RecordedPreCost{}, err
	}
	doc, err := u.Documents().Revise(ctx, id, b)
	if err != nil {
		return RecordedPreCost{}, err
	}
	return RecordedPreCost{Doc: doc, Cost: c}, nil
}

// DecideIn records the accountant's decision on a cost in u and posts it in the same unit of work: a new version of the
// cost with the decision, its entry under pre_incorporation_cost@1, and the cost posted. The caller locked it with
// Undecided. The cost must have a receipt attached (design D7: evidence required). today is the day of the decision in
// the organisation's time zone; decidedBy is who decided it ("type:id").
func (s PreCosts) DecideIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, d director.Decision, decidedBy string, today time.Time) (RecordedPreCost, error) {
	if probs := director.CheckDecision(d); len(probs) > 0 {
		return RecordedPreCost{}, probs
	}
	doc, c, err := Undecided(ctx, u, id)
	if err != nil {
		return RecordedPreCost{}, err
	}
	if err := checkPreCost(ctx, u, c); err != nil { // the books start or the person may have changed since
		return RecordedPreCost{}, err
	}
	files, err := u.Evidence().OfDocument(ctx, id, uuid.Nil, 100)
	if err != nil {
		return RecordedPreCost{}, err
	}
	receipt := false
	for _, f := range files {
		receipt = receipt || f.State != storage.EvidenceRemoved
	}
	if !receipt {
		return RecordedPreCost{}, &director.Error{Code: director.CodeNoReceipt, Message: "the cost has no receipt attached",
			Fix: "Attach the receipt or invoice the person paid, then decide it"}
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return RecordedPreCost{}, err
	}
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return RecordedPreCost{}, err
	}
	y, m, dd := today.Date()
	d.DecidedOn = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
	d.PostOn = director.PostOn(periods, *o.Profile.BooksStart, d.DecidedOn)
	d.DecidedBy = decidedBy
	c.Decision = &d
	b, err := director.EncodePreCost(c)
	if err != nil {
		return RecordedPreCost{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedPreCost{}, err
	}
	regs, err := u.GSTRegistrations().List(ctx)
	if err != nil {
		return RecordedPreCost{}, err
	}
	_, registered := gst.StatusOn(regs, d.PostOn)
	if doc, err = u.Documents().Revise(ctx, id, b); err != nil {
		return RecordedPreCost{}, err
	}
	entry, err := ledger.PostPreIncorporationCost(c.Rule(doc, settings.Base, registered), settings)
	if err != nil {
		return RecordedPreCost{}, err
	}
	stored, err := u.Ledger().Append(ctx, toStorage(entry, nil))
	if err != nil {
		return RecordedPreCost{}, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return RecordedPreCost{}, err
	}
	doc.Status = storage.DocumentPosted
	out := RecordedPreCost{Doc: doc, Cost: c, Entry: &stored}
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, doc, stored)
}

// replayPreCost posts a decided cost's version again as its entry posted it (ENG-06), with the GST registration it was
// posted under read from the entry: a GST line means the GST was claimed while registered.
func replayPreCost(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	c, err := director.ReadPreCost(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayPreIncorporationCost(e.RuleVersion, c.Rule(doc, s.Base, c.Decision != nil && c.Decision.ClaimGST), s)
}
