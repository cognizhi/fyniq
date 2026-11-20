// Package invoicenow builds, checks and reads InvoiceNow documents: Singapore's PINT-SG profile of Peppol BIS Billing
// (FR-AR-12, FR-AP-05), and decides which documents of a GST return are still to be submitted (FR-TAX-10, FR-TAX-11).
//
// Sources (retrieved 20 Nov 2026), docs.peppol.eu/poac/sg/pint-sg/trn-invoice/, "PINT SG Billing v1.4.1", released
// 25 May 2026: the specification identifier (rule PEPPOL-EN16931-R004-SG), the element order of the document root, the
// electronic address scheme 0195 ("Singapore UEN identifier"), the GST-cat code list, the additional document references
// "sgdtotal-excl-gst" and "sgdtotal-incl-gst", and the Singapore rules BR-100-GST-SG to BR-113-GST-SG and BR-45-GST-SG to
// BR-48-GST-SG. Fyniq checks the rules it can state itself (Check), so a document the network would refuse never leaves;
// the access point's complete Schematron run is what [live-verify] covers.
package invoicenow

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The identifiers the PINT-SG rules require.
const (
	SpecificationID = "urn:peppol:pint:billing-1@sg-1" // rule PEPPOL-EN16931-R004-SG
	BusinessProcess = "urn:peppol:bis:billing"
	UENScheme       = "0195" // the electronic address scheme of a Singapore UEN
	TaxScheme       = "GST"
)

// Kind is the type of a document.
type Kind string

// The kinds of document: an invoice (type code 380), a credit note (a CreditNote document, 381) and a debit note (an
// Invoice document with type code 383).
const (
	Invoice    Kind = "invoice"
	CreditNote Kind = "credit_note"
	DebitNote  Kind = "debit_note"
)

// Categories are the GST categories of the GST-cat code list.
var Categories = []string{"SR", "SRCA-S", "SRCA-C", "SROVR-RS", "SROVR-LVG", "SRRC", "SRLVG", "ZR", "ES33", "ESN33", "DS", "OS", "NA", "NG"}

// taxed are the categories that make a GST-registered seller state its identifier, addresses and UUID (BR-105 to BR-108).
var taxed = []string{"SR", "SRCA-S", "SRCA-C", "ZR", "SRRC", "SROVR-RS", "SROVR-LVG", "SRLVG", "NA"}

// Address is a postal address.
type Address struct {
	Line1, Line2, City, PostCode string
	Country                      string // ISO 3166-1 alpha-2
}

// Party is the seller or the buyer. A Singapore business is its UEN; GST is its GST registration number.
type Party struct {
	Name    string
	UEN     string
	GST     string
	Address Address
	// Endpoint overrides the electronic address (scheme and value) when the party is on the network by another scheme.
	EndpointScheme, EndpointID string
}

// UENPrefix is what a Singapore participant ID of the UEN scheme starts its identifier with: the UEN 201912345A is the
// participant 0195:SGUEN201912345A (the form the contacts' Peppol ID takes, CT-08; [verify] with the access point).
const UENPrefix = "SGUEN"

func (p Party) endpoint() (string, string) {
	if p.EndpointID != "" {
		return p.EndpointScheme, p.EndpointID
	}
	if p.UEN == "" {
		return UENScheme, ""
	}
	return UENScheme, UENPrefix + p.UEN
}

// ParticipantID is the party's Peppol participant ID, scheme and identifier: where its documents are sent.
func (p Party) ParticipantID() string {
	sch, id := p.endpoint()
	if id == "" {
		return ""
	}
	return sch + ":" + id
}

// Line is an invoice line. Net is the line's amount before GST; Rate is the GST rate in percent, such as "9".
type Line struct {
	ID       string
	Name     string
	Quantity string // a decimal
	Unit     string // a UN/ECE Recommendation 20 code, such as C62 (one) or HUR (hour)
	NetPrice string // the unit price before GST, a decimal as the engine holds it
	Net      money.Money
	Category string
	Rate     string
}

// TaxSubtotal is the GST of one category and rate.
type TaxSubtotal struct {
	Category string
	Rate     string
	Taxable  money.Money
	Tax      money.Money
}

