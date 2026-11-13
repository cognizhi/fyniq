package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the reconciliation (§17.1 M4, FR-MIG-02, design D8)

// MigrationDifference is a figure on which Fyniq's books and the workbook's Trial Balance differ: a line of the
// difference register.
type MigrationDifference struct {
	Key        string                     `json:"key" doc:"The line's key in the difference register: the Fyniq account's code, the month (YYYY-MM) and the figure, or the code and opening." example:"6510.2026-03.movement"`
	Account    string                     `json:"account" doc:"The Fyniq account's code; the workbook's accounts that map onto it are added." example:"6510"`
	Name       string                     `json:"name" doc:"The account's name in Fyniq." example:"Depreciation"`
	Kind       string                     `json:"kind" enum:"opening,movement,closing" doc:"opening: the balance at the books start; movement: the month's; closing: the balance at the month's end." example:"movement"`
	Month      string                     `json:"month,omitempty" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month (YYYY-MM); absent for the opening." example:"2026-03"`
	Fyniq      string                     `json:"fyniq" doc:"Fyniq's figure in the base currency, debit positive." example:"620.00"`
	Workbook   string                     `json:"workbook" doc:"The workbook's, from its Trial Balance sheet after RND-IMPORT." example:"620.01"`
	Difference string                     `json:"difference" doc:"Fyniq's less the workbook's." example:"-0.01"`
	Proposed   string                     `json:"proposed_class" enum:",a,b,c" doc:"The class proposed (design D8): a, a cent allocation on a spread or depreciation account within a cent a month; b, a deliberate rule change on a revaluation or statutory account; empty when none fits, for the Accountant to decide." example:"a"`
	Decision   *MigrationRegisterDecision `json:"decision,omitempty" doc:"The Accountant's decision on the line; absent while undecided."`
}

// MigrationRegisterDecision is the Accountant's decision on a line of the difference register (FR-MIG-02; rev 0.266).
type MigrationRegisterDecision struct {
	Class     string    `json:"class" enum:"a,b,c" doc:"a: a cent allocation of spreads and depreciation; b: a deliberate rule change; c: a defect of the workbook found during the migration." example:"a"`
	Note      string    `json:"note,omitempty" doc:"Why, as the Accountant wrote it; a workbook defect says what it is." example:"ALLOC-EVEN puts the quarter's cent in its last month"`
	Version   int       `json:"version" doc:"The decision's version: deciding the line again takes the next." example:"1"`
	DecidedBy string    `json:"decided_by" doc:"Who decided it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a11"`
	DecidedAt time.Time `json:"decided_at" doc:"When." example:"2026-11-13T09:30:00Z"`
	Stale     bool      `json:"stale" doc:"true: it was decided on other figures than the report's, so it no longer counts: decide the line again." example:"false"`
}

// MigrationFyniqCheck is one of Fyniq's checks of a workbook check's Appendix C.1 number, with its status.
type MigrationFyniqCheck struct {
	Key    string `json:"key" doc:"The check's key." example:"books.debits-equal-credits"`
	Status string `json:"status" enum:"ok,warning,error,due,not_assessed,failed" doc:"Its status at its last run." example:"ok"`
}

// MigrationCheckParity is one of the workbook's Checks rows against Fyniq's checks of its number.
type MigrationCheckParity struct {
	Key      string                     `json:"key" doc:"Its line's key in the difference register, which a different status needs: check and its number." example:"check.30"`
	Number   int                        `json:"number" doc:"The workbook check's number, which is its Appendix C.1 number." example:"1"`
	Name     string                     `json:"name" doc:"What the workbook checks." example:"Debits equal credits (trial balance)"`
	Row      int                        `json:"row" doc:"Its row on the Checks sheet." example:"7"`
	Workbook string                     `json:"workbook" enum:"fine,review,blocked" doc:"Its status in the workbook: fine ✔, review ⚠ or blocked ✖." example:"fine"`
	Fyniq    []MigrationFyniqCheck      `json:"fyniq" nullable:"false" doc:"Fyniq's checks of the number."`
	Same     bool                       `json:"same" doc:"Whether the worst of Fyniq's statuses is the workbook's: a different one needs a register line or a fix." example:"true"`
	Missing  bool                       `json:"missing" doc:"Fyniq has no check of the number (the workbook's own checks of its sheets, such as Reports frozen)." example:"false"`
	Decision *MigrationRegisterDecision `json:"decision,omitempty" doc:"The Accountant's decision on a different status; absent while undecided."`
}

