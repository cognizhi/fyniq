package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/timesheet"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// seesAllTime says the caller reads every submitted week: any right on "Timesheets and project time" without a qualifier. The
// Employee's "C (own)" and the Approver's "A (project manager)" read their own and the weeks of the projects they manage.
func seesAllTime(p authn.Principal) bool {
	return authz.Decide(authz.Policy{Operation: "view", Capability: authz.Timesheets, Need: authz.View, Unqualified: true}, p.Roles, p.AI()).Allowed
}

// managed are the projects a person manages.
func managedProjects(ctx context.Context, u storage.UnitOfWork, user uuid.UUID) (map[uuid.UUID]bool, error) {
	ids, err := u.Projects().OfUser(ctx, user)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		ms, err := u.Projects().Members(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if m.UserID == user && m.Active && m.Manager {
				out[id] = true
			}
		}
	}
	return out, nil
}

func (a *API) submissionDetail(ctx context.Context, u storage.UnitOfWork, p authn.Principal, s storage.Submission) (TimesheetSubmissionDetail, error) {
	names, err := memberNames(ctx, u, a.deps.Clock.Now())
	if err != nil {
		return TimesheetSubmissionDetail{}, err
	}
	pr, err := u.Projects().Get(ctx, s.ProjectID)
	if err != nil {
		return TimesheetSubmissionDetail{}, err
	}
	es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{SubmissionID: s.ID, Limit: 500})
	if err != nil {
		return TimesheetSubmissionDetail{}, err
	}
	out := TimesheetSubmissionDetail{Entries: []TimeEntry{}}
	for _, e := range es {
		out.Entries = append(out.Entries, timeEntryOf(e))
	}
	out.TimesheetSubmission = a.submissionOf(ctx, u, p, s, names, map[uuid.UUID]string{s.ProjectID: pr.Name}, timesheet.Net(es))
	return out, nil
}

