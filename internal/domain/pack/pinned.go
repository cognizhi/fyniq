package pack

import (
	"fmt"
	"strconv"

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
