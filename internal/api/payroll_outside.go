package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payrollimport"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ payroll that Nemura does not run (FR-PAY-15, FR-PPL-05, FR-PPL-09; p1-payroll-link tasks 7.1, 7.2)
//
// Two controls stand where payroll is not run in Nemura. The go-live gate (FR-PAY-15, G4-009) holds a pay run back from approval for payment until the organisation has recorded, as evidence, how
// that month's statutory outputs are met: itemised payslips, key employment terms, CPF, SDL and SHG submission and payment, and where they apply the Auto-Inclusion Scheme and IR21; a salary batch
// is asked for only with every item met. And a provider's journal or a file import is only a draft journal (the external system's approval cannot be verified), idempotent per source system, pay
// period and version, previewed before it is saved and approved by a Controller or Accountant under Journals; it is refused while payroll lives in Nemura (the link is live and the source of payroll
// is Nemura), so a pay run is never posted twice from two sources.

// PayrollArrangement is how one item of the gate is met from a month.
type PayrollArrangement struct {
	ID            string `json:"id" format:"uuid" doc:"The arrangement." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c70"`
	Item          string `json:"item" enum:"payslips,kets,cpf_sdl_shg,ais,ir21" doc:"What it covers." example:"payslips"`
	Method        string `json:"method" enum:"nemura,provider,manual,not_applicable" doc:"nemura: Nemura issues it (payslips and KETs); provider: a named payroll provider; manual: a documented manual process; not_applicable: the Auto-Inclusion Scheme or IR21 does not apply, with the reason." example:"provider"`
	Provider      string `json:"provider,omitempty" doc:"The provider's name." example:"Talenox"`
	Note          string `json:"note,omitempty" doc:"How it is met, or why it does not apply." example:"Fewer than five employees: the AIS does not yet apply"`
	EvidenceID    string `json:"evidence_id,omitempty" format:"uuid" doc:"The evidence file that shows it (FR-DOC-05): a contract with the provider, the documented process, a bank advice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c71"`
	EffectiveFrom string `json:"effective_from" doc:"The first wage month it holds for, YYYY-MM." example:"2026-11"`
	RecordedBy    string `json:"recorded_by" doc:"Who recorded it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	RecordedAt    string `json:"recorded_at" format:"date-time" doc:"When." example:"2026-11-20T09:00:00Z"`
}

// PayrollArrangements are the arrangements in force for a wage month and what is still missing.
type PayrollArrangements struct {
	Month   string               `json:"month" doc:"The wage month, YYYY-MM." example:"2026-11"`
	InForce []PayrollArrangement `json:"in_force" nullable:"false" doc:"The arrangement of each item in force."`
	Missing []string             `json:"missing" nullable:"false" doc:"The items with none: while any is missing a pay run that pays a person is not approved for payment (G4-009)." example:"[\"kets\"]"`
	Met     bool                 `json:"met" doc:"Every item has an arrangement in force: the gate is open." example:"false"`
}

// PayrollArrangementChange records how an item is met.
type PayrollArrangementChange struct {
	Item          string `json:"item" enum:"payslips,kets,cpf_sdl_shg,ais,ir21" doc:"What it covers." example:"payslips"`
	Method        string `json:"method" enum:"nemura,provider,manual,not_applicable" doc:"How it is met. nemura is for payslips and KETs only and needs an active link whose payroll is Nemura's." example:"provider"`
	Provider      string `json:"provider,omitempty" maxLength:"120" doc:"The provider's name, with method provider." example:"Talenox"`
	Note          string `json:"note,omitempty" maxLength:"500" doc:"How it is met; with not_applicable, why." example:"Fewer than five employees"`
	EvidenceID    string `json:"evidence_id,omitempty" format:"uuid" doc:"A file of evidence on a record of the organisation: required for every method but nemura." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c71"`
	EffectiveFrom string `json:"effective_from" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The first wage month it holds for." example:"2026-11"`
}

