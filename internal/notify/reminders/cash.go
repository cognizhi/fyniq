package reminders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/cashflow"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CashBufferJob is the daily platform job that tells when the short-term cash flow falls below the buffer line
// (FR-PLN-09, FR-NTF-01; p1-planning design D7), after the deadlines and before the 08:00 digest
// (outbox.PlatformSchedules).
const CashBufferJob = "notify.cash_buffer"

// The notification types of the buffer.
const (
	CashBelowBuffer     = "cash.below_buffer"
	CashBufferRecovered = "cash.buffer_recovered"
)

// cashCrossing keys each organisation's crossings of the buffer, the nth below and its recovery.
var cashCrossing = uuid.MustParse("6d0f4b3e-2a8c-4f5d-b1e7-9c3a5e7f2d41")

// crossingKey is the record a crossing's notice is of: so a rerun adds nothing, and the notices so far say where the
// organisation stands.
func crossingKey(org uuid.UUID, typ string, n int) uuid.UUID {
	return uuid.NewSHA1(cashCrossing, []byte(fmt.Sprintf("%s:%s:%d", org, typ, n)))
}

// CashBuffer tells, once a day in each organisation, when the default 60-day projection first falls below the buffer
// line, once per crossing: it tells again only after the projection has gone back above the line, which it also tells.
// It returns how many notices were emitted; an organisation that fails does not stop the others.
func CashBuffer(ctx context.Context, store storage.Store, d outbox.Dispatcher) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var told int
	var errs []error
	for _, org := range orgs {
		n, err := CashBufferIn(ctx, store, d, org)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
		told += n
	}
	return told, errors.Join(errs...)
}

// CashBufferIn is CashBuffer in one organisation: how many notices it emitted. Those who hold C or A on "Budgets,
// forecasts" hear (Appendix D).
func CashBufferIn(ctx context.Context, store storage.Store, d outbox.Dispatcher, org uuid.UUID) (int, error) {
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
		in, err := cashflow.Read(ctx, u, today, cashflow.DefaultHorizon)
		if err != nil {
			return err
		}
		p, err := in.Project(cashflow.DefaultHorizon)
		if err != nil {
			return err
		}
		// The crossings told so far, and whether the last has recovered.
		n := 0
		for {
			done, err := u.Notifications().Emitted(ctx, CashBelowBuffer, crossingKey(org, CashBelowBuffer, n+1))
			if err != nil {
				return err
			}
			if !done {
				break
			}
			n++
		}
		below := n > 0
		if below {
			if below, err = u.Notifications().Emitted(ctx, CashBufferRecovered, crossingKey(org, CashBufferRecovered, n)); err != nil {
				return err
			}
			below = !below
		}
		var typ, title string
		var key uuid.UUID
		switch {
		case p.BelowOn != nil && !below:
			typ, key = CashBelowBuffer, crossingKey(org, CashBelowBuffer, n+1)
			title = fmt.Sprintf("Cash is projected to fall below the buffer of %d months' burn on %s", p.Buffer.Months, p.BelowOn.Format("2 Jan 2006"))
			if p.BelowOn.Equal(today) {
				title = fmt.Sprintf("Cash is below the buffer of %d months' burn today", p.Buffer.Months)
			}
		case p.BelowOn == nil && below:
			typ, key = CashBufferRecovered, crossingKey(org, CashBufferRecovered, n)
			title = fmt.Sprintf("Cash is projected to stay above the buffer for the next %d days", p.Days)
		default:
			return nil
		}
		if _, err := notify.Emit(ctx, d, u, org, notify.Event{Type: typ, RecordType: "cash_projection", RecordID: &key, Title: title, Link: "/",
			At: now, Audience: notify.Audience{Capability: string(authz.Budgets), Need: "CA"}}); err != nil {
			return err
		}
		told++
		return nil
	})
	return told, err
}
