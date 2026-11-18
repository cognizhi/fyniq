package pdftext_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/pdftext"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/render/typst"
)

func compiler(t *testing.T) *typst.Compiler {
	t.Helper()
	c, err := typst.New(os.Getenv("FYNIQ_TYPST"), t.TempDir())
	if err != nil {
		t.Fatal(err) // make setup installs the pinned Typst; the tests never skip it
	}
	return c
}

// A report printed through the pinned Typst reads back as it was written: its heading on the first page, and each row's
// label and figures in order, an accented name, a minus sign and a figure in parentheses among them; and a second page's
// text comes after the first's. What is not a PDF is refused.
// req: QA-RPT-03
func TestAPrintedReportReadsBack(t *testing.T) {
	rows := []render.ReportRow{{Kind: render.RowSection, Label: "Revenue"},
		{Kind: render.RowLine, Code: "4010", Label: "Subscription revenue", Cells: []string{"5,000.00", "4,250.00"}},
		{Kind: render.RowLine, Code: "4020", Label: "Café services", Cells: []string{"−1,234.56", "(10.00)"}},
		{Kind: render.RowTotal, Label: "Total revenue", Cells: []string{"3,765.44", "4,240.00"}}}
	for i := range 60 { // enough for a second page
		rows = append(rows, render.ReportRow{Kind: render.RowLine, Code: "6" + strings.Repeat("0", 2) + string(rune('0'+i%10)),
			Label: "Expense line " + string(rune('A'+i%26)), Cells: []string{"1.00", "2.00"}})
	}
	rows = append(rows, render.ReportRow{Kind: render.RowMeasure, Label: "Net profit", Cells: []string{"3,705.44", "4,120.00"}})
	r := render.Report{Title: "Profit and loss", Author: "Cognizhi Pte Ltd", Lines: []string{"Management statements", "Amounts in SGD"},
		Label: "SGD", Columns: []string{"Oct 2026", "Sep 2026"}, Rows: rows, Footer: "Read at ledger revision 42"}
	pdf, err := compiler(t).PDF(context.Background(), typst.Input{Report: &r, Created: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := pdftext.Pages(pdf)
	if err != nil || len(pages) < 2 {
		t.Fatalf("%d pages (%v)", len(pages), err)
	}
	text := strings.Join(pages, "\n")
	for _, want := range []string{"Profit and loss", "Cognizhi Pte Ltd", "Management statements", "Amounts in SGD", "Read at ledger revision 42"} {
		if !strings.Contains(pages[0], want) {
			t.Errorf("%q is not on the first page:\n%s", want, pages[0])
		}
	}
	last := -1
	for _, want := range []string{"Oct 2026", "Sep 2026", "Subscription revenue", "5,000.00", "4,250.00", "Café services", "−1,234.56", "(10.00)",
		"Total revenue", "3,765.44", "Net profit", "3,705.44", "4,120.00"} {
		i := strings.Index(text, want)
		if i < 0 {
			t.Errorf("%q is not in the text:\n%s", want, text)
			continue
		}
		if i < last {
			t.Errorf("%q comes before what was printed above it", want)
		}
		last = i
	}
	if !strings.Contains(pages[len(pages)-1], "Net profit") || strings.Contains(pages[0], "Net profit") {
		t.Errorf("the last row is not on the last page")
	}
	for _, bad := range [][]byte{nil, []byte("<html>"), []byte("%PDF-1.7\n")} {
		if _, err := pdftext.Pages(bad); err == nil {
			t.Errorf("%q read as a PDF", bad)
		}
	}
}

// An invoice printed from its starter template reads back with its particulars and its total.
// req: QA-RPT-03
func TestAPrintedInvoiceReadsBack(t *testing.T) {
	l, err := render.Build(doctemplate.Invoice, doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice), render.Sample(doctemplate.Invoice))
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := compiler(t).PDF(context.Background(), typst.Input{Layout: l, Created: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	text, err := pdftext.Text(pdf)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Example Analytics Pte Ltd", "202400001A"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not in the invoice's text:\n%s", want, text)
		}
	}
}
