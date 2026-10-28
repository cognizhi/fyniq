package postgres

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the exports that run in the background (FR-RPT-09)

func (u *unitOfWork) ReportExports() storage.ReportExports { return reportExports{u} }

type reportExports struct{ u *unitOfWork }

func toReportExport(r sqlcdb.FyniqReportExport) storage.ReportExport {
	out := storage.ReportExport{ID: r.ID, Report: r.Report, Format: r.Format, Parameters: r.Parameters, RequestedBy: r.RequestedBy,
		RequestedAt: r.RequestedAt, Status: r.Status}
	if r.Failure != nil {
		out.Failure = *r.Failure
	}
	if r.LedgerRevision != nil && r.FileName != nil && r.ReadyAt != nil && r.ExpiresAt != nil {
		f := storage.ReportFileDone{Revision: *r.LedgerRevision, FileName: *r.FileName, SHA256: r.FileSha256, ReadyAt: *r.ReadyAt, ExpiresAt: *r.ExpiresAt}
		if r.MediaType != nil {
			f.MediaType = *r.MediaType
		}
		if r.SizeBytes != nil {
			f.Size = *r.SizeBytes
		}
		if r.RowsCount != nil {
			f.Rows = *r.RowsCount
		}
		out.File = &f
	}
	return out
}

func (r reportExports) Ask(ctx context.Context, x storage.NewReportExport) (storage.ReportExport, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportExport{}, err
	}
	row, err := r.u.q.InsertReportExport(ctx, sqlcdb.InsertReportExportParams{Report: x.Report, Format: x.Format, Parameters: x.Parameters,
		RequestedBy: x.RequestedBy, RequestedAt: x.RequestedAt})
	if err != nil {
		return storage.ReportExport{}, mapError(err)
	}
	out := toReportExport(row)
	return out, r.u.event(ctx, "report_export.requested", "report_export", out.ID, nil, map[string][2]any{
		"export": {nil, map[string]any{"report": out.Report, "format": out.Format, "parameters": string(out.Parameters)}}})
}

func (r reportExports) Get(ctx context.Context, id uuid.UUID) (storage.ReportExport, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportExport{}, err
	}
	row, err := r.u.q.GetReportExport(ctx, id)
	if err != nil {
		return storage.ReportExport{}, mapError(err)
	}
	return toReportExport(row), nil
}

func (r reportExports) Mine(ctx context.Context, requestedBy string, limit int) ([]storage.ReportExport, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ReportExportsOf(ctx, sqlcdb.ReportExportsOfParams{RequestedBy: requestedBy, PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ReportExport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toReportExport(row))
	}
	return out, nil
}

func (r reportExports) Ready(ctx context.Context, id uuid.UUID, f storage.ReportFileDone) (storage.ReportExport, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportExport{}, err
	}
	row, err := r.u.q.ReadyReportExport(ctx, sqlcdb.ReadyReportExportParams{ID: id, LedgerRevision: &f.Revision, FileName: &f.FileName,
		MediaType: &f.MediaType, SizeBytes: &f.Size, FileSha256: f.SHA256, RowsCount: &f.Rows, ReadyAt: &f.ReadyAt, ExpiresAt: &f.ExpiresAt})
	if err != nil {
		return storage.ReportExport{}, mapError(err)
	}
	// The file's name and hash only: what it holds is the books, which the trail need not copy.
	return toReportExport(row), r.u.event(ctx, "report_export.ready", "report_export", id, nil, map[string][2]any{
		"status": {storage.ExportPending, storage.ExportReady}, "file": {nil, map[string]any{"name": f.FileName, "sha256": hex.EncodeToString(f.SHA256),
			"rows": f.Rows, "ledger_revision": f.Revision, "expires_at": f.ExpiresAt.UTC().Format(time.RFC3339)}}})
}

func (r reportExports) Fail(ctx context.Context, id uuid.UUID, why string) (storage.ReportExport, error) {
	if err := r.u.check(); err != nil {
		return storage.ReportExport{}, err
	}
	row, err := r.u.q.FailReportExport(ctx, sqlcdb.FailReportExportParams{ID: id, Failure: &why})
	if err != nil {
		return storage.ReportExport{}, mapError(err)
	}
	return toReportExport(row), r.u.event(ctx, "report_export.failed", "report_export", id, nil, map[string][2]any{
		"status": {storage.ExportPending, storage.ExportFailed}, "failure": {nil, why}})
}

func (r reportExports) Expiring(ctx context.Context, now time.Time, limit int) ([]storage.ReportExport, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ExpiringReportExports(ctx, sqlcdb.ExpiringReportExportsParams{Now: &now, PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ReportExport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toReportExport(row))
	}
	return out, nil
}

func (r reportExports) Expire(ctx context.Context, id uuid.UUID) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if err := r.u.q.ExpireReportExport(ctx, id); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "report_export.expired", "report_export", id, nil, map[string][2]any{"status": {storage.ExportReady, storage.ExportExpired}})
}
