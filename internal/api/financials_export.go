package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// ------------------------------------------------- a financial statement's export (FR-RPT-05, FR-RPT-08, FR-RPT-09)

// ReportFile is an exported report: the file, as written, with what it was read at.
type ReportFile struct {
	ExportID  string `json:"export_id" format:"uuid" doc:"The export, as the audit trail records it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	FileName  string `json:"file_name" doc:"The file's name." example:"profit-and-loss-2026-10.xlsx"`
	MediaType string `json:"media_type" doc:"Its type." example:"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"`
	SHA256    string `json:"sha256" doc:"The SHA-256 of the content, in hexadecimal, as the audit trail records it." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Size      int    `json:"size_bytes" doc:"Its size, in bytes." example:"4096"`
	Rows      int    `json:"rows" doc:"The rows of figures it holds." example:"42"`
	Revision  int64  `json:"ledger_revision" doc:"The ledger revision its figures are read at (FR-RPT-08)." example:"42"`
	Content   string `json:"content_base64" doc:"The file, in base64." example:"UGF5ZWUsUGF5ZWUgdHlwZQ=="`
}

type financialExportInput struct {
	Kind     string `path:"kind" enum:"pl,bs,cf,tb" doc:"pl: the profit and loss; bs: the balance sheet; cf: the cash flow; tb: the trial balance." example:"pl"`
	From     string `query:"from" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's first month, YYYY-MM; left out, the last month's. A balance sheet is at the last month's end, with the movement in equity over the run; a trial balance is at the last month's end." example:"2026-10"`
	To       string `query:"to" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's last month, YYYY-MM; left out, the month of the organisation's today." example:"2026-10"`
	Compare  string `query:"compare" enum:"prior_period,prior_year" doc:"A comparative column: the run of the same length just before, or the same months a year earlier. Left out when the books do not have all its months. Budget and forecast arrive with planning." example:"prior_period"`
	Columns  string `query:"columns" enum:"total,month" default:"total" doc:"total: one column for the run, and its comparative; month: a column per month and, for a P&L, the total, without a comparative." example:"total"`
	Revision int64  `query:"as_at_revision" minimum:"1" doc:"Read as at this ledger revision (FR-RPT-08), as an earlier statement recorded it; left out, the ledger as it stands." example:"42"`
	Format   string `query:"format" enum:"csv,xlsx,pdf" default:"xlsx" doc:"csv: the statement's rows, plain decimals with an ASCII minus; xlsx: a workbook with the statement and a data tab of every figure, amounts as number cells; pdf: the statement to print, archival (PDF/A-2a) and accessible (PDF/UA-1), with the revision on every page." example:"xlsx"`
}

// CodeReportRender is a PDF the render service could not make: none is configured, it is busy, or it failed.
const CodeReportRender = "REPORT-08"

type reportFileOutput struct{ Body ReportFile }

// The media types of the exports.
const (
	mediaCSV  = "text/csv; charset=utf-8"
	mediaXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	mediaPDF  = "application/pdf"
)

var statementSlugs = map[string]string{"pl": "profit-and-loss", "bs": "balance-sheet", "cf": "cash-flow", "tb": "trial-balance"}

// columnHeading is a column's heading: its month, its run of months, or the total.
func columnHeading(c FinancialColumn) string {
	if c.Key == "total" {
		return "Total"
	}
	from, _ := time.Parse(time.DateOnly, c.From)
	to, _ := time.Parse(time.DateOnly, c.To)
	if from.Format("2006-01") == to.Format("2006-01") {
		return from.Format("Jan 2006")
	}
	return from.Format("Jan 2006") + " to " + to.Format("Jan 2006")
}

