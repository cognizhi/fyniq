//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// FR-RPT-09 and FR-RPT-10 (p1-reports tasks 3.3 and 3.4): an accountant export is asked for through the API's role and
// prepared by the worker's: the trial balance at 31 October as CSV, debits and credits that add up; the general
// ledger and the journal entries of October; the fixed asset schedule and the receivables, empty but for their totals
// and headers. Each is kept in the blob store with its SHA-256 and the revision it read, and expires 24 hours later. A
// day the books have no period for fails with why. When the 24 hours are up the files are deleted. The database lets
// the API only ask, and the worker only move an export forward (FY243).
// req: FR-RPT-09
// req: FR-RPT-10
// req: QA-ENG-05
// asvs: V13.2.2, V8.2.2
func TestAnAccountantExportIsPreparedInTheBackground(t *testing.T) {
	person := uuid.New()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: person.String()}})
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := newFinancialBooks(t, ctx, api, "Exports Pte Ltd")
	b.post(t, ctx, api, ledger.SourceOpeningBalance, 10, 1, "1010", "3010", "10000.00")
	b.post(t, ctx, api, ledger.SourceManualJournal, 10, 3, "1100", "4010", "5000.00")
	b.post(t, ctx, api, ledger.SourceManualJournal, 10, 5, "6110", "1010", "500.00")
	at := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	clk := clock.NewFrozen(at)
	files, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ask := func(report, from, to, format string) storage.ReportExport {
		t.Helper()
		params, _ := json.Marshal(reportexport.Parameters{From: from, To: to})
		var x storage.ReportExport
		if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if x, err = u.ReportExports().Ask(ctx, storage.NewReportExport{Report: report, Format: format, Parameters: params, RequestedBy: "user:" + person.String(),
				RequestedAt: at}); err != nil {
				return err
			}
			return reportexport.Queue(ctx, outbox.Dispatcher{Store: api, Queue: local.NewMemory(), Clock: clk}, u, b.Org, x)
		}); err != nil {
			t.Fatal(err)
		}
		return x
	}
	p := reportexport.Preparer{Store: worker, Files: files, Outbox: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: clk}, Clock: clk}
	prepare := func(x storage.ReportExport) storage.ReportExport {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"export_id": x.ID.String()})
		if err := p.Work()(context.Background(), b.Org, storage.OutboxRow{Kind: reportexport.Kind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if err := worker.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			x, err = u.ReportExports().Get(ctx, x.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return x
	}
	read := func(x storage.ReportExport) string {
		t.Helper()
		r, err := files.Get(context.Background(), reportexport.Key(b.Org, x.ID, x.File.FileName))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, _ := io.ReadAll(r)
		return string(data)
	}

	tb := prepare(ask(reportexport.TrialBalance, "", "2026-10-31", "csv"))
	if tb.Status != storage.ExportReady || tb.File == nil || tb.File.FileName != "trial-balance-2026-10-31.csv" || !tb.File.ExpiresAt.Equal(at.Add(24*time.Hour)) ||
		len(tb.File.SHA256) != 32 || tb.File.Revision == 0 {
		t.Fatalf("the trial balance: %+v %+v", tb, tb.File)
	}
	csv := read(tb)
	for _, want := range []string{
		"\"Code\",\"Account\",\"Type\",\"Debit SGD\",\"Credit SGD\"\r\n",
		"\"1010\",\"DBS current\",\"asset\",9500.00,\"\"\r\n",
		"\"4010\",\"Subscription revenue\",\"revenue\",\"\",5000.00\r\n",
		"\"\",\"Total, at 2026-10-31\",\"\",15000.00,15000.00\r\n",
	} {
		if !strings.Contains(csv, want) {
			t.Errorf("the trial balance lacks %q:\n%s", want, csv)
		}
	}
	if gl := prepare(ask(reportexport.GeneralLedger, "2026-10-01", "2026-10-31", "xlsx")); gl.Status != storage.ExportReady ||
		gl.File.FileName != "general-ledger-2026-10-01-to-2026-10-31.xlsx" || gl.File.Rows != 16 {
		t.Errorf("the general ledger: %+v %+v", gl, gl.File)
	}
	if j := prepare(ask(reportexport.Journals, "2026-10-01", "2026-10-31", "csv")); j.Status != storage.ExportReady || j.File.Rows != 6 ||
		!strings.Contains(read(j), "\"2026-10-05\",") {
		t.Errorf("the journal entries: %+v %+v", j, j.File)
	}
	for _, r := range []string{reportexport.FixedAssets, reportexport.Receivables} {
		if x := prepare(ask(r, "", "2026-10-31", "xlsx")); x.Status != storage.ExportReady {
			t.Errorf("%s: %+v", r, x)
		}
	}
	if x := prepare(ask(reportexport.TrialBalance, "", "2027-01-31", "csv")); x.Status != storage.ExportFailed || x.Failure != "the books have no period for its day" {
		t.Errorf("a day the books have no period for: %+v", x)
	}

	// 25 hours later the ready ones expire, and their files are gone.
	n, err := p.Expire(context.Background(), at.Add(25*time.Hour))
	if err != nil || n != 5 {
		t.Fatalf("the expiry: %d %v", n, err)
	}
	if _, err := files.Get(context.Background(), reportexport.Key(b.Org, tb.ID, tb.File.FileName)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("the trial balance's file after it expired: %v", err)
	}

	// FY243 and the grants: the API only asks; the worker only moves an export forward; nothing deletes one.
	for _, c := range []struct {
		role, sql string
		code      string
	}{
		{"fyniq_api", `INSERT INTO fyniq.report_export (report, format, parameters, requested_by, requested_at, status) VALUES ('journals', 'csv', '{}', 'user:x', now(), 'ready')`, "42501"},
		{"fyniq_api", `UPDATE fyniq.report_export SET failure = 'x' WHERE id = '` + tb.ID.String() + `'`, "42501"},
		{"fyniq_worker", `UPDATE fyniq.report_export SET status = 'pending' WHERE id = '` + tb.ID.String() + `'`, "FY243"},
		{"fyniq_worker", `UPDATE fyniq.report_export SET file_name = 'other.csv' WHERE id = '` + tb.ID.String() + `'`, "FY243"},
		{"fyniq_worker", `DELETE FROM fyniq.report_export WHERE id = '` + tb.ID.String() + `'`, "42501"},
	} {
		err := asOrg(context.Background(), conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), c.sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %s: want %s, got %v", c.role, c.sql, c.code, err)
		}
	}
}
