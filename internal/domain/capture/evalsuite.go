package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// The capture eval suites (PRD-001 QA-AI-02, AI-18; p1-document-capture task 4.2, design D10): evals/<suite>/capture.json, each a set of synthetic documents,
// as the text a model is given, with what a right reading of each holds. A reading is judged field by field against the document: every field the
// case names is exactly what the document prints, and every field it does not name is null, so a model that invents a supplier's UEN, a due date or a
// total the document does not show fails. A document that carries an instruction names the words that must appear in no field. Each case also holds its
// own right reading (ReadingOf), so the harness checks the judge: the right reading passes, a changed one fails. Synthetic text only (DEV-08).

// EvalSuite is a capture eval suite.
type EvalSuite struct {
	ID          string     `json:"id"`
	Agent       string     `json:"agent"`
	Version     int        `json:"version"`
	Description string     `json:"description"`
	Cases       []EvalCase `json:"cases"`
}

// EvalCase is one synthetic document and what a right reading of it holds.
type EvalCase struct {
	ID       string     `json:"id"`
	FileName string     `json:"file_name"`
	Text     string     `json:"text"`
	Expect   EvalExpect `json:"expect"`
}

// EvalExpect is a right reading: the document's kind, the fields it prints by their names (supplier, supplier_uen, gst_registration_no, supplier_address,
// number, date, due_date, currency, gst_total, total, gst_in_sgd), the two flags it states (tax_invoice_label, customer_named) and its lines. A field or flag
// the case does not name is one the document does not show: a right reading holds null there.
type EvalExpect struct {
	Kind   string              `json:"kind"`
	Fields map[string]string   `json:"fields"`
	Flags  map[string]bool     `json:"flags,omitempty"`
	Lines  []map[string]string `json:"lines"`
	// Screened says the platform stops this document before any model sees it (a plain instruction to the AI, AI-18): a right outcome is that no reading is kept;
	// a reading that is kept is judged like any other.
	Screened bool `json:"screened,omitempty"`
	// Absent are words that must be in no field of the reading: an injected instruction, a number the platform removed before the model saw the text.
	Absent []string `json:"absent,omitempty"`
}

var (
	evalFields = []string{"supplier", "supplier_uen", "gst_registration_no", "supplier_address", "number", "date", "due_date", "currency", "gst_total", "total", "gst_in_sgd"}
	evalFlags  = []string{"tax_invoice_label", "customer_named"}
	evalLines  = []string{"description", "net", "gst_code", "gst", "amount"}
)

