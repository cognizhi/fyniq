package doctemplate

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

func codes(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Code+" "+p.Field)
	}
	return out
}

// req: FR-TPL-05
// req: FR-TPL-02
func TestEveryDefaultMeetsTheRules(t *testing.T) {
	for _, kind := range Kinds {
		for _, typ := range Types {
			if ps := Check(kind, typ, Default(kind, typ)); len(ps) > 0 {
				t.Errorf("the default %s %s breaks rules: %v", typ, kind, codes(ps))
			}
		}
	}
	if ps := Check("letter", Invoice, Default(KindDocument, Invoice)); !slices.Equal(codes(ps), []string{CodeKind + " document_type"}) {
		t.Errorf("an unknown kind: %v", codes(ps))
	}
	if ps := Check(KindDocument, "payslip", Default(KindDocument, Invoice)); !slices.Equal(codes(ps), []string{CodeKind + " document_type"}) {
		t.Errorf("payslips are Nemura's, P2: %v", codes(ps))
	}
	if ps := CheckName("  "); len(ps) != 1 || ps[0].Code != CodeName {
		t.Errorf("a blank name: %v", codes(ps))
	}
	if ps := CheckName(strings.Repeat("n", MaxName+1)); len(ps) != 1 {
		t.Errorf("a long name: %v", codes(ps))
	}
}

func block(c *Content, kind string) *Block {
	for i := range c.Blocks {
		if c.Blocks[i].Kind == kind {
			return &c.Blocks[i]
		}
	}
	return nil
}

