package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the commit (§17.1 M3, design D3, rev 0.258)

// MigrationCommitRequest is the founders' choice at M3.
type MigrationCommitRequest struct {
	Path string `json:"path" enum:"history,clean_start" doc:"history: the current financial year's history from the books start; clean_start: Carry Forward's balances and open items at the cut-over, the day after the workbook's as-of date (§17.1 M3)." example:"history"`
}

// MigrationCommitKind is how many records of a kind the commit has imported.
type MigrationCommitKind struct {
	Kind string `json:"kind" doc:"What the rows became, such as contact, item or bank_rule." example:"contact"`
	Done int    `json:"done" doc:"How many are imported." example:"16"`
}

// MigrationCommitNext is the next record a batch imports.
type MigrationCommitNext struct {
	Key   string `json:"key" doc:"Its business key from its row, unique in the commit." example:"contact:harbourline logistics pte ltd"`
	Kind  string `json:"kind" doc:"What the row becomes." example:"contact"`
	Sheet string `json:"sheet" doc:"The workbook's sheet." example:"Contacts"`
	Row   int    `json:"row" doc:"Its row; 0 for a sheet's settings as a whole." example:"6"`
}

// MigrationCommitView is the import's commit and how far it is (rev 0.258): none before it is asked for, pending while
// its approval waits, then running and committed.
type MigrationCommitView struct {
	ID          string                `json:"id,omitempty" format:"uuid" doc:"The commit, once approved." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b10"`
	Status      string                `json:"status" enum:"none,pending,running,committed" doc:"none: not asked for; pending: its approval waits for an Owner; running while records are left to import, in batches; committed once every record is." example:"running"`
	Path        string                `json:"path,omitempty" enum:"history,clean_start" doc:"The founders' choice at M3, once asked for." example:"history"`
	ApprovalID  string                `json:"approval_id,omitempty" format:"uuid" doc:"The request that asks for it, and once approved commits it: each imported record names it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6c10"`
	ApprovedBy  string                `json:"approved_by,omitempty" doc:"The Owner who approved it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	ApprovedAt  string                `json:"approved_at,omitempty" format:"date-time" doc:"When." example:"2026-11-13T11:00:00Z"`
	CommittedAt string                `json:"committed_at,omitempty" format:"date-time" doc:"When the last record was imported." example:"2026-11-13T11:04:00Z"`
	StepID      string                `json:"step_id,omitempty" format:"uuid" doc:"The M1 step whose workbook it imports, once approved." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a20"`
	MappingID   string                `json:"mapping_id,omitempty" format:"uuid" doc:"The signed mapping version it imports by, once approved." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6d10"`
	Total       int                   `json:"total" doc:"The records it plans, once asked for." example:"118"`
	Done        int                   `json:"done" doc:"The records imported." example:"50"`
	Kinds       []MigrationCommitKind `json:"kinds" nullable:"false" doc:"The records imported, by kind."`
}

// MigrationBatchView is what a batch imported, and how far the commit is.
type MigrationBatchView struct {
	MigrationCommitView
	Imported int                   `json:"imported" doc:"The records this batch imported; with Prefer: return=preview, none." example:"50"`
	Next     *MigrationCommitNext  `json:"next,omitempty" doc:"The next record to import; absent once the commit is committed."`
	Planned  []MigrationCommitNext `json:"planned,omitempty" doc:"With Prefer: return=preview, the records the next batch would import, in order; before the approval, the first batch of the latest valid workbook by its latest mapping."`
}

type (
	migrationCommitRequestInput struct {
		MigrationPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body MigrationCommitRequest
	}
	migrationCommitRequestOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     Approval
	}
	migrationCommitOutput struct{ Body MigrationCommitView }
	migrationRunInput     struct {
		MigrationPath
		PreferHeader
	}
	migrationRunOutput struct {
		Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was imported (FR-INT-14)."`
		Body    MigrationBatchView
	}
)

