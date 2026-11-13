package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the generic import (FR-MIG-03, design D9; rev 0.270)

// ImportReport is what a file of the generic import makes, or would make on a dry run: its rows' records, and every
// problem with its row and column, what is wrong and how to fix it.
type ImportReport struct {
	Entity   string             `json:"entity" enum:"contacts,items,accounts,journals,invoices,bills,fx-rates,bank-rules,opening-balances,budgets" doc:"What the file holds." example:"contacts"`
	FileName string             `json:"file_name" doc:"As the uploader named it." example:"contacts.csv"`
	Rows     int                `json:"rows" doc:"The rows read." example:"42"`
	Made     int                `json:"made" doc:"The records made: every row's, or none, as a file is imported whole or not at all; on a dry run, the ones it would make." example:"42"`
	Errors   int                `json:"errors" doc:"The problems that stop the import." example:"0"`
	Problems []MigrationProblem `json:"problems" nullable:"false" doc:"Every problem, with the row and column as the file numbers them."`
}

type (
	genericImportInput struct {
		KeyHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[MigrationUpload]
	}
	genericImportOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a dry run: nothing was saved (FR-INT-14)."`
		Body     ImportReport
	}
	importTemplateInput struct {
		Format string `query:"format" enum:"csv,xlsx" default:"csv" doc:"csv: UTF-8, its header row first; xlsx: a workbook of one sheet named as the workbook's." example:"csv"`
	}
	importTemplateFile struct {
		ContentType        string `header:"Content-Type" doc:"text/csv or the .xlsx type."`
		ContentDisposition string `header:"Content-Disposition" doc:"attachment, with the template's name."`
		Body               []byte
	}
)

// importPolicies are each entity's operations: the template is read under its own right V, the import made under C,
// as entering its records by hand is (FR-MIG-03: each entity needs its own Appendix D row's C).
var importPolicies = map[string][2]authz.Policy{
	"contacts":         {authz.GetContactsTemplate, authz.ImportContacts},
	"items":            {authz.GetItemsTemplate, authz.ImportItems},
	"accounts":         {authz.GetAccountsTemplate, authz.ImportAccounts},
	"journals":         {authz.GetJournalsTemplate, authz.ImportJournals},
	"invoices":         {authz.GetInvoicesTemplate, authz.ImportInvoices},
	"bills":            {authz.GetBillsTemplate, authz.ImportBills},
	"fx-rates":         {authz.GetFxRatesTemplate, authz.ImportFxRates},
	"bank-rules":       {authz.GetBankRulesTemplate, authz.ImportBankRules},
	"opening-balances": {authz.GetOpeningBalancesTemplate, authz.ImportOpeningBalances},
	"budgets":          {authz.GetBudgetsTemplate, authz.ImportBudgets},
}

// importNames are each entity's words in the contract.
var importNames = map[string]string{"contacts": "contacts", "items": "items", "accounts": "accounts of the chart",
	"journals": "journals, as drafts", "invoices": "invoices and credit notes, as drafts", "bills": "bills and vendor credits, as drafts",
	"fx-rates": "monthly FX rates", "bank-rules": "bank rules", "opening-balances": "opening balances, as a draft", "budgets": "budgets, as drafts"}

func registerImports(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	for _, entity := range migration.ImportEntities() {
		policies, words := importPolicies[entity], importNames[entity]
		register(a, policies[0], huma.Operation{
			OperationID: policies[0].Operation, Method: http.MethodGet, Path: "/v1/imports/" + entity + "/template", Tags: []string{"Import"},
			Summary: "Get the template for " + words,
			Description: "The template for importing " + words + " (FR-MIG-03, design D9; rev 0.270): the workbook's sheet for them, its input " +
				"columns as the header and no row, as CSV or as an .xlsx workbook. Fill a row in per record; a date is YYYY-MM-DD in CSV, and an " +
				"amount a plain decimal. The workbook's own sheet, as it is, imports too. Read-only; no side effects.",
			Responses: map[string]*huma.Response{"200": {Description: "The template, as an attachment.",
				Content: map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary",
					Examples: []any{`"Name","Customer / supplier","Email"…`}}}}}},
		}, locks, func(ctx context.Context, in *importTemplateInput) (*importTemplateFile, error) {
			b, err := migration.Template(entity, in.Format, a.deps.Clock.Now())
			if err != nil {
				return nil, err
			}
			out := &importTemplateFile{ContentType: "text/csv; charset=utf-8", Body: b,
				ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": entity + "-template." + in.Format})}
			if in.Format == "xlsx" {
				out.ContentType = evidence.XLSX
			}
			return out, nil
		})

		register(a, policies[1], huma.Operation{
			OperationID: policies[1].Operation, Method: http.MethodPost, Path: "/v1/imports/" + entity, Tags: []string{"Import"},
			DefaultStatus: http.StatusCreated, Summary: "Import " + words,
			Description: "Imports " + words + " from a file filled in their template (FR-MIG-03, design D9; rev 0.270), as multipart/form-data: " +
				"CSV or an .xlsx workbook, at most 1,000 rows. The file is an untrusted upload: its type is decided from its content and it is " +
				"scanned for malware (FR-DOC-08). Each row becomes its record through the command that enters it by hand, with its checks, in " +
				"any organisation; a journal, an invoice or a bill is made a draft, keeping the file's number, and is posted later through its " +
				"own approvals (rev 0.271); an invoices .xlsx file itemises an invoice on its Doc Lines sheet, as the workbook does. Opening " +
				"balances, each on its account's own side, are saved as the draft the opening balances page posts; each financial year's " +
				"budget is a draft; a bank rule goes after the organisation's own and suggests until an approver makes it auto-apply " +
				"(rev 0.272). A file is " +
				"imported whole or not at all, so a row refused refuses the file (MIGRATION-17, every problem with " +
				"its row and column). Prefer: return=preview is the dry run: the same report, every problem listed, nothing saved. Side " +
				"effects: the records and their audit events, which name the file, and the run's own, import.run. Idempotent with Idempotency-Key.",
		}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid,
			CodeUnavailable, migration.CodeGeneric, storage.CodeVersion}, append(locks, EvidenceCodes...)...),
			func(ctx context.Context, in *genericImportInput) (*genericImportOutput, error) {
				return a.importFile(ctx, entity, in)
			})
		if op := a.OpenAPI().Paths["/v1/imports/"+entity].Post; op != nil && op.RequestBody != nil {
			if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
				mt.Schema.Properties["file"].Examples = []any{"\"Name\",\"Customer / supplier\",… (the file's bytes)"}
			}
		}
	}
}

