package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/timesheet"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ time entry, the timer and approval (FR-PRJ-02, FR-PRJ-05, FR-PRJ-07; p1-projects task 2.4)
//
// A person's week is their own: they save its grid, run a timer, and submit each project's part to that project's managers,
// who approve or return it. Time is whole minutes and posts nothing. Everything here answers PRJ-09 while Projects and time
// are switched off (design D7).

// TimeEntry is a person's time on a task on a day, in whole minutes.
type TimeEntry struct {
	ID           string `json:"id" format:"uuid" doc:"The entry's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5001"`
	ProjectID    string `json:"project_id" format:"uuid" doc:"The project." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	TaskID       string `json:"task_id" format:"uuid" doc:"The task." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e51"`
	WorkDate     string `json:"work_date" format:"date" doc:"The day the work was done." example:"2026-11-17"`
	Minutes      int    `json:"minutes" doc:"Whole minutes, 1 to 1,440; a reversing entry has the opposite of the entry it reverses (below zero)." example:"135"`
	Billable     bool   `json:"billable" doc:"Whether it is billed." example:"true"`
	Note         string `json:"note,omitempty" doc:"What was done." example:"Migrated the customer tables"`
	Status       string `json:"status" enum:"draft,submitted,approved,returned" doc:"draft and returned entries can be changed; submitted and approved ones cannot." example:"draft"`
	SubmissionID string `json:"submission_id,omitempty" format:"uuid" doc:"The submission it was sent in." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5101"`
	ReversesID   string `json:"reverses_id,omitempty" format:"uuid" doc:"For a reversing entry, the approved entry it reverses." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5002"`
	Version      int    `json:"version" doc:"Its version." example:"1"`
}

// TimesheetDay is a day of the week.
type TimesheetDay struct {
	Date    string `json:"date" format:"date" doc:"The day." example:"2026-11-17"`
	Weekday string `json:"weekday" enum:"Mon,Tue,Wed,Thu,Fri,Sat,Sun" doc:"Its name." example:"Tue"`
	Holiday string `json:"holiday,omitempty" doc:"The public holiday it is, from the statutory calendar's pack." example:"Deepavali"`
	Missing bool   `json:"missing" doc:"true for a working day, up to today, with no time entered (FR-PRJ-05)." example:"false"`
	Minutes int    `json:"minutes" doc:"The net minutes entered on it, reversing entries counted." example:"480"`
}

// TimesheetTask is a task a person can log time on.
type TimesheetTask struct {
	ID       string `json:"id" format:"uuid" doc:"The task's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e51"`
	Name     string `json:"name" doc:"Its name." example:"Data migration"`
	Billable bool   `json:"billable" doc:"Whether time on it is billable by default." example:"true"`
}

// TimesheetProject is a project a person can log time on, with its tasks in use.
type TimesheetProject struct {
	ID      string          `json:"id" format:"uuid" doc:"The project's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	Code    string          `json:"code" doc:"Its code." example:"ACME-01"`
	Name    string          `json:"name" doc:"Its name." example:"Acme rollout"`
	Billing string          `json:"billing_method" enum:"time_and_materials,fixed_fee,retainer,internal" doc:"How it is billed; time on an internal project is never billable." example:"time_and_materials"`
	Tasks   []TimesheetTask `json:"tasks" nullable:"false" doc:"Its tasks in use."`
}

// TimerView is a person's running timer.
type TimerView struct {
	ProjectID      string `json:"project_id" format:"uuid" doc:"The project." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	TaskID         string `json:"task_id" format:"uuid" doc:"The task." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e51"`
	Billable       bool   `json:"billable" doc:"Whether the entry will be billable." example:"true"`
	Note           string `json:"note,omitempty" doc:"The note the entry will carry." example:"Data migration"`
	StartedAt      string `json:"started_at" format:"date-time" doc:"When it started, by the app's clock." example:"2026-11-17T09:00:00+08:00"`
	ElapsedMinutes int    `json:"elapsed_minutes" doc:"The whole minutes it has run, now." example:"135"`
}

