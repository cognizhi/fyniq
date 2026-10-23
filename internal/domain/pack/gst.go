package pack

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// GSTCode is one GST code of the sg-gst pack: its direction, whether GST is charged on it at the
// standard rate, and the GST F5 boxes of its value and of its GST (0: not reported there).
type GSTCode struct {
	Code, Direction, Description string
	StandardRated                bool
	ValueBox, GSTBox             int
}

// The directions of a GST code.
const (
	DirSupply   = "supply"
	DirPurchase = "purchase"
	DirNone     = "none"
)

var codePattern = regexp.MustCompile(`^[A-Z]{2,4}$`) // as the chart, contacts and items store it

// checkGST checks the sg-gst tables: rate periods that follow one another without a gap or an overlap,
// each rate an exact decimal fraction; codes in the format the books store, each once, with a direction
// and F5 boxes between 1 and 15.
func checkGST(dir string, v Version) Problems {
	var out Problems
	add := func(code, field, message, fix string) {
		out = append(out, Problem{code, dir + ": " + field, message, fix})
	}
	rates, ok := v.Tables["rates"]
	if ok {
		var prevTo *time.Time
		for i, row := range rates.Rows {
			at := fmt.Sprintf("rates.csv row %d", i+2)
			from, err := ParseDate(row[0])
			if err != nil {
				add(CodeDates, at, fmt.Sprintf("effective_from %q is not a date", row[0]), "Enter the first day of the rate (YYYY-MM-DD)")
				continue
			}
			if i == 0 && from.Before(v.EffectiveFrom) {
				add(CodeDates, at, "starts before the version is in force", "Start the first rate on or after effective_from")
			}
			if i > 0 && (prevTo == nil || !from.Equal(prevTo.AddDate(0, 0, 1))) {
				add(CodeDates, at, fmt.Sprintf("starts on %s, not the day after the previous rate ends", row[0]),
					"Rate periods follow one another without a gap or an overlap")
			}
			prevTo = nil
			if row[1] != "" {
				to, err := ParseDate(row[1])
				if err != nil || to.Before(from) {
					add(CodeDates, at, fmt.Sprintf("effective_to %q is not a date on or after %s", row[1], row[0]), "Enter the last day of the rate, or leave it blank for the current rate")
				} else {
					prevTo = &to
				}
			} else if i != len(rates.Rows)-1 {
				add(CodeDates, at, "is open-ended but is not the last rate", "Only the current rate has no end")
			}
			if _, err := fraction(row[2]); err != nil {
				add(CodeValue, at, fmt.Sprintf("rate %q %v", row[2], err), "Write the rate as an exact decimal fraction, such as 0.09 for 9%")
			}
		}
		if len(rates.Rows) == 0 {
			add(CodeTable, "rates.csv", "has no rates", "Add the rate history")
		}
	}
	if codes, ok := v.Tables["codes"]; ok {
		seen := map[string]bool{}
		for i, row := range codes.Rows {
			at := fmt.Sprintf("codes.csv row %d", i+2)
			if !codePattern.MatchString(row[0]) || seen[row[0]] {
				add(CodeValue, at, fmt.Sprintf("code %q is not 2 to 4 capital letters, or is repeated", row[0]), "Give every code its own 2 to 4 capital letters")
			}
			seen[row[0]] = true
			if row[1] != DirSupply && row[1] != DirPurchase && row[1] != DirNone {
				add(CodeValue, at, fmt.Sprintf("direction %q is not supply, purchase or none", row[1]), "Enter supply, purchase or none")
			}
			if row[2] != "true" && row[2] != "false" {
				add(CodeValue, at, fmt.Sprintf("standard_rated %q is not true or false", row[2]), "Enter true or false")
			}
			for _, box := range row[3:5] {
				if _, err := f5Box(box); err != nil {
					add(CodeValue, at, fmt.Sprintf("box %q %v", box, err), "Enter a GST F5 box from 1 to 15, or leave it blank when not reported")
				}
			}
		}
	}
	if limits, ok := v.Tables["limits"]; ok {
		last := map[string]*time.Time{} // each limit's previous period's last day; nil while open-ended
		seen := map[string]bool{}
		for i, row := range limits.Rows {
			at := fmt.Sprintf("limits.csv row %d", i+2)
			if !slices.Contains(Limits, row[0]) {
				add(CodeValue, at, fmt.Sprintf("limit %q is not one Fyniq reads", row[0]), "Use one of "+strings.Join(Limits, ", "))
			}
			from, err := ParseDate(row[1])
			if err != nil || from.Before(v.EffectiveFrom) {
				add(CodeDates, at, fmt.Sprintf("effective_from %q is not a date in the version's range", row[1]), "Enter the first day the limit is in force (YYYY-MM-DD)")
				continue
			}
			if prev, ok := last[row[0]]; seen[row[0]] && (!ok || prev == nil || !from.Equal(prev.AddDate(0, 0, 1))) {
				add(CodeDates, at, fmt.Sprintf("starts on %s, not the day after the limit's previous period ends", row[1]),
					"A limit's periods follow one another without a gap or an overlap")
			}
			seen[row[0]] = true
			last[row[0]] = nil
			if row[2] != "" {
				to, err := ParseDate(row[2])
				if err != nil || to.Before(from) {
					add(CodeDates, at, fmt.Sprintf("effective_to %q is not a date on or after %s", row[2], row[1]), "Enter the last day of the limit, or leave it blank while it is in force")
				} else {
					last[row[0]] = &to
				}
			}
			if !amountPattern.MatchString(row[3]) {
				add(CodeValue, at, fmt.Sprintf("amount %q is not an exact decimal of at least 0", row[3]), "Write the amount as a decimal such as 1000.00")
			}
			if !currencyPattern.MatchString(row[4]) {
				add(CodeValue, at, fmt.Sprintf("currency %q is not an ISO 4217 code", row[4]), "Enter the currency of the amount, such as SGD")
			}
		}
	}
	return out
}

