package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/reportdef"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// More stable codes of the report builder (ENG-23); RDEF-01 to RDEF-08 are the definition's own (domain/reportdef).
const (
	CodeReportNotYours  = "RDEF-09" // a private report is its owner's, and only its owner, or an Owner or Administrator for a shared one, changes it
	CodeReportScheduler = "RDEF-10" // scheduling a report is for the Owner, Administrator, Controller and Accountant
	CodeReportRecipient = "RDEF-11" // a recipient who is no active member, or may not read the report
)

var reportDefCodes = []string{reportdef.CodeSchema, reportdef.CodeRow, reportdef.CodeRowFields, reportdef.CodeFormula, reportdef.CodeReference,
	reportdef.CodeLimit, reportdef.CodeColumn, reportdef.CodeFilter}

// MemberUserID is a member's user ID, as listMembers gives it.
type MemberUserID string

// Schema implements huma.SchemaProvider: a UUID string, with an example.
func (MemberUserID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A member's user ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"}}
}

// ReportRowBody is one row of a report definition (design D1).
type ReportRowBody struct {
	ID      string `json:"id" minLength:"1" maxLength:"24" doc:"The row's ID: lower-case letters, digits and underscores, starting with a letter. A formula names a row by it." example:"rev"`
	Label   string `json:"label" minLength:"1" maxLength:"80" doc:"What the row is called." example:"Revenue"`
	Kind    string `json:"kind" enum:"account,group,calc,heading" doc:"account: one account by its code; group: every account of a report group of the chart; calc: a formula over rows above it; heading: a label with no figures." example:"group"`
	Code    string `json:"code,omitempty" maxLength:"20" doc:"An account row's account code." example:"4000"`
	Group   string `json:"group,omitempty" maxLength:"80" doc:"A group row's report group, as the chart names it." example:"Revenue"`
	Formula string `json:"formula,omitempty" maxLength:"300" doc:"A calc row's formula: numbers, the IDs of rows above it, + - * / and brackets, such as (rev - cogs) / rev. A division by zero is not available, never zero." example:"(rev - cogs) / rev"`
	Format  string `json:"format,omitempty" enum:"amount,percent,ratio" doc:"A calc row's format: amount (the currency's decimals), percent (the quotient times 100, 1 decimal) or ratio (2 decimals), rounded once, half away from zero (RND-KPI)." example:"percent"`
}

// ReportColumnBody is one column of a report definition.
type ReportColumnBody struct {
	Key   string `json:"key" enum:"current,prior_period,prior_year,ytd" doc:"current: the run asked for; prior_period: the run of the same length just before; prior_year: the same months a year earlier; ytd: the financial year's first month to the run's last. A balance sheet account's figure is its position at the column's last day, a P&L account's the movement over the column's months." example:"current"`
	Label string `json:"label,omitempty" maxLength:"40" doc:"The column's heading; left out, the builder names it." example:"This month"`
}

// ReportFiltersBody are a definition's filters.
type ReportFiltersBody struct {
	DimensionValueIDs []DimensionValueID `json:"dimension_value_ids,omitempty" maxItems:"10" doc:"The P&L rows count only the lines that carry each of these dimension values (FR-DIM-02); a balance sheet row is not narrowed, since a dimension's lines are one side of their entries."`
	IncludeZero       bool               `json:"include_zero,omitempty" doc:"Show an account or group row whose figures are all zero; off by default." example:"false"`
}

// ReportDefinitionBody is a report definition, fyniq-report@1.
type ReportDefinitionBody struct {
	Schema  string             `json:"schema" enum:"fyniq-report@1" doc:"The definition's schema." example:"fyniq-report@1"`
	Rows    []ReportRowBody    `json:"rows" minItems:"1" maxItems:"60" nullable:"false" doc:"The rows, in order; a formula uses only rows above it."`
	Columns []ReportColumnBody `json:"columns" minItems:"1" maxItems:"12" nullable:"false" doc:"The columns, in order."`
	Filters ReportFiltersBody  `json:"filters" doc:"The filters."`
}

func (b ReportDefinitionBody) domain() reportdef.Definition {
	d := reportdef.Definition{Schema: b.Schema, Filters: reportdef.Filters{IncludeZero: b.Filters.IncludeZero}}
	for _, v := range b.Filters.DimensionValueIDs {
		d.Filters.DimensionValueIDs = append(d.Filters.DimensionValueIDs, string(v))
	}
	for _, r := range b.Rows {
		d.Rows = append(d.Rows, reportdef.Row(r))
	}
	for _, c := range b.Columns {
		d.Columns = append(d.Columns, reportdef.Column(c))
	}
	return d
}

func reportBodyOf(d reportdef.Definition) ReportDefinitionBody {
	b := ReportDefinitionBody{Schema: d.Schema, Rows: []ReportRowBody{}, Columns: []ReportColumnBody{}, Filters: ReportFiltersBody{IncludeZero: d.Filters.IncludeZero}}
	for _, v := range d.Filters.DimensionValueIDs {
		b.Filters.DimensionValueIDs = append(b.Filters.DimensionValueIDs, DimensionValueID(v))
	}
	for _, r := range d.Rows {
		b.Rows = append(b.Rows, ReportRowBody(r))
	}
	for _, c := range d.Columns {
		b.Columns = append(b.Columns, ReportColumnBody(c))
	}
	return b
}

// checkReportDefinition refuses a definition that breaks a rule, naming each field.
func checkReportDefinition(b ReportDefinitionBody) (reportdef.Definition, error) {
	d := b.domain()
	var bad []FieldProblem
	for _, p := range reportdef.Validate(d) {
		bad = append(bad, FieldProblem{Code: p.Code, Field: "body.definition." + p.Field, Message: p.Message, Fix: p.Fix})
	}
	if len(bad) > 0 {
		return d, invalid(bad)
	}
	return d, nil
}