// PayrollImportRequest is a provider's journal for a pay period, as a CSV.
type PayrollImportRequest struct {
	SourceSystem  string                 `json:"source_system" pattern:"^[a-z][a-z0-9_-]{0,39}$" doc:"The system it comes from, such as talenox or file." example:"talenox"`
	Period        string                 `json:"period" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The wage month the journal is for." example:"2026-10"`
	Version       int                    `json:"version" minimum:"1" doc:"The period's version: a later one states the period's full figures again and drafts only the difference from the version before. The same version again with the same file is a repeat; with another file it is refused." example:"1"`
	Mapping       *payrollimport.Mapping `json:"mapping,omitempty" doc:"The file's column headers, if not Fyniq's own: account, department (optional), debit, credit, memo (optional)."`
	ContentBase64 string                 `json:"content_base64" maxLength:"4000000" doc:"The CSV, UTF-8, comma separated, with a header row, by account and department: no column may name a person (FR-PAY-13)." example:"YWNjb3VudCxkZXBhcnRtZW50LGRlYml0LGNyZWRpdA=="`
}

// PayrollImportLine is a line of the draft, by account and department.
type PayrollImportLine struct {
	AccountID    string `json:"account_id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c72"`
	DepartmentID string `json:"department_id,omitempty" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c73"`
	Amount       Money  `json:"amount" doc:"Signed: a debit positive, a credit negative."`
}

// PayrollImport is what an import made, or would make: the draft journal, its lines and what it changes over the version before.
type PayrollImport struct {
	ID           string              `json:"id,omitempty" format:"uuid" doc:"The import, absent in a preview." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c74"`
	SourceSystem string              `json:"source_system" example:"talenox"`
	Period       string              `json:"period" example:"2026-10"`
	Version      int                 `json:"version" example:"1"`
	Outcome      string              `json:"outcome" enum:"drafted,unchanged,preview,nothing_changed" doc:"drafted: the draft journal was made; unchanged: the same file for the same version was imported before; nothing_changed: the version states the figures already drafted, so there is nothing to draft; preview: nothing was saved." example:"drafted"`
	JournalID    string              `json:"journal_id,omitempty" format:"uuid" doc:"The draft journal, to be approved under Journals." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c75"`
	PostingDate  string              `json:"posting_date" format:"date" doc:"The draft's date: the wage month's last day, or today when that month is closed." example:"2026-10-31"`
	Rows         int                 `json:"rows" doc:"How many rows the file had." example:"14"`
	Lines        []PayrollImportLine `json:"lines" nullable:"false" doc:"What the draft holds: the version's difference from the one before, by account and department."`
	SHA256       string              `json:"file_sha256" doc:"The file's SHA-256, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

type (
	arrangementsInput struct {
		Month string `query:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The wage month, YYYY-MM; the current month when left out." example:"2026-11"`
	}
	arrangementsOutput  struct{ Body PayrollArrangements }
	setArrangementInput struct {
		KeyHeader
		PreferHeader
		Body PayrollArrangementChange
	}
	setArrangementOutput struct{ Body PayrollArrangement }
	payrollImportInput   struct {
		KeyHeader
		PreferHeader
		Body PayrollImportRequest
	}
	payrollImportOutput struct {
		Status  int
		Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved."`
		Body    PayrollImport
	}
	importListOutput struct{ Body PayrollImportPage }
	importListInput  struct{ PageQuery }
)

