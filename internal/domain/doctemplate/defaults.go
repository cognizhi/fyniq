package doctemplate

// Accent is the default accent colour: the design system's brand teal at a shade that reads on white (5.4:1).
const Accent = "#0F766E"

// titles are the documents' titles; an invoice's is the renderer's ("Tax Invoice" when the organisation is
// GST-registered, FR-TPL-03).
var titles = map[string]string{
	Quote: "Quotation", CreditNote: "Credit note", Receipt: "Receipt", Statement: "Statement of account",
	Reminder: "Payment reminder", Remittance: "Remittance advice", PurchaseOrder: "Purchase order",
}

// recipients are the recipient block's label per document type.
var recipients = map[string]string{
	Invoice: "Bill to", Quote: "Prepared for", CreditNote: "Credit to", Receipt: "Received from", Statement: "Account of",
	Reminder: "To", Remittance: "Paid to", PurchaseOrder: "Supplier",
}

// emails are the default subject and body of each document type's email, in the catalogue's fields.
var emails = map[string][2]string{
	Invoice: {"Invoice {{document.number}} from {{organisation.name}}",
		"Dear {{customer.name}},\n\nPlease find attached invoice {{document.number}} for {{document.total}}, due on {{document.due_date|long}}.\n\nThank you,\n{{organisation.name}}"},
	Quote: {"Quotation {{document.number}} from {{organisation.name}}",
		"Dear {{customer.name}},\n\nPlease find attached our quotation {{document.number}} for {{document.total}}, valid until {{document.valid_until|long}}.\n\nKind regards,\n{{organisation.name}}"},
	CreditNote: {"Credit note {{document.number}} from {{organisation.name}}",
		"Dear {{customer.name}},\n\nPlease find attached credit note {{document.number}} for {{document.total}}, against invoice {{document.invoice_number}}.\n\nKind regards,\n{{organisation.name}}"},
	Receipt: {"Receipt {{document.number}} from {{organisation.name}}",
		"Dear {{customer.name}},\n\nThank you for your payment of {{document.amount}}. Your receipt {{document.number}} is attached.\n\nKind regards,\n{{organisation.name}}"},
	Statement: {"Statement of account from {{organisation.name}}",
		"Dear {{customer.name}},\n\nPlease find attached your statement to {{document.period_end|long}}. The balance due is {{document.balance_due}}.\n\nKind regards,\n{{organisation.name}}"},
	Reminder: {"Payment reminder from {{organisation.name}}",
		"Dear {{customer.name}},\n\nOur records show {{document.overdue}} overdue since {{document.oldest_due_date|long}}. Please find the details attached; if you have paid already, thank you, and please ignore this reminder.\n\nKind regards,\n{{organisation.name}}"},
	Remittance: {"Payment from {{organisation.name}}",
		"Dear {{supplier.name}},\n\nWe have paid {{document.amount}} on {{document.date|long}}. The remittance advice {{document.number}} attached lists the bills it settles.\n\nKind regards,\n{{organisation.name}}"},
	PurchaseOrder: {"Purchase order {{document.number}} from {{organisation.name}}",
		"Dear {{supplier.name}},\n\nPlease find attached purchase order {{document.number}} for {{document.total}}. Please quote its number on your invoice.\n\nKind regards,\n{{organisation.name}}"},
}

// Default is the content a new template of kind for documentType starts from: every block the type takes in the
// starter order, the optional ones a small company rarely needs hidden; or the email's subject and body.
func Default(kind, documentType string) Content {
	if kind == KindEmail {
		e := emails[documentType]
		return Content{Schema: Schema, Subject: e[0], Body: e[1]}
	}
	c := Content{
		Schema: Schema,
		Page:   &Page{Size: "A4", Margins: Margins{Top: 20, Right: 18, Bottom: 20, Left: 18}},
		Theme:  &Theme{Logo: true, Accent: Accent, Font: Fonts[0]},
	}
	for _, kind := range Blocks(documentType) {
		b := Block{Kind: kind, Shown: true}
		switch kind {
		case BlockHeader:
			if t, ok := titles[documentType]; ok {
				b.Texts = map[string]string{TextTitle: t}
			}
		case BlockRecipient:
			b.Texts = map[string]string{TextLabel: recipients[documentType]}
		case BlockPaymentDetails:
			b.Texts = map[string]string{TextLabel: "How to pay", TextBody: "Bank transfer to {{payment.account_name}}, {{payment.bank_name}} account {{payment.account_number}}, or PayNow to UEN {{payment.paynow}}. Please quote {{document.number}}."}
		case BlockNotes:
			b.Texts = map[string]string{TextLabel: "Notes", TextBody: ""}
			b.Shown = false
		case BlockSignature:
			b.Texts = map[string]string{TextLabel: "Authorised signature"}
			b.Shown = false
		case BlockFooter:
			b.Texts = map[string]string{TextBody: "{{organisation.name}} · UEN {{organisation.uen}}"}
		case BlockText:
			b.Texts = map[string]string{TextBody: ""}
			b.Shown = false
		}
		c.Blocks = append(c.Blocks, b)
	}
	return c
}
