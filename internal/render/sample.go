package render

import "github.com/cognizhi/fyniq/internal/domain/doctemplate"

func text(s string) Value       { return Value{Text: s} }
func day(s string) Value        { return Value{Date: s} }
func sgd(s string) Value        { return Value{Amount: &Amount{Amount: s, Currency: "SGD"}} }
func amount(s, c string) Amount { return Amount{Amount: s, Currency: c} }

// Sample is made-up data for a document of documentType from a GST-registered organisation: what the template
// editor's preview and a publication's test documents render (FR-TPL-01, FR-TPL-03). Every name and number in it is
// synthetic (DEV-08).
func Sample(documentType string) Data {
	n := int64(12)
	f := map[string]Value{
		"organisation.name": text("Example Analytics Pte Ltd"), "organisation.uen": text("202400001A"), "organisation.gst_number": text("M90000001A"),
		"organisation.address": text("1 Example Road #01-01, Singapore 000001"), "organisation.email": text("accounts@example.com"),
		"organisation.phone": text("+65 6000 0000"), "document.date": day("2026-10-23"),
		"document.currency": text("SGD"),
		"customer.name":     text("Acme Trading Pte Ltd"), "customer.address": text("2 Sample Street, Singapore 000002"), "customer.uen": text("201900002B"),
		"customer.email": text("ap@acme.example"),
		"supplier.name":  text("Cloud Tools Pte Ltd"), "supplier.address": text("3 Sample Avenue, Singapore 000003"), "supplier.uen": text("202000003C"),
		"supplier.email":    text("billing@cloudtools.example"),
		"payment.bank_name": text("DBS Bank"), "payment.account_name": text("Example Analytics Pte Ltd"), "payment.account_number": text("000-000000-0"),
		"payment.paynow": text("202400001A"), "payment.link": text("https://pay.example.com/inv-0042"),
	}
	d := Data{Fields: f, GSTRegistered: true,
		Lines: []Line{
			{Description: "Data platform subscription, October 2026", Quantity: "1", UnitPrice: "1000.00", GSTRate: "9%", Amount: amount("1000.00", "SGD")},
			{Description: "Onboarding workshop (half day)", Quantity: "2", UnitPrice: "250.00", GSTRate: "9%", Amount: amount("500.00", "SGD")},
		},
		GSTSummary: []GSTRow{{Rate: "9%", Net: amount("1500.00", "SGD"), GST: amount("135.00", "SGD")}}}
	totals := func() {
		f["document.subtotal"], f["document.gst_total"], f["document.total"] = sgd("1500.00"), sgd("135.00"), sgd("1635.00")
		f["document.reference"] = text("PO-4411")
	}
	switch documentType {
	case doctemplate.Invoice:
		totals()
		f["document.number"], f["document.due_date"], f["document.payment_terms"] = text("INV-0042"), day("2026-11-22"), text("30 days")
		f["document.amount_paid"], f["document.amount_due"] = sgd("0.00"), sgd("1635.00")
	case doctemplate.Quote:
		totals()
		f["document.number"], f["document.valid_until"] = text("QUO-0007"), day("2026-11-22")
	case doctemplate.CreditNote:
		f["document.number"], f["document.invoice_number"], f["document.invoice_date"] = text("CN-0003"), text("INV-0042"), day("2026-10-23")
		f["document.reason"] = text("The workshop was shortened to one session")
		d.Lines = []Line{{Description: "Onboarding workshop (half day)", Quantity: "1", UnitPrice: "250.00", GSTRate: "9%", Amount: amount("250.00", "SGD")}}
		d.GSTSummary = []GSTRow{{Rate: "9%", Net: amount("250.00", "SGD"), GST: amount("22.50", "SGD")}}
		f["document.subtotal"], f["document.gst_total"], f["document.total"] = sgd("250.00"), sgd("22.50"), sgd("272.50")
	case doctemplate.Receipt:
		f["document.number"], f["document.amount"], f["document.method"] = text("RCT-0019"), sgd("1635.00"), text("PayNow")
		d.Lines = []Line{{Description: "INV-0042 of 23 Oct 2026", Amount: amount("1635.00", "SGD")}}
	case doctemplate.Statement:
		f["document.number"], f["document.period_start"], f["document.period_end"] = text("STM-2026-10"), day("2026-10-01"), day("2026-10-31")
		f["document.opening_balance"], f["document.balance_due"], f["document.overdue"] = sgd("545.00"), sgd("2180.00"), sgd("545.00")
		d.Lines = []Line{{Description: "INV-0031 of 15 Sep 2026, due 15 Oct 2026", Amount: amount("545.00", "SGD")},
			{Description: "INV-0042 of 23 Oct 2026, due 22 Nov 2026", Amount: amount("1635.00", "SGD")}}
	case doctemplate.Reminder:
		f["document.number"], f["document.amount_due"], f["document.overdue"] = text("REM-0004"), sgd("545.00"), sgd("545.00")
		f["document.oldest_due_date"], f["document.days_overdue"] = day("2026-10-15"), Value{Number: &n}
		d.Lines = []Line{{Description: "INV-0031 of 15 Sep 2026, due 15 Oct 2026", Amount: amount("545.00", "SGD")}}
	case doctemplate.Remittance:
		f["document.number"], f["document.amount"], f["document.method"] = text("PAY-0028"), sgd("872.00"), text("Bank transfer")
		d.Lines = []Line{{Description: "Their invoice CT-2210 of 1 Oct 2026", Amount: amount("872.00", "SGD")}}
	case doctemplate.PurchaseOrder:
		totals()
		f["document.number"], f["document.delivery_date"] = text("PO-0012"), day("2026-11-06")
	}
	return d
}
