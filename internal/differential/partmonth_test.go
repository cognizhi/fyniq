package differential

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"

	schemas "github.com/cognizhi/fyniq/test/differential"

	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

type partMonthCase struct {
	Name     string `json:"name"`
	Basic    string `json:"basic"`
	Month    string `json:"month"`
	Weekdays []int  `json:"working_weekdays"`
	From     string `json:"from"`
	To       string `json:"to"`
	WantDays int    `json:"want_days"`
	WantDen  int    `json:"want_denominator"`
	WantPay  string `json:"want_pay"`
}

// The hand-worked part-month cases (NM-PAY-12, MOM's working days) conform to their schema and the engine reproduces each: the working days of the span and of the month, and the pay rounded once. The oracle's
// own module reproduces the same file (oracle/tests/test_part_month.py), which is how the two implementations are held to one rule (QA-ENG-02).
// req: QA-ENG-02, NM-PAY-12
func TestThePartMonthCasesConformAndTheEngineReproducesEach(t *testing.T) {
	schema := compile("part-month.schema.json", schemas.PartMonthSchema)
	files, _ := filepath.Glob("../../test/differential/part-months/*.json")
	if len(files) == 0 {
		t.Fatal("no part-month cases")
	}
	cases := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		for i, d := range doc.([]any) {
			if err := schema.Validate(d); err != nil {
				t.Errorf("%s case %d: %v", f, i, err)
			}
		}
		var list []partMonthCase
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		for _, c := range list {
			cases++
			day := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
			works := func(w time.Weekday) bool {
				iso := int(w)
				if w == time.Sunday {
					iso = 7
				}
				for _, x := range c.Weekdays {
					if x == iso {
						return true
					}
				}
				return false
			}
			month := day(c.Month)
			last := month.AddDate(0, 1, -1)
			if den := payroll.WorkingDays(works, month, last); den != c.WantDen {
				t.Errorf("%s: the month has %d working days, want %d", c.Name, den, c.WantDen)
			}
			if !day(c.To).Before(day(c.From)) {
				if n := payroll.WorkingDays(works, day(c.From), day(c.To)); n != c.WantDays {
					t.Errorf("%s: %d working days in the span, want %d", c.Name, n, c.WantDays)
				}
			} else if c.WantDays != 0 {
				t.Errorf("%s: no span but %d days wanted", c.Name, c.WantDays)
			}
			basic, _, _ := apd.NewFromString(c.Basic)
			if got := payroll.PartMonthPay(basic, works, month, day(c.From), day(c.To)).Text('f'); got != c.WantPay {
				t.Errorf("%s: pay %s, want %s", c.Name, got, c.WantPay)
			}
		}
	}
	if cases < 15 {
		t.Errorf("%d cases", cases)
	}
}

// The working days of a span split at any day add up to the working days of the whole span, and a span's days never pass the month's: the property behind a pay change in the month (two records) and a joiner.
// req: NM-PAY-12
func TestTheWorkingDaysOfASplitSpanAddUp(t *testing.T) {
	works := func(w time.Weekday) bool { return w != time.Sunday && w != time.Wednesday }
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	last := month.AddDate(0, 1, -1)
	whole := payroll.WorkingDays(works, month, last)
	for a := month; !a.After(last); a = a.AddDate(0, 0, 1) {
		for b := a; !b.After(last); b = b.AddDate(0, 0, 1) {
			left := payroll.WorkingDays(works, month, a.AddDate(0, 0, -1))
			span := payroll.WorkingDays(works, a, b)
			right := payroll.WorkingDays(works, b.AddDate(0, 0, 1), last)
			if left+span+right != whole {
				t.Fatalf("%s to %s: %d + %d + %d != %d", a.Format(time.DateOnly), b.Format(time.DateOnly), left, span, right, whole)
			}
		}
	}
	if payroll.PartMonthPay(apd.New(100000, -2), works, month, last.AddDate(0, 0, 5), last.AddDate(0, 0, 9)).Text('f') != "0.00" {
		t.Error("a span after the month")
	}
}
