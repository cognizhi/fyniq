package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the parallel month (§17.1 M5, FR-MIG-02; rev 0.268)

// parallelReady says whether the migration takes the parallel month's workbook: once its import is committed and its
// parity report signed (M4).
func parallelReady(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) error {
	c, err := u.Migrations().Commit(ctx, run)
	switch {
	case errors.Is(err, ports.ErrNotFound) || err == nil && c.Status != storage.CommitCommitted:
		return problem(migration.CodeCommit, "The import is not committed yet, so there is no parallel month.",
			"Commit the import and sign its parity report first.")
	case err != nil:
		return err
	}
	offs, err := u.Migrations().SignOffs(ctx, run)
	if err != nil {
		return err
	}
	for _, o := range offs {
		if o.Subject == storage.SignOffReconciliation {
			return nil
		}
	}
	return problem(migration.CodeParallel, "The parity report is not signed yet, so the parallel month cannot start.",
		"Decide the register's lines and sign the reconciliation (M4) first.")
}

// parallelSource is the parallel month's report's source for a change: refused before the commit or the workbook.
func (a *API) parallelSource(ctx context.Context, id uuid.UUID) (reconciliationSource, error) {
	p, _ := authn.From(ctx)
	src, err := a.parallelSourceOf(ctx, p.Organisation, id)
	switch {
	case errors.Is(err, errNotCommitted):
		return src, problem(migration.CodeCommit, "The import is not committed yet, so there is no parallel month.",
			"Commit the import and sign its parity report first.")
	case errors.Is(err, errNotStarted):
		return src, problem(migration.CodeParallel, "The parallel month has no valid workbook yet, so there is no report to decide or sign.",
			"Upload the workbook as it stands at the end of the parallel month, and wait for its validation.")
	}
	return src, err
}

