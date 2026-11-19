package reminders

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/domain/timesheet"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// TimeJob is the daily platform job that tells each person on a project of the weekdays of this week with no time entered, on the Friday of
// the week in the organisation's own time zone (FR-PRJ-05, FR-NTF-01; p1-projects task 2.6), at 16:00 Singapore time so there is an afternoon
// left to enter it. It does nothing while Projects and time are switched off (FR-ORG-06).
const TimeJob = "notify.timesheets"

// TimeMissing is the notification type: the person's own, never another's (no one reads another person's time through it).
const TimeMissing = "time.missing"

// timeWeek keys each person's notice of a week, so a rerun adds nothing.
var timeWeek = uuid.MustParse("8b4e2c7a-5d1f-4a96-b3e0-7f6a9c1d2e58")

// TimeMissingAll tells, in each organisation, each person on an active project of this week's weekdays with no time entered. It returns how many
// notices were emitted; an organisation that fails does not stop the others.
func TimeMissingAll(ctx context.Context, store storage.Store, d outbox.Dispatcher) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var told int
	var errs []error
	for _, org := range orgs {
		n, err := TimeMissingIn(ctx, store, d, org)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
		told += n
	}
	return told, errors.Join(errs...)
}

// TimeMissingIn is TimeMissingAll in one organisation. Only on a Friday of the organisation's own calendar, and only for a person who is an
// active member of a configured, active project; a weekday that is a public holiday needs no time, and a person who entered time on a weekday
// (a draft, submitted or approved entry on any project) has none missing.
func TimeMissingIn(ctx context.Context, store storage.Store, d outbox.Dispatcher, org uuid.UUID) (int, error) {
	now := d.Clock.Now()
	told := 0
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Profile.TimeZone)
		if err != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		y, m, dd := now.In(loc).Date()
		today := time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		if today.Weekday() != time.Friday {
			return nil
		}
		changes, err := u.Settings().Changes(ctx, "")
		if err != nil {
			return err
		}
		def, ok := settings.Lookup("features.projects")
		if !ok || settings.On(def, changes, today).Value != "true" {
			return nil
		}
		holidays := map[time.Time]bool{}
		if v, found, err := gst.PackOn(ctx, u, "sg-deadlines", today); err != nil {
			return err
		} else if found {
			for _, h := range pack.Holidays(v) {
				holidays[h.On] = true
			}
		}
		week := timesheet.Week(today)
		var days []time.Time
		for i := 0; i < 5; i++ {
			if day := week.AddDate(0, 0, i); !holidays[day] {
				days = append(days, day)
			}
		}
		if len(days) == 0 {
			return nil
		}
		ps, err := u.Projects().List(ctx)
		if err != nil {
			return err
		}
		var people []uuid.UUID
		for _, p := range ps {
			if !p.Configured || p.Status != storage.ProjectActive {
				continue
			}
			ms, err := u.Projects().Members(ctx, p.ID)
			if err != nil {
				return err
			}
			for _, mb := range ms {
				if mb.Active && !slices.Contains(people, mb.UserID) {
					people = append(people, mb.UserID)
				}
			}
		}
		slices.SortFunc(people, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		for _, who := range people {
			es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{UserID: who, From: week, To: week.AddDate(0, 0, 6), Limit: 500})
			if err != nil {
				return err
			}
			have := map[time.Time]bool{}
			for _, e := range es {
				have[e.WorkDate] = true
			}
			var missing []string
			for _, day := range days {
				if !have[day] {
					missing = append(missing, day.Format("Mon 2 Jan"))
				}
			}
			if len(missing) == 0 {
				continue
			}
			key := uuid.NewSHA1(timeWeek, []byte(org.String()+":"+who.String()+":"+week.Format(time.DateOnly)))
			if done, err := u.Notifications().Emitted(ctx, TimeMissing, key); err != nil || done {
				if err != nil {
					return err
				}
				continue
			}
			title := "No time is entered for " + strings.Join(missing, ", ") + ". Add it before you submit the week."
			if _, err := notify.Emit(ctx, d, u, org, notify.Event{Type: TimeMissing, RecordType: "time_week", RecordID: &key, Title: title, Link: "/me/time",
				At: now, Audience: notify.Audience{Users: []uuid.UUID{who}}}); err != nil {
				return err
			}
			told++
		}
		return nil
	})
	return told, err
}
