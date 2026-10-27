package journal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Depreciator is the scheduler's principal for depreciation (FR-AUT-02; p1-fixed-assets design D3):
// "scheduler:assets.depreciation", recorded on every month it posts, whether the hourly job posts it or registering or
// changing the asset does.
var Depreciator = storage.Actor{Type: "scheduler", ID: "assets.depreciation"}

// ActionDepreciationWaiting is the audit event of a month of depreciation a run could not post: no open period covers
// its last day, or the rule refuses it (the chart lost 6610 or 1590). The months after it wait with it, and the next run
// tries again.
const ActionDepreciationWaiting = "fixed_asset.depreciation_waiting"

// DepreciationReason is the reason the months of depreciation carry.
const DepreciationReason = "Monthly depreciation"

// Depreciations posts the monthly depreciation of registered assets (FR-FA-02, ENG-05): each month in its own unit of
// work, under the scheduler's principal, with the asset locked so that a change or a disposal of it and its depreciation
// never interleave (ENG-47). Months post in order: one that waits holds back the months after it, so a version's posted
// months always run from its first month without a gap (design D3).
type Depreciations struct {
	Store storage.Store
}

// DepreciationRun is what a run of depreciation did in one organisation.
type DepreciationRun struct {
	Organisation uuid.UUID
	Day          time.Time // the organisation's day the run depreciated up to
	Posted       int
	Waiting      int
}

// withDepreciator is ctx acting as the scheduler's principal for depreciation.
func withDepreciator(ctx context.Context) context.Context {
	return storage.WithAudit(ctx, storage.AuditContext{Actor: &Depreciator, Reason: DepreciationReason})
}

// assetMemo names an asset in its entries: its number and name.
func assetMemo(doc storage.Document, a asset.Asset) string {
	return strings.TrimSpace(doc.Number + " " + a.Name)
}

// depreciable is an asset version as its schedule reads it.
func depreciable(doc storage.Document, a asset.Asset) ledger.DepreciableAsset {
	return ledger.DepreciableAsset{ID: doc.ID, Version: doc.Version, Memo: assetMemo(doc, a), Cost: a.Cost, Residual: a.Residual, Carried: a.Carried,
		From: a.From, End: a.End()}
}

// depreciationOf is the schedule of an asset version (ledger.DepreciationSchedule), leaving out the months whose part is
// zero; nothing for a version that a disposal of the whole made.
func depreciationOf(doc storage.Document) ([]ledger.Depreciation, error) {
	a, err := asset.Read(doc)
	if err != nil {
		return nil, err
	}
	if a.Disposed {
		return nil, nil
	}
	ds, err := ledger.DepreciationSchedule(depreciable(doc, a))
	if err != nil {
		return nil, err
	}
	out := ds[:0]
	for _, d := range ds {
		if !d.Amount.IsZero() {
			out = append(out, d)
		}
	}
	return out, nil
}

// identities are the posting identities of every entry naming a source, reversals included: a month whose identity is
// among them never posts again.
func identities(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (map[string]bool, error) {
	entries, err := u.Ledger().OfSource(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.PostingIdentity] = true
	}
	return out, nil
}

// errDepreciated is a month that needs nothing any more: posted, or its asset changed or disposed of, meanwhile.
var errDepreciated = errors.New("the month is posted, or its asset changed")

// Depreciate posts the months of one registered asset's current version whose last day is on or before day (ENG-05: up
// to the current period, never the future), under the scheduler's principal: what a run does for the asset, and what
// registering or changing it does at once.
func (s Depreciations) Depreciate(ctx context.Context, org, id uuid.UUID, day time.Time) (DepreciationRun, error) {
	ctx = withDepreciator(ctx)
	run := DepreciationRun{Organisation: org, Day: day}
	var due []ledger.Depreciation
	if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil || !asset.IsAsset(doc) || doc.Status != storage.DocumentPosted {
			return err
		}
		ds, err := depreciationOf(doc)
		if err != nil || len(ds) == 0 {
			return err
		}
		posted, err := identities(ctx, u, id)
		if err != nil {
			return err
		}
		for _, d := range ds {
			if d.Date.After(day) {
				break // the future is not posted (ENG-05)
			}
			if !posted[d.Identity(ledger.RuleAssetDepreciation)] {
				due = append(due, d)
			}
		}
		return nil
	}); err != nil {
		return run, err
	}
	for i, d := range due {
		err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1: a change or a disposal of the asset waits for its month
			if err != nil {
				return err
			}
			if doc.Status != storage.DocumentPosted || doc.Version != d.Version {
				return errDepreciated
			}
			if _, err := u.Ledger().ByIdentity(ctx, d.Identity(ledger.RuleAssetDepreciation)); err == nil {
				return errDepreciated
			} else if !errors.Is(err, ports.ErrNotFound) {
				return err
			}
			settings, err := loadSettings(ctx, u)
			if err != nil {
				return err
			}
			e, err := ledger.DepreciationRules[ledger.RuleAssetDepreciation](d, settings)
			if err != nil {
				return err
			}
			_, err = u.Ledger().Append(ctx, toStorage(e, nil))
			return err
		})
		var probs ledger.Problems
		switch code := storage.CodeOf(err); {
		case err == nil:
			run.Posted++
		case errors.Is(err, errDepreciated):
		case code == storage.CodePeriodClosed || code == storage.CodeNoPeriod || errors.As(err, &probs):
			run.Waiting += len(due) - i // the months after it wait for it
			why := code
			if why == "" {
				why = err.Error()
			}
			return run, s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionDepreciationWaiting, ObjectType: "document", ObjectID: id,
					Changes: map[string][2]any{"depreciation": {nil, d.Identity(ledger.RuleAssetDepreciation)}, "date": {nil, d.Date.Format(time.DateOnly)},
						"waiting_for": {nil, why}}})
			})
		default:
			return run, fmt.Errorf("asset %s, depreciation of %s: %w", id, d.Month.Format("2006-01"), err)
		}
	}
	return run, nil
}

