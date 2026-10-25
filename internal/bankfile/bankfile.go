// Package bankfile reads bank statements (PRD-001 FR-BNK-02, FR-BNK-15; p1-bank-import-reconcile design D3): MT940
// (SWIFT and SAP), camt.053, OFX and QFX, and CSV or XLSX through a saved mapping, into one model of statements and
// lines. Statement files are untrusted input: every reader is bounded (size, lines, field length), reads amounts as
// exact decimals at the currency's minor units (ENG-10, ENG-44, no binary floating point), and refuses what it cannot
// read with the line and the field named (ENG-23). Readers are pure: no clock, no I/O beyond the bytes given.
package bankfile

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The formats, as the storage port names them.
const (
	FormatCSV      = "csv"
	FormatXLSX     = "xlsx"
	FormatOFX      = "ofx"
	FormatCAMT053  = "camt053"
	FormatMT940    = "mt940"
	FormatMT940SAP = "mt940_sap"
)

// Versions are each reader's version, recorded with every statement it read: a reader's change bumps its number.
var Versions = map[string]string{
	FormatCSV: "csv@1", FormatXLSX: "xlsx@1", FormatOFX: "ofx@1", FormatCAMT053: "camt053@1", FormatMT940: "mt940@1",
	FormatMT940SAP: "mt940_sap@1",
}

// The stable codes of the readers (ENG-23).
const (
	CodeTooLarge = "BANKFILE-01" // the file, a line count or a field is beyond its limit
	CodeFormat   = "BANKFILE-02" // the file is not of its format, or a required part is missing
	CodeAmount   = "BANKFILE-03" // an amount is not an exact amount in its currency
	CodeDate     = "BANKFILE-04" // a date is not a date in the expected form
	CodeCurrency = "BANKFILE-05" // a currency is unknown, or a statement mixes currencies
	CodeMapping  = "BANKFILE-06" // a CSV or XLSX mapping is incomplete or names a missing column
)

// Error is a refusal: what is wrong, where, and how to fix it.
type Error struct {
	Code    string
	Line    int    // 1-based line, row or entry; 0 when the whole file
	Field   string // the field or column, "" when none
	Message string
	Fix     string
}

func (e *Error) Error() string {
	where := ""
	if e.Line > 0 {
		where = fmt.Sprintf(" (line %d", e.Line)
		if e.Field != "" {
			where += ", " + e.Field
		}
		where += ")"
	} else if e.Field != "" {
		where = " (" + e.Field + ")"
	}
	return fmt.Sprintf("%s: %s%s", e.Code, e.Message, where)
}

func refuse(code string, line int, field, fix, format string, args ...any) *Error {
	return &Error{Code: code, Line: line, Field: field, Message: fmt.Sprintf(format, args...), Fix: fix}
}

// CodeOf is err's code, "" when it is not a reader's refusal.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Limits bound a read: the file's size, its lines and each text field.
type Limits struct {
	MaxBytes int
	MaxLines int
	MaxField int
}

// DefaultLimits are generous for a month of a busy account and small enough to read safely.
var DefaultLimits = Limits{MaxBytes: 10 << 20, MaxLines: 50000, MaxField: 500}

// Statement is one account's statement as the file gives it.
type Statement struct {
	Account  string // the identifier the file names (an account number, an IBAN); "" when the format names none
	Currency money.Currency
	From, To time.Time
	Opening  *money.Money // when the format gives it
	Closing  *money.Money
	Lines    []Line
}

// Line is one booked line.
type Line struct {
	Position     int // 1-based, in the statement's order
	Booked       time.Time
	Value        *time.Time
	Amount       money.Money // signed: money in positive, money out negative
	Reference    string      // the bank's own reference, "" when none
	Counterparty string
	Description  string
	Balance      *money.Money // the running balance after the line, when the file gives it
}