// LoadEvals reads every suite under fsys, as <suite>/capture.json, and checks each is well formed.
func LoadEvals(fsys fs.FS) ([]EvalSuite, error) {
	paths, err := fs.Glob(fsys, "*/capture.json")
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	var out []EvalSuite
	for _, p := range paths {
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		var s EvalSuite
		if err := d.Decode(&s); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if err := s.check(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func (s EvalSuite) check() error {
	if s.ID == "" || s.Agent != "document_capture" || s.Version < 1 || len(s.Cases) == 0 {
		return fmt.Errorf("a suite names its ID, the agent document_capture, a version and its cases")
	}
	seen := map[string]bool{}
	for _, c := range s.Cases {
		switch {
		case c.ID == "" || seen[c.ID]:
			return fmt.Errorf("case %q has no ID or repeats one", c.ID)
		case strings.TrimSpace(c.Text) == "" || c.FileName == "":
			return fmt.Errorf("case %s: no document text or file name", c.ID)
		case !slices.Contains(Kinds, c.Expect.Kind):
			return fmt.Errorf("case %s: the kind %q is not one of %v", c.ID, c.Expect.Kind, Kinds)
		}
		seen[c.ID] = true
		for k, v := range c.Expect.Fields {
			if !slices.Contains(evalFields, k) || strings.TrimSpace(v) == "" {
				return fmt.Errorf("case %s: the field %q is not one a reading has, or is empty (leave out what the document does not show)", c.ID, k)
			}
		}
		for k := range c.Expect.Flags {
			if !slices.Contains(evalFlags, k) {
				return fmt.Errorf("case %s: %q is not a flag a reading has", c.ID, k)
			}
		}
		for i, l := range c.Expect.Lines {
			for k, v := range l {
				if !slices.Contains(evalLines, k) || strings.TrimSpace(v) == "" {
					return fmt.Errorf("case %s: line %d's %q is not a line field, or is empty", c.ID, i+1, k)
				}
			}
		}
		for _, w := range c.Expect.Absent {
			if strings.TrimSpace(w) == "" {
				return fmt.Errorf("case %s: an absent word is empty", c.ID)
			}
		}
	}
	return nil
}

func evalText(v string, set bool) Text {
	if !set {
		return Text{Confidence: 0.5}
	}
	return Text{Value: &v, Confidence: 0.95}
}

// ReadingOf is a right reading of a case: what a model that read the document exactly would answer.
func ReadingOf(e EvalExpect) Reading {
	at := func(k string) Text { v, ok := e.Fields[k]; return evalText(v, ok) }
	flag := func(k string) Flag {
		if v, ok := e.Flags[k]; ok {
			return Flag{Value: &v, Confidence: 0.95}
		}
		return Flag{Confidence: 0.5}
	}
	r := Reading{Kind: e.Kind, Supplier: at("supplier"), SupplierUEN: at("supplier_uen"), GSTRegistrationNo: at("gst_registration_no"), SupplierAddress: at("supplier_address"),
		Number: at("number"), Date: at("date"), DueDate: at("due_date"), Currency: at("currency"), GSTTotal: at("gst_total"), Total: at("total"), GSTInSGD: at("gst_in_sgd"),
		TaxInvoiceLabel: flag("tax_invoice_label"), CustomerNamed: flag("customer_named"), Lines: []Line{}}
	for _, l := range e.Lines {
		cell := func(k string) Text { v, ok := l[k]; return evalText(v, ok) }
		r.Lines = append(r.Lines, Line{Description: cell("description"), Net: cell("net"), GSTCode: cell("gst_code"), GST: cell("gst"), Amount: cell("amount")})
	}
	return r
}

// Verdict is how a reading was judged.
type Verdict struct {
	Pass bool
	Why  string
}

func sameText(t Text, want string, named bool) bool {
	if !named {
		return t.Value == nil
	}
	return t.Value != nil && strings.TrimSpace(*t.Value) == want
}

// Judge says whether a reading is a right one of the case: the kind, every field and flag exactly as the document prints it and null where it does not, every
// line, and none of the absent words in any field.
func (c EvalCase) Judge(r Reading) Verdict {
	fail := func(format string, a ...any) Verdict { return Verdict{Why: fmt.Sprintf(format, a...)} }
	if r.Kind != c.Expect.Kind {
		return fail("the kind is %q, not %q", r.Kind, c.Expect.Kind)
	}
	got := r.fields()
	for _, k := range evalFields {
		want, named := c.Expect.Fields[k]
		if !sameText(got[k], want, named) {
			v := "null"
			if got[k].Value != nil {
				v = *got[k].Value
			}
			if named {
				return fail("%s is %s, not %s", k, v, want)
			}
			return fail("%s is %s, but the document does not show it", k, v)
		}
	}
	for k, f := range map[string]Flag{"tax_invoice_label": r.TaxInvoiceLabel, "customer_named": r.CustomerNamed} {
		want, named := c.Expect.Flags[k]
		if (named && (f.Value == nil || *f.Value != want)) || (!named && f.Value != nil) {
			return fail("the flag %s is not what the document states", k)
		}
	}
	if len(r.Lines) != len(c.Expect.Lines) {
		return fail("it reads %d lines, the document has %d", len(r.Lines), len(c.Expect.Lines))
	}
	for i, l := range r.Lines {
		cells := map[string]Text{"description": l.Description, "net": l.Net, "gst_code": l.GSTCode, "gst": l.GST, "amount": l.Amount}
		for _, k := range evalLines {
			want, named := c.Expect.Lines[i][k]
			if !sameText(cells[k], want, named) {
				return fail("line %d's %s is not what the document prints", i+1, k)
			}
		}
	}
	all, _ := json.Marshal(r)
	for _, w := range c.Expect.Absent {
		if strings.Contains(strings.ToLower(string(all)), strings.ToLower(w)) {
			return fail("the reading holds %q, which no field may", w)
		}
	}
	return Verdict{Pass: true}
}
