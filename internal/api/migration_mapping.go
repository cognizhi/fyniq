package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the migration's mapping (§17.1 M2, design D4)

// MigrationAccountMap is a workbook account and the Fyniq account it maps onto.
type MigrationAccountMap struct {
	Workbook     string `json:"workbook" minLength:"1" maxLength:"20" doc:"The workbook's account code: the entry's key." example:"4030"`
	Name         string `json:"name,omitempty" doc:"The workbook's name for it; ignored in a change." example:"AI project & solution revenue"`
	WorkbookType string `json:"workbook_type,omitempty" doc:"The workbook's type, such as Cost of Sales; ignored in a change." example:"Revenue"`
	Row          int    `json:"row,omitempty" doc:"Its row on the Accounts sheet; ignored in a change." example:"36"`
	Fyniq        string `json:"fyniq" minLength:"1" maxLength:"20" doc:"The Fyniq account it maps onto, or is created as, by code." example:"4030"`
	Action       string `json:"action" enum:"existing,create" doc:"existing: Fyniq's chart has it; create: the set-up creates it." example:"existing"`
	Type         string `json:"type,omitempty" doc:"Fyniq's type: asset, liability, equity, revenue or expense; the workbook's Cost of Sales, Other Expense and Tax are expense and Other Income revenue." example:"revenue"`
	ReportGroup  string `json:"report_group,omitempty" doc:"An account to create: its report group." example:"Services & project revenue"`
	CashFlowLine string `json:"cash_flow_line,omitempty" doc:"An account to create: its cash-flow line." example:"Operating"`
	Function     string `json:"function,omitempty" doc:"An account to create: its function (COGS, R&D, S&M, G&A or Other)." example:"Other"`
	MoneyAccount bool   `json:"money_account,omitempty" doc:"An account to create: whether money moves through it." example:"false"`
	DefaultGST   string `json:"default_gst_code,omitempty" doc:"An account to create: its default GST code." example:"SR"`
	Role         string `json:"role,omitempty" doc:"The system role of the Fyniq account it maps onto, if any (FR-COA-03); ignored in a change." example:"director-loan"`
	HasOpening   bool   `json:"has_opening,omitempty" doc:"Whether the workbook gives it an opening balance; ignored in a change." example:"true"`
	// The person a director's or subscriber's account's opening balance belongs to (rev 0.259).
	OpeningPerson string `json:"opening_person,omitempty" maxLength:"200" doc:"On a director's or subscriber's account with an opening balance: the contact, one of the mapping's people, whose balance it is (B.7, rev 0.259)." example:"Alex Tan"`
}

// MigrationNatureMap is a supplier resident for tax outside Singapore and the nature of payment its bills record (rev 0.259).
type MigrationNatureMap struct {
	Contact string `json:"contact" minLength:"1" maxLength:"200" doc:"The supplier, by its name on the Contacts sheet: the entry's key." example:"CloudNimbus Hosting"`
	Row     int    `json:"row,omitempty" doc:"Its row on the Contacts sheet; ignored in a change." example:"12"`
	Country string `json:"country,omitempty" doc:"Where it is resident for tax; ignored in a change." example:"US"`
	Nature  string `json:"nature" enum:",goods,software_use,software_rights,services_in_singapore,services_outside_singapore,interest,movable_rent,other" doc:"The nature of payment its bills record for the tax agent's withholding decision (FR-AP-19); empty until the Accountant chooses, which stops the sign-off." example:"services_outside_singapore"`
}

// MigrationPersonMap is a person the workbook names and the contact they become (rev 0.259).
type MigrationPersonMap struct {
	Workbook    string `json:"workbook" minLength:"1" maxLength:"200" doc:"The name the workbook gives them: the entry's key." example:"Alex Tan (CEO)"`
	Sheet       string `json:"sheet,omitempty" doc:"Where the workbook first names them; ignored in a change." example:"Transactions"`
	Row         int    `json:"row,omitempty" doc:"Its row; ignored in a change." example:"127"`
	Contact     string `json:"contact" maxLength:"200" doc:"The contact's legal name: two names of one person map onto one contact." example:"Alex Tan"`
	Director    bool   `json:"director,omitempty" doc:"Marked director: a director's account names them (FR-EQ-01)." example:"true"`
	Shareholder bool   `json:"shareholder,omitempty" doc:"Marked shareholder." example:"true"`
}

