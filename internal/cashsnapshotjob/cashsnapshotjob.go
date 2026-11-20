// Package cashsnapshotjob keeps each week's short-term cash projection (PRD-001 FR-PLN-04; p2-reporting-planning design D12). Daily, for each organisation, it
// takes the 13-week projection (FR-PLN-09) as it stands and keeps it once for the week, under the Monday of the organisation's week. A later night finds the
// week's snapshot and does nothing; four and thirteen weeks on, the forecast accuracy view compares it with the cash that came.
package cashsnapshotjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/cashflow"
	"github.com/cognizhi/fyniq/internal/domain/forecastaccuracy"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Job is the platform job's name (outbox.PlatformSchedules).
const Job = "cash.snapshots"

// Days is how far the kept projection runs: 13 weeks.
const Days = 91

// Actor is the principal the job records its snapshots as.
var Actor = storage.Actor{Type: "system", ID: Job}

// Runner keeps what is due.
type Runner struct {
	Store storage.Store
	Clock ports.Clock
}

// RunAll looks at every organisation and returns how many snapshots it kept.
func (r Runner) RunAll(ctx context.Context) (int, error) {
	orgs, err := r.Store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor})
	total := 0
	for _, org := range orgs {
		n, err := r.RunOrganisation(ctx, org)
		total += n
		if err != nil {
			return total, fmt.Errorf("cashsnapshotjob: %s: %w", org, err)
		}
	}
	return total, nil
}

// Monday is the first day of the week of day.
func Monday(day time.Time) time.Time {
	back := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -back)
}

// RunOrganisation keeps the week's snapshot of one organisation, unless it holds it or keeps no books yet.
func (r Runner) RunOrganisation(ctx context.Context, org uuid.UUID) (int, error) {
	kept := 0
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Profile.TimeZone)
		if err != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		y, m, d := r.Clock.Now().In(loc).Date()
		today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		week := Monday(today)
		have, err := u.CashSnapshots().List(ctx)
		if err != nil {
			return err
		}
		for _, s := range have {
			if s.WeekOf.Equal(week) {
				return nil
			}
		}
		rev, err := u.Ledger().Revision(ctx)
		if err != nil {
			return err
		}
		if rev == 0 { // nothing posted yet: there is no cash to project
			return nil
		}
		in, err := cashflow.Read(ctx, u, today, Days)
		if err != nil {
			return err
		}
		p, err := in.Project(Days)
		if err != nil {
			return err
		}
		doc := forecastaccuracy.Projection{Today: today.Format(time.DateOnly), Opening: p.Opening.String(), Weekly: []forecastaccuracy.Week{}, Items: []forecastaccuracy.Item{}}
		for _, w := range p.Weekly {
			doc.Weekly = append(doc.Weekly, forecastaccuracy.Week{From: w.From.Format(time.DateOnly), To: w.To.Format(time.DateOnly), In: w.In.String(), Out: w.Out.String(), Close: w.Close.String()})
		}
		for _, it := range p.Items {
			doc.Items = append(doc.Items, forecastaccuracy.Item{Key: it.Key(), Kind: it.Kind, ID: it.ID.String(), ExpectedOn: it.On.Format(time.DateOnly), Amount: it.Amount.String()})
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		if _, err := u.CashSnapshots().Take(ctx, storage.CashSnapshot{WeekOf: week, TakenOn: today, Currency: p.Opening.Currency().Code, LedgerRevision: rev, Projection: raw, TakenAt: r.Clock.Now()}); err != nil {
			if storage.CodeOf(err) == storage.CodeCashSnapshotWeek || errors.Is(err, ports.ErrNotFound) {
				return nil // another run kept it meanwhile
			}
			return err
		}
		kept++
		return nil
	})
	return kept, err
}