func registerMigrationParallel(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid,
		CodeNotFound, CodeUnavailable, CodeIfMatchRequired, CodeStale, storage.CodeVersion, storage.CodeMigration}, locks...)

	register(a, authz.UploadMigrationParallelWorkbook, huma.Operation{
		OperationID: "uploadMigrationParallelWorkbook", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/parallel/workbook",
		Tags: []string{"Migration"}, DefaultStatus: http.StatusAccepted, Summary: "Upload the parallel month's workbook",
		Description: "M5 (§17.1, FR-MIG-02; rev 0.268): uploads the workbook as it stands at the end of the parallel month, recorded in both " +
			"the workbook and Fyniq, as multipart/form-data, once the import is committed (MIGRATION-14) and its parity report signed " +
			"(MIGRATION-16). It is an untrusted upload, checked and kept as M1's is, and validated in the background: read and reported, " +
			"without a mapping, as it goes into the books the import made. The answer is 202 with the step and the operation to poll; then " +
			"read getMigrationParallelReport, and once valid, getMigrationParallel. Uploading again validates the new workbook as a new " +
			"attempt. Needs If-Match with the migration's ETag. Side effects: the file, the step, the job, the audit event " +
			"migration.workbook_received, and later migration.validated. Idempotent with Idempotency-Key; Prefer: return=preview checks the " +
			"file and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeUnavailable,
		migration.CodeCommit, migration.CodeParallel}, append(writes, EvidenceCodes...)...),
		func(ctx context.Context, in *migrationUploadInput) (*migrationUploadOutput, error) {
			return a.uploadWorkbook(ctx, in, storage.StepParallel, parallelReady)
		})
	if op := a.OpenAPI().Paths["/v1/migrations/{migration_id}/parallel/workbook"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"PK… (the workbook's bytes)"}
		}
	}

	register(a, authz.GetMigrationParallelReport, huma.Operation{
		OperationID: "getMigrationParallelReport", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/parallel/report", Tags: []string{"Migration"},
		Summary: "Get the parallel month's validation report",
		Description: "M5's latest validation report (rev 0.268): the parallel month's workbook as read, each sheet's rows and every problem with " +
			"its sheet, row and column, what is wrong and how to fix it, as M1's report gives them; an error leaves the workbook invalid, " +
			"and the parallel month's report reads the latest valid one. Before one is uploaded there is none to read. Read-only; no side " +
			"effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *MigrationPath) (*migrationReportOutput, error) {
		id, err := parseID("migration_id", in.MigrationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &migrationReportOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := migrationRun(ctx, u, id); err != nil {
				return err
			}
			steps, err := u.Migrations().Steps(ctx, id)
			if err != nil {
				return err
			}
			var last *storage.MigrationStep
			for i := range steps {
				if steps[i].Step == storage.StepParallel {
					last = &steps[i]
				}
			}
			if last == nil {
				return problem(CodeNotFound, "No workbook of the parallel month has been uploaded yet.",
					"Upload it with uploadMigrationParallelWorkbook once the parity report is signed.")
			}
			out.Body, err = migrationReportOf(*last)
			return err
		})
		return out, err
	})

	register(a, authz.GetMigrationParallel, huma.Operation{
		OperationID: "getMigrationParallel", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/parallel", Tags: []string{"Migration"},
		Summary: "Get the parallel month's reconciliation",
		Description: "The parallel month's report (§17.1 M5, FR-MIG-02; rev 0.268): the latest valid workbook of M5 reconciled with Fyniq's " +
			"books the same way as M4, through that workbook's as-of: on the history each account's opening and each month's movement and " +
			"closing; on a clean start each opening against the cut-over's Carry Forward, and each month from the books start, its closing " +
			"the Carry Forward balance and the workbook's movements since; each contact's open total, and the pay runs the import brought. " +
			"Its lines are the difference register's, shared with M4: a decision counts while its figures are the report's. sha256, the " +
			"ETag, is what a decision and the sign-off are made on. Before the import is committed, status not_committed; before a valid " +
			"workbook of the parallel month, not_started; neither holds figures. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeUnavailable}, locks...),
		func(ctx context.Context, in *MigrationPath) (*migrationReconciliationOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			src, err := a.parallelSourceOf(ctx, p.Organisation, id)
			switch {
			case errors.Is(err, errNotCommitted):
				return &migrationReconciliationOutput{Body: notCommittedReport()}, nil
			case errors.Is(err, errNotStarted):
				r := notCommittedReport()
				r.Status = "not_started"
				return &migrationReconciliationOutput{Body: r}, nil
			case err != nil:
				return nil, err
			}
			var r MigrationReconciliation
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				r, err = reportIn(ctx, u, id, src)
				return err
			}); err != nil {
				return nil, err
			}
			return &migrationReconciliationOutput{ETag: `"` + r.ReportSHA256 + `"`, Body: r}, nil
		})

	register(a, authz.DecideMigrationParallelDifference, huma.Operation{
		OperationID: "decideMigrationParallelDifference", Method: http.MethodPatch, Path: "/v1/migrations/{migration_id}/parallel/register/{line}",
		Tags: []string{"Migration"}, Summary: "Decide a line of the parallel month's register",
		Description: "The Accountant decides a line of the parallel month's report (§17.1 M5, FR-MIG-02; rev 0.268) as decideMigrationDifference " +
			"does M4's: a, b or c (a workbook defect with a note), kept with the line's figures, stale once they move. The register is " +
			"shared: a line M4 decided on the same figures needs no decision again. A line the report does not have is MIGRATION-15; before " +
			"the import is committed, MIGRATION-14; before a valid workbook of the parallel month, MIGRATION-16. Needs If-Match with the " +
			"report's ETag. Side effects: the decision and the audit event migration.difference_decided. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{migration.CodeCommit, migration.CodeParallel, migration.CodeRegister}, writes...),
		func(ctx context.Context, in *migrationRegisterInput) (*migrationReconciliationWriteOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			src, err := a.parallelSource(ctx, id)
			if err != nil {
				return nil, err
			}
			return a.changeReport(ctx, id, in.IdempotencyKey, in.Prefer, in.IfMatch, in, src, decideLine(id, in.Line, in.Body, a.deps.Clock.Now()))
		})

	register(a, authz.SignMigrationParallel, huma.Operation{
		OperationID: "signMigrationParallel", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/parallel/sign-off",
		Tags: []string{"Migration"}, Summary: "Sign the parallel month",
		Description: "The Accountant, stepped up in the last 5 minutes, signs the parallel month's report and its register together (§17.1 M5, " +
			"FR-MIG-02; rev 0.268), over the report's SHA-256, as signMigrationReconciliation does M4's: refused (MIGRATION-15) while a line " +
			"has no decision that counts or Fyniq's checks have never run; a report is signed once. Before the import is committed, " +
			"MIGRATION-14; before a valid workbook of the parallel month, MIGRATION-16. Needs If-Match with the report's ETag. Side effects: " +
			"the sign-off and the audit event migration.parallel_signed, with the ledger revision the report was read at. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeSessionRequired, CodeStepUpRequired, migration.CodeCommit, migration.CodeParallel, migration.CodeRegister}, writes...),
		func(ctx context.Context, in *migrationReconciliationSignInput) (*migrationReconciliationWriteOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			person, err := a.steppedUpPerson(ctx, "Signing the parallel month")
			if err != nil {
				return nil, err
			}
			src, err := a.parallelSource(ctx, id)
			if err != nil {
				return nil, err
			}
			return a.changeReport(ctx, id, in.IdempotencyKey, in.Prefer, in.IfMatch, in, src, signReport(id, person, a.deps.Clock.Now()))
		})
}
