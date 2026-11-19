package letters_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/letters"
)

var good = []letters.Block{
	{Kind: letters.Letterhead, Text: "{{company.name}}\nUEN {{company.uen}}"},
	{Kind: letters.Heading, Text: "Employment confirmation"},
	{Kind: letters.Paragraph, Text: "This confirms that {{person.legal_name}} has been employed as {{job.title}} since {{employment.start}}."},
	{Kind: letters.Signature, Text: "For {{company.name}}"},
}

func etype(t *testing.T) letters.Type {
	t.Helper()
	ty, ok := letters.TypeOf("employment_confirmation")
	if !ok {
		t.Fatal("no such type")
	}
	return ty
}

// A template is text blocks with fields from the typed catalogue and a type's own list: an unknown field, a salary field on a type that does not show it, a block that is
// not a block, a template with no signature and one too long are each refused, all at once, with the block named (NM-LTR-01, NM-LTR-02).
// req: NM-LTR-01, NM-LTR-02
func TestATemplateUsesOnlyTheFieldsOfItsType(t *testing.T) {
	ty := etype(t)
	if p := letters.Check(ty, good); len(p) != 0 {
		t.Fatalf("a good template: %+v", p)
	}
	bad := []letters.Block{
		{Kind: letters.Paragraph, Text: "Pays {{pay.basic_salary}} to {{person.nickname}} and {{ broken"},
		{Kind: "script", Text: "x"},
		{Kind: letters.Paragraph, Text: strings.Repeat("a", letters.MaxText+1)},
	}
	got := letters.Check(ty, bad)
	text := ""
	for _, p := range got {
		text += p.Message + "\n"
	}
	for _, want := range []string{"{{pay.basic_salary}}, which a employment confirmation letter may not show", "{{person.nickname}}, which is not a merge field", "not written as {{group.name}}", `"script", which is not a block`, "longer than 2000", "ends with a signature"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if p := letters.Check(ty, nil); len(p) != 1 {
		t.Errorf("an empty template: %+v", p)
	}
	// A field the catalogue has but the type does not list is refused.
	if p := letters.Check(ty, []letters.Block{{Kind: letters.Paragraph, Text: "Until {{employment.end}}"}, {Kind: letters.Signature, Text: "x"}}); len(p) != 1 {
		t.Errorf("a field outside the type: %+v", p)
	}
	if used := letters.FieldsUsed(good); len(used) != 5 || used[0] != "company.name" {
		t.Errorf("fields used: %v", used)
	}
	// The types that show salary or need the terms say so, and no type shows evaluative text: there is no such field.
	for _, ty := range letters.Types {
		if ty.Salary && ty.Needs != "salary" {
			t.Errorf("%s shows salary but is offered", ty.Key)
		}
		for _, k := range ty.FieldKeys {
			if f, ok := letters.FieldOf(k); !ok && !strings.HasPrefix(k, "pay.") {
				t.Errorf("%s lists %s, which is no field (%v)", ty.Key, k, f)
			}
		}
	}
}

// Filling a template writes dates in words and refuses a field the record does not hold, naming it, so a letter never goes out with a blank (NM-LTR-03).
// req: NM-LTR-03
func TestFillingWritesDatesAndRefusesAMissingFact(t *testing.T) {
	v := letters.Values{"company.name": "Acme Pte. Ltd.", "company.uen": "201912345A", "person.legal_name": "Ana Tan", "job.title": "Engineer", "employment.start": "2026-01-05"}
	f, err := letters.Fill(good, v)
	if err != nil || !strings.Contains(f.Blocks[2].Text, "Ana Tan has been employed as Engineer since 5 January 2026.") || f.Blocks[0].Text != "Acme Pte. Ltd.\nUEN 201912345A" {
		t.Fatalf("fill: %+v %v", f, err)
	}
	delete(v, "employment.start")
	v["job.title"] = "  "
	if _, err := letters.Fill(good, v); err == nil || !strings.Contains(err.Error(), "Job title, Start date") {
		t.Errorf("a missing fact: %v", err)
	}
}

// The local renderer gives the same bytes for the same letter, a valid PDF with a page footer that says it is local, and refuses a character it cannot draw
// (design D3, NM-LTR-07).
// req: NM-LTR-07
func TestTheLocalRendererIsDeterministicAndRefusesWhatItCannotDraw(t *testing.T) {
	f, _ := letters.Fill(good, letters.Values{"company.name": "Acme (Singapore) Pte. Ltd.", "company.uen": "201912345A", "person.legal_name": "Ana Tan", "job.title": "Engineer", "employment.start": "2026-01-05"})
	a, err := letters.Local{}.Render(context.Background(), letters.Document{Blocks: f.Blocks, Footer: "Ref EC-2026-0001"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := letters.Local{}.Render(context.Background(), letters.Document{Blocks: f.Blocks, Footer: "Ref EC-2026-0001"})
	if !bytes.Equal(a, b) || sha256.Sum256(a) != sha256.Sum256(b) {
		t.Error("the same letter gave different bytes")
	}
	if !bytes.HasPrefix(a, []byte("%PDF-1.4")) || !bytes.HasSuffix(a, []byte("%%EOF\n")) || !bytes.Contains(a, []byte("Local rendering: for the local build only.")) || !bytes.Contains(a, []byte(`Acme \(Singapore\) Pte. Ltd.`)) {
		t.Errorf("not the PDF expected: %q", a[:120])
	}
	c, _ := letters.Local{}.Render(context.Background(), letters.Document{Blocks: f.Blocks, Footer: "Ref EC-2026-0002"})
	if bytes.Equal(a, c) {
		t.Error("a different reference gave the same bytes")
	}
	if _, err := (letters.Local{}).Render(context.Background(), letters.Document{Blocks: []letters.Block{{Kind: letters.Paragraph, Text: "陈大文 is employed."}}}); err == nil {
		t.Error("a character the renderer cannot draw was drawn")
	}
	long := letters.Document{Blocks: []letters.Block{{Kind: letters.Paragraph, Text: strings.Repeat("word ", 1200)}}}
	if out, err := (letters.Local{}).Render(context.Background(), long); err != nil || !bytes.Contains(out, []byte("Page 2 of")) {
		t.Errorf("a long letter: %v", err)
	}
	if got := letters.Reference(2026, "employment_confirmation", 7); got != "EC-2026-0007" {
		t.Errorf("reference %s", got)
	}
}

// The standard text of the key employment terms passes its own type's check, lists all seventeen items in the official order, and uses the pay fields that only this type may
// show: any other type refuses them (NM-LTR-06, NM-LTR-01).
// req: NM-LTR-06
func TestTheStandardKeyEmploymentTermsUseThePayFieldsOnlyTheirTypeMayShow(t *testing.T) {
	kets, _ := letters.TypeOf("kets")
	if p := letters.Check(kets, letters.KETDefault); len(p) != 0 {
		t.Fatalf("the standard text: %+v", p)
	}
	all := ""
	for _, b := range letters.KETDefault {
		all += b.Text + "\n"
	}
	last := -1
	for _, want := range []string{"1. Employer", "2. Employee", "3. Job title", "5. Duration of employment", "8. Basic salary per salary period", "11. Overtime payment period", "14. Leave entitlements", "17. Notice period"} {
		i := strings.Index(all, want)
		if i < 0 || i <= last {
			t.Errorf("%q is missing or out of order", want)
		}
		last = i
	}
	used := letters.FieldsUsed(letters.KETDefault)
	for _, k := range letters.KETFields {
		if !strings.Contains(strings.Join(used, ","), k) {
			t.Errorf("the standard text does not use %s", k)
		}
	}
	other, _ := letters.TypeOf("service_letter")
	p := letters.Check(other, []letters.Block{{Kind: letters.Paragraph, Text: "{{terms.basic_salary}}"}, {Kind: letters.Signature, Text: "x"}})
	if len(p) != 1 || !strings.Contains(p[0].Message, "pay term that only the key employment terms may show") {
		t.Errorf("a pay field on another type: %+v", p)
	}
	for _, ty := range letters.Types {
		if ty.Pay && ty.Key != "kets" {
			t.Errorf("%s may show pay terms", ty.Key)
		}
	}
}
