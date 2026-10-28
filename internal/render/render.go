// Package render turns a published template version and a document's data into the layout the render service
// compiles with Typst (PRD-001 §8.25, FR-TPL-02, FR-TPL-03; ADR-T1; p1-templates-email task 1.2). Everything a
// document says is decided here, in Go: each text's fields filled and formatted, each block's lines, rows and
// columns, and which IRAS particular each carries. The Typst source the service owns only lays the layout out, so
// a template holds no code and the same version and data give the same layout, and so the same bytes.
package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// Amount is an amount and its currency, as decimal strings (ENG-12).
type Amount struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Money reads the amount.
func (a Amount) Money() (money.Money, error) {
	c, err := money.Lookup(a.Currency)
	if err != nil {
		return money.Money{}, err
	}
	return money.Parse(a.Amount, c)
}

// Value is one field's value: a text, a day (YYYY-MM-DD), an amount or a whole number.
type Value struct {
	Text   string  `json:"text,omitempty"`
	Date   string  `json:"date,omitempty"`
	Amount *Amount `json:"amount,omitempty"`
	Number *int64  `json:"number,omitempty"`
}

// Line is one line of a document's line table, its amounts formatted by the issuer as it posts them.
type Line struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity,omitempty"`
	UnitPrice   string `json:"unit_price,omitempty"` // a decimal string, up to six places, as entered
	GSTRate     string `json:"gst_rate,omitempty"`   // such as "9%", or "SR" or "ZR" for a zero-rated or exempt line
	Amount      Amount `json:"amount"`
}

// GSTRow is one rate of the GST summary: the net amount at the rate and its GST.
type GSTRow struct {
	Rate string `json:"rate"`
	Net  Amount `json:"net"`
	GST  Amount `json:"gst"`
}

// Data is a document's data: its fields by the catalogue's paths, its lines and GST summary, and what decides its
// particulars (FR-AR-05).
type Data struct {
	Fields        map[string]Value `json:"fields"`
	Lines         []Line           `json:"lines,omitempty"`
	GSTSummary    []GSTRow         `json:"gst_summary,omitempty"`
	GSTRegistered bool             `json:"gst_registered"`
	Simplified    bool             `json:"simplified,omitempty"` // a simplified tax invoice: S$1,000 or less with GST
	Logo          bool             `json:"logo,omitempty"`       // the organisation has a logo, sent beside the data
}

// The IRAS particulars a block can carry (FR-AR-05, FR-TPL-03): what the render service finds in the compiled
// document and publication checks. Source: IRAS, "Invoicing customers" (retrieved 23 Oct 2026).
const (
	PTitle           = "title"            // the words "Tax Invoice"
	PSupplierName    = "supplier_name"    // the organisation's name
	PSupplierAddress = "supplier_address" // and address
	PSupplierGST     = "supplier_gst_number"
	PNumber          = "number" // the identifying number
	PDate            = "date"   // the date of issue
	PCustomerName    = "customer_name"
	PCustomerAddress = "customer_address"
	PDescription     = "description" // of the goods or services
	PQuantity        = "quantity"    // a credit note's quantity credited per line
	PRate            = "gst_rate"
	PNet             = "total_before_gst"
	PGST             = "gst_total"
	PTotal           = "total_with_gst"
	PSGD             = "sgd_amounts"            // a foreign-currency tax invoice's totals in SGD
	PIncludesGST     = "includes_gst_statement" // a simplified tax invoice's "price payable includes GST"
	PReason          = "reason"                 // a credit note's reason for the credit
)

// Required are the particulars a document of documentType with data must show: none when the organisation is not
// GST-registered, and none for documents that are not tax invoices or credit notes.
func Required(documentType string, d Data) []string {
	if !d.GSTRegistered {
		return nil
	}
	supplier := []string{PSupplierName, PSupplierAddress, PSupplierGST, PNumber, PDate}
	switch documentType {
	case doctemplate.Invoice:
		if d.Simplified {
			return append(supplier, PDescription, PTotal, PIncludesGST)
		}
		out := append([]string{PTitle}, supplier...)
		out = append(out, PCustomerName, PCustomerAddress, PDescription, PRate, PNet, PGST, PTotal)
		if foreign(d) {
			out = append(out, PSGD)
		}
		return out
	case doctemplate.CreditNote:
		return append(supplier, PCustomerName, PCustomerAddress, PReason, PDescription, PQuantity, PRate, PNet, PGST, PTotal)
	}
	return nil
}