// MigrationReconciliationSignOff is the latest sign-off of the parity report and its register (§17.1 M4).
type MigrationReconciliationSignOff struct {
	SignedBy string    `json:"signed_by" doc:"The Accountant who signed it, stepped up." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a11"`
	SignedAt time.Time `json:"signed_at" doc:"When." example:"2026-11-13T10:00:00Z"`
	SHA256   string    `json:"sha256" doc:"The report's SHA-256 it signs." example:"5d1e…"`
	Current  bool      `json:"current" doc:"Whether it names this report: once the books or a decision change, the report is another, and the step stays signed by the earlier one." example:"true"`
}

// MigrationReconciliation is the parity report (§17.1 M4, design D8): every figure of the workbook's Trial Balance sheet
// compared with Fyniq's books, the differences, and the workbook's checks against Fyniq's.
type MigrationReconciliation struct {
	Status       string                          `json:"status" enum:"not_committed,ready,signed" doc:"not_committed: the import is not committed yet, so there is nothing to reconcile and the report holds no figures; ready: the report of the books as they stand; signed: the Accountant signed this report and its register (M4)." example:"ready"`
	Path         string                          `json:"path" enum:"history,clean_start" doc:"The path imported: history compares the opening and each month's movement and closing; clean_start the opening against the workbook's closing at its as-of." example:"history"`
	BooksStart   string                          `json:"books_start" format:"date" doc:"The books start." example:"2026-01-01"`
	Through      string                          `json:"through" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month compared: the workbook's as-of." example:"2026-09"`
	Revision     int64                           `json:"ledger_revision" doc:"The ledger revision Fyniq's figures were read at (ENG-46)." example:"4211"`
	Accounts     int                             `json:"accounts" doc:"The Fyniq accounts compared." example:"64"`
	Figures      int                             `json:"figures" doc:"The figures compared." example:"1216"`
	Differences  []MigrationDifference           `json:"differences" nullable:"false" doc:"The figures that differ, by account, month and figure: the difference register's lines."`
	Checks       []MigrationCheckParity          `json:"checks" nullable:"false" doc:"The workbook's Checks rows against Fyniq's."`
	ChecksRunAt  *time.Time                      `json:"checks_run_at,omitempty" doc:"When Fyniq's checks last ran; absent when they never did." example:"2026-11-13T09:00:00Z"`
	ChecksDiffer int                             `json:"checks_differ" doc:"The workbook's checks whose status Fyniq's do not share." example:"0"`
	Undecided    int                             `json:"undecided" doc:"The register's lines without a decision that counts: the differences, and the checks whose status differs. The sign-off needs none." example:"0"`
	ReportSHA256 string                          `json:"sha256" doc:"The SHA-256 of the report's figures, checks and decisions (not its status, revision, run time or sign-off): what the Accountant's sign-off names, and the ETag (design D8)." example:"5d1e…"`
	SignOff      *MigrationReconciliationSignOff `json:"sign_off,omitempty" doc:"The latest sign-off of the report and its register; absent before the first."`
}