// ReportDefinition is a saved report (FR-RPT-04).
type ReportDefinition struct {
	ID          string               `json:"id" format:"uuid" doc:"The report's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
	Name        string               `json:"name" doc:"Its name, unique among the live reports of the organisation." example:"Gross margin by month"`
	Description string               `json:"description,omitempty" doc:"What it is for." example:"The board's margin view"`
	Visibility  string               `json:"visibility" enum:"private,shared" doc:"private: its owner's alone; shared: every person who may read the whole ledger's reports can run it." example:"shared"`
	OwnerID     string               `json:"owner_id" format:"uuid" doc:"The member who made it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Mine        bool                 `json:"mine" doc:"True when the caller made it." example:"true"`
	Definition  ReportDefinitionBody `json:"definition" doc:"Its rows, columns and filters."`
	Version     int                  `json:"version" doc:"Its version, which is its ETag." example:"1"`
	UpdatedAt   time.Time            `json:"updated_at" doc:"When it last changed." example:"2026-11-23T03:00:00Z"`
	Archived    bool                 `json:"archived" doc:"True when it is archived: it stays for the schedules and packs that named it, and runs no more." example:"false"`
}

// ReportDefinitionPage is a page of reports.
type ReportDefinitionPage struct {
	Items      []ReportDefinition `json:"items" nullable:"false" doc:"The reports the caller may read: their own and the shared ones, by name."`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
}

// ReportDefinitionIn is a report to save.
type ReportDefinitionIn struct {
	Name        string               `json:"name" minLength:"1" maxLength:"80" doc:"Its name." example:"Gross margin by month"`
	Description string               `json:"description,omitempty" maxLength:"500" doc:"What it is for." example:"The board's margin view"`
	Visibility  string               `json:"visibility" enum:"private,shared" doc:"private keeps it to its owner; shared lets every person who may read the whole ledger's reports run it. Running always checks the runner's own rights." example:"shared"`
	Definition  ReportDefinitionBody `json:"definition" doc:"Its rows, columns and filters."`
}

// ReportRunColumn is a column of a run.
type ReportRunColumn struct {
	Key       string `json:"key" doc:"The column's key." example:"current"`
	Label     string `json:"label" doc:"Its heading." example:"Oct 2026"`
	From      string `json:"from,omitempty" format:"date" doc:"Its first day; absent when the books lack the months." example:"2026-10-01"`
	To        string `json:"to,omitempty" format:"date" doc:"Its last day." example:"2026-10-31"`
	Available bool   `json:"available" doc:"False when the books do not have all the months the column needs, as a prior year they do not have: its cells are not available." example:"true"`
}

// ReportRunCell is one figure.
type ReportRunCell struct {
	Value        string `json:"value,omitempty" doc:"A plain decimal with an ASCII minus: an amount at the currency's decimals, a percent to 1 decimal, a ratio to 2." example:"1000.00"`
	NotAvailable bool   `json:"not_available,omitempty" doc:"True when the figure cannot be worked out (a division by zero, or a column the books lack): never zero." example:"false"`
}

// ReportRunRow is a row of a run.
type ReportRunRow struct {
	ID     string          `json:"id" doc:"The row's ID." example:"rev"`
	Label  string          `json:"label" doc:"Its label." example:"Revenue"`
	Kind   string          `json:"kind" enum:"account,group,calc,heading" doc:"Its kind." example:"group"`
	Format string          `json:"format,omitempty" enum:"amount,percent,ratio" doc:"Its figures' format; a heading has none." example:"amount"`
	Cells  []ReportRunCell `json:"cells" nullable:"false" doc:"One per column."`
}

// ReportRun is a report worked out as at a ledger revision (FR-RPT-04, FR-RPT-08).
type ReportRun struct {
	DefinitionID    string            `json:"definition_id,omitempty" format:"uuid" doc:"The saved report it ran; absent for a draft run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
	DefinitionVer   int               `json:"definition_version,omitempty" doc:"The version it ran." example:"1"`
	Currency        string            `json:"currency" doc:"The base currency of the amounts." example:"SGD"`
	From            string            `json:"from" format:"date" doc:"The run's first day." example:"2026-10-01"`
	To              string            `json:"to" format:"date" doc:"The run's last day." example:"2026-10-31"`
	Revision        int64             `json:"ledger_revision" doc:"The ledger revision every figure was read at (FR-RPT-08)." example:"42"`
	PlatformVersion string            `json:"platform_version" doc:"The version of Fyniq that worked it out." example:"0.1.0"`
	Columns         []ReportRunColumn `json:"columns" nullable:"false" doc:"The columns."`
	Rows            []ReportRunRow    `json:"rows" nullable:"false" doc:"The rows; an account or group row of zeros is left out unless the definition includes it."`
}

// ReportDefinitionPath names a saved report in the path.
type ReportDefinitionPath struct {
	ID string `path:"id" format:"uuid" doc:"The report's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
}

// ReportRunQuery is the run a report is worked out for.
type ReportRunQuery struct {
	From     string `query:"from" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's first month, YYYY-MM; left out, the last month's." example:"2026-10"`
	To       string `query:"to" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's last month, YYYY-MM; left out, the month of the organisation's today." example:"2026-10"`
	Revision int64  `query:"as_at_revision" minimum:"1" doc:"Read as at this ledger revision (FR-RPT-08); left out, the ledger as it stands." example:"42"`
}

type listReportDefinitionsInput struct {
	PageQuery
	Archived bool `query:"include_archived" doc:"Include the archived reports." example:"false"`
}

type reportDefinitionOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	ETag     string `header:"ETag" doc:"The report's version, for If-Match."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ReportDefinition
}

type reportDefinitionsOutput struct{ Body ReportDefinitionPage }

type createReportDefinitionInput struct {
	KeyHeader
	PreferHeader
	Body ReportDefinitionIn
}

type replaceReportDefinitionInput struct {
	ReportDefinitionPath
	IfMatchHeader
	PreferHeader
	Body ReportDefinitionIn
}

type archiveReportDefinitionInput struct {
	ReportDefinitionPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type getReportDefinitionInput struct{ ReportDefinitionPath }

type runReportDefinitionInput struct {
	ReportDefinitionPath
	ReportRunQuery
}

type runReportDraftInput struct {
	Body struct {
		Definition ReportDefinitionBody `json:"definition" doc:"The definition to run; nothing is saved."`
		From       string               `json:"from,omitempty" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's first month, YYYY-MM; left out, the last month's." example:"2026-10"`
		To         string               `json:"to,omitempty" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's last month, YYYY-MM; left out, the month of the organisation's today." example:"2026-10"`
		Revision   int64                `json:"as_at_revision,omitempty" minimum:"1" doc:"Read as at this ledger revision." example:"42"`
	}
}

