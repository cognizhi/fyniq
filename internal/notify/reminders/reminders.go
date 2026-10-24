// Package reminders tells, once a day, of what waits to be done by a date (PRD-001 FR-NTF-01, rev 0.110): the bills to
// pay, as the Inbox lists them (FR-AP-13). It reads the ledger, so package notify does not.
package reminders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Job is the daily platform job that tells of the bills to pay, before the 08:00 digest (outbox.PlatformSchedules).
const Job = "notify.bills_due"

// billsDue keys each organisation's day of bills to pay, so a second run that day adds nothing.
var billsDue = uuid.MustParse("6f1d6c1e-55a8-4f39-9a5e-9b3f0e0c2d41")

// BillsDue tells those who record bill payments (P on the bills row), once a day in each organisation, of the bills to
// pay as the Inbox lists them (FR-AP-13): overdue, or due or planned within the organisation's reminder days. It
// returns how many organisations were told; one that fails does not stop the others.
func BillsDue(ctx context.Context, store storage.Store, d outbox.Dispatcher) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var told int
	var errs []error
	for _, org := range orgs {
		ok, err := BillsDueIn(ctx, store, d, org)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
		if ok {
			told++
		}
	}
	return told, errors.Join(errs...)
}

// BillsDueIn is BillsDue in one organisation: true when it told of bills to pay.
func BillsDueIn(ctx context.Context, store storage.Store, d outbox.Dispatcher, org uuid.UUID) (bool, error) {
	now := d.Clock.Now()
	told := false
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
		key := uuid.NewSHA1(billsDue, []byte(org.String()+":"+today.Format(time.DateOnly)))
		if done, err := u.Notifications().Emitted(ctx, "bills.due", key); err != nil || done {
			return err
		}
		base, err := money.Lookup(o.BaseCurrency)
		if err != nil {
			return err
		}
		accounts, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		var ap uuid.UUID
		for _, x := range accounts {
			if string(x.Role) == ledger.RoleAccountsPayable {
				ap = x.ID
			}
		}
		if ap == uuid.Nil {
			return nil // no accounts payable: no bill has posted
		}
		def, _ := settings.Lookup("purchases.reminder_days")
		changes, err := u.Settings().Changes(ctx, def.Key)
		if err != nil {
			return err
		}
		days, err := strconv.Atoi(settings.On(def, changes, today).Value)
		if err != nil {
			return err
		}
		rs, err := payables.Reminders(ctx, u, base, ap, today, days)
		if err != nil || len(rs) == 0 {
			return err
		}
		overdue := 0
		for _, r := range rs {
			if r.Why == payables.WhyOverdue {
				overdue++
			}
		}
		title := fmt.Sprintf("%d bills to pay: %d overdue, %d due or planned within %d days", len(rs), overdue, len(rs)-overdue, days)
		if len(rs) == 1 {
			title = "1 bill to pay: overdue"
			if overdue == 0 {
				title = fmt.Sprintf("1 bill to pay: due or planned within %d days", days)
			}
		}
		if _, err := notify.Emit(ctx, d, u, org, notify.Event{Type: "bills.due", RecordType: "bills_due", RecordID: &key, Title: title, Link: "/inbox", At: now,
			Audience: notify.Audience{Capability: approval.Rows[approval.ObjectPayment], Need: "P"}}); err != nil {
			return err
		}
		told = true
		return nil
	})
	return told, err
}
