package budget

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// MaxImportRows is the most rows an imported sheet holds: the header and a budget's lines.
const MaxImportRows = MaxLines + 1

// column letters, for the messages: 0 is A, 26 is AA.
func letter(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// monthOf reads a month column's header: YYYY-MM, "Jan 2027", or a date the sheet stores as a number.
func monthOf(g sheet.Grid, row, col int) (time.Time, bool) {
	h := strings.TrimSpace(g.Rows[row][col])
	if g.Numeric[row][col] {
		if d, ok := g.Date(h); ok {
			return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC), true
		}
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01", "Jan 2006", "January 2006", "2006-01-02"} {
		if d, err := time.ParseInLocation(layout, h, time.UTC); err == nil {
			return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC), true
		}
	}
	return time.Time{}, false
}

// FromSheet reads a budget's lines from a sheet for the year (design D6): a header row with Account, a column for each
// dimension by its name, and a column for each month of the year; then one row per line, the account by its code
// (a name after it is ignored), each dimension's value by its code, and each month's amount, blank for nothing. Every
// problem comes back at once, each with its row and column (ENG-23); nothing is loaded while any remains.
func FromSheet(g sheet.Grid, y period.Year, chart Chart) ([]BudgetLine, ledger.Problems) {
	var probs ledger.Problems
	add := func(row, col int, msg, fix string) {
		field := fmt.Sprintf("row %d", row+1)
		if col >= 0 {
			field += ", column " + letter(col)
		}
		probs = append(probs, ledger.Problem{Code: CodeImport, Field: field, Message: msg, Fix: fix})
	}
	if len(g.Rows) == 0 {
		add(0, -1, "the sheet is empty", "Put a header row first: Account, the dimensions and the year's months")
		return nil, probs
	}
	account, dims, months := -1, map[int]uuid.UUID{}, map[int]int{}
	want := map[string]int{}
	for i, m := range y.Months {
		want[m.StartsOn.Format("2006-01")] = i
	}
	for c, h := range g.Rows[0] {
		name := strings.ToLower(strings.TrimSpace(h))
		switch {
		case name == "":
		case name == "account":
			account = c
		case chart.Dimensions[name] != uuid.Nil:
			dims[c] = chart.Dimensions[name]
		default:
			m, ok := monthOf(g, 0, c)
			i, inYear := want[m.Format("2006-01")]
			switch {
			case !ok:
				add(0, c, fmt.Sprintf("%q is neither Account, a dimension nor a month", strings.TrimSpace(h)),
					"Name the column Account, a dimension such as Department, or a month such as 2027-01")
			case !inYear:
				add(0, c, fmt.Sprintf("%s is not a month of the year %s to %s", m.Format("Jan 2006"), y.StartsOn.Format("Jan 2006"), y.EndsOn.Format("Jan 2006")),
					"Keep the year's months only")
			default:
				months[c] = i
			}
		}
	}
	if account < 0 {
		add(0, -1, "the header has no Account column", "Name the column of account codes Account")
	}
	if len(months) != len(y.Months) {
		add(0, -1, fmt.Sprintf("the header has %d of the year's %d months", len(months), len(y.Months)), "Give a column for each month of the year")
	}
	if len(probs) > 0 {
		return nil, probs
	}
	var out []BudgetLine
	for r := 1; r < len(g.Rows); r++ {
		row := g.Rows[r]
		cell := func(c int) string {
			if c < len(row) {
				return strings.TrimSpace(row[c])
			}
			return ""
		}
		blank := true
		for _, v := range row {
			blank = blank && strings.TrimSpace(v) == ""
		}
		if blank {
			continue
		}
		code, _, _ := strings.Cut(cell(account), " ")
		acc, ok := chart.ByCode[code]
		l := BudgetLine{Dimensions: []string{}, Amounts: make([]MonthAmount, len(y.Months))}
		switch {
		case !ok:
			add(r, account, fmt.Sprintf("the chart has no account %q", code), "Enter an account's code, such as 6110")
		case !Budgetable(acc):
			add(r, account, fmt.Sprintf("%s %s is not a postable, active income or expense account", acc.Code, acc.Name), "Budget income and expense accounts only")
		default:
			l.AccountID = acc.ID.String()
		}
		for c, d := range dims {
			v := cell(c)
			if v == "" {
				continue
			}
			id, ok := chart.Codes[d][strings.ToLower(v)]
			if !ok {
				add(r, c, fmt.Sprintf("the dimension has no value %q", v), "Enter a value's code, or leave the cell blank for the whole company")
				continue
			}
			l.Dimensions = append(l.Dimensions, id.String())
		}
		for c, i := range months {
			v := strings.ReplaceAll(cell(c), ",", "")
			if v == "" {
				v = "0"
			}
			if !amountPattern.MatchString(v) {
				add(r, c, fmt.Sprintf("%q is not an amount", cell(c)), "Enter an amount, such as 2500.00, or leave the cell blank")
				continue
			}
			l.Amounts[i] = v
		}
		out = append(out, l)
	}
	if len(out) > MaxLines {
		add(0, -1, fmt.Sprintf("the sheet has %d lines; a budget keeps at most %d", len(out), MaxLines), "Budget fewer accounts or values")
	}
	if len(probs) > 0 {
		return nil, probs
	}
	return out, nil
}
