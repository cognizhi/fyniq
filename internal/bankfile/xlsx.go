package bankfile

import (
	"errors"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// XLSX: the first sheet's values, read by internal/sheet with archive/zip and encoding/xml alone (design D3: no
// spreadsheet library for one sheet of untrusted values), bounded so a zip bomb stops at its limit. A number is the
// decimal text the sheet stores, read exactly; a sheet that stored an amount as an inexact binary number (such as
// 1350.4999999999998) is refused with a fix, never rounded. A date column's serial numbers become dates in the 1900 or
// 1904 system the workbook names.

func parseXLSX(data []byte, m Mapping, cur money.Currency, lim Limits) (Statement, error) {
	g, err := sheet.ReadXLSX(data, lim.MaxLines+m.Skip+1)
	var re *sheet.ReadError
	if errors.As(err, &re) {
		code := CodeFormat
		if re.TooLarge {
			code = CodeTooLarge
		}
		return Statement{}, refuse(code, re.Row, re.Ref, re.Fix+" Or export the statement as CSV.", "%s", re.Message)
	}
	if err != nil {
		return Statement{}, err
	}
	table, numeric := g.Rows, g.Numeric
	if len(table) <= m.Skip {
		return Statement{}, refuse(CodeFormat, 0, "", "Check the number of rows the mapping skips.", "the sheet has no header row")
	}
	// A date column's serial numbers become ISO dates, read by a layout added for them.
	dateCols := map[int]bool{}
	for i, h := range table[m.Skip] {
		name := strings.ToLower(strings.TrimSpace(h))
		if name != "" && (name == strings.ToLower(strings.TrimSpace(m.Date)) || name == strings.ToLower(strings.TrimSpace(m.ValueDate))) {
			dateCols[i] = true
		}
	}
	for ri := m.Skip + 1; ri < len(table); ri++ {
		for ci := range dateCols {
			if ci >= len(table[ri]) || !numeric[ri][ci] {
				continue
			}
			d, ok := g.Date(table[ri][ci])
			if !ok {
				return Statement{}, refuse(CodeDate, ri+1, table[m.Skip][ci], "Format the date column as dates in the sheet.", "%q is not a date", table[ri][ci])
			}
			table[ri][ci] = d.Format("2006-01-02")
		}
	}
	mm := m
	mm.DateFormats = append(append([]string{}, m.DateFormats...), "2006-01-02")
	return rows(table, mm, cur, lim)
}