// statementSheets are a statement as sheets (FR-RPT-05): the statement as it reads, a row per account, group, section
// total and measure; and a data tab with every account's figure in every column, one a row.
func statementSheets(s FinancialStatement, minor int) ([]sheet.Sheet, int) {
	header := []string{s.Currency}
	for _, c := range s.Columns {
		header = append(header, columnHeading(c))
	}
	pl := s.Kind == "pl"
	if pl {
		header = append(header, "% of revenue")
	}
	amounts := func(label string, ms []Money, ps []Percent) []sheet.Cell {
		cells := []sheet.Cell{sheet.Text(label)}
		for _, m := range ms {
			cells = append(cells, sheet.Number(m.Amount, minor))
		}
		if pl {
			pc := ""
			if len(ps) > 0 {
				pc = string(ps[0])
			}
			cells = append(cells, sheet.Number(pc, 1))
		}
		return cells
	}
	main := sheet.Sheet{Name: statementNames[s.Kind], Header: header}
	data := sheet.Sheet{Name: "Data", Header: []string{"Block", "Group", "Code", "Account", "Column", "From", "To", "Amount"}}
	figures := 0
	for _, b := range s.Blocks {
		if b.Measure {
			main.Rows = append(main.Rows, sheet.Row{Cells: amounts(b.Title, b.Totals, b.Percents), Strong: true})
			continue
		}
		main.Rows = append(main.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(b.Title)}, Strong: true})
		for _, g := range b.Groups {
			if len(g.Lines) == 0 {
				main.Rows = append(main.Rows, sheet.Row{Cells: amounts(g.Name, g.Totals, g.Percents)})
			}
			for _, l := range g.Lines {
				main.Rows = append(main.Rows, sheet.Row{Cells: amounts(l.Code+" "+l.Name, l.Amounts, l.Percents)})
				for i, m := range l.Amounts {
					c := s.Columns[i]
					data.Rows = append(data.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(b.Title), sheet.Text(g.Name), sheet.Text(l.Code), sheet.Text(l.Name),
						sheet.Text(columnHeading(c)), sheet.Text(c.From), sheet.Text(c.To), sheet.Number(m.Amount, minor)}})
					figures++
				}
			}
			if len(b.Groups) > 1 && len(g.Lines) > 1 {
				main.Rows = append(main.Rows, sheet.Row{Cells: amounts(g.Name, g.Totals, g.Percents)})
			}
		}
		main.Rows = append(main.Rows, sheet.Row{Cells: amounts("Total "+strings.ToLower(b.Title), b.Totals, b.Percents), Strong: true})
	}
	foot := s.StatementTitle + " from " + s.From + " to " + s.To + ", read at ledger revision " + strconv.FormatInt(s.Revision, 10)
	if s.Restated {
		foot += ", restated presentation"
	}
	main.Rows = append(main.Rows, sheet.Row{}, sheet.Row{Cells: []sheet.Cell{sheet.Text(foot)}})
	for _, c := range s.Checks {
		if !c.OK {
			main.Rows = append(main.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(c.Title + ": " + c.Detail)}})
		}
	}
	return []sheet.Sheet{main, data}, figures
}

// statementNames are the statements' names, as a sheet is titled.
var statementNames = map[string]string{"pl": "Profit and loss", "bs": "Balance sheet", "cf": "Cash flow", "tb": "Trial balance"}

// printed is an amount as a printed statement writes it: grouped, with U+2212 before a negative (RND-DISPLAY).
func printed(m Money) string {
	x, err := render.Amount{Amount: m.Amount, Currency: m.Currency}.Money()
	if err != nil {
		return m.Amount
	}
	return render.FormatMoney(x, "plain")
}

// longDay is a day as a printed statement writes it, such as 31 October 2026.
func longDay(s string) string {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return s
	}
	return render.FormatDate(d, "long")
}

