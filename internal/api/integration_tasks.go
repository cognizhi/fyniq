package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Tasks made by integrations (PRD-001 FR-INT-24, rev 0.610; p2-api-third-parties task 2, design D5). A ticketing system or another API client holding the scope
// fyniq.write.tasks asks a person to do something: it makes tasks of its own, with a reference it chooses, and updates only those, finishing one once (done or
// cancelled). A task is not a compliance task (AI-65): no operation here reads, closes, dismisses or edits a statutory or agent task, and the client's database role
// cannot write that table. A person who may view the row (Appendix D) reads every client's tasks; a client reads only its own. Never the AI, never the MCP surface.

// The refusals of an integration's task (ENG-23).
const (
	CodeIntegrationTaskClient   = "ITASK-01" // the caller is not an integration API client
	CodeIntegrationTaskFinished = "ITASK-02" // the task is finished already
)

// IntegrationTaskCodes are the refusals of the operations that make or update a task.
var IntegrationTaskCodes = []string{CodeIntegrationTaskClient, CodeIntegrationTaskFinished}

// IntegrationTask is a task an API client made.
type IntegrationTask struct {
	ID          string `json:"id" format:"uuid" example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c21"`
	ExternalRef string `json:"external_ref" doc:"The client's own reference, such as the ticket key." example:"OPS-142"`
	Title       string `json:"title" example:"Chase the signed engagement letter"`
	Detail      string `json:"detail,omitempty" example:"Raised from OPS-142."`
	DueOn       string `json:"due_on,omitempty" format:"date" example:"2026-12-15"`
	LinkURL     string `json:"link_url,omitempty" format:"uri" doc:"Where the ticket is." example:"https://example.atlassian.net/browse/OPS-142"`
	Status      string `json:"status" enum:"open,done,cancelled" doc:"open, then done or cancelled by the client that made it, for good." example:"open"`
	ClientID    string `json:"client_id" doc:"The API client that made it: the only one that updates it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c22"`
	CreatedAt   string `json:"created_at" format:"date-time" example:"2026-11-20T02:00:00Z"`
	UpdatedAt   string `json:"updated_at" format:"date-time" example:"2026-11-20T02:00:00Z"`
	ClosedAt    string `json:"closed_at,omitempty" format:"date-time" example:"2026-11-22T02:00:00Z"`
	Version     int    `json:"version" example:"1"`
}

// IntegrationTaskPage is a page of tasks, newest first.
type IntegrationTaskPage struct {
	Items      []IntegrationTask `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6IjAxOTMifQ"`
}

// IntegrationTaskCreate is a task for a client to make.
type IntegrationTaskCreate struct {
	ExternalRef string `json:"external_ref" minLength:"1" maxLength:"200" pattern:"^[\\x21-\\x7E]+$" doc:"Your reference for it, such as the ticket key, unique among your own tasks: making a task you made already answers with it, unchanged." example:"OPS-142"`
	Title       string `json:"title" minLength:"1" maxLength:"200" doc:"What a person is asked to do, in a line." example:"Chase the signed engagement letter"`
	Detail      string `json:"detail,omitempty" maxLength:"2000" doc:"More about it. Do not put personal data or amounts in it." example:"Raised from OPS-142."`
	DueOn       string `json:"due_on,omitempty" format:"date" example:"2026-12-15"`
	LinkURL     string `json:"link_url,omitempty" maxLength:"2048" doc:"An https address to the ticket." example:"https://example.atlassian.net/browse/OPS-142"`
}