// TimesheetSubmission is a person's week on a project, sent for approval.
type TimesheetSubmission struct {
	ID            string `json:"id" format:"uuid" doc:"The submission's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5101"`
	ProjectID     string `json:"project_id" format:"uuid" doc:"The project." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	ProjectName   string `json:"project_name" doc:"Its name." example:"Acme rollout"`
	UserID        string `json:"user_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	PersonName    string `json:"person_name" doc:"Their name." example:"Priya Nair"`
	WeekStart     string `json:"week_start" format:"date" doc:"The Monday of the week." example:"2026-11-16"`
	Status        string `json:"status" enum:"submitted,approved,returned" doc:"submitted waits for a manager; approved locks its entries; returned sends them back with a comment." example:"submitted"`
	SubmittedAt   string `json:"submitted_at" format:"date-time" doc:"When it was sent." example:"2026-11-20T17:30:00+08:00"`
	DecidedBy     string `json:"decided_by,omitempty" format:"uuid" doc:"Who decided it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e62"`
	DecidedByName string `json:"decided_by_name,omitempty" doc:"Their name." example:"Wei Lim"`
	DecidedAt     string `json:"decided_at,omitempty" format:"date-time" doc:"When." example:"2026-11-23T09:10:00+08:00"`
	Comment       string `json:"comment,omitempty" doc:"What to fix, when it was returned." example:"Add what was done on Wednesday."`
	Minutes       int    `json:"minutes" doc:"The net minutes in it." example:"2250"`
	CanDecide     bool   `json:"can_decide" doc:"Whether the caller may approve or return it now: a submitted week of a project they manage, or any for the Controller, never their own." example:"true"`
	Version       int    `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// Timesheet is a person's week: their entries, the days with what is missing, the projects they can log on, what they have
// submitted and their running timer.
type Timesheet struct {
	WeekStart    string                `json:"week_start" format:"date" doc:"The Monday." example:"2026-11-16"`
	WeekEnd      string                `json:"week_end" format:"date" doc:"The Sunday." example:"2026-11-22"`
	Days         []TimesheetDay        `json:"days" nullable:"false" doc:"The seven days."`
	Entries      []TimeEntry           `json:"entries" nullable:"false" doc:"The week's entries, by day."`
	Submissions  []TimesheetSubmission `json:"submissions" nullable:"false" doc:"What was submitted for the week, newest first."`
	Projects     []TimesheetProject    `json:"projects" nullable:"false" doc:"The projects the person is on, with their tasks in use."`
	Timer        *TimerView            `json:"timer,omitempty" doc:"The running timer, if any."`
	TotalMinutes int                   `json:"total_minutes" doc:"The net minutes of the week." example:"2250"`
	version      string                // the week's ETag, for the answer's header
}

// TimesheetEntryDraft is an entry as saved.
type TimesheetEntryDraft struct {
	ID        string `json:"id,omitempty" format:"uuid" doc:"The ID of an entry that exists, to change it; left out for a new one." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5001"`
	ProjectID string `json:"project_id" format:"uuid" doc:"A project the person is on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	TaskID    string `json:"task_id" format:"uuid" doc:"One of its tasks in use." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e51"`
	WorkDate  string `json:"work_date" format:"date" doc:"A day of the week." example:"2026-11-17"`
	Minutes   int    `json:"minutes" minimum:"1" maximum:"1440" doc:"Whole minutes." example:"135"`
	Billable  *bool  `json:"billable,omitempty" doc:"Left out, as the task is." example:"true"`
	Note      string `json:"note,omitempty" maxLength:"500" doc:"What was done." example:"Migrated the customer tables"`
}

// TimesheetSave is the open entries of a week as saved.
type TimesheetSave struct {
	Entries []TimesheetEntryDraft `json:"entries" maxItems:"400" doc:"Every draft or returned entry of the week as it should be: one with an ID changes that entry, one without is new, and a draft or returned entry left out is removed. Submitted and approved entries, and reversing entries, are not part of it."`
}

// TimesheetSubmit names the projects whose part of the week is submitted.
type TimesheetSubmit struct {
	ProjectIDs []ProjectRef `json:"project_ids,omitempty" maxItems:"100" doc:"The projects to submit; left out, every project with open entries."`
}

// ProjectRef is a project's ID.
type ProjectRef string

