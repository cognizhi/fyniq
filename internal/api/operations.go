package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ long operations (FR-INT-11)

// Operation is long work the caller asked for, which runs in the background: poll it until it is done.
type Operation struct {
	ID          string `json:"id" format:"uuid" doc:"The operation." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Kind        string `json:"kind" enum:"report_export,checks_run,migration_validation" doc:"What the work is: report_export prepares an accountant export (requestReportExport); checks_run runs every check (runChecks); migration_validation validates an uploaded workbook (uploadMigrationWorkbook)." example:"report_export"`
	Status      string `json:"status" enum:"running,succeeded,failed" doc:"running: waiting or under way, and retried after a failure; succeeded: done, so read the resource; failed: it gave up after its attempts, so ask again." example:"succeeded"`
	Resource    string `json:"resource" doc:"The path of what the work makes or changes: read it for the outcome, such as the export's file or the checks' last run." example:"/v1/report-exports/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	RequestedAt string `json:"requested_at" format:"date-time" doc:"When it was asked for." example:"2026-11-07T09:00:00Z"`
	FinishedAt  string `json:"finished_at,omitempty" format:"date-time" doc:"When it succeeded or failed; absent while it runs." example:"2026-11-07T09:00:04Z"`
}

// OperationPath names an operation.
type OperationPath struct {
	OperationID string `path:"operation_id" format:"uuid" doc:"The operation, from the Location of the answer that started it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
}

// OperationList is a page of the caller's operations.
type OperationList struct {
	Items      []Operation `json:"items" nullable:"false" doc:"The caller's operations, newest first."`
	NextCursor string      `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

type operationListOutput struct{ Body OperationList }

// maxOperationsListed is the most of a person's operations the list pages through: the latest.
const maxOperationsListed = 1000

type operationOutput struct {
	RetryAfter string `header:"Retry-After" doc:"While it runs: the seconds to wait before polling again."`
	Body       Operation
}

// operationOf is an operation as the API shows it: its status is its job's.
func operationOf(o storage.Operation) Operation {
	out := Operation{ID: o.ID.String(), Kind: o.Kind, Status: "running", Resource: o.Resource, RequestedAt: o.RequestedAt.UTC().Format(time.RFC3339)}
	switch o.JobState {
	case storage.OutboxDone:
		out.Status = "succeeded"
	case storage.OutboxDead:
		out.Status = "failed"
	}
	if out.Status != "running" {
		out.FinishedAt = o.JobUpdated.UTC().Format(time.RFC3339)
	}
	return out
}

// startOperation records that job does the long work of kind on resource for the caller, in u (FR-INT-11), and returns
// the path the caller polls.
func startOperation(ctx context.Context, u storage.UnitOfWork, kind string, job uuid.UUID, resource string, at time.Time) (storage.Operation, error) {
	return u.Operations().Create(ctx, storage.NewOperation{Kind: kind, JobID: job, Resource: resource, RequestedBy: uploader(ctx), RequestedAt: at})
}

// operationPath is where an operation is polled.
func operationPath(id string) string { return "/v1/operations/" + id }

func registerOperations(a *API) {
	register(a, authz.ListMyOperations, huma.Operation{
		OperationID: "listMyOperations", Method: http.MethodGet, Path: "/v1/operations", Tags: []string{"Operations"},
		Summary: "List my long operations",
		Description: "The long work the caller asked for, newest first, a page at a time (FR-INT-10, FR-INT-11): each with its kind, its " +
			"status and its resource, as getOperation shows it. The web app's operations tray reads it, polling while any runs. Only the " +
			"caller's own: nobody lists another person's. Read-only; no side effects.",
	}, []string{CodeCursor, storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, in *PageQuery) (*operationListOutput, error) {
		out := OperationList{Items: []Operation{}}
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			xs, err := u.Operations().Mine(ctx, uploader(ctx), maxOperationsListed)
			if err != nil {
				return err
			}
			page, next, err := pageOf(xs, func(x storage.Operation) string { return x.ID.String() }, *in)
			for _, x := range page {
				out.Items = append(out.Items, operationOf(x))
			}
			out.NextCursor = next
			return err
		})
		return &operationListOutput{Body: out}, err
	})

	register(a, authz.GetOperation, huma.Operation{
		OperationID: "getOperation", Method: http.MethodGet, Path: "/v1/operations/{operation_id}", Tags: []string{"Operations"},
		Summary: "Poll a long operation",
		Description: "The status of long work the caller asked for, which runs in the background (FR-INT-11): an answer of 202 names it in " +
			"its Location. running answers Retry-After; succeeded and failed say when, and resource is what the work made or changed. " +
			"Only whoever asked for it reads it: anyone else gets 404. Read-only; no side effects.",
	}, []string{CodeNotFound, storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, in *OperationPath) (*operationOutput, error) {
		id, err := parseID("operation_id", in.OperationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var o storage.Operation
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err = u.Operations().Get(ctx, id)
			return err
		})
		if err != nil {
			return nil, err
		}
		if o.RequestedBy != uploader(ctx) {
			return nil, problem(CodeNotFound, "There is no operation of yours with that ID.", "Use the Location of the answer that started it")
		}
		out := &operationOutput{Body: operationOf(o)}
		if out.Body.Status == "running" {
			out.RetryAfter = "2"
		}
		return out, nil
	})
}
