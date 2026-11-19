package checks

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/projectrate"
	"github.com/cognizhi/fyniq/internal/domain/projectreport"
	"github.com/cognizhi/fyniq/internal/domain/timesheet"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D rows whose view opens the projects' items (design D7).
const (
	viewTimesheets = "Timesheets and project time (rev 0.7)"
)

// staleSubmitted is how long a submitted week may wait for its manager, and a returned one for its person, before it is flagged
// (C.1 #27); closedWeeks is how many past weeks C.2 #41 looks back over.
const (
	staleDays   = 7
	closedWeeks = 4
)

var onTime = []string{"time_week", "project", "dimension", "journal_entry", "document", "period"}

// The checks of projects and time (p1-projects task 4.3): off with the organisation's feature switch.
var projectChecks = []Definition{
	{Key: "projects.budget-and-margin", Ref: "C.1 #26", Area: "Projects", Worst: Warning, Title: "No project is over its hours budget or loses money",
		Explain: "A project whose approved hours passed its hours budget, or whose posted revenue is below its direct costs and its time at cost (at the " +
			"role-average or standard rate, never a person's own), needs a look before more is spent on it. Margin is revenue less time at cost less direct costs.",
		Rules: "FR-PRJ-09, FR-PRJ-12", Owner: "controller", View: viewTimesheets, Module: "features.projects", Triggers: onTime, Run: overBudgetOrLosing},
	{Key: "projects.timesheet-problems", Ref: "C.1 #27", Area: "Projects", Worst: Warning, Title: "No week of time waits too long",
		Explain: "A week submitted more than 7 days ago that its manager has not decided, or one returned more than 7 days ago that its person has not fixed " +
			"and sent again, keeps time out of the project's figures and out of invoices.",
		Rules: "FR-PRJ-05, FR-PRJ-07", Owner: "controller", View: viewTimesheets, Module: "features.projects", Triggers: onTime, Run: timesheetProblems},
	{Key: "projects.closed-weeks", Ref: "C.2 #41", Area: "Projects", Worst: Warning, Title: "Every closed week has approved time",
		Explain: "For each of the last four weeks that ended, a person on a project who entered no time, or whose time is not approved yet, is listed, so " +
			"utilisation, profitability and invoices are not worked out from half a month of time.",
		Rules: "FR-PRJ-05, FR-PRJ-07", Owner: "controller", View: viewTimesheets, Module: "features.projects", Triggers: onTime, Run: closedWeeksNeedApprovedTime},
}