func foreign(d Data) bool {
	c := d.Fields["document.currency"].Text
	return c != "" && c != "SGD"
}

// Item is one line of text, and the particular it carries, if any.
type Item struct {
	Text       string `json:"text"`
	Particular string `json:"particular,omitempty"`
}

// Row is a label and its value, and the particulars they carry.
type Row struct {
	Label       string   `json:"label"`
	Value       string   `json:"value"`
	Particulars []string `json:"particulars,omitempty"`
	Strong      bool     `json:"strong,omitempty"`
}

// Column is a column of the line table.
type Column struct {
	Title string `json:"title"`
	Right bool   `json:"right,omitempty"`
}

// Block is a shown block, laid out.
type Block struct {
	Kind        string     `json:"kind"`
	Title       string     `json:"title,omitempty"`
	Label       string     `json:"label,omitempty"`
	Body        string     `json:"body,omitempty"`
	Items       []Item     `json:"items,omitempty"`
	Rows        []Row      `json:"rows,omitempty"`
	Columns     []Column   `json:"columns,omitempty"`
	Cells       [][]string `json:"cells,omitempty"`
	Particulars []string   `json:"particulars,omitempty"` // carried by the block as a whole, such as the line table's
	Note        *Item      `json:"note,omitempty"`
	Logo        bool       `json:"logo,omitempty"`
}

// Layout is what the render service's Typst source reads as its data file.
type Layout struct {
	Title   string  `json:"title"` // the PDF's title (PDF/UA-1)
	Author  string  `json:"author"`
	Lang    string  `json:"lang"`
	Region  string  `json:"region"`
	Paper   string  `json:"paper"`
	Margins [4]int  `json:"margins_mm"` // top, right, bottom, left
	Accent  string  `json:"accent"`
	Font    string  `json:"font"`
	Style   string  `json:"style"` // the theme's style; classic when the template names none
	Blocks  []Block `json:"blocks"`
}

// titles are the documents' titles when the template gives none.
var titles = map[string]string{
	doctemplate.Quote: "Quotation", doctemplate.CreditNote: "Credit note", doctemplate.Receipt: "Receipt",
	doctemplate.Statement: "Statement of account", doctemplate.Reminder: "Payment reminder", doctemplate.Remittance: "Remittance advice",
	doctemplate.PurchaseOrder: "Purchase order",
}

// numbers are the label of a document type's number.
var numbers = map[string]string{
	doctemplate.Invoice: "Invoice no.", doctemplate.Quote: "Quotation no.", doctemplate.CreditNote: "Credit note no.", doctemplate.Receipt: "Receipt no.",
	doctemplate.Statement: "Statement no.", doctemplate.Reminder: "Reference", doctemplate.Remittance: "Payment no.", doctemplate.PurchaseOrder: "PO no.",
}

type builder struct {
	typ string
	d   Data
	err error
}

// value is a field's value in format, or "" when the document has none.
func (b *builder) value(path, format string) string {
	v, ok := b.d.Fields[path]
	if !ok {
		return ""
	}
	s, err := Format(v, format)
	if err != nil && b.err == nil {
		b.err = fmt.Errorf("render: %s: %w", path, err)
	}
	return s
}

func (b *builder) text(t string) string { return doctemplate.Expand(t, b.value) }