type reportRunOutput struct{ Body ReportRun }

func reportDefinitionOf(d storage.ReportDefinition, caller uuid.UUID) ReportDefinition {
	var def reportdef.Definition
	_ = json.Unmarshal(d.Definition, &def) // written by the API: always a definition
	return ReportDefinition{ID: d.ID.String(), Name: d.Name, Description: d.Description, Visibility: d.Visibility, OwnerID: d.OwnerID.String(),
		Mine: d.OwnerID == caller, Definition: reportBodyOf(def), Version: d.Version, UpdatedAt: d.UpdatedAt.UTC(), Archived: d.ArchivedAt != nil}
}

func hasAnyRole(roles []authz.Role, want ...authz.Role) bool {
	return slices.ContainsFunc(roles, func(r authz.Role) bool { return slices.Contains(want, r) })
}

// mayReadReport says whether the caller may open a saved report: their own, or a shared one.
func mayReadReport(d storage.ReportDefinition, p authn.Principal) bool {
	return d.Visibility == storage.ReportShared || d.OwnerID == p.UserID
}

// mayChangeReport says whether the caller may change or archive a report: its owner, or an Owner or Administrator for a shared one.
func mayChangeReport(d storage.ReportDefinition, p authn.Principal) bool {
	return d.OwnerID == p.UserID || d.Visibility == storage.ReportShared && hasAnyRole(p.Roles, authz.Owner, authz.Administrator)
}

func reportNotFound() error {
	return problem(CodeNotFound, "No report with that ID is open to you.", "List the reports and send the ID of one")
}

// savedReport reads a report the caller may open, or says there is none.
func savedReport(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id string) (storage.ReportDefinition, error) {
	rid, err := parseID("id", id)
	if err != nil {
		return storage.ReportDefinition{}, err
	}
	d, err := u.ReportDefinitions().Get(ctx, rid)
	if errors.Is(err, ports.ErrNotFound) || err == nil && !mayReadReport(d, p) {
		return storage.ReportDefinition{}, reportNotFound()
	}
	return d, err
}

func reportETag(version int) string { return `"` + strconv.Itoa(version) + `"` }

// monthLabel is a period run's heading: "Oct 2026", or "Jul to Oct 2026".
func monthLabel(first, last storage.Period) string {
	a, b := first.StartsOn, last.StartsOn
	switch {
	case a.Equal(b):
		return b.Format("Jan 2006")
	case a.Year() == b.Year():
		return a.Format("Jan") + " to " + b.Format("Jan 2006")
	}
	return a.Format("Jan 2006") + " to " + b.Format("Jan 2006")
}

// reportColumns are the periods each of the definition's columns reads, and whether the books have them.
func (a *API) reportColumns(ctx context.Context, u storage.UnitOfWork, d reportdef.Definition, r run, to string) ([]financials.ColumnSpec, []bool, error) {
	all, first, last := r.all, r.first, r.last
	current := all[first : last+1]
	n := len(current)
	ytdFirst := first
	if to == "" {
		to = all[last].StartsOn.Format(statementMonthForm)
	}
	if ys, err := yearStart(ctx, u, to); err != nil {
		return nil, nil, err
	} else if ys != "" {
		for i, p := range all {
			if p.StartsOn.Format(statementMonthForm) == ys {
				ytdFirst = min(i, first)
				break
			}
		}
	}
	specs := make([]financials.ColumnSpec, len(d.Columns))
	have := make([]bool, len(d.Columns))
	for i, c := range d.Columns {
		var ps []storage.Period
		label := c.Label
		switch c.Key {
		case reportdef.ColCurrent:
			ps = current
			if label == "" {
				label = monthLabel(all[first], all[last])
			}
		case reportdef.ColPriorPeriod:
			if first-n >= 0 {
				ps = all[first-n : first]
			}
			if label == "" {
				label = "Prior period"
			}
		case reportdef.ColPriorYear:
			for _, p := range current {
				for _, q := range all {
					if q.StartsOn.Equal(p.StartsOn.AddDate(-1, 0, 0)) && q.EndsOn.Equal(p.EndsOn.AddDate(-1, 0, 0)) {
						ps = append(ps, q)
					}
				}
			}
			if len(ps) != n {
				ps = nil
			}
			if label == "" {
				label = "Prior year"
			}
		case reportdef.ColYTD:
			ps = all[ytdFirst : last+1]
			if label == "" {
				label = "Year to date"
			}
		}
		specs[i], have[i] = financials.ColumnSpec{Key: c.Key, Label: label, Periods: ps}, len(ps) > 0
	}
	return specs, have, nil
}

