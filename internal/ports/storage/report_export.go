package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The statuses of a report export (FR-RPT-09).
const (
	ExportPending = "pending"
	ExportReady   = "ready"
	ExportFailed  = "failed"
	ExportExpired = "expired"
)

// CodeReportExport is the database's refusal of an export written prepared, or moved other than pending to ready or
// failed and ready to expired, or changed in what it holds (FY243).
const CodeReportExport = "STORAGE-EXPORT-01"

// NewReportExport is an export a person asks for: the report, its format and parameters.
type NewReportExport struct {
	Report      string
	Format      string
	Parameters  []byte // a JSON object
	RequestedBy string // "user:<id>"
	RequestedAt time.Time
}

// ReportFileDone is what the worker prepared: the file, its hash and the ledger revision its figures are read at.
type ReportFileDone struct {
	Revision  int64
	FileName  string
	MediaType string
	Size      int64
	Rows      int64
	SHA256    []byte
	ReadyAt   time.Time
	ExpiresAt time.Time
}

// ReportExport is an export as it stands.
type ReportExport struct {
	ID          uuid.UUID
	Report      string
	Format      string
	Parameters  []byte
	RequestedBy string
	RequestedAt time.Time
	Status      string
	File        *ReportFileDone // once ready, and after it expires
	Failure     string
}

// ReportExports are the exports that run in the background (FR-RPT-09, FR-RPT-10): asked for by the API, prepared by
// the worker, expired by a job. Each change is audited on the export. The database refuses anything else (CodeReportExport).
type ReportExports interface {
	Ask(ctx context.Context, x NewReportExport) (ReportExport, error)
	// Get is an export, or ports.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (ReportExport, error)
	// Mine are a person's exports, newest first, at most limit.
	Mine(ctx context.Context, requestedBy string, limit int) ([]ReportExport, error)
	Ready(ctx context.Context, id uuid.UUID, f ReportFileDone) (ReportExport, error)
	Fail(ctx context.Context, id uuid.UUID, why string) (ReportExport, error)
	// Expiring are the ready exports whose time is up by now, at most limit.
	Expiring(ctx context.Context, now time.Time, limit int) ([]ReportExport, error)
	Expire(ctx context.Context, id uuid.UUID) error
}
