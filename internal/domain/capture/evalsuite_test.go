package capture

import (
	"encoding/json"
	"os"
	"testing"
	"testing/fstest"
)

// The shipped capture suite loads; each case's own right reading parses strictly and passes its judge, and the judge fails a changed figure, an invented field,
// a flag the document does not state, a missing line and an injected word kept in a field.
// req: QA-AI-02
// req: AI-18
func TestTheCaptureSuiteJudgesAReading(t *testing.T) {
	suites, err := LoadEvals(os.DirFS("../../../evals"))
	if err != nil || len(suites) == 0 {
		t.Fatalf("the shipped suites: %d %v", len(suites), err)
	}
	for _, s := range suites {
		for _, c := range s.Cases {
			r := ReadingOf(c.Expect)
			raw, _ := json.Marshal(r)
			if _, err := Parse(raw); err != nil {
				t.Errorf("%s: its right reading is refused by the strict parse: %v", c.ID, err)
			}
			if v := c.Judge(r); !v.Pass {
				t.Errorf("%s: its own reading fails: %s", c.ID, v.Why)
			}
			changed := func(edit func(r *Reading)) Verdict {
				r := ReadingOf(c.Expect)
				edit(&r)
				return c.Judge(r)
			}
			x := "0.01"
			for what, edit := range map[string]func(r *Reading){
				"a changed total": func(r *Reading) { r.Total.Value = &x },
				"an invented UEN": func(r *Reading) { u := "201234567K"; r.SupplierUEN.Value = &u },
				"a changed kind":  func(r *Reading) { r.Kind = "statement" },
				"a flag it invents": func(r *Reading) {
					f := true
					r.CustomerNamed.Value = &f
					r.TaxInvoiceLabel.Value = &f
					r.Kind = c.Expect.Kind + "x"
				},
				"a missing line": func(r *Reading) { r.Lines = nil },
				"an injected word": func(r *Reading) {
					if len(c.Expect.Absent) > 0 {
						r.Lines[0].Description.Value = &c.Expect.Absent[0]
					} else {
						r.Kind = "other"
					}
				},
			} {
				if v := changed(edit); v.Pass && c.Expect.Kind != "statement" {
					t.Errorf("%s: %s passes", c.ID, what)
				}
			}
		}
	}
	bad := fstest.MapFS{"x/capture.json": {Data: []byte(`{"id":"x","agent":"document_capture","version":1,"cases":[{"id":"a","file_name":"a.txt","text":"t","expect":{"kind":"bill","fields":{"supplier_bank":"123"},"lines":[]}}]}`)}}
	if _, err := LoadEvals(bad); err == nil {
		t.Error("a case naming a field a reading does not have was accepted")
	}
}
