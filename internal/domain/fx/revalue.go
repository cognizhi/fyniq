package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The month-end revaluation (FR-GL-04; PRD-001 rev 0.73, provisional until the accountant's sign-off). A person who runs
// the close's checks revalues a period once its last day has come: every account marked revalue with a balance in a
// foreign currency at the period end is taken to the rate the cascade finds for that day (ENG-40), as a document of its
// own whose entry is dated the period's last day and reversed on the next period's first. A run records the ledger
// revision it read; a later posting dated in or before the period makes it stale, and a run then posts what changed
// since: each run's change is taken against the carrying value the earlier runs left, so the period ends at the rate.
// This package works the revaluation out and reads its runs; the posting service (journal.PostRevaluation) posts it, as
// it posts everything (ENG-02, the one posting path).

// DocumentFXRevaluation is the revaluation's document type.
const DocumentFXRevaluation storage.DocumentType = "fx_revaluation"

// The revaluation's refusals.
const (
	CodeNotOpen    = "FX-05" // the period is not open
	CodeNotEnded   = "FX-06" // the period's last day has not come
	CodeUnrealised = "FX-07" // no account has the unrealised-fx role
)

// saved is a revaluation's document version: what its run read (FR-GL-04).
type saved struct {
	PeriodID   string      `json:"period_id"`
	Date       string      `json:"date"`       // the period's last day: the entry's date
	ReverseOn  string      `json:"reverse_on"` // the next period's first day: the reversal's
	Revision   int64       `json:"revision"`   // the ledger revision it read
	Unrealised string      `json:"unrealised_account_id"`
	Items      []savedItem `json:"items"`
}

type savedItem struct {
	AccountID  string `json:"account_id"`
	Currency   string `json:"currency"`
	Open       string `json:"open"`
	Carrying   string `json:"carrying"`
	Rate       string `json:"rate"`
	RateSource string `json:"rate_source"`
}

// Item is one account's balance in one foreign currency at the period end, revalued.
type Item struct {
	Account storage.Account
	ledger.RevaluationItem
	Change money.Money // the unrealised gain (positive) or loss
}

// NotRevalued is a foreign-currency balance on an account not marked revalue: it stays at the rates it was recorded at.
type NotRevalued struct {
	Account storage.Account
	Amount  money.Money
	Base    money.Money
}

// Run is a posted revaluation of a period.
type Run struct {
	Document  storage.Document
	Revision  int64          // what it read
	Entry     *storage.Entry // nil when nothing changed
	Reversal  *storage.Entry // nil until it posts
	ReverseOn time.Time
	Current   bool // no posting dated on or before the period end came after it
}

// Missing is a currency the cascade finds no rate for on the period's last day, and FX-01's refusal.
type Missing struct {
	Currency string
	*Error
}

// Plan is a period's revaluation as it would post now.
type Plan struct {
	Period      storage.Period
	ReverseOn   time.Time
	Revision    int64
	Unrealised  *storage.Account
	Items       []Item
	Missing     []Missing // a currency with no rate on the period's last day (FX-01)
	NotRevalued []NotRevalued
	Entry       ledger.Entry // what it would post; no lines when nothing changed or a rate is missing
	Latest      *Run         // the period's latest run
}

// Due reports whether the period has balances to revalue and no current run for them.
func (p Plan) Due() bool { return len(p.Items) > 0 && (p.Latest == nil || !p.Latest.Current) }

// Prepare works out a period's revaluation in u as it would post now, and finds its latest run. It writes nothing.
func Prepare(ctx context.Context, u storage.UnitOfWork, periodID uuid.UUID) (Plan, error) {
	p, err := u.Periods().Get(ctx, periodID)
	if err != nil {
		return Plan{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return Plan{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return Plan{}, err
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return Plan{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Period: p, ReverseOn: p.EndsOn.AddDate(0, 0, 1)}
	byID := map[uuid.UUID]storage.Account{}
	settings := ledger.Settings{Base: base, Accounts: map[uuid.UUID]ledger.Account{}}
	for _, a := range accounts {
		byID[a.ID] = a
		settings.Accounts[a.ID] = ledger.Account{ID: a.ID, Code: a.Code, Group: a.Group, Active: a.Active, Revalue: a.Revalue,
			UnrealisedFX: a.Role == storage.RoleUnrealisedFX}
		if a.Role == storage.RoleUnrealisedFX {
			x := a
			plan.Unrealised = &x
		}
	}
	rows, err := balance.Foreign(ctx, u, base, all, p)
	if err != nil {
		return Plan{}, err
	}
	rates := map[string]*money.Rate{}
	missing := map[string]bool{}
	for _, r := range rows {
		a := byID[r.AccountID]
		if !a.Revalue {
			plan.NotRevalued = append(plan.NotRevalued, NotRevalued{Account: a, Amount: r.Amount, Base: r.Base})
			continue
		}
		cur := r.Amount.Currency()
		rate, ok := rates[cur.Code]
		if !ok && !missing[cur.Code] {
			f, err := Lookup(ctx, u, base, cur, p.EndsOn)
			var fe *Error
			switch {
			case errors.As(err, &fe):
				missing[cur.Code] = true
				plan.Missing = append(plan.Missing, Missing{Currency: cur.Code, Error: fe})
			case err != nil:
				return Plan{}, err
			default:
				rate = &f.Rate
				rates[cur.Code] = rate
			}
		}
		it := Item{Account: a, RevaluationItem: ledger.RevaluationItem{AccountID: a.ID, Open: r.Amount, Carrying: r.Base}}
		if rate != nil {
			it.Rate = *rate
			if it.Change, err = it.RevaluationItem.Change(); err != nil {
				return Plan{}, err
			}
		}
		plan.Items = append(plan.Items, it)
	}
	if plan.Revision, err = u.Ledger().Revision(ctx); err != nil {
		return Plan{}, err
	}
	if len(plan.Missing) == 0 && plan.Unrealised != nil && len(plan.Items) > 0 {
		items := make([]ledger.RevaluationItem, len(plan.Items))
		for i, it := range plan.Items {
			items[i] = it.RevaluationItem
		}
		if plan.Entry, err = ledger.Revalue(ledger.Revaluation{ID: uuid.Nil, Version: 1, Date: p.EndsOn, Items: items, Unrealised: plan.Unrealised.ID}, settings); err != nil {
			return Plan{}, err
		}
	}
	if plan.Latest, err = latest(ctx, u, p); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// latest is a period's latest run, with whether it is current, or nil.
func latest(ctx context.Context, u storage.UnitOfWork, p storage.Period) (*Run, error) {
	day := p.EndsOn.Format(time.DateOnly)
	docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: DocumentFXRevaluation, From: day, To: day, Limit: 100})
	if err != nil {
		return nil, err
	}
	for _, d := range docs { // newest first
		var s saved
		if err := json.Unmarshal(d.Payload, &s); err != nil {
			return nil, fmt.Errorf("revaluation %s: %w", d.ID, err)
		}
		if s.PeriodID != p.ID.String() {
			continue
		}
		r, err := RunOf(ctx, u, d)
		if err != nil {
			return nil, err
		}
		seen := r.Revision
		if r.Entry != nil {
			seen = r.Entry.LedgerRevision
		}
		changed, err := u.Closes().ChangedSince(ctx, p.EndsOn, seen)
		if err != nil {
			return nil, err
		}
		r.Current = !changed
		return &r, nil
	}
	return nil, nil
}

// RunOf is a revaluation document with its entry and reversal, if they posted.
func RunOf(ctx context.Context, u storage.UnitOfWork, d storage.Document) (Run, error) {
	var s saved
	if err := json.Unmarshal(d.Payload, &s); err != nil {
		return Run{}, fmt.Errorf("revaluation %s: %w", d.ID, err)
	}
	r := Run{Document: d, Revision: s.Revision}
	r.ReverseOn, _ = time.Parse(time.DateOnly, s.ReverseOn)
	identity := ledger.RevaluationIdentity(d.ID, d.Version, ledger.RuleFXRevaluation)
	e, err := u.Ledger().ByIdentity(ctx, identity)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return r, nil
	case err != nil:
		return Run{}, err
	}
	r.Entry = &e
	back, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(identity))
	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return Run{}, err
	default:
		r.Reversal = &back
	}
	return r, nil
}