// workReportDefinition works a definition out in u as at a revision (FR-RPT-04, FR-RPT-08).
func (a *API) workReportDefinition(ctx context.Context, u storage.UnitOfWork, d reportdef.Definition, q ReportRunQuery) (ReportRun, error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return ReportRun{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return ReportRun{}, err
	}
	rn, err := a.monthsOf(ctx, u, q.From, q.To, q.Revision)
	if err != nil {
		return ReportRun{}, err
	}
	var only []uuid.UUID
	for i, s := range d.Filters.DimensionValueIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return ReportRun{}, invalid([]FieldProblem{{Code: reportdef.CodeFilter, Field: "definition.filters.dimension_value_ids", Message: "is not a dimension value's ID", Fix: "Send the ID listDimensionValues gives"}})
		}
		if _, err := u.Dimensions().GetValue(ctx, id); errors.Is(err, ports.ErrNotFound) {
			return ReportRun{}, invalid([]FieldProblem{{Code: reportdef.CodeFilter, Field: "definition.filters.dimension_value_ids[" + strconv.Itoa(i) + "]",
				Message: "is not one of the organisation's dimension values", Fix: "Send the ID listDimensionValues gives"}})
		} else if err != nil {
			return ReportRun{}, err
		}
		only = append(only, id)
	}
	specs, have, err := a.reportColumns(ctx, u, d, rn, q.To)
	if err != nil {
		return ReportRun{}, err
	}
	figs, accounts, err := financials.AccountFigures(ctx, u, base, rn.revision, rn.all, specs, only)
	if err != nil {
		return ReportRun{}, err
	}
	rows, err := reportdef.Compute(d, base, accounts, figs)
	if err != nil {
		return ReportRun{}, invalid([]FieldProblem{{Code: reportdef.CodeRowFields, Field: "definition", Message: err.Error(), Fix: "Name an account of the chart, or load the standard chart"}})
	}
	out := ReportRun{Currency: base.Code, From: dateOf(rn.all[rn.first].StartsOn), To: dateOf(rn.all[rn.last].EndsOn), Revision: rn.revision,
		PlatformVersion: a.deps.Version, Columns: []ReportRunColumn{}, Rows: []ReportRunRow{}}
	for i, s := range specs {
		c := ReportRunColumn{Key: s.Key, Label: s.Label, Available: have[i]}
		if have[i] {
			c.From, c.To = dateOf(s.Periods[0].StartsOn), dateOf(s.Periods[len(s.Periods)-1].EndsOn)
		}
		out.Columns = append(out.Columns, c)
	}
	for _, r := range rows {
		x := ReportRunRow{ID: r.ID, Label: r.Label, Kind: r.Kind, Format: r.Format, Cells: make([]ReportRunCell, len(r.Cells))}
		for i, c := range r.Cells {
			if !have[i] && r.Kind != reportdef.KindHeading {
				x.Cells[i] = ReportRunCell{NotAvailable: true}
				continue
			}
			x.Cells[i] = ReportRunCell{Value: c.Value, NotAvailable: c.NA}
		}
		out.Rows = append(out.Rows, x)
	}
	return out, nil
}

