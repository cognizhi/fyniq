// Package reportdef is the report builder's definition and its calculation (PRD-001 FR-RPT-04; p2-reporting-planning design D1).
// A definition is rows (an account, a report group, a calculated row or a heading), columns (the run, the run before it, the same
// run a year earlier, the year to date) and filters; Compute turns the figures the balance service read into its cells. Every
// figure is an exact decimal: a calculated row is evaluated exactly and rounded once, by the policy of its format (RND-KPI), and a
// division by zero is "not available", never zero.
package reportdef

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Schema is the definition's schema identifier.
const Schema = "fyniq-report@1"

// The limits of a definition.
const (
	MaxRows    = 60
	MaxColumns = 12
)

// The codes of a definition's rules.
const (
	CodeSchema    = "RDEF-01" // not a fyniq-report@1 object
	CodeRow       = "RDEF-02" // a row's ID, label or kind
	CodeRowFields = "RDEF-03" // the fields a row's kind needs
	CodeFormula   = "RDEF-04" // a formula that does not parse
	CodeReference = "RDEF-05" // a formula's reference to a row that is not before it
	CodeLimit     = "RDEF-06" // too many rows or columns
	CodeColumn    = "RDEF-07" // a column's key
	CodeFilter    = "RDEF-08" // a filter that is not a dimension value's ID
)

// Row kinds.
const (
	KindAccount = "account"
	KindGroup   = "group"
	KindCalc    = "calc"
	KindHeading = "heading"
)

// Formats of a calculated row.
const (
	FormatAmount  = "amount"
	FormatPercent = "percent"
	FormatRatio   = "ratio"
)

// Column keys.
const (
	ColCurrent     = "current"      // the run asked for
	ColPriorPeriod = "prior_period" // the run of the same length just before
	ColPriorYear   = "prior_year"   // the same months a year earlier
	ColYTD         = "ytd"          // the financial year's first month to the run's last
)

// Definition is a saved report.
type Definition struct {
	Schema  string   `json:"schema"`
	Rows    []Row    `json:"rows"`
	Columns []Column `json:"columns"`
	Filters Filters  `json:"filters"`
}

// Row is one row: an account (its code), a report group (the chart's report_group), a calculation over rows before it, or a heading.
type Row struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Code    string `json:"code,omitempty"`
	Group   string `json:"group,omitempty"`
	Formula string `json:"formula,omitempty"`
	Format  string `json:"format,omitempty"`
}

// Column is one column: its key says which run it reads.
type Column struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
}

// Filters narrow the P&L rows to lines that carry every one of the dimension values (FR-DIM-02), and say whether a row of zeros shows.
type Filters struct {
	DimensionValueIDs []string `json:"dimension_value_ids,omitempty"`
	IncludeZero       bool     `json:"include_zero,omitempty"`
}

// Problem is one rule that failed.
type Problem struct{ Code, Field, Message, Fix string }

var (
	idForm   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)
	codeForm = regexp.MustCompile(`^[A-Za-z0-9._-]{1,20}$`)
)