// Document is what a PINT-SG document is built from. Every amount is already computed by the engine; Build and Check
// verify them against one another and never recompute what the books say.
type Document struct {
	Kind     Kind
	Number   string
	UUID     string
	Issued   time.Time
	Due      time.Time // zero for none
	Currency money.Currency
	Note     string // a credit note's reason (BR-111-GST-SG)
	Preceded string // the number of the invoice a credit or debit note refers to
	Seller   Party
	Buyer    Party
	Lines    []Line
	Tax      []TaxSubtotal
	Net      money.Money // the lines' total, before GST
	TaxTotal money.Money
	Gross    money.Money // Net + TaxTotal
	// For a document in another currency than SGD (BR-113-GST-SG): the GST and the two totals in Singapore dollars.
	SGDTax, SGDNet, SGDGross *money.Money
}

// Finding is a rule a document breaks, with the field to fix (ENG-23).
type Finding struct {
	Rule    string
	Field   string
	Message string
}

func (f Finding) String() string { return f.Rule + " (" + f.Field + "): " + f.Message }

// RefusedError is a document the rules refuse: it is never queued.
type RefusedError struct{ Findings []Finding }

func (e *RefusedError) Error() string {
	parts := make([]string, len(e.Findings))
	for i, f := range e.Findings {
		parts[i] = f.String()
	}
	return "the document breaks " + fmt.Sprint(len(parts)) + " InvoiceNow rule(s): " + strings.Join(parts, "; ")
}