// IntegrationTaskChange updates a task the client made; a field left out stays.
type IntegrationTaskChange struct {
	Title   *string `json:"title,omitempty" minLength:"1" maxLength:"200" example:"Chase the signed engagement letter"`
	Detail  *string `json:"detail,omitempty" maxLength:"2000" example:"Raised from OPS-142."`
	DueOn   *string `json:"due_on,omitempty" pattern:"^([0-9]{4}-[0-9]{2}-[0-9]{2})?$" doc:"A new date; an empty string clears it." example:"2026-12-15"`
	LinkURL *string `json:"link_url,omitempty" maxLength:"2048" doc:"A new https address; an empty string clears it." example:"https://example.atlassian.net/browse/OPS-142"`
	Status  *string `json:"status,omitempty" enum:"done,cancelled" doc:"Finish the task: for good, and only the client that made it may." example:"done"`
}

type (
	integrationTaskListInput struct {
		PageQuery
		Status string `query:"status" enum:"open,done,cancelled" doc:"Only tasks in this state; leave it out for all." example:"open"`
	}
	integrationTaskListOutput struct{ Body IntegrationTaskPage }
	// IntegrationTaskPath names a task.
	IntegrationTaskPath struct {
		TaskID string `path:"task_id" format:"uuid" doc:"The task's ID." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c21"`
	}
	integrationTaskGetInput struct{ IntegrationTaskPath }
	integrationTaskOutput   struct {
		ETag string `header:"ETag" doc:"The task's version, for If-Match."`
		Body IntegrationTask
	}
	integrationTaskCreateInput struct {
		KeyHeader
		PreferHeader
		Body IntegrationTaskCreate
	}
	integrationTaskUpdateInput struct {
		IntegrationTaskPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body IntegrationTaskChange
	}
	integrationTaskWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The task's version after the command, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     IntegrationTask
	}
)

