package invoicenow_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/invoicenow"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

var (
	sgd = money.MustLookup("SGD")
	usd = money.MustLookup("USD")
)

func m(t *testing.T, s string, c money.Currency) money.Money {
	t.Helper()
	v, err := money.Parse(s, c)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func seller() invoicenow.Party {
	return invoicenow.Party{Name: "Cognizhi Pte. Ltd.", UEN: "202012345K", GST: "202012345K",
		Address: invoicenow.Address{Line1: "1 Raffles Place", City: "Singapore", PostCode: "048616", Country: "SG"}}
}

func buyer() invoicenow.Party {
	return invoicenow.Party{Name: "Lumen Health Pte. Ltd.", UEN: "201912345A",
		Address: invoicenow.Address{Line1: "10 Anson Road", City: "Singapore", PostCode: "079903", Country: "SG"}}
}

// An SGD invoice worked by hand: 10 hours at S$100.00, standard-rated at 9%, is S$1,000.00 and S$90.00 of GST; an export
// service of S$500.00 is zero-rated. The total before GST is S$1,500.00, the GST S$90.00, the total S$1,590.00.
func sgdInvoice(t *testing.T) invoicenow.Document {
	return invoicenow.Document{Kind: invoicenow.Invoice, Number: "INV-2027-0007", UUID: "0a1b2c3d-1111-4222-8333-444455556666",
		Issued: time.Date(2027, 2, 3, 0, 0, 0, 0, time.UTC), Due: time.Date(2027, 3, 5, 0, 0, 0, 0, time.UTC), Currency: sgd,
		Seller: seller(), Buyer: buyer(),
		Lines: []invoicenow.Line{
			{ID: "1", Name: "Consulting", Quantity: "10", Unit: "HUR", NetPrice: "100.00", Net: m(t, "1000.00", sgd), Category: "SR", Rate: "9"},
			{ID: "2", Name: "Export service", Quantity: "1", Unit: "C62", NetPrice: "500.00", Net: m(t, "500.00", sgd), Category: "ZR", Rate: "0"},
		},
		Tax: []invoicenow.TaxSubtotal{
			{Category: "SR", Rate: "9", Taxable: m(t, "1000.00", sgd), Tax: m(t, "90.00", sgd)},
			{Category: "ZR", Rate: "0", Taxable: m(t, "500.00", sgd), Tax: m(t, "0.00", sgd)},
		},
		Net: m(t, "1500.00", sgd), TaxTotal: m(t, "90.00", sgd), Gross: m(t, "1590.00", sgd)}
}

// A document in US dollars: 100.00 USD at 9% is 9.00 USD; at 1.35 SGD to the dollar the totals are S$135.00 before GST,
// S$12.15 of GST and S$147.15 in all (BR-113-GST-SG, the sgdtotal references).
func usdInvoice(t *testing.T) invoicenow.Document {
	d := sgdInvoice(t)
	d.Currency = usd
	d.Lines = []invoicenow.Line{{ID: "1", Name: "Licence", Quantity: "1", Unit: "C62", NetPrice: "100.00", Net: m(t, "100.00", usd), Category: "SR", Rate: "9"}}
	d.Tax = []invoicenow.TaxSubtotal{{Category: "SR", Rate: "9", Taxable: m(t, "100.00", usd), Tax: m(t, "9.00", usd)}}
	d.Net, d.TaxTotal, d.Gross = m(t, "100.00", usd), m(t, "9.00", usd), m(t, "109.00", usd)
	tax, net, gross := m(t, "12.15", sgd), m(t, "135.00", sgd), m(t, "147.15", sgd)
	d.SGDTax, d.SGDNet, d.SGDGross = &tax, &net, &gross
	return d
}

func rules(fs []invoicenow.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	slices.Sort(out)
	return out
}

// req: FR-AR-12
func TestAnSGDInvoiceBuildsToTheGoldenDocument(t *testing.T) {
	got, err := invoicenow.Build(sgdInvoice(t))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/invoice-sgd.xml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("the document differs from testdata/invoice-sgd.xml:\n%s", got)
	}
	// the identifiers PINT-SG v1.4.1 requires, and the order of its root elements: the UUID follows the number, the type
	// code follows the dates, the currency follows the type, the parties follow the currency
	s := string(got)
	for _, must := range []string{"<cbc:CustomizationID>urn:peppol:pint:billing-1@sg-1</cbc:CustomizationID>", "<cbc:ProfileID>urn:peppol:bis:billing</cbc:ProfileID>",
		`<cbc:EndpointID schemeID="0195">SGUEN201912345A</cbc:EndpointID>`, "<cbc:InvoiceTypeCode>380</cbc:InvoiceTypeCode>"} {
		if !strings.Contains(s, must) {
			t.Errorf("the document lacks %s", must)
		}
	}
	order := []string{"cbc:CustomizationID", "cbc:ProfileID", "cbc:ID>", "cbc:UUID", "cbc:IssueDate", "cbc:DueDate", "cbc:InvoiceTypeCode", "cbc:DocumentCurrencyCode",
		"cac:AccountingSupplierParty", "cac:AccountingCustomerParty", "cac:TaxTotal", "cac:LegalMonetaryTotal", "cac:InvoiceLine"}
	last := -1
	for _, tag := range order {
		i := strings.Index(s, "<"+tag)
		if i < 0 || i < last {
			t.Fatalf("%s is missing or out of order in the document", tag)
		}
		last = i
	}
	if strings.Contains(s, "TaxCurrencyCode") || strings.Contains(s, "sgdtotal") {
		t.Error("an SGD invoice gives no accounting currency (BR-113-GST-SG)")
	}
}