// Refusal says why the plan cannot post today, or nil: the period is not open (FX-05), its last day has not come
// (FX-06), a currency has no rate (FX-01), or no account takes unrealised FX (FX-07).
func (p Plan) Refusal(today time.Time) error {
	switch {
	case p.Period.Status != storage.PeriodOpen:
		return &Error{Code: CodeNotOpen, Message: fmt.Sprintf("the period %s to %s is %s", day(p.Period.StartsOn), day(p.Period.EndsOn), p.Period.Status),
			Fix: "Revalue a period while it is open; ask for it to be reopened"}
	case today.Before(p.Period.EndsOn):
		return &Error{Code: CodeNotEnded, Message: fmt.Sprintf("the period ends on %s, after today", p.Period.EndsOn.Format("2 Jan 2006")),
			Fix: "Revalue it from its last day; preview it before"}
	case len(p.Missing) > 0:
		words := make([]string, len(p.Missing))
		for i, m := range p.Missing {
			words[i] = m.Message
		}
		return &Error{Code: CodeNoRate, Field: "rates", Message: strings.Join(words, "; "), Fix: p.Missing[0].Fix}
	case len(p.Items) > 0 && p.Unrealised == nil:
		return &Error{Code: CodeUnrealised, Message: "no account has the unrealised-fx role",
			Fix: "Load the standard chart's 7040 Unrealised FX gain or loss, or give an income account the role"}
	}
	return nil
}

