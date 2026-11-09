package pack

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// Result is the outcome of one pinned statutory test (QA-ENG-08).
type Result struct {
	Table, Case, Why string
	Pass             bool
	Got, Want        string
}

// RunPinned runs every pinned case of v against v's own tables. The cases are worked by hand from the
// official pages, not from the tables, so a wrong value in a table fails its case (ENG-32).
func RunPinned(v Version) []Result {
	var out []Result
	if t, ok := v.Pinned["rates"]; ok && v.Pack == "sg-gst" {
		for _, row := range t.Rows {
			r := Result{Table: "rates", Case: row[0], Why: row[3], Want: row[2]}
			d, err := ParseDate(row[1])
			if err != nil {
				r.Got = fmt.Sprintf("the date %q does not parse", row[1])
				out = append(out, r)
				continue
			}
			rate, ok := RateOn(v, d)
			if !ok {
				r.Got, r.Pass = "none", row[2] == "none"
			} else {
				r.Got = rate
				r.Pass = sameDecimal(rate, row[2])
			}
			out = append(out, r)
		}
	}
	if t, ok := v.Pinned["codes"]; ok && v.Pack == "sg-gst" {
		for _, row := range t.Rows {
			want := fmt.Sprintf("%s value %s GST %s", row[2], box(row[3]), box(row[4]))
			r := Result{Table: "codes", Case: row[0] + " " + row[1], Why: row[5], Want: want, Got: "no such code"}
			if c, ok := Code(v, row[1]); ok {
				r.Got = fmt.Sprintf("%s value %s GST %s", c.Direction, box(strconv.Itoa(c.ValueBox)), box(strconv.Itoa(c.GSTBox)))
				r.Pass = r.Got == want
			}
			out = append(out, r)
		}
	}
	if t, ok := v.Pinned["limits"]; ok && v.Pack == "sg-gst" {
		for _, row := range t.Rows {
			r := Result{Table: "limits", Case: row[0] + " " + row[1], Why: row[4], Want: row[3]}
			d, err := ParseDate(row[2])
			if err != nil {
				r.Got = fmt.Sprintf("the date %q does not parse", row[2])
				out = append(out, r)
				continue
			}
			amount, cur, ok := Limit(v, row[1], d)
			if !ok {
				r.Got, r.Pass = "none", row[3] == "none"
			} else {
				r.Got = amount + " " + cur
				want := strings.Fields(row[3])
				r.Pass = len(want) == 2 && sameDecimal(amount, want[0]) && cur == want[1]
			}
			out = append(out, r)
		}
	}
	out = append(out, runDeadlines(v)...)
	out = append(out, runCIT(v)...)
	return append(out, runWages(v)...)
}

// runDeadlines runs the sg-deadlines pinned cases: each obligation's due date for its anchor and condition, each next
// working day, and each date either side of an extension; "none" where the pack must not know one.
func runDeadlines(v Version) []Result {
	if v.Pack != "sg-deadlines" {
		return nil
	}
	var out []Result
	date := func(r *Result, s string) (time.Time, bool) {
		d, err := ParseDate(s)
		if err != nil {
			r.Got = fmt.Sprintf("the date %q does not parse", s)
		}
		return d, err == nil
	}
	answer := func(r *Result, d time.Time, err error) {
		if err != nil {
			r.Got, r.Pass = "none", r.Want == "none"
			return
		}
		r.Got = d.Format(time.DateOnly)
		r.Pass = r.Got == r.Want
	}
	for _, row := range v.Pinned["due_dates"].Rows {
		r := Result{Table: "due_dates", Case: row[0] + " " + row[1], Why: row[5], Want: row[4]}
		if anchor, ok := date(&r, row[2]); ok {
			d, err := DueOn(v, row[1], anchor, Facts(row[3]))
			answer(&r, d.On, err)
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["working_days"].Rows {
		r := Result{Table: "working_days", Case: row[0] + " " + row[1], Why: row[3], Want: row[2]}
		if d, ok := date(&r, row[1]); ok {
			w, err := NextWorkingDay(v, d)
			answer(&r, w, err)
		}
		out = append(out, r)
	}
	for _, row := range v.Pinned["extensions"].Rows {
		r := Result{Table: "extensions", Case: row[0] + " " + row[1], Why: row[4], Want: row[3]}
		if d, ok := date(&r, row[2]); ok {
			e, _ := Extended(v, row[1], d)
			answer(&r, e, nil)
		}
		out = append(out, r)
	}
	return out
}

func box(s string) string {
	if s == "" || s == "0" {
		return "-"
	}
	return "box " + s
}

func sameDecimal(a, b string) bool {
	x, _, err1 := apd.NewFromString(a)
	y, _, err2 := apd.NewFromString(b)
	return err1 == nil && err2 == nil && x.Cmp(y) == 0
}

// Failed returns the results that did not pass.
func Failed(rs []Result) []Result {
	var out []Result
	for _, r := range rs {
		if !r.Pass {
			out = append(out, r)
		}
	}
	return out
}
