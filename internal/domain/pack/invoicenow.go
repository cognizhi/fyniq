package pack

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// The sg-invoicenow pack (PRD-001 FR-AR-12, FR-TAX-10; p2-invoicenow design D2 and D4): which lines of a GST return
// IRAS's InvoiceNow requirement makes a business submit, by GST code of sg-gst, why a document may be left out, and the
// day each group of businesses must start. A pure library: no clock, no I/O.

// What a code's lines are, for the requirement.
const (
	ScopeSubmit      = "submit"       // in the return and submitted
	ScopeExclude     = "exclude"      // in the return, and excluded from the requirement, with a reason
	ScopeNotReported = "not_reported" // not in the return: nothing is submitted and no exclusion is recorded
	ScopeChoose      = "choose"       // the tax agent chooses the GST category (ES33 or ESN33)
	ScopeNone        = "none"         // not a supply or a purchase
)

func init() {
	Schemas["sg-invoicenow"] = Schema{
		Tables: map[string][]string{
			"codes":   {"code", "direction", "scope", "gst_cat", "reason", "description", "source"},
			"reasons": {"reason", "label", "source"},
			"phases":  {"group", "from", "description", "source"},
		},
		Pinned: map[string][]string{
			"codes":  {"case", "code", "want_scope", "want_category", "want_reason", "why", "source"},
			"phases": {"case", "group", "want_from", "why", "source"},
		},
	}
}

// InvoiceNowCode is what the requirement says of a GST code.
type InvoiceNowCode struct {
	Code, Direction, Scope, Category, Reason, Description string
}

// InvoiceNow returns the requirement's treatment of a GST code in v, an sg-invoicenow version.
func InvoiceNow(v Version, code string) (InvoiceNowCode, bool) {
	for _, r := range v.Tables["codes"].Rows {
		if r[0] == code {
			return InvoiceNowCode{Code: r[0], Direction: r[1], Scope: r[2], Category: r[3], Reason: r[4], Description: r[5]}, true
		}
	}
	return InvoiceNowCode{}, false
}

// InvoiceNowReasons are the reasons a document may be left out, with their labels, in the pack's order.
func InvoiceNowReasons(v Version) [][2]string {
	var out [][2]string
	for _, r := range v.Tables["reasons"].Rows {
		out = append(out, [2]string{r[0], r[1]})
	}
	return out
}

// InvoiceNowPhase is the day a group of businesses must start to submit.
type InvoiceNowPhase struct {
	Group       string
	From        time.Time
	Description string
}

// InvoiceNowPhases are the phases in the pack's order.
func InvoiceNowPhases(v Version) []InvoiceNowPhase {
	var out []InvoiceNowPhase
	for _, r := range v.Tables["phases"].Rows {
		d, err := ParseDate(r[1])
		if err != nil {
			continue // checkInvoiceNow refuses the pack
		}
		out = append(out, InvoiceNowPhase{Group: r[0], From: d, Description: r[2]})
	}
	return out
}

var invoiceNowScopes = []string{ScopeSubmit, ScopeExclude, ScopeNotReported, ScopeChoose, ScopeNone}

// checkInvoiceNow checks the sg-invoicenow tables: each code once with a known scope, a category for a code that is
// submitted, a reason from the reasons table for a code that is excluded or not reported, and a date for each phase.
func checkInvoiceNow(dir string, v Version) Problems {
	var out Problems
	add := func(field, message, fix string) {
		out = append(out, Problem{CodeValue, dir + ": " + field, message, fix})
	}
	reasons := map[string]bool{}
	for i, r := range v.Tables["reasons"].Rows {
		if reasons[r[0]] {
			add(fmt.Sprintf("reasons.csv row %d", i+2), fmt.Sprintf("the reason %q is listed twice", r[0]), "List each reason once")
		}
		reasons[r[0]] = true
	}
	seen := map[string]bool{}
	for i, r := range v.Tables["codes"].Rows {
		at := fmt.Sprintf("codes.csv row %d", i+2)
		switch {
		case seen[r[0]]:
			add(at, fmt.Sprintf("the code %q is listed twice", r[0]), "List each GST code once")
		case !slices.Contains(invoiceNowScopes, r[2]):
			add(at, fmt.Sprintf("the scope %q is not one of %s", r[2], strings.Join(invoiceNowScopes, ", ")), "Use a scope the table's description names")
		case r[2] == ScopeSubmit && r[3] == "":
			add(at, "a code that is submitted names its GST category", "Fill gst_cat")
		case (r[2] == ScopeExclude || r[2] == ScopeNotReported) && !reasons[r[4]]:
			add(at, fmt.Sprintf("the reason %q is not in the reasons table", r[4]), "Use a reason of reasons.csv")
		case r[1] != "supply" && r[1] != "purchase" && r[1] != "none":
			add(at, fmt.Sprintf("the direction %q is not supply, purchase or none", r[1]), "Use the direction of the code in sg-gst")
		}
		seen[r[0]] = true
	}
	groups := map[string]bool{}
	for i, r := range v.Tables["phases"].Rows {
		at := fmt.Sprintf("phases.csv row %d", i+2)
		if _, err := ParseDate(r[1]); err != nil {
			add(at, fmt.Sprintf("the date %q is not YYYY-MM-DD", r[1]), "Write the day the group starts")
		}
		if groups[r[0]] {
			add(at, fmt.Sprintf("the group %q is listed twice", r[0]), "List each group once")
		}
		groups[r[0]] = true
	}
	return out
}

// runInvoiceNow runs the sg-invoicenow pinned cases.
func runInvoiceNow(v Version) []Result {
	if v.Pack != "sg-invoicenow" {
		return nil
	}
	var out []Result
	for _, row := range v.Pinned["codes"].Rows {
		want := fmt.Sprintf("%s %s %s", row[2], row[3], row[4])
		r := Result{Table: "codes", Case: row[0] + " " + row[1], Why: row[5], Want: want, Got: "no such code"}
		if c, ok := InvoiceNow(v, row[1]); ok {
			r.Got = fmt.Sprintf("%s %s %s", c.Scope, c.Category, c.Reason)
			r.Pass = r.Got == want
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["phases"].Rows {
		r := Result{Table: "phases", Case: row[0] + " " + row[1], Why: row[3], Want: row[2], Got: "no such group"}
		for _, p := range InvoiceNowPhases(v) {
			if p.Group == row[1] {
				r.Got = p.From.Format(time.DateOnly)
				r.Pass = r.Got == row[2]
			}
		}
		out = append(out, r)
	}
	return out
}