func registerReportDefinitions(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)
	runCodes := append([]string{CodeInvalid, CodeNotFound, CodeNoPeriod, CodeLaterRevision}, reportDefCodes...)

	register(a, authz.ListReportDefinitions, huma.Operation{
		OperationID: "listReportDefinitions", Method: http.MethodGet, Path: "/v1/report-definitions", Tags: []string{"Reports"},
		Summary: "List the saved reports",
		Description: "The reports the caller may open: their own and the shared ones, by name, a page at a time (FR-RPT-04). Needs a view of the whole ledger's reports: " +
			"a role limited to sales, purchases, payroll, a department or the board pack reads none, because a definition can name any account. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *listReportDefinitionsInput) (*reportDefinitionsOutput, error) {
		p, _ := authn.From(ctx)
		var all []storage.ReportDefinition
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			all, err = u.ReportDefinitions().List(ctx, in.Archived)
			return err
		})
		if err != nil {
			return nil, err
		}
		all = slices.DeleteFunc(all, func(d storage.ReportDefinition) bool { return !mayReadReport(d, p) })
		items, next, err := pageOf(all, func(d storage.ReportDefinition) string { return d.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := ReportDefinitionPage{Items: []ReportDefinition{}, NextCursor: next}
		for _, d := range items {
			out.Items = append(out.Items, reportDefinitionOf(d, p.UserID))
		}
		return &reportDefinitionsOutput{Body: out}, nil
	})

	register(a, authz.CreateReportDefinition, huma.Operation{
		OperationID: "createReportDefinition", Method: http.MethodPost, Path: "/v1/report-definitions", Tags: []string{"Reports"}, DefaultStatus: http.StatusCreated,
		Summary: "Save a report",
		Description: "Saves a report definition (FR-RPT-04): rows of accounts, report groups and calculated rows, columns of periods and comparatives, and filters, " +
			"checked against fyniq-report@1 (RDEF-01 to RDEF-08). It is private to its caller or shared with everyone who may read the whole ledger's reports; running it " +
			"always checks the runner's own rights. A name is used once among the live reports. Side effects: the report at version 1 and its audit event. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(slices.Clone(writes), append([]string{storage.CodeReportName, storage.CodeReportDefinition}, reportDefCodes...)...),
		func(ctx context.Context, in *createReportDefinitionInput) (*reportDefinitionOutput, error) {
			p, _ := authn.From(ctx)
			if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
				return nil, problem(CodeReportNotYours, "Only a person who is a member saves a report.", "Sign in as yourself")
			}
			d, err := checkReportDefinition(in.Body.Definition)
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(d)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ReportDefinition, error) {
				now := a.deps.Clock.Now()
				saved, err := u.ReportDefinitions().Create(ctx, storage.ReportDefinition{Name: strings.TrimSpace(in.Body.Name), Description: in.Body.Description, Definition: raw,
					Visibility: in.Body.Visibility, OwnerID: p.UserID, CreatedBy: uploader(ctx), CreatedAt: now, UpdatedBy: uploader(ctx), UpdatedAt: now})
				if err != nil {
					return ReportDefinition{}, err
				}
				return reportDefinitionOf(saved, p.UserID), nil
			})
			if err != nil {
				return nil, err
			}
			return &reportDefinitionOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), ETag: reportETag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.GetReportDefinition, huma.Operation{
		OperationID: "getReportDefinition", Method: http.MethodGet, Path: "/v1/report-definitions/{id}", Tags: []string{"Reports"},
		Summary:     "Get a saved report",
		Description: "A report the caller may open: its definition, visibility, owner and version (the ETag). Another person's private report is not found. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeInvalid}, locks...), func(ctx context.Context, in *getReportDefinitionInput) (*reportDefinitionOutput, error) {
		p, _ := authn.From(ctx)
		var out ReportDefinition
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err := savedReport(ctx, u, p, in.ID)
			out = reportDefinitionOf(d, p.UserID)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &reportDefinitionOutput{Status: http.StatusOK, ETag: reportETag(out.Version), Body: out}, nil
	})

	register(a, authz.ReplaceReportDefinition, huma.Operation{
		OperationID: "replaceReportDefinition", Method: http.MethodPut, Path: "/v1/report-definitions/{id}", Tags: []string{"Reports"},
		Summary: "Change a saved report",
		Description: "Saves a change to a report as its next version (FR-RPT-04); only its owner, or an Owner or Administrator for a shared one, changes it (RDEF-09). " +
			"Needs If-Match with the report's ETag. Side effects: the next version and its audit event, with the definition before and after. " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append(slices.Clone(writes), append([]string{CodeStale, CodeIfMatchRequired, CodeReportNotYours, storage.CodeVersion, storage.CodeReportName, storage.CodeReportDefinition}, reportDefCodes...)...),
		func(ctx context.Context, in *replaceReportDefinitionInput) (*reportDefinitionOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			d, err := checkReportDefinition(in.Body.Definition)
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(d)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ReportDefinition, error) {
				cur, err := savedReport(ctx, u, p, in.ID)
				if err != nil {
					return ReportDefinition{}, err
				}
				if !mayChangeReport(cur, p) {
					return ReportDefinition{}, problem(CodeReportNotYours, "Only a report's owner, or an Owner or Administrator for a shared one, changes it.", "Save a copy under your own name")
				}
				if err := checkIfMatch(in.IfMatch, reportETag(cur.Version)); err != nil {
					return ReportDefinition{}, err
				}
				cur.Name, cur.Description, cur.Definition, cur.Visibility = strings.TrimSpace(in.Body.Name), in.Body.Description, raw, in.Body.Visibility
				cur.UpdatedBy, cur.UpdatedAt = uploader(ctx), a.deps.Clock.Now()
				saved, err := u.ReportDefinitions().Replace(ctx, cur, cur.Version)
				if err != nil {
					return ReportDefinition{}, err
				}
				return reportDefinitionOf(saved, p.UserID), nil
			})
			if err != nil {
				return nil, err
			}
			return &reportDefinitionOutput{Status: http.StatusOK, ETag: reportETag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ArchiveReportDefinition, huma.Operation{
		OperationID: "archiveReportDefinition", Method: http.MethodPost, Path: "/v1/report-definitions/{id}/archive", Tags: []string{"Reports"},
		Summary: "Archive a saved report",
		Description: "Ends a report (FR-RPT-04): it is never deleted, so a schedule or a board pack that named it keeps naming it, and its schedules stop. Only its owner, " +
			"or an Owner or Administrator for a shared one, archives it (RDEF-09). Needs If-Match. Side effects: the next version and its audit event.",
	}, append(slices.Clone(writes), CodeStale, CodeIfMatchRequired, CodeReportNotYours, storage.CodeVersion, storage.CodeReportDefinition),
		func(ctx context.Context, in *archiveReportDefinitionInput) (*reportDefinitionOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ReportDefinition, error) {
				cur, err := savedReport(ctx, u, p, in.ID)
				if err != nil {
					return ReportDefinition{}, err
				}
				if !mayChangeReport(cur, p) {
					return ReportDefinition{}, problem(CodeReportNotYours, "Only a report's owner, or an Owner or Administrator for a shared one, archives it.", "Ask its owner")
				}
				if err := checkIfMatch(in.IfMatch, reportETag(cur.Version)); err != nil {
					return ReportDefinition{}, err
				}
				saved, err := u.ReportDefinitions().Archive(ctx, cur.ID, uploader(ctx), a.deps.Clock.Now(), cur.Version)
				if err != nil {
					return ReportDefinition{}, err
				}
				return reportDefinitionOf(saved, p.UserID), nil
			})
			if err != nil {
				return nil, err
			}
			return &reportDefinitionOutput{Status: http.StatusOK, ETag: reportETag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.RunReportDefinition, huma.Operation{
		OperationID: "runReportDefinition", Method: http.MethodGet, Path: "/v1/report-definitions/{id}/run", Tags: []string{"Reports"},
		Summary: "Run a saved report",
		Description: "Works a saved report out for a run of months as at a ledger revision (FR-RPT-04, FR-RPT-08): each account or group row from the balance service, " +
			"each calculated row an exact formula rounded once by its format (RND-KPI), a division by zero not available, never zero. The caller's own rights apply at run time: " +
			"a role limited to part of the ledger reads none. A month the books have no period for is REPORT-03, a later revision REPORT-04. Read-only; no side effects.",
	}, append(slices.Clone(runCodes), locks...), func(ctx context.Context, in *runReportDefinitionInput) (*reportRunOutput, error) {
		p, _ := authn.From(ctx)
		var out ReportRun
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err := savedReport(ctx, u, p, in.ID)
			if err != nil {
				return err
			}
			var def reportdef.Definition
			if err := json.Unmarshal(d.Definition, &def); err != nil {
				return err
			}
			if out, err = a.workReportDefinition(ctx, u, def, in.ReportRunQuery); err != nil {
				return err
			}
			out.DefinitionID, out.DefinitionVer = d.ID.String(), d.Version
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &reportRunOutput{Body: out}, nil
	})

	// A run saves nothing, so a repeat is harmless and return=preview would answer the same.
	register(a, authz.RunReportDraft, exempt(exempt(huma.Operation{
		OperationID: "runReportDraft", Method: http.MethodPost, Path: "/v1/report-builder/run", Tags: []string{"Reports"},
		Summary: "Run a report definition without saving it",
		Description: "Works a definition that is sent out as a saved report would be (FR-RPT-04), so the builder shows the figures as it is edited. Nothing is saved " +
			"and nothing is audited: it is a read. The definition is checked against fyniq-report@1 first (RDEF-01 to RDEF-08).",
	}, "Idempotency-Key", "A run saves nothing, so a repeat is harmless and answers the same (FR-RPT-04)."),
		"Prefer", "A run saves nothing: return=preview would answer the same as the request itself (FR-INT-14)."), append(slices.Clone(runCodes), append([]string{CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)...), func(ctx context.Context, in *runReportDraftInput) (*reportRunOutput, error) {
		p, _ := authn.From(ctx)
		d, err := checkReportDefinition(in.Body.Definition)
		if err != nil {
			return nil, err
		}
		var out ReportRun
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.workReportDefinition(ctx, u, d, ReportRunQuery{From: in.Body.From, To: in.Body.To, Revision: in.Body.Revision})
			return err
		})
		if err != nil {
			return nil, err
		}
		return &reportRunOutput{Body: out}, nil
	})
}

// ------------------------------------------------------------------ schedules (FR-RPT-07)