type (
	migrationReconciliationOutput struct {
		ETag string `header:"ETag" doc:"The report's SHA-256, for If-Match: a decision or the sign-off is made on the report as read."`
		Body MigrationReconciliation
	}
	// MigrationRegisterChange is the Accountant's decision on a line.
	MigrationRegisterChange struct {
		Class string `json:"class" enum:"a,b,c" doc:"a: a cent allocation of spreads and depreciation; b: a deliberate rule change; c: a defect of the workbook found during the migration." example:"a"`
		Note  string `json:"note,omitempty" maxLength:"500" doc:"Why; a workbook defect (c) says what it is." example:"ALLOC-EVEN puts the quarter's cent in its last month"`
	}
	migrationRegisterInput struct {
		MigrationPath
		Line string `path:"line" pattern:"^([0-9A-Za-z_-]{1,20}\\.(opening|[0-9]{4}-(0[1-9]|1[0-2])\\.(movement|closing))|check\\.[0-9]{1,3})$" doc:"The line's key, as the report gives it." example:"6610.2026-06.movement"`
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body MigrationRegisterChange
	}
	migrationReconciliationSignInput struct {
		MigrationPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	migrationReconciliationWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The report's SHA-256, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     MigrationReconciliation
	}
)

// openingRules are the rules whose entries are the books' opening at the books start: the opening balances and the
// items open then (FR-GL-06; rev 0.257, 0.261).
var openingRules = map[string]bool{ledger.RuleOpeningBalance: true, ledger.RuleOpeningInvoice: true, ledger.RuleOpeningBill: true,
	ledger.RuleOpeningCreditNote: true, ledger.RuleOpeningVendorCredit: true}

// c1Number is a check's Appendix C.1 number, from its reference ("C.1 #12"), or 0.
func c1Number(ref string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(ref, "C.1 #"))
	if err != nil || !strings.HasPrefix(ref, "C.1 #") {
		return 0
	}
	return n
}

// cents is a figure with the base currency's decimals.
func cents(d *apd.Decimal, places int) string {
	x := new(apd.Decimal)
	if d != nil {
		if _, err := apd.BaseContext.WithPrecision(40).Quantize(x, d, -int32(places)); err != nil { // #nosec G115 -- a currency's minor units
			return d.Text('f')
		}
	}
	return x.Text('f')
}

// booksOf are Fyniq's figures for the parity at a revision (design D8), read through the balance service: each
// account's opening, the entries of the opening rules, and its movement in each month from the books start through
// through, the opening left out.
func booksOf(ctx context.Context, u storage.UnitOfWork, start, through time.Time, revision int64) (migration.Books, error) {
	b := migration.Books{Opening: map[string]*apd.Decimal{}, Movement: map[string]map[time.Time]*apd.Decimal{}, Roles: map[string]string{},
		Names: map[string]string{}}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return b, err
	}
	code := map[uuid.UUID]string{}
	for _, a := range accounts {
		code[a.ID] = a.Code
		b.Roles[a.Code], b.Names[a.Code] = string(a.Role), a.Name
	}
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return b, err
	}
	var ids []uuid.UUID
	for _, p := range periods {
		if !p.EndsOn.Before(start) && !p.StartsOn.After(through) {
			ids = append(ids, p.ID)
		}
	}
	add := func(to *apd.Decimal, x storage.Decimal) error {
		d, _, err := apd.NewFromString(string(x))
		if err != nil {
			return err
		}
		_, err = apd.BaseContext.WithPrecision(40).Add(to, to, d)
		return err
	}
	err = balance.EachEntryIn(ctx, u, ids, revision, func(e storage.Entry) error {
		month := time.Date(e.PostingDate.Year(), e.PostingDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		for _, l := range e.Lines {
			c := code[l.AccountID]
			var to *apd.Decimal
			if openingRules[e.RuleVersion] {
				if b.Opening[c] == nil {
					b.Opening[c] = new(apd.Decimal)
				}
				to = b.Opening[c]
			} else {
				if b.Movement[c] == nil {
					b.Movement[c] = map[time.Time]*apd.Decimal{}
				}
				if b.Movement[c][month] == nil {
					b.Movement[c][month] = new(apd.Decimal)
				}
				to = b.Movement[c][month]
			}
			if err := add(to, l.BaseAmount); err != nil {
				return err
			}
		}
		return nil
	})
	return b, err
}

// errNotCommitted ends the read of a migration whose import is not committed: its report holds nothing.
var errNotCommitted = errors.New("api: the import is not committed")

// reconciliationSource is what a committed import's report is worked out from: its commit, the kept workbook and the
// mapping it imported by.
type reconciliationSource struct {
	commit storage.MigrationCommit
	w      *migration.Workbook
	m      migration.Mapping
}