// Schema implements huma.SchemaProvider.
func (ProjectRef) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A project's ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"}}
}

// TimerStart starts the timer.
type TimerStart struct {
	ProjectID string `json:"project_id" format:"uuid" doc:"A project the person is on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	TaskID    string `json:"task_id" format:"uuid" doc:"One of its tasks in use." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e51"`
	Billable  *bool  `json:"billable,omitempty" doc:"Left out, as the task is." example:"true"`
	Note      string `json:"note,omitempty" maxLength:"500" doc:"What is being done." example:"Data migration"`
}

// TimesheetDecision is a manager's decision on a week.
type TimesheetDecision struct {
	Comment string `json:"comment,omitempty" maxLength:"1000" doc:"What to fix: required when returning a week, optional when approving." example:"Add what was done on Wednesday."`
}

// TimesheetSubmissionPage is a page of submissions.
type TimesheetSubmissionPage struct {
	Items      []TimesheetSubmission `json:"items" nullable:"false"`
	NextCursor string                `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// TimesheetSubmissionDetail is a submission with the entries in it.
type TimesheetSubmissionDetail struct {
	TimesheetSubmission
	Entries []TimeEntry `json:"entries" nullable:"false" doc:"The entries in it, by day."`
}

type TimesheetPath struct {
	WeekStart string `path:"week_start" format:"date" doc:"The Monday of the week." example:"2026-11-16"`
}

type getTimesheetInput struct{ TimesheetPath }

type timesheetOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The week's version, for If-Match: it changes with every change to the person's entries of the week."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Timesheet
}

type saveTimesheetInput struct {
	TimesheetPath
	IfMatchHeader
	PreferHeader
	Body TimesheetSave
}

type submitTimesheetInput struct {
	TimesheetPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body TimesheetSubmit
}

type timerOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     TimerState
}

// TimerState is whether a timer runs.
type TimerState struct {
	Running bool       `json:"running" doc:"true while a timer runs." example:"true"`
	Timer   *TimerView `json:"timer,omitempty" doc:"The running timer."`
	Entry   *TimeEntry `json:"entry,omitempty" doc:"The draft entry a stopped timer made."`
}

type startTimerInput struct {
	KeyHeader
	PreferHeader
	Body TimerStart
}

type timerActionInput struct {
	KeyHeader
	PreferHeader
}

type reverseEntryInput struct {
	EntryID string `path:"entry_id" format:"uuid" doc:"The approved entry's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5001"`
	KeyHeader
	PreferHeader
}

type reverseEntryOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     TimeEntry
}

type submissionListInput struct {
	PageQuery
	Whose     string `query:"whose" enum:"mine,waiting_for_me,all" default:"mine" doc:"mine: the caller's own weeks; waiting_for_me: submitted weeks the caller may decide; all: every week, for those who see them all." example:"waiting_for_me"`
	ProjectID string `query:"project_id" format:"uuid" doc:"Only this project's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e31"`
	Status    string `query:"status" enum:"submitted,approved,returned" doc:"Only weeks in this status." example:"submitted"`
}

type submissionListOutput struct{ Body TimesheetSubmissionPage }

type SubmissionPath struct {
	SubmissionID string `path:"submission_id" format:"uuid" doc:"The submission's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5101"`
}

type getSubmissionInput struct{ SubmissionPath }

type submissionOutput struct {
	Status  int
	ETag    string `header:"ETag" doc:"The submission's version, for If-Match."`
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    TimesheetSubmissionDetail
}

type decideInput struct {
	SubmissionPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body TimesheetDecision
}

// TimeCodes are the codes a write of time may answer.
var TimeCodes = []string{CodeProjectsOff, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, timesheet.CodeMinutes, timesheet.CodeDay, timesheet.CodeProject, timesheet.CodeTask,
	timesheet.CodeBillable, timesheet.CodeLocked, timesheet.CodeTimer, timesheet.CodeDecide, timesheet.CodeSubmit, timesheet.CodeReverse, storage.CodeTime, storage.CodeRetry,
	storage.CodeLockTimeout}

var weekdayNames = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// timePerson is the caller as a person who logs time: signed in as themselves.
func timePerson(ctx context.Context) (authn.Principal, error) {
	p, _ := authn.From(ctx)
	if p.UserID == uuid.Nil || p.Class != authn.ClassUser || p.AI() {
		return p, problem(CodeClaimNotYours, "Time is a person's own: only a member signed in as themselves logs it.", "Sign in as yourself")
	}
	return p, nil
}

func timeEntryOf(e storage.TimeEntry) TimeEntry {
	out := TimeEntry{ID: e.ID.String(), ProjectID: e.ProjectID.String(), TaskID: e.TaskID.String(), WorkDate: e.WorkDate.Format(time.DateOnly), Minutes: e.Minutes,
		Billable: e.Billable, Note: e.Note, Status: e.Status, Version: e.Version}
	if e.SubmissionID != nil {
		out.SubmissionID = e.SubmissionID.String()
	}
	if e.ReversesID != nil {
		out.ReversesID = e.ReversesID.String()
	}
	return out
}

func timeProblems(p []timesheet.Problem) error {
	if len(p) == 0 {
		return nil
	}
	bad := make([]FieldProblem, 0, len(p))
	for _, x := range p {
		bad = append(bad, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return invalid(bad)
}

func parseWeek(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return t, invalid([]FieldProblem{{Code: timesheet.CodeDay, Field: "path.week_start", Message: "is not a date", Fix: "Use the Monday of the week, such as 2026-11-16"}})
	}
	if t.Weekday() != time.Monday {
		return t, invalid([]FieldProblem{{Code: timesheet.CodeDay, Field: "path.week_start", Message: "is not a Monday", Fix: "Use the first day of the week, a Monday, such as 2026-11-16"}})
	}
	return t, nil
}

// weekETag is the version of a person's week: it changes with any change to their entries of it.
func weekETag(entries []storage.TimeEntry) string {
	sum, latest := 0, int64(0)
	for _, e := range entries {
		sum += e.Version
		if n := e.ChangedAt.UnixNano(); n > latest {
			latest = n
		}
	}
	return etag(len(entries), sum, latest)
}

// holidaysOf are the public holidays of the sg-deadlines version in force, by day (none when no version is).
func holidaysOf(ctx context.Context, u storage.UnitOfWork, today time.Time) (map[time.Time]string, error) {
	v, ok, err := gst.PackOn(ctx, u, "sg-deadlines", today)
	if err != nil || !ok {
		return nil, err
	}
	out := map[time.Time]string{}
	for _, h := range pack.Holidays(v) {
		out[h.On] = h.Name
	}
	return out, nil
}

// localDay is the organisation's calendar day of an instant.
func localDay(ctx context.Context, u storage.UnitOfWork, at time.Time) (time.Time, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := time.LoadLocation(o.Profile.TimeZone)
	if err != nil || o.Profile.TimeZone == "" {
		loc = time.UTC
	}
	y, m, d := at.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
}

// timeKnow reads the projects a person is on, with their tasks, for the checks of their time.
func timeKnow(ctx context.Context, u storage.UnitOfWork, user uuid.UUID) (timesheet.Know, []TimesheetProject, error) {
	k := timesheet.Know{Projects: map[uuid.UUID]timesheet.ProjectFact{}, Tasks: map[uuid.UUID]timesheet.TaskFact{}}
	ids, err := u.Projects().OfUser(ctx, user)
	if err != nil {
		return k, nil, err
	}
	var out []TimesheetProject
	for _, id := range ids {
		p, err := u.Projects().Get(ctx, id)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return k, nil, err
		}
		k.Projects[id] = timesheet.ProjectFact{Name: p.Name, Status: p.Status, Billing: p.BillingMethod, Member: true, Configured: p.Configured}
		ts, err := u.Projects().Tasks(ctx, id)
		if err != nil {
			return k, nil, err
		}
		view := TimesheetProject{ID: id.String(), Code: p.Code, Name: p.Name, Billing: p.BillingMethod, Tasks: []TimesheetTask{}}
		for _, t := range ts {
			k.Tasks[t.ID] = timesheet.TaskFact{ProjectID: id, Name: t.Name, Active: t.Active, Billable: t.Billable}
			if t.Active {
				view.Tasks = append(view.Tasks, TimesheetTask{ID: t.ID.String(), Name: t.Name, Billable: t.Billable})
			}
		}
		if p.Configured && p.Status != storage.ProjectClosed {
			out = append(out, view)
		}
	}
	return k, out, nil
}

// submissionOf is a submission as the caller sees it.
func (a *API) submissionOf(ctx context.Context, u storage.UnitOfWork, p authn.Principal, s storage.Submission, names map[uuid.UUID]string, projects map[uuid.UUID]string, minutes int) TimesheetSubmission {
	out := TimesheetSubmission{ID: s.ID.String(), ProjectID: s.ProjectID.String(), ProjectName: projects[s.ProjectID], UserID: s.UserID.String(), PersonName: names[s.UserID],
		WeekStart: s.WeekStart.Format(time.DateOnly), Status: s.Status, SubmittedAt: s.SubmittedAt.Format(time.RFC3339), Comment: s.Comment, Minutes: minutes, Version: s.Version}
	if s.DecidedBy != nil {
		out.DecidedBy, out.DecidedByName = s.DecidedBy.String(), names[*s.DecidedBy]
	}
	if s.DecidedAt != nil {
		out.DecidedAt = s.DecidedAt.Format(time.RFC3339)
	}
	if s.Status == storage.TimeSubmitted {
		if err := a.mayDecide(ctx, u, p, s); err == nil {
			out.CanDecide = true
		}
	}
	return out
}

// mayDecide says whether the caller may decide a submission now (design D4): never their own; they hold A on "Timesheets and
// project time"; and they manage the project or hold the Controller role. Being named a manager gives no right.
func (a *API) mayDecide(ctx context.Context, u storage.UnitOfWork, p authn.Principal, s storage.Submission) error {
	holdsA := authz.Decide(authz.Policy{Operation: "approve", Capability: authz.Timesheets, Need: authz.Approve}, p.Roles, p.AI()).Allowed
	controller := slices.Contains(p.Roles, authz.Controller)
	manages := false
	if holdsA && !controller {
		ms, err := u.Projects().Members(ctx, s.ProjectID)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if m.UserID == p.UserID && m.Active && m.Manager {
				manages = true
			}
		}
	}
	if pr := timesheet.MayDecide(p.UserID, s.UserID, holdsA, manages, controller); pr != nil {
		return problem(pr.Code, "You may not decide this week: "+pr.Message+".", pr.Fix)
	}
	return nil
}

// timesheetOf builds a person's week.
func (a *API) timesheetOf(ctx context.Context, u storage.UnitOfWork, p authn.Principal, user uuid.UUID, week time.Time) (Timesheet, []storage.TimeEntry, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return Timesheet{}, nil, err
	}
	entries, err := u.Timesheets().Entries(ctx, storage.TimeFilter{UserID: user, From: week, To: week.AddDate(0, 0, 6), Limit: 500})
	if err != nil {
		return Timesheet{}, nil, err
	}
	holidays, err := holidaysOf(ctx, u, today)
	if err != nil {
		return Timesheet{}, nil, err
	}
	_, projects, err := timeKnow(ctx, u, user)
	if err != nil {
		return Timesheet{}, nil, err
	}
	// A person with no project has no working days to miss.
	missing := map[time.Time]bool{}
	if len(projects) > 0 {
		for _, d := range timesheet.Missing(entries, week, today, holidays) {
			missing[d] = true
		}
	}
	net := map[time.Time]int{}
	out := Timesheet{WeekStart: week.Format(time.DateOnly), WeekEnd: week.AddDate(0, 0, 6).Format(time.DateOnly), Days: []TimesheetDay{}, Entries: []TimeEntry{},
		Submissions: []TimesheetSubmission{}, Projects: projects, TotalMinutes: timesheet.Net(entries), version: weekETag(entries)}
	if out.Projects == nil {
		out.Projects = []TimesheetProject{}
	}
	for _, e := range entries {
		net[e.WorkDate] += e.Minutes
		out.Entries = append(out.Entries, timeEntryOf(e))
	}
	for _, d := range timesheet.Days(week) {
		out.Days = append(out.Days, TimesheetDay{Date: d.Format(time.DateOnly), Weekday: weekdayNames[d.Weekday()], Holiday: holidays[d], Missing: missing[d], Minutes: net[d]})
	}
	subs, err := u.Timesheets().Submissions(ctx, storage.SubmissionFilter{UserID: user, Limit: 500})
	if err != nil {
		return out, entries, err
	}
	names, err := memberNames(ctx, u, a.deps.Clock.Now())
	if err != nil {
		return out, entries, err
	}
	pnames := map[uuid.UUID]string{}
	all, err := u.Projects().List(ctx)
	if err != nil {
		return out, entries, err
	}
	for _, x := range all {
		pnames[x.ID] = x.Name
	}
	for _, s := range subs {
		if s.WeekStart.Equal(week) {
			mins := 0
			for _, e := range entries {
				if e.SubmissionID != nil && *e.SubmissionID == s.ID {
					mins += e.Minutes
				}
			}
			out.Submissions = append(out.Submissions, a.submissionOf(ctx, u, p, s, names, pnames, mins))
		}
	}
	if t, err := u.Timesheets().Timer(ctx, user); err == nil {
		v := timerViewOf(t, a.deps.Clock.Now())
		out.Timer = &v
	} else if !errors.Is(err, ports.ErrNotFound) {
		return out, entries, err
	}
	return out, entries, nil
}

func timerViewOf(t storage.Timer, now time.Time) TimerView {
	m, _ := timesheet.TimerMinutes(t.StartedAt, now)
	if m == 0 || now.Before(t.StartedAt) {
		m = 0
	}
	if now.Sub(t.StartedAt) < 30*time.Second {
		m = 0
	}
	return TimerView{ProjectID: t.ProjectID.String(), TaskID: t.TaskID.String(), Billable: t.Billable, Note: t.Note, StartedAt: t.StartedAt.Format(time.RFC3339), ElapsedMinutes: m}
}

func registerTimesheets(a *API) {
	register(a, authz.GetTimesheet, huma.Operation{
		OperationID: "getTimesheet", Method: http.MethodGet, Path: "/v1/timesheet/{week_start}", Tags: []string{"Projects"},
		Summary: "Read the caller's own week of time",
		Description: "The caller's entries of the week from the Monday given (FR-PRJ-05), the days with the public holidays and the working days nobody entered " +
			"time on, the projects the caller is on with their tasks in use, what was submitted for the week, and the running timer. It is always the " +
			"caller's own week. The ETag changes with any change to it. Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeProjectsOff, CodeClaimNotYours, timesheet.CodeDay}, func(ctx context.Context, in *getTimesheetInput) (*timesheetOutput, error) {
		p, err := timePerson(ctx)
		if err != nil {
			return nil, err
		}
		week, err := parseWeek(in.WeekStart)
		if err != nil {
			return nil, err
		}
		out := timesheetOutput{Status: http.StatusOK}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			body, entries, err := a.timesheetOf(ctx, u, p, p.UserID, week)
			out.Body, out.ETag = body, weekETag(entries)
			return err
		})
		return &out, err
	})

	register(a, authz.SaveTimesheet, huma.Operation{
		OperationID: "saveTimesheet", Method: http.MethodPut, Path: "/v1/timesheet/{week_start}", Tags: []string{"Projects"},
		Summary: "Save the caller's week of time",
		Description: "Saves the draft and returned entries of the caller's week (FR-PRJ-05): an entry with an ID changes that entry, one without is new, and an " +
			"open entry left out is removed. Time is whole minutes, 1 to 1,440, on a project the caller is on and one of its tasks in use; time on an " +
			"internal project is not billable; an entry stays on its project. Submitted and approved entries never change: correct an approved one with " +
			"reverseTimeEntry. Every problem comes back together. Needs If-Match with the week's ETag, so what is saved is what the caller saw. Side " +
			"effects: the entries, and one audit event for the week. Refused with PRJ-09 while Projects and time are switched off. Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeClaimNotYours, storage.CodeVersion}, TimeCodes...),
		func(ctx context.Context, in *saveTimesheetInput) (*timesheetOutput, error) {
			p, err := timePerson(ctx)
			if err != nil {
				return nil, err
			}
			week, err := parseWeek(in.WeekStart)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Timesheet, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return Timesheet{}, err
				}
				current, err := u.Timesheets().Entries(ctx, storage.TimeFilter{UserID: p.UserID, From: week, To: week.AddDate(0, 0, 6), Limit: 500})
				if err != nil {
					return Timesheet{}, err
				}
				if err := checkIfMatch(in.IfMatch, weekETag(current)); err != nil {
					return Timesheet{}, err
				}
				k, _, err := timeKnow(ctx, u, p.UserID)
				if err != nil {
					return Timesheet{}, err
				}
				open := map[uuid.UUID]storage.TimeEntry{}
				for _, e := range current {
					if (e.Status == storage.TimeDraft || e.Status == storage.TimeReturned) && e.ReversesID == nil {
						open[e.ID] = e
					}
				}
				var entries []timesheet.Entry
				var probs []FieldProblem
				for i, d := range in.Body.Entries {
					e := timesheet.Entry{Minutes: d.Minutes, Note: strings.TrimSpace(d.Note)}
					bad := func(field, msg string) {
						probs = append(probs, FieldProblem{Code: CodeInvalid, Field: "body.entries[" + strconv.Itoa(i) + "]." + field, Message: msg, Fix: "Send an ID"})
					}
					var perr error
					if d.ID != "" {
						if e.ID, perr = uuid.Parse(d.ID); perr != nil {
							bad("id", "is not an ID")
						}
					}
					if e.ProjectID, perr = uuid.Parse(d.ProjectID); perr != nil {
						bad("project_id", "is not an ID")
					}
					if e.TaskID, perr = uuid.Parse(d.TaskID); perr != nil {
						bad("task_id", "is not an ID")
					}
					if e.Day, perr = time.Parse(time.DateOnly, d.WorkDate); perr != nil {
						probs = append(probs, FieldProblem{Code: timesheet.CodeDay, Field: "body.entries[" + strconv.Itoa(i) + "].work_date", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
					}
					e.Billable = billableOr(k, e.ProjectID, e.TaskID, d.Billable)
					entries = append(entries, e)
				}
				if len(probs) > 0 {
					return Timesheet{}, invalid(probs)
				}
				if err := timeProblems(timesheet.CheckWeek(entries, week, k, open)); err != nil {
					return Timesheet{}, err
				}
				var edits storage.TimeWeekEdits
				seen := map[uuid.UUID]bool{}
				for _, e := range entries {
					if e.ID == uuid.Nil {
						edits.Add = append(edits.Add, storage.NewTimeEntry{UserID: p.UserID, ProjectID: e.ProjectID, TaskID: e.TaskID, WorkDate: e.Day, Minutes: e.Minutes,
							Billable: e.Billable, Note: e.Note})
						continue
					}
					seen[e.ID] = true
					old := open[e.ID]
					if old.TaskID != e.TaskID || !old.WorkDate.Equal(e.Day) || old.Minutes != e.Minutes || old.Billable != e.Billable || old.Note != e.Note {
						edits.Change = append(edits.Change, storage.TimeEdit{ID: e.ID, Version: old.Version, TaskID: e.TaskID, WorkDate: e.Day, Minutes: e.Minutes, Billable: e.Billable, Note: e.Note})
					}
				}
				for id, old := range open {
					if !seen[id] {
						edits.Remove = append(edits.Remove, storage.TimeRef{ID: id, Version: old.Version})
					}
				}
				if _, err := u.Timesheets().SaveWeek(ctx, p.UserID, week, edits); err != nil {
					return Timesheet{}, err
				}
				body, _, err := a.timesheetOf(ctx, u, p, p.UserID, week)
				return body, err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &timesheetOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &timesheetOutput{Status: w.Status, ETag: w.Body.version, Body: w.Body}, nil
		})
}

// billableOr is whether an entry is billable: as said, else as its task is, and never on an internal project.
func billableOr(k timesheet.Know, project, task uuid.UUID, said *bool) bool {
	if said != nil {
		return *said
	}
	t, ok := k.Tasks[task]
	return ok && t.Billable && k.Projects[project].Billing != storage.BillingInternal
}
