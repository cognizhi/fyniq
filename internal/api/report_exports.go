package api

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// ------------------------------------------------------ the exports that run in the background (FR-RPT-09, FR-RPT-10)

// The background exports' error codes.
const (
	CodeExportNotReady = "REPORT-05" // the export is pending, or failed
	CodeExportExpired  = "REPORT-06" // its 24 hours are up, and its file is gone
	CodeExportNotYours = "REPORT-07" // another person asked for it
)

// ReportExportRequest is an accountant export asked for.
type ReportExportRequest struct {
	Report string `json:"report" enum:"general_ledger,trial_balance,journals,fixed_assets,receivables,payables" doc:"general_ledger: every account's lines over the range with the running balance; trial_balance: every account's balance at the end of the month that holds to; journals: every entry posted over the range, a row per line; fixed_assets: the register's cost, accumulated depreciation and net book value at to; receivables and payables: each open document and credit by customer or supplier at to." example:"general_ledger"`
	From   string `json:"from,omitempty" format:"date" doc:"The first day: needed for general_ledger and journals, which run over a range." example:"2026-10-01"`
	To     string `json:"to" format:"date" doc:"The last day of the range, or the day the report is as at." example:"2026-10-31"`
	Format string `json:"format,omitempty" enum:"csv,xlsx" default:"xlsx" doc:"csv: the rows, amounts as plain decimals with an ASCII minus; xlsx: a workbook, amounts as number cells. Left out, xlsx." example:"xlsx"`
}

// ReportExportOut is an export as it stands.
type ReportExportOut struct {
	ID          string `json:"id" format:"uuid" doc:"The export." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	Report      string `json:"report" doc:"What it exports." example:"general_ledger"`
	Format      string `json:"format" doc:"csv or xlsx." example:"xlsx"`
	From        string `json:"from,omitempty" format:"date" doc:"The first day, for a range." example:"2026-10-01"`
	To          string `json:"to" format:"date" doc:"The last day, or the day it is as at." example:"2026-10-31"`
	Status      string `json:"status" enum:"pending,ready,failed,expired" doc:"pending: the worker is preparing it; ready: download it until expires_at; failed: why says why; expired: its 24 hours are up, and its file is gone." example:"ready"`
	RequestedAt string `json:"requested_at" format:"date-time" doc:"When it was asked for." example:"2026-10-31T09:00:00Z"`
	FileName    string `json:"file_name,omitempty" doc:"Its file's name, once ready." example:"general-ledger-2026-10-01-to-2026-10-31.xlsx"`
	MediaType   string `json:"media_type,omitempty" doc:"Its type." example:"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"`
	Size        int64  `json:"size_bytes,omitempty" doc:"Its size, in bytes." example:"40960"`
	Rows        int64  `json:"rows,omitempty" doc:"The rows it holds." example:"512"`
	SHA256      string `json:"sha256,omitempty" doc:"The SHA-256 of the file, in hexadecimal, as the audit trail records it." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Revision    int64  `json:"ledger_revision,omitempty" doc:"The ledger revision it read the books at (FR-RPT-08)." example:"42"`
	ReadyAt     string `json:"ready_at,omitempty" format:"date-time" doc:"When it was ready." example:"2026-10-31T09:00:05Z"`
	ExpiresAt   string `json:"expires_at,omitempty" format:"date-time" doc:"Until when it can be downloaded: 24 hours after it was ready." example:"2026-11-01T09:00:05Z"`
	Why         string `json:"why,omitempty" doc:"Why it failed, and what to do." example:"the range crosses the start of a financial year: export each year apart"`
}

// ReportExportList is a person's exports, newest first.
type ReportExportList struct {
	Items []ReportExportOut `json:"items" nullable:"false" doc:"The caller's exports, newest first, at most 50."`
}

