package reminders

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/deadlines"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DeadlinesJob is the daily platform job that tells of the statutory calendar's deadlines (FR-TAX-07, CMP-02; p1-gst
// design D10), after the bills to pay and before the 08:00 digest (outbox.PlatformSchedules).
const DeadlinesJob = "notify.deadlines"

// The notification types of the deadlines.
const (
	DeadlineReminder = "deadline.reminder"
	DeadlineOverdue  = "deadline.overdue"
)

// ReminderDays are the days before an item's due day that it is told of (CMP-02).
var ReminderDays = []int{30, 14, 7, 1}

// deadlineDue keys each organisation's notice of an item on a day before it, so a rerun adds nothing.
var deadlineDue = uuid.MustParse("3f6b1f6a-7c2e-4f0e-9a51-2d8c4b7e9a17")

// DeadlinesDue tells, once a day in each organisation, of each item of the statutory calendar 30, 14, 7 or 1 days
// before its due day with nothing recorded, and of each the day after it was due. It returns how many notices were
// emitted; an organisation that fails does not stop the others.
func DeadlinesDue(ctx context.Context, store storage.Store, d outbox.Dispatcher) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var told int
	var errs []error
	for _, org := range orgs {
		n, err := DeadlinesDueIn(ctx, store, d, org)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
		told += n
	}
	return told, errors.Join(errs...)
}

// DeadlinesDueIn is DeadlinesDue in one organisation: how many notices it emitted. Those who record the calendar's
// filings hear (Appendix D's Statutory calendar row, C).
func DeadlinesDueIn(ctx context.Context, store storage.Store, d outbox.Dispatcher, org uuid.UUID) (int, error) {
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
		c, err := deadlines.Read(ctx, u, today, today.AddDate(0, 0, slices.Max(ReminderDays)), today)
		if err != nil {
			return err
		}
		audience := notify.Audience{Capability: string(authz.StatutoryCalendar), Need: "C"}
		for _, it := range c.Items {
			if it.Due.IsZero() {
				continue
			}
			left := int(it.Due.Sub(today).Hours() / 24)
			var typ, title string
			switch {
			case (it.Status == deadlines.Open || it.Status == deadlines.DueSoon) && slices.Contains(ReminderDays, left):
				typ, title = DeadlineReminder, fmt.Sprintf("%s: due in %d days, on %s", it.Title, left, it.Due.Format("2 Jan 2006"))
				if left == 1 {
					title = fmt.Sprintf("%s: due tomorrow, %s", it.Title, it.Due.Format("2 Jan 2006"))
				}
			case it.Status == deadlines.Overdue && left == -1:
				typ, title = DeadlineOverdue, fmt.Sprintf("%s: it was due yesterday, %s, and nothing is recorded", it.Title, it.Due.Format("2 Jan 2006"))
			default:
				continue
			}
			key := uuid.NewSHA1(deadlineDue, []byte(org.String()+":"+typ+":"+it.Key+":"+fmt.Sprint(left)))
			if done, err := u.Notifications().Emitted(ctx, typ, key); err != nil || done {
				if err != nil {
					return err
				}
				continue
			}
			link := "/compliance/calendar"
			if it.Register && it.Link != "" {
				link = it.Link
			}
			if _, err := notify.Emit(ctx, d, u, org, notify.Event{Type: typ, RecordType: "statutory_deadline", RecordID: &key, Title: title, Link: link,
				At: now, Audience: audience}); err != nil {
				return err
			}
			told++
		}
		return nil
	})
	return told, err
}