// ReportSchedule says when a saved report is sent and to whom.
type ReportSchedule struct {
	ID           string         `json:"id" format:"uuid" doc:"The schedule's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b10"`
	DefinitionID string         `json:"definition_id" format:"uuid" doc:"The report it sends." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
	Cadence      string         `json:"cadence" enum:"weekly,monthly" doc:"weekly: on a weekday; monthly: on a day of the month." example:"monthly"`
	Weekday      *int           `json:"weekday,omitempty" minimum:"1" maximum:"7" doc:"A weekly schedule's weekday, 1 Monday to 7 Sunday." example:"1"`
	DayOfMonth   *int           `json:"day_of_month,omitempty" minimum:"1" maximum:"28" doc:"A monthly schedule's day, 1 to 28 so that every month has it." example:"5"`
	PeriodRule   string         `json:"period_rule" enum:"last_closed_month,month_to_date" doc:"The month the report is for when it is sent: the latest closed month, or the month so far." example:"last_closed_month"`
	Channel      string         `json:"channel" enum:"email,slack" doc:"email: a notification that links to the signed-in report, with no figure in it and no file; slack: a post to a channel through the Slack connector (not live yet: [live-verify])." example:"email"`
	SlackChannel string         `json:"slack_channel,omitempty" maxLength:"80" doc:"The Slack channel, for the slack channel." example:"#finance"`
	Recipients   []MemberUserID `json:"recipients" nullable:"false" doc:"Member user IDs; each is checked again at send time, and one who may no longer read the report is skipped."`
	Active       bool           `json:"active" doc:"False when the schedule is switched off." example:"true"`
	NextRunOn    string         `json:"next_run_on" format:"date" doc:"The next day it runs." example:"2026-12-05"`
	LastRunOn    string         `json:"last_run_on,omitempty" format:"date" doc:"The last day it ran." example:"2026-11-05"`
	Version      int            `json:"version" doc:"Its version, which is its ETag." example:"1"`
	UpdatedAt    time.Time      `json:"updated_at" doc:"When it last changed." example:"2026-11-23T03:00:00Z"`
}

// ReportScheduleIn is a schedule to save.
type ReportScheduleIn struct {
	Cadence      string         `json:"cadence" enum:"weekly,monthly" doc:"weekly: on a weekday; monthly: on a day of the month." example:"monthly"`
	Weekday      *int           `json:"weekday,omitempty" minimum:"1" maximum:"7" doc:"A weekly schedule's weekday, 1 Monday to 7 Sunday." example:"1"`
	DayOfMonth   *int           `json:"day_of_month,omitempty" minimum:"1" maximum:"28" doc:"A monthly schedule's day, 1 to 28." example:"5"`
	PeriodRule   string         `json:"period_rule" enum:"last_closed_month,month_to_date" doc:"The month the report is for when it is sent." example:"last_closed_month"`
	Channel      string         `json:"channel" enum:"email,slack" doc:"email or slack." example:"email"`
	SlackChannel string         `json:"slack_channel,omitempty" maxLength:"80" doc:"The Slack channel, for the slack channel." example:"#finance"`
	Recipients   []MemberUserID `json:"recipients" minItems:"1" maxItems:"50" nullable:"false" doc:"Member user IDs, each an active member who may read the report (RDEF-11)."`
	Active       bool           `json:"active" doc:"False switches the schedule off." example:"true"`
}

// ReportScheduleRun is one run of a schedule: what it sent and whom it skipped.
type ReportScheduleRun struct {
	RanOn          string        `json:"ran_on" format:"date" doc:"The day it ran." example:"2026-11-05"`
	Period         string        `json:"period" doc:"The month it was for." example:"2026-10"`
	LedgerRevision int64         `json:"ledger_revision" doc:"The ledger revision when it ran." example:"42"`
	Sent           int           `json:"sent" doc:"How many recipients were told." example:"3"`
	Skipped        []SkippedSend `json:"skipped" nullable:"false" doc:"Whom it skipped, and why."`
	RanAt          time.Time     `json:"ran_at" doc:"When it ran." example:"2026-11-05T01:00:00Z"`
}

// SkippedSend is a recipient a run did not tell.
type SkippedSend struct {
	UserID string `json:"user_id" doc:"The recipient." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Why    string `json:"why" doc:"Why." example:"no longer reads the whole ledger's reports"`
}

// ReportSchedulePath names a schedule in the path.
type ReportSchedulePath struct {
	ID string `path:"id" format:"uuid" doc:"The schedule's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b10"`
}

type reportScheduleOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	ETag     string `header:"ETag" doc:"The schedule's version, for If-Match."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ReportSchedule
}

type listReportSchedulesInput struct {
	PageQuery
	DefinitionID string `query:"definition_id" format:"uuid" doc:"The saved report whose schedules to list." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
}

type reportSchedulesOutput struct {
	Body struct {
		Items      []ReportSchedule `json:"items" nullable:"false" doc:"The report's schedules, oldest first."`
		NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
	}
}

type listReportScheduleRunsInput struct {
	ReportSchedulePath
	PageQuery
}

type reportScheduleRunsOutput struct {
	Body struct {
		Items      []ReportScheduleRun `json:"items" nullable:"false" doc:"The schedule's last 50 runs, newest first."`
		NextCursor string              `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
	}
}

type createReportScheduleInput struct {
	KeyHeader
	PreferHeader
	Body struct {
		DefinitionID string `json:"definition_id" format:"uuid" doc:"The saved report to send." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
		ReportScheduleIn
	}
}

type replaceReportScheduleInput struct {
	ReportSchedulePath
	IfMatchHeader
	PreferHeader
	Body ReportScheduleIn
}

func reportScheduleOf(s storage.ReportSchedule) ReportSchedule {
	out := ReportSchedule{ID: s.ID.String(), DefinitionID: s.DefinitionID.String(), Cadence: s.Cadence, Weekday: s.Weekday, DayOfMonth: s.DayOfMonth, PeriodRule: s.PeriodRule,
		Channel: s.Channel, SlackChannel: s.SlackChannel, Recipients: []MemberUserID{}, Active: s.Active, NextRunOn: dateOf(s.NextRunOn), Version: s.Version, UpdatedAt: s.UpdatedAt.UTC()}
	for _, r := range s.Recipients {
		out.Recipients = append(out.Recipients, MemberUserID(r.String()))
	}
	if s.LastRunOn != nil {
		out.LastRunOn = dateOf(*s.LastRunOn)
	}
	return out
}