func reportExportOut(x storage.ReportExport) ReportExportOut {
	var p reportexport.Parameters
	_ = json.Unmarshal(x.Parameters, &p) // written by the API: always an object of these
	out := ReportExportOut{ID: x.ID.String(), Report: x.Report, Format: x.Format, From: p.From, To: p.To, Status: x.Status,
		RequestedAt: x.RequestedAt.UTC().Format(time.RFC3339), Why: x.Failure}
	if f := x.File; f != nil {
		out.FileName, out.MediaType, out.Size, out.Rows, out.SHA256, out.Revision = f.FileName, f.MediaType, f.Size, f.Rows, hex.EncodeToString(f.SHA256), f.Revision
		out.ReadyAt, out.ExpiresAt = f.ReadyAt.UTC().Format(time.RFC3339), f.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return out
}

type reportExportInput struct {
	KeyHeader
	PreferHeader
	Body ReportExportRequest
}

type reportExportOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ReportExportOut
}

type reportExportPath struct {
	ExportID string `path:"export_id" format:"uuid" doc:"The export." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
}

type reportExportGetOutput struct{ Body ReportExportOut }

type reportExportListOutput struct{ Body ReportExportList }

// mine is an export the caller asked for, else REPORT-07; or not found.
func mine(ctx context.Context, u storage.UnitOfWork, path reportExportPath) (storage.ReportExport, error) {
	id, err := parseID("export_id", path.ExportID)
	if err != nil {
		return storage.ReportExport{}, err
	}
	x, err := u.ReportExports().Get(ctx, id)
	if err != nil {
		return storage.ReportExport{}, err
	}
	if x.RequestedBy != uploader(ctx) {
		return storage.ReportExport{}, problem(CodeExportNotYours, "Another person asked for this export, and only they download it.", "Ask for the export yourself")
	}
	return x, nil
}

func registerReportExports(a *API) {
	register(a, authz.RequestReportExport, huma.Operation{
		OperationID: "requestReportExport", Method: http.MethodPost, Path: "/v1/report-exports", Tags: []string{"Reports"}, DefaultStatus: http.StatusAccepted,
		Summary: "Ask for an accountant export",
		Description: "Asks for one of the accountant exports (FR-RPT-10) as CSV or XLSX, which the worker prepares in the background " +
			"(FR-RPT-09): the general ledger or the journal entries over a range of days, or the trial balance, the fixed asset schedule, " +
			"or the receivables or payables in detail at a day. The answer is 202 with the export, pending; getReportExport says when it " +
			"is ready, and the caller is notified. Its file is downloaded with getReportExportFile, by the caller only, until 24 hours " +
			"after it is ready. Side effects: the export and its audit event, and the job. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and asks for nothing.",
	}, []string{CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeReportExport},
		func(ctx context.Context, in *reportExportInput) (*reportExportOutput, error) {
			p, _ := authn.From(ctx)
			b := in.Body
			var bad []FieldProblem
			to, err := time.Parse(time.DateOnly, b.To)
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.to", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
			}
			if reportexport.Reports[b.Report] {
				if from, err := time.Parse(time.DateOnly, b.From); err != nil {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.from", Message: "is needed, as a date, for a report over a range", Fix: "Send the first day as YYYY-MM-DD"})
				} else if from.After(to) {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.from", Message: "is after to", Fix: "Send a first day on or before the last"})
				}
			} else {
				b.From = ""
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			params, err := json.Marshal(reportexport.Parameters{From: b.From, To: b.To})
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (ReportExportOut, error) {
				x, err := u.ReportExports().Ask(ctx, storage.NewReportExport{Report: b.Report, Format: b.Format, Parameters: params, RequestedBy: uploader(ctx),
					RequestedAt: a.deps.Clock.Now()})
				if err != nil {
					return ReportExportOut{}, err
				}
				if a.deps.Outbox != nil {
					if err := reportexport.Queue(ctx, *a.deps.Outbox, u, p.Organisation, x); err != nil {
						return ReportExportOut{}, err
					}
				}
				return reportExportOut(x), nil
			})
			if err != nil {
				return nil, err
			}
			return &reportExportOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ListReportExports, huma.Operation{
		OperationID: "listReportExports", Method: http.MethodGet, Path: "/v1/report-exports", Tags: []string{"Reports"},
		Summary:     "List my exports",
		Description: "The caller's background exports, newest first, at most 50, each with its status (FR-RPT-09). Read-only; no side effects.",
	}, nil, func(ctx context.Context, _ *struct{}) (*reportExportListOutput, error) {
		out := ReportExportList{Items: []ReportExportOut{}}
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			xs, err := u.ReportExports().Mine(ctx, uploader(ctx), 50)
			for _, x := range xs {
				out.Items = append(out.Items, reportExportOut(x))
			}
			return err
		})
		return &reportExportListOutput{Body: out}, err
	})

	register(a, authz.GetReportExport, huma.Operation{
		OperationID: "getReportExport", Method: http.MethodGet, Path: "/v1/report-exports/{export_id}", Tags: []string{"Reports"},
		Summary:     "Get one of my exports",
		Description: "An export the caller asked for and its status: pending, ready with its file's name, hash and expiry, failed with why, or expired (FR-RPT-09). Another person's is REPORT-07. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeExportNotYours}, func(ctx context.Context, in *reportExportPath) (*reportExportGetOutput, error) {
		var out ReportExportOut
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			x, err := mine(ctx, u, *in)
			out = reportExportOut(x)
			return err
		})
		return &reportExportGetOutput{Body: out}, err
	})

	register(a, authz.GetReportExportFile, huma.Operation{
		OperationID: "getReportExportFile", Method: http.MethodGet, Path: "/v1/report-exports/{export_id}/file", Tags: []string{"Reports"},
		Summary: "Download one of my exports",
		Description: "The file of a ready export the caller asked for, in base64 with its SHA-256 (FR-RPT-09). The caller's rights are checked " +
			"again at each download, so a role taken away stops it at once. A pending or failed export is REPORT-05; one past its 24 hours " +
			"is REPORT-06; another person's is REPORT-07. Side effects: an audit event of the download, with the file's hash. Never cached.",
	}, []string{CodeNotFound, CodeExportNotReady, CodeExportExpired, CodeExportNotYours}, func(ctx context.Context, in *reportExportPath) (*reportFileOutput, error) {
		var out ReportFile
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			x, err := mine(ctx, u, *in)
			if err != nil {
				return err
			}
			now := a.deps.Clock.Now()
			switch {
			case x.Status == storage.ExportExpired || (x.File != nil && !now.Before(x.File.ExpiresAt)):
				return problem(CodeExportExpired, "The export's 24 hours are up, and its file is gone.", "Ask for the export again")
			case x.Status != storage.ExportReady || x.File == nil:
				return problem(CodeExportNotReady, "The export is "+x.Status+", so it has no file to download.", "Wait until it is ready, or ask again if it failed")
			case a.deps.Staging == nil:
				return errors.New("api: no blob store holds the exports")
			}
			r, err := a.deps.Staging.Get(ctx, reportexport.Key(p.Organisation, x.ID, x.File.FileName))
			if errors.Is(err, ports.ErrNotFound) {
				return problem(CodeExportExpired, "The export's file is gone.", "Ask for the export again")
			}
			if err != nil {
				return err
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "report_export.downloaded", ObjectType: "report_export", ObjectID: x.ID,
				Changes: map[string][2]any{"file": {nil, map[string]any{"name": x.File.FileName, "sha256": hex.EncodeToString(x.File.SHA256)}}}}); err != nil {
				return err
			}
			out = ReportFile{ExportID: x.ID.String(), FileName: x.File.FileName, MediaType: x.File.MediaType, SHA256: hex.EncodeToString(x.File.SHA256),
				Size: len(data), Rows: int(x.File.Rows), Revision: x.File.Revision, Content: base64.StdEncoding.EncodeToString(data)}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &reportFileOutput{Body: out}, nil
	})
}