// The limits of the sg-gst pack (rev 0.89).
const (
	// LimitSimplifiedInvoice is the most a simplified tax invoice's total payable, GST included, may be for its input
	// tax to be claimed (FR-AP-20).
	LimitSimplifiedInvoice = "simplified_invoice"
)

// Limits are the limits Fyniq reads.
var Limits = []string{LimitSimplifiedInvoice}

var (
	amountPattern   = regexp.MustCompile(`^[0-9]{1,15}(\.[0-9]{1,4})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Limit returns the limit name of v in force on d, as its exact amount and its currency, or false when v holds none
// then: a version from before the limits were read, or a date outside the limit's periods.
func Limit(v Version, name string, d time.Time) (amount, currency string, ok bool) {
	for _, row := range v.Tables["limits"].Rows {
		if row[0] != name {
			continue
		}
		if from, _ := ParseDate(row[1]); d.Before(from) {
			continue
		}
		if row[2] != "" {
			if to, _ := ParseDate(row[2]); d.After(to) {
				continue
			}
		}
		return row[3], row[4], true
	}
	return "", "", false
}

// fraction parses an exact decimal rate from 0 up to, not including, 1.
func fraction(s string) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil || d.Form != apd.Finite {
		return nil, fmt.Errorf("is not an exact decimal")
	}
	if d.Negative || d.Cmp(apd.New(1, 0)) >= 0 {
		return nil, fmt.Errorf("is not a fraction from 0 up to 1")
	}
	return d, nil
}

func f5Box(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 15 {
		return 0, fmt.Errorf("is not a box of the GST F5 return")
	}
	return n, nil
}

// RateOn returns the standard rate of GST in v on d, as an exact decimal fraction ("0.09"), or false
// when v holds no rate on d (before GST was introduced).
func RateOn(v Version, d time.Time) (string, bool) {
	for _, row := range v.Tables["rates"].Rows {
		from, _ := ParseDate(row[0])
		if d.Before(from) {
			continue
		}
		if row[1] != "" {
			if to, _ := ParseDate(row[1]); d.After(to) {
				continue
			}
		}
		return row[2], true
	}
	return "", false
}

// Code returns the GST code c of v, or false when v has no such code.
func Code(v Version, c string) (GSTCode, bool) {
	for _, row := range v.Tables["codes"].Rows {
		if row[0] == c {
			vb, _ := f5Box(row[3])
			gb, _ := f5Box(row[4])
			return GSTCode{Code: row[0], Direction: row[1], StandardRated: row[2] == "true", ValueBox: vb, GSTBox: gb, Description: row[5]}, true
		}
	}
	return GSTCode{}, false
}

// GSTRate returns the standard rate in force at a document's tax point, from the sg-gst version in
// force then among the organisation's active versions, and that version's id (ENG-31, ENG-45). A later
// version, or a later rate, never changes it.
func GSTRate(active []Version, taxPoint time.Time) (rate, version string, err error) {
	v, ok := At(active, "sg-gst", taxPoint)
	if !ok {
		return "", "", Problems{{CodeNone, "tax_point", fmt.Sprintf("no active sg-gst version is in force on %s", taxPoint.Format(time.DateOnly)),
			"Activate the sg-gst version that covers the date"}}
	}
	r, ok := RateOn(v, taxPoint)
	if !ok {
		return "", v.ID, Problems{{CodeNone, "tax_point", fmt.Sprintf("%s holds no GST rate on %s", v.ID, taxPoint.Format(time.DateOnly)),
			"GST was introduced on 1 Apr 1994; a supply before then bears none"}}
	}
	return r, v.ID, nil
}