// MigrationCodeMap is a workbook GST code and the sg-gst code it maps onto.
type MigrationCodeMap struct {
	Workbook string `json:"workbook" minLength:"1" maxLength:"10" doc:"The workbook's GST code: the entry's key." example:"SR"`
	Row      int    `json:"row,omitempty" doc:"Its row on the Lists sheet; ignored in a change." example:"6"`
	Fyniq    string `json:"fyniq" minLength:"1" maxLength:"10" doc:"The sg-gst pack's code." example:"SR"`
}

// MigrationValueMap is a workbook department or project and the dimension value it maps onto.
type MigrationValueMap struct {
	Workbook string `json:"workbook" minLength:"1" maxLength:"200" doc:"The workbook's name for it: the entry's key." example:"Product & Engineering"`
	Code     string `json:"code" minLength:"1" maxLength:"30" doc:"The dimension value's code." example:"PRODUCT-ENGINEERING"`
	Action   string `json:"action" enum:"existing,create" doc:"existing: Fyniq has the value; create: the set-up creates it." example:"create"`
}

// MigrationLineMap is a budget's P&L line and the account its amounts go to.
type MigrationLineMap struct {
	Line    string `json:"line" minLength:"1" maxLength:"200" doc:"The Budget sheet's P&L line: the entry's key." example:"Subscription revenue"`
	Account string `json:"account" maxLength:"20" doc:"The revenue or expense account its amounts go to, by code." example:"4010"`
}

// MigrationRuleMap is a bank rule, in its order.
type MigrationRuleMap struct {
	Order    int    `json:"order" minimum:"1" doc:"Its place among the rules, first match first: the entry's key." example:"1"`
	Contains string `json:"contains,omitempty" doc:"The text a bank description contains; ignored in a change." example:"STRIPE"`
	Account  string `json:"account" maxLength:"20" doc:"The account it categorises to, by code." example:"1050"`
	GST      string `json:"gst,omitempty" maxLength:"10" doc:"The GST code it gives, if any." example:"SR"`
}

