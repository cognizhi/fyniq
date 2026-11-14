package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	Key        string `json:"key" doc:"The line's key in the difference register: the Fyniq account's code, the month (YYYY-MM) and the figure, or the code and opening." example:"6510.2026-03.movement"`
	Account    string `json:"account" doc:"The Fyniq account's code; the workbook's accounts that map onto it are added." example:"6510"`
	Name       string `json:"name" doc:"The account's name in Fyniq." example:"Depreciation"`
	Kind       string `json:"kind" enum:"opening,movement,closing" doc:"opening: the balance at the books start; movement: the month's; closing: the balance at the month's end." example:"movement"`
	Month      string `json:"month,omitempty" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month (YYYY-MM); absent for the opening." example:"2026-03"`
	Fyniq      string `json:"fyniq" doc:"Fyniq's figure in the base currency, debit positive." example:"620.00"`
	Workbook   string `json:"workbook" doc:"The workbook's, from its Trial Balance sheet after RND-IMPORT." example:"620.01"`
	Difference string `json:"difference" doc:"Fyniq's less the workbook's." example:"-0.01"`
	Proposed   string `json:"proposed_class" enum:",a,b,c" doc:"The class proposed (design D8): a, a cent allocation on a spread or depreciation account within a cent a month; b, a deliberate rule change on a revaluation or statutory account; empty when none fits, for the Accountant to decide." example:"a"`
}

// MigrationFyniqCheck is one of Fyniq's checks of a workbook check's Appendix C.1 number, with its status.
type MigrationFyniqCheck struct {
	Key    string `json:"key" doc:"The check's key." example:"books.debits-equal-credits"`
	Status string `json:"status" enum:"ok,warning,error,due,not_assessed,failed" doc:"Its status at its last run." example:"ok"`
}

// MigrationCheckParity is one of the workbook's Checks rows against Fyniq's checks of its number.
type MigrationCheckParity struct {
	Number   int                   `json:"number" doc:"The workbook check's number, which is its Appendix C.1 number." example:"1"`
	Name     string                `json:"name" doc:"What the workbook checks." example:"Debits equal credits (trial balance)"`
	Row      int                   `json:"row" doc:"Its row on the Checks sheet." example:"7"`
	Workbook string                `json:"workbook" enum:"fine,review,blocked" doc:"Its status in the workbook: fine ✔, review ⚠ or blocked ✖." example:"fine"`
	Fyniq    []MigrationFyniqCheck `json:"fyniq" nullable:"false" doc:"Fyniq's checks of the number."`
	Same     bool                  `json:"same" doc:"Whether the worst of Fyniq's statuses is the workbook's: a different one needs a register line or a fix." example:"true"`
	Missing  bool                  `json:"missing" doc:"Fyniq has no check of the number (the workbook's own checks of its sheets, such as Reports frozen)." example:"false"`
}

// MigrationReconciliation is the parity report (§17.1 M4, design D8): every figure of the workbook's Trial Balance sheet
// compared with Fyniq's books, the differences, and the workbook's checks against Fyniq's.
type MigrationReconciliation struct {
	Status       string                 `json:"status" enum:"not_committed,ready" doc:"not_committed: the import is not committed yet, so there is nothing to reconcile and the report holds no figures; ready: the report of the books as they stand." example:"ready"`
	Path         string                 `json:"path" enum:"history,clean_start" doc:"The path imported: history compares the opening and each month's movement and closing; clean_start the opening against the workbook's closing at its as-of." example:"history"`
	BooksStart   string                 `json:"books_start" format:"date" doc:"The books start." example:"2026-01-01"`
	Through      string                 `json:"through" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month compared: the workbook's as-of." example:"2026-09"`
	Revision     int64                  `json:"ledger_revision" doc:"The ledger revision Fyniq's figures were read at (ENG-46)." example:"4211"`
	Accounts     int                    `json:"accounts" doc:"The Fyniq accounts compared." example:"64"`
	Figures      int                    `json:"figures" doc:"The figures compared." example:"1216"`
	Differences  []MigrationDifference  `json:"differences" nullable:"false" doc:"The figures that differ, by account, month and figure: the difference register's lines."`
	Checks       []MigrationCheckParity `json:"checks" nullable:"false" doc:"The workbook's Checks rows against Fyniq's."`
	ChecksRunAt  *time.Time             `json:"checks_run_at,omitempty" doc:"When Fyniq's checks last ran; absent when they never did." example:"2026-11-13T09:00:00Z"`
	ChecksDiffer int                    `json:"checks_differ" doc:"The workbook's checks whose status Fyniq's do not share." example:"0"`
	ReportSHA256 string                 `json:"sha256" doc:"The SHA-256 of the report as given, without this field: what the Accountant's sign-off names (design D8)." example:"5d1e…"`
}

