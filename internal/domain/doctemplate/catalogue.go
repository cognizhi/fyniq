package doctemplate

import "slices"

// The document types a template serves (FR-TPL-05); payslips are Nemura's (P2).
const (
	Invoice       = "invoice"
	Quote         = "quote"
	CreditNote    = "credit_note"
	Receipt       = "receipt"
	Statement     = "statement"
	Reminder      = "reminder"
	Remittance    = "remittance"
	PurchaseOrder = "purchase_order"
)

// Types are the document types, in the order the screens list them.
var Types = []string{Invoice, Quote, CreditNote, Receipt, Statement, Reminder, Remittance, PurchaseOrder}

// The kinds of template: a document rendered to PDF, or the email that carries it (FR-TPL-05).
const (
	KindDocument = "document"
	KindEmail    = "email"
)

// Kinds are the kinds of template.
var Kinds = []string{KindDocument, KindEmail}

// The types of a field (FR-TPL-02): what a value is decides the formats it takes.
const (
	TypeText   = "text"
	TypeDate   = "date"
	TypeMoney  = "money"
	TypeNumber = "number"
)

// Formats are, per field type, the formats a field takes; the first is the default. A date's: long "23 October
// 2026", medium "23 Oct 2026", short "23/10/2026", iso "2026-10-23". An amount's: code "SGD 1,234.50", symbol
// "S$1,234.50", plain "1,234.50".
var Formats = map[string][]string{
	TypeText:   nil,
	TypeDate:   {"medium", "long", "short", "iso"},
	TypeMoney:  {"code", "symbol", "plain"},
	TypeNumber: nil,
}

// Field is one field of the typed catalogue (FR-TPL-02): a text writes it as {{path}} or {{path|format}}.
type Field struct {
	Path, Type, Label string
}

// The block kinds (FR-TPL-01). Their order is the starter templates' order; drag-and-drop layout is P2.
const (
	BlockHeader          = "header"           // the logo and the document's title
	BlockOurDetails      = "our_details"      // the organisation's name, address, UEN and GST number
	BlockRecipient       = "recipient"        // the customer's or supplier's details
	BlockDocumentDetails = "document_details" // number, date, due date, reference
	BlockLineItems       = "line_items"       // the line table
	BlockTotals          = "totals"           // totals and the GST summary, with SGD equivalents
	BlockPaymentDetails  = "payment_details"  // bank, PayNow and payment link
	BlockNotes           = "notes"            // notes and terms
	BlockSignature       = "signature"        // a signature or stamp
	BlockFooter          = "footer"
	BlockText            = "text" // free text, up to MaxTextBlocks of them
)

// The texts a block takes, and their limits.
const (
	TextTitle = "title"
	TextLabel = "label"
	TextBody  = "body"

	MaxLabel      = 80
	MaxBody       = 2000
	MaxTextBlocks = 5
)

// blockTexts are the texts each block kind takes. The rest of a block's content comes from the document.
var blockTexts = map[string][]string{
	BlockHeader:          {TextTitle},
	BlockOurDetails:      nil,
	BlockRecipient:       {TextLabel},
	BlockDocumentDetails: nil,
	BlockLineItems:       nil,
	BlockTotals:          nil,
	BlockPaymentDetails:  {TextLabel, TextBody},
	BlockNotes:           {TextLabel, TextBody},
	BlockSignature:       {TextLabel},
	BlockFooter:          {TextBody},
	BlockText:            {TextBody},
}

// BlockKinds are the block kinds, in the starter templates' order.
var BlockKinds = []string{BlockHeader, BlockOurDetails, BlockRecipient, BlockDocumentDetails, BlockLineItems, BlockTotals,
	BlockPaymentDetails, BlockNotes, BlockSignature, BlockFooter, BlockText}

// Locked are, per document type, the blocks that carry the IRAS particulars (FR-TPL-03, FR-AR-05): present and
// shown. A full tax invoice shows the words "Tax Invoice", so an invoice's title is the renderer's, never a text;
// a credit note needs no title. Simplified tax invoices render from the invoice template. Source: IRAS, "Invoicing
// customers" (retrieved 23 Oct 2026).
var Locked = map[string][]string{
	Invoice:    {BlockHeader, BlockOurDetails, BlockRecipient, BlockDocumentDetails, BlockLineItems, BlockTotals},
	CreditNote: {BlockOurDetails, BlockRecipient, BlockDocumentDetails, BlockLineItems, BlockTotals},
}

// LockedTitle reports whether the renderer writes a document type's title: an invoice's "Tax Invoice".
func LockedTitle(documentType string) bool { return documentType == Invoice }

// Blocks are the block kinds a document type takes.
func Blocks(documentType string) []string {
	out := []string{BlockHeader, BlockOurDetails, BlockRecipient, BlockDocumentDetails, BlockLineItems, BlockTotals}
	switch documentType {
	case Invoice, Quote, Statement, Reminder:
		out = append(out, BlockPaymentDetails)
	}
	out = append(out, BlockNotes)
	switch documentType {
	case Invoice, Quote, CreditNote, PurchaseOrder:
		out = append(out, BlockSignature)
	}
	return append(out, BlockFooter, BlockText)
}