var (
	uuidForm = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	rateForm = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,4})?$`)
)

func decimalOf(s string) (*apd.Decimal, bool) {
	d, _, err := apd.NewFromString(s)
	return d, err == nil
}

func taxedCategory(c string) bool { return slices.Contains(taxed, c) }

// Check lists the rules the document breaks: the Singapore rules of PINT-SG v1.4.1 that Fyniq can state itself, and the
// arithmetic of EN 16931 they rest on (BR-CO-10, BR-CO-13, BR-CO-15, BR-CO-17-GST-SG). An empty list means Fyniq has no
// objection: the access point's validator has the last word.
func Check(d Document) []Finding {
	var out []Finding
	add := func(rule, field, msg string) { out = append(out, Finding{Rule: rule, Field: field, Message: msg}) }
	if d.Kind != Invoice && d.Kind != CreditNote && d.Kind != DebitNote {
		add("IBR-004", "kind", "the document is not an invoice, a credit note or a debit note")
	}
	if d.Number == "" {
		add("IBR-002", "number", "an invoice number is needed: issue the document first")
	}
	if d.Issued.IsZero() {
		add("IBR-003", "issued", "an issue date is needed")
	}
	if d.Currency.Code == "" {
		add("IBR-005", "currency", "a currency is needed")
	}
	if len(d.Lines) == 0 {
		add("IBR-016", "lines", "an invoice must have at least one line")
	}
	anyTaxed := false
	for _, t := range d.Tax {
		if taxedCategory(t.Category) {
			anyTaxed = true
		}
	}
	for _, l := range d.Lines {
		if taxedCategory(l.Category) {
			anyTaxed = true
		}
	}
	// the Singapore rules about a taxed supply
	if anyTaxed {
		if d.Seller.GST == "" {
			add("BR-105-GST-SG", "seller.gst", "the seller's GST registration number is needed when a line is standard-rated, zero-rated or has another taxed category")
		}
		if d.Seller.Address.Line1 == "" || d.Seller.Address.PostCode == "" {
			add("BR-106-GST-SG", "seller.address", "the seller's address line 1 and post code are needed")
		}
		if d.Buyer.Address.Line1 == "" || d.Buyer.Address.PostCode == "" {
			add("BR-107-GST-SG", "buyer.address", "the buyer's address line 1 and post code are needed")
		}
		if d.UUID == "" {
			add("BR-108-GST-SG", "uuid", "a unique document identifier (UUID) is needed")
		}
		if d.Seller.UEN == "" {
			add("BR-112-GST-SG", "seller.uen", "the seller's legal registration identifier (its UEN) is needed")
		}
		if d.Kind == CreditNote && strings.TrimSpace(d.Note) == "" {
			add("BR-111-GST-SG", "note", "a credit note gives its reason")
		}
	}
	if d.UUID != "" && !uuidForm.MatchString(d.UUID) {
		add("BR-109-GST-SG", "uuid", "the UUID must be written as 8-4-4-4-12 hexadecimal digits")
	}
	if d.Buyer.Address.Country == "" {
		add("BR-11-SG", "buyer.address.country", "the buyer's country code is needed")
	}
	if d.Seller.Address.Country == "" {
		add("IBR-009", "seller.address.country", "the seller's country code is needed")
	}
	if d.Seller.Name == "" || d.Buyer.Name == "" {
		add("IBR-006", "party.name", "the seller's and the buyer's names are needed")
	}
	if (d.Kind == CreditNote || d.Kind == DebitNote) && d.Preceded == "" && anyTaxed {
		add("IBR-055", "preceded", "a credit or debit note names the invoice it refers to")
	}
	if _, id := d.Buyer.endpoint(); id == "" {
		add("IBR-SR-27", "buyer.endpoint", "the buyer's electronic address (its UEN or Peppol ID) is needed")
	}
	// the accounting currency (BR-113-GST-SG)
	foreign := d.Currency.Code != "" && d.Currency.Code != "SGD"
	if foreign && anyTaxed && (d.SGDTax == nil || d.SGDNet == nil || d.SGDGross == nil) {
		add("BR-113-GST-SG", "sgd_totals", "the GST and the totals in Singapore dollars are needed when the invoice is in another currency")
	}
	if !foreign && (d.SGDTax != nil || d.SGDNet != nil || d.SGDGross != nil) {
		add("BR-113-GST-SG", "sgd_totals", "the accounting currency is not given when the invoice is in Singapore dollars")
	}
	for _, m := range []*money.Money{d.SGDTax, d.SGDNet, d.SGDGross} {
		if m != nil && m.Currency().Code != "SGD" {
			add("BR-100-GST-SG", "sgd_totals", "the totals in Singapore dollars must be in SGD")
		}
	}
	// lines
	lineSum, _ := money.Sum(d.Currency)
	for i, l := range d.Lines {
		f := fmt.Sprintf("lines[%d]", i)
		if l.ID == "" {
			add("IBR-021", f+".id", "each line has an identifier")
		}
		if l.Name == "" {
			add("IBR-025", f+".name", "each line has an item name")
		}
		if q, err := money.ParseQuantity(l.Quantity, f+".quantity"); err != nil || q.IsZero() && l.Quantity == "" {
			add("IBR-022", f+".quantity", "each line has a quantity")
		}
		if l.Unit == "" {
			add("IBR-023", f+".unit", "each line has a unit of measure code")
		}
		if !slices.Contains(Categories, l.Category) {
			add("BR-47-GST-SG", f+".category", fmt.Sprintf("%q is not a GST category of the GST-cat list", l.Category))
		}
		if taxedCategory(l.Category) && !rateForm.MatchString(l.Rate) {
			add("BR-48-GST-SG", f+".rate", "a taxed line has its GST rate in percent")
		}
		if l.Net.Currency() != d.Currency {
			add("IBR-024", f+".net", "the line's amount is in the document's currency")
			continue
		}
		if s, err := lineSum.Add(l.Net); err == nil {
			lineSum = s
		}
	}
	// totals
	if len(d.Lines) > 0 && d.Net.Currency() == d.Currency && !d.Net.Equal(lineSum) {
		add("BR-CO-10", "net", fmt.Sprintf("the total before GST %s is not the sum of the lines %s", d.Net, lineSum))
	}
	taxSum, _ := money.Sum(d.Currency)
	for i, t := range d.Tax {
		f := fmt.Sprintf("tax[%d]", i)
		if !slices.Contains(Categories, t.Category) {
			add("BR-47-GST-SG", f+".category", fmt.Sprintf("%q is not a GST category of the GST-cat list", t.Category))
		}
		if t.Taxable.Currency() != d.Currency || t.Tax.Currency() != d.Currency {
			add("BR-45-GST-SG", f, "the breakdown's amounts are in the document's currency")
			continue
		}
		if taxedCategory(t.Category) {
			if !rateForm.MatchString(t.Rate) {
				add("BR-48-GST-SG", f+".rate", "a taxed breakdown has its GST rate in percent")
			} else if !taxMatches(t) {
				add("BR-CO-17-GST-SG", f, fmt.Sprintf("the GST %s is not %s%% of %s", t.Tax, t.Rate, t.Taxable))
			}
		} else if !t.Tax.IsZero() {
			add("BR-CO-17-GST-SG", f, "a category that is not taxed carries no GST")
		}
		if s, err := taxSum.Add(t.Tax); err == nil {
			taxSum = s
		}
	}
	if len(d.Tax) > 0 && d.TaxTotal.Currency() == d.Currency && !d.TaxTotal.Equal(taxSum) {
		add("BR-CO-14", "tax_total", fmt.Sprintf("the GST total %s is not the sum of the breakdowns %s", d.TaxTotal, taxSum))
	}
	if d.Net.Currency() == d.Currency && d.TaxTotal.Currency() == d.Currency {
		if want, err := d.Net.Add(d.TaxTotal); err == nil && d.Gross.Currency() == d.Currency && !d.Gross.Equal(want) {
			add("BR-CO-15", "gross", fmt.Sprintf("the total with GST %s is not %s", d.Gross, want))
		}
	}
	if len(d.Tax) == 0 && len(d.Lines) > 0 {
		add("BR-45-GST-SG", "tax", "a GST breakdown is needed")
	}
	return out
}

// taxMatches says whether the breakdown's GST is its taxable amount times the rate, to the currency's smallest unit
// (BR-CO-17-GST-SG; the exact tolerance of the network's validator is [live-verify]).
func taxMatches(t TaxSubtotal) bool {
	rate, ok := decimalOf(t.Rate)
	if !ok {
		return false
	}
	var want apd.Decimal
	c := apd.BaseContext.WithPrecision(40)
	if _, err := c.Mul(&want, t.Taxable.Decimal(), rate); err != nil {
		return false
	}
	var hundred, scaled apd.Decimal
	hundred.SetInt64(100)
	if _, err := c.Quo(&scaled, &want, &hundred); err != nil {
		return false
	}
	var diff apd.Decimal
	if _, err := c.Sub(&diff, &scaled, t.Tax.Decimal()); err != nil {
		return false
	}
	diff.Abs(&diff)
	limit := apd.New(1, -int32(t.Tax.Currency().MinorUnits))
	return diff.Cmp(limit) <= 0
}

// ------------------------------------------------------------------ the UBL document

const (
	nsInvoice    = "urn:oasis:names:specification:ubl:schema:xsd:Invoice-2"
	nsCreditNote = "urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2"
	nsCAC        = "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2"
	nsCBC        = "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2"
)

type amount struct {
	Currency string `xml:"currencyID,attr"`
	Value    string `xml:",chardata"`
}

type quantity struct {
	Unit  string `xml:"unitCode,attr"`
	Value string `xml:",chardata"`
}

type scheme struct {
	Scheme string `xml:"schemeID,attr,omitempty"`
	Value  string `xml:",chardata"`
}

type country struct {
	Code string `xml:"cbc:IdentificationCode"`
}

type postal struct {
	Street  string  `xml:"cbc:StreetName,omitempty"`
	Street2 string  `xml:"cbc:AdditionalStreetName,omitempty"`
	City    string  `xml:"cbc:CityName,omitempty"`
	Post    string  `xml:"cbc:PostalZone,omitempty"`
	Country country `xml:"cac:Country"`
}

type taxScheme struct {
	ID string `xml:"cbc:ID"`
}

type partyTax struct {
	CompanyID string    `xml:"cbc:CompanyID"`
	Scheme    taxScheme `xml:"cac:TaxScheme"`
}

type legalEntity struct {
	Name      string  `xml:"cbc:RegistrationName"`
	CompanyID *scheme `xml:"cbc:CompanyID,omitempty"`
}

type partyName struct {
	Name string `xml:"cbc:Name"`
}

type party struct {
	Endpoint scheme      `xml:"cbc:EndpointID"`
	Name     partyName   `xml:"cac:PartyName"`
	Address  postal      `xml:"cac:PostalAddress"`
	Tax      *partyTax   `xml:"cac:PartyTaxScheme,omitempty"`
	Legal    legalEntity `xml:"cac:PartyLegalEntity"`
}

type partyWrap struct {
	Party party `xml:"cac:Party"`
}

type invoiceRef struct {
	ID string `xml:"cbc:ID"`
}

type billingRef struct {
	Ref invoiceRef `xml:"cac:InvoiceDocumentReference"`
}

type additionalRef struct {
	ID          string `xml:"cbc:ID"`
	TypeCode    string `xml:"cbc:DocumentTypeCode"`
	Description string `xml:"cbc:DocumentDescription"`
}

type taxCategory struct {
	ID      string    `xml:"cbc:ID"`
	Percent string    `xml:"cbc:Percent,omitempty"`
	Scheme  taxScheme `xml:"cac:TaxScheme"`
}

type taxSubtotal struct {
	Taxable  amount      `xml:"cbc:TaxableAmount"`
	Tax      amount      `xml:"cbc:TaxAmount"`
	Category taxCategory `xml:"cac:TaxCategory"`
}

type taxTotal struct {
	Tax      amount        `xml:"cbc:TaxAmount"`
	Subtotal []taxSubtotal `xml:"cac:TaxSubtotal"`
}

type monetary struct {
	LineExtension amount `xml:"cbc:LineExtensionAmount"`
	TaxExclusive  amount `xml:"cbc:TaxExclusiveAmount"`
	TaxInclusive  amount `xml:"cbc:TaxInclusiveAmount"`
	Payable       amount `xml:"cbc:PayableAmount"`
}

type itemTax struct {
	ID      string    `xml:"cbc:ID"`
	Percent string    `xml:"cbc:Percent,omitempty"`
	Scheme  taxScheme `xml:"cac:TaxScheme"`
}

type item struct {
	Name string  `xml:"cbc:Name"`
	Tax  itemTax `xml:"cac:ClassifiedTaxCategory"`
}

type price struct {
	Amount amount `xml:"cbc:PriceAmount"`
}

// invoiceLine and creditLine differ only in the quantity element's name.
type invoiceLine struct {
	ID       string   `xml:"cbc:ID"`
	Quantity quantity `xml:"cbc:InvoicedQuantity"`
	Net      amount   `xml:"cbc:LineExtensionAmount"`
	Item     item     `xml:"cac:Item"`
	Price    price    `xml:"cac:Price"`
}

type creditLine struct {
	ID       string   `xml:"cbc:ID"`
	Quantity quantity `xml:"cbc:CreditedQuantity"`
	Net      amount   `xml:"cbc:LineExtensionAmount"`
	Item     item     `xml:"cac:Item"`
	Price    price    `xml:"cac:Price"`
}

func amt(m money.Money) amount { return amount{Currency: m.Currency().Code, Value: m.String()} }

func partyOf(p Party) partyWrap {
	sch, id := p.endpoint()
	out := party{Endpoint: scheme{Scheme: sch, Value: id}, Name: partyName{Name: p.Name},
		Address: postal{Street: p.Address.Line1, Street2: p.Address.Line2, City: p.Address.City, Post: p.Address.PostCode, Country: country{Code: p.Address.Country}},
		Legal:   legalEntity{Name: p.Name}}
	if p.GST != "" {
		out.Tax = &partyTax{CompanyID: p.GST, Scheme: taxScheme{ID: TaxScheme}}
	}
	if p.UEN != "" {
		out.Legal.CompanyID = &scheme{Scheme: UENScheme, Value: p.UEN}
	}
	return partyWrap{Party: out}
}

func day(t time.Time) string { return t.Format("2006-01-02") }

// Build is the PINT-SG UBL document of d, UTF-8 XML. It returns a *RefusedError, and no document, while Check finds a
// rule broken.
func Build(d Document) ([]byte, error) {
	if f := Check(d); len(f) > 0 {
		return nil, &RefusedError{Findings: f}
	}
	var b bytes.Buffer
	b.WriteString(xml.Header)
	w := &writer{b: &b}
	name, ns, typeTag, typeCode := "Invoice", nsInvoice, "cbc:InvoiceTypeCode", "380"
	switch d.Kind {
	case CreditNote:
		name, ns, typeTag, typeCode = "CreditNote", nsCreditNote, "cbc:CreditNoteTypeCode", "381"
	case DebitNote:
		typeCode = "383"
	}
	w.open(name, `xmlns="`+ns+`" xmlns:cac="`+nsCAC+`" xmlns:cbc="`+nsCBC+`"`)
	w.text("cbc:CustomizationID", SpecificationID)
	w.text("cbc:ProfileID", BusinessProcess)
	w.text("cbc:ID", d.Number)
	w.text("cbc:UUID", d.UUID)
	w.text("cbc:IssueDate", day(d.Issued))
	if !d.Due.IsZero() && d.Kind != CreditNote {
		w.text("cbc:DueDate", day(d.Due))
	}
	w.text(typeTag, typeCode)
	w.text("cbc:Note", d.Note)
	w.text("cbc:DocumentCurrencyCode", d.Currency.Code)
	if d.SGDTax != nil {
		w.text("cbc:TaxCurrencyCode", "SGD")
	}
	if d.Preceded != "" {
		w.value(billingRef{Ref: invoiceRef{ID: d.Preceded}}, "cac:BillingReference")
	}
	if d.SGDNet != nil && d.SGDGross != nil {
		w.value(additionalRef{ID: "SGD", TypeCode: "sgdtotal-excl-gst", Description: d.SGDNet.String()}, "cac:AdditionalDocumentReference")
		w.value(additionalRef{ID: "SGD", TypeCode: "sgdtotal-incl-gst", Description: d.SGDGross.String()}, "cac:AdditionalDocumentReference")
	}
	w.value(partyOf(d.Seller), "cac:AccountingSupplierParty")
	w.value(partyOf(d.Buyer), "cac:AccountingCustomerParty")
	tt := taxTotal{Tax: amt(d.TaxTotal)}
	for _, t := range d.Tax {
		c := taxCategory{ID: t.Category, Scheme: taxScheme{ID: TaxScheme}}
		if taxedCategory(t.Category) {
			c.Percent = t.Rate
		}
		tt.Subtotal = append(tt.Subtotal, taxSubtotal{Taxable: amt(t.Taxable), Tax: amt(t.Tax), Category: c})
	}
	w.value(tt, "cac:TaxTotal")
	if d.SGDTax != nil {
		w.value(taxTotal{Tax: amt(*d.SGDTax)}, "cac:TaxTotal")
	}
	w.value(monetary{LineExtension: amt(d.Net), TaxExclusive: amt(d.Net), TaxInclusive: amt(d.Gross), Payable: amt(d.Gross)}, "cac:LegalMonetaryTotal")
	for _, l := range d.Lines {
		it := item{Name: l.Name, Tax: itemTax{ID: l.Category, Scheme: taxScheme{ID: TaxScheme}}}
		if taxedCategory(l.Category) {
			it.Tax.Percent = l.Rate
		}
		q := quantity{Unit: l.Unit, Value: l.Quantity}
		if d.Kind == CreditNote {
			w.value(creditLine{ID: l.ID, Quantity: q, Net: amt(l.Net), Item: it, Price: price{Amount: amount{Currency: d.Currency.Code, Value: l.NetPrice}}}, "cac:CreditNoteLine")
		} else {
			w.value(invoiceLine{ID: l.ID, Quantity: q, Net: amt(l.Net), Item: it, Price: price{Amount: amount{Currency: d.Currency.Code, Value: l.NetPrice}}}, "cac:InvoiceLine")
		}
	}
	w.close(name)
	if w.err != nil {
		return nil, w.err
	}
	return b.Bytes(), nil
}

// writer writes the document's elements in the order given, one per line, which the XML marshaller does not guarantee
// across a mix of attributes and prefixed names.
type writer struct {
	b   *bytes.Buffer
	err error
}

func (w *writer) open(tag, attrs string) { fmt.Fprintf(w.b, "<%s %s>\n", tag, attrs) }
func (w *writer) close(tag string)       { fmt.Fprintf(w.b, "</%s>\n", tag) }

func (w *writer) text(tag, v string) {
	if v == "" {
		return
	}
	var esc bytes.Buffer
	if err := xml.EscapeText(&esc, []byte(v)); err != nil && w.err == nil {
		w.err = err
	}
	fmt.Fprintf(w.b, "  <%s>%s</%s>\n", tag, esc.String(), tag)
}

func (w *writer) value(v any, tag string) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("  ", "  ")
	if err := enc.EncodeElement(v, xml.StartElement{Name: xml.Name{Local: tag}}); err != nil {
		if w.err == nil {
			w.err = err
		}
		return
	}
	w.b.Write(buf.Bytes())
	w.b.WriteByte('\n')
}