type migrationReconciliationOutput struct {
	Body MigrationReconciliation
}

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

// reconcile is a committed migration's parity report (design D8), at the ledger's current revision.
func (a *API) reconcile(ctx context.Context, org, id uuid.UUID) (MigrationReconciliation, error) {
	var c storage.MigrationCommit
	var file storage.MigrationFile
	var mapping storage.MigrationMapping
	err := a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err := migrationRun(ctx, u, id); err != nil {
			return err
		}
		switch c, err = u.Migrations().Commit(ctx, id); {
		case errors.Is(err, ports.ErrNotFound) || err == nil && c.Status != storage.CommitCommitted:
			return errNotCommitted
		case err != nil:
			return err
		}
		if mapping, err = u.Migrations().MappingVersion(ctx, c.MappingID); err != nil {
			return err
		}
		step, err := u.Migrations().Step(ctx, c.StepID)
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
	if errors.Is(err, errNotCommitted) {
		return MigrationReconciliation{Status: "not_committed", Differences: []MigrationDifference{}, Checks: []MigrationCheckParity{}}, nil
	}
	if err != nil {
		return MigrationReconciliation{}, err
	}
	w, err := a.readWorkbook(ctx, file)
	if err != nil {
		return MigrationReconciliation{}, err
	}
	var m migration.Mapping
	if err := json.Unmarshal(mapping.Mapping, &m); err != nil {
		return MigrationReconciliation{}, err
	}
	out := MigrationReconciliation{Status: "ready", Path: c.Path, Differences: []MigrationDifference{}, Checks: []MigrationCheckParity{}}
	err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		base, err := baseOf(ctx, u)
		if err != nil {
			return err
		}
		if out.Revision, err = u.Ledger().Revision(ctx); err != nil {
			return err
		}
		start := w.BooksStart(c.Path)
		through := w.Settings.AsOf
		if through.Before(start) {
			through = start
		}
		books, err := booksOf(ctx, u, start, through, out.Revision)
		if err != nil {
			return err
		}
		p := w.Reconcile(m, c.Path, books)
		out.BooksStart, out.Through, out.Accounts, out.Figures = start.Format(time.DateOnly), p.Through.Format("2006-01"), p.Accounts, p.Figures
		for _, f := range p.Differences {
			d := MigrationDifference{Key: f.Key(), Account: f.Account, Name: f.Name, Kind: f.Kind, Fyniq: cents(f.Fyniq, base.MinorUnits),
				Workbook: cents(f.Workbook, base.MinorUnits), Difference: cents(f.Difference, base.MinorUnits), Proposed: f.Proposed}
			if f.Kind != migration.FigureOpening {
				d.Month = f.Month.Format("2006-01")
			}
			out.Differences = append(out.Differences, d)
		}
		states, err := u.Checks().States(ctx)
		if err != nil {
			return err
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
			v := MigrationCheckParity{Number: x.Number, Name: x.Name, Row: x.Row, Workbook: string(x.Workbook), Fyniq: []MigrationFyniqCheck{},
				Same: x.Same, Missing: x.Missing}
			for _, f := range x.Fyniq {
				key, status, _ := strings.Cut(f, "=")
				v.Fyniq = append(v.Fyniq, MigrationFyniqCheck{Key: key, Status: status})
			}
			if !x.Same {
				out.ChecksDiffer++
			}
			out.Checks = append(out.Checks, v)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(b)
	out.ReportSHA256 = hex.EncodeToString(sum[:])
	return out, nil
}

func registerMigrationReconciliation(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetMigrationReconciliation, huma.Operation{
		OperationID: "getMigrationReconciliation", Method: http.MethodGet, Path: "/v1/migrations/{migration_id}/reconciliation", Tags: []string{"Migration"},
		Summary: "Get the reconciliation",
		Description: "The parity report of a committed import (§17.1 M4, FR-MIG-02, QA-MIG-01; design D8): Fyniq's books against the workbook's own " +
			"engine, its Trial Balance sheet, to the cent after RND-IMPORT. On the history, each Fyniq account's opening at the books start and " +
			"its movement and closing each month through the workbook's as-of; on a clean start, its opening against the Carry Forward sheet's " +
			"balance at the as-of; the workbook's accounts that map onto one Fyniq account are added. Each figure that differs is a line of the difference " +
			"register, with a proposed class (a, a cent allocation; b, a deliberate rule change) the Accountant decides. The workbook's Checks " +
			"rows are compared, by their Appendix C.1 number, with the statuses of Fyniq's checks at their last run. sha256 is what the " +
			"sign-off names. Before the import is committed, status not_committed and no figures. Read-only; no side effects.",
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
			return &migrationReconciliationOutput{Body: r}, nil
		})
}