// Record is the plan as a revaluation document's payload: what the run read (FR-GL-04).
func (p Plan) Record() ([]byte, error) {
	s := saved{PeriodID: p.Period.ID.String(), Date: day(p.Period.EndsOn), ReverseOn: day(p.ReverseOn), Revision: p.Revision}
	if p.Unrealised != nil {
		s.Unrealised = p.Unrealised.ID.String()
	}
	for _, it := range p.Items {
		s.Items = append(s.Items, savedItem{AccountID: it.AccountID.String(), Currency: it.Open.Currency().Code, Open: it.Open.String(),
			Carrying: it.Carrying.String(), Rate: it.Rate.String(), RateSource: it.Rate.Source})
	}
	return json.Marshal(s)
}

// ReadRevaluation is a revaluation document's version as the engine's input, with the settings its accounts give: the
// replay re-runs the rule over it (ENG-06).
func ReadRevaluation(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (ledger.Revaluation, ledger.Settings, error) {
	var s saved
	if err := json.Unmarshal(doc.Payload, &s); err != nil {
		return ledger.Revaluation{}, ledger.Settings{}, fmt.Errorf("revaluation %s: %w", doc.ID, err)
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return ledger.Revaluation{}, ledger.Settings{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return ledger.Revaluation{}, ledger.Settings{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return ledger.Revaluation{}, ledger.Settings{}, err
	}
	settings := ledger.Settings{Base: base, Accounts: map[uuid.UUID]ledger.Account{}}
	for _, a := range accounts {
		settings.Accounts[a.ID] = ledger.Account{ID: a.ID, Code: a.Code, Group: a.Group, Active: true, Revalue: a.Revalue,
			UnrealisedFX: a.Role == storage.RoleUnrealisedFX}
	}
	r := ledger.Revaluation{ID: doc.ID, Version: doc.Version}
	if r.Date, err = time.Parse(time.DateOnly, s.Date); err != nil {
		return ledger.Revaluation{}, ledger.Settings{}, fmt.Errorf("revaluation %s: %w", doc.ID, err)
	}
	if s.Unrealised != "" {
		if r.Unrealised, err = uuid.Parse(s.Unrealised); err != nil {
			return ledger.Revaluation{}, ledger.Settings{}, err
		}
	}
	for _, x := range s.Items {
		cur, err := money.Lookup(x.Currency)
		if err != nil {
			return ledger.Revaluation{}, ledger.Settings{}, err
		}
		id, err := uuid.Parse(x.AccountID)
		if err != nil {
			return ledger.Revaluation{}, ledger.Settings{}, err
		}
		open, err := money.ParseStored(x.Open, cur)
		if err != nil {
			return ledger.Revaluation{}, ledger.Settings{}, err
		}
		carrying, err := money.ParseStored(x.Carrying, base)
		if err != nil {
			return ledger.Revaluation{}, ledger.Settings{}, err
		}
		rate, err := money.ParseRate(x.Rate, cur, base, x.RateSource)
		if err != nil {
			return ledger.Revaluation{}, ledger.Settings{}, err
		}
		r.Items = append(r.Items, ledger.RevaluationItem{AccountID: id, Open: open, Carrying: carrying, Rate: rate})
	}
	return r, settings, nil
}

func day(t time.Time) string { return t.Format(time.DateOnly) }
