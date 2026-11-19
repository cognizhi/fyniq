package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// The project report as a file (PRD-001 FR-PRJ-12, FR-RPT-09; p1-projects task 4.2): the rows getProjectReport gives the caller, and
// nothing it would not: costs, margin and salary tagged are columns only for a viewer who may see them, so an export never carries a
// figure the screen would not (G4-050).

type projectReportExportInput struct {
	From   string `query:"from" format:"date" required:"true" doc:"A day in the first month." example:"2026-11-01"`
	To     string `query:"to" format:"date" required:"true" doc:"A day in the last month, at most 12 months after the first." example:"2026-11-30"`
	Format string `query:"format" enum:"csv,xlsx" default:"csv" doc:"csv: the projects' rows, plain decimals with an ASCII minus; xlsx: a workbook with the projects and each person's utilisation, amounts as number cells." example:"csv"`
}

// projectReportSheets are the report's rows as sheets: the projects (with the lines tagged with no project when the caller sees every
// project) and each person's utilisation. Minutes are whole numbers, amounts plain decimals in the base currency's minor units, and a
// figure the caller may not see is not a column at all.
func projectReportSheets(r ProjectReport, minor int) (projects, people sheet.Sheet, rows int) {
	header := []string{"Code", "Project", "Customer", "Revenue " + r.Currency}
	if r.CostsVisible {
		header = append(header, "Direct costs "+r.Currency, "Salary tagged "+r.Currency, "Time at cost "+r.Currency, "Margin "+r.Currency)
	}
	header = append(header, "Approved minutes", "Billable minutes")
	if r.CostsVisible {
		header = append(header, "Standard value "+r.Currency, "Realisation %", "Rate basis")
	}
	header = append(header, "Budget minutes", "Budget used %")
	projects = sheet.Sheet{Name: "Projects", Header: header}
	money := func(v string) sheet.Cell { return sheet.Number(v, minor) }
	row := func(x ProjectReportRow) sheet.Row {
		cells := []sheet.Cell{sheet.Text(x.Code), sheet.Text(x.Name), sheet.Text(x.CustomerName), money(x.Revenue)}
		if r.CostsVisible {
			cells = append(cells, money(x.DirectCosts), money(x.PayrollTagged), money(x.TimeCost), money(x.Margin))
		}
		cells = append(cells, sheet.Number(strconv.Itoa(x.Minutes), 0), sheet.Number(strconv.Itoa(x.Billable), 0))
		if r.CostsVisible {
			cells = append(cells, money(x.Standard), sheet.Number(x.Realisation, 1), sheet.Text(x.Basis))
		}
		budget := ""
		if x.BudgetMinutes != nil {
			budget = strconv.Itoa(*x.BudgetMinutes)
		}
		cells = append(cells, sheet.Number(budget, 0), sheet.Number(x.BudgetUsed, 1))
		return sheet.Row{Cells: cells}
	}
	for _, x := range r.Projects {
		projects.Rows = append(projects.Rows, row(x))
		rows++
	}
	if r.Untagged != nil {
		u := *r.Untagged
		u.Name = "Tagged with no project"
		projects.Rows = append(projects.Rows, row(u))
		rows++
	}
	pheader := []string{"Person", "Approved minutes", "Billable minutes", "Available minutes", "Utilisation %"}
	withCost := false
	for _, p := range r.People {
		withCost = withCost || p.Cost != ""
	}
	if withCost {
		pheader = append(pheader, "Cost "+r.Currency)
	}
	people = sheet.Sheet{Name: "Utilisation", Header: pheader}
	for _, p := range r.People {
		cells := []sheet.Cell{sheet.Text(p.PersonName), sheet.Number(strconv.Itoa(p.Minutes), 0), sheet.Number(strconv.Itoa(p.Billable), 0),
			sheet.Number(strconv.Itoa(p.Available), 0), sheet.Number(p.Utilisation, 1)}
		if withCost {
			cells = append(cells, money(p.Cost))
		}
		people.Rows = append(people.Rows, sheet.Row{Cells: cells})
		rows++
	}
	return projects, people, rows
}

func registerProjectReportExport(a *API) {
	register(a, authz.ExportProjectReport, huma.Operation{
		OperationID: "exportProjectReport", Method: http.MethodGet, Path: "/v1/reports/projects/export", Tags: []string{"Projects"},
		Summary: "Export the project report",
		Description: "The report getProjectReport gives for the same months, as a file (FR-PRJ-12, FR-RPT-09): CSV, the projects' rows with amounts as plain " +
			"decimals and an ASCII minus; or XLSX, a workbook with the projects and each person's utilisation, amounts as number cells. No text in either becomes " +
			"a formula. It holds only what the caller's own report holds: costs, margin and salary tagged are columns only for a caller who may see them, and a " +
			"person's cost only for the Owner and the Payroll administrator. Returned in base64 with its SHA-256 and the ledger revision. Refused with PRJ-09 while " +
			"Projects and time are switched off. Side effects: an audit event of the export with what was exported, its months, rows and hash (FR-RPT-09). Never cached.",
	}, []string{CodeInvalid, CodeProjectsOff}, func(ctx context.Context, in *projectReportExportInput) (*reportFileOutput, error) {
		from, ferr := rateDay("query.from", in.From)
		to, terr := rateDay("query.to", in.To)
		if ferr != nil || terr != nil || to.Before(from) || to.After(from.AddDate(1, 0, 0)) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.to", Message: "is not a day on or after from, within 12 months", Fix: "Ask for at most a year"}})
		}
		p, _ := authn.From(ctx)
		var body ProjectReport
		var revision int64
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if err = requireProjects(ctx, a, u); err != nil {
				return err
			}
			if body, err = a.projectReportOf(ctx, u, p, from, to); err != nil {
				return err
			}
			revision, err = u.Ledger().Revision(ctx)
			return err
		})
		if err != nil {
			return nil, err
		}
		base, err := money.Lookup(body.Currency)
		if err != nil {
			return nil, err
		}
		now := a.deps.Clock.Now()
		projects, people, rows := projectReportSheets(body, base.MinorUnits)
		name := "projects-" + body.From[:7]
		if body.To[:7] != body.From[:7] {
			name += "-to-" + body.To[:7]
		}
		var data []byte
		media := mediaCSV
		if in.Format == "xlsx" {
			if data, err = sheet.XLSX(a.footed(now, projects, people), now); err != nil {
				return nil, err
			}
			media, name = mediaXLSX, name+".xlsx"
		} else {
			data, name = sheet.CSV(a.footed(now, projects)[0]), name+".csv"
		}
		sum := sha256.Sum256(data)
		id := uuid.Must(uuid.NewV7())
		format := in.Format
		if format == "" {
			format = "csv"
		}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "report.exported", ObjectType: "report", ObjectID: id,
				Changes: map[string][2]any{"export": {nil, map[string]any{"report": "projects", "format": format, "from": body.From, "to": body.To,
					"costs_visible": body.CostsVisible, "ledger_revision": revision, "rows": rows, "file": name, "sha256": hex.EncodeToString(sum[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		return &reportFileOutput{Body: ReportFile{ExportID: id.String(), FileName: name, MediaType: media, SHA256: hex.EncodeToString(sum[:]), Size: len(data), Rows: rows,
			Revision: revision, Content: base64.StdEncoding.EncodeToString(data)}}, nil
	})
}
