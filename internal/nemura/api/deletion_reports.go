package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// The deletion reports (PRD-002 NM-SEC-05): the retention engine never deletes silently. Each night it reports what is due for deletion for a record type (grievances, candidates), a person reviews the report
// within 30 days, and only then does the nightly job delete what is still due and not held, leaving a proof. An Owner or an HR administrator reviews; the page names no person and no record, only the type and the counts.

// DeletionReportView is one report.
type DeletionReportView struct {
	ID         string `json:"id" format:"uuid" doc:"The report." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d6a01"`
	RecordType string `json:"record_type" doc:"The kind of record that is due: grievances or candidates." example:"candidates"`
	Due        int    `json:"due" doc:"How many records are due for deletion." example:"3"`
	Held       int    `json:"held" doc:"How many a legal hold keeps." example:"0"`
	CreatedOn  string `json:"created_on" format:"date" doc:"The day the report was made." example:"2027-05-02"`
	ReviewBy   string `json:"review_by" format:"date" doc:"The day the report must have been reviewed: 30 days after it was made." example:"2027-06-01"`
	State      string `json:"state" enum:"pending,reviewed,deleted" doc:"Waiting for review, reviewed and waiting for the nightly job, or deleted." example:"pending"`
	Overdue    bool   `json:"overdue" doc:"The review date has passed with no review;" example:"false"`
	ReviewedBy string `json:"reviewed_by,omitempty" doc:"Who reviewed it." example:"Hana HR"`
	ReviewedOn string `json:"reviewed_on,omitempty" format:"date" doc:"When." example:"2027-05-03"`
}

// DeletionReports is the reports behind the API.
type DeletionReports interface {
	List(ctx context.Context, s Session) ([]DeletionReportView, error)
	Review(ctx context.Context, s Session, id uuid.UUID) error
}

type deletionReportsOutput struct {
	Body struct {
		Reports []DeletionReportView `json:"reports" nullable:"false" doc:"The reports, the newest first."`
	}
}
type deletionReviewInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The report." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d6a01"`
}
type deletionReviewOutput struct {
	Body struct {
		Reviewed bool `json:"reviewed" doc:"True once the report is reviewed." example:"true"`
	}
}

func init() {
	IdempotencyExempt["POST /v1/deletion-reports/{id}/review"] = "a report is reviewed once: a second review is refused (NM_RETENTION_CONFLICT)"
}

func (a *API) registerDeletionReports() {
	gate := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		held, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
		if err != nil {
			return s, huma.Error500InternalServerError("The roles could not be read.")
		}
		if !holdsErasure(held, authz.Create) {
			return s, Err(CodeRoleRequired, "Only an Owner or an HR administrator sees and reviews the reports of records due for deletion.")
		}
		if a.d.DeletionReports == nil {
			return s, Err(CodeUnavailable, "Deletion reports are not set up in this deployment.")
		}
		return s, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listDeletionReports", Method: http.MethodGet, Path: "/v1/deletion-reports", Tags: []string{"Privacy"}, Summary: "The reports of records due for deletion",
		Description: "PRD-002 NM-SEC-05. For each kind of record the retention engine deletes (grievances, candidates): how many are due, how many a legal hold keeps, the day the report must be reviewed and its state. No person and no record is named. Only an Owner or an HR administrator. It changes nothing.", Errors: []int{401, 403, 503},
	}, func(ctx context.Context, in *selfInput) (*deletionReportsOutput, error) {
		s, err := gate(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		l, err := a.d.DeletionReports.List(ctx, s)
		if err != nil {
			return nil, huma.Error500InternalServerError("The reports could not be read.")
		}
		out := &deletionReportsOutput{}
		out.Body.Reports = append([]DeletionReportView{}, l...)
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "reviewDeletionReport", Method: http.MethodPost, Path: "/v1/deletion-reports/{id}/review", Tags: []string{"Privacy"}, Summary: "Review a report of records due for deletion",
		Description: "PRD-002 NM-SEC-05. Records that a person has seen what is due and what a hold keeps; only then does the nightly job delete what is still due and not held, with a proof of each that holds no content. A report is reviewed once. Only an Owner or an HR administrator. It is audited and is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 404, 409, 503},
	}, func(ctx context.Context, in *deletionReviewInput) (*deletionReviewOutput, error) {
		s, err := gate(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		u, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such report.")
		}
		if err := a.d.DeletionReports.Review(ctx, s, u); err != nil {
			return nil, Err("NM_RETENTION_CONFLICT", err.Error())
		}
		out := &deletionReviewOutput{}
		out.Body.Reviewed = true
		return out, nil
	})
}