// req: FR-TPL-03
func TestTheIRASParticularsAreLocked(t *testing.T) {
	c := Default(KindDocument, Invoice)
	block(&c, BlockTotals).Shown = false
	block(&c, BlockHeader).Texts = map[string]string{TextTitle: "Invoice"}
	c.Blocks = slices.DeleteFunc(c.Blocks, func(b Block) bool { return b.Kind == BlockOurDetails })
	got := codes(Check(KindDocument, Invoice, c))
	want := []string{CodeLocked + " content.blocks[0].texts.title", CodeLocked + " content.blocks[4].shown", CodeLocked + " content.blocks"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, p := range Check(KindDocument, Invoice, c) {
		if p.Message == "" || p.Fix == "" {
			t.Errorf("%s says nothing or offers no fix", p.Code)
		}
	}

	// A credit note keeps its particulars but needs no title, so its header may change or hide.
	cn := Default(KindDocument, CreditNote)
	block(&cn, BlockHeader).Texts = map[string]string{TextTitle: "Credit memo"}
	block(&cn, BlockHeader).Shown = false
	if ps := Check(KindDocument, CreditNote, cn); len(ps) > 0 {
		t.Errorf("a credit note's header: %v", codes(ps))
	}
	block(&cn, BlockLineItems).Shown = false
	if got := codes(Check(KindDocument, CreditNote, cn)); !slices.Equal(got, []string{CodeLocked + " content.blocks[4].shown"}) {
		t.Errorf("a credit note's lines hidden: %v", got)
	}

	// Other documents carry no IRAS particulars: any block may hide.
	q := Default(KindDocument, Quote)
	for i := range q.Blocks {
		q.Blocks[i].Shown = false
	}
	if ps := Check(KindDocument, Quote, q); len(ps) > 0 {
		t.Errorf("a quote with every block hidden: %v", codes(ps))
	}
}

// req: FR-TPL-02
func TestTextsUseOnlyTypedFields(t *testing.T) {
	fields := map[string]string{}
	for _, f := range Fields(Invoice) {
		fields[f.Path] = f.Type
	}
	for _, c := range []struct {
		text string
		want int
	}{
		{"Due {{document.due_date}} or {{ document.due_date | long }}; {{document.total|symbol}}", 0},
		{"{{document.due_date|iso}} {{document.total|plain}} {{customer.name}}", 0},
		{"Price {1} and {braces} are words", 0},
		{"{{customer.nmae}}", 1},               // not in the catalogue
		{"{{supplier.name}}", 1},               // an invoice's party is a customer
		{"{{document.total|long}}", 1},         // a date's format on an amount
		{"{{customer.name|upper}}", 1},         // a text takes no format
		{"{{customer.name}", 1},                // a loose brace
		{"{{ range .Lines }}{{ end }}", 1},     // code is not a field
		{"{{document.valid_until}}", 1},        // a quote's field
		{"{{x.y}} {{document.total|code}}", 1}, // one bad, one good
	} {
		if got := checkFields("t", c.text, fields); len(got) != c.want {
			t.Errorf("%q: %d problems %v, want %d", c.text, len(got), codes(got), c.want)
		}
	}
	for _, typ := range Types {
		seen := map[string]bool{}
		for _, f := range Fields(typ) {
			if seen[f.Path] || f.Label == "" || !reference.MatchString("{{"+f.Path+"}}") {
				t.Errorf("%s: field %q repeated, unlabelled or not writable", typ, f.Path)
			}
			if _, ok := Formats[f.Type]; !ok {
				t.Errorf("%s: field %q has no type", typ, f.Path)
			}
			seen[f.Path] = true
		}
	}
	// The SGD equivalents a foreign-currency tax invoice shows (FR-AR-05).
	for _, path := range []string{"document.subtotal_sgd", "document.gst_total_sgd", "document.total_sgd"} {
		if fields[path] != TypeMoney {
			t.Errorf("an invoice's %s is not an amount", path)
		}
	}
}

// req: FR-TPL-01
func TestThePageThemeAndBlocks(t *testing.T) {
	c := Default(KindDocument, PurchaseOrder)
	c.Page.Size = "A5"
	c.Page.Margins.Left = 5
	c.Theme.Font = "Comic Sans"
	c.Theme.Accent = "#FFFF00"
	c.Blocks = append(c.Blocks, Block{Kind: BlockPaymentDetails, Shown: true}, Block{Kind: BlockNotes, Shown: true},
		Block{Kind: BlockFooter, Texts: map[string]string{TextLabel: "x"}}, Block{Kind: BlockOurDetails, Texts: map[string]string{TextBody: "x"}})
	for range MaxTextBlocks {
		c.Blocks = append(c.Blocks, Block{Kind: BlockText, Shown: true, Texts: map[string]string{TextBody: strings.Repeat("a", 10)}})
	}
	block(&c, BlockSignature).Texts[TextLabel] = "two\nlines"
	got := codes(Check(KindDocument, PurchaseOrder, c))
	n := len(c.Blocks)
	want := []string{
		CodePage + " content.page.size", CodePage + " content.page.margins_mm.left", CodeTheme + " content.theme.font", CodeTheme + " content.theme.accent",
		CodeText + " content.blocks[7].texts.label",
		CodeBlock + fmt.Sprintf(" content.blocks[%d].kind", n-9), CodeBlock + fmt.Sprintf(" content.blocks[%d]", n-8),
		CodeBlock + fmt.Sprintf(" content.blocks[%d]", n-7), CodeText + fmt.Sprintf(" content.blocks[%d].texts.label", n-7),
		CodeBlock + fmt.Sprintf(" content.blocks[%d]", n-6), CodeText + fmt.Sprintf(" content.blocks[%d].texts.body", n-6),
		CodeBlock + fmt.Sprintf(" content.blocks[%d]", n-1),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}

	long := Default(KindDocument, Invoice)
	block(&long, BlockFooter).Texts[TextBody] = strings.Repeat("a", MaxBody+1)
	long.Subject = "x"
	if got := codes(Check(KindDocument, Invoice, long)); !slices.Equal(got, []string{CodeSchema + " content", CodeText + " content.blocks[9].texts.body"}) {
		t.Errorf("a long body and a subject on a document: %v", got)
	}
	none := Content{Schema: Schema}
	if got := codes(Check(KindDocument, Receipt, none)); !slices.Equal(got, []string{CodeSchema + " content.page", CodeSchema + " content.theme"}) {
		t.Errorf("no page or theme: %v", got)
	}
	if got := codes(Check(KindDocument, Receipt, Content{Schema: "fyniq.template/2"})); !slices.Equal(got, []string{CodeSchema + " content.schema"}) {
		t.Errorf("another format: %v", got)
	}
}

// req: FR-TPL-05
func TestEmailTemplates(t *testing.T) {
	c := Default(KindEmail, Reminder)
	c.Subject = "Overdue\n{{document.overdue}}"
	c.Body = "{{document.overdue|long}}"
	c.Theme = &Theme{}
	got := codes(Check(KindEmail, Reminder, c))
	if want := []string{CodeSchema + " content", CodeSubject + " content.subject", CodeField + " content.body"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := codes(Check(KindEmail, Reminder, Content{Schema: Schema})); !slices.Equal(got, []string{CodeSubject + " content.subject", CodeBody + " content.body"}) {
		t.Errorf("a blank email: %v", got)
	}
}

// req: FR-TPL-01
func TestTheAccentReadsOnWhite(t *testing.T) {
	for _, c := range []struct {
		hex  string
		want int64
	}{{"#000000", 210}, {"#FFFFFF", 10}, {Accent, 54}, {"#767676", 45}, {"#777777", 44}, {"#ffff00", 10}} {
		got, err := ContrastOnWhite(c.hex)
		if err != nil || got != c.want {
			t.Errorf("%s: %d (%v), want %d", c.hex, got, err, c.want)
		}
	}
	for _, bad := range []string{"0F766E", "#0F766", "#GGGGGG", "teal"} {
		if _, err := ContrastOnWhite(bad); err == nil {
			t.Errorf("%q read as a colour", bad)
		}
	}
	// The table is WCAG 2.2's linearisation, to the unit.
	for i, v := range linear {
		c := float64(i) / 255
		l := c / 12.92
		if c > 0.04045 {
			l = math.Pow((c+0.055)/1.055, 2.4)
		}
		if d := math.Abs(l*1e7 - float64(v)); d > 1 {
			t.Errorf("linear[%d] = %d, want %.1f", i, v, l*1e7)
		}
	}
}

// req: FR-TPL-04
func TestTheHashIsTheContentsCanonicalJSON(t *testing.T) {
	a := Default(KindDocument, Invoice)
	b := Default(KindDocument, Invoice)
	block(&b, BlockPaymentDetails).Texts = map[string]string{TextBody: a.Blocks[6].Texts[TextBody], TextLabel: a.Blocks[6].Texts[TextLabel]}
	ha, err := Hash(a)
	if err != nil {
		t.Fatal(err)
	}
	if hb, _ := Hash(b); !bytes.Equal(ha, hb) {
		t.Error("the same content hashes differently")
	}
	block(&b, BlockNotes).Shown = true
	if hb, _ := Hash(b); bytes.Equal(ha, hb) {
		t.Error("different content hashes the same")
	}
	raw, err := Canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Read(raw)
	if err != nil {
		t.Fatal(err)
	}
	if hc, _ := Hash(back); !bytes.Equal(ha, hc) {
		t.Error("content read back hashes differently")
	}
	empty := Default(KindDocument, Invoice)
	block(&empty, BlockNotes).Texts = map[string]string{TextLabel: "Notes"}
	block(&empty, BlockText).Texts = nil
	if he, _ := Hash(empty); !bytes.Equal(ha, he) {
		t.Error("content with its empty texts left out hashes differently")
	}
	if _, err := Read([]byte(`{"schema":"fyniq.template/1","script":"x"}`)); err == nil {
		t.Error("an unknown member was read")
	}
}
