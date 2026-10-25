package bankfile

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// Mapping is a saved column mapping for an institution's CSV or XLSX statements (FR-BNK-02): which columns hold what,
// how dates and amounts are written, and how many rows to skip before the header. A column is named by its header
// text, matched without case or surrounding spaces.
type Mapping struct {
	Skip         int      `json:"skip"`         // rows before the header row
	Delimiter    string   `json:"delimiter"`    // CSV only: "," (default), ";", "\t" or "|"
	DateFormats  []string `json:"date_formats"` // Go layouts, such as "02/01/2006"; the first that reads wins
	Number       Number   `json:"number"`
	Date         string   `json:"date"`       // the booking date column (required)
	ValueDate    string   `json:"value_date"` // optional
	Amount       string   `json:"amount"`     // a signed amount column, or
	In           string   `json:"in"`         // money in and
	Out          string   `json:"out"`        // money out columns, each positive
	Reference    string   `json:"reference"`  // the bank's reference
	Counterparty string   `json:"counterparty"`
	Description  []string `json:"description"` // columns joined in order
	Balance      string   `json:"balance"`     // the running balance after the line
}

// ReadMapping reads a saved mapping's JSON and checks it is complete.
func ReadMapping(spec []byte) (Mapping, error) {
	var m Mapping
	dec := json.NewDecoder(bytes.NewReader(spec))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, refuse(CodeMapping, 0, "", "Save the mapping again from the import screen.", "the mapping is not valid: %v", err)
	}
	return m, m.check()
}

func (m Mapping) check() error {
	switch {
	case strings.TrimSpace(m.Date) == "":
		return refuse(CodeMapping, 0, "date", "Choose the column that holds the booking date.", "the mapping names no date column")
	case m.Amount == "" && (m.In == "" || m.Out == ""):
		return refuse(CodeMapping, 0, "amount", "Choose a signed amount column, or the money-in and money-out columns.",
			"the mapping names neither an amount column nor money-in and money-out columns")
	case m.Amount != "" && (m.In != "" || m.Out != ""):
		return refuse(CodeMapping, 0, "amount", "Choose either a signed amount column or money-in and money-out columns.",
			"the mapping names both an amount column and money-in or money-out columns")
	case len(m.DateFormats) == 0:
		return refuse(CodeMapping, 0, "date_formats", "Choose how dates are written, such as 02/01/2006.", "the mapping names no date format")
	case m.Skip < 0 || m.Skip > 50:
		return refuse(CodeMapping, 0, "skip", "Skip between 0 and 50 rows.", "the mapping skips %d rows", m.Skip)
	case m.Number.Decimal != "" && m.Number.Decimal != "." && m.Number.Decimal != ",":
		return refuse(CodeMapping, 0, "number.decimal", "Use a point or a comma.", "%q is not a decimal mark", m.Number.Decimal)
	case m.Number.Decimal == m.Number.Thousands && m.Number.Thousands != "":
		return refuse(CodeMapping, 0, "number.thousands", "Use a thousands separator other than the decimal mark.", "the decimal mark and the thousands separator are both %q", m.Number.Thousands)
	}
	return nil
}