// notCommittedReport is the report of an import not committed yet: nothing to reconcile.
func notCommittedReport() MigrationReconciliation {
	return MigrationReconciliation{Status: "not_committed", Differences: []MigrationDifference{}, Checks: []MigrationCheckParity{}}
}

// sourceOf reads a migration's committed import: errNotCommitted before the commit.
func (a *API) sourceOf(ctx context.Context, org, id uuid.UUID) (reconciliationSource, error) {
	var src reconciliationSource
	var file storage.MigrationFile
	var mapping storage.MigrationMapping
	err := a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err := migrationRun(ctx, u, id); err != nil {
			return err
		}
		switch src.commit, err = u.Migrations().Commit(ctx, id); {
		case errors.Is(err, ports.ErrNotFound) || err == nil && src.commit.Status != storage.CommitCommitted:
			return errNotCommitted
		case err != nil:
			return err
		}
		if mapping, err = u.Migrations().MappingVersion(ctx, src.commit.MappingID); err != nil {
			return err
		}
		step, err := u.Migrations().Step(ctx, src.commit.StepID)
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
	})
	if err != nil {
		return src, err
	}
	if src.w, err = a.readWorkbook(ctx, file); err != nil {
		return src, err
	}
	return src, json.Unmarshal(mapping.Mapping, &src.m)
}

// decisionOf is a line's decision as the report shows it: stale when made on other figures than the report's.
func decisionOf(d storage.MigrationDecision, fyniq, workbook string) *MigrationRegisterDecision {
	return &MigrationRegisterDecision{Class: d.Class, Note: d.Note, Version: d.Version, DecidedBy: d.DecidedBy, DecidedAt: d.DecidedAt,
		Stale: d.Fyniq != fyniq || d.Workbook != workbook}
}

// checkFigures are a check line's figures as a decision keeps them: Fyniq's statuses by key, and the workbook's.
func checkFigures(c MigrationCheckParity) (string, string) {
	var parts []string
	for _, f := range c.Fyniq {
		parts = append(parts, f.Key+"="+f.Status)
	}
	return strings.Join(parts, ","), c.Workbook
}

// lineFigures are the figures of the report's line by its key, and whether the report has the line.
func (r MigrationReconciliation) lineFigures(key string) (string, string, bool) {
	for _, d := range r.Differences {
		if d.Key == key {
			return d.Fyniq, d.Workbook, true
		}
	}
	for _, c := range r.Checks {
		if c.Key == key && !c.Same {
			f, w := checkFigures(c)
			return f, w, true
		}
	}
	return "", "", false
}