// Parse reads data in format. CSV and XLSX need their mapping and the account's currency; the structured formats name
// their own. A file may hold several statements, as MT940 and camt.053 do for several accounts.
func Parse(format string, data []byte, m *Mapping, cur money.Currency, lim Limits) ([]Statement, error) {
	if lim.MaxBytes == 0 {
		lim = DefaultLimits
	}
	if len(data) > lim.MaxBytes {
		return nil, refuse(CodeTooLarge, 0, "", "Split the statement by month, or export a shorter period.",
			"the file is %d bytes; the most is %d", len(data), lim.MaxBytes)
	}
	switch format {
	case FormatMT940, FormatMT940SAP:
		return parseMT940(data, format == FormatMT940SAP, lim)
	case FormatCAMT053:
		return parseCAMT053(data, lim)
	case FormatOFX:
		return parseOFX(data, lim)
	case FormatCSV, FormatXLSX:
		if m == nil {
			return nil, refuse(CodeMapping, 0, "", "Choose or save the bank's column mapping first.", "a %s statement needs its column mapping", format)
		}
		var s Statement
		var err error
		if format == FormatCSV {
			s, err = parseCSV(data, *m, cur, lim)
		} else {
			s, err = parseXLSX(data, *m, cur, lim)
		}
		if err != nil {
			return nil, err
		}
		return []Statement{s}, nil
	}
	return nil, refuse(CodeFormat, 0, "", "Use CSV, XLSX, OFX, QFX, camt.053 or MT940.", "%q is not a statement format Fyniq reads", format)
}

// text is a field cleaned for keeping: valid UTF-8, control characters dropped, spaces collapsed, at most max runes.
func text(s string, line int, field string, lim Limits) (string, error) {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "?")
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > lim.MaxField {
		return "", refuse(CodeTooLarge, line, field, "Shorten the text in the bank's export, or ask the bank for the standard format.",
			"the field has %d characters; the most is %d", utf8.RuneCountInString(s), lim.MaxField)
	}
	return s, nil
}

var plain = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// Number is how a mapping writes amounts: its decimal mark, its thousands separator, and whether brackets mean money
// out.
type Number struct {
	Decimal   string `json:"decimal"`   // "." (default) or ","
	Thousands string `json:"thousands"` // "", ",", ".", " " or "'"
	Brackets  bool   `json:"brackets"`  // (1,234.56) is −1,234.56
}

// amount reads a written amount exactly: currency symbols and codes dropped, the thousands separator removed, the
// decimal mark made a point, a leading or trailing minus or brackets read as negative. It never rounds.
func amount(s string, n Number, cur money.Currency, line int, field string) (money.Money, error) {
	raw := strings.TrimSpace(s)
	v := raw
	neg := false
	if n.Brackets && strings.HasPrefix(v, "(") && strings.HasSuffix(v, ")") {
		neg, v = true, strings.TrimSpace(v[1:len(v)-1])
	}
	for _, p := range []string{cur.Code, "S$", "US$", "$", "€", "£", "¥"} {
		v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(v, p), p))
	}
	switch {
	case strings.HasPrefix(v, "-"), strings.HasPrefix(v, "−"):
		neg, v = !neg, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "-"), "−"))
	case strings.HasSuffix(v, "-"):
		neg, v = !neg, strings.TrimSpace(strings.TrimSuffix(v, "-"))
	case strings.HasPrefix(v, "+"):
		v = strings.TrimSpace(v[1:])
	}
	if n.Thousands != "" {
		v = strings.ReplaceAll(v, n.Thousands, "")
	}
	if n.Decimal == "," {
		v = strings.Replace(v, ",", ".", 1)
	}
	if !plain.MatchString(v) || strings.HasPrefix(v, "-") {
		return money.Money{}, refuse(CodeAmount, line, field, "Check the column mapping's decimal mark and thousands separator.",
			"%q is not an amount", raw)
	}
	if neg {
		v = "-" + v
	}
	m, err := money.ParseField(v, cur, field)
	if err != nil {
		return money.Money{}, refuse(CodeAmount, line, field, "An amount has at most its currency's decimals; check the column mapping.",
			"%q is not an exact amount in %s: %v", raw, cur.Code, err)
	}
	return m, nil
}

// date reads a date in one of layouts.
func date(s string, layouts []string, line int, field string) (time.Time, error) {
	v := strings.TrimSpace(s)
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, refuse(CodeDate, line, field, "Check the column mapping's date format, such as 02/01/2006 for day/month/year.",
		"%q is not a date in the form %s", v, strings.Join(layouts, " or "))
}

// span sets a statement's period from its lines when the format gives none.
func span(s *Statement) {
	for _, l := range s.Lines {
		if s.From.IsZero() || l.Booked.Before(s.From) {
			s.From = l.Booked
		}
		if s.To.IsZero() || l.Booked.After(s.To) {
			s.To = l.Booked
		}
	}
}

// currency looks code up.
func currency(code string, line int, field string) (money.Currency, error) {
	c, err := money.Lookup(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return money.Currency{}, refuse(CodeCurrency, line, field, "Use the account's ISO 4217 currency code, such as SGD.", "%q is not a currency Fyniq knows", code)
	}
	return c, nil
}
