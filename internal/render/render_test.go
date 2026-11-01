package render

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// req: FR-TPL-02
func TestFieldsAreFormatted(t *testing.T) {
	m := func(s, c string) money.Money { x, _ := money.Parse(s, money.MustLookup(c)); return x }
	for _, c := range []struct{ got, want string }{
		{FormatMoney(m("1234567.5", "SGD"), "code"), "SGD 1,234,567.50"},
		{FormatMoney(m("1234.5", "SGD"), "symbol"), "S$1,234.50"},
		{FormatMoney(m("-1234.5", "USD"), "symbol"), "−US$1,234.50"},
		{FormatMoney(m("-0.5", "SGD"), "plain"), "−0.50"},
		{FormatMoney(m("999", "JPY"), "symbol"), "¥999"},
		{FormatMoney(m("12.345", "KWD"), "symbol"), "KWD 12.345"}, // no symbol: the code
		{FormatMoney(m("100", "SGD"), ""), "SGD 100.00"},
		{FormatDate(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), "medium"), "3 Oct 2026"},
		{FormatDate(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), "long"), "3 October 2026"},
		{FormatDate(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), "short"), "03/10/2026"},
		{FormatDate(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), "iso"), "2026-10-03"},
		{FormatDate(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), ""), "3 Oct 2026"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	n := int64(1200)
	if s, _ := Format(Value{Number: &n}, ""); s != "1,200" {
		t.Errorf("a number: %q", s)
	}
	if _, err := Format(Value{Date: "23/10/2026"}, ""); err == nil {
		t.Error("a date that is not YYYY-MM-DD was read")
	}
	if _, err := Format(Value{Amount: &Amount{Amount: "1.234", Currency: "SGD"}}, ""); err == nil {
		t.Error("an amount beyond its currency's scale was read")
	}
}

func find(l Layout, kind string) *Block {
	for i := range l.Blocks {
		if l.Blocks[i].Kind == kind {
			return &l.Blocks[i]
		}
	}
	return nil
}