// Validate checks a definition's form and every formula's references; it returns each problem it finds.
func Validate(d Definition) []Problem {
	var bad []Problem
	add := func(code, field, msg, fix string) { bad = append(bad, Problem{code, field, msg, fix}) }
	if d.Schema != Schema {
		add(CodeSchema, "schema", "is not "+Schema, "Send schema "+Schema)
	}
	if len(d.Rows) == 0 || len(d.Rows) > MaxRows {
		add(CodeLimit, "rows", fmt.Sprintf("has %d rows; a report has 1 to %d", len(d.Rows), MaxRows), "Keep the report to 60 rows or fewer")
	}
	if len(d.Columns) == 0 || len(d.Columns) > MaxColumns {
		add(CodeLimit, "columns", fmt.Sprintf("has %d columns; a report has 1 to %d", len(d.Columns), MaxColumns), "Keep the report to 12 columns or fewer")
	}
	seenCol := map[string]bool{}
	for i, c := range d.Columns {
		f := fmt.Sprintf("columns[%d].key", i)
		switch c.Key {
		case ColCurrent, ColPriorPeriod, ColPriorYear, ColYTD:
		default:
			add(CodeColumn, f, fmt.Sprintf("%q is not a column", c.Key), "Use current, prior_period, prior_year or ytd")
		}
		if seenCol[c.Key] {
			add(CodeColumn, f, fmt.Sprintf("%q appears twice", c.Key), "Name each column once")
		}
		seenCol[c.Key] = true
		if len([]rune(c.Label)) > 40 {
			add(CodeColumn, fmt.Sprintf("columns[%d].label", i), "is longer than 40 characters", "Shorten the label")
		}
	}
	for i, v := range d.Filters.DimensionValueIDs {
		if _, err := uuid.Parse(v); err != nil {
			add(CodeFilter, fmt.Sprintf("filters.dimension_value_ids[%d]", i), "is not a dimension value's ID", "Send the ID listDimensionValues gives")
		}
	}
	before := map[string]bool{}
	for i, r := range d.Rows {
		f := fmt.Sprintf("rows[%d]", i)
		if !idForm.MatchString(r.ID) {
			add(CodeRow, f+".id", fmt.Sprintf("%q is not a row ID", r.ID), "Use lower-case letters, digits and underscores, starting with a letter (up to 24)")
		} else if before[r.ID] {
			add(CodeRow, f+".id", fmt.Sprintf("%q is used twice", r.ID), "Give each row its own ID")
		}
		if l := len([]rune(strings.TrimSpace(r.Label))); l == 0 || l > 80 {
			add(CodeRow, f+".label", "must be 1 to 80 characters", "Give the row a short label")
		}
		switch r.Kind {
		case KindAccount:
			if !codeForm.MatchString(r.Code) || r.Group != "" || r.Formula != "" {
				add(CodeRowFields, f, "an account row names an account code and nothing else", "Send code, and leave group and formula out")
			}
		case KindGroup:
			if strings.TrimSpace(r.Group) == "" || len(r.Group) > 80 || r.Code != "" || r.Formula != "" {
				add(CodeRowFields, f, "a group row names a report group and nothing else", "Send group, and leave code and formula out")
			}
		case KindCalc:
			if r.Code != "" || r.Group != "" {
				add(CodeRowFields, f, "a calculated row has a formula and a format only", "Leave code and group out")
			}
			switch r.Format {
			case FormatAmount, FormatPercent, FormatRatio:
			default:
				add(CodeRowFields, f+".format", fmt.Sprintf("%q is not a format", r.Format), "Use amount, percent or ratio")
			}
			e, err := parse(r.Formula)
			if err != nil {
				add(CodeFormula, f+".formula", err.Error(), "Write the formula with row IDs, numbers, + - * / and brackets")
			} else {
				for _, ref := range e.refs() {
					if !before[ref] {
						add(CodeReference, f+".formula", fmt.Sprintf("%q is not a row before this one", ref), "A formula uses the IDs of rows above it")
					}
				}
			}
		case KindHeading:
			if r.Code != "" || r.Group != "" || r.Formula != "" || r.Format != "" {
				add(CodeRowFields, f, "a heading has a label only", "Leave the other fields out")
			}
		default:
			add(CodeRow, f+".kind", fmt.Sprintf("%q is not a kind of row", r.Kind), "Use account, group, calc or heading")
		}
		before[r.ID] = true
	}
	return bad
}

// Cell is one figure: a plain decimal, or not available.
type Cell struct {
	Value string
	NA    bool
}

// Result is a row with its cells, one per column.
type Result struct {
	ID, Label, Kind, Format string
	Cells                   []Cell
}

