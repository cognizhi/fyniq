package perf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Months is how far back the grown ledger reaches: two years, so a twelve-month report has its comparative year.
const Months = 24

// Grower adds synthetic journals to an organisation through the posting engine, as the API would post them: each a
// payment of many expenses from one money account, so every line is valid and the trial balance stays balanced.
type Grower struct {
	Store storage.Store
	Org   uuid.UUID
	Clock ports.Clock
	// Lines is how many ledger lines to add; PerJournal how many lines each journal has (50: NFR-PERF-03's document).
	Lines, PerJournal int
	Seed              uint64
	Log               io.Writer // progress; nil says nothing
}

// GrowReport is what a growth added.
type GrowReport struct {
	Journals       int           `json:"journals"`
	Lines          int           `json:"lines"`
	Periods        int           `json:"periods_created"`
	Took           time.Duration `json:"took"`
	LinesPerSecond float64       `json:"lines_per_second"`
	Sample         []uuid.UUID   `json:"-"` // some of the journals posted, for the reads to fetch
	Expenses       []uuid.UUID   `json:"-"` // the accounts the journals debit
	Money          uuid.UUID     `json:"-"` // the money account they credit
	Currency       string        `json:"currency"`
}

// Plan is what the grower posts into: the money account it credits, the expense accounts it debits, the currency and the
// months.
type Plan struct {
	Money    uuid.UUID
	Expenses []uuid.UUID
	Currency money.Currency
	Months   []time.Time // first days, oldest first
}

// Prepare finds the accounts to post to and makes sure each of the last Months months before this one has an open
// period, creating those missing.
func (g Grower) Prepare(ctx context.Context) (Plan, int, error) {
	var p Plan
	created := 0
	now := g.Clock.Now().UTC()
	this := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := Months; i >= 1; i-- {
		p.Months = append(p.Months, this.AddDate(0, -i, 0))
	}
	err := g.Store.Do(ctx, g.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if p.Currency, err = money.Lookup(o.BaseCurrency); err != nil {
			return err
		}
		accounts, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			usable := a.Active && !a.Group && a.MergedInto == nil && !a.Control && len(a.RequiredDimensions) == 0 && a.Role == ""
			switch {
			case usable && a.MoneyAccount && (a.Currency == "" || a.Currency == o.BaseCurrency) && p.Money == uuid.Nil:
				p.Money = a.ID
			case usable && a.Type == storage.Expense && !a.MoneyAccount && !a.PayrollCost:
				p.Expenses = append(p.Expenses, a.ID)
			}
		}
		if p.Money == uuid.Nil || len(p.Expenses) == 0 {
			return errors.New("the organisation has no money account and expense accounts to post to: load the sample company first (make seed-load)")
		}
		periods, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		have := map[time.Time]bool{}
		for _, x := range periods {
			have[x.StartsOn.UTC()] = true
		}
		for _, m := range p.Months {
			if have[m] {
				continue
			}
			if _, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: m, EndsOn: m.AddDate(0, 1, -1)}); err != nil {
				return fmt.Errorf("the period %s: %w", m.Format("2006-01"), err)
			}
			created++
		}
		return nil
	})
	return p, created, err
}

// Draft is the i'th synthetic journal: PerJournal-1 expenses paid from the money account in one payment, on a day of
// one of the months.
func (g Grower) Draft(p Plan, i int, r *rand.Rand) journal.Draft {
	month := p.Months[i%len(p.Months)]
	d := journal.Draft{Date: month.AddDate(0, 0, r.IntN(28)), Memo: fmt.Sprintf("perf payment %d", i)}
	var total int64
	for k := range g.PerJournal - 1 {
		cents := int64(r.IntN(99_900) + 100)
		total += cents
		d.Lines = append(d.Lines, ledger.JournalLine{AccountID: p.Expenses[(i+k)%len(p.Expenses)], Amount: money.FromMinor(cents, p.Currency),
			Description: fmt.Sprintf("perf expense %d", k)})
	}
	d.Lines = append(d.Lines, ledger.JournalLine{AccountID: p.Money, Amount: money.FromMinor(-total, p.Currency), Description: "perf payment"})
	return d
}

// Grow posts journals until it has added Lines ledger lines.
func (g Grower) Grow(ctx context.Context) (GrowReport, error) {
	if g.PerJournal < 2 {
		g.PerJournal = 50
	}
	p, created, err := g.Prepare(ctx)
	rep := GrowReport{Periods: created, Expenses: p.Expenses, Money: p.Money, Currency: p.Currency.Code}
	if err != nil {
		return rep, err
	}
	r := rand.New(rand.NewPCG(g.Seed, g.Seed+1)) // #nosec G404 -- synthetic amounts, not a secret
	svc := journal.Service{Store: g.Store}
	start := g.Clock.Now()
	for i := 0; rep.Lines < g.Lines; i++ {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		d := g.Draft(p, i, r)
		doc, err := svc.Create(ctx, g.Org, d)
		if err != nil {
			return rep, fmt.Errorf("journal %d: %w", i, err)
		}
		if _, err := svc.Post(ctx, g.Org, doc.ID); err != nil {
			return rep, fmt.Errorf("post journal %d: %w", i, err)
		}
		rep.Journals++
		rep.Lines += len(d.Lines)
		if len(rep.Sample) < 1000 && i%7 == 0 {
			rep.Sample = append(rep.Sample, doc.ID)
		}
		if g.Log != nil && rep.Journals%1000 == 0 {
			took := g.Clock.Now().Sub(start)
			fmt.Fprintf(g.Log, "perf: %d of %d lines in %s (%.0f a second)\n", rep.Lines, g.Lines, took.Round(time.Second), float64(rep.Lines)/took.Seconds())
		}
	}
	rep.Took = g.Clock.Now().Sub(start)
	if s := rep.Took.Seconds(); s > 0 {
		rep.LinesPerSecond = float64(rep.Lines) / s
	}
	return rep, nil
}
