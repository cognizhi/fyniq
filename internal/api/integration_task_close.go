package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeIntegrationTaskPerson is the refusal of a closing by anything but a person signed in to Fyniq.
const CodeIntegrationTaskPerson = "ITASK-03"

// IntegrationTaskClose is how a person finishes a task an integration made.
type IntegrationTaskClose struct {
	Status string `json:"status" enum:"done,cancelled" doc:"done when the person did what the task asked; cancelled when it is no longer needed. For good: a finished task stays finished." example:"done"`
}

type integrationTaskCloseInput struct {
	IntegrationTaskPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body IntegrationTaskClose
}

// registerIntegrationTaskClose is the Inbox's one write on these tasks (FR-INT-24): a person with the right on the row, an Owner or an Administrator, finishes a
// task a client made, over the version they read. It changes no field but the status, reaches no compliance task, and no client, scope, token, agent or AI can
// call it: the operation belongs to no scope.
func registerIntegrationTaskClose(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := []string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.CloseIntegrationTask, huma.Operation{
		OperationID: "closeIntegrationTask", Method: http.MethodPost, Path: "/v1/tasks/{task_id}/close", Tags: []string{"Tasks"},
		Summary: "Finish a task an integration made, as a person",
		Description: "A person who holds the right on tasks made by integrations (an Owner or an Administrator) marks one done or cancelled, for good, over the version they read " +
			"(If-Match; FR-INT-24). Only the status changes. The person must be signed in to Fyniq: no API client, token, agent or AI closes a task this way, and a compliance " +
			"task is not found here. Side effects: the change and its audit event naming the person. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeIntegrationTaskPerson, CodeIntegrationTaskFinished, CodeIfMatchRequired, CodeStale, storage.CodeVersion}, writes...), locks...),
		func(ctx context.Context, in *integrationTaskCloseInput) (*integrationTaskWriteOutput, error) {
			p, _ := authn.From(ctx)
			if p.Scoped || p.Link || p.AI() || p.Class != authn.ClassUser {
				return nil, problem(CodeIntegrationTaskPerson, "Only a person signed in to Fyniq finishes a task an integration made.",
					"Sign in and finish it from the Inbox; an integration finishes its own tasks with updateIntegrationTask")
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
				if err := checkIfMatch(in.IfMatch, etag(t.Version)); err != nil {
					return IntegrationTask{}, err
				}
				if t.Status != storage.IntegrationTaskOpen {
					return IntegrationTask{}, problem(CodeIntegrationTaskFinished, "The task is "+t.Status+" already.", "A finished task stays finished")
				}
				c := storage.IntegrationTaskChange{Title: t.Title, Detail: t.Detail, DueOn: t.DueOn, LinkURL: t.LinkURL, Status: in.Body.Status, At: a.deps.Clock.Now()}
				out, err := u.IntegrationTasks().Update(ctx, t.ClientID, id, t.Version, c) // the task's own client: the row's identity never changes
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
