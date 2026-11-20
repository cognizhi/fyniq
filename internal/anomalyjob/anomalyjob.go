// Package anomalyjob raises the spend anomaly alerts (PRD-001 FR-KPI-06; p2-reporting-planning design D10). Daily, for each organisation, it takes
// the latest closed month and each expense category's spend in it against the months before it (up to twelve), and where the spend is three
// standard deviations above its trend (domain/anomaly) it records one alert for the category and month and tells the people who approve budgets.
// An alert is raised once; a later night finds it recorded and does nothing.
package anomalyjob

import (
	"context"
	"fmt"
	"sort"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/anomaly"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Job is the platform job's name (outbox.PlatformSchedules).
const Job = "kpi.anomalies"

// Actor is the principal the job records its alerts as.
var Actor = storage.Actor{Type: "system", ID: Job}

// HistoryMonths is the most months of trend an alert is worked out from.
const HistoryMonths = 12

// Runner raises what is due.
type Runner struct {
	Store  storage.Store
	Outbox outbox.Dispatcher
	Clock  ports.Clock
}

// RunAll looks at every organisation and returns how many alerts it raised.
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
			return total, fmt.Errorf("anomalyjob: %s: %w", org, err)
		}
	}
	return total, nil
}

// RunOrganisation raises one organisation's alerts for its latest closed month.
func (r Runner) RunOrganisation(ctx context.Context, org uuid.UUID) (int, error) {
	raised := 0
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		target := -1
		for i, p := range all {
			if p.Status == storage.PeriodClosed {
				target = i
			}
		}
		if target < anomaly.MinHistory {
			return nil // not enough months before the latest closed one
		}
		first := max(0, target-HistoryMonths)
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(o.BaseCurrency)
		if err != nil {
			return err
		}
		revision, err := u.Ledger().Revision(ctx)
		if err != nil {
			return err
		}
		var cols []financials.ColumnSpec
		for i := first; i <= target; i++ {
			cols = append(cols, financials.ColumnSpec{Key: all[i].StartsOn.Format("2006-01"), Periods: all[i : i+1]})
		}
		figs, accounts, err := financials.AccountFigures(ctx, u, base, revision, all, cols, nil)
		if err != nil {
			return err
		}
		byCategory := map[string][]*apd.Decimal{}
		for _, a := range accounts {
			if a.Type != storage.Expense || a.Group {
				continue
			}
			category := a.ReportGroup
			if category == "" {
				category = a.Name
			}
			ms, ok := figs[a.ID]
			if !ok {
				continue
			}
			if byCategory[category] == nil {
				byCategory[category] = make([]*apd.Decimal, len(cols))
				for i := range byCategory[category] {
					byCategory[category][i] = apd.New(0, 0)
				}
			}
			for i, m := range ms {
				if _, err := (apd.BaseContext.WithPrecision(100)).Add(byCategory[category][i], byCategory[category][i], m.Decimal()); err != nil {
					return err
				}
			}
		}
		names := make([]string, 0, len(byCategory))
		for c := range byCategory {
			names = append(names, c)
		}
		sort.Strings(names)
		now := r.Clock.Now()
		month := all[target].StartsOn
		for _, c := range names {
			series := byCategory[c]
			spend, history := series[len(series)-1], series[:len(series)-1]
			if spend.Sign() <= 0 {
				continue
			}
			a, hit, err := anomaly.Detect(history, spend)
			if err != nil {
				return err
			}
			if !hit {
				continue
			}
			rec, isNew, err := u.KPIAlerts().Raise(ctx, storage.KPIAlert{Kind: storage.AlertSpendAnomaly, Category: c, Month: month, Currency: base.Code,
				Spend: storage.Decimal(rounding.KPI(spend, 4)), Mean: storage.Decimal(rounding.KPI(a.Mean, 4)), HistoryMonths: len(history), LedgerRevision: revision, RaisedAt: now})
			if err != nil {
				return err
			}
			if !isNew {
				continue
			}
			raised++
			if _, err := notify.Emit(ctx, r.Outbox, u, org, notify.Event{Type: notify.KPIAnomalyType, RecordType: "kpi_alert", RecordID: &rec.ID, At: now,
				Title: "Spend on " + c + " in " + month.Format("2006-01") + " is well above its trend", Link: "/reports/kpis?month=" + month.Format("2006-01"),
				Audience: notify.Audience{Capability: string(authz.Budgets), Need: "A"}}); err != nil {
				return err
			}
		}
		return nil
	})
	return raised, err
}