// req: FR-AR-12
func TestAForeignCurrencyInvoiceStatesItsTotalsInSGD(t *testing.T) {
	got, err := invoicenow.Build(usdInvoice(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, must := range []string{"<cbc:TaxCurrencyCode>SGD</cbc:TaxCurrencyCode>", "<cbc:DocumentTypeCode>sgdtotal-excl-gst</cbc:DocumentTypeCode>",
		"<cbc:DocumentDescription>135.00</cbc:DocumentDescription>", "<cbc:DocumentTypeCode>sgdtotal-incl-gst</cbc:DocumentTypeCode>",
		"<cbc:DocumentDescription>147.15</cbc:DocumentDescription>", `<cbc:TaxAmount currencyID="SGD">12.15</cbc:TaxAmount>`} {
		if !strings.Contains(s, must) {
			t.Errorf("the document lacks %s", must)
		}
	}
}

// req: FR-AR-12
func TestACreditNoteIsACreditNoteDocumentWithItsReason(t *testing.T) {
	d := sgdInvoice(t)
	d.Kind, d.Number, d.Preceded, d.Note = invoicenow.CreditNote, "CN-2027-0001", "INV-2027-0007", "Hours overstated"
	got, err := invoicenow.Build(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, must := range []string{"<CreditNote ", "<cbc:CreditNoteTypeCode>381</cbc:CreditNoteTypeCode>", "<cbc:Note>Hours overstated</cbc:Note>",
		"<cac:BillingReference>", "<cbc:CreditedQuantity unitCode=\"HUR\">10</cbc:CreditedQuantity>", "<cac:CreditNoteLine>"} {
		if !strings.Contains(s, must) {
			t.Errorf("the credit note lacks %s", must)
		}
	}
	if strings.Contains(s, "InvoicedQuantity") || strings.Contains(s, "<cbc:DueDate>") {
		t.Error("a credit note has credited quantities and no due date")
	}
	d.Note = ""
	if _, err := invoicenow.Build(d); err == nil || !slices.Contains(rules(refused(t, err)), "BR-111-GST-SG") {
		t.Errorf("a credit note without its reason must break BR-111-GST-SG: %v", err)
	}
}

func refused(t *testing.T, err error) []invoicenow.Finding {
	t.Helper()
	var r *invoicenow.RefusedError
	if !errors.As(err, &r) {
		t.Fatalf("want a RefusedError, got %v", err)
	}
	return r.Findings
}

// Each rule is broken by one change to a good document, and only that rule is reported (no document leaves while it
// breaks one).
// req: FR-AR-12
func TestEachRuleRefusesItsDocument(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*testing.T, *invoicenow.Document)
		want   string
	}{
		"no seller GST":      {func(_ *testing.T, d *invoicenow.Document) { d.Seller.GST = "" }, "BR-105-GST-SG"},
		"no seller postcode": {func(_ *testing.T, d *invoicenow.Document) { d.Seller.Address.PostCode = "" }, "BR-106-GST-SG"},
		"no buyer address":   {func(_ *testing.T, d *invoicenow.Document) { d.Buyer.Address.Line1 = "" }, "BR-107-GST-SG"},
		"no UUID":            {func(_ *testing.T, d *invoicenow.Document) { d.UUID = "" }, "BR-108-GST-SG"},
		"a bad UUID":         {func(_ *testing.T, d *invoicenow.Document) { d.UUID = "not-a-uuid" }, "BR-109-GST-SG"},
		"no buyer country":   {func(_ *testing.T, d *invoicenow.Document) { d.Buyer.Address.Country = "" }, "BR-11-SG"},
		"no seller UEN":      {func(_ *testing.T, d *invoicenow.Document) { d.Seller.UEN = "" }, "BR-112-GST-SG"},
		"no lines":           {func(_ *testing.T, d *invoicenow.Document) { d.Lines = nil }, "IBR-016"},
		"a category unknown": {func(_ *testing.T, d *invoicenow.Document) { d.Lines[0].Category = "XX" }, "BR-47-GST-SG"},
		"a line sum off": {func(t *testing.T, d *invoicenow.Document) {
			d.Net = m(t, "1500.01", sgd)
		}, "BR-CO-10"},
		"the GST off": {func(t *testing.T, d *invoicenow.Document) {
			d.Tax[0].Tax = m(t, "91.00", sgd)
		}, "BR-CO-17-GST-SG"},
		"the total off": {func(t *testing.T, d *invoicenow.Document) {
			d.Gross = m(t, "1600.00", sgd)
		}, "BR-CO-15"},
		"SGD totals on an SGD invoice": {func(t *testing.T, d *invoicenow.Document) {
			x := m(t, "1.00", sgd)
			d.SGDTax = &x
		}, "BR-113-GST-SG"},
	} {
		t.Run(name, func(t *testing.T) {
			d := sgdInvoice(t)
			tc.change(t, &d)
			if _, err := invoicenow.Build(d); err == nil {
				t.Fatal("the document was built")
			}
			if got := rules(invoicenow.Check(d)); !slices.Contains(got, tc.want) {
				t.Fatalf("want %s among %v", tc.want, got)
			}
		})
	}
	// a foreign invoice with no SGD totals
	d := usdInvoice(t)
	d.SGDTax, d.SGDNet, d.SGDGross = nil, nil, nil
	if got := rules(invoicenow.Check(d)); !slices.Equal(got, []string{"BR-113-GST-SG"}) {
		t.Errorf("a USD invoice with no SGD totals breaks BR-113-GST-SG only: %v", got)
	}
	// a good document breaks nothing, in either currency
	if f := invoicenow.Check(sgdInvoice(t)); len(f) != 0 {
		t.Errorf("a good SGD invoice: %v", f)
	}
	if f := invoicenow.Check(usdInvoice(t)); len(f) != 0 {
		t.Errorf("a good USD invoice: %v", f)
	}
}

// The GST of a breakdown may differ from the exact product by up to the smallest unit of the currency, the rounding of
// a subtotal (RND-GST-EXCL), and by no more.
// req: FR-AR-12
func TestTheGSTMayDifferByTheRoundingOfASubtotalAndNoMore(t *testing.T) {
	d := sgdInvoice(t)
	// 33.33 at 9% is 2.9997: S$3.00 rounded, S$2.99 a cent short, S$2.98 two
	d.Lines = []invoicenow.Line{{ID: "1", Name: "Item", Quantity: "1", Unit: "C62", NetPrice: "33.33", Net: m(t, "33.33", sgd), Category: "SR", Rate: "9"}}
	d.Net = m(t, "33.33", sgd)
	for tax, ok := range map[string]bool{"3.00": true, "2.99": true, "2.98": false, "3.02": false} {
		d.Tax = []invoicenow.TaxSubtotal{{Category: "SR", Rate: "9", Taxable: m(t, "33.33", sgd), Tax: m(t, tax, sgd)}}
		d.TaxTotal = m(t, tax, sgd)
		g, _ := d.Net.Add(d.TaxTotal)
		d.Gross = g
		if got := len(invoicenow.Check(d)) == 0; got != ok {
			t.Errorf("GST %s on 33.33 at 9%%: accepted %v, want %v: %v", tax, got, ok, invoicenow.Check(d))
		}
	}
}

// What is built is read back as a draft bill's fields (FR-AP-05); a document that is not PINT-SG, not XML or not an
// invoice is refused with its reason.
func TestABuiltDocumentIsReadBack(t *testing.T) {
	b, err := invoicenow.Build(sgdInvoice(t))
	if err != nil {
		t.Fatal(err)
	}
	r, err := invoicenow.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != invoicenow.Invoice || r.Number != "INV-2027-0007" || r.UUID != "0a1b2c3d-1111-4222-8333-444455556666" || r.Issued != "2027-02-03" || r.Due != "2027-03-05" ||
		r.Currency != "SGD" || r.Net != "1500.00" || r.TaxTotal != "90.00" || r.Gross != "1590.00" {
		t.Errorf("the header is read as %+v", r)
	}
	if r.Seller.Name != "Cognizhi Pte. Ltd." || r.Seller.EndpointScheme != "0195" || r.Seller.EndpointID != "SGUEN202012345K" || r.Seller.GST != "202012345K" || r.Seller.PostCode != "048616" || r.Seller.Country != "SG" {
		t.Errorf("the sender is read as %+v", r.Seller)
	}
	if len(r.Lines) != 2 || r.Lines[0].Name != "Consulting" || r.Lines[0].Quantity != "10" || r.Lines[0].Unit != "HUR" || r.Lines[0].Net != "1000.00" ||
		r.Lines[0].Category != "SR" || r.Lines[0].Rate != "9" || r.Lines[1].Category != "ZR" {
		t.Errorf("the lines are read as %+v", r.Lines)
	}
	d := sgdInvoice(t)
	d.Kind, d.Preceded, d.Note = invoicenow.CreditNote, "INV-2027-0007", "x"
	cb, err := invoicenow.Build(d)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := invoicenow.Parse(cb); err != nil || c.Kind != invoicenow.CreditNote || len(c.Lines) != 2 || c.Lines[0].Quantity != "10" {
		t.Errorf("a credit note is read as %+v, %v", c, err)
	}
	d.Kind = invoicenow.DebitNote
	db, _ := invoicenow.Build(d)
	if c, err := invoicenow.Parse(db); err != nil || c.Kind != invoicenow.DebitNote {
		t.Errorf("a debit note is read as %+v, %v", c, err)
	}
}

func TestADocumentThatIsNotPINTSGIsRefused(t *testing.T) {
	good, _ := invoicenow.Build(sgdInvoice(t))
	for name, b := range map[string][]byte{
		"empty":                 nil,
		"not XML":               []byte("not xml"),
		"another root":          []byte(`<Order xmlns="urn:x"><ID>1</ID></Order>`),
		"another specification": []byte(strings.Replace(string(good), "urn:peppol:pint:billing-1@sg-1", "urn:cen.eu:en16931:2017", 1)),
		"no number":             []byte(strings.Replace(string(good), "<cbc:ID>INV-2027-0007</cbc:ID>", "", 1)),
		"an external entity":    []byte(`<?xml version="1.0"?><!DOCTYPE d [<!ENTITY x SYSTEM "file:///etc/passwd">]><Invoice><ID>&x;</ID></Invoice>`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := invoicenow.Parse(b)
			var pe *invoicenow.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("want a ParseError, got %v", err)
			}
		})
	}
}

// A document in US dollars states its GST twice, in dollars and in Singapore dollars: the received GST total is the
// document's own.
func TestAForeignCurrencyDocumentIsReadInItsOwnCurrency(t *testing.T) {
	b, err := invoicenow.Build(usdInvoice(t))
	if err != nil {
		t.Fatal(err)
	}
	r, err := invoicenow.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Currency != "USD" || r.TaxTotal != "9.00" || r.Net != "100.00" || r.Gross != "109.00" {
		t.Errorf("the totals are read as %+v", r)
	}
}
