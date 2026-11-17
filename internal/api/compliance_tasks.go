package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Compliance tasks (PRD-001 AI-65; p1-scheduled-agents design D7): what a person must do about a finding of a scheduled
// agent's run, or what an approved `sec.create_task` proposal made, one open per finding. A person marks one done, or
// dismisses it with a reason; nothing else changes them here: the run updates and clears its own.

// CodeTaskReason is a dismissal with no reason.
const CodeTaskReason = "TASK-01"

// ComplianceTask is a task as the Inbox and the company secretary's page show it.
type ComplianceTask struct {
	ID         string `json:"id" format:"uuid" example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c11"`
	FindingKey string `json:"finding_key" doc:"The finding it is about: one open task per finding." example:"calendar:gst_f5:2026-12-31"`
	Title      string `json:"title" example:"File the GST return for October to December"`
	Detail     string `json:"detail,omitempty" doc:"What to do, why and by when, in the platform's words." example:"Due by 31 January 2027 (sg-gst@2026.1)."`
	OwnerRole  string `json:"owner_role,omitempty" doc:"The role that owns it, when no member does." example:"owner"`
	OwnerUser  string `json:"owner_user,omitempty" format:"uuid" doc:"The member who owns it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	DueOn      string `json:"due_on,omitempty" format:"date" example:"2027-01-31"`
	Escalated  bool   `json:"escalated" doc:"Whether the Owner is told too, as its deadline nears (AI-62)." example:"false"`
	Source     string `json:"source" enum:"agent_run,ai_proposal" doc:"What raised it: a run's finding, or an approved proposal." example:"agent_run"`
	RunID      string `json:"run_id,omitempty" format:"uuid" doc:"The run whose finding raised it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c12"`
	ProposalID string `json:"proposal_id,omitempty" format:"uuid" doc:"The approved proposal that made it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	Status     string `json:"status" enum:"open,done,dismissed,cleared" doc:"open; done or dismissed by a person; cleared when its finding cleared." example:"open"`
	Reason     string `json:"reason,omitempty" doc:"Why it was dismissed." example:"Filed by the tax agent on 20 January"`
	CreatedAt  string `json:"created_at" format:"date-time" example:"2026-11-15T23:00:00Z"`
	ClosedAt   string `json:"closed_at,omitempty" format:"date-time" example:"2026-11-20T02:00:00Z"`
	ClosedBy   string `json:"closed_by,omitempty" format:"uuid" doc:"Who marked it done or dismissed it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	Version    int    `json:"version" example:"1"`
}

// ComplianceTaskPage is a page of tasks, newest first.
type ComplianceTaskPage struct {
	Items      []ComplianceTask `json:"items" nullable:"false"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6IjAxOTMifQ"`
}

// TaskDismissal is why a task is dismissed.
type TaskDismissal struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it needs nothing done, for the record." example:"Filed by the tax agent on 20 January"`
}

type (
	complianceTaskListInput struct {
		PageQuery
		Status string `query:"status" enum:"open,done,dismissed,cleared" doc:"Only tasks in this state; leave it out for all." example:"open"`
	}
	complianceTaskListOutput struct{ Body ComplianceTaskPage }
	// ComplianceTaskPath names a task.
	ComplianceTaskPath struct {
		TaskID string `path:"task_id" format:"uuid" doc:"The task's ID." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c11"`
	}
	complianceTaskGetInput struct{ ComplianceTaskPath }
	complianceTaskOutput   struct {
		ETag string `header:"ETag" doc:"The task's version, for If-Match."`
		Body ComplianceTask
	}
	complianceTaskDoneInput struct {
		ComplianceTaskPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	complianceTaskDismissInput struct {
		ComplianceTaskPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body TaskDismissal
	}
	complianceTaskWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The task's version after the command, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ComplianceTask
	}
)