// mayReadReports says whether a person holding these roles may run saved reports: an unqualified view of the reports.
func mayReadReports(roles []authz.Role) bool {
	return authz.Decide(authz.RunReportDefinition, roles, false).Allowed
}

// checkReportSchedule checks a schedule's form and its recipients, each an active member who may read the report (D3).
func checkReportSchedule(ctx context.Context, a *API, u storage.UnitOfWork, d storage.ReportDefinition, in ReportScheduleIn) ([]uuid.UUID, error) {
	var bad []FieldProblem
	switch {
	case in.Cadence == storage.CadenceWeekly && (in.Weekday == nil || in.DayOfMonth != nil):
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.weekday", Message: "a weekly schedule names its weekday and no day of the month", Fix: "Send weekday, 1 to 7"})
	case in.Cadence == storage.CadenceMonthly && (in.DayOfMonth == nil || in.Weekday != nil):
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.day_of_month", Message: "a monthly schedule names its day of the month and no weekday", Fix: "Send day_of_month, 1 to 28"})
	}
	if in.Channel == storage.ChannelSlack && strings.TrimSpace(in.SlackChannel) == "" || in.Channel == storage.ChannelEmail && in.SlackChannel != "" {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.slack_channel", Message: "belongs to the slack channel and to no other", Fix: "Name the Slack channel for slack, and leave it out for email"})
	}
	var ids []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for i, r := range in.Recipients {
		field := "body.recipients[" + strconv.Itoa(i) + "]"
		id, err := uuid.Parse(string(r))
		if err != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field, Message: "is not a member's user ID", Fix: "Send the user IDs listMembers gives"})
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if why := recipientWhy(ctx, a, u, d, id); why != "" {
			bad = append(bad, FieldProblem{Code: CodeReportRecipient, Field: field, Message: why, Fix: "Choose an active member who may read the whole ledger's reports, and share the report if it is private"})
		}
	}
	if len(bad) > 0 {
		return nil, invalid(bad)
	}
	return ids, nil
}

// recipientWhy is why a person may not be sent a report, or "" when they may: an active member holding an unqualified view of
// the reports, and, for a private report, its owner.
func recipientWhy(ctx context.Context, a *API, u storage.UnitOfWork, d storage.ReportDefinition, user uuid.UUID) string {
	rs, err := u.Members().Roles(ctx, user, a.deps.Clock.Now())
	if err != nil || len(rs) == 0 {
		return "is not an active member of the organisation"
	}
	roles := make([]authz.Role, 0, len(rs))
	for _, r := range rs {
		roles = append(roles, authz.Role(r.Role))
	}
	switch {
	case !mayReadReports(roles):
		return "does not read the whole ledger's reports"
	case d.Visibility != storage.ReportShared && d.OwnerID != user:
		return "cannot open a private report: share it first"
	}
	return ""
}

func mayScheduleReports(p authn.Principal) bool {
	return p.Class == authn.ClassUser && hasAnyRole(p.Roles, authz.Owner, authz.Administrator, authz.Controller, authz.Accountant)
}

func nextReportRun(s ReportScheduleIn, today time.Time) time.Time {
	w, d := 0, 0
	if s.Weekday != nil {
		w = *s.Weekday
	}
	if s.DayOfMonth != nil {
		d = *s.DayOfMonth
	}
	return reportdef.NextRun(s.Cadence, w, d, today.AddDate(0, 0, -1))
}