// migrationCommitView is a commit with its records imported.
func migrationCommitView(c storage.MigrationCommit, records []storage.MigrationRecord) MigrationCommitView {
	v := MigrationCommitView{ID: c.ID.String(), Status: c.Status, Path: c.Path, ApprovalID: c.ApprovalID.String(), ApprovedBy: c.ApprovedBy,
		ApprovedAt: at(c.ApprovedAt), StepID: c.StepID.String(), MappingID: c.MappingID.String(), Total: c.Records, Done: len(records),
		Kinds: []MigrationCommitKind{}}
	if c.CommittedAt != nil {
		v.CommittedAt = at(*c.CommittedAt)
	}
	index := map[string]int{}
	for _, r := range records {
		i, ok := index[r.Kind]
		if !ok {
			i = len(v.Kinds)
			index[r.Kind] = i
			v.Kinds = append(v.Kinds, MigrationCommitKind{Kind: r.Kind})
		}
		v.Kinds[i].Done++
	}
	return v
}

func nextOf(r *migration.Record) *MigrationCommitNext {
	if r == nil {
		return nil
	}
	return &MigrationCommitNext{Key: r.Key, Kind: r.Kind, Sheet: r.Sheet, Row: r.Row}
}

// committable is what a commit request imports: the migration's latest workbook, valid, and its latest mapping, signed,
// as read now; MIGRATION-14 when there is no such pair, or the migration is committed already.
type committable struct {
	step    storage.MigrationStep
	file    storage.MigrationFile
	mapping mappingState
}

func migrationCommittable(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) (committable, error) {
	var c committable
	if _, err := u.Migrations().Commit(ctx, run); err == nil {
		return c, problem(migration.CodeCommit, "The workbook is imported already: the migration has its commit.",
			"Follow the commit with getMigrationCommit; a rejected import starts again in a new organisation.")
	} else if !errors.Is(err, ports.ErrNotFound) {
		return c, err
	}
	steps, err := u.Migrations().Steps(ctx, run)
	if err != nil {
		return c, err
	}
	var last *storage.MigrationStep
	for i := range steps {
		if steps[i].Step == storage.StepWorkbook {
			last = &steps[i]
		}
	}
	if last == nil || last.State != storage.StepValid {
		return c, problem(migration.CodeCommit, "The migration's latest workbook is not valid, so there is nothing to import.",
			"Upload the frozen workbook, and wait until its validation report holds no error.")
	}
	c.step = *last
	s, err := migrationMappingOf(ctx, u, run)
	if err != nil {
		return c, err
	}
	if s.signed == nil || s.kept.StepID != last.ID {
		return c, problem(migration.CodeCommit, "The mapping of the latest workbook is not signed off.",
			"The Accountant reviews the mapping and signs it (signMigrationMapping), then ask again.")
	}
	c.mapping = s
	files, err := u.Migrations().Files(ctx, run)
	for _, f := range files {
		if f.ID == last.WorkbookFileID {
			c.file = f
		}
	}
	return c, err
}

// migrationCommitState is the import's commit as the API shows it: the commit once approved, else the request that
// waits for its approval, else none.
func (a *API) migrationCommitState(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) (MigrationCommitView, error) {
	c, err := u.Migrations().Commit(ctx, run)
	if err == nil {
		records, err := u.Migrations().Records(ctx, c.ID)
		return migrationCommitView(c, records), err
	}
	if !errors.Is(err, ports.ErrNotFound) {
		return MigrationCommitView{}, err
	}
	v := MigrationCommitView{Status: "none", Kinds: []MigrationCommitKind{}}
	pending, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return v, err
	}
	now := a.deps.Clock.Now()
	for _, r := range pending {
		if r.ObjectType != approval.ObjectMigrationCommit || r.ObjectKey != run.String() || approval.StatusAt(requestOf(r), now) != approval.Pending {
			continue
		}
		s, err := storedOf(r)
		if err != nil {
			return v, err
		}
		total, _ := strconv.Atoi(fmt.Sprint(s.Execute["records"]))
		v.Status, v.ApprovalID, v.Path, v.Total = "pending", r.ID.String(), fmt.Sprint(s.Execute["path"]), total
	}
	return v, nil
}