// parts expands a text of parts joined by " · ", leaving out a part whose fields are all empty: the footer's
// "UEN {{organisation.uen}}" of an organisation with no UEN, rather than a dangling "· UEN".
func (b *builder) parts(t string) string {
	var out []string
	for _, p := range strings.Split(t, " · ") {
		fields, filled := false, false
		s := doctemplate.Expand(p, func(path, format string) string {
			v := b.value(path, format)
			fields, filled = true, filled || strings.TrimSpace(v) != ""
			return v
		})
		if fields && !filled {
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, " · ")
}

func (b *builder) item(path, particular string) []Item {
	s := strings.TrimSpace(b.value(path, ""))
	if s == "" {
		return nil
	}
	return []Item{{Text: s, Particular: particular}}
}

func (b *builder) row(label, path, format string, strong bool, particulars ...string) []Row {
	s := b.value(path, format)
	if s == "" {
		return nil
	}
	return []Row{{Label: label, Value: s, Particulars: particulars, Strong: strong}}
}

// party is whom the document is addressed to: the supplier of a remittance or purchase order, else the customer.
func (b *builder) party() string {
	if b.typ == doctemplate.Remittance || b.typ == doctemplate.PurchaseOrder {
		return "supplier"
	}
	return "customer"
}

// Build lays out content, a version of a document template of documentType, with d: the shown blocks in their order.
func Build(documentType string, c doctemplate.Content, d Data) (Layout, error) {
	if c.Page == nil || c.Theme == nil {
		return Layout{}, fmt.Errorf("render: a %s template without a page or theme", documentType)
	}
	if !slices.Contains(doctemplate.Fonts, c.Theme.Font) {
		return Layout{}, fmt.Errorf("render: the font %q is not one the render service ships", c.Theme.Font)
	}
	b := &builder{typ: documentType, d: d}
	m := c.Page.Margins
	out := Layout{Author: b.value("organisation.name", ""), Lang: "en", Region: "SG", Paper: strings.ToLower(c.Page.Size),
		Margins: [4]int{m.Top, m.Right, m.Bottom, m.Left}, Accent: c.Theme.Accent, Font: c.Theme.Font, Style: cmpOr(c.Theme.Style, "classic"), Blocks: []Block{}}
	title := titles[documentType]
	for _, x := range c.Blocks {
		if x.Kind == doctemplate.BlockHeader && x.Texts[doctemplate.TextTitle] != "" && !doctemplate.LockedTitle(documentType) {
			title = x.Texts[doctemplate.TextTitle]
		}
	}
	titleParticular := ""
	if documentType == doctemplate.Invoice {
		title = "Invoice"
		if d.GSTRegistered {
			title, titleParticular = "Tax Invoice", PTitle
		}
	}
	out.Title = strings.TrimSpace(title + " " + b.value("document.number", ""))
	for _, x := range c.Blocks {
		if !x.Shown {
			continue
		}
		t := x.Texts
		blk := Block{Kind: x.Kind}
		switch x.Kind {
		case doctemplate.BlockHeader:
			blk.Items = []Item{{Text: title, Particular: titleParticular}}
			blk.Logo = c.Theme.Logo && d.Logo
		case doctemplate.BlockOurDetails:
			blk.Items = slices.Concat(b.item("organisation.name", PSupplierName), b.item("organisation.address", PSupplierAddress),
				prefixed("UEN ", b.item("organisation.uen", "")))
			if d.GSTRegistered {
				blk.Items = append(blk.Items, prefixed("GST Reg. No. ", b.item("organisation.gst_number", PSupplierGST))...)
			}
			blk.Items = slices.Concat(blk.Items, b.item("organisation.email", ""), b.item("organisation.phone", ""))
		case doctemplate.BlockRecipient:
			blk.Label = t[doctemplate.TextLabel]
			name, address := "", ""
			if b.party() == "customer" {
				name, address = PCustomerName, PCustomerAddress
			}
			p := b.party()
			blk.Items = slices.Concat(b.item(p+".name", name), b.item(p+".address", address), prefixed("UEN ", b.item(p+".uen", "")), b.item(p+".email", ""))
		case doctemplate.BlockDocumentDetails:
			blk.Rows = b.details()
		case doctemplate.BlockLineItems:
			b.lines(&blk)
		case doctemplate.BlockTotals:
			b.totals(&blk)
		case doctemplate.BlockPaymentDetails, doctemplate.BlockNotes:
			blk.Label, blk.Body = t[doctemplate.TextLabel], strings.TrimSpace(b.text(t[doctemplate.TextBody]))
		case doctemplate.BlockSignature:
			blk.Label = t[doctemplate.TextLabel]
		case doctemplate.BlockFooter:
			blk.Body = strings.TrimSpace(b.parts(t[doctemplate.TextBody]))
		case doctemplate.BlockText:
			blk.Body = strings.TrimSpace(b.text(t[doctemplate.TextBody]))
		default:
			continue
		}
		out.Blocks = append(out.Blocks, blk)
	}
	return out, b.err
}

func prefixed(p string, items []Item) []Item {
	for i := range items {
		items[i].Text = p + items[i].Text
	}
	return items
}

// details are the document details' rows for its type.
func (b *builder) details() []Row {
	out := slices.Concat(b.row(numbers[b.typ], "document.number", "", true, PNumber), b.row("Date", "document.date", "", false, PDate))
	switch b.typ {
	case doctemplate.Invoice:
		out = slices.Concat(out, b.row("Due date", "document.due_date", "", false), b.row("Terms", "document.payment_terms", "", false))
	case doctemplate.Quote:
		out = append(out, b.row("Valid until", "document.valid_until", "", false)...)
	case doctemplate.CreditNote:
		out = slices.Concat(out, b.row("Invoice credited", "document.invoice_number", "", false), b.row("Invoice date", "document.invoice_date", "", false),
			b.row("Reason", "document.reason", "", false, PReason))
	case doctemplate.Statement:
		out = slices.Concat(out, b.row("From", "document.period_start", "", false), b.row("To", "document.period_end", "", false))
	case doctemplate.PurchaseOrder:
		out = append(out, b.row("Deliver by", "document.delivery_date", "", false)...)
	case doctemplate.Receipt, doctemplate.Remittance:
		out = append(out, b.row("Paid by", "document.method", "", false)...)
	}
	return append(out, b.row("Reference", "document.reference", "", false)...)
}

// lines lays out the line table: description, quantity, unit price, the GST rate when registered, and the amount.
func (b *builder) lines(blk *Block) {
	currency := b.value("document.currency", "")
	qty, price, rates := false, false, false
	for _, l := range b.d.Lines {
		qty, price, rates = qty || l.Quantity != "", price || l.UnitPrice != "", rates || (b.d.GSTRegistered && l.GSTRate != "")
	}
	blk.Columns = []Column{{Title: "Description"}}
	if qty {
		blk.Columns = append(blk.Columns, Column{Title: "Qty", Right: true})
	}
	if price {
		blk.Columns = append(blk.Columns, Column{Title: "Unit price", Right: true})
	}
	if rates {
		blk.Columns = append(blk.Columns, Column{Title: "GST", Right: true})
	}
	blk.Columns = append(blk.Columns, Column{Title: strings.TrimSpace("Amount " + currency), Right: true})
	blk.Cells = [][]string{}
	described, quantified, rated := false, false, false
	for _, l := range b.d.Lines {
		row := []string{l.Description}
		described = described || strings.TrimSpace(l.Description) != ""
		if qty {
			row = append(row, l.Quantity)
			quantified = quantified || l.Quantity != ""
		}
		if price {
			row = append(row, grouped(l.UnitPrice))
		}
		if rates {
			row = append(row, l.GSTRate)
			rated = rated || l.GSTRate != ""
		}
		blk.Cells = append(blk.Cells, append(row, b.plain(&l.Amount)))
	}
	for _, p := range []struct {
		ok bool
		p  string
	}{{described, PDescription}, {quantified, PQuantity}, {rated, PRate}} {
		if p.ok {
			blk.Particulars = append(blk.Particulars, p.p)
		}
	}
}

func (b *builder) plain(a *Amount) string {
	if a == nil {
		return ""
	}
	m, err := a.Money()
	if err != nil {
		if b.err == nil {
			b.err = fmt.Errorf("render: a line's amount: %w", err)
		}
		return ""
	}
	return FormatMoney(m, "plain")
}

// totals lays out the totals and the GST summary, with the SGD equivalents of a foreign-currency tax invoice.
func (b *builder) totals(blk *Block) {
	switch b.typ {
	case doctemplate.Receipt:
		blk.Rows = b.row("Amount received", "document.amount", "", true)
		return
	case doctemplate.Remittance:
		blk.Rows = b.row("Amount paid", "document.amount", "", true)
		return
	case doctemplate.Statement:
		// The ageing of the balance due adds up to it, with any unapplied credits (FR-AR-08).
		blk.Rows = slices.Concat(b.row("Opening balance", "document.opening_balance", "", false),
			b.row("Not yet due", "document.not_yet_due", "", false), b.row("1–30 days overdue", "document.overdue_month_one", "", false),
			b.row("31–60 days overdue", "document.overdue_month_two", "", false), b.row("61–90 days overdue", "document.overdue_month_three", "", false),
			b.row("Over 90 days overdue", "document.overdue_older", "", false), b.row("Unapplied credits", "document.unapplied_credits", "", false),
			b.row("Overdue", "document.overdue", "", false), b.row("Balance due", "document.balance_due", "", true))
		return
	case doctemplate.Reminder:
		blk.Rows = slices.Concat(b.row("Overdue", "document.overdue", "", false), b.row("Amount due", "document.amount_due", "", true))
		return
	}
	if b.d.GSTRegistered && !b.d.Simplified {
		blk.Rows = b.row("Total before GST", "document.subtotal", "", false, PNet)
		if len(b.d.GSTSummary) == 1 { // one rate: its GST is the total GST
			blk.Rows = append(blk.Rows, b.row("GST "+b.d.GSTSummary[0].Rate, "document.gst_total", "", false, PRate, PGST)...)
		} else {
			for _, g := range b.d.GSTSummary {
				blk.Rows = append(blk.Rows, Row{Label: "GST " + g.Rate + " on " + b.money(g.Net), Value: b.money(g.GST), Particulars: []string{PRate}})
			}
			blk.Rows = append(blk.Rows, b.row("GST", "document.gst_total", "", false, PGST)...)
		}
	}
	label := "Total"
	if b.d.GSTRegistered {
		label = "Total with GST"
	}
	blk.Rows = append(blk.Rows, b.row(label, "document.total", "", true, PTotal)...)
	if b.typ == doctemplate.Invoice {
		if v := b.d.Fields["document.amount_paid"]; v.Amount != nil && strings.Trim(v.Amount.Amount, "0.") != "" {
			blk.Rows = append(blk.Rows, b.row("Paid", "document.amount_paid", "", false)...)
		}
		blk.Rows = append(blk.Rows, b.row("Amount due", "document.amount_due", "", true)...)
	}
	if b.d.GSTRegistered && b.d.Simplified {
		blk.Note = &Item{Text: "Price payable includes GST.", Particular: PIncludesGST}
	}
	if b.d.GSTRegistered && foreign(b.d) && (b.typ == doctemplate.Invoice || b.typ == doctemplate.CreditNote) {
		sgd := slices.Concat(b.row("Total before GST in SGD", "document.subtotal_sgd", "", false), b.row("GST in SGD", "document.gst_total_sgd", "", false),
			b.row("Total with GST in SGD", "document.total_sgd", "", false))
		if len(sgd) == 3 {
			sgd[0].Particulars = []string{PSGD}
		}
		blk.Rows = append(blk.Rows, sgd...)
	}
}

func (b *builder) money(a Amount) string {
	m, err := a.Money()
	if err != nil {
		if b.err == nil {
			b.err = fmt.Errorf("render: the GST summary: %w", err)
		}
		return ""
	}
	return FormatMoney(m, "")
}

// Email fills an email template's subject and body with d's fields (FR-TPL-05): what the email that carries the
// document says. The subject stays on one line.
func Email(c doctemplate.Content, d Data) (subject, body string, err error) {
	b := &builder{d: d}
	subject = strings.Join(strings.Fields(b.text(c.Subject)), " ")
	body = strings.TrimSpace(b.text(c.Body))
	return subject, body, b.err
}
