package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ statutory values in force (ENG-30, ENG-31, AI-53)

// StatutoryValues are a statutory pack's values in force on a day: the version the organisation reads then (its active
// one, else the one this release carries), each of its tables' rows in force that day, and the official sources they
// cite. The AI's packs.get reads statutory values only from here (p1-ai-assistant design D5).
type StatutoryValues struct {
	Pack    string           `json:"pack" example:"sg-gst"`
	Version string           `json:"version" doc:"The version in force: the organisation's active one, else the one this release carries (ENG-31)." example:"sg-gst@2026.3"`
	On      string           `json:"on" format:"date" doc:"The day." example:"2026-11-15"`
	Tables  []StatutoryTable `json:"tables" nullable:"false" doc:"Its tables, each with its rows in force on the day."`
	Sources []PackSource     `json:"sources" nullable:"false" doc:"The official sources the rows cite."`
}

// StatutoryTable is one of a pack's tables, with its rows in force on the day.
type StatutoryTable struct {
	Name        string            `json:"name" example:"limits"`
	Description string            `json:"description,omitempty" example:"GST limits in force by date"`
	Columns     []StatutoryColumn `json:"columns" nullable:"false" doc:"Its columns, in order."`
	Rows        []StatutoryRow    `json:"rows" nullable:"false"`
}

// StatutoryColumn is a column of a pack's table.
type StatutoryColumn string

// Schema implements huma.SchemaProvider.
func (StatutoryColumn) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A column of the table, as the pack names it.", Examples: []any{"amount"}}
}

// StatutoryRow is a row of a pack's table, by column, as the pack writes it.
type StatutoryRow struct {
	Values map[string]string `json:"values" doc:"Each column's value, as the pack writes it: decimals as text, dates as YYYY-MM-DD, empty for none." example:"{\"limit\":\"registration_threshold\",\"amount\":\"1000000.00\",\"currency\":\"SGD\"}"`
}

type statutoryValuesInput struct {
	Pack string `query:"pack" required:"true" pattern:"^[a-z]{2}-[a-z][a-z-]{1,30}$" doc:"The pack." example:"sg-gst"`
	On   string `query:"on" format:"date" doc:"The day; left out, the organisation's today." example:"2026-11-15"`
	Name string `query:"name" maxLength:"60" doc:"Only this table, or the rows whose first column is this, such as a limit's or a GST code's name." example:"registration_threshold"`
}

type statutoryValuesOutput struct{ Body StatutoryValues }

func registerStatutoryValues(a *API) {
	register(a, authz.GetStatutoryValues, huma.Operation{
		OperationID: "getStatutoryValues", Method: http.MethodGet, Path: "/v1/statutory-values", Tags: []string{"Statutory packs"},
		Summary: "Get the statutory values in force on a day",
		Description: "A pack's values in force on a day (ENG-31): from the organisation's active version in force then, else the version " +
			"this release carries; each table's rows whose effective dates cover the day (a table without them, such as the GST codes, " +
			"whole), with the official sources they cite (ENG-30). name keeps one table, or the rows whose first column is it. A pack " +
			"with no version in force on the day is not found. Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *statutoryValuesInput) (*statutoryValuesOutput, error) {
		p, _ := authn.From(ctx)
		var out StatutoryValues
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			on := a.deps.Clock.Now().UTC().Truncate(24 * time.Hour)
			if in.On != "" {
				var err error
				if on, err = pack.ParseDate(in.On); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "on", Message: "is not a date", Fix: "Send it as YYYY-MM-DD"}})
				}
			}
			v, ok, err := gst.PackOn(ctx, u, in.Pack, on)
			if err != nil {
				return err
			}
			if !ok {
				return ports.ErrNotFound
			}
			out = valuesOn(v, on, in.Name)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &statutoryValuesOutput{Body: out}, nil
	})
}

// valuesOn are v's rows in force on a day, and the sources they cite.
func valuesOn(v pack.Version, on time.Time, name string) StatutoryValues {
	out := StatutoryValues{Pack: v.Pack, Version: v.ID, On: on.Format(time.DateOnly), Tables: []StatutoryTable{}, Sources: []PackSource{}}
	cited := map[string]bool{}
	names := make([]string, 0, len(v.Tables))
	for n := range v.Tables {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		t := v.Tables[n]
		table := StatutoryTable{Name: n, Description: t.Description, Columns: make([]StatutoryColumn, len(t.Columns)), Rows: []StatutoryRow{}}
		for i, c := range t.Columns {
			table.Columns[i] = StatutoryColumn(c)
		}
		from, to := t.Col("effective_from"), t.Col("effective_to")
		for _, row := range t.Rows {
			if from >= 0 && from < len(row) && row[from] != "" {
				if d, err := pack.ParseDate(row[from]); err == nil && d.After(on) {
					continue
				}
			}
			if to >= 0 && to < len(row) && row[to] != "" {
				if d, err := pack.ParseDate(row[to]); err == nil && d.Before(on) {
					continue
				}
			}
			if name != "" && name != n && (len(row) == 0 || row[0] != name) {
				continue
			}
			values := map[string]string{}
			for i, c := range t.Columns {
				if i < len(row) {
					values[c] = row[i]
				}
			}
			if s := t.Col("source"); s >= 0 && s < len(row) {
				for _, id := range strings.Fields(row[s]) {
					cited[id] = true
				}
			}
			table.Rows = append(table.Rows, StatutoryRow{Values: values})
		}
		if len(table.Rows) > 0 || name == n {
			out.Tables = append(out.Tables, table)
		}
	}
	for _, s := range v.Sources {
		if cited[s.ID] {
			out.Sources = append(out.Sources, PackSource{ID: s.ID, Title: s.Title, URL: s.URL, Section: s.Section, Kind: s.Kind, Retrieved: s.Retrieved})
		}
	}
	return out
}