// req: FR-TPL-01
// req: FR-TPL-03
func TestTheLayoutFollowsTheTemplate(t *testing.T) {
	c := doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice)
	for i := range c.Blocks {
		switch c.Blocks[i].Kind {
		case doctemplate.BlockPaymentDetails:
			c.Blocks[i].Texts[doctemplate.TextBody] = "Pay {{document.amount_due|symbol}} by {{document.due_date|long}}; {{document.unknown}}."
		case doctemplate.BlockSignature:
			c.Blocks[i].Shown = true
		}
	}
	l, err := Build(doctemplate.Invoice, c, Sample(doctemplate.Invoice))
	if err != nil {
		t.Fatal(err)
	}
	if l.Title != "Tax Invoice INV-0042" || l.Paper != "a4" || l.Margins != [4]int{20, 18, 20, 18} || l.Font != "Inter" || l.Lang != "en" {
		t.Errorf("the layout: %+v", l)
	}
	var kinds []string
	for _, b := range l.Blocks {
		kinds = append(kinds, b.Kind)
	}
	if want := []string{"header", "our_details", "recipient", "document_details", "line_items", "totals", "payment_details", "signature", "footer"}; !slices.Equal(kinds, want) {
		t.Errorf("the shown blocks are %v, want %v", kinds, want)
	}
	if b := find(l, "payment_details"); b.Body != "Pay S$1,635.00 by 22 November 2026; ." {
		t.Errorf("the payment text: %q", b.Body)
	}
	if b := find(l, "header"); b.Items[0] != (Item{Text: "Tax Invoice", Particular: PTitle}) || b.Logo {
		t.Errorf("the header: %+v", b)
	}
	tot := find(l, "totals")
	var rows []string
	for _, r := range tot.Rows {
		rows = append(rows, r.Label+"="+r.Value+strings.Join(r.Particulars, "+"))
	}
	if want := "Total before GST=SGD 1,500.00total_before_gst|GST 9%=SGD 135.00gst_rate+gst_total|Total with GST=SGD 1,635.00total_with_gst|Amount due=SGD 1,635.00"; strings.Join(rows, "|") != want {
		t.Errorf("the totals: %s", strings.Join(rows, "|"))
	}
	lines := find(l, "line_items")
	if len(lines.Columns) != 5 || lines.Columns[4].Title != "Amount SGD" || !slices.Equal(lines.Cells[0], []string{"Data platform subscription, October 2026", "1", "1,000.00", "9%", "1,000.00"}) ||
		!slices.Equal(lines.Particulars, []string{PDescription, PQuantity, PRate}) {
		t.Errorf("the lines: %+v", lines)
	}

	// A foreign-currency tax invoice shows its totals in SGD; a simplified one says the price includes GST.
	d := Sample(doctemplate.Invoice)
	d.Fields["document.currency"] = Value{Text: "USD"}
	d.Fields["document.subtotal_sgd"], d.Fields["document.gst_total_sgd"], d.Fields["document.total_sgd"] = sgd("1950.00"), sgd("175.50"), sgd("2125.50")
	l, _ = Build(doctemplate.Invoice, doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice), d)
	if r := find(l, "totals").Rows; r[len(r)-3].Label != "Total before GST in SGD" || !slices.Equal(r[len(r)-3].Particulars, []string{PSGD}) ||
		!slices.Contains(Required(doctemplate.Invoice, d), PSGD) {
		t.Errorf("the SGD equivalents: %+v", r)
	}
	d = Sample(doctemplate.Invoice)
	d.Simplified = true
	l, _ = Build(doctemplate.Invoice, doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice), d)
	if tot := find(l, "totals"); tot.Note == nil || tot.Note.Particular != PIncludesGST || tot.Rows[0].Label != "Total with GST" {
		t.Errorf("a simplified tax invoice: %+v", tot)
	}
	if want := []string{PSupplierName, PSupplierAddress, PSupplierGST, PNumber, PDate, PDescription, PTotal, PIncludesGST}; !slices.Equal(Required(doctemplate.Invoice, d), want) {
		t.Errorf("a simplified tax invoice requires %v", Required(doctemplate.Invoice, d))
	}

	// The footer leaves out a part whose fields are all empty: an organisation with no UEN shows its name alone, not a
	// dangling "· UEN"; text without a field always stays.
	d = Sample(doctemplate.Invoice)
	if l, _ = Build(doctemplate.Invoice, doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice), d); find(l, "footer").Body != "Example Analytics Pte Ltd · UEN 202400001A" {
		t.Errorf("the footer with a UEN: %q", find(l, "footer").Body)
	}
	delete(d.Fields, "organisation.uen")
	if l, _ = Build(doctemplate.Invoice, doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice), d); find(l, "footer").Body != "Example Analytics Pte Ltd" {
		t.Errorf("the footer without a UEN: %q", find(l, "footer").Body)
	}
	thanks := doctemplate.Default(doctemplate.KindDocument, doctemplate.Invoice)
	for i := range thanks.Blocks {
		if thanks.Blocks[i].Kind == doctemplate.BlockFooter {
			thanks.Blocks[i].Texts[doctemplate.TextBody] = "Thank you · UEN {{organisation.uen}} · {{organisation.email}}"
		}
	}
	if l, _ = Build(doctemplate.Invoice, thanks, d); find(l, "footer").Body != "Thank you · accounts@example.com" {
		t.Errorf("a footer of text and fields: %q", find(l, "footer").Body)
	}

	// A purchase order is addressed to its supplier, whose details carry no customer particular.
	l, _ = Build(doctemplate.PurchaseOrder, doctemplate.Default(doctemplate.KindDocument, doctemplate.PurchaseOrder), Sample(doctemplate.PurchaseOrder))
	if r := find(l, "recipient"); r.Label != "Supplier" || r.Items[0] != (Item{Text: "Cloud Tools Pte Ltd"}) || Required(doctemplate.PurchaseOrder, Sample(doctemplate.PurchaseOrder)) != nil {
		t.Errorf("a purchase order's recipient: %+v", r)
	}
	bad := doctemplate.Default(doctemplate.KindDocument, doctemplate.Quote)
	bad.Theme.Font = "Comic Sans"
	if _, err := Build(doctemplate.Quote, bad, Sample(doctemplate.Quote)); err == nil {
		t.Error("a font the service does not ship was laid out")
	}
	if _, err := Build(doctemplate.Quote, doctemplate.Default(doctemplate.KindEmail, doctemplate.Quote), Sample(doctemplate.Quote)); err == nil {
		t.Error("an email template was laid out as a document")
	}
}

// A report is checked before the render service lays it out: a title, its columns and rows within bounds, each row of a
// known kind with a figure per column, and no text longer than 500 characters.
// req: FR-RPT-05
func TestAReportIsCheckedBeforeItIsLaidOut(t *testing.T) {
	good := func() Report {
		return Report{Title: "Profit and loss", Columns: []string{"Oct 2026"}, Rows: []ReportRow{{Kind: RowSection, Label: "Revenue"},
			{Kind: RowLine, Code: "4010", Label: "Sales", Cells: []string{"10.00"}}, {Kind: RowMeasure, Label: "Net profit", Cells: []string{"10.00"}}}}
	}
	if err := good().Check(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(r *Report){
		"no title":          func(r *Report) { r.Title = " " },
		"no columns":        func(r *Report) { r.Columns = nil },
		"too many columns":  func(r *Report) { r.Columns = make([]string, MaxReportColumns+1) },
		"too many rows":     func(r *Report) { r.Rows = make([]ReportRow, MaxReportRows+1) },
		"a section's cells": func(r *Report) { r.Rows[0].Cells = []string{"1"} },
		"a missing figure":  func(r *Report) { r.Rows[1].Cells = nil },
		"an unknown kind":   func(r *Report) { r.Rows[2].Kind = "chart" },
		"a long note":       func(r *Report) { r.Notes = []ReportNote{{Title: "Checks", Items: []string{strings.Repeat("x", 501)}}} },
	} {
		r := good()
		change(&r)
		if err := r.Check(); !errors.Is(err, ErrReport) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