func registerTimesheetOps(a *API) {
	register(a, authz.SubmitTimesheet, huma.Operation{
		OperationID: "submitTimesheet", Method: http.MethodPost, Path: "/v1/timesheet/{week_start}/submit", Tags: []string{"Projects"},
		Summary: "Submit the caller's week for approval", DefaultStatus: http.StatusOK,
		Description: "Sends the caller's draft and returned entries of the week, each project's part as one submission to that project's managers (FR-PRJ-07); " +
			"the projects to send can be named. TIME-09 when there is nothing to send. Needs If-Match with the week's ETag. Side effects: a " +
			"submission for each project, its entries marked submitted and locked, and an audit event for the week. Refused with PRJ-09 while " +
			"Projects and time are switched off. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeClaimNotYours, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeVersion}, TimeCodes...),
		func(ctx context.Context, in *submitTimesheetInput) (*timesheetOutput, error) {
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
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Timesheet, error) {
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
				want := map[uuid.UUID]bool{}
				for _, id := range in.Body.ProjectIDs {
					pid, err := uuid.Parse(string(id))
					if err != nil {
						return Timesheet{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.project_ids", Message: "holds something that is not an ID", Fix: "Send projects' IDs"}})
					}
					want[pid] = true
				}
				var projects []uuid.UUID
				for _, e := range current {
					if (e.Status == storage.TimeDraft || e.Status == storage.TimeReturned) && (len(want) == 0 || want[e.ProjectID]) && !slices.Contains(projects, e.ProjectID) {
						projects = append(projects, e.ProjectID)
					}
				}
				if len(projects) == 0 {
					return Timesheet{}, problem(timesheet.CodeSubmit, "There is nothing to submit for this week.", "Enter the time first, then submit the week")
				}
				for _, pid := range projects {
					if _, _, err := u.Timesheets().Submit(ctx, p.UserID, pid, week); err != nil {
						return Timesheet{}, err
					}
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
			return &timesheetOutput{Status: w.Status, ETag: w.Body.version, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetTimer, huma.Operation{
		OperationID: "getTimer", Method: http.MethodGet, Path: "/v1/timer", Tags: []string{"Projects"},
		Summary: "Read the caller's running timer",
		Description: "Whether the caller's timer runs, on what, since when and for how many whole minutes (FR-PRJ-05). Refused with PRJ-09 while Projects and time " +
			"are switched off. Read-only; no side effects.",
	}, []string{CodeProjectsOff, CodeClaimNotYours}, func(ctx context.Context, _ *struct{}) (*timerOutput, error) {
		p, err := timePerson(ctx)
		if err != nil {
			return nil, err
		}
		out := timerOutput{Status: http.StatusOK}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			t, err := u.Timesheets().Timer(ctx, p.UserID)
			if errors.Is(err, ports.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			v := timerViewOf(t, a.deps.Clock.Now())
			out.Body = TimerState{Running: true, Timer: &v}
			return nil
		})
		return &out, err
	})

	register(a, authz.StartTimer, huma.Operation{
		OperationID: "startTimer", Method: http.MethodPost, Path: "/v1/timer/start", Tags: []string{"Projects"},
		Summary: "Start the caller's timer", DefaultStatus: http.StatusOK,
		Description: "Starts a timer on a project the caller is on and one of its tasks in use, at the app's clock (FR-PRJ-05); one runs at a time. Side effects: " +
			"the timer and its audit event. Refused with PRJ-09 while Projects and time are switched off. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeClaimNotYours, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, TimeCodes...), func(ctx context.Context, in *startTimerInput) (*timerOutput, error) {
		p, err := timePerson(ctx)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TimerState, error) {
			if err := requireProjects(ctx, a, u); err != nil {
				return TimerState{}, err
			}
			pid, perr := uuid.Parse(in.Body.ProjectID)
			tid, terr := uuid.Parse(in.Body.TaskID)
			if perr != nil || terr != nil {
				return TimerState{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.project_id", Message: "and task_id are IDs", Fix: "Send the project's and the task's IDs"}})
			}
			k, _, err := timeKnow(ctx, u, p.UserID)
			if err != nil {
				return TimerState{}, err
			}
			bill := billableOr(k, pid, tid, in.Body.Billable)
			now := a.deps.Clock.Now()
			day, err := localDay(ctx, u, now)
			if err != nil {
				return TimerState{}, err
			}
			week := timesheet.Week(day)
			e := timesheet.Entry{ProjectID: pid, TaskID: tid, Day: day, Minutes: 1, Billable: bill}
			if err := timeProblems(timesheet.CheckWeek([]timesheet.Entry{e}, week, k, nil)); err != nil {
				return TimerState{}, err
			}
			t := storage.Timer{UserID: p.UserID, ProjectID: pid, TaskID: tid, Billable: bill, Note: strings.TrimSpace(in.Body.Note), StartedAt: now}
			if err := u.Timesheets().StartTimer(ctx, t); err != nil {
				return TimerState{}, err
			}
			v := timerViewOf(t, now)
			return TimerState{Running: true, Timer: &v}, nil
		})
		if err != nil {
			return nil, err
		}
		return &timerOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.StopTimer, huma.Operation{
		OperationID: "stopTimer", Method: http.MethodPost, Path: "/v1/timer/stop", Tags: []string{"Projects"},
		Summary: "Stop the caller's timer into a draft entry", DefaultStatus: http.StatusOK,
		Description: "Stops the timer and adds its time as a draft entry on the day it started: the nearest whole minute, half up, at least one (RND-TIME), at most a " +
			"day (TIME-07 beyond that: drop the timer and enter the time by hand). Side effects: the entry, and the audit events. Refused with PRJ-09 while Projects " +
			"and time are switched off. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeClaimNotYours, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, TimeCodes...), func(ctx context.Context, in *timerActionInput) (*timerOutput, error) {
		p, err := timePerson(ctx)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, struct{}{}, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TimerState, error) {
			if err := requireProjects(ctx, a, u); err != nil {
				return TimerState{}, err
			}
			t, err := u.Timesheets().Timer(ctx, p.UserID)
			if err != nil {
				return TimerState{}, err
			}
			minutes, pr := timesheet.TimerMinutes(t.StartedAt, a.deps.Clock.Now())
			if pr != nil {
				return TimerState{}, problem(pr.Code, "The timer "+pr.Message+".", pr.Fix)
			}
			day, err := localDay(ctx, u, t.StartedAt)
			if err != nil {
				return TimerState{}, err
			}
			e, err := u.Timesheets().StopTimer(ctx, p.UserID, day, minutes)
			if err != nil {
				return TimerState{}, err
			}
			v := timeEntryOf(e)
			return TimerState{Entry: &v}, nil
		})
		if err != nil {
			return nil, err
		}
		return &timerOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.DropTimer, huma.Operation{
		OperationID: "dropTimer", Method: http.MethodPost, Path: "/v1/timer/drop", Tags: []string{"Projects"},
		Summary: "Drop the caller's timer without an entry", DefaultStatus: http.StatusOK,
		Description: "Ends the timer and keeps nothing of it (FR-PRJ-05). Side effects: the audit event. Refused with PRJ-09 while Projects and time are switched off. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeClaimNotYours, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, TimeCodes...), func(ctx context.Context, in *timerActionInput) (*timerOutput, error) {
		p, err := timePerson(ctx)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, struct{}{}, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TimerState, error) {
			if err := requireProjects(ctx, a, u); err != nil {
				return TimerState{}, err
			}
			return TimerState{}, u.Timesheets().DropTimer(ctx, p.UserID)
		})
		if err != nil {
			return nil, err
		}
		return &timerOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ReverseTimeEntry, huma.Operation{
		OperationID: "reverseTimeEntry", Method: http.MethodPost, Path: "/v1/time-entries/{entry_id}/reverse", Tags: []string{"Projects"},
		Summary: "Reverse an approved entry of the caller's", DefaultStatus: http.StatusCreated,
		Description: "Corrects an approved entry the way posted history is corrected (FR-PRJ-05, UC-PRJ-01): a draft reversing entry with the opposite minutes, once, " +
			"which the caller submits with the week; they then enter the right time as a new entry, and the manager approves both again. The approved entry never " +
			"changes. TIME-10 when the entry is not the caller's, not approved, itself a reversal, or already reversed. Side effects: the entry and its audit event. " +
			"Refused with PRJ-09 while Projects and time are switched off. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeClaimNotYours, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, TimeCodes...), func(ctx context.Context, in *reverseEntryInput) (*reverseEntryOutput, error) {
		p, err := timePerson(ctx)
		if err != nil {
			return nil, err
		}
		id, err := parseID("entry_id", in.EntryID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, id, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (TimeEntry, error) {
			if err := requireProjects(ctx, a, u); err != nil {
				return TimeEntry{}, err
			}
			e, err := u.Timesheets().GetEntry(ctx, id)
			if err != nil {
				return TimeEntry{}, err
			}
			if e.UserID != p.UserID {
				return TimeEntry{}, ports.ErrNotFound
			}
			if e.Status != storage.TimeApproved || e.ReversesID != nil {
				return TimeEntry{}, problem(timesheet.CodeReverse, "Only an approved entry is reversed, and a reversal is not.", "Change a draft or returned entry in the grid instead")
			}
			all, err := u.Timesheets().Entries(ctx, storage.TimeFilter{UserID: p.UserID, ProjectID: e.ProjectID, From: e.WorkDate, To: e.WorkDate, Limit: 500})
			if err != nil {
				return TimeEntry{}, err
			}
			if slices.ContainsFunc(all, func(x storage.TimeEntry) bool { return x.ReversesID != nil && *x.ReversesID == e.ID }) {
				return TimeEntry{}, problem(timesheet.CodeReverse, "This entry is reversed already.", "Enter the right time as a new entry")
			}
			r, err := u.Timesheets().Reverse(ctx, id)
			return timeEntryOf(r), err
		})
		if err != nil {
			return nil, err
		}
		return &reverseEntryOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ListTimesheetSubmissions, huma.Operation{
		OperationID: "listTimesheetSubmissions", Method: http.MethodGet, Path: "/v1/timesheet-submissions", Tags: []string{"Projects"},
		Summary: "List submitted weeks",
		Description: "Each person's week on a project that was sent for approval (FR-PRJ-07), newest week first, a page at a time: the caller's own (the default), those " +
			"the caller may decide now (waiting_for_me), or everyone's. A person who reads only their own and their projects' weeks (an Employee, the Approver) is " +
			"answered those whatever they ask for. Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeCursor, CodeProjectsOff, CodeClaimNotYours}, func(ctx context.Context, in *submissionListInput) (*submissionListOutput, error) {
		p, _ := authn.From(ctx)
		whose := cmpOr(in.Whose, "mine")
		out := submissionListOutput{Body: TimesheetSubmissionPage{Items: []TimesheetSubmission{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			if !seesAllTime(p) && whose == "all" {
				whose = "waiting_for_me"
			}
			f := storage.SubmissionFilter{Limit: 500, Status: in.Status}
			if in.ProjectID != "" {
				id, err := parseID("project_id", in.ProjectID)
				if err != nil {
					return err
				}
				f.ProjectID = id
			}
			if whose == "mine" {
				f.UserID = p.UserID
			}
			if whose == "waiting_for_me" {
				f.Status = storage.TimeSubmitted
			}
			subs, err := u.Timesheets().Submissions(ctx, f)
			if err != nil {
				return err
			}
			managed, err := managedProjects(ctx, u, p.UserID)
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			all, err := u.Projects().List(ctx)
			if err != nil {
				return err
			}
			pnames := map[uuid.UUID]string{}
			for _, x := range all {
				pnames[x.ID] = x.Name
			}
			var shown []storage.Submission
			for _, s := range subs {
				mine := s.UserID == p.UserID
				switch {
				case whose == "waiting_for_me":
					if a.mayDecide(ctx, u, p, s) == nil {
						shown = append(shown, s)
					}
				case seesAllTime(p) || mine || managed[s.ProjectID]:
					shown = append(shown, s)
				}
			}
			from, to, next, err := pageAt(len(shown), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, s := range shown[from:to] {
				es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{SubmissionID: s.ID, Limit: 500})
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, a.submissionOf(ctx, u, p, s, names, pnames, timesheet.Net(es)))
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.GetTimesheetSubmission, huma.Operation{
		OperationID: "getTimesheetSubmission", Method: http.MethodGet, Path: "/v1/timesheet-submissions/{submission_id}", Tags: []string{"Projects"},
		Summary: "Read a submitted week with its entries",
		Description: "A submission and the entries in it, with its ETag and whether the caller may decide it now (FR-PRJ-07). A person who may not see it is answered not " +
			"found. Refused with PRJ-09 while Projects and time are switched off. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeProjectsOff}, func(ctx context.Context, in *getSubmissionInput) (*submissionOutput, error) {
		id, err := parseID("submission_id", in.SubmissionID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := submissionOutput{Status: http.StatusOK}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := requireProjects(ctx, a, u); err != nil {
				return err
			}
			s, err := a.visibleSubmission(ctx, u, p, id)
			if err != nil {
				return err
			}
			out.Body, err = a.submissionDetail(ctx, u, p, s)
			out.ETag = etag(s.Version, s.Status)
			return err
		})
		return &out, err
	})

	decide := func(pol authz.Policy, id, summary, desc string, approve bool) {
		register(a, pol, huma.Operation{
			OperationID: id, Method: http.MethodPost, Path: "/v1/timesheet-submissions/{submission_id}/" + map[bool]string{true: "approve", false: "return"}[approve], Tags: []string{"Projects"},
			Summary: summary, DefaultStatus: http.StatusOK, Description: desc,
		}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeVersion}, TimeCodes...), func(ctx context.Context, in *decideInput) (*submissionOutput, error) {
			sid, err := parseID("submission_id", in.SubmissionID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, perr := timePerson(ctx)
			if perr != nil {
				return nil, perr
			}
			comment := strings.TrimSpace(in.Body.Comment)
			if !approve && comment == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.comment", Message: "is empty: a returned week says what to fix", Fix: "Say what the person should fix"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TimesheetSubmissionDetail, error) {
				if err := requireProjects(ctx, a, u); err != nil {
					return TimesheetSubmissionDetail{}, err
				}
				s, err := a.visibleSubmission(ctx, u, p, sid)
				if err != nil {
					return TimesheetSubmissionDetail{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(s.Version, s.Status)); err != nil {
					return TimesheetSubmissionDetail{}, err
				}
				if s.Status != storage.TimeSubmitted {
					return TimesheetSubmissionDetail{}, problem(timesheet.CodeDecide, "This week was decided already.", "Open it to see who decided it; a correction is a reversing entry")
				}
				if err := a.mayDecide(ctx, u, p, s); err != nil {
					return TimesheetSubmissionDetail{}, err
				}
				done, err := u.Timesheets().Decide(ctx, sid, s.Version, p.UserID, approve, comment, a.deps.Clock.Now())
				if err != nil {
					return TimesheetSubmissionDetail{}, err
				}
				return a.submissionDetail(ctx, u, p, done)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &submissionOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &submissionOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Body: w.Body}, nil
		})
	}
	decide(authz.ApproveTimesheetSubmission, "approveTimesheetSubmission", "Approve a submitted week",
		"Approves a person's week on a project (FR-PRJ-07), which locks its entries (FR-PRJ-05). The caller holds A on \"Timesheets and project time\" (the Approver or the "+
			"Controller), manages the project or is the Controller, and is not the person: TIME-08 otherwise, and the database refuses it too. Needs If-Match with the "+
			"submission's ETag. Side effects: the submission and its entries approved, and an audit event for the week. Time posts nothing. Refused with PRJ-09 while "+
			"Projects and time are switched off. Prefer: return=preview checks it and saves nothing.", true)
	decide(authz.ReturnTimesheetSubmission, "returnTimesheetSubmission", "Return a submitted week with a comment",
		"Sends a person's week back (FR-PRJ-07) with what to fix; its entries are theirs to change again and to submit anew, and each version is kept. The same "+
			"rules as approving it. Needs If-Match with the submission's ETag. Side effects: the submission and its entries returned, and an audit event for the week. "+
			"Refused with PRJ-09 while Projects and time are switched off. Prefer: return=preview checks it and saves nothing.", false)
}

// visibleSubmission reads a submission the caller may see: their own, a project they manage, or any for those who see every week;
// not found for another.
func (a *API) visibleSubmission(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Submission, error) {
	s, err := u.Timesheets().GetSubmission(ctx, id)
	if err != nil {
		return s, err
	}
	if seesAllTime(p) || s.UserID == p.UserID {
		return s, nil
	}
	managed, err := managedProjects(ctx, u, p.UserID)
	if err != nil {
		return s, err
	}
	if !managed[s.ProjectID] {
		return storage.Submission{}, ports.ErrNotFound
	}
	return s, nil
}