// MigrationMappingView is the mapping's latest version (§17.1 M2, design D4), what it gets wrong, and its sign-off.
type MigrationMappingView struct {
	Version     int                   `json:"version" doc:"The version: the validation proposes one from each valid workbook, and each change makes the next." example:"2"`
	StepID      string                `json:"step_id" format:"uuid" doc:"The M1 step whose workbook it maps." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a20"`
	SHA256      string                `json:"sha256" doc:"The SHA-256 of the version as kept: what the sign-off names." example:"5d1e…"`
	MadeBy      string                `json:"made_by" doc:"Who made the version: the validation, or a person." example:"system:migration.validate"`
	MadeAt      string                `json:"made_at" format:"date-time" doc:"When." example:"2026-11-13T09:05:20Z"`
	Signed      bool                  `json:"signed" doc:"Whether the Accountant signed this version; a later version needs its own sign-off." example:"false"`
	SignedBy    string                `json:"signed_by,omitempty" doc:"Who signed it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	SignedAt    string                `json:"signed_at,omitempty" format:"date-time" doc:"When." example:"2026-11-13T10:00:00Z"`
	Accounts    []MigrationAccountMap `json:"accounts" nullable:"false" doc:"Each workbook account, in the sheet's order."`
	GSTCodes    []MigrationCodeMap    `json:"gst_codes" nullable:"false" doc:"Each workbook GST code."`
	Departments []MigrationValueMap   `json:"departments" nullable:"false" doc:"Each department of the Employees sheet, as a value of the Department dimension (rev 0.254)."`
	Projects    []MigrationValueMap   `json:"projects" nullable:"false" doc:"Each project the records name, as a value of the Project dimension (rev 0.254)."`
	BudgetLines []MigrationLineMap    `json:"budget_lines" nullable:"false" doc:"Each P&L line of the Budget sheet."`
	BankRules   []MigrationRuleMap    `json:"bank_rules" nullable:"false" doc:"The bank rules, in order."`
	People      []MigrationPersonMap  `json:"people" nullable:"false" doc:"The Funding sheet's founders and everyone named on a director's or subscriber's account, each the contact they become (rev 0.259)."`
	Natures     []MigrationNatureMap  `json:"natures" nullable:"false" doc:"Each supplier resident for tax outside Singapore, with the nature of payment its bills record (FR-AP-19, rev 0.259)."`
	Problems    []MigrationProblem    `json:"problems" nullable:"false" doc:"What the version gets wrong against the organisation's set-up now: each an error that stops the sign-off."`
	Errors      int                   `json:"errors" doc:"The errors." example:"0"`
}

// MigrationMappingChange changes entries of the mapping: each replaces the entry with its key.
type MigrationMappingChange struct {
	Accounts    []MigrationAccountMap `json:"accounts,omitempty" doc:"Accounts to map again, by the workbook's code."`
	GSTCodes    []MigrationCodeMap    `json:"gst_codes,omitempty" doc:"GST codes to map again, by the workbook's code."`
	Departments []MigrationValueMap   `json:"departments,omitempty" doc:"Departments to map again, by the workbook's name."`
	Projects    []MigrationValueMap   `json:"projects,omitempty" doc:"Projects to map again, by the workbook's name."`
	BudgetLines []MigrationLineMap    `json:"budget_lines,omitempty" doc:"Budget lines to map again, by the line."`
	BankRules   []MigrationRuleMap    `json:"bank_rules,omitempty" doc:"Bank rules to map again, by their order."`
	People      []MigrationPersonMap  `json:"people,omitempty" doc:"People to map again, by the workbook's name."`
	Natures     []MigrationNatureMap  `json:"natures,omitempty" doc:"Suppliers' natures of payment to choose, by the supplier's name."`
}

type (
	migrationMappingOutput struct {
		ETag string `header:"ETag" doc:"The version and whether it is signed, for If-Match."`
		Body MigrationMappingView
	}
	migrationMappingChangeInput struct {
		MigrationPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body MigrationMappingChange
	}
	migrationMappingSignInput struct {
		MigrationPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	migrationMappingWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The version and whether it is signed, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     MigrationMappingView
	}
)

// recast copies a value onto a type with the same JSON.
func recast[T any](from any) (T, error) {
	var out T
	b, err := json.Marshal(from)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	return out, err
}

// mappingState is a migration's latest mapping, as kept and as read, with its sign-off.
type mappingState struct {
	kept    storage.MigrationMapping
	mapping migration.Mapping
	signed  *storage.MigrationSignOff
}

func (s mappingState) etag() string {
	if s.signed != nil {
		return etag(s.kept.Version, "signed")
	}
	return etag(s.kept.Version, "open")
}

// migrationMappingOf is a migration's latest mapping: CodeNotFound before a valid workbook proposes one.
func migrationMappingOf(ctx context.Context, u storage.UnitOfWork, run uuid.UUID) (mappingState, error) {
	m, kept, err := migration.LatestMapping(ctx, u, run)
	if err != nil {
		return mappingState{}, err
	}
	if m == nil {
		return mappingState{}, problem(CodeNotFound, "The migration has no mapping yet: a valid workbook proposes one.",
			"Upload the frozen workbook; once it is valid, its mapping is here.")
	}
	s := mappingState{kept: kept, mapping: *m}
	offs, err := u.Migrations().SignOffs(ctx, run)
	for i := range offs {
		if offs[i].Subject == storage.SignOffMapping && offs[i].MappingID != nil && *offs[i].MappingID == kept.ID {
			s.signed = &offs[i]
		}
	}
	return s, err
}

// mappingView is the mapping with what it gets wrong against the organisation's set-up now.
func mappingView(ctx context.Context, u storage.UnitOfWork, s mappingState) (MigrationMappingView, []migration.Problem, error) {
	target, err := migration.TargetOf(ctx, u, time.Time{})
	if err != nil {
		return MigrationMappingView{}, nil, err
	}
	v, err := recast[MigrationMappingView](s.mapping)
	if err != nil {
		return v, nil, err
	}
	v.Version, v.StepID, v.SHA256, v.MadeBy, v.MadeAt = s.kept.Version, s.kept.StepID.String(), s.kept.SHA256, s.kept.MadeBy, at(s.kept.MadeAt)
	if s.signed != nil {
		v.Signed, v.SignedBy, v.SignedAt = true, s.signed.SignedBy, at(s.signed.SignedAt)
	}
	problems := s.mapping.Check(target)
	v.Problems = []MigrationProblem{}
	for _, p := range problems {
		v.Problems = append(v.Problems, MigrationProblem{Sheet: p.Sheet, Row: p.Row, Column: p.Column, Severity: string(p.Severity), Code: p.Code,
			Message: p.Message, Fix: p.Fix})
	}
	v.Errors = len(problems)
	if v.Accounts == nil {
		v.Accounts = []MigrationAccountMap{}
	}
	if v.GSTCodes == nil {
		v.GSTCodes = []MigrationCodeMap{}
	}
	if v.Departments == nil {
		v.Departments = []MigrationValueMap{}
	}
	if v.Projects == nil {
		v.Projects = []MigrationValueMap{}
	}
	if v.BudgetLines == nil {
		v.BudgetLines = []MigrationLineMap{}
	}
	if v.BankRules == nil {
		v.BankRules = []MigrationRuleMap{}
	}
	if v.People == nil {
		v.People = []MigrationPersonMap{}
	}
	if v.Natures == nil {
		v.Natures = []MigrationNatureMap{}
	}
	return v, problems, nil
}

func registerMigrationMapping(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		storage.CodeMigration, storage.CodeVersion, storage.CodeDuplicate}, locks...)

	register(a, authz.GetMigrationMapping, huma.Operation{
		OperationID: "getMigrationMapping", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/mapping", Tags: []string{"Migration"},
		Summary: "Get the mapping",
		Description: "M2 (§17.1, design D4): the workbook's set-up mapped onto Fyniq's, as its latest version. Accounts map by code onto the " +
			"organisation's chart (or the standard chart a new organisation loads), the workbook's 7040 onto Fyniq's 7050, and an account " +
			"Fyniq lacks is created from the Accounts sheet; GST codes onto the sg-gst pack's; departments and projects as dimension values " +
			"(rev 0.254); budget lines onto their report group's first account; bank rules in their order. Each problem against the " +
			"organisation's set-up now is an error that stops the Accountant's sign-off. A valid workbook proposes a version, keeping the " +
			"choices of the one before. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *MigrationPath) (*migrationMappingOutput, error) {
		id, err := parseID("migration_id", in.MigrationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &migrationMappingOutput{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := migrationRun(ctx, u, id); err != nil {
				return err
			}
			s, err := migrationMappingOf(ctx, u, id)
			if err != nil {
				return err
			}
			out.ETag = s.etag()
			out.Body, _, err = mappingView(ctx, u, s)
			return err
		})
		return out, err
	})

	register(a, authz.ChangeMigrationMapping, huma.Operation{
		OperationID: "changeMigrationMapping", Method: http.MethodPatch, Path: "/v1/migrations/{migration_id}/mapping", Tags: []string{"Migration"},
		Summary: "Change the mapping",
		Description: "Changes entries of the mapping as its next version (§17.1 M2): each entry sent replaces the one with its key, the " +
			"workbook's code or name, the budget line or the bank rule's order. An entry the mapping does not hold is MIGRATION-11. The " +
			"new version is kept even with errors against the set-up, which it lists, and needs the Accountant's sign-off of its own. Once the " +
			"import is committed the mapping is fixed (MIGRATION-14). " +
			"Needs If-Match with the mapping's ETag. Side effects: the version, the audit event migration.mapped. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, migration.CodeMapping, migration.CodeCommit}, writes...),
		func(ctx context.Context, in *migrationMappingChangeInput) (*migrationMappingWriteOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			change, err := recast[migration.Change](in.Body)
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MigrationMappingView, error) {
				if _, err := migrationRun(ctx, u, id); err != nil {
					return MigrationMappingView{}, err
				}
				s, err := migrationMappingOf(ctx, u, id)
				if err != nil {
					return MigrationMappingView{}, err
				}
				if err := checkIfMatch(in.IfMatch, s.etag()); err != nil {
					return MigrationMappingView{}, err
				}
				if err := migrationFixed(ctx, u, id); err != nil {
					return MigrationMappingView{}, err
				}
				next, unknown := s.mapping.Apply(change)
				if len(unknown) > 0 {
					var bad []FieldProblem
					for _, p := range unknown {
						bad = append(bad, FieldProblem{Code: migration.CodeMapping, Field: "body", Message: p.Message, Fix: p.Fix})
					}
					return MigrationMappingView{}, invalid(bad)
				}
				sealed, sum, err := next.Sealed()
				if err != nil {
					return MigrationMappingView{}, err
				}
				kept, err := u.Migrations().Map(ctx, storage.MigrationMapping{RunID: id, Version: s.kept.Version + 1, StepID: s.kept.StepID, Mapping: sealed,
					SHA256: sum, MadeBy: uploader(ctx), MadeAt: now})
				if err != nil {
					return MigrationMappingView{}, err
				}
				v, _, err := mappingView(ctx, u, mappingState{kept: kept, mapping: next})
				return v, err
			})
			if err != nil {
				return nil, err
			}
			out := &migrationMappingWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag = etag(w.Body.Version, "open")
			}
			return out, nil
		})

	register(a, authz.SignMigrationMapping, huma.Operation{
		OperationID: "signMigrationMapping", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/mapping/sign-off", Tags: []string{"Migration"},
		Summary: "Sign the mapping",
		Description: "The Accountant signs the mapping's latest version off (§17.1 M2, rev 0.256), over its SHA-256, stepped up in the last 5 " +
			"minutes: the commit (M3) needs it. A version with an error against the organisation's set-up is MIGRATION-11, and one of a " +
			"workbook that is not the latest valid upload is MIGRATION-12. A version is signed once; a later version needs its own " +
			"sign-off, and once the import is committed nothing is signed (MIGRATION-14). Needs If-Match with the mapping's ETag. Side effects: the sign-off, the audit event migration.mapping_signed. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeSessionRequired, CodeStepUpRequired, migration.CodeMapping, migration.CodeNotSignable, migration.CodeCommit}, writes...),
		func(ctx context.Context, in *migrationMappingSignInput) (*migrationMappingWriteOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			person, err := a.steppedUpPerson(ctx, "Signing the mapping")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MigrationMappingView, error) {
				if _, err := migrationRun(ctx, u, id); err != nil {
					return MigrationMappingView{}, err
				}
				s, err := migrationMappingOf(ctx, u, id)
				if err != nil {
					return MigrationMappingView{}, err
				}
				if err := checkIfMatch(in.IfMatch, s.etag()); err != nil {
					return MigrationMappingView{}, err
				}
				if err := migrationFixed(ctx, u, id); err != nil {
					return MigrationMappingView{}, err
				}
				steps, err := u.Migrations().Steps(ctx, id)
				if err != nil {
					return MigrationMappingView{}, err
				}
				var last *storage.MigrationStep
				for i := range steps {
					if steps[i].Step == storage.StepWorkbook {
						last = &steps[i]
					}
				}
				if last == nil || last.ID != s.kept.StepID || last.State != storage.StepValid {
					return MigrationMappingView{}, problem(migration.CodeNotSignable, "The mapping is of a workbook that is not the latest valid upload.",
						"Wait for the latest workbook's validation, then review the mapping it proposes and sign that.")
				}
				_, problems, err := mappingView(ctx, u, s)
				if err != nil {
					return MigrationMappingView{}, err
				}
				if len(problems) > 0 {
					return MigrationMappingView{}, problem(migration.CodeMapping, fmt.Sprintf("The mapping has %d errors against the organisation's set-up: the first, %s.",
						len(problems), problems[0].Message), "Change the mapping until it lists no problem, then sign it.")
				}
				off, err := u.Migrations().SignOff(ctx, storage.MigrationSignOff{RunID: id, Subject: storage.SignOffMapping, MappingID: &s.kept.ID,
					SubjectSHA256: s.kept.SHA256, SignedBy: "user:" + person.UserID.String(), SignedAt: now})
				if err != nil {
					return MigrationMappingView{}, err
				}
				s.signed = &off
				v, _, err := mappingView(ctx, u, s)
				return v, err
			})
			if err != nil {
				return nil, err
			}
			out := &migrationMappingWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag = etag(w.Body.Version, "signed")
			}
			return out, nil
		})
}
