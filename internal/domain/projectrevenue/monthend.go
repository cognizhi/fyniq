package projectrevenue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MonthEndJob is the daily platform job that proposes, for each project elected for revenue over time, the journal of the month that has just ended, as a draft only, through the database's narrow
// function (FR-PRJ-11, rev 0.587; p2-customer-portal-subscriptions task 4.1). It does nothing while Projects and time are switched off, and a month already proposed, a project with nothing to
// move, an earlier month not posted yet, or accounts not set up are skipped, each to be tried again the next day. A person approves and posts each draft.
const MonthEndJob = "projects.revenue_proposals"

// MonthEndAll proposes the previous month's journals in every organisation; one organisation's or one project's failure never stops the others. It returns how many drafts were made.
func MonthEndAll(ctx context.Context, store storage.Store, clock ports.Clock) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var made int
	var errs []error
	for _, org := range orgs {
		n, err := MonthEndIn(ctx, store, clock, org)
		made += n
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return made, errors.Join(errs...)
}

// MonthEndIn is MonthEndAll in one organisation.
func MonthEndIn(ctx context.Context, store storage.Store, clock ports.Clock, org uuid.UUID) (int, error) {
	var projects []uuid.UUID
	var today time.Time
	if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		def, _ := settings.Lookup("features.projects")
		changes, err := u.Settings().Changes(ctx, def.Key)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Profile.TimeZone)
		if err != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		y, m, d := clock.Now().In(loc).Date()
		today = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		if settings.On(def, changes, today).Value != "true" {
			return nil
		}
		all, err := u.ProjectRevenue().Elections(ctx)
		for _, e := range all {
			if e.Method == storage.RevenueOverTime {
				projects = append(projects, e.ProjectID)
			}
		}
		return err
	}); err != nil {
		return 0, err
	}
	month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	var made int
	var errs []error
	for _, p := range projects {
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, _, err := ProposeByFunction(ctx, u, p, month, today)
			return err
		})
		var refusal *Error
		switch {
		case err == nil:
			made++
		case errors.As(err, &refusal):
			// Not made today, and not a failure: the next day tries again.
		default:
			errs = append(errs, fmt.Errorf("project %s: %w", p, err))
		}
	}
	return made, errors.Join(errs...)
}