// rows reads a table's rows (a header row first, after Skip rows) into lines through m.
func rows(table [][]string, m Mapping, cur money.Currency, lim Limits) (Statement, error) {
	if err := m.check(); err != nil {
		return Statement{}, err
	}
	if cur.Code == "" {
		return Statement{}, refuse(CodeCurrency, 0, "", "Open the import from the money account.", "a CSV or XLSX statement needs its account's currency")
	}
	if len(table) <= m.Skip {
		return Statement{}, refuse(CodeFormat, 0, "", "Check the number of rows the mapping skips.", "the file has no header row")
	}
	header := map[string]int{}
	for i, h := range table[m.Skip] {
		header[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))] = i
	}
	col := func(name string) (int, error) {
		if name == "" {
			return -1, nil
		}
		i, ok := header[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return -1, refuse(CodeMapping, m.Skip+1, name, "Check the mapping's column names against the file's header row.", "the file has no column %q", name)
		}
		return i, nil
	}
	var idx struct{ date, value, amount, in, out, ref, party, bal int }
	var err error
	for _, x := range []struct {
		dst  *int
		name string
	}{{&idx.date, m.Date}, {&idx.value, m.ValueDate}, {&idx.amount, m.Amount}, {&idx.in, m.In}, {&idx.out, m.Out},
		{&idx.ref, m.Reference}, {&idx.party, m.Counterparty}, {&idx.bal, m.Balance}} {
		if *x.dst, err = col(x.name); err != nil {
			return Statement{}, err
		}
	}
	var desc []int
	for _, d := range m.Description {
		i, err := col(d)
		if err != nil {
			return Statement{}, err
		}
		desc = append(desc, i)
	}
	s := Statement{Currency: cur}
	body := table[m.Skip+1:]
	if len(body) > lim.MaxLines {
		return Statement{}, refuse(CodeTooLarge, 0, "", "Split the statement by month.", "the file has %d rows; the most is %d", len(body), lim.MaxLines)
	}
	cell := func(r []string, i int) string {
		if i < 0 || i >= len(r) {
			return ""
		}
		return strings.TrimSpace(r[i])
	}
	for n, r := range body {
		line := m.Skip + 2 + n
		if strings.TrimSpace(strings.Join(r, "")) == "" {
			continue // blank rows, such as a footer's spacing
		}
		booked, err := date(cell(r, idx.date), m.DateFormats, line, m.Date)
		if err != nil {
			return Statement{}, err
		}
		var amt money.Money
		if idx.amount >= 0 {
			if amt, err = amount(cell(r, idx.amount), m.Number, cur, line, m.Amount); err != nil {
				return Statement{}, err
			}
		} else {
			in, out := cell(r, idx.in), cell(r, idx.out)
			switch {
			case in != "" && out != "":
				return Statement{}, refuse(CodeAmount, line, m.In, "A row has money in or money out, not both.", "the row has money in %q and money out %q", in, out)
			case in != "":
				amt, err = amount(in, m.Number, cur, line, m.In)
			case out != "":
				amt, err = amount(out, m.Number, cur, line, m.Out)
				amt = amt.Neg()
			default:
				return Statement{}, refuse(CodeAmount, line, m.In, "Check the mapping's money-in and money-out columns.", "the row has no amount")
			}
			if err != nil {
				return Statement{}, err
			}
		}
		if amt.IsZero() {
			continue // a zero line moves nothing and is not a statement line
		}
		l := Line{Position: len(s.Lines) + 1, Booked: booked, Amount: amt}
		if v := cell(r, idx.value); v != "" {
			vd, err := date(v, m.DateFormats, line, m.ValueDate)
			if err != nil {
				return Statement{}, err
			}
			if !vd.Equal(booked) {
				l.Value = &vd
			}
		}
		if l.Reference, err = text(cell(r, idx.ref), line, m.Reference, lim); err != nil {
			return Statement{}, err
		}
		if l.Counterparty, err = text(cell(r, idx.party), line, m.Counterparty, lim); err != nil {
			return Statement{}, err
		}
		var parts []string
		for _, i := range desc {
			parts = append(parts, cell(r, i))
		}
		if l.Description, err = text(strings.Join(parts, " "), line, "description", lim); err != nil {
			return Statement{}, err
		}
		if v := cell(r, idx.bal); v != "" {
			b, err := amount(v, m.Number, cur, line, m.Balance)
			if err != nil {
				return Statement{}, err
			}
			l.Balance = &b
		}
		s.Lines = append(s.Lines, l)
	}
	if len(s.Lines) == 0 {
		return Statement{}, refuse(CodeFormat, 0, "", "Check the file and the mapping: the export may be empty.", "the file has no lines with an amount")
	}
	span(&s)
	return s, nil
}

func parseCSV(data []byte, m Mapping, cur money.Currency, lim Limits) (Statement, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.ReuseRecord = false
	switch m.Delimiter {
	case "", ",":
		r.Comma = ','
	case ";":
		r.Comma = ';'
	case "\t", "tab":
		r.Comma = '\t'
	case "|":
		r.Comma = '|'
	default:
		return Statement{}, refuse(CodeMapping, 0, "delimiter", "Use a comma, a semicolon, a tab or a bar.", "%q is not a delimiter", m.Delimiter)
	}
	var table [][]string
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Statement{}, refuse(CodeFormat, len(table)+1, "", "Export the statement as CSV again.", "the file is not CSV: %v", err)
		}
		if len(table) > lim.MaxLines+m.Skip+1 {
			return Statement{}, refuse(CodeTooLarge, len(table)+1, "", "Split the statement by month.", "the file has more than %d rows", lim.MaxLines)
		}
		table = append(table, rec)
	}
	return rows(table, m, cur, lim)
}

// excelEpoch is day 0 of Excel's 1900 date system, as Excel counts it (it has a 29 February 1900 that never was, so
// serials from 61 on are one day late against a true calendar; 30 December 1899 corrects that for every date a bank
// statement can hold).
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
