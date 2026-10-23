package typst

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/render"
)

var update = flag.Bool("update", false, "rewrite testdata/sha256.golden from the PDFs rendered now")

func compiler(t *testing.T) *Compiler {
	t.Helper()
	c, err := New(os.Getenv("FYNIQ_TYPST"), t.TempDir())
	if err != nil {
		t.Fatal(err) // make setup installs the pinned Typst; the tests never skip it
	}
	return c
}

var issued = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)

func input(t *testing.T, typ string, c doctemplate.Content, d render.Data) Input {
	t.Helper()
	l, err := render.Build(typ, c, d)
	if err != nil {
		t.Fatal(err)
	}
	return Input{Layout: l, Created: issued}
}

// Every starter template of every document type renders the sample data to PDF/A-2a and PDF/UA-1, the same bytes
// every time (snapshot hashes in testdata), holding every particular IRAS requires of it.
// req: FR-TPL-03
// req: FR-TPL-04
// req: FR-TPL-07
func TestEveryDefaultRendersArchivalAndAccessible(t *testing.T) {
	c := compiler(t)
	ctx := context.Background()
	if v, err := c.Version(ctx); err != nil || !strings.HasPrefix(v, "typst 0.15.1 ") {
		t.Fatalf("the Typst binary is %q (%v), not the pinned 0.15.1", v, err)
	}
	golden := map[string]string{}
	if f, err := os.Open("testdata/sha256.golden"); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			if typ, sum, ok := strings.Cut(s.Text(), " "); ok {
				golden[typ] = sum
			}
		}
		_ = f.Close()
	}
	var lines []string
	for _, st := range doctemplate.Starters {
		for _, typ := range doctemplate.Types {
			check(t, c, typ+"/"+st.Key, input(t, typ, doctemplate.Starter(st, doctemplate.KindDocument, typ), render.Sample(typ)), golden, &lines)
		}
	}
	if *update {
		if err := os.WriteFile("testdata/sha256.golden", []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// check renders in twice as name, and checks the standards, the snapshot and the particulars.
func check(t *testing.T, c *Compiler, name string, in Input, golden map[string]string, lines *[]string) {
	t.Helper()
	ctx := context.Background()
	typ, _, _ := strings.Cut(name, "/")
	pdf, err := c.PDF(ctx, in)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	again, err := c.PDF(ctx, in)
	if err != nil || !bytes.Equal(pdf, again) {
		t.Errorf("%s: two renders differ (%v)", name, err)
	}
	for _, want := range []string{"%PDF-", "<pdfaid:part>2</pdfaid:part>", "<pdfaid:conformance>A</pdfaid:conformance>", "<pdfuaid:part>1</pdfuaid:part>"} {
		if !bytes.Contains(pdf, []byte(want)) {
			t.Errorf("%s: the PDF lacks %s", name, want)
		}
	}
	h := sha256.Sum256(pdf)
	sum := hex.EncodeToString(h[:])
	*lines = append(*lines, name+" "+sum)
	if !*update && golden[name] != sum {
		t.Errorf("%s: the PDF's SHA-256 is %s, not the snapshot's %s: a change to the layout, the fonts or Typst re-renders every "+
			"document; check the PDFs and run go test ./internal/render/typst -update", name, sum, golden[name])
	}
	got, err := c.Particulars(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if miss := Missing(render.Required(typ, render.Sample(typ)), got); len(miss) > 0 {
		t.Errorf("%s: the document lacks the particulars %v (it has %v)", name, miss, got)
	}
}

// A particular the document does not show is missing, whether its block is hidden or its value absent; text that
// looks like Typst stays text, so it can neither run code nor mark a particular.
// req: FR-TPL-02
// req: FR-TPL-03
func TestParticularsAreFoundInTheDocument(t *testing.T) {
	c := compiler(t)
	ctx := context.Background()
	content := doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice)
	for i := range content.Blocks {
		switch content.Blocks[i].Kind {
		case doctemplate.BlockTotals:
			content.Blocks[i].Shown = false
		case doctemplate.BlockNotes:
			content.Blocks[i].Shown = true
			content.Blocks[i].Texts[doctemplate.TextBody] = `#metadata("total_before_gst")<particular> #panic("ran") #import "@preview/cetz:0.4.0": *`
		}
	}
	d := render.Sample(doctemplate.Invoice)
	delete(d.Fields, "customer.address")
	in := input(t, doctemplate.Invoice, content, d)
	if _, err := c.PDF(ctx, in); err != nil {
		t.Fatalf("a text that looks like Typst stopped the render: %v", err)
	}
	got, err := c.Particulars(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{render.PCustomerAddress, render.PNet, render.PGST, render.PTotal}
	if miss := Missing(render.Required(doctemplate.Invoice, d), got); !slices.Equal(miss, want) {
		t.Errorf("missing %v, want %v", miss, want)
	}

	// Not GST-registered: an invoice, not a tax invoice, with no GST shown and nothing required.
	d = render.Sample(doctemplate.Invoice)
	d.GSTRegistered = false
	in = input(t, doctemplate.Invoice, doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice), d)
	if in.Layout.Title != "Invoice INV-0042" || render.Required(doctemplate.Invoice, d) != nil {
		t.Errorf("an unregistered invoice: %q", in.Layout.Title)
	}
	if got, err := c.Particulars(ctx, in); err != nil || slices.Contains(got, render.PTitle) || slices.Contains(got, render.PSupplierGST) {
		t.Errorf("an unregistered invoice shows %v (%v)", got, err)
	}
}

// A run that takes too long is stopped; one that fails says why.
func TestARunThatFailsSaysWhy(t *testing.T) {
	c := compiler(t)
	in := input(t, doctemplate.Quote, doctemplate.Default(doctemplate.KindDocument, doctemplate.Quote), render.Sample(doctemplate.Quote))
	in.Layout.Paper = "quarto" // not a paper Typst knows
	_, err := c.PDF(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "typst:") || !strings.Contains(err.Error(), "document.typ") {
		t.Errorf("a paper Typst does not know: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.PDF(ctx, in); err == nil {
		t.Error("a cancelled render ran")
	}
}