// PayrollImportPage is the imports, newest period first.
type PayrollImportPage struct {
	Items      []PayrollImportRow `json:"items" nullable:"false"`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"MTA"`
}

// PayrollImportRow is an import as the list shows it.
type PayrollImportRow struct {
	ID           string `json:"id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c74"`
	SourceSystem string `json:"source_system" example:"talenox"`
	Period       string `json:"period" example:"2026-10"`
	Version      int    `json:"version" example:"1"`
	JournalID    string `json:"journal_id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c75"`
	Lines        int    `json:"lines" doc:"How many lines its draft holds." example:"6"`
	CreatedAt    string `json:"created_at" format:"date-time" example:"2026-11-20T09:00:00Z"`
}

func arrangementOf(a storage.PayrollArrangement) PayrollArrangement {
	out := PayrollArrangement{ID: a.ID.String(), Item: a.Item, Method: a.Method, Provider: a.Provider, Note: a.Note, EffectiveFrom: a.EffectiveFrom.Format("2006-01"), RecordedBy: a.RecordedBy,
		RecordedAt: a.RecordedAt.UTC().Format(time.RFC3339)}
	if a.EvidenceID != uuid.Nil {
		out.EvidenceID = a.EvidenceID.String()
	}
	return out
}

func arrangementMonth(s string, today time.Time) (time.Time, error) {
	if s == "" {
		return time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	m, err := time.Parse("2006-01", s)
	if err != nil {
		return m, problem(CodeInvalid, "The month is not YYYY-MM.", "Send the month as YYYY-MM")
	}
	return m, nil
}

// payrollGate is the go-live gate for a wage month: the items of FR-PAY-15 with no arrangement in force. Where payroll lives in Nemura with the link active, Nemura issues the payslips and the KETs
// (FR-PAY-08, superseded for Fyniq): those two items are met by the link, so only the statutory submissions and the schemes need their own.
func payrollGate(ctx context.Context, u storage.UnitOfWork, month time.Time) ([]string, error) {
	have, err := u.PayrollOutside().Arrangements(ctx, month)
	if err != nil {
		return nil, err
	}
	met := map[string]bool{}
	for _, a := range have {
		met[a.Item] = true
	}
	if inNemura, err := payrollInNemura(ctx, u); err != nil {
		return nil, err
	} else if inNemura {
		met[storage.ArrangePayslips], met[storage.ArrangeKETs] = true, true
	}
	var missing []string
	for _, item := range payrollimport.GateItems {
		if !met[item] {
			missing = append(missing, item)
		}
	}
	return missing, nil
}

// payrollInNemura says payroll lives in Nemura: the link is active and the source of payroll is Nemura's.
func payrollInNemura(ctx context.Context, u storage.UnitOfWork) (bool, error) {
	l, err := u.PeopleLinks().Live(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil || l.State != storage.LinkActive {
		return false, err
	}
	sources, err := u.PeopleLinks().Sources(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range sources {
		if s.Feature == storage.FeaturePayroll {
			return s.Source == storage.SourceNemura, nil
		}
	}
	return false, nil
}

type chartResolver struct {
	accounts map[string]uuid.UUID
	depts    map[string]uuid.UUID
}

func (c chartResolver) Account(code string) (uuid.UUID, bool) {
	id, ok := c.accounts[code]
	return id, ok
}
func (c chartResolver) Department(code string) (uuid.UUID, bool) {
	id, ok := c.depts[code]
	return id, ok
}

// payrollItemExamples gives the examples a schema's arrays of strings take on their items, and the column headers of an import's mapping, which come from a domain type that carries no documentation tags.
func payrollItemExamples(a *API) {
	schemas := a.OpenAPI().Components.Schemas.Map()
	items := func(schema, field string, example any) {
		if s := schemas[schema]; s != nil {
			if p := s.Properties[field]; p != nil && p.Items != nil && len(p.Items.Examples) == 0 {
				p.Items.Examples = []any{example}
			}
		}
	}
	items("PayrollArrangements", "missing", "kets")
	items("SalaryBankMatch", "line_ids", "01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c11")
	if s := schemas["Mapping"]; s != nil {
		for field, example := range map[string]string{"account": "Account", "department": "Department", "debit": "Debit", "credit": "Credit", "memo": "Memo"} {
			if p := s.Properties[field]; p != nil && len(p.Examples) == 0 {
				p.Examples = []any{example}
			}
		}
	}
}

func registerPayrollOutside(a *API) {
	defer payrollItemExamples(a)
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetPayrollArrangements, huma.Operation{
		OperationID: "getPayrollArrangements", Method: http.MethodGet, Path: "/v1/payroll-arrangements", Tags: []string{"People"},
		Summary: "Show how a month's statutory payroll outputs are met",
		Description: "The arrangement in force for each item of the payroll go-live gate (FR-PAY-15) for a wage month, and what is missing: itemised payslips, key employment terms, CPF, SDL and SHG submission and " +
			"payment, and where they apply the Auto-Inclusion Scheme and IR21. While an item is missing, a pay run that pays a person is not approved for payment (PPL-30, G4-009). Where payroll lives in Nemura " +
			"with the link active, Nemura's issuing of payslips and KETs counts as their arrangement. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *arrangementsInput) (*arrangementsOutput, error) {
		p, _ := authn.From(ctx)
		out := arrangementsOutput{Body: PayrollArrangements{InForce: []PayrollArrangement{}, Missing: []string{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			month, err := arrangementMonth(in.Month, today)
			if err != nil {
				return err
			}
			out.Body.Month = month.Format("2006-01")
			have, err := u.PayrollOutside().Arrangements(ctx, month)
			if err != nil {
				return err
			}
			for _, h := range have {
				out.Body.InForce = append(out.Body.InForce, arrangementOf(h))
			}
			if out.Body.Missing, err = payrollGate(ctx, u, month); err != nil {
				return err
			}
			if out.Body.Missing == nil {
				out.Body.Missing = []string{}
			}
			out.Body.Met = len(out.Body.Missing) == 0
			return nil
		})
		return &out, err
	})

	register(a, authz.SetPayrollArrangement, huma.Operation{
		OperationID: "setPayrollArrangement", Method: http.MethodPost, Path: "/v1/payroll-arrangements", Tags: []string{"People"}, DefaultStatus: http.StatusCreated,
		Summary: "Record how an item of the payroll go-live gate is met",
		Description: "Records, as evidence, how one item of the gate (FR-PAY-15) is met from a wage month: a provider's (named), a documented manual process, Nemura's own (payslips and KETs, while the link is " +
			"active and payroll is Nemura's), or not applicable (the Auto-Inclusion Scheme and IR21 only, with the reason). Every method but Nemura's needs a file of evidence on a record of the organisation " +
			"(FR-DOC-05). An arrangement is never changed: a new one from a later month replaces it. Needs a person who passed step-up. Side effects: the arrangement and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, CodeSessionRequired, CodeStepUpRequired, storage.CodeStorageFailure}, locks...),
		func(ctx context.Context, in *setArrangementInput) (*setArrangementOutput, error) {
			p, err := a.steppedUpPerson(ctx, "Recording an arrangement of the payroll go-live gate")
			if err != nil {
				return nil, err
			}
			from, err := time.Parse("2006-01", in.Body.EffectiveFrom)
			if err != nil {
				return nil, problem(CodeInvalid, "The month is not YYYY-MM.", "Send the month as YYYY-MM")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PayrollArrangement, error) {
				arr := storage.PayrollArrangement{Item: in.Body.Item, Method: in.Body.Method, Provider: strings.TrimSpace(in.Body.Provider), Note: strings.TrimSpace(in.Body.Note), EffectiveFrom: from,
					RecordedBy: "user:" + p.UserID.String(), RecordedAt: a.deps.Clock.Now()}
				fail := func(field, msg, fix string) (PayrollArrangement, error) {
					return PayrollArrangement{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: field, Message: msg, Fix: fix}})
				}
				switch in.Body.Method {
				case storage.MethodNemura:
					if in.Body.Item != storage.ArrangePayslips && in.Body.Item != storage.ArrangeKETs {
						return fail("body.method", "Nemura issues payslips and KETs only", "Choose provider or manual for this item")
					}
					if inN, err := payrollInNemura(ctx, u); err != nil {
						return PayrollArrangement{}, err
					} else if !inN {
						return fail("body.method", "payroll does not live in Nemura here: the link is not active or its source for payroll is not Nemura", "Link Nemura in Settings, People and HR, or choose provider or manual")
					}
				case storage.MethodProvider:
					if arr.Provider == "" {
						return fail("body.provider", "a provider is named by its name", "Name the payroll provider")
					}
				case storage.MethodNotApplicable:
					if (in.Body.Item != storage.ArrangeAIS && in.Body.Item != storage.ArrangeIR21) || arr.Note == "" {
						return fail("body.note", "only the Auto-Inclusion Scheme and IR21 may be not applicable, and with the reason", "Say why it does not apply")
					}
				}
				if in.Body.Method != storage.MethodNemura {
					id, err := uuid.Parse(in.Body.EvidenceID)
					if err != nil {
						return fail("body.evidence_id", "is needed: an arrangement is recorded as evidence (FR-DOC-05)", "Send the ID of a file of evidence")
					}
					if err := salaryEvidence(ctx, u, id); err != nil {
						return PayrollArrangement{}, err
					}
					arr.EvidenceID = id
				}
				rec, err := u.PayrollOutside().Arrange(ctx, arr)
				if err != nil {
					return PayrollArrangement{}, err
				}
				return arrangementOf(rec), nil
			})
			if err != nil {
				return nil, err
			}
			return &setArrangementOutput{Body: w.Body}, nil
		})

	register(a, authz.ListPayrollImports, huma.Operation{
		OperationID: "listPayrollImports", Method: http.MethodGet, Path: "/v1/payroll-imports", Tags: []string{"People"},
		Summary:     "List the payroll imports",
		Description: "The provider journals and file imports of pay periods (FR-PPL-05), newest period first, each with the draft journal it made for approval under Journals. Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *importListInput) (*importListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		skip := 0
		if after != "" {
			if skip, err = strconv.Atoi(after); err != nil || skip < 0 {
				return nil, problem(CodeCursor, "The cursor is not one this list gave.", "Start again without a cursor")
			}
		}
		p, _ := authn.From(ctx)
		out := importListOutput{Body: PayrollImportPage{Items: []PayrollImportRow{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.PayrollOutside().Imports(ctx)
			if skip > len(all) {
				skip = len(all)
			}
			all = all[skip:]
			if len(all) > in.PageSize {
				out.Body.NextCursor = encodeCursor(strconv.Itoa(skip + in.PageSize))
				all = all[:in.PageSize]
			}
			for _, i := range all {
				out.Body.Items = append(out.Body.Items, PayrollImportRow{ID: i.ID.String(), SourceSystem: i.SourceSystem, Period: i.Period.Format("2006-01"), Version: i.Version, JournalID: i.JournalID.String(),
					Lines: i.LineCount, CreatedAt: i.CreatedAt.UTC().Format(time.RFC3339)})
			}
			return err
		})
		return &out, err
	})

	register(a, authz.ImportPayrollJournal, huma.Operation{
		OperationID: "importPayrollJournal", Method: http.MethodPost, Path: "/v1/payroll-imports", Tags: []string{"People"}, DefaultStatus: http.StatusCreated,
		Summary: "Import a provider's payroll journal as a draft",
		Description: "Reads a payroll provider's journal for a pay period, or a file import, as a CSV by account and department (no column may name a person: FR-PAY-13), and makes it a **draft journal** under " +
			"Journals that a Controller or Accountant approves, because the external system's approval cannot be verified (FR-PPL-05, NM-EXT-03). It is idempotent per source system, pay period and version: " +
			"the same version with the same file answers unchanged, with another file is refused, and a later version drafts only its difference from the version before (the earlier version's journal must be " +
			"posted first). The draft is dated on the wage month's last day, or today when that month is closed. It is refused while payroll lives in Nemura (the link is live and Nemura holds payroll: " +
			"PPL-29), so one pay run is never posted from two sources. Prefer: return=preview shows the draft's lines and what changes and saves nothing (FR-INT-27). Side effects: the draft journal and the import's " +
			"record, each audited. Idempotent with Idempotency-Key. Needs the right to prepare payroll; a draft is posted only under Journals' own approval.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, peoplelink.CodeInNemura, payrollimport.CodeFile, payrollimport.CodeColumn, payrollimport.CodeAccount,
		payrollimport.CodeAmount, payrollimport.CodeUnbalance, payrollimport.CodeDept, payrollimport.CodeTooMany, payrollimport.CodePerson, storage.CodePayrollRun}, locks...),
		func(ctx context.Context, in *payrollImportInput) (*payrollImportOutput, error) {
			p, _ := authn.From(ctx)
			period, err := time.Parse("2006-01", in.Body.Period)
			if err != nil {
				return nil, problem(CodeInvalid, "The period is not YYYY-MM.", "Send the wage month as YYYY-MM")
			}
			data, err := base64.StdEncoding.DecodeString(in.Body.ContentBase64)
			if err != nil || len(data) == 0 {
				return nil, problem(payrollimport.CodeFile, "The content is not base64 of a CSV.", "Send the CSV's bytes in base64")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PayrollImport, error) {
				if inN, err := payrollInNemura(ctx, u); err != nil {
					return PayrollImport{}, err
				} else if inN {
					return PayrollImport{}, problem(peoplelink.CodeInNemura, "Payroll lives in Nemura here, so a provider's journal or a file import is not taken: the pay run posts through the link.",
						"Change where payroll lives in Settings, People and HR first (it needs the Owner and a second approver)")
				}
				org, err := u.Organisation(ctx)
				if err != nil {
					return PayrollImport{}, err
				}
				base, err := money.Lookup(org.BaseCurrency)
				if err != nil {
					return PayrollImport{}, err
				}
				settings, err := journal.LoadSettings(ctx, u)
				if err != nil {
					return PayrollImport{}, err
				}
				res := chartResolver{accounts: map[string]uuid.UUID{}, depts: map[string]uuid.UUID{}}
				for _, acc := range settings.Accounts {
					if !acc.Group && acc.Active {
						res.accounts[acc.Code] = acc.ID
					}
				}
				for id, dv := range settings.Values {
					if settings.Dimensions[dv.Dimension].Name == "Department" && dv.Active {
						res.depts[dv.Code] = id
					}
				}
				m := payrollimport.Default
				if in.Body.Mapping != nil {
					m = *in.Body.Mapping
				}
				read, probs := payrollimport.Read(data, m, res, base)
				if len(probs) > 0 {
					fields := make([]FieldProblem, len(probs))
					for i, x := range probs {
						field := "body.content_base64"
						if x.Row > 0 {
							field = fmt.Sprintf("row %d", x.Row)
						}
						fields[i] = FieldProblem{Code: x.Code, Field: field, Message: x.Message, Fix: x.Fix}
					}
					return PayrollImport{}, problem(probs[0].Code, fmt.Sprintf("The file cannot be imported: %d problem(s), each listed in errors.", len(probs)), "Correct them and send it again.", fields...)
				}
				out := PayrollImport{SourceSystem: in.Body.SourceSystem, Period: in.Body.Period, Version: in.Body.Version, Rows: read.Rows, SHA256: read.SHA256, Lines: []PayrollImportLine{}}
				latest, err := u.PayrollOutside().LatestImport(ctx, in.Body.SourceSystem, period)
				if err != nil && !errors.Is(err, ports.ErrNotFound) {
					return PayrollImport{}, err
				}
				have := err == nil
				if have && in.Body.Version <= latest.Version {
					same, err := u.PayrollOutside().ImportVersion(ctx, in.Body.SourceSystem, period, in.Body.Version)
					if err == nil && same.FileSHA256 == read.SHA256 {
						out.Outcome, out.ID, out.JournalID = "unchanged", same.ID.String(), same.JournalID.String()
						return out, nil
					}
					return PayrollImport{}, problem(storage.CodePayrollRun, "A version of a pay period is imported once: this version exists with another file, or an earlier one than the latest.", "Send the period's next version")
				}
				if in.Body.Version != 1 && !have || have && in.Body.Version != latest.Version+1 {
					return PayrollImport{}, problem(CodeInvalid, "Versions go 1, 2, 3 for a period, one at a time.", "Send the period's next version")
				}
				prior := map[string]string{}
				if have {
					jd, err := u.Documents().Get(ctx, latest.JournalID)
					if err != nil {
						return PayrollImport{}, err
					}
					if jd.Status != storage.DocumentPosted {
						return PayrollImport{}, problem(storage.CodePayrollRun, "The earlier version's draft journal is not posted yet, so a later version has nothing to take its difference from.",
							"Approve and post the earlier version's journal under Journals, or discard it and import again")
					}
					prior = latest.Totals
				}
				diff, err := payrollimport.Difference(prior, read.Lines, base)
				if err != nil {
					return PayrollImport{}, err
				}
				posting := period.AddDate(0, 1, -1)
				periods, err := u.Periods().List(ctx)
				if err != nil {
					return PayrollImport{}, err
				}
				now := a.deps.Clock.Now()
				for _, per := range periods { // as a pay run does: a closed month's difference is drafted in the current open month (FR-PPL-11)
					if !posting.Before(per.StartsOn) && !posting.After(per.EndsOn) && per.Status == storage.PeriodClosed {
						posting = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
					}
				}
				out.PostingDate = posting.Format(time.DateOnly)
				var lines []ledger.JournalLine
				for _, d := range diff {
					l := PayrollImportLine{AccountID: d.Account.String(), Amount: moneyOut(d.Amount)}
					if len(d.Values) > 0 {
						l.DepartmentID = d.Values[0].String()
					}
					out.Lines = append(out.Lines, l)
					lines = append(lines, ledger.JournalLine{AccountID: d.Account, Amount: d.Amount, Values: d.Values, Description: d.Memo})
				}
				if len(diff) == 0 {
					out.Outcome = "nothing_changed"
				}
				if len(diff) == 0 && !have {
					return out, nil // an empty first version drafts nothing and records nothing: there is no journal to name
				}
				// A preview ends here: it is the same transaction with a rollback, so the draft below is made and discarded (FR-INT-14).
				memo := fmt.Sprintf("Payroll %s %s, version %d, imported from %s: a draft for approval", in.Body.SourceSystem, in.Body.Period, in.Body.Version, in.Body.SourceSystem)
				var journalID uuid.UUID
				if len(diff) > 0 {
					doc, err := journal.CreateIn(ctx, u, journal.Draft{Date: posting, Memo: memo, Lines: lines})
					if err != nil {
						return PayrollImport{}, err
					}
					journalID = doc.ID
					out.Outcome, out.JournalID = "drafted", doc.ID.String()
				} else {
					journalID = latest.JournalID // nothing to draft: the version is recorded with the earlier version's journal, so the next version follows it
				}
				rec, err := u.PayrollOutside().Import(ctx, storage.PayrollImport{SourceSystem: in.Body.SourceSystem, Period: period, Version: in.Body.Version, FileSHA256: read.SHA256, JournalID: journalID,
					LineCount: len(diff), Totals: payrollimport.Totals(read.Lines), CreatedBy: "user:" + p.UserID.String(), CreatedAt: now})
				if err != nil {
					return PayrollImport{}, err
				}
				out.ID = rec.ID.String()
				return out, nil
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed && w.Body.Outcome == "drafted" {
				w.Body.Outcome, w.Body.ID, w.Body.JournalID = "preview", "", ""
			}
			return &payrollImportOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		})
}
