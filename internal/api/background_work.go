package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// BackgroundWork is a task of the organisation's background work (§12.11): what it is and where it stands.
type BackgroundWork struct {
	ID        string `json:"id" format:"uuid" doc:"The task's ID, as the check C.2 #12 lists it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Kind      string `json:"kind" doc:"What the task does." example:"email.send"`
	State     string `json:"state" enum:"pending,dispatched,running,done,failed,unknown_outcome,dead" doc:"Where it stands: pending once replayed." example:"pending"`
	Attempts  int    `json:"attempts" minimum:"0" doc:"Its attempts so far: none again once replayed." example:"0"`
	LastError string `json:"last_error,omitempty" doc:"Why its last attempt failed, kept when it is replayed." example:"the provider refused the connection"`
}

// WorkPath is a task of the background work, by its ID.
type WorkPath struct {
	WorkID string `path:"work_id" format:"uuid" doc:"The task's ID, as the check C.2 #12 lists it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
}

type backgroundWorkReplayInput struct {
	WorkPath
	KeyHeader
	PreferHeader
}

type backgroundWorkOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this answer is the one recorded for the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BackgroundWork
}

// registerBackgroundWork adds the replay of a task that stopped (C.2 #12, G4-073).
func registerBackgroundWork(a *API) {
	register(a, authz.ReplayBackgroundWork, exempt(huma.Operation{
		OperationID: "replayBackgroundWork", Method: http.MethodPost, Path: "/v1/background-work/{work_id}/replay", Tags: []string{"Checks"},
		Summary: "Replay a background task that stopped",
		Description: "Sends a task of the organisation's background work whose attempts ran out back to pending, with its attempts " +
			"counted afresh and its last error kept, once what stopped it is fixed (§12.11, C.2 #12, G4-073). It is dispatched at " +
			"once after the change commits, and the next tick sends it if that fails. Only a stopped task replays: any other is " +
			"API-404-01. An Owner or an Administrator, who are told when a task stops (FR-NTF-01). Side effects: the task pending " +
			"again and its audit event; the check lists it no more from its next run. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, "If-Match", "A replay acts on a task that stopped, which nothing else changes: replaying one not stopped is refused."),
		[]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *backgroundWorkReplayInput) (*backgroundWorkOutput, error) {
			id, err := parseID("work_id", in.WorkID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BackgroundWork, error) {
				row, err := u.Outbox().Get(ctx, id)
				if err == nil && row.State != storage.OutboxDead {
					err = ports.ErrNotFound
				}
				if err == nil {
					if a.deps.Outbox != nil {
						row, err = a.deps.Outbox.Replay(ctx, u, p.Organisation, id)
					} else {
						row, err = u.Outbox().Replay(ctx, id)
					}
				}
				if errors.Is(err, ports.ErrNotFound) {
					return BackgroundWork{}, problem(CodeNotFound, "No stopped background task with that ID exists in this organisation.",
						"Open the check C.2 #12, Every scheduled job ran on time, for the tasks that stopped")
				}
				if err != nil {
					return BackgroundWork{}, err
				}
				if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "outbox.row.replayed", ObjectType: "outbox", ObjectID: row.ID,
					Changes: map[string][2]any{"state": {string(storage.OutboxDead), string(row.State)}, "kind": {nil, row.Kind}}}); err != nil {
					return BackgroundWork{}, err
				}
				return BackgroundWork{ID: row.ID.String(), Kind: row.Kind, State: string(row.State), Attempts: row.Attempts, LastError: row.LastError}, nil
			})
			if err != nil {
				return nil, err
			}
			return &backgroundWorkOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