// reportIn is a committed import's parity report in u (design D8), at the ledger's current revision, with the
// register's decisions and the latest sign-off.
func reportIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, src reconciliationSource) (MigrationReconciliation, error) {
	c, w, m := src.commit, src.w, src.m
	out := MigrationReconciliation{Status: "ready", Path: c.Path, Differences: []MigrationDifference{}, Checks: []MigrationCheckParity{}}
	base, err := baseOf(ctx, u)
	if err != nil {
		return out, err
	}
	if out.Revision, err = u.Ledger().Revision(ctx); err != nil {
		return out, err
	}
	decisions, err := u.Migrations().Decisions(ctx, id)
	if err != nil {
		return out, err
	}
	decided := map[string]storage.MigrationDecision{}
	for _, d := range decisions {
		decided[d.Line] = d
	}
	start := w.BooksStart(c.Path)
	through := w.Settings.AsOf
	if through.Before(start) {
		through = start
	}
	books, err := booksOf(ctx, u, start, through, out.Revision)
	if err != nil {
		return out, err
	}
	p := w.Reconcile(m, c.Path, books)
	out.BooksStart, out.Through, out.Accounts, out.Figures = start.Format(time.DateOnly), p.Through.Format("2006-01"), p.Accounts, p.Figures
	for _, f := range p.Differences {
		d := MigrationDifference{Key: f.Key(), Account: f.Account, Name: f.Name, Kind: f.Kind, Fyniq: cents(f.Fyniq, base.MinorUnits),
			Workbook: cents(f.Workbook, base.MinorUnits), Difference: cents(f.Difference, base.MinorUnits), Proposed: f.Proposed}
		if f.Kind != migration.FigureOpening {
			d.Month = f.Month.Format("2006-01")
		}
		if x, ok := decided[d.Key]; ok {
			d.Decision = decisionOf(x, d.Fyniq, d.Workbook)
		}
		if d.Decision == nil || d.Decision.Stale {
			out.Undecided++
		}
		out.Differences = append(out.Differences, d)
	}
	states, err := u.Checks().States(ctx)
	if err != nil {
		return out, err
	}
	state := map[string]storage.CheckState{}
	for _, x := range states {
		state[x.Key] = x
		if out.ChecksRunAt == nil || x.RunAt.After(*out.ChecksRunAt) {
			at := x.RunAt
			out.ChecksRunAt = &at
		}
	}
	// Each of the catalogue's checks of a C.1 number, with its status at its last run; one that never ran is not
	// assessed, which no workbook status is.
	var fyniq []migration.FyniqCheck
	for _, d := range checks.Catalogue() {
		if n := c1Number(d.Ref); n > 0 {
			status := "not_assessed"
			if x, ok := state[d.Key]; ok {
				status = x.Status
			}
			fyniq = append(fyniq, migration.FyniqCheck{Number: n, Key: d.Key, Status: status})
		}
	}
	slices.SortFunc(fyniq, func(a, b migration.FyniqCheck) int { return strings.Compare(a.Key, b.Key) })
	for _, x := range w.CompareChecks(fyniq) {
		v := MigrationCheckParity{Key: "check." + strconv.Itoa(x.Number), Number: x.Number, Name: x.Name, Row: x.Row, Workbook: string(x.Workbook),
			Fyniq: []MigrationFyniqCheck{}, Same: x.Same, Missing: x.Missing}
		for _, f := range x.Fyniq {
			key, status, _ := strings.Cut(f, "=")
			v.Fyniq = append(v.Fyniq, MigrationFyniqCheck{Key: key, Status: status})
		}
		if !x.Same {
			out.ChecksDiffer++
			fy, wb := checkFigures(v)
			if d, ok := decided[v.Key]; ok {
				v.Decision = decisionOf(d, fy, wb)
			}
			if v.Decision == nil || v.Decision.Stale {
				out.Undecided++
			}
		}
		out.Checks = append(out.Checks, v)
	}
	// The hash covers what the report says, not when or where it was read: its figures, checks and decisions.
	hashed := out
	hashed.Status, hashed.Revision, hashed.ChecksRunAt = "", 0, nil
	b, err := json.Marshal(hashed)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(b)
	out.ReportSHA256 = hex.EncodeToString(sum[:])
	offs, err := u.Migrations().SignOffs(ctx, id)
	if err != nil {
		return out, err
	}
	for _, o := range offs {
		if o.Subject == storage.SignOffReconciliation {
			out.SignOff = &MigrationReconciliationSignOff{SignedBy: o.SignedBy, SignedAt: o.SignedAt, SHA256: o.SubjectSHA256, Current: o.SubjectSHA256 == out.ReportSHA256}
		}
	}
	if out.SignOff != nil && out.SignOff.Current {
		out.Status = "signed"
	}
	return out, nil
}

// reconcile is a migration's parity report as it stands.
func (a *API) reconcile(ctx context.Context, org, id uuid.UUID) (MigrationReconciliation, error) {
	src, err := a.sourceOf(ctx, org, id)
	if errors.Is(err, errNotCommitted) {
		return notCommittedReport(), nil
	}
	if err != nil {
		return MigrationReconciliation{}, err
	}
	var out MigrationReconciliation
	err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = reportIn(ctx, u, id, src)
		return err
	})
	return out, err
}

