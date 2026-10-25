package bankfile

import (
	"regexp"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// MT940 (SWIFT customer statement; DBS IDEAL exports it as "MT940 SWIFT" and "MT940 SAP"): a file holds one or more
// statements, each from a :20: tag to its :62F: or :62M: closing balance. :25: names the account, :60F:/:60M: the
// opening balance, :61: each line and :86: its details, free text in SWIFT and ?-coded subfields in the SAP variant
// (?20 to ?29 the purpose, ?32 and ?33 the counterparty's name) [verify DBS's SAP layout against a sample].

// mt940Balance is :60F:, :60M:, :62F: or :62M: — a debit or credit mark, a date, the currency and the amount.
var mt940Balance = regexp.MustCompile(`^([CD])(\d{6})([A-Z]{3})([0-9]+,[0-9]*)$`)

// mt940Line is :61: — the value date, an optional entry date, the mark (C, D, RC, RD), an optional funds code, the
// amount, the transaction type, the customer reference and, after //, the bank's reference.
var mt940Line = regexp.MustCompile(`^(\d{6})(\d{4})?(R?[CD])([A-Z])?([0-9]+,[0-9]*)([NFS][A-Z0-9]{3})([^/]*?)(?://(.*))?$`)

type mt940Field struct {
	tag   string
	value string
	line  int
}

// mt940Fields splits the text into its tags, joining continuation lines, inside the {4: block when there is one.
func mt940Fields(data []byte, lim Limits) ([]mt940Field, error) {
	s := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	if i := strings.Index(s, "{4:"); i >= 0 {
		s = s[i+3:]
	}
	var out []mt940Field
	for n, raw := range strings.Split(s, "\n") {
		if n+1 > lim.MaxLines*4 {
			return nil, refuse(CodeTooLarge, n+1, "", "Split the statement by month.", "the file has more than %d lines", lim.MaxLines*4)
		}
		l := strings.TrimRight(raw, " ")
		switch {
		case l == "" || l == "-" || strings.HasPrefix(l, "-}") || l == "}":
			continue
		case strings.HasPrefix(l, ":") && strings.Index(l[1:], ":") > 0:
			end := 1 + strings.Index(l[1:], ":")
			out = append(out, mt940Field{tag: l[1:end], value: l[end+1:], line: n + 1})
		case len(out) > 0:
			out[len(out)-1].value += "\n" + l
		}
	}
	if len(out) == 0 {
		return nil, refuse(CodeFormat, 0, "", "Export the statement as MT940 from the bank.", "the file has no MT940 fields")
	}
	return out, nil
}

func mt940Date(s string, line int, field string) (time.Time, error) {
	return date(s, []string{"060102"}, line, field)
}

func mt940Amount(mark, s string, cur money.Currency, line int, field string) (money.Money, error) {
	m, err := amount(s, Number{Decimal: ","}, cur, line, field)
	if err != nil {
		return m, err
	}
	if mark == "D" || mark == "RC" {
		return m.Neg(), nil
	}
	return m, nil
}

func parseMT940(data []byte, sap bool, lim Limits) ([]Statement, error) {
	fields, err := mt940Fields(data, lim)
	if err != nil {
		return nil, err
	}
	var out []Statement
	var cur *Statement
	var last *Line
	lines := 0
	for _, f := range fields {
		v := strings.TrimSpace(f.value)
		switch f.tag {
		case "20":
			out = append(out, Statement{})
			cur, last = &out[len(out)-1], nil
		case "25":
			if cur == nil {
				return nil, refuse(CodeFormat, f.line, ":25:", "Export the statement as MT940 from the bank.", "an account before the statement's :20:")
			}
			id := v
			if i := strings.LastIndex(id, "/"); i >= 0 {
				id = id[i+1:] // BIC/account: the account
			}
			if cur.Account, err = text(id, f.line, ":25:", lim); err != nil {
				return nil, err
			}
		case "60F", "60M", "62F", "62M":
			if cur == nil {
				return nil, refuse(CodeFormat, f.line, ":"+f.tag+":", "Export the statement as MT940 from the bank.", "a balance before the statement's :20:")
			}
			m := mt940Balance.FindStringSubmatch(v)
			if m == nil {
				return nil, refuse(CodeFormat, f.line, ":"+f.tag+":", "Export the statement as MT940 from the bank.", "%q is not a balance", v)
			}
			c, err := currency(m[3], f.line, ":"+f.tag+":")
			if err != nil {
				return nil, err
			}
			if cur.Currency.Code != "" && cur.Currency != c {
				return nil, refuse(CodeCurrency, f.line, ":"+f.tag+":", "Import each currency's statement on its own.", "a statement in %s and %s", cur.Currency.Code, c.Code)
			}
			cur.Currency = c
			on, err := mt940Date(m[2], f.line, ":"+f.tag+":")
			if err != nil {
				return nil, err
			}
			amt, err := mt940Amount(m[1], m[4], c, f.line, ":"+f.tag+":")
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(f.tag, "60") {
				cur.Opening, cur.From = &amt, on
			} else {
				cur.Closing, cur.To = &amt, on
			}
		case "61":
			if cur == nil || cur.Currency.Code == "" {
				return nil, refuse(CodeFormat, f.line, ":61:", "Export the statement as MT940 from the bank.", "a line before the statement's opening balance")
			}
			first, rest, _ := strings.Cut(v, "\n")
			m := mt940Line.FindStringSubmatch(first)
			if m == nil {
				return nil, refuse(CodeFormat, f.line, ":61:", "Export the statement as MT940 from the bank.", "%q is not a statement line", first)
			}
			lines++
			if lines > lim.MaxLines {
				return nil, refuse(CodeTooLarge, f.line, ":61:", "Split the statement by month.", "the file has more than %d lines", lim.MaxLines)
			}
			value, err := mt940Date(m[1], f.line, ":61: value date")
			if err != nil {
				return nil, err
			}
			booked := value
			if m[2] != "" {
				// The entry date has no year: the value date's, or the next or previous one across a year end.
				if booked, err = date(value.Format("2006")+m[2], []string{"20060102"}, f.line, ":61: entry date"); err != nil {
					return nil, err
				}
				if d := booked.Sub(value); d > 180*24*time.Hour {
					booked = booked.AddDate(-1, 0, 0)
				} else if d < -180*24*time.Hour {
					booked = booked.AddDate(1, 0, 0)
				}
			}
			amt, err := mt940Amount(m[3], m[5], cur.Currency, f.line, ":61: amount")
			if err != nil {
				return nil, err
			}
			ref := strings.TrimSpace(m[8])
			customer := strings.TrimSpace(m[7])
			if customer == "NONREF" {
				customer = ""
			}
			l := Line{Position: len(cur.Lines) + 1, Booked: booked, Amount: amt}
			if !value.Equal(booked) {
				l.Value = &value
			}
			if l.Reference, err = text(ref, f.line, ":61: bank reference", lim); err != nil {
				return nil, err
			}
			if l.Description, err = text(strings.TrimSpace(customer+" "+rest), f.line, ":61: reference", lim); err != nil {
				return nil, err
			}
			cur.Lines = append(cur.Lines, l)
			last = &cur.Lines[len(cur.Lines)-1]
		case "86":
			if last == nil {
				continue // details of the statement as a whole
			}
			desc, party := v, ""
			if sap && strings.Contains(v, "?") {
				desc, party = sapDetails(v)
			}
			if d, err := text(strings.TrimSpace(last.Description+" "+desc), f.line, ":86:", lim); err == nil {
				last.Description = d
			} else {
				return nil, err
			}
			if party != "" {
				if last.Counterparty, err = text(party, f.line, ":86: ?32", lim); err != nil {
					return nil, err
				}
			}
		}
	}
	for i := range out {
		if out[i].Currency.Code == "" {
			return nil, refuse(CodeFormat, 0, ":60F:", "Export the statement as MT940 from the bank.", "statement %d has no opening balance", i+1)
		}
		if out[i].From.IsZero() || out[i].To.IsZero() {
			span(&out[i])
		}
	}
	return out, nil
}

// sapDetails reads the SAP variant's ?-coded :86: subfields: the purpose (?20 to ?29) and the counterparty's name (?32,
// ?33).
func sapDetails(v string) (purpose, party string) {
	flat := strings.ReplaceAll(v, "\n", "")
	var parts, names []string
	for _, f := range strings.Split(flat, "?")[1:] {
		if len(f) < 2 {
			continue
		}
		code, body := f[:2], f[2:]
		switch {
		case code >= "20" && code <= "29":
			parts = append(parts, body)
		case code == "32" || code == "33":
			names = append(names, body)
		}
	}
	return strings.Join(parts, " "), strings.Join(names, " ")
}