func registerComplianceTasks(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := []string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.ListComplianceTasks, huma.Operation{
		OperationID: "listComplianceTasks", Method: http.MethodGet, Path: "/v1/compliance/tasks", Tags: []string{"Compliance"},
		Summary: "List the compliance tasks",
		Description: "The tasks the scheduled agents' findings and their approved proposals raised (AI-65), newest first, a page at a time, " +
			"filtered by state: one open task per finding. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *complianceTaskListInput) (*complianceTaskListOutput, error) {
		p, _ := authn.From(ctx)
		after := uuid.Nil
		if in.Cursor != "" {
			c, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if after, err = uuid.Parse(c); err != nil {
				return nil, badCursor()
			}
		}
		var statuses []string
		if in.Status != "" {
			statuses = []string{in.Status}
		}
		out := ComplianceTaskPage{Items: []ComplianceTask{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			list, err := u.ComplianceTasks().List(ctx, storage.ComplianceTaskQuery{Statuses: statuses, After: after, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(list) > in.PageSize {
				list = list[:in.PageSize]
				out.NextCursor = encodeCursor(list[len(list)-1].ID.String())
			}
			for _, t := range list {
				out.Items = append(out.Items, complianceTaskOf(t))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &complianceTaskListOutput{Body: out}, nil
	})

	register(a, authz.GetComplianceTask, huma.Operation{
		OperationID: "getComplianceTask", Method: http.MethodGet, Path: "/v1/compliance/tasks/{task_id}", Tags: []string{"Compliance"},
		Summary: "Read a compliance task",
		Description: "A task with its finding, owner, due date, what raised it and what became of it, and its ETag for If-Match (AI-65). " +
			"Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *complianceTaskGetInput) (*complianceTaskOutput, error) {
		p, _ := authn.From(ctx)
		id, err := uuid.Parse(in.TaskID)
		if err != nil {
			return nil, taskNotFound()
		}
		var t storage.ComplianceTask
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			t, err = u.ComplianceTasks().Get(ctx, id)
			return err
		})
		if errors.Is(err, ports.ErrNotFound) {
			return nil, taskNotFound()
		}
		if err != nil {
			return nil, err
		}
		return &complianceTaskOutput{ETag: etag(t.Version), Body: complianceTaskOf(t)}, nil
	})

	register(a, authz.CompleteComplianceTask, huma.Operation{
		OperationID: "completeComplianceTask", Method: http.MethodPost, Path: "/v1/compliance/tasks/{task_id}/done", Tags: []string{"Compliance"},
		Summary: "Mark a compliance task done",
		Description: "Marks an open task done, as you, over the version you read (If-Match). It records that you did what the task asks; " +
			"it never records a filing or marks a statutory obligation met, which the calendar's own record does (FR-TAX-07). Side effects: " +
			"the task's change and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeSessionRequired, CodeIfMatchRequired, CodeStale, storage.CodeVersion, storage.CodeComplianceTask}, writes...), locks...),
		func(ctx context.Context, in *complianceTaskDoneInput) (*complianceTaskWriteOutput, error) {
			return a.closeTask(ctx, in.TaskID, in.IdempotencyKey, in.Prefer, in, in.IfMatch, storage.TaskDone, "")
		})

	register(a, authz.DismissComplianceTask, huma.Operation{
		OperationID: "dismissComplianceTask", Method: http.MethodPost, Path: "/v1/compliance/tasks/{task_id}/dismiss", Tags: []string{"Compliance"},
		Summary: "Dismiss a compliance task",
		Description: "Dismisses an open task, as you, with why it needs nothing done, over the version you read (If-Match). A finding that " +
			"returns raises a new task. Side effects: the task's change and its audit event. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeSessionRequired, CodeIfMatchRequired, CodeStale, CodeTaskReason, storage.CodeVersion, storage.CodeComplianceTask}, writes...), locks...),
		func(ctx context.Context, in *complianceTaskDismissInput) (*complianceTaskWriteOutput, error) {
			if strings.TrimSpace(in.Body.Reason) == "" {
				return nil, problem(CodeTaskReason, "A dismissal needs a reason.", "Say why the task needs nothing done",
					FieldProblem{Code: CodeTaskReason, Field: "body.reason", Message: "is empty", Fix: "Say why"})
			}
			return a.closeTask(ctx, in.TaskID, in.IdempotencyKey, in.Prefer, in, in.IfMatch, storage.TaskDismissed, strings.TrimSpace(in.Body.Reason))
		})
}

// closeTask marks an open task done or dismissed, for the person signed in, over the version they read.
func (a *API) closeTask(ctx context.Context, taskID, key, prefer string, in any, ifMatch, status, reason string) (*complianceTaskWriteOutput, error) {
	p, _ := authn.From(ctx)
	if p.AI() || p.UserID == uuid.Nil {
		return nil, problem(CodeSessionRequired, "Closing a task needs a person with a Fyniq user, who closes it by name.",
			"Sign in to Fyniq in the browser and do it there")
	}
	id, err := uuid.Parse(taskID)
	if err != nil {
		return nil, taskNotFound()
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	w, err := write(ctx, a, key, prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ComplianceTask, error) {
		t, err := u.ComplianceTasks().Get(ctx, id)
		if err != nil {
			return ComplianceTask{}, err
		}
		if err := checkIfMatch(ifMatch, etag(t.Version)); err != nil {
			return ComplianceTask{}, err
		}
		if t.Status != storage.TaskOpen {
			return ComplianceTask{}, problem(storage.CodeComplianceTask, "The task is "+t.Status+" already.",
				"Nothing to do: a finding that returns raises a new task")
		}
		out, err := u.ComplianceTasks().Close(ctx, t.ID, t.Version, status, reason, p.UserID, a.deps.Clock.Now())
		return complianceTaskOf(out), err
	})
	if errors.Is(err, ports.ErrNotFound) {
		return nil, taskNotFound()
	}
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &complianceTaskWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	}
	return &complianceTaskWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}

func taskNotFound() error {
	return problem(CodeNotFound, "There is no such task.", "Choose one of the tasks GET /v1/compliance/tasks lists")
}

func complianceTaskOf(t storage.ComplianceTask) ComplianceTask {
	out := ComplianceTask{ID: t.ID.String(), FindingKey: t.FindingKey, Title: t.Title, Detail: t.Detail, OwnerRole: t.OwnerRole, Escalated: t.Escalated,
		Source: t.Source, Status: t.Status, Reason: t.Reason, CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339), Version: t.Version}
	if t.OwnerUser != nil {
		out.OwnerUser = t.OwnerUser.String()
	}
	if t.DueOn != nil {
		out.DueOn = t.DueOn.Format(time.DateOnly)
	}
	if t.Run != nil {
		out.RunID = t.Run.String()
	}
	if t.Proposal != nil {
		out.ProposalID = t.Proposal.String()
	}
	if t.ClosedAt != nil {
		out.ClosedAt = t.ClosedAt.UTC().Format(time.RFC3339)
	}
	if t.ClosedBy != nil {
		out.ClosedBy = t.ClosedBy.String()
	}
	return out
}