// migrationFixed refuses a change to the workbook or the mapping once the migration has a commit (MIGRATION-14): the
// database refuses it too (FY327).
func migrationFixed(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) error {
	_, err := u.Migrations().Commit(ctx, run)
	switch {
	case err == nil:
		return problem(migration.CodeCommit, "The import is committed: its workbook and mapping are fixed.",
			"A rejected import starts again in a new organisation, with the corrected workbook.")
	case errors.Is(err, ports.ErrNotFound):
		return nil
	}
	return err
}

// migrationCommitDependencies are what a commit's envelope holds besides its change (FR-APR-05): the migration's status
// and whether it has a commit, the workbook and its report, and the signed mapping; a new upload or mapping version
// voids the request.
func migrationCommitDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	r, err := u.Migrations().Run(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	status := r.Status
	if _, err := u.Migrations().Commit(ctx, id); err == nil {
		status += "/committed"
	} else if !errors.Is(err, ports.ErrNotFound) {
		return 0, nil, err
	}
	deps := map[string]string{"status": status, "workbook": "", "mapping": ""}
	steps, err := u.Migrations().Steps(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	for _, s := range steps {
		if s.Step == storage.StepWorkbook {
			deps["workbook"] = s.ID.String() + " " + s.State + " " + s.ReportSHA256
		}
	}
	m, err := u.Migrations().Mapping(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, deps, nil
	}
	if err != nil {
		return 0, nil, err
	}
	signed := "unsigned"
	offs, err := u.Migrations().SignOffs(ctx, id)
	for _, o := range offs {
		if o.MappingID != nil && *o.MappingID == m.ID {
			signed = "signed " + o.SubjectSHA256
		}
	}
	deps["mapping"] = m.ID.String() + " " + m.SHA256 + " " + signed
	return m.Version, deps, err
}

// readWorkbook is a migration's workbook as kept, read into the model and checked against its hash.
func (a *API) readWorkbook(ctx context.Context, f storage.MigrationFile) (*migration.Workbook, error) {
	if a.deps.Staging == nil {
		return nil, problem(CodeUnavailable, "The workbook cannot be read here: no file store is configured.",
			"Ask your administrator to configure it (FR-DOC-01).")
	}
	rc, err := a.deps.Staging.Get(ctx, f.StorageKey)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(rc, migration.MaxBytes+1))
	_ = rc.Close()
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.SHA256 {
		return nil, problem(migration.CodeCommit, "The workbook kept is not the one uploaded: its SHA-256 differs.",
			"Start again in a new organisation with the frozen workbook.")
	}
	w, err := migration.Read(data)
	if err != nil {
		return nil, err
	}
	w.Freeze()
	return w, nil
}

// executeMigrationCommit records the commit its approval approved: its batches then import the records (rev 0.258).
func (a *API) executeMigrationCommit(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored, signer uuid.UUID) error {
	ids := map[string]uuid.UUID{}
	for _, k := range []string{"migration_id", "step_id", "mapping_id"} {
		id, err := uuid.Parse(fmt.Sprint(s.Execute[k]))
		if err != nil {
			return err
		}
		ids[k] = id
	}
	records, err := strconv.Atoi(fmt.Sprint(s.Execute["records"]))
	if err != nil {
		return err
	}
	_, err = u.Migrations().StartCommit(ctx, storage.MigrationCommit{RunID: ids["migration_id"], ApprovalID: r.ID, Path: fmt.Sprint(s.Execute["path"]),
		StepID: ids["step_id"], MappingID: ids["mapping_id"], Records: records, ApprovedBy: "user:" + signer.String(), ApprovedAt: a.deps.Clock.Now()})
	return err
}

