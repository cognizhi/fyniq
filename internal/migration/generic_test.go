package migration

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The generic import's files (FR-MIG-03, design D9): each entity's template, CSV or XLSX, names its workbook sheet's
// input columns; filled in, it reads as the sheet does, its dates written YYYY-MM-DD in CSV; the workbook's own sheet
// reads as it is; a file without the header, or of more than 1,000 rows, is refused, saying why.
// req: FR-MIG-03
func TestTheGenericImportReadsItsTemplatesAndTheWorkbooksSheets(t *testing.T) {
	sgd := money.MustLookup("SGD")
	created := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	if got := ImportEntities(); strings.Join(got, ",") != "accounts,bills,contacts,invoices,items,journals" {
		t.Errorf("the entities: %v", got)
	}
	for _, entity := range ImportEntities() {
		for _, format := range []string{"csv", "xlsx"} {
			b, err := Template(entity, format, created)
			if err != nil {
				t.Fatalf("%s's %s template: %v", entity, format, err)
			}
			w, err := ReadImport(entity, b, sgd)
			if err != nil || len(w.Problems) != 0 || len(w.Contacts)+len(w.Items)+len(w.Accounts) != 0 {
				t.Errorf("%s's empty %s template: %v %+v", entity, format, err, w)
			}
		}
	}
	tpl, _ := Template("contacts", "csv", created)
	if !strings.HasPrefix(string(tpl), `"Name","Customer / supplier","Email",`) {
		t.Errorf("the contacts template: %q", tpl)
	}
	file := string(tpl) + "Acme Pte Ltd,Customer,ar@acme.example,,1 Example Road,SGD,30,4010,SG,\r\n" +
		"\"Beta, Gamma LLP\",Supplier,,,,USD,14,,US,a note\r\n"
	w, err := ReadImport("contacts", []byte(file), sgd)
	if err != nil || len(w.Problems) != 0 || len(w.Contacts) != 2 || w.Contacts[1].Name != "Beta, Gamma LLP" || w.Contacts[0].Terms.Text('f') != "30" ||
		w.Contacts[0].Category != "4010" || w.Contacts[0].Row != 2 || w.Contacts[1].Row != 3 {
		t.Fatalf("the contacts file: %v %+v", err, w)
	}

	// The workbook's own sheet, pasted with its title rows: its header on row 5.
	sample := read(t, sample(t))
	w, err = ReadImport("contacts", sampleBytes(t), sgd)
	if err != nil || len(w.Contacts) != len(sample.Contacts) || w.Contacts[0].Name != sample.Contacts[0].Name || w.Contacts[0].Row != sample.Contacts[0].Row {
		t.Errorf("the workbook's Contacts sheet: %v, %d contacts", err, len(w.Contacts))
	}
	w, err = ReadImport("items", sampleBytes(t), sgd)
	if err != nil || len(w.Items) != len(sample.Items) || w.Items[0].Price.Text('f') != sample.Items[0].Price.Text('f') {
		t.Errorf("the workbook's Items sheet: %v, %d items", err, len(w.Items))
	}

	// The documents' sheets (rev 0.271): the Invoices sheet with the Doc Lines beside it in the same workbook, the Bills and
	// the Journal sheets.
	w, err = ReadImport("invoices", sampleBytes(t), sgd)
	if err != nil || len(w.Invoices) != len(sample.Invoices) || len(w.DocLines) != len(sample.DocLines) || w.Invoices[0].Number != sample.Invoices[0].Number {
		t.Errorf("the workbook's Invoices sheet: %v, %d invoices, %d lines", err, len(w.Invoices), len(w.DocLines))
	}
	w, err = ReadImport("bills", sampleBytes(t), sgd)
	if err != nil || len(w.Bills) != len(sample.Bills) {
		t.Errorf("the workbook's Bills sheet: %v, %d bills", err, len(w.Bills))
	}
	w, err = ReadImport("journals", sampleBytes(t), sgd)
	if err != nil || len(w.Journal) != len(sample.Journal) || len(w.journals()) != len(sample.journals()) {
		t.Errorf("the workbook's Journal sheet: %v, %d lines", err, len(w.Journal))
	}
	jt, _ := Template("journals", "csv", created)
	if w, err := ReadImport("journals", append(jt, []byte(",2026-03-31,6310,No entry,10,,,,\r\n")...), sgd); err != nil {
		t.Errorf("a line with no entry: %v", err)
	} else if len(w.Problems) != 1 || w.Problems[0].Column != "Entry #" || w.Problems[0].Row != 2 {
		t.Errorf("a line with no entry: %+v", w.Problems)
	}

	var refused Refused
	for what, data := range map[string]string{
		"a file with no header": "a,b,c\r\n1,2,3\r\n",
		"a file of 1,001 rows":  string(tpl) + strings.Repeat("X,Customer,,,,,,,,\r\n", 1001),
		"a broken CSV":          "Name,\"Customer\r\n\"x\"y\",z\r\n",
	} {
		if _, err := ReadImport("contacts", []byte(data), sgd); !errors.As(err, &refused) || refused[0].Code != CodeGeneric || refused[0].Fix == "" {
			t.Errorf("%s: %v", what, err)
		}
	}
}

func sampleBytes(t *testing.T) []byte { return sample(t) }
