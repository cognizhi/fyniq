package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// symbols are the currency symbols the symbol format writes; another currency is written with its code.
var symbols = map[string]string{
	"SGD": "S$", "USD": "US$", "EUR": "€", "GBP": "£", "JPY": "¥", "AUD": "A$", "HKD": "HK$", "MYR": "RM", "CNY": "CN¥", "INR": "₹",
	"NZD": "NZ$", "CAD": "C$",
}

// Minus is the sign a document writes before a negative amount: U+2212, as the screens write it (MASTER.md).
const Minus = "−"

// grouped writes a decimal string, such as "-1234.50", with commas between thousands and the minus sign.
func grouped(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if hasFrac {
		b.WriteString("." + frac)
	}
	if neg {
		return Minus + b.String()
	}
	return b.String()
}

// FormatMoney writes an amount in a format of the catalogue: code "SGD 1,234.50", symbol "S$1,234.50", plain "1,234.50".
// The amount is at its currency's scale already; nothing is rounded (RND-DISPLAY).
func FormatMoney(m money.Money, format string) string {
	s := grouped(m.String())
	switch format {
	case "plain":
		return s
	case "symbol":
		if sym, ok := symbols[m.Currency().Code]; ok {
			if neg := strings.HasPrefix(s, Minus); neg {
				return Minus + sym + strings.TrimPrefix(s, Minus)
			}
			return sym + s
		}
	}
	return m.Currency().Code + " " + s
}

// FormatDate writes a day in a format of the catalogue: medium "23 Oct 2026", long "23 October 2026", short
// "23/10/2026", iso "2026-10-23".
func FormatDate(d time.Time, format string) string {
	switch format {
	case "long":
		return d.Format("2 January 2006")
	case "short":
		return d.Format("02/01/2006")
	case "iso":
		return d.Format(time.DateOnly)
	}
	return d.Format("2 Jan 2006")
}

// Format writes a field's value in format, the default for its type when format is "".
func Format(v Value, format string) (string, error) {
	switch {
	case v.Amount != nil:
		m, err := v.Amount.Money()
		if err != nil {
			return "", err
		}
		return FormatMoney(m, cmpOr(format, doctemplate.Formats[doctemplate.TypeMoney][0])), nil
	case v.Date != "":
		d, err := time.Parse(time.DateOnly, v.Date)
		if err != nil {
			return "", fmt.Errorf("render: %q is not a date", v.Date)
		}
		return FormatDate(d, cmpOr(format, doctemplate.Formats[doctemplate.TypeDate][0])), nil
	case v.Number != nil:
		return grouped(fmt.Sprint(*v.Number)), nil
	}
	return v.Text, nil
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