// overBudgetOrLosing lists each project over its hours budget or with a negative margin (C.1 #26).
func overBudgetOrLosing(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	ps, err := u.Projects().List(ctx)
	if err != nil {
		return Result{}, err
	}
	costRates, err := u.Rates().CostRates(ctx)
	if err != nil {
		return Result{}, err
	}
	var first, last time.Time
	if len(e.Periods) > 0 {
		first, last = e.Periods[0].StartsOn, e.Periods[len(e.Periods)-1].StartsOn
	}
	ledger := map[uuid.UUID]projectreport.Ledger{}
	if !first.IsZero() {
		if ledger, _, err = projectreport.LedgerByProject(ctx, u, e.Base, first, last); err != nil {
			return Result{}, err
		}
	}
	var items []Item
	for _, p := range ps {
		if !p.Configured || p.Status == storage.ProjectClosed || p.Status == storage.ProjectPlanned {
			continue
		}
		es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{ProjectID: p.ID, Status: storage.TimeApproved, Limit: 20000})
		if err != nil {
			return Result{}, err
		}
		billing, err := u.Rates().BillingRates(ctx, p.ID, p.CustomerID)
		if err != nil {
			return Result{}, err
		}
		t, err := projectreport.TimeOf(es, costRates, billing, p, projectrate.Averaged, e.Base)
		if err != nil {
			return Result{}, err
		}
		it := Item{Kind: "document", ID: p.ID.String(), Label: p.Code + " " + p.Name}
		if p.BudgetMinutes != nil && *p.BudgetMinutes > 0 && t.Minutes > *p.BudgetMinutes {
			it.Detail = fmt.Sprintf("%d of %d budgeted minutes used", t.Minutes, *p.BudgetMinutes)
		}
		if l, ok := ledger[p.ID]; ok && l.Revenue.Sign() > 0 {
			if m, err := projectreport.Margin(l, t); err == nil && m != nil && m.Sign() < 0 {
				it.Detail = strings.TrimSpace(it.Detail + " loses " + m.Neg().String() + " " + e.Base.Code)
			}
		}
		if it.Detail != "" {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return Fine("No project is over its hours budget or loses money"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d project(s) over budget or losing money", len(items)), "Open the project's report, and its time and bills, and decide whether to change its budget or its billing"), nil
}

// timesheetProblems lists weeks submitted or returned and left for more than staleDays (C.1 #27).
func timesheetProblems(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	names := map[uuid.UUID]string{}
	ps, err := u.Projects().List(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, p := range ps {
		names[p.ID] = p.Code
	}
	var items []Item
	for _, status := range []string{storage.TimeSubmitted, storage.TimeReturned} {
		subs, err := u.Timesheets().Submissions(ctx, storage.SubmissionFilter{Status: status, Limit: 500})
		if err != nil {
			return Result{}, err
		}
		for _, s := range subs {
			since := s.SubmittedAt
			if s.DecidedAt != nil {
				since = *s.DecidedAt
			}
			// A returned week is the person's again until they send it anew: its entries then belong to a later submission.
			if status == storage.TimeReturned {
				es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{SubmissionID: s.ID, Status: storage.TimeReturned, Limit: 1})
				if err != nil {
					return Result{}, err
				}
				if len(es) == 0 {
					continue
				}
			}
			if e.Now.Sub(since) > staleDays*24*time.Hour {
				what := "waits for its manager"
				if status == storage.TimeReturned {
					what = "was returned and not sent again"
				}
				items = append(items, Item{Kind: "document", ID: s.ID.String(), Label: fmt.Sprintf("%s, week of %s", names[s.ProjectID], s.WeekStart.Format(time.DateOnly)), Detail: what})
			}
		}
	}
	if len(items) == 0 {
		return Fine("No week of time waits more than 7 days"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d week(s) of time left waiting", len(items)), "Approve or return the weeks waiting for a manager; ask the person to fix and send a returned week"), nil
}

// closedWeeksNeedApprovedTime lists, for each of the last closedWeeks weeks that ended, a member of an active project with no time
// entered or time not yet approved (C.2 #41).
func closedWeeksNeedApprovedTime(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	ps, err := u.Projects().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var people []uuid.UUID
	for _, p := range ps {
		if !p.Configured || p.Status != storage.ProjectActive {
			continue
		}
		ms, err := u.Projects().Members(ctx, p.ID)
		if err != nil {
			return Result{}, err
		}
		for _, m := range ms {
			if m.Active && !slices.Contains(people, m.UserID) {
				people = append(people, m.UserID)
			}
		}
	}
	thisWeek := timesheet.Week(e.Today)
	names := map[uuid.UUID]string{}
	if ms, err := u.Members().List(ctx, e.Now); err == nil {
		for _, m := range ms {
			names[m.UserID] = m.DisplayName
		}
	}
	var items []Item
	for i := 1; i <= closedWeeks; i++ {
		week := thisWeek.AddDate(0, 0, -7*i)
		for _, who := range people {
			es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{UserID: who, From: week, To: week.AddDate(0, 0, 6), Limit: 500})
			if err != nil {
				return Result{}, err
			}
			it := Item{Kind: "document", ID: who.String() + "@" + week.Format(time.DateOnly), Label: fmt.Sprintf("%s, week of %s", names[who], week.Format(time.DateOnly))}
			switch {
			case len(es) == 0:
				it.Detail = "no time entered"
			case slices.ContainsFunc(es, func(x storage.TimeEntry) bool { return x.Status != storage.TimeApproved }):
				it.Detail = "time not approved yet"
			default:
				continue
			}
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return Fine("Every closed week of the last four has approved time"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d closed week(s) with missing or unapproved time", len(items)), "Enter the missing time, then send the week; managers approve what is waiting"), nil
}