// DepreciateDue posts every month of depreciation due on or before day of the organisation's registered assets
// (FR-FA-02, ENG-05): the hourly job assets.depreciation. A month that cannot post waits, with an audit event that says
// why, and the next run tries it again.
func (s Depreciations) DepreciateDue(ctx context.Context, org uuid.UUID, day time.Time) (DepreciationRun, error) {
	run := DepreciationRun{Organisation: org, Day: day}
	var afterDate string
	var afterID *uuid.UUID
	for {
		var docs []storage.Document
		if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			docs, err = u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentFixedAsset, Status: storage.DocumentPosted,
				AfterDate: afterDate, AfterID: afterID, Limit: MaxDue})
			return err
		}); err != nil {
			return run, err
		}
		for _, doc := range docs {
			r, err := s.Depreciate(ctx, org, doc.ID, day)
			run.Posted += r.Posted
			run.Waiting += r.Waiting
			if err != nil {
				return run, err
			}
		}
		if len(docs) < MaxDue {
			return run, nil
		}
		afterDate, afterID = docs[len(docs)-1].Date, &docs[len(docs)-1].ID
	}
}

// DepreciateDueAll runs depreciation in every organisation up to its own today, in its time zone, at now. An
// organisation that fails does not stop the others; their errors come back together.
func DepreciateDueAll(ctx context.Context, store storage.Store, now time.Time) ([]DepreciationRun, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	s := Depreciations{Store: store}
	var runs []DepreciationRun
	var errs []error
	for _, org := range orgs {
		day, err := todayIn(ctx, store, org, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		run, err := s.DepreciateDue(ctx, org, day)
		runs = append(runs, run)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return runs, errors.Join(errs...)
}

// replayDepreciation posts a stored month of depreciation again under the rule that posted e (ENG-06): the month of the
// asset version's schedule that has e's identity.
func replayDepreciation(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	ds, err := depreciationOf(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, d := range ds {
		if d.Identity(e.RuleVersion) == e.PostingIdentity {
			return ledger.ReplayDepreciation(e.RuleVersion, d, s)
		}
	}
	return ledger.Entry{}, fmt.Errorf("the schedule of %s version %d has no month %s", doc.Number, doc.Version, e.PostingIdentity)
}

// The states of a month of an asset's depreciation, as its schedule shows it (ENG-05: the future is a projection,
// never mixed with actuals).
const (
	DepreciationPosted    = "posted"    // in the ledger
	DepreciationReversed  = "reversed"  // posted, and reversed by a disposal dated before it
	DepreciationWaiting   = "waiting"   // due by the organisation's today, not posted yet: the hourly job posts it, or its period is not open
	DepreciationProjected = "projected" // in the future: not in the ledger
)

// DepreciationState is one month of an asset's depreciation and its state, with its entry once posted.
type DepreciationState struct {
	Month   time.Time
	Date    time.Time
	Version int
	Amount  money.Money
	Status  string
	EntryID uuid.UUID
}

// DepreciationStates are an asset's months of depreciation on today: every month posted under any version, reversed or
// not, and its current version's months not posted, waiting when due by today and projected after, in month order.
func DepreciationStates(ctx context.Context, u storage.UnitOfWork, doc storage.Document, today time.Time, base money.Currency) ([]DepreciationState, error) {
	entries, err := u.Ledger().OfSource(ctx, doc.ID)
	if err != nil {
		return nil, err
	}
	reversed := map[uuid.UUID]bool{}
	posted := map[string]bool{}
	for _, e := range entries {
		posted[e.PostingIdentity] = true
		if e.ReversesEntryID != nil {
			reversed[*e.ReversesEntryID] = true
		}
	}
	var out []DepreciationState
	for _, e := range entries {
		if e.Source.Type != ledger.SourceAssetDepreciation || e.ReversesEntryID != nil {
			continue
		}
		x, err := fromStorage(e, base)
		if err != nil {
			return nil, err
		}
		st := DepreciationState{Month: asset.Month(x.PostingDate), Date: x.PostingDate, Version: e.Source.Version, Amount: money.Zero(base),
			Status: DepreciationPosted, EntryID: e.ID}
		for _, l := range x.Lines {
			if l.Base.Sign() > 0 {
				st.Amount = l.Base
			}
		}
		if reversed[e.ID] {
			st.Status = DepreciationReversed
		}
		out = append(out, st)
	}
	ds, err := depreciationOf(doc)
	if err != nil {
		return nil, err
	}
	for _, d := range ds {
		if posted[d.Identity(ledger.RuleAssetDepreciation)] {
			continue
		}
		st := DepreciationState{Month: d.Month, Date: d.Date, Version: d.Version, Amount: d.Amount, Status: DepreciationProjected}
		if !d.Date.After(today) {
			st.Status = DepreciationWaiting
		}
		out = append(out, st)
	}
	// By month; a reversed month, posted earlier, stays before the one that posted it again.
	slices.SortStableFunc(out, func(a, b DepreciationState) int { return a.Month.Compare(b.Month) })
	return out, nil
}