var (
	organisationFields = []Field{
		{"organisation.name", TypeText, "Your company's name"},
		{"organisation.uen", TypeText, "Your UEN"},
		{"organisation.gst_number", TypeText, "Your GST registration number"},
		{"organisation.address", TypeText, "Your address"},
		{"organisation.email", TypeText, "Your email address"},
		{"organisation.phone", TypeText, "Your phone number"},
	}
	documentFields = []Field{
		{"document.number", TypeText, "Document number"},
		{"document.date", TypeDate, "Document date"},
		{"document.reference", TypeText, "Reference"},
		{"document.currency", TypeText, "Currency"},
	}
	customerFields = []Field{
		{"customer.name", TypeText, "Customer's name"},
		{"customer.address", TypeText, "Customer's address"},
		{"customer.uen", TypeText, "Customer's UEN or tax ID"},
		{"customer.email", TypeText, "Customer's billing email"},
	}
	supplierFields = []Field{
		{"supplier.name", TypeText, "Supplier's name"},
		{"supplier.address", TypeText, "Supplier's address"},
		{"supplier.uen", TypeText, "Supplier's UEN or tax ID"},
		{"supplier.email", TypeText, "Supplier's email"},
	}
	paymentFields = []Field{
		{"payment.bank_name", TypeText, "Your bank"},
		{"payment.account_name", TypeText, "Your account's name"},
		{"payment.account_number", TypeText, "Your account's number"},
		{"payment.paynow", TypeText, "Your PayNow UEN"},
		{"payment.link", TypeText, "Payment link"},
	}
	totalFields = []Field{
		{"document.subtotal", TypeMoney, "Total before GST"},
		{"document.gst_total", TypeMoney, "GST"},
		{"document.total", TypeMoney, "Total"},
	}
	sgdFields = []Field{
		{"document.subtotal_sgd", TypeMoney, "Total before GST in SGD"},
		{"document.gst_total_sgd", TypeMoney, "GST in SGD"},
		{"document.total_sgd", TypeMoney, "Total in SGD"},
	}
)

// Fields are the typed catalogue of a document type (FR-TPL-02): the organisation's and the document's fields,
// the customer's or supplier's, and the type's own.
func Fields(documentType string) []Field {
	out := slices.Concat(organisationFields, documentFields)
	switch documentType {
	case Remittance, PurchaseOrder:
		out = append(out, supplierFields...)
	default:
		out = append(out, customerFields...)
	}
	switch documentType {
	case Invoice:
		out = slices.Concat(out, totalFields, sgdFields, []Field{
			{"document.due_date", TypeDate, "Due date"},
			{"document.payment_terms", TypeText, "Payment terms"},
			{"document.amount_paid", TypeMoney, "Amount paid"},
			{"document.amount_due", TypeMoney, "Amount due"},
		}, paymentFields)
	case Quote:
		out = slices.Concat(out, totalFields, []Field{{"document.valid_until", TypeDate, "Valid until"}}, paymentFields)
	case CreditNote:
		out = slices.Concat(out, totalFields, sgdFields, []Field{
			{"document.invoice_number", TypeText, "Invoice credited"},
			{"document.invoice_date", TypeDate, "Date of the invoice credited"},
			{"document.reason", TypeText, "Reason for the credit"},
		})
	case Receipt:
		out = slices.Concat(out, []Field{
			{"document.amount", TypeMoney, "Amount received"},
			{"document.method", TypeText, "How it was paid"},
		})
	case Statement:
		out = slices.Concat(out, []Field{
			{"document.period_start", TypeDate, "Statement from"},
			{"document.period_end", TypeDate, "Statement to"},
			{"document.opening_balance", TypeMoney, "Opening balance"},
			{"document.balance_due", TypeMoney, "Balance due"},
			{"document.overdue", TypeMoney, "Overdue"},
		}, paymentFields)
	case Reminder:
		out = slices.Concat(out, []Field{
			{"document.amount_due", TypeMoney, "Amount due"},
			{"document.overdue", TypeMoney, "Overdue"},
			{"document.oldest_due_date", TypeDate, "Earliest due date"},
			{"document.days_overdue", TypeNumber, "Days overdue"},
		}, paymentFields)
	case Remittance:
		out = slices.Concat(out, []Field{
			{"document.amount", TypeMoney, "Amount paid"},
			{"document.method", TypeText, "How it was paid"},
		})
	case PurchaseOrder:
		out = slices.Concat(out, totalFields, []Field{{"document.delivery_date", TypeDate, "Delivery date"}})
	}
	return out
}

// Styles are the looks the renderer draws (FR-TPL-07; the owner's decision of 23 Oct 2026, rev 0.98), one per starter
// template: classic (the accent on the title and the table's rule), band (an accent band behind the header and the
// table's head), minimal (no rules, the accent on labels only) and compact (smaller type and tighter spacing).
var Styles = []string{"classic", "band", "minimal", "compact"}

// The theme's choices (FR-TPL-01): fonts the render service ships, self-hosted under the SIL Open Font Licence, and
// the page sizes and margins.
var (
	Fonts      = []string{"Inter", "IBM Plex Sans", "IBM Plex Serif"}
	PageSizes  = []string{"A4", "Letter"}
	MinMargin  = 10 // mm
	MaxMargin  = 40 // mm
	MinOnWhite = 45 // the accent colour's contrast against white, in tenths: 4.5:1, WCAG 2.2 AA for text
)

// BlockTexts are the texts a block kind takes on a document type: an invoice's header takes no title.
func BlockTexts(documentType, kind string) []string {
	out := []string{}
	for _, t := range blockTexts[kind] {
		if t == TextTitle && LockedTitle(documentType) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// IsLocked reports whether a block carries IRAS particulars on a document type (FR-TPL-03).
func IsLocked(documentType, kind string) bool { return slices.Contains(Locked[documentType], kind) }