func registerReportSchedules(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)

	register(a, authz.ListReportSchedules, huma.Operation{
		OperationID: "listReportSchedules", Method: http.MethodGet, Path: "/v1/report-schedules", Tags: []string{"Reports"},
		Summary:     "List a saved report's schedules",
		Description: "The schedules of a report the caller may open, oldest first, a page at a time (FR-RPT-07). Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeNotFound, CodeInvalid}, locks...), func(ctx context.Context, in *listReportSchedulesInput) (*reportSchedulesOutput, error) {
		p, _ := authn.From(ctx)
		out := &reportSchedulesOutput{}
		out.Body.Items = []ReportSchedule{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err := savedReport(ctx, u, p, in.DefinitionID)
			if err != nil {
				return err
			}
			ss, err := u.ReportSchedules().ForDefinition(ctx, d.ID)
			if err != nil {
				return err
			}
			page, next, err := pageOf(ss, func(s storage.ReportSchedule) string { return s.ID.String() }, in.PageQuery)
			for _, s := range page {
				out.Body.Items = append(out.Body.Items, reportScheduleOf(s))
			}
			out.Body.NextCursor = next
			return err
		})
		return out, err
	})

	register(a, authz.CreateReportSchedule, huma.Operation{
		OperationID: "createReportSchedule", Method: http.MethodPost, Path: "/v1/report-schedules", Tags: []string{"Reports"}, DefaultStatus: http.StatusCreated,
		Summary: "Schedule a saved report",
		Description: "Sends a report on a schedule (FR-RPT-07): weekly on a weekday or monthly on a day (1 to 28), for the latest closed month or the month so far. Each recipient " +
			"must be an active member who may read the whole ledger's reports, and the owner alone for a private report (RDEF-11). At send time each recipient's right is checked " +
			"again and one who lost it is skipped. The email is a notification that links to the signed-in report: it carries no figure and attaches no file. Only an Owner, " +
			"Administrator, Controller or Accountant schedules a report (RDEF-10). Side effects: the schedule and its audit event. Idempotent with Idempotency-Key.",
	}, append(slices.Clone(writes), CodeReportScheduler, CodeReportRecipient, storage.CodeReportSchedule), func(ctx context.Context, in *createReportScheduleInput) (*reportScheduleOutput, error) {
		p, _ := authn.From(ctx)
		if !mayScheduleReports(p) {
			return nil, problem(CodeReportScheduler, "Only an Owner, Administrator, Controller or Accountant schedules a report.", "Ask one of them")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ReportSchedule, error) {
			d, err := savedReport(ctx, u, p, in.Body.DefinitionID)
			if err != nil {
				return ReportSchedule{}, err
			}
			if d.ArchivedAt != nil {
				return ReportSchedule{}, problem(CodeNotFound, "That report is archived.", "Schedule a live report")
			}
			ids, err := checkReportSchedule(ctx, a, u, d, in.Body.ReportScheduleIn)
			if err != nil {
				return ReportSchedule{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return ReportSchedule{}, err
			}
			now := a.deps.Clock.Now()
			s := storage.ReportSchedule{DefinitionID: d.ID, Cadence: in.Body.Cadence, Weekday: in.Body.Weekday, DayOfMonth: in.Body.DayOfMonth, PeriodRule: in.Body.PeriodRule,
				Channel: in.Body.Channel, SlackChannel: in.Body.SlackChannel, Recipients: ids, Active: in.Body.Active, NextRunOn: nextReportRun(in.Body.ReportScheduleIn, today), CreatedBy: uploader(ctx), CreatedAt: now, UpdatedAt: now}
			saved, err := u.ReportSchedules().Create(ctx, s)
			return reportScheduleOf(saved), err
		})
		if err != nil {
			return nil, err
		}
		return &reportScheduleOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), ETag: reportETag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ReplaceReportSchedule, huma.Operation{
		OperationID: "replaceReportSchedule", Method: http.MethodPut, Path: "/v1/report-schedules/{id}", Tags: []string{"Reports"},
		Summary: "Change a report's schedule",
		Description: "Saves a change to a schedule as its next version (FR-RPT-07), with the same checks as making one; its report never changes. Switching it off is active=false. " +
			"Needs If-Match. Only an Owner, Administrator, Controller or Accountant (RDEF-10). Side effects: the next version and its audit event.",
	}, append(slices.Clone(writes), CodeStale, CodeIfMatchRequired, CodeReportScheduler, CodeReportRecipient, storage.CodeVersion, storage.CodeReportSchedule),
		func(ctx context.Context, in *replaceReportScheduleInput) (*reportScheduleOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if !mayScheduleReports(p) {
				return nil, problem(CodeReportScheduler, "Only an Owner, Administrator, Controller or Accountant schedules a report.", "Ask one of them")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ReportSchedule, error) {
				sid, err := parseID("id", in.ID)
				if err != nil {
					return ReportSchedule{}, err
				}
				cur, err := u.ReportSchedules().Get(ctx, sid)
				if errors.Is(err, ports.ErrNotFound) {
					return ReportSchedule{}, problem(CodeNotFound, "No schedule with that ID is open to you.", "List the report's schedules")
				}
				if err != nil {
					return ReportSchedule{}, err
				}
				d, err := u.ReportDefinitions().Get(ctx, cur.DefinitionID)
				if err != nil || !mayReadReport(d, p) {
					return ReportSchedule{}, problem(CodeNotFound, "No schedule with that ID is open to you.", "List the report's schedules")
				}
				if err := checkIfMatch(in.IfMatch, reportETag(cur.Version)); err != nil {
					return ReportSchedule{}, err
				}
				ids, err := checkReportSchedule(ctx, a, u, d, in.Body)
				if err != nil {
					return ReportSchedule{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return ReportSchedule{}, err
				}
				next := cur.NextRunOn
				if in.Body.Cadence != cur.Cadence || !slices.Equal(deref(in.Body.Weekday), deref(cur.Weekday)) || !slices.Equal(deref(in.Body.DayOfMonth), deref(cur.DayOfMonth)) {
					next = nextReportRun(in.Body, today)
				}
				cur.Cadence, cur.Weekday, cur.DayOfMonth, cur.PeriodRule, cur.Channel, cur.SlackChannel = in.Body.Cadence, in.Body.Weekday, in.Body.DayOfMonth, in.Body.PeriodRule, in.Body.Channel, in.Body.SlackChannel
				cur.Recipients, cur.Active, cur.NextRunOn, cur.UpdatedAt = ids, in.Body.Active, next, a.deps.Clock.Now()
				saved, err := u.ReportSchedules().Replace(ctx, cur, cur.Version)
				return reportScheduleOf(saved), err
			})
			if err != nil {
				return nil, err
			}
			return &reportScheduleOutput{Status: http.StatusOK, ETag: reportETag(w.Body.Version), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ListReportScheduleRuns, huma.Operation{
		OperationID: "listReportScheduleRuns", Method: http.MethodGet, Path: "/v1/report-schedules/{id}/runs", Tags: []string{"Reports"},
		Summary:     "List a schedule's runs",
		Description: "The last 50 runs of a schedule, newest first, a page at a time, each with whom it told and whom it skipped and why (FR-RPT-07). Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeNotFound, CodeInvalid}, locks...), func(ctx context.Context, in *listReportScheduleRunsInput) (*reportScheduleRunsOutput, error) {
		p, _ := authn.From(ctx)
		out := &reportScheduleRunsOutput{}
		out.Body.Items = []ReportScheduleRun{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			sid, err := parseID("id", in.ID)
			if err != nil {
				return err
			}
			s, err := u.ReportSchedules().Get(ctx, sid)
			if errors.Is(err, ports.ErrNotFound) {
				return problem(CodeNotFound, "No schedule with that ID is open to you.", "List the report's schedules")
			}
			if err != nil {
				return err
			}
			if d, err := u.ReportDefinitions().Get(ctx, s.DefinitionID); err != nil || !mayReadReport(d, p) {
				return problem(CodeNotFound, "No schedule with that ID is open to you.", "List the report's schedules")
			}
			runs, err := u.ReportSchedules().Runs(ctx, s.ID, 50)
			if err != nil {
				return err
			}
			page, next, err := pageOf(runs, func(r storage.ReportScheduleRun) string { return r.ID.String() }, in.PageQuery)
			if err != nil {
				return err
			}
			out.Body.NextCursor = next
			for _, r := range page {
				x := ReportScheduleRun{RanOn: dateOf(r.RanOn), Period: r.PeriodLabel, LedgerRevision: r.LedgerRevision, Sent: r.Sent, Skipped: []SkippedSend{}, RanAt: r.RanAt.UTC()}
				_ = json.Unmarshal(r.Skipped, &x.Skipped)
				out.Body.Items = append(out.Body.Items, x)
			}
			return err
		})
		return out, err
	})
}

func deref(p *int) []int {
	if p == nil {
		return nil
	}
	return []int{*p}
}