func registerIntegrationTasks(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := []string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}

	register(a, authz.ListIntegrationTasks, huma.Operation{
		OperationID: "listIntegrationTasks", Method: http.MethodGet, Path: "/v1/tasks", Tags: []string{"Tasks"},
		Summary: "List the tasks integrations made",
		Description: "The tasks API clients made with fyniq.write.tasks (FR-INT-24), newest first, a page at a time, filtered by state. A client sees only its own; a person who " +
			"may view the row sees every client's. Compliance tasks are not here (listComplianceTasks). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *integrationTaskListInput) (*integrationTaskListOutput, error) {
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
		q := storage.IntegrationTaskQuery{Statuses: statuses, After: after, Limit: in.PageSize + 1}
		if p.Scoped {
			q.ClientID = p.Client // a client reads only its own
		}
		out := IntegrationTaskPage{Items: []IntegrationTask{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			list, err := u.IntegrationTasks().List(ctx, q)
			if err != nil {
				return err
			}
			if len(list) > in.PageSize {
				list = list[:in.PageSize]
				out.NextCursor = encodeCursor(list[len(list)-1].ID.String())
			}
			for _, t := range list {
				out.Items = append(out.Items, integrationTaskOf(t))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &integrationTaskListOutput{Body: out}, nil
	})

	register(a, authz.GetIntegrationTask, huma.Operation{
		OperationID: "getIntegrationTask", Method: http.MethodGet, Path: "/v1/tasks/{task_id}", Tags: []string{"Tasks"},
		Summary: "Read a task an integration made",
		Description: "A task with its ETag for If-Match (FR-INT-24). A client reads only its own: another client's task, and a compliance task, are not found. " +
			"Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *integrationTaskGetInput) (*integrationTaskOutput, error) {
		p, _ := authn.From(ctx)
		id, err := uuid.Parse(in.TaskID)
		if err != nil {
			return nil, integrationTaskNotFound()
		}
		var t storage.IntegrationTask
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			t, err = u.IntegrationTasks().Get(ctx, id)
			return err
		})
		if errors.Is(err, ports.ErrNotFound) || (err == nil && p.Scoped && t.ClientID != p.Client) {
			return nil, integrationTaskNotFound()
		}
		if err != nil {
			return nil, err
		}
		return &integrationTaskOutput{ETag: etag(t.Version), Body: integrationTaskOf(t)}, nil
	})

	register(a, authz.CreateIntegrationTask, huma.Operation{
		OperationID: "createIntegrationTask", Method: http.MethodPost, Path: "/v1/tasks", Tags: []string{"Tasks"}, DefaultStatus: http.StatusCreated,
		Summary: "Make a task as an integration",
		Description: "Makes a task for a person to do, as the integration client that calls (the scope fyniq.write.tasks; FR-INT-24). It is the client's own: only it updates it. " +
			"external_ref is your reference, unique among your tasks: asking again with the same reference answers with the task you made, unchanged (200), so a retry is safe. " +
			"It never makes a compliance task, never a financial record, and it is not on the AI or MCP surface. Only an API client makes one, not a person in the browser. " +
			"Side effects: the task and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIntegrationTaskClient}, append(writes, locks...)...), func(ctx context.Context, in *integrationTaskCreateInput) (*integrationTaskWriteOutput, error) {
		p, err := a.taskClient(ctx)
		if err != nil {
			return nil, err
		}
		title := strings.TrimSpace(in.Body.Title)
		if title == "" {
			return nil, problem(CodeInvalid, "The task needs a title.", "Say what the person is asked to do",
				FieldProblem{Code: CodeInvalid, Field: "body.title", Message: "is empty", Fix: "Say what the person is asked to do"})
		}
		due, link, err := integrationTaskFields(in.Body.DueOn, in.Body.LinkURL)
		if err != nil {
			return nil, err
		}
		created := false
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (IntegrationTask, error) {
			t, made, err := u.IntegrationTasks().Make(ctx, storage.NewIntegrationTask{ClientID: p.Client, ExternalRef: in.Body.ExternalRef, Title: title,
				Detail: strings.TrimSpace(in.Body.Detail), DueOn: due, LinkURL: link, CreatedBy: p.UserID, At: a.deps.Clock.Now()})
			created = made
			return integrationTaskOf(t), err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &integrationTaskWriteOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
		}
		if !created && !w.Replayed {
			w.Status = http.StatusOK // the task the client made already
		}
		return &integrationTaskWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.UpdateIntegrationTask, huma.Operation{
		OperationID: "updateIntegrationTask", Method: http.MethodPatch, Path: "/v1/tasks/{task_id}", Tags: []string{"Tasks"},
		Summary: "Update a task an integration made",
		Description: "Updates the title, detail, due date or link of a task the calling client made, or finishes it (done or cancelled, for good), over the version you read (If-Match; FR-INT-24). " +
			"Another client's task and every compliance task are not found: no client edits, completes or dismisses a statutory or agent task. A field left out stays. " +
			"Side effects: the change and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeIntegrationTaskClient, CodeIntegrationTaskFinished, CodeIfMatchRequired, CodeStale, storage.CodeVersion}, writes...), locks...),
		func(ctx context.Context, in *integrationTaskUpdateInput) (*integrationTaskWriteOutput, error) {
			p, err := a.taskClient(ctx)
			if err != nil {
				return nil, err
			}
			id, err := uuid.Parse(in.TaskID)
			if err != nil {
				return nil, integrationTaskNotFound()
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (IntegrationTask, error) {
				t, err := u.IntegrationTasks().Get(ctx, id)
				if err != nil {
					return IntegrationTask{}, err
				}
				if t.ClientID != p.Client {
					return IntegrationTask{}, ports.ErrNotFound // another client's task is not found
				}
				if err := checkIfMatch(in.IfMatch, etag(t.Version)); err != nil {
					return IntegrationTask{}, err
				}
				if t.Status != storage.IntegrationTaskOpen {
					return IntegrationTask{}, problem(CodeIntegrationTaskFinished, "The task is "+t.Status+" already.", "A finished task stays finished: make a new one with another reference")
				}
				c, err := integrationTaskChange(t, in.Body, a.deps.Clock.Now())
				if err != nil {
					return IntegrationTask{}, err
				}
				out, err := u.IntegrationTasks().Update(ctx, p.Client, id, t.Version, c)
				return integrationTaskOf(out), err
			})
			if errors.Is(err, ports.ErrNotFound) {
				return nil, integrationTaskNotFound()
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &integrationTaskWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &integrationTaskWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}

// taskClient is the principal when it is an integration API client acting for a member: not a person in the browser, not the AI, not the link's client,
// not an agent-class client. The policy layer has already narrowed it to the scope fyniq.write.tasks (authz.ScopeAllows).
func (a *API) taskClient(ctx context.Context) (authn.Principal, error) {
	p, _ := authn.From(ctx)
	if !p.Scoped || p.Link || p.AI() || p.Class != authn.ClassIntegration || p.UserID == uuid.Nil || p.Client == "" {
		return p, problem(CodeIntegrationTaskClient, "Only an integration API client with the scope fyniq.write.tasks makes or updates these tasks.",
			"Register an integration client with that scope; a person does their own work in the browser")
	}
	return p, nil
}

func integrationTaskNotFound() error {
	return problem(CodeNotFound, "There is no such task among yours.", "Choose one of the tasks GET /v1/tasks lists")
}

// integrationTaskFields checks a due date and a link, and returns them as the store takes them.
func integrationTaskFields(dueOn, link string) (*time.Time, string, error) {
	var due *time.Time
	if dueOn != "" {
		d, err := time.Parse(time.DateOnly, dueOn)
		if err != nil {
			return nil, "", problem(CodeInvalid, "The due date is not a date.", "Write it as 2026-12-15",
				FieldProblem{Code: CodeInvalid, Field: "body.due_on", Message: "is not a date", Fix: "Write it as 2026-12-15"})
		}
		due = &d
	}
	if link != "" {
		u, err := url.Parse(link)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return nil, "", problem(CodeInvalid, "The link is not an https address.", "Give the ticket's https address",
				FieldProblem{Code: CodeInvalid, Field: "body.link_url", Message: "is not an https address", Fix: "Give the ticket's https address"})
		}
	}
	return due, link, nil
}

// integrationTaskChange is t with the fields the request names changed, and finished when it says so.
func integrationTaskChange(t storage.IntegrationTask, b IntegrationTaskChange, now time.Time) (storage.IntegrationTaskChange, error) {
	c := storage.IntegrationTaskChange{Title: t.Title, Detail: t.Detail, DueOn: t.DueOn, LinkURL: t.LinkURL, Status: storage.IntegrationTaskOpen, At: now}
	if b.Title != nil {
		if c.Title = strings.TrimSpace(*b.Title); c.Title == "" {
			return c, problem(CodeInvalid, "The task needs a title.", "Say what the person is asked to do",
				FieldProblem{Code: CodeInvalid, Field: "body.title", Message: "is empty", Fix: "Say what the person is asked to do"})
		}
	}
	if b.Detail != nil {
		c.Detail = strings.TrimSpace(*b.Detail)
	}
	if b.DueOn != nil {
		due, _, err := integrationTaskFields(*b.DueOn, "")
		if err != nil {
			return c, err
		}
		c.DueOn = due
	}
	if b.LinkURL != nil {
		_, link, err := integrationTaskFields("", *b.LinkURL)
		if err != nil {
			return c, err
		}
		c.LinkURL = link
	}
	if b.Status != nil {
		c.Status = *b.Status
	}
	return c, nil
}

func integrationTaskOf(t storage.IntegrationTask) IntegrationTask {
	out := IntegrationTask{ID: t.ID.String(), ExternalRef: t.ExternalRef, Title: t.Title, Detail: t.Detail, LinkURL: t.LinkURL, Status: t.Status, ClientID: t.ClientID,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: t.UpdatedAt.UTC().Format(time.RFC3339), Version: t.Version}
	if t.DueOn != nil {
		out.DueOn = t.DueOn.Format(time.DateOnly)
	}
	if t.ClosedAt != nil {
		out.ClosedAt = t.ClosedAt.UTC().Format(time.RFC3339)
	}
	return out
}