// importProblem is a record a batch could not import, as the API answers it: its sheet and row, what is wrong and how
// to fix it (MIGRATION-13).
func importProblem(e *migration.ImportError) error {
	r := e.Record
	where := fmt.Sprintf("%s row %d", r.Sheet, r.Row)
	if r.Row == 0 {
		where = "The " + r.Sheet + " sheet"
	}
	msg, fix := e.Err.Error(), "Correct the row in the workbook and import it again into a new organisation; the records before it stay imported."
	var refused migration.Refused
	var se *storage.Error
	switch {
	case errors.As(e.Err, &refused) && len(refused) > 0:
		msg, fix = refused[0].Field+" "+refused[0].Message, refused[0].Fix
	case errors.As(e.Err, &se):
		msg = se.Message
		if se.Hint != "" {
			fix = se.Hint
		}
	}
	return problem(migration.CodeImport, fmt.Sprintf("%s (%s, %s) could not be imported: %s.", where, r.Kind, r.Key, msg), sentence(fix))
}

func registerMigrationCommit(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.RequestMigrationCommit, huma.Operation{
		OperationID: "requestMigrationCommit", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/commit", Tags: []string{"Migration"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to commit the import",
		Description: "M3 (§17.1, FR-MIG-01, rev 0.258): asks to import the workbook by the founders' choice, its history or a clean start, as one " +
			"approval request over the latest workbook, its validation report and the signed mapping. An Owner who did not ask approves it, " +
			"stepped up (FM-1); approving records the commit, which runCommit then imports in batches. A workbook that is not valid, a " +
			"mapping not signed off, or a migration committed already is MIGRATION-14, and so is a workbook two of whose rows would import " +
			"as one record. A new upload or mapping version before the approval voids it. Needs a person signed in who passed step-up, and " +
			"If-Match with the mapping's ETag. Side effects: the approval request, its notifications, audit events. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType,
		CodeInvalid, CodeUnavailable, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, approval.CodePending, migration.CodeCommit}, locks...),
		func(ctx context.Context, in *migrationCommitRequestInput) (*migrationCommitRequestOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			// The workbook is read outside the request's transaction: it is checked against its hash, and the request
			// checks again that it is the latest valid one.
			var c committable
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				if _, err = migrationRun(ctx, u, id); err != nil {
					return err
				}
				c, err = migrationCommittable(ctx, u, id)
				return err
			}); err != nil {
				return nil, err
			}
			w, err := a.readWorkbook(ctx, c.file)
			if err != nil {
				return nil, err
			}
			if in.Body.Path == storage.PathCleanStart && len(w.CarryForward.Balances) == 0 {
				return nil, problem(migration.CodeCommit, "The workbook's Carry Forward sheet holds no balances, so a clean start has nothing to start from.",
					"Import its history instead, or fill in Carry Forward in the workbook and upload it again.")
			}
			plan, probs := w.Plan(c.mapping.mapping, in.Body.Path)
			if len(probs) > 0 {
				return nil, problem(migration.CodeCommit, fmt.Sprintf("%s row %d: %s.", probs[0].Sheet, probs[0].Row, probs[0].Message), sentence(probs[0].Fix))
			}
			kinds := map[string]int{}
			var order []string
			for _, r := range plan {
				if kinds[r.Kind] == 0 {
					order = append(order, r.Kind)
				}
				kinds[r.Kind]++
			}
			resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				now, err := migrationCommittable(ctx, u, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, now.mapping.etag()); err != nil {
					return Approval{}, err
				}
				if now.step.ID != c.step.ID || now.mapping.kept.ID != c.mapping.kept.ID {
					return Approval{}, problem(CodeStale, "The workbook or its mapping changed while the request was checked.", "Read the mapping again and ask again.")
				}
				diff := []ApprovalDiff{{Field: "path", After: in.Body.Path}, {Field: "workbook", After: c.file.FileName + " " + c.file.SHA256},
					{Field: "report_sha256", After: c.step.ReportSHA256}, {Field: "mapping", After: fmt.Sprintf("version %d %s", c.mapping.kept.Version, c.mapping.kept.SHA256)},
					{Field: "books_start", After: w.BooksStart(in.Body.Path).Format("2006-01-02")}, {Field: "records", After: strconv.Itoa(len(plan))}}
				for _, k := range order {
					diff = append(diff, ApprovalDiff{Field: "records." + k, After: strconv.Itoa(kinds[k])})
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectMigrationCommit, ObjectKey: id.String(), Operation: approval.OpMigrationCommit,
					Summary: fmt.Sprintf("Import the workbook %s (%s): %d records", c.file.FileName, in.Body.Path, len(plan)),
					Execute: map[string]any{"migration_id": id.String(), "path": in.Body.Path, "step_id": c.step.ID.String(),
						"mapping_id": c.mapping.kept.ID.String(), "records": strconv.Itoa(len(plan))}, Diff: diff})
			})
			if err != nil {
				return nil, err
			}
			return &migrationCommitRequestOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
		})

	register(a, authz.GetMigrationCommit, huma.Operation{
		OperationID: "getMigrationCommit", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/commit", Tags: []string{"Migration"},
		Summary: "Get the commit",
		Description: "The import's commit (§17.1 M3, rev 0.258): none before it is asked for; pending, with its request and path, while an Owner's " +
			"approval waits; then running and committed, with the approval and how many of the records it plans are imported, by kind. " +
			"Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *MigrationPath) (*migrationCommitOutput, error) {
		id, err := parseID("migration_id", in.MigrationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &migrationCommitOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := migrationRun(ctx, u, id); err != nil {
				return err
			}
			out.Body, err = a.migrationCommitState(ctx, u, id)
			return err
		})
		return out, err
	})

	register(a, authz.RunMigrationCommit, exempt(exempt(huma.Operation{
		OperationID: "runMigrationCommit", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/commit:run", Tags: []string{"Migration"},
		Summary: "Import the next batch",
		Description: "Imports the approved commit's next batch (rev 0.258): at most 50 records not yet imported, in the plan's order, each through " +
			"the domain command a person's request uses and in its own transaction with its migration record, which names its business key, " +
			"its sheet and row and the commit, whose approval it carries out. Send it again until the commit is committed: closing the page " +
			"pauses the import, and the next batch resumes at the first record not yet imported. A record that cannot be imported stops the " +
			"batch with MIGRATION-13, naming its row, what is wrong and how to fix it; the records before it stay imported. Before the " +
			"approval it is MIGRATION-14. Side effects: the records, their audit events, migration.record_imported for each, and " +
			"migration.committed after the last. Prefer: return=preview lists the records the next batch would import and imports nothing; " +
			"before the approval, the first batch of the latest valid workbook by its latest mapping, by the path asked for (history when " +
			"none is).",
	}, "Idempotency-Key", "Each record imports in its own transaction under its business key, so a repeat finds those records imported and continues after them (rev 0.258)."),
		"If-Match", "A batch continues after the records already imported, whatever the progress its sender read; two batches at once import each record once, under the commit's lock (rev 0.258)."),
		append([]string{CodeNotFound, CodeUnavailable, migration.CodeCommit, migration.CodeImport}, locks...),
		func(ctx context.Context, in *migrationRunInput) (*migrationRunOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			preview := wantsPreview(in.Prefer)
			var c *storage.MigrationCommit
			var file storage.MigrationFile
			var mapping storage.MigrationMapping
			var state MigrationCommitView
			done := map[string]bool{}
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				if _, err := migrationRun(ctx, u, id); err != nil {
					return err
				}
				if state, err = a.migrationCommitState(ctx, u, id); err != nil {
					return err
				}
				var stepID uuid.UUID
				switch x, err := u.Migrations().Commit(ctx, id); {
				case err == nil:
					c, stepID = &x, x.StepID
					if mapping, err = u.Migrations().MappingVersion(ctx, x.MappingID); err != nil {
						return err
					}
					records, err := u.Migrations().Records(ctx, x.ID)
					if err != nil {
						return err
					}
					for _, r := range records {
						done[r.Key] = true
					}
				case !errors.Is(err, ports.ErrNotFound):
					return err
				case !preview:
					return problem(migration.CodeCommit, "The import is not approved yet, so there is nothing to run.",
						"Ask to commit it with requestMigrationCommit; once an Owner approves it, run its batches.")
				default: // a preview before the approval: the latest valid workbook by its latest mapping
					steps, err := u.Migrations().Steps(ctx, id)
					if err != nil {
						return err
					}
					for _, s := range steps {
						if s.Step == storage.StepWorkbook {
							stepID = s.ID
							if s.State != storage.StepValid {
								stepID = uuid.Nil
							}
						}
					}
					if mapping, err = u.Migrations().Mapping(ctx, id); errors.Is(err, ports.ErrNotFound) || stepID == uuid.Nil || mapping.StepID != stepID {
						return problem(migration.CodeCommit, "The migration has no valid workbook with its mapping, so there is nothing to plan.",
							"Upload the frozen workbook, and wait until its validation report holds no error.")
					} else if err != nil {
						return err
					}
				}
				step, err := u.Migrations().Step(ctx, stepID)
				if err != nil {
					return err
				}
				files, err := u.Migrations().Files(ctx, id)
				for _, f := range files {
					if f.ID == step.WorkbookFileID {
						file = f
					}
				}
				return err
			}); err != nil {
				return nil, err
			}
			view := func(next *migration.Record, imported int) (*migrationRunOutput, error) {
				out := &migrationRunOutput{}
				err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
					v, err := a.migrationCommitState(ctx, u, id)
					out.Body = MigrationBatchView{MigrationCommitView: v, Imported: imported, Next: nextOf(next)}
					return err
				})
				return out, err
			}
			if c != nil && c.Status == storage.CommitCommitted {
				return view(nil, 0)
			}
			w, err := a.readWorkbook(ctx, file)
			if err != nil {
				return nil, err
			}
			var m migration.Mapping
			if err := json.Unmarshal(mapping.Mapping, &m); err != nil {
				return nil, err
			}
			path := cmpOr(state.Path, storage.PathHistory)
			plan, probs := w.Plan(m, path)
			if c != nil && (len(probs) > 0 || len(plan) != c.Records) {
				return nil, problem(migration.CodeCommit, fmt.Sprintf("The workbook now plans %d records; the approval approved %d.", len(plan), c.Records),
					"Start again in a new organisation: the import changed since it was approved.")
			}
			if preview {
				out := &migrationRunOutput{Applied: previewApplied, Body: MigrationBatchView{MigrationCommitView: state}}
				for i := range plan {
					if !done[plan[i].Key] && len(out.Body.Planned) < migration.BatchSize {
						out.Body.Planned = append(out.Body.Planned, *nextOf(&plan[i]))
					}
				}
				if len(out.Body.Planned) > 0 {
					out.Body.Next = &out.Body.Planned[0]
				}
				return out, nil
			}
			audit := storage.AuditFrom(ctx)
			audit.ApprovalIDs = append(audit.ApprovalIDs, c.ApprovalID.String())
			audit.Reason = "The workbook import, approved in " + c.ApprovalID.String() + " (FR-MIG-01)"
			progress, err := migration.Batch(storage.WithAudit(ctx, audit), a.deps.Store, p.Organisation, *c, plan, migration.BatchSize, uploader(ctx), a.deps.Clock.Now)
			var ie *migration.ImportError
			if errors.As(err, &ie) {
				return nil, importProblem(ie)
			}
			if err != nil {
				return nil, err
			}
			return view(progress.Next, progress.Imported)
		})
}