func registerMigrationReconciliation(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid,
		CodeNotFound, CodeUnavailable, CodeIfMatchRequired, CodeStale, storage.CodeVersion, storage.CodeMigration}, locks...)
	register(a, authz.GetMigrationReconciliation, huma.Operation{
		OperationID: "getMigrationReconciliation", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/reconciliation", Tags: []string{"Migration"},
		Summary: "Get the reconciliation",
		Description: "The parity report of a committed import (§17.1 M4, FR-MIG-02, QA-MIG-01; design D8): Fyniq's books against the workbook's own " +
			"engine, its Trial Balance sheet, to the cent after RND-IMPORT. On the history, each Fyniq account's opening at the books start and " +
			"its movement and closing each month through the workbook's as-of; on a clean start, its opening against the Carry Forward sheet's " +
			"balance at the as-of; the workbook's accounts that map onto one Fyniq account are added. Each figure that differs is a line of the " +
			"difference register, with a proposed class (a, a cent allocation; b, a deliberate rule change) and the Accountant's decision; so " +
			"is each of the workbook's Checks rows whose status Fyniq's checks of its Appendix C.1 number, at their last run, do not share. A " +
			"decision made on other figures is stale. sha256, the ETag, is what a decision and the sign-off are made on. Before the import is " +
			"committed, status not_committed and no figures. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeUnavailable}, locks...),
		func(ctx context.Context, in *MigrationPath) (*migrationReconciliationOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			r, err := a.reconcile(ctx, p.Organisation, id)
			if err != nil {
				return nil, err
			}
			out := &migrationReconciliationOutput{Body: r}
			if r.ReportSHA256 != "" {
				out.ETag = `"` + r.ReportSHA256 + `"`
			}
			return out, nil
		})

	// change runs a decision or the sign-off on the report as read (If-Match), in one unit of work.
	change := func(ctx context.Context, id uuid.UUID, key, prefer, ifMatch string, request any,
		do func(ctx context.Context, u storage.UnitOfWork, r MigrationReconciliation, src reconciliationSource) error) (*migrationReconciliationWriteOutput, error) {
		p, _ := authn.From(ctx)
		src, err := a.sourceOf(ctx, p.Organisation, id)
		if errors.Is(err, errNotCommitted) {
			return nil, problem(migration.CodeCommit, "The import is not committed yet, so there is no register to decide or sign.",
				"Run the commit's batches until it is committed, then reconcile.")
		}
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, key, prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MigrationReconciliation, error) {
			r, err := reportIn(ctx, u, id, src)
			if err != nil {
				return r, err
			}
			if err := checkIfMatch(ifMatch, `"`+r.ReportSHA256+`"`); err != nil {
				return r, err
			}
			if err := do(ctx, u, r, src); err != nil {
				return r, err
			}
			return reportIn(ctx, u, id, src)
		})
		if err != nil {
			return nil, err
		}
		out := &migrationReconciliationWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
		if !w.Previewed {
			out.ETag = `"` + w.Body.ReportSHA256 + `"`
		}
		return out, nil
	}

	register(a, authz.DecideMigrationDifference, huma.Operation{
		OperationID: "decideMigrationDifference", Method: http.MethodPatch, Path: "/v1/migrations/{migration_id}/reconciliation/register/{line}",
		Tags: []string{"Migration"}, Summary: "Decide a line of the register",
		Description: "The Accountant decides a line of the difference register (FR-MIG-02, §17.1 M4; rev 0.266): a figure that differs, or a " +
			"workbook check whose status Fyniq's do not share, as a (a cent allocation of spreads and depreciation), b (a deliberate rule " +
			"change) or c (a workbook defect found during the migration, with a note saying what it is). The decision keeps the line's two " +
			"figures: once they move it is stale and the line is decided again, as its next version. A line the report does not have is " +
			"MIGRATION-15; before the import is committed, MIGRATION-14. Needs If-Match with the report's ETag: the decision is made on the " +
			"report as read. Side effects: the decision and the audit event migration.difference_decided. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{migration.CodeCommit, migration.CodeRegister}, writes...),
		func(ctx context.Context, in *migrationRegisterInput) (*migrationReconciliationWriteOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			return change(ctx, id, in.IdempotencyKey, in.Prefer, in.IfMatch, in, func(ctx context.Context, u storage.UnitOfWork, r MigrationReconciliation, _ reconciliationSource) error {
				fyniq, workbook, ok := r.lineFigures(in.Line)
				if !ok {
					return problem(migration.CodeRegister, fmt.Sprintf("The register has no line %s: the report shows no difference there.", in.Line),
						"Decide a line the report lists, by its key.", FieldProblem{Field: "line", Code: migration.CodeRegister,
							Message: "is not a line of the report", Fix: "Read the report again"})
				}
				if in.Body.Class == migration.ClassDefect && strings.TrimSpace(in.Body.Note) == "" {
					return problem(migration.CodeRegister, "A workbook defect (class c) says what it is.", "Write what the workbook got wrong in the note.",
						FieldProblem{Field: "note", Code: migration.CodeRegister, Message: "is empty", Fix: "Say what the workbook got wrong"})
				}
				version := 0
				decisions, err := u.Migrations().Decisions(ctx, id)
				if err != nil {
					return err
				}
				for _, d := range decisions {
					if d.Line == in.Line {
						version = d.Version
					}
				}
				_, err = u.Migrations().Decide(ctx, storage.MigrationDecision{RunID: id, Line: in.Line, Class: in.Body.Class, Note: strings.TrimSpace(in.Body.Note),
					Fyniq: fyniq, Workbook: workbook, Version: version, DecidedBy: uploader(ctx), DecidedAt: now})
				return err
			})
		})

	register(a, authz.SignMigrationReconciliation, huma.Operation{
		OperationID: "signMigrationReconciliation", Method: http.MethodPost, Path: "/v1/migrations/{migration_id}/reconciliation/sign-off",
		Tags: []string{"Migration"}, Summary: "Sign the reconciliation",
		Description: "The Accountant signs the parity report and its difference register together (§17.1 M4, FR-MIG-02; rev 0.266), over the " +
			"report's SHA-256, stepped up in the last 5 minutes. Refused (MIGRATION-15) while a line has no decision that counts (undecided, " +
			"or stale), or Fyniq's checks have never run; a report is signed once. Once the books or a decision change, the report is another " +
			"and shows the earlier sign-off as not current. Needs If-Match with the report's ETag. Side effects: the sign-off and the audit " +
			"event migration.reconciliation_signed, with the ledger revision the report was read at. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeSessionRequired, CodeStepUpRequired, migration.CodeCommit, migration.CodeRegister}, writes...),
		func(ctx context.Context, in *migrationReconciliationSignInput) (*migrationReconciliationWriteOutput, error) {
			id, err := parseID("migration_id", in.MigrationID)
			if err != nil {
				return nil, err
			}
			person, err := a.steppedUpPerson(ctx, "Signing the reconciliation")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			return change(ctx, id, in.IdempotencyKey, in.Prefer, in.IfMatch, in, func(ctx context.Context, u storage.UnitOfWork, r MigrationReconciliation, _ reconciliationSource) error {
				switch {
				case r.Undecided > 0:
					return problem(migration.CodeRegister, fmt.Sprintf("%d lines of the register have no decision that counts.", r.Undecided),
						"Decide each line the report lists, again where its figures moved, then sign.")
				case r.ChecksRunAt == nil:
					return problem(migration.CodeRegister, "Fyniq's checks have never run, so the workbook's cannot be compared.",
						"Run the checks, decide each that differs, then sign.")
				case r.SignOff != nil && r.SignOff.Current:
					return problem(migration.CodeRegister, "The report is signed already.", "Nothing to do: the step is signed.")
				}
				audit := storage.AuditFrom(ctx)
				audit.Reason = fmt.Sprintf("The parity report at ledger revision %d and its register (FR-MIG-02)", r.Revision)
				_, err := u.Migrations().SignOff(storage.WithAudit(ctx, audit), storage.MigrationSignOff{RunID: id, Subject: storage.SignOffReconciliation,
					SubjectSHA256: r.ReportSHA256, SignedBy: "user:" + person.UserID.String(), SignedAt: now})
				return err
			})
		})
}