// statementReport is a statement as a report the render service lays out (FR-RPT-05): the statement as it reads, a row
// per section, account, group total, section total and measure, as the sheets have them, with each column's % of
// revenue beside it in a P&L of one or two columns; the checks that failed, the restated presentation and the cash
// flow's transactions that moved no money as notes; and the revision in each page's foot (FR-RPT-08).
func statementReport(s FinancialStatement, author, version string) render.Report {
	pct := s.Kind == "pl" && len(s.Columns) <= 2
	basis := s.StatementTitle
	if s.Basis != "" {
		basis += ". " + s.Basis
	}
	period := longDay(s.From) + " to " + longDay(s.To)
	if s.Kind == "bs" || s.Kind == "tb" {
		period = "At " + longDay(s.To)
	}
	r := render.Report{Title: statementNames[s.Kind], Author: author, Lines: []string{basis, period, "Amounts in " + s.Currency}, Label: s.Currency,
		Rows: []render.ReportRow{}}
	for _, c := range s.Columns {
		r.Columns = append(r.Columns, columnHeading(c))
		if pct {
			r.Columns = append(r.Columns, "% of revenue")
		}
	}
	row := func(kind, code, label string, ms []Money, ps []Percent) {
		x := render.ReportRow{Kind: kind, Code: code, Label: label, Cells: []string{}}
		for i, m := range ms {
			x.Cells = append(x.Cells, printed(m))
			if pct {
				p := ""
				if i < len(ps) && ps[i] != "" {
					p = strings.Replace(string(ps[i]), "-", render.Minus, 1) + "%"
				}
				x.Cells = append(x.Cells, p)
			}
		}
		r.Rows = append(r.Rows, x)
	}
	for _, b := range s.Blocks {
		if b.Measure {
			row(render.RowMeasure, "", b.Title, b.Totals, b.Percents)
			continue
		}
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowSection, Label: b.Title})
		for _, g := range b.Groups {
			if len(g.Lines) == 0 {
				row(render.RowSubtotal, "", g.Name, g.Totals, g.Percents)
			}
			for _, l := range g.Lines {
				row(render.RowLine, l.Code, l.Name, l.Amounts, l.Percents)
			}
			if len(b.Groups) > 1 && len(g.Lines) > 1 {
				row(render.RowSubtotal, "", g.Name, g.Totals, g.Percents)
			}
		}
		row(render.RowTotal, "", "Total "+strings.ToLower(b.Title), b.Totals, b.Percents)
	}
	var failed []string
	for _, c := range s.Checks {
		if !c.OK {
			failed = append(failed, strings.TrimSuffix(c.Title+": "+c.Detail, ": "))
		}
	}
	if len(failed) > 0 {
		r.Notes = append(r.Notes, render.ReportNote{Title: "Checks that need attention", Items: failed})
	}
	if s.Restated {
		r.Notes = append(r.Notes, render.ReportNote{Title: "Restated presentation", Items: []string{"The chart of accounts changed after ledger revision " +
			strconv.FormatInt(s.Revision, 10) + ": the figures are those at the revision, the account names and groups today's."}})
	}
	if len(s.NonCash) > 0 {
		n := render.ReportNote{Title: "Investing and financing transactions that moved no money"}
		for _, x := range s.NonCash {
			n.Items = append(n.Items, strings.TrimSpace(longDay(x.Date)+" "+x.Memo)+": "+printed(x.Amount))
		}
		r.Notes = append(r.Notes, n)
	}
	r.Footer = s.StatementTitle + " · read at ledger revision " + strconv.FormatInt(s.Revision, 10) + " · Fyniq " + version
	if s.Restated {
		r.Footer += " · restated presentation"
	}
	return r
}

// renderReport renders r through the render service as made at created (FR-RPT-05): REPORT-08 when no render service is
// configured, it is busy, or it fails.
func (a *API) renderReport(ctx context.Context, r render.Report, created time.Time) ([]byte, error) {
	if a.deps.Renderer == nil {
		return nil, problem(CodeReportRender, "No render service is configured, so the PDF cannot be made.",
			"Export to Excel or CSV, or ask the operator to configure the render service")
	}
	res, err := a.deps.Renderer.Render(ctx, render.Request{Report: &r, Created: created})
	var rp *render.Problem
	switch {
	case err == nil:
		return res.PDF, nil
	case errors.As(err, &rp) && rp.Code == render.CodeBusy:
		return nil, problem(CodeReportRender, "The render service is busy.", "Try again in a moment").withHeader("Retry-After", "1")
	}
	if a.deps.Log != nil {
		a.deps.Log.ErrorContext(ctx, "render", "error", err.Error())
	}
	return nil, problem(CodeReportRender, "The render service did not make the PDF.", "Try again, or export to Excel or CSV; if it persists, report it")
}