// importFile imports an entity's file, or dry-runs it.
func (a *API) importFile(ctx context.Context, entity string, in *genericImportInput) (*genericImportOutput, error) {
	if a.deps.Scanner == nil {
		return nil, problem(CodeUnavailable, "Files cannot be imported here: no malware scanner is configured.", "Ask your administrator to configure it (FR-DOC-01).")
	}
	up := in.RawBody.Data()
	if up == nil || up.File.File == nil {
		return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.file", Message: "is missing", Fix: "Send the file in the form field file"}})
	}
	defer up.File.Close()
	data, err := io.ReadAll(io.LimitReader(up.File, migration.MaxBytes+1))
	if err != nil {
		return nil, problem(CodeMalformed, "The file could not be read in full.", "Send it again.")
	}
	file, problems, err := evidence.Check(ctx, a.deps.Scanner, up.File.Filename, data)
	if err != nil {
		logFailure(ctx, err)
		return nil, problem(CodeUnavailable, "The malware scan is not available, so the file was not read.", "Try again in a few minutes.")
	}
	var bad []FieldProblem
	for _, x := range problems {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) == 0 && file.MediaType != evidence.XLSX && file.MediaType != evidence.CSV && file.MediaType != evidence.Text {
		bad = append(bad, FieldProblem{Code: evidence.CodeType, Field: "body.file", Message: "is neither CSV nor an .xlsx workbook",
			Fix: "Save the template as CSV (UTF-8) or as an .xlsx workbook"})
	}
	if len(bad) > 0 {
		return nil, invalid(bad)
	}
	p, _ := authn.From(ctx)
	now := a.deps.Clock.Now()
	preview := wantsPreview(in.Prefer)
	audit := storage.AuditFrom(ctx)
	audit.Reason = fmt.Sprintf("Imported from %s (FR-MIG-03)", file.Name)
	ctx = storage.WithAudit(ctx, audit)
	request := struct{ Entity, SHA256 string }{entity, file.SHA256}
	w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ImportReport, error) {
		base, err := baseOf(ctx, u)
		if err != nil {
			return ImportReport{}, err
		}
		wb, err := migration.ReadImport(entity, file.Data, base)
		var refused migration.Refused
		if errors.As(err, &refused) {
			return ImportReport{}, problem(migration.CodeGeneric, refused[0].Message, refused[0].Fix)
		}
		if err != nil {
			return ImportReport{}, err
		}
		res, err := migration.RunImport(ctx, u, entity, wb, base, &migration.Env{Now: now, Store: a.deps.Store, Org: p.Organisation, Clock: a.deps.Clock,
			Person: p.UserID})
		if err != nil {
			return ImportReport{}, err
		}
		r := ImportReport{Entity: entity, FileName: file.Name, Rows: res.Rows, Made: len(res.Made), Problems: []MigrationProblem{}}
		for _, x := range res.Problems {
			if x.Severity == migration.Error {
				r.Errors++
			}
			r.Problems = append(r.Problems, MigrationProblem{Sheet: x.Sheet, Row: x.Row, Column: x.Column, Severity: string(x.Severity), Code: x.Code,
				Message: x.Message, Fix: x.Fix})
		}
		if r.Errors > 0 {
			if preview { // the dry run lists them; the preview saves nothing either way
				r.Made = 0
				return r, nil
			}
			var first MigrationProblem
			fields := make([]FieldProblem, 0, min(len(r.Problems), 50))
			for _, x := range r.Problems {
				if x.Severity != string(migration.Error) {
					continue
				}
				if len(fields) == 0 {
					first = x
				}
				if len(fields) < 50 {
					fields = append(fields, FieldProblem{Field: fmt.Sprintf("rows[%d].%s", x.Row, x.Column), Code: x.Code, Message: x.Message, Fix: x.Fix})
				}
			}
			return r, problem(migration.CodeGeneric, fmt.Sprintf("%d problems stop the import; the first, on row %d: %s.", r.Errors, first.Row, first.Message),
				"Fix the rows in the file and import it again: nothing was imported.", fields...)
		}
		// The run itself, beside each record's own event (design D13): the file, its hash and what it made.
		return r, u.Audit().Record(ctx, storage.AuditRecord{Action: "import.run", ObjectType: "import", ObjectID: uuid.Must(uuid.NewV7()),
			Changes: map[string][2]any{"file": {nil, map[string]any{"entity": entity, "name": file.Name, "sha256": file.SHA256, "rows": r.Rows,
				"made": r.Made}}}})
	})
	if err != nil {
		return nil, err
	}
	return &genericImportOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
}
