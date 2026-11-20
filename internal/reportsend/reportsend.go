// Package reportsend sends the saved reports that are due (PRD-001 FR-RPT-07; p2-reporting-planning design D3). The hourly platform job
// reads each organisation's due schedules and, for each recipient, checks again that the person is an active member who may read the
// whole ledger's reports and open the report; a recipient who may not is skipped and the run says why. A recipient who may is told by a
// notification that names the report and its month and links to the signed-in report: it carries no figure and attaches no file, so
// nothing is read in an email that the page would not show only to the right person. A run is recorded once per schedule and day.
package reportsend

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/reportdef"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Job is the platform job's name (outbox.PlatformSchedules).
const Job = "reports.send"

// Actor is the principal the job records its runs as.
var Actor = storage.Actor{Type: "system", ID: Job}

// Runner sends what is due.
type Runner struct {
	Store  storage.Store
	Outbox outbox.Dispatcher
	Clock  ports.Clock
}

type skipped struct {
	UserID string `json:"user_id"`
	Why    string `json:"why"`
}

// RunAll runs every organisation's due schedules and returns how many recipients were told.
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
			return total, fmt.Errorf("reportsend: %s: %w", org, err)
		}
	}
	return total, nil
}

// RunOrganisation runs one organisation's due schedules, each in its own transaction, so one that fails leaves the others to run.
func (r Runner) RunOrganisation(ctx context.Context, org uuid.UUID) (int, error) {
	now := r.Clock.Now()
	today := dayOf(now.In(appclock.Singapore))
	var due []uuid.UUID
	if err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		ss, err := u.ReportSchedules().Due(ctx, today)
		for _, s := range ss {
			due = append(due, s.ID)
		}
		return err
	}); err != nil {
		return 0, err
	}
	sent := 0
	var first error
	for _, id := range due {
		n, err := r.runOne(ctx, org, id, today, now)
		sent += n
		if err != nil && first == nil {
			first = err
		}
	}
	return sent, first
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (r Runner) runOne(ctx context.Context, org, id uuid.UUID, today, now time.Time) (sent int, err error) {
	err = r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		s, err := u.ReportSchedules().Get(ctx, id)
		if err != nil {
			return err
		}
		if !s.Active || s.NextRunOn.After(today) {
			return nil // changed since the list was read
		}
		d, err := u.ReportDefinitions().Get(ctx, s.DefinitionID)
		if err != nil {
			return err
		}
		period, why, err := periodOf(ctx, u, s.PeriodRule, today)
		if err != nil {
			return err
		}
		revision, err := u.Ledger().Revision(ctx)
		if err != nil {
			return err
		}
		var skips []skipped
		switch {
		case s.Channel == storage.ChannelSlack:
			for _, rc := range s.Recipients {
				skips = append(skips, skipped{rc.String(), "Slack delivery waits for the Slack connector [live-verify]"})
			}
		case why != "":
			for _, rc := range s.Recipients {
				skips = append(skips, skipped{rc.String(), why})
			}
		default:
			for _, rc := range s.Recipients {
				if reason := mayRead(ctx, u, d, rc, now); reason != "" {
					skips = append(skips, skipped{rc.String(), reason})
					continue
				}
				link := "/reports/builder?report=" + d.ID.String() + "&to=" + period
				if _, err := notify.Emit(ctx, r.Outbox, u, org, notify.Event{Type: notify.ReportScheduledType, RecordType: "report_definition", RecordID: &d.ID, At: now,
					Title: "The report " + d.Name + " for " + period + " is ready", Link: link, Audience: notify.Audience{Users: []uuid.UUID{rc}}}); err != nil {
					return err
				}
				sent++
			}
		}
		raw, _ := json.Marshal(skips)
		if skips == nil {
			raw = []byte("[]")
		}
		w, dom := 0, 0
		if s.Weekday != nil {
			w = *s.Weekday
		}
		if s.DayOfMonth != nil {
			dom = *s.DayOfMonth
		}
		next := reportdef.NextRun(s.Cadence, w, dom, today)
		_, err = u.ReportSchedules().Ran(ctx, storage.ReportScheduleRun{ScheduleID: s.ID, RanOn: today, PeriodLabel: period, LedgerRevision: revision, Sent: sent, Skipped: raw, RanAt: now}, next, s.Version)
		return err
	})
	if err != nil {
		sent = 0
	}
	return sent, err
}

// periodOf is the month a schedule's report is for on day today: the latest closed month, or the month so far. why is set when there is
// no closed month yet.
func periodOf(ctx context.Context, u storage.UnitOfWork, rule string, today time.Time) (period, why string, err error) {
	if rule == storage.PeriodMonthToDate {
		return today.Format("2006-01"), "", nil
	}
	ps, err := u.Periods().List(ctx)
	if err != nil {
		return "", "", err
	}
	var latest *storage.Period
	for i := range ps {
		if ps[i].Status == storage.PeriodClosed && (latest == nil || ps[i].StartsOn.After(latest.StartsOn)) {
			latest = &ps[i]
		}
	}
	if latest == nil {
		return "none", "no month is closed yet", nil
	}
	return latest.StartsOn.Format("2006-01"), "", nil
}

// mayRead is why a recipient is not told, or "" when they are: an active member, holding an unqualified view of the reports, who may open
// the report (its owner, or it is shared).
func mayRead(ctx context.Context, u storage.UnitOfWork, d storage.ReportDefinition, user uuid.UUID, now time.Time) string {
	rs, err := u.Members().Roles(ctx, user, now)
	if err != nil || len(rs) == 0 {
		return "is no longer an active member"
	}
	roles := make([]authz.Role, 0, len(rs))
	for _, x := range rs {
		roles = append(roles, authz.Role(x.Role))
	}
	switch {
	case !authz.Decide(authz.RunReportDefinition, roles, false).Allowed:
		return "no longer reads the whole ledger's reports"
	case d.Visibility != storage.ReportShared && d.OwnerID != user:
		return "cannot open a private report"
	}
	return ""
}