func registerFinancialExports(a *API) {
	register(a, authz.ExportFinancialStatement, huma.Operation{
		OperationID: "exportFinancialStatement", Method: http.MethodGet, Path: "/v1/reports/statements/{kind}/export", Tags: []string{"Reports"},
		Summary: "Export a financial statement",
		Description: "The statement getFinancialStatement gives for the same parameters, as a file (FR-RPT-05): CSV, the statement's rows " +
			"with amounts as plain decimals and an ASCII minus; XLSX, a workbook with the statement, totals in bold, and a data tab of " +
			"every account's figure in every column, amounts as number cells; or PDF, the statement to print from the render service, " +
			"archival and accessible, with the organisation's name, the statements' title and the revision on every page. No text in " +
			"any becomes a formula. Returned in base64 with its SHA-256 and the ledger revision its figures are read at (FR-RPT-08). A " +
			"PDF the render service cannot make now is REPORT-08. Side effects: an audit event of the export with what was exported, " +
			"its filters, rows and hash (FR-RPT-09). Never cached.",
	}, []string{CodeInvalid, CodeNoPeriod, CodeLaterRevision, CodeReportRender}, func(ctx context.Context, in *financialExportInput) (*reportFileOutput, error) {
		p, _ := authn.From(ctx)
		var s FinancialStatement
		author := ""
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			s, err = a.financialStatement(ctx, u, &financialStatementInput{Kind: in.Kind, From: in.From, To: in.To, Compare: in.Compare,
				Columns: in.Columns, Revision: in.Revision})
			if err != nil {
				return err
			}
			o, err := u.Organisation(ctx)
			author = cmpOr(o.Profile.LegalName, o.Name)
			return err
		})
		if err != nil {
			return nil, err
		}
		base, err := money.Lookup(s.Currency)
		if err != nil {
			return nil, err
		}
		// The file is written outside the transaction, so the books are not held while the render service lays a PDF out.
		now := a.deps.Clock.Now()
		sheets, rows := statementSheets(s, base.MinorUnits)
		name := statementSlugs[s.Kind] + "-" + s.From[:7]
		if s.To[:7] != s.From[:7] {
			name += "-to-" + s.To[:7]
		}
		var data []byte
		media := mediaXLSX
		switch in.Format {
		case "csv":
			data, media, name = sheet.CSV(sheets[0]), mediaCSV, name+".csv"
		case "pdf":
			if data, err = a.renderReport(ctx, statementReport(s, author, a.deps.Version), now); err != nil {
				return nil, err
			}
			media, name = mediaPDF, name+".pdf"
		default:
			if data, err = sheet.XLSX(sheets, now); err != nil {
				return nil, err
			}
			name += ".xlsx"
		}
		sum := sha256.Sum256(data)
		id := uuid.Must(uuid.NewV7())
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "report.exported", ObjectType: "report", ObjectID: id,
				Changes: map[string][2]any{"export": {nil, map[string]any{"report": "statement", "kind": s.Kind, "format": in.Format, "from": s.From,
					"to": s.To, "compare": in.Compare, "columns": in.Columns, "ledger_revision": s.Revision, "rows": rows, "file": name,
					"sha256": hex.EncodeToString(sum[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		out := ReportFile{ExportID: id.String(), FileName: name, MediaType: media, SHA256: hex.EncodeToString(sum[:]), Size: len(data), Rows: rows,
			Revision: s.Revision, Content: base64.StdEncoding.EncodeToString(data)}
		return &reportFileOutput{Body: out}, nil
	})
}