// Compute works a definition out: figures are each account's figure per column as the statements show them (financials.AccountFigures),
// and accounts every account of the chart. A row for an account the chart does not have is an error naming the code. Rows of zeros
// are left out unless the definition includes them; a heading and a calculated row always stay.
func Compute(d Definition, base money.Currency, accounts []storage.Account, figures map[uuid.UUID][]money.Money) ([]Result, error) {
	if bad := Validate(d); len(bad) > 0 {
		return nil, fmt.Errorf("%s: %s %s", bad[0].Code, bad[0].Field, bad[0].Message)
	}
	n := len(d.Columns)
	byCode := map[string]storage.Account{}
	for _, a := range accounts {
		byCode[a.Code] = a
	}
	sumOf := func(a storage.Account) []*apd.Decimal {
		out := make([]*apd.Decimal, n)
		for i := range out {
			out[i] = apd.New(0, 0)
		}
		if ms, ok := figures[a.ID]; ok {
			for i := 0; i < n && i < len(ms); i++ {
				out[i] = ms[i].Decimal()
			}
		}
		return out
	}
	vals := map[string][]*apd.Decimal{} // by row ID: nil entry = not available
	var out []Result
	for _, r := range d.Rows {
		res := Result{ID: r.ID, Label: r.Label, Kind: r.Kind, Cells: make([]Cell, n)}
		switch r.Kind {
		case KindHeading:
		case KindAccount:
			a, ok := byCode[r.Code]
			if !ok {
				return nil, fmt.Errorf("%s: row %s names the account %s, which the chart does not have", CodeRowFields, r.ID, r.Code)
			}
			v := sumOf(a)
			vals[r.ID] = v
			res.Format = FormatAmount
			fillAmount(&res, v, base)
		case KindGroup:
			total := make([]*apd.Decimal, n)
			for i := range total {
				total[i] = apd.New(0, 0)
			}
			for _, a := range accounts {
				if a.ReportGroup != r.Group || a.Group {
					continue
				}
				for i, x := range sumOf(a) {
					if _, err := ctx.Add(total[i], total[i], x); err != nil {
						return nil, err
					}
				}
			}
			vals[r.ID] = total
			res.Format = FormatAmount
			fillAmount(&res, total, base)
		case KindCalc:
			e, _ := parse(r.Formula) // validated above
			v := make([]*apd.Decimal, n)
			for i := 0; i < n; i++ {
				env := func(id string) *apd.Decimal {
					if xs, ok := vals[id]; ok {
						return xs[i]
					}
					return nil
				}
				v[i] = e.eval(env)
			}
			vals[r.ID] = v
			res.Format = r.Format
			for i, x := range v {
				if x == nil {
					res.Cells[i] = Cell{NA: true}
					continue
				}
				res.Cells[i] = Cell{Value: show(x, r.Format, base)}
			}
		}
		if (r.Kind == KindAccount || r.Kind == KindGroup) && !d.Filters.IncludeZero && allZero(vals[r.ID]) {
			continue
		}
		out = append(out, res)
	}
	return out, nil
}

func allZero(xs []*apd.Decimal) bool {
	for _, x := range xs {
		if x != nil && !x.IsZero() {
			return false
		}
	}
	return true
}

func fillAmount(r *Result, v []*apd.Decimal, base money.Currency) {
	for i, x := range v {
		r.Cells[i] = Cell{Value: show(x, FormatAmount, base)}
	}
}

// show rounds an exact value once for its format: an amount to the currency's minor units, a percent to 1 decimal of its
// hundredfold, a ratio to 2 decimals, each half away from zero (RND-KPI).
func show(x *apd.Decimal, format string, base money.Currency) string {
	switch format {
	case FormatPercent:
		var h apd.Decimal
		_, _ = ctx.Mul(&h, x, apd.New(100, 0))
		return rounding.KPI(&h, 1)
	case FormatRatio:
		return rounding.KPI(x, 2)
	}
	return rounding.KPI(x, base.MinorUnits)
}
