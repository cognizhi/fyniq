package outbox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Schedule is one of the platform's own jobs. Its slots fall every Every after Offset from the
// Unix epoch; a run holds Lease, and the watchdog gives a run up when its lease expires.
type Schedule struct {
	Job    string
	Every  time.Duration
	Offset time.Duration
	Lease  time.Duration
	// Stale is how long without a heartbeat before the absence alert fires.
	Stale time.Duration
}

// PlatformSchedules are the jobs the tick runs for the installation (§12.11): audit anchoring every
// hour, the nightly verification at 02:00 Singapore time (18:00 UTC), the hourly purge of expired
// idempotency keys (FR-INT-08: kept 24 hours) and of ended rate-limit windows (FR-INT-16), the nightly
// comparison of every organisation's balances
// with its journal lines at 02:30 (CTL-FIN-01), the nightly replay of every entry under the rule version that posted
// it at 02:45 (ENG-06, QA-ENG-06), the nightly count of Super admins at 03:00 (CTL-IAM-07),
// and the night's control evidence at 04:00, after them (compliance/, PRD-001 §14.1), the sending domains' check at
// 04:30 (FR-INT-21) and the end of expired quarantined mail at 04:45 (FR-INT-22); every hour, the
// commit and the sweep of evidence files (FR-DOC-13), and at 03:30 the re-verification of their hashes.
var PlatformSchedules = []Schedule{
	{Job: "audit.anchor", Every: time.Hour, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	{Job: "audit.verify", Every: 24 * time.Hour, Offset: 18 * time.Hour, Lease: 6 * time.Hour, Stale: 26 * time.Hour},
	{Job: "idempotency.purge", Every: time.Hour, Offset: 30 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	{Job: "ratelimit.purge", Every: time.Hour, Offset: 45 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	{Job: "ledger.verify", Every: 24 * time.Hour, Offset: 18*time.Hour + 30*time.Minute, Lease: 2 * time.Hour, Stale: 26 * time.Hour},
	// Nightly at 02:45, every stored entry posted again under the rule version that posted it (ENG-06, QA-ENG-06; rev 0.67).
	{Job: "ledger.replay", Every: 24 * time.Hour, Offset: 18*time.Hour + 45*time.Minute, Lease: 2 * time.Hour, Stale: 26 * time.Hour},
	{Job: "superadmins.verify", Every: 24 * time.Hour, Offset: 19 * time.Hour, Lease: 30 * time.Minute, Stale: 26 * time.Hour},
	{Job: "compliance.evidence", Every: 24 * time.Hour, Offset: 20 * time.Hour, Lease: time.Hour, Stale: 26 * time.Hour},
	// Nightly at 04:30, the sending domains' DNS records and DMARC policy (FR-INT-21, rev 0.104).
	{Job: "email.domains", Every: 24 * time.Hour, Offset: 20*time.Hour + 30*time.Minute, Lease: 30 * time.Minute, Stale: 26 * time.Hour},
	// Nightly at 04:45, quarantined mail whose days have passed ends and its files go (FR-INT-22, rev 0.106).
	{Job: "inbound.expire", Every: 24 * time.Hour, Offset: 20*time.Hour + 45*time.Minute, Lease: 30 * time.Minute, Stale: 26 * time.Hour},
	// Daily at 07:30 Singapore time, before the daily digest, each organisation's bills to pay (FR-NTF-01, FR-AP-13; rev 0.110).
	{Job: "notify.bills_due", Every: 24 * time.Hour, Offset: 23*time.Hour + 30*time.Minute, Lease: 30 * time.Minute, Stale: 26 * time.Hour},
	// Every hour, notifications' held and digested email (FR-NTF-02, rev 0.108): the daily digest after 08:00 Singapore
	// time, the weekly on Mondays, and immediate email held in quiet hours once they end.
	{Job: "notify.digest", Every: time.Hour, Offset: 5 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	// Evidence (FR-DOC-13): hourly, commit the files of records no longer drafts that the outbox missed, and
	// sweep staging files no row uses after a day, reporting locked files no row knows.
	{Job: "evidence.commit", Every: time.Hour, Offset: 10 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	{Job: "evidence.sweep", Every: time.Hour, Offset: 50 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	// …and nightly at 03:30, re-hash every committed file (FR-DOC-07, CTL-AUD-06).
	{Job: "evidence.verify", Every: 24 * time.Hour, Offset: 19*time.Hour + 30*time.Minute, Lease: 2 * time.Hour, Stale: 26 * time.Hour},
	// Hourly, the automatic reversals due by each organisation's own today (FR-GL-02, FR-AUT-01; rev 0.55).
	{Job: "journals.auto-reverse", Every: time.Hour, Offset: 20 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	// Hourly, the drafts of the recurring journals due by each organisation's own today (FR-GL-02, FR-AUT-01; rev 0.60).
	{Job: "journals.recurring", Every: time.Hour, Offset: 10 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	// Daily at 02:25 in Singapore, the feed's rates of the last week's days that have none yet (FR-GL-05; rev 0.70).
	{Job: "fx.feed", Every: 24 * time.Hour, Offset: 18*time.Hour + 25*time.Minute, Lease: 30 * time.Minute, Stale: 26 * time.Hour},
	// Daily at 03:05 in Singapore, after the feed: the month-end revaluations due whose last day's rates are in
	// (FR-GL-04, FR-AUT-01; rev 0.75).
	{Job: "fx.revalue", Every: 24 * time.Hour, Offset: 19*time.Hour + 5*time.Minute, Lease: time.Hour, Stale: 26 * time.Hour},
	// Hourly, the reversals of month-end revaluations that waited for their next period to open (FR-GL-04; rev 0.73).
	{Job: "fx.reversals", Every: time.Hour, Offset: 35 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	// Hourly, the draft bills of the recurring bills due by each organisation's own today (FR-AP-09, FR-AUT-01; rev 0.84).
	{Job: "bills.recurring", Every: time.Hour, Offset: 15 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	// Hourly, the releases of bills' spread costs due by each organisation's own today (FR-GL-03, ENG-05; rev 0.83).
	{Job: "bills.releases", Every: time.Hour, Offset: 40 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
	// Hourly, the releases of invoices' spread revenue due by each organisation's own today (FR-GL-03, ENG-05; rev 0.118).
	{Job: "invoices.releases", Every: time.Hour, Offset: 45 * time.Minute, Lease: 30 * time.Minute, Stale: 3 * time.Hour},
}

// TickInterval is how often Cloud Scheduler (locally the API's own loop) calls the tick; a setting.
const TickInterval = 5 * time.Minute

// Slot is the latest slot of s at or before now.
func (s Schedule) Slot(now time.Time) time.Time {
	since := now.UTC().Sub(time.Unix(0, 0).UTC().Add(s.Offset))
	return time.Unix(0, 0).UTC().Add(s.Offset + since/s.Every*s.Every)
}

// Ticker is the one Scheduler tick (§12.11): it starts due platform jobs, sweeps every
// organisation's outbox, and beats. It runs inside a request (Cloud Scheduler calls the API).
type Ticker struct {
	Store      storage.Store
	Platform   storage.Platform
	Dispatcher Dispatcher
	Jobs       ports.JobRunner
	Clock      ports.Clock
	Holder     string
	Schedules  []Schedule
	// PendingGrace leaves a just-committed row to its own after-commit dispatch; 0 means a minute.
	PendingGrace time.Duration
	// Dead runs after the sweep marks an overrun row dead; nil does nothing.
	Dead Dead
}

// TickReport says what one tick did.
type TickReport struct {
	Started    []string // job@slot
	Skipped    int      // missed slots recorded as skipped
	Busy       []string // jobs whose previous run still holds its lease
	Dispatched int      // outbox rows (re)dispatched
	Dead       int      // rows whose attempts ran out
	Errors     []string
}

// Tick runs once. Errors in one job or organisation do not stop the others; the tick beats only
// when it finished without error, so a failing tick is caught by the absence alert.
func (t Ticker) Tick(ctx context.Context) (TickReport, error) {
	var r TickReport
	now := t.Clock.Now()
	for _, s := range t.Schedules {
		if err := t.schedule(ctx, s, now, &r); err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: %v", s.Job, err))
		}
	}
	if err := t.sweep(ctx, now, &r); err != nil {
		r.Errors = append(r.Errors, "sweep: "+err.Error())
	}
	if len(r.Errors) > 0 {
		return r, errors.New(fmt.Sprint(r.Errors))
	}
	return r, t.Platform.Beat(ctx, "tick", now, fmt.Sprintf("%d started, %d dispatched", len(r.Started), r.Dispatched))
}

// schedule runs a job's latest slot once, after recording the slots a gap made it miss
// (Missed slots and overlapping runs).
func (t Ticker) schedule(ctx context.Context, s Schedule, now time.Time, r *TickReport) error {
	latest := s.Slot(now)
	last, ran, err := t.Platform.LatestSlot(ctx, s.Job)
	if err != nil {
		return err
	}
	if ran && !last.Before(latest) {
		return nil
	}
	if ran {
		var missed []time.Time
		for slot := last.Add(s.Every); slot.Before(latest); slot = slot.Add(s.Every) {
			missed = append(missed, slot)
		}
		if len(missed) > 0 {
			n, err := t.Platform.SkipSlots(ctx, s.Job, missed, now)
			if err != nil {
				return err
			}
			r.Skipped += n
		}
	}
	claimed, err := t.Platform.ClaimSlot(ctx, s.Job, latest, t.Holder, now.Add(s.Lease), now)
	if err != nil {
		return err
	}
	if !claimed {
		r.Busy = append(r.Busy, s.Job)
		return nil
	}
	exec, err := t.Jobs.Start(ctx, s.Job, latest)
	if err != nil {
		_, _ = t.Platform.FinishSlot(ctx, s.Job, latest, false, "could not start: "+err.Error(), now)
		return err
	}
	r.Started = append(r.Started, fmt.Sprintf("%s@%s", s.Job, latest.Format(time.RFC3339)))
	return t.Platform.StartedSlot(ctx, s.Job, latest, exec)
}

// sweep dispatches rows that never left or overran, and ends rows whose attempts ran out. Rows
// waiting on reconciliation are left alone.
func (t Ticker) sweep(ctx context.Context, now time.Time, r *TickReport) error {
	grace := t.PendingGrace
	if grace <= 0 {
		grace = time.Minute
	}
	orgs, err := t.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, org := range orgs {
		var resend, dead []storage.OutboxRow
		err := t.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			resend, dead = nil, nil // a retried unit of work starts again
			due, err := u.Sweeping().Due(ctx, now, now.Add(-grace))
			if err != nil {
				return err
			}
			for _, row := range due {
				if row.State != storage.OutboxPending && row.Attempts >= row.MaxAttempts {
					if err := u.Sweeping().MarkDead(ctx, row.ID, "the attempts ran out and the last one overran its deadline"); err != nil {
						return err
					}
					if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "outbox.row.dead", ObjectType: "outbox", ObjectID: row.ID,
						Changes: map[string][2]any{"state": {string(row.State), "dead"}, "kind": {nil, row.Kind}, "attempts": {nil, row.Attempts}}}); err != nil {
						return err
					}
					dead = append(dead, row)
					r.Dead++
					continue
				}
				resend = append(resend, row)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		for _, row := range dead {
			if t.Dead == nil {
				break
			}
			if err := t.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				return t.Dead(ctx, u, org, row, "the attempts ran out and the last one overran its deadline")
			}); err != nil {
				errs = append(errs, fmt.Errorf("telling of dead row %s: %w", row.ID, err))
			}
		}
		for _, row := range resend {
			if err := t.Dispatcher.Dispatch(ctx, org, row); err != nil {
				errs = append(errs, fmt.Errorf("dispatch %s: %w", row.ID, err))
				continue
			}
			r.Dispatched++
		}
	}
	return errors.Join(errs...)
}

// RunSlot runs a platform job's slot where the job executes, then records how it ended and beats on
// success. A failed run does not beat, so the absence alert notices repeated failures too.
func RunSlot(ctx context.Context, p storage.Platform, clock ports.Clock, job string, slot time.Time, run func(ctx context.Context) error) error {
	err := run(ctx)
	detail := "done"
	if err != nil {
		detail = err.Error()
	}
	if _, ferr := p.FinishSlot(ctx, job, slot, err == nil, detail, clock.Now()); ferr != nil && err == nil {
		err = ferr
	}
	if err == nil {
		err = p.Beat(ctx, job, clock.Now(), slot.Format(time.RFC3339))
	}
	return err
}

// Absences returns the heartbeats that are missing or older than their limit: the tick (three tick
// intervals) and each schedule's Stale. It runs outside the request path (Heartbeat outside the
// request path): Cloud Monitoring's absence alert in production, `jobs heartbeat-check` locally.
func Absences(ctx context.Context, p storage.Platform, now time.Time, schedules []Schedule) ([]string, error) {
	beats, err := p.Heartbeats(ctx)
	if err != nil {
		return nil, err
	}
	limits := map[string]time.Duration{"tick": 3 * TickInterval}
	for _, s := range schedules {
		limits[s.Job] = s.Stale
	}
	var out []string
	for name, limit := range limits {
		last, ok := beats[name]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s has never beaten", name))
		case now.Sub(last) > limit:
			out = append(out, fmt.Sprintf("%s last beat %s ago (limit %s)", name, now.Sub(last).Round(time.Minute), limit))
		}
	}
	sort.Strings(out)
	return out, nil
}
