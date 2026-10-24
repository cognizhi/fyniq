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
		"Dear {{customer.name}},\n\nPlease find attached invoice {{document.number}} for {{document.total}}, due on {{document.due_date|long}}. You can also view it online: {{document.link}}\n\nThank you,\n{{organisation.name}}"},
	Quote: {"Quotation {{document.number}} from {{organisation.name}}",
		"Dear {{customer.name}},\n\nPlease find attached our quotation {{document.number}} for {{document.total}}, valid until {{document.valid_until|long}}. You can view it, and accept or decline it, online: {{document.link}}\n\nKind regards,\n{{organisation.name}}"},
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

// StarterTemplate is one of the starter gallery's templates (FR-TPL-07): a style with its font, accent and page.
type StarterTemplate struct {
	Key, Name, Description string
	Theme                  Theme
	Margins                Margins
}

// Starters are the starter gallery, the first the default (FR-TPL-07; the owner's decision of 23 Oct 2026, rev 0.98):
// one per style, each accent at least 4.5:1 against white, none violet, which marks AI content in Fyniq.
var Starters = []StarterTemplate{
	{Key: "classic", Name: "Classic", Description: "Inter, a teal title and a rule under the table's head",
		Theme: Theme{Logo: true, Accent: Accent, Font: "Inter", Style: "classic"}, Margins: Margins{Top: 20, Right: 18, Bottom: 20, Left: 18}},
	{Key: "band", Name: "Band", Description: "IBM Plex Sans, a navy band behind the header and the table's head",
		Theme: Theme{Logo: true, Accent: "#1E3A8A", Font: "IBM Plex Sans", Style: "band"}, Margins: Margins{Top: 16, Right: 18, Bottom: 20, Left: 18}},
	{Key: "minimal", Name: "Minimal", Description: "IBM Plex Serif, no rules, generous margins",
		Theme: Theme{Logo: true, Accent: "#374151", Font: "IBM Plex Serif", Style: "minimal"}, Margins: Margins{Top: 24, Right: 22, Bottom: 24, Left: 22}},
	{Key: "compact", Name: "Compact", Description: "Inter in smaller type, for long line tables",
		Theme: Theme{Logo: true, Accent: "#7C2D12", Font: "Inter", Style: "compact"}, Margins: Margins{Top: 14, Right: 14, Bottom: 14, Left: 14}},
}

// StarterKey looks a starter up by its key.
func StarterKey(key string) (StarterTemplate, bool) {
	for _, s := range Starters {
		if s.Key == key {
			return s, true
		}
	}
	return StarterTemplate{}, false
}

// Default is the content a new template of kind for documentType starts from: the classic starter.
func Default(kind, documentType string) Content { return Starter(Starters[0], kind, documentType) }

// Starter is the content of a starter template of kind for documentType: every block the type takes in the starter
// order, the optional ones a small company rarely needs hidden; or the email's subject and body, the same for every
// starter.
func Starter(s StarterTemplate, kind, documentType string) Content {
	if kind == KindEmail {
		e := emails[documentType]
		return Content{Schema: Schema, Subject: e[0], Body: e[1]}
	}
	theme := s.Theme
	c := Content{
		Schema: Schema,
		Page:   &Page{Size: "A4", Margins: s.Margins},
		Theme:  &theme,
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
