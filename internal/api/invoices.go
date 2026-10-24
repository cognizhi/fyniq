package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ invoices (FR-AR-03, FM-1, FM-2, SOD-3; p1-sales)
//
// A customer's invoice is drafted, checked, issued and voided as a bill is, through the posting service (invoice@1):
// issuing posts it. Approval as for bills: an invoice whose total in the base currency is above the self-approval
// threshold is issued only on another person's approval, in any team mode; at or below it, a Founder-mode issue is
// logged as self-approved for the weekly review (FM-2, FM-3); whoever prepared it never approves it (SOD-3); and in
// Founder mode voiding or editing an issued invoice needs a second person (FM-1). A credit note to a customer (FR-AR-04)
// is an invoice of kind credit_note through the same operations, posted as its reverse (credit_note@1), numbered from
// its own sequence and judged on its size by the same threshold, as a vendor credit is a bill's.

// The API's codes for invoices' approvals, beside the domain's INVOICE-01 to INVOICE-09, INVOICE-13 and INVOICE-15.
const (
	CodeInvoiceNeedsApproval = "INVOICE-10" // the invoice's total is above the self-approval threshold: ask for approval (FM-2)
	CodeInvoiceVoidApproval  = "INVOICE-11" // in Founder mode a void needs a second person (FM-1)
	CodeOwnInvoice           = "INVOICE-12" // whoever prepared the invoice never approves its issue (SOD-3)
	CodeInvoiceEditApproval  = "INVOICE-14" // in Founder mode an edit of an issued invoice needs a second person (FM-1)
)

// InvoiceCodes are the codes the posting rules and the invoice's own checks answer.
var InvoiceCodes = []string{invoice.CodeCustomer, invoice.CodeGSTCode, invoice.CodeDueDate, invoice.CodeTooLong, invoice.CodeLine,
	invoice.CodeCredited, ledger.CodeInvoiceTotal, ledger.CodeGSTRate, ledger.CodeSystemRole, ledger.CodeRuleAccount, ledger.CodeLineCurrency, ledger.CodeSpread}

// InvoiceApprovalCodes are the codes an approval of an invoice's issue, void or edit may answer: SOD-3, and the
// posting's own.
func InvoiceApprovalCodes() []string {
	return append([]string{CodeOwnInvoice, invoice.CodeNotInvoice, invoice.CodeNotDraft, invoice.CodeNotPosted, invoice.CodeBadDraft,
		invoice.CodeSettled}, InvoiceCodes...)
}

// InvoiceLineDraft is one line of an invoice as entered: what it sells, how many at what price less a discount, the
// account it earns and its GST code. A single-amount invoice is one line of quantity 1.
type InvoiceLineDraft struct {
	ItemID      string `json:"item_id,omitempty" format:"uuid" doc:"The item it sells, if any; the line keeps its own description, price and account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f50"`
	Description string `json:"description,omitempty" maxLength:"500" doc:"What the line is for, as the customer reads it." example:"Analytics platform, October"`
	Quantity    string `json:"quantity" pattern:"^(0|[1-9][0-9]{0,13})(\\.[0-9]{1,6})?$" doc:"How many, an exact decimal with up to 6 decimals; 1 for a single amount." example:"2"`
	UnitPrice   string `json:"unit_price" pattern:"^(0|[1-9][0-9]{0,13})(\\.[0-9]{1,6})?$" doc:"The price of one before GST, in the invoice's currency, an exact decimal with up to 6 decimals." example:"1500.00"`
	Discount    string `json:"discount_percent,omitempty" pattern:"^(100(\\.0{1,2})?|[0-9]{1,2}(\\.[0-9]{1,2})?)$" doc:"A discount off the line, as a percentage with at most two decimals. The line's net is RND-LINE: quantity × unit price × (100 − discount) ÷ 100, rounded once to the currency's minor units, half away from zero." example:"10"`
	AccountID   string `json:"account_id" format:"uuid" doc:"The revenue account the line earns." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	GSTCode     string `json:"gst_code" pattern:"^[A-Z]{2,4}$" doc:"The line's GST code for supplies, from the sg-gst pack in force at the invoice's date, such as SR (standard-rated), ZR (zero-rated), ES (exempt) or OS (out of scope). No GST is charged before the organisation is GST-registered." example:"SR"`
	// Spreads (FR-GL-03; rev 0.118).
	SpreadMonths int    `json:"spread_months,omitempty" minimum:"1" maximum:"120" doc:"Spread the line's revenue over this many months (FR-GL-03): issuing puts its net on 2400 Deferred revenue in the base currency, and one part a month (ALLOC-EVEN) is released to the line's account by the scheduler, up to the organisation's today and never ahead (ENG-05)." example:"12"`
	SpreadFrom   string `json:"spread_from,omitempty" format:"date" doc:"The spread's first month, as any day of it: when the service starts. Left out, the invoice's month. Months before the invoice's date release on its date." example:"2026-10-01"`
}

// InvoiceLine is one line of an invoice as the API shows it: as entered, with its net.
type InvoiceLine struct {
	InvoiceLineDraft
	Net *Money `json:"net,omitempty" doc:"The line's net under RND-LINE, in the invoice's currency; absent while its quantity, price or discount cannot be one."`
}

// InvoiceRelease is one month's release of an invoice line's spread revenue (FR-GL-03, ENG-05).
type InvoiceRelease struct {
	Line    int    `json:"line" doc:"The invoice's line it releases, from 1." example:"1"`
	Month   string `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month it releases." example:"2026-11"`
	Part    int    `json:"part" doc:"Which month of the spread it is, from 1." example:"2"`
	Of      int    `json:"of" doc:"How many months the line is spread over." example:"12"`
	Date    string `json:"date" format:"date" doc:"The day it posts: the first of its month, or the invoice's date when that is later." example:"2026-11-01"`
	Amount  Money  `json:"amount" doc:"What it moves from 2400 Deferred revenue to the line's account, in the base currency: its ALLOC-EVEN part of the line's net."`
	Status  string `json:"status" enum:"posted,waiting,projected,reversed" doc:"posted; waiting: due by the organisation's today but not in the ledger yet (the hourly job posts it, or waits for its period to open); projected: in the future, or of a draft, and not in the ledger (ENG-05); reversed by a void or an edit of the invoice." example:"posted"`
	EntryID string `json:"entry_id,omitempty" format:"uuid" doc:"Its entry, once posted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e71"`
}

// InvoiceDraft is an invoice as entered: checked for its form when saved, and by the posting rules when it is issued.
type InvoiceDraft struct {
	Kind       string             `json:"kind,omitempty" enum:"invoice,credit_note" doc:"invoice, the default, or credit_note: a credit note to the customer, entered as an invoice is and issued as its reverse (FR-AR-04). Fixed once saved." example:"invoice"`
	InvoiceID  string             `json:"invoice_id,omitempty" format:"uuid" doc:"A credit note only: the issued invoice of the same customer and currency it credits, in full or in part; left out, a standalone credit note. A linked credit note adjusts GST at the rates in force on that invoice's date, and credits at most its total." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	CustomerID string             `json:"customer_id,omitempty" format:"uuid" doc:"The customer: a contact marked as a customer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Reference  string             `json:"reference,omitempty" maxLength:"60" doc:"The customer's reference for it, such as their purchase order number." example:"PO-88213"`
	Date       string             `json:"date,omitempty" format:"date" doc:"The invoice's date: its tax point." example:"2026-10-24"`
	DueDate    string             `json:"due_date,omitempty" format:"date" doc:"When it falls due; left out, the customer's payment terms, else the organisation's (FR-ORG-04). A credit note has none." example:"2026-11-23"`
	Currency   string             `json:"currency" pattern:"^[A-Z]{3}$" doc:"The invoice's currency." example:"SGD"`
	FXRate     string             `json:"fx_rate,omitempty" doc:"For an invoice in another currency: base currency per one unit. Left out, the invoice takes the cascade's rate for its date (ENG-40)." example:"1.34"`
	Lines      []InvoiceLineDraft `json:"lines" maxItems:"200" doc:"The invoice's lines."`
	Notes      string             `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with the invoice." example:"Thank you for your business"`
}

// InvoiceCustomer is an invoice's customer, as the invoice shows it.
type InvoiceCustomer struct {
	ID   string `json:"id" format:"uuid" doc:"The contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Name string `json:"name,omitempty" doc:"The contact's legal name." example:"Acme Analytics Pte Ltd"`
}

// InvoiceGST is the GST of one GST code's lines under RND-GST-EXCL.
type InvoiceGST struct {
	GSTCode string `json:"gst_code" doc:"The GST code." example:"SR"`
	Rate    string `json:"rate" doc:"Its rate as a fraction, from the sg-gst pack in force at the invoice's date." example:"0.09"`
	Net     Money  `json:"net" doc:"The code's lines' net, in the invoice's currency."`
	GST     Money  `json:"gst" doc:"Their GST, in the invoice's currency."`
}

// InvoiceStates are an invoice's separate states (FR-AR-03): only issuing and voiding change the ledger; a delivery
// failure or becoming overdue never does.
type InvoiceStates struct {
	Approval   string `json:"approval" enum:"draft,awaiting_approval,approved,rejected" doc:"draft until it is issued or asked for; awaiting_approval while a request to issue it waits; approved once issued; rejected when the latest request was rejected and it is still a draft." example:"draft"`
	Ledger     string `json:"ledger" enum:"not_posted,posted,voided" doc:"not_posted, posted (issued), or voided by a reversal." example:"not_posted"`
	Delivery   string `json:"delivery" enum:"not_sent,sent,delivered,viewed,bounced" doc:"not_sent until sending arrives (FR-AR-06): then sent, delivered, viewed (the customer first opened its secure link) or bounced." example:"not_sent"`
	Settlement string `json:"settlement" enum:"open,part_paid,paid,written_off" doc:"What is settled of it, from the receipts, credits and write-offs applied to it net of their voids (FR-AR-18): open, part_paid, paid, or written_off when it is settled and a write-off settles part of it (FR-AR-17)." example:"open"`
	Due        string `json:"due" enum:"not_due,overdue" doc:"overdue once its due date has passed while it is issued and not paid in full; not_due otherwise." example:"not_due"`
}

// InvoiceCheck is a problem that stops a draft invoice being issued (✖).
type InvoiceCheck struct {
	Severity string `json:"severity" enum:"error,warning" doc:"error (✖): it cannot be issued until fixed; warning (⚠): it is issued, and reviewed." example:"error"`
	Code     string `json:"code" doc:"The rule's stable code (ENG-23)." example:"INVOICE-05"`
	Field    string `json:"field,omitempty" doc:"The field it is about." example:"customer_id"`
	Message  string `json:"message" doc:"What is wrong." example:"the invoice has no customer"`
	Fix      string `json:"fix,omitempty" doc:"How to fix it." example:"Choose the customer it is for"`
}

// Invoice is a customer's invoice: its current version, its states and what issuing it does.
type Invoice struct {
	ID            string           `json:"id" format:"uuid" doc:"The invoice's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Kind          string           `json:"kind" enum:"invoice,credit_note" doc:"invoice, or credit_note: a credit note to the customer, issued as an invoice's reverse (FR-AR-04)." example:"invoice"`
	Credits       *InvoiceRef      `json:"credits,omitempty" doc:"A credit note linked to an invoice: the invoice it credits."`
	Number        string           `json:"number,omitempty" doc:"Its number from the invoice sequence (a credit note's, from the credit note sequence), given when it is issued (FR-ORG-04); a draft has none." example:"INV-0042"`
	Status        string           `json:"status" enum:"draft,posted,reversed" doc:"Draft (✎), posted (issued), or reversed by a void." example:"draft"`
	Version       int              `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Customer      *InvoiceCustomer `json:"customer,omitempty" doc:"The customer, with its name."`
	Reference     string           `json:"reference,omitempty" doc:"The customer's reference for it." example:"PO-88213"`
	Date          string           `json:"date,omitempty" format:"date" doc:"The invoice's date." example:"2026-10-24"`
	DueDate       string           `json:"due_date,omitempty" format:"date" doc:"When it falls due: as entered, or from the payment terms." example:"2026-11-23"`
	Currency      string           `json:"currency" doc:"The invoice's currency." example:"SGD"`
	FXRate        string           `json:"fx_rate,omitempty" doc:"The rate typed for it, if one was." example:"1.34"`
	Lines         []InvoiceLine    `json:"lines" nullable:"false" doc:"The lines as entered, each with its net."`
	Notes         string           `json:"notes,omitempty" doc:"Its notes." example:"Thank you for your business"`
	GSTCharged    bool             `json:"gst_charged" doc:"Whether it charges GST: the organisation is GST-registered on the invoice's date (FR-ORG-03)." example:"true"`
	GST           []InvoiceGST     `json:"gst_subtotals" nullable:"false" doc:"Its GST by GST code under RND-GST-EXCL, per code's subtotal or per line by the organisation's setting; empty when it charges none."`
	Net           *Money           `json:"net,omitempty" doc:"Every line's net, in the invoice's currency."`
	GSTTotal      *Money           `json:"gst,omitempty" doc:"Its GST, in the invoice's currency."`
	Total         *Money           `json:"total,omitempty" doc:"Its net and GST, in the invoice's currency: what the customer owes; absent while a line cannot be read."`
	Receivable    *Money           `json:"receivable,omitempty" doc:"What it adds to accounts receivable in the base currency (a credit note: what it takes off): as issued, or as issuing would post it; absent while it cannot be issued."`
	Open          *Money           `json:"open,omitempty" doc:"An issued invoice: what is still to be received of it, in its currency: its total less what receipts and credits settle (FR-AR-18). An issued credit note: what is left of it to apply or refund."`
	States        InvoiceStates    `json:"states" doc:"Its approval, ledger, delivery, settlement and due states."`
	Releases      []InvoiceRelease `json:"releases" nullable:"false" doc:"getInvoice only: the monthly releases of its spread lines (FR-GL-03), by line and month, each with its state; the months still to come are projections, not in the ledger (ENG-05)."`
	Checks        []InvoiceCheck   `json:"checks" nullable:"false" doc:"getInvoice only: what stops a draft being issued (✖)."`
	LedgerImpact  []EntryLine      `json:"ledger_impact" nullable:"false" doc:"getInvoice only: the lines issuing it writes (a draft), or wrote (issued): debits positive."`
	PreparedBy    *Person          `json:"prepared_by,omitempty" doc:"Who saved the current version."`
	PostedBy      *Person          `json:"posted_by,omitempty" doc:"Who issued it."`
	PostedAt      *time.Time       `json:"posted_at,omitempty" doc:"When it was issued." example:"2026-10-24T09:30:00Z"`
	ApprovalAsked string           `json:"approval_asked,omitempty" format:"uuid" doc:"getInvoice only: the pending request to issue, void or edit it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd2"`
}

// InvoiceRef names an invoice another document refers to.
type InvoiceRef struct {
	ID     string `json:"id" format:"uuid" doc:"The invoice's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	Number string `json:"number,omitempty" doc:"Its number, once issued." example:"INV-0042"`
}

// InvoicePage is a page of invoices, newest date first.
type InvoicePage struct {
	Items      []Invoice `json:"items" nullable:"false"`
	NextCursor string    `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// InvoicePath names an invoice in the path.
type InvoicePath struct {
	InvoiceID string `path:"invoice_id" format:"uuid" doc:"The invoice's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
}

type invoiceListInput struct {
	PageQuery
	Kind   string `query:"kind" enum:"invoice,credit_note" default:"invoice" doc:"Invoices, the default, or credit notes." example:"invoice"`
	Status string `query:"status" enum:"draft,posted,reversed" doc:"Only invoices with this status." example:"draft"`
	From   string `query:"from" format:"date" doc:"Only invoices dated on or after this day." example:"2026-10-01"`
	To     string `query:"to" format:"date" doc:"Only invoices dated on or before this day." example:"2026-10-31"`
	Q      string `query:"q" maxLength:"100" doc:"Only invoices whose number contains this text, ignoring case." example:"INV-0042"`
}

type invoiceListOutput struct{ Body InvoicePage }

type createInvoiceInput struct {
	KeyHeader
	PreferHeader
	Body InvoiceDraft
}

type replaceInvoiceInput struct {
	InvoicePath
	IfMatchHeader
	PreferHeader
	Body InvoiceDraft
}

type invoiceOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The invoice's version and status, for If-Match."`
	Location string `header:"Location" doc:"The invoice's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Invoice
}

type invoiceReadOutput struct {
	ETag string `header:"ETag" doc:"The invoice's version and status, for If-Match."`
	Body Invoice
}

type invoiceCommandInput struct {
	InvoicePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type invoiceVoidInput struct {
	InvoicePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

// InvoiceEdit is the correction of an issued invoice: its new content, and why (FR-GL-12).
type InvoiceEdit struct {
	InvoiceDraft
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is edited: the audit trail and the reversal's memo keep it (FR-GL-12)." example:"The customer's PO number was wrong"`
}

// InvoiceEdited is what an edit posted.
type InvoiceEdited struct {
	Invoice  Invoice `json:"invoice" doc:"The invoice at its new version, still issued, with whoever posted the edit."`
	Reversal Entry   `json:"reversal" doc:"The entry that reverses the one the invoice had, dated on that entry's date."`
	Entry    Entry   `json:"entry" doc:"The entry of the new version, on its date."`
}

type invoiceEditInput struct {
	InvoicePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body InvoiceEdit
}

type invoiceEditOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The invoice's new version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entries show what the edit would post (FR-INT-14)."`
	Body     InvoiceEdited
}

// InvoicePosting is an invoice after an issue or a void, with the entry it made.
type InvoicePosting struct {
	Invoice Invoice `json:"invoice" doc:"The invoice after the command."`
	Entry   Entry   `json:"entry" doc:"The entry the command posted: the invoice's entry, or its reversing entry."`
}

type invoicePostingOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The invoice's new version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what the command would write (FR-INT-14)."`
	Body     InvoicePosting
}

type invoiceRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

func invoiceETag(i Invoice) string { return etag(i.Version, i.Status) }

// invoiceDraftOf reads an invoice from the request, with every invalid field at once.
func invoiceDraftOf(in InvoiceDraft, base money.Currency) (invoice.Draft, error) {
	var d invoice.Draft
	var fields []FieldProblem
	if in.CustomerID != "" {
		id, err := uuid.Parse(in.CustomerID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.customer_id", Message: "is not an ID", Fix: "Send the customer's contact ID"})
		}
		d.CustomerID = id
	}
	if in.InvoiceID != "" {
		id, err := uuid.Parse(in.InvoiceID)
		switch {
		case err != nil:
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.invoice_id", Message: "is not an ID", Fix: "Send the ID of the invoice it credits"})
		case in.Kind != "credit_note":
			fields = append(fields, FieldProblem{Code: invoice.CodeCredited, Field: "body.invoice_id", Message: "names an invoice, which only a credit note credits",
				Fix: "Leave it out, or send kind credit_note"})
		}
		d.Credits = id
	}
	d.Reference, d.Notes = strings.TrimSpace(in.Reference), in.Notes
	for _, x := range []struct {
		text, field string
		to          *time.Time
	}{{in.Date, "body.date", &d.Date}, {in.DueDate, "body.due_date", &d.DueDate}} {
		if x.text == "" {
			continue
		}
		t, err := time.Parse(time.DateOnly, x.text)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		*x.to = t
	}
	cur, err := money.Lookup(in.Currency)
	if err != nil {
		return d, invalid(append(fields, FieldProblem{Code: money.CodeCurrency, Field: "body.currency", Message: "is not a currency Fyniq knows",
			Fix: "Send an ISO 4217 code such as SGD"}))
	}
	d.Currency = cur
	if in.FXRate != "" {
		r, err := money.ParseRate(in.FXRate, cur, base, RateSourceDocument)
		if err != nil {
			fields = append(fields, fieldOf(err, "body.fx_rate"))
		} else {
			d.Rate = &r
		}
	}
	for i, l := range in.Lines {
		at := func(f string) string { return "body.lines[" + strconv.Itoa(i) + "]." + f }
		id, err := uuid.Parse(l.AccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("account_id"), Message: "is not an ID", Fix: "Send the account's ID"})
		}
		line := invoice.Line{AccountID: id, Description: l.Description, Discount: l.Discount, GSTCode: l.GSTCode, SpreadMonths: l.SpreadMonths}
		if l.ItemID != "" {
			item, err := uuid.Parse(l.ItemID)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("item_id"), Message: "is not an ID", Fix: "Send the item's ID, or leave it out"})
				continue
			}
			line.ItemID = &item
		}
		if line.Quantity, err = money.ParseQuantity(l.Quantity, at("quantity")); err != nil {
			fields = append(fields, fieldOf(err, at("quantity")))
			continue
		}
		if line.UnitPrice, err = money.ParseUnitPrice(l.UnitPrice, cur, at("unit_price")); err != nil {
			fields = append(fields, fieldOf(err, at("unit_price")))
			continue
		}
		if l.SpreadFrom != "" {
			if line.SpreadFrom, err = time.Parse(time.DateOnly, l.SpreadFrom); err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("spread_from"), Message: "is not a date", Fix: "Send the spread's first month as YYYY-MM-DD"})
				continue
			}
		}
		d.Lines = append(d.Lines, line)
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// invoiceType is the document type of a kind of invoice; invoiceKind the kind of a document.
func invoiceType(kind string) storage.DocumentType {
	if kind == "credit_note" {
		return storage.DocumentCreditNote
	}
	return storage.DocumentInvoice
}

func invoiceKind(doc storage.Document) string {
	if invoice.IsCreditNote(doc) {
		return "credit_note"
	}
	return "invoice"
}

// sameKind refuses a new version of another kind than the document's: an invoice never becomes a credit note. The
// draft's kind is taken as the document's, so its invoice_id is read as a credit note's.
func sameKind(in *InvoiceDraft, before Invoice) error {
	if in.Kind != "" && in.Kind != before.Kind {
		return invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.kind", Message: "is " + before.Kind + " and never changes",
			Fix: "Leave it out, or save a new document of the other kind"}})
	}
	in.Kind = before.Kind
	return nil
}

// invoiceView is what showing invoices reads once per request: the base currency, the organisation's today, the
// accounts receivable account, and the pending and rejected requests about invoices.
type invoiceView struct {
	base        money.Currency
	today       time.Time
	receivables uuid.UUID
	gst         uuid.UUID         // 2200, the GST control account
	asked       map[string]string // an invoice's ID: its pending request
	rejected    map[string]int    // an invoice's ID: the version its latest rejected request to issue was for
}

func (a *API) invoiceView(ctx context.Context, u storage.UnitOfWork) (invoiceView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return invoiceView{}, err
	}
	v := invoiceView{asked: map[string]string{}, rejected: map[string]int{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return invoiceView{}, err
	}
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return invoiceView{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return invoiceView{}, err
	}
	for _, x := range accounts {
		switch string(x.Role) {
		case ledger.RoleAccountsReceivable:
			v.receivables = x.ID
		case ledger.RoleGSTControl:
			v.gst = x.ID
		}
	}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return invoiceView{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType != approval.ObjectInvoice {
			continue
		}
		switch approval.StatusAt(requestOf(r), now) {
		case approval.Pending:
			v.asked[r.ObjectKey] = r.ID.String()
		case approval.Rejected:
			if r.Operation == approval.OpInvoicePost {
				var c struct {
					Version string `json:"version"`
				}
				if json.Unmarshal(r.Change, &c) == nil {
					if n, err := strconv.Atoi(c.Version); err == nil && n >= v.rejected[r.ObjectKey] {
						v.rejected[r.ObjectKey] = n
					}
				}
			}
		}
	}
	return v, nil
}

// invoiceOf is an invoice as the API shows it, from its document. withChecks fills its checks and ledger impact
// (getInvoice).
func (a *API) invoiceOf(ctx context.Context, u storage.UnitOfWork, v invoiceView, doc storage.Document, withChecks bool) (Invoice, error) {
	d, err := invoice.Read(doc, v.base)
	if err != nil {
		return Invoice{}, err
	}
	out := Invoice{ID: doc.ID.String(), Kind: invoiceKind(doc), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Reference: d.Reference,
		Date: dayText(d.Date), DueDate: dayText(d.DueDate), Currency: d.Currency.Code, Notes: d.Notes, Lines: invoiceLinesOut(d),
		GST: []InvoiceGST{}, Releases: []InvoiceRelease{}, Checks: []InvoiceCheck{}, LedgerImpact: []EntryLine{}, PreparedBy: personOf(doc.SavedBy), PostedBy: personOf(doc.PostedBy),
		PostedAt: utc(doc.PostedAt)}
	if d.Rate != nil {
		out.FXRate = d.Rate.String()
	}
	if d.Credits != uuid.Nil {
		out.Credits = &InvoiceRef{ID: d.Credits.String()}
		if x, err := u.Documents().Get(ctx, d.Credits); err == nil {
			out.Credits.Number = x.Number
		}
	}
	if withChecks {
		rs, err := journal.ReleasesOf(ctx, u, doc, v.today)
		if err != nil {
			return Invoice{}, err
		}
		for _, r := range rs {
			x := InvoiceRelease{Line: r.Release.Line, Month: r.Release.Month.Format("2006-01"), Part: r.Release.Part, Of: r.Release.Of,
				Date: dayText(r.Release.Date), Amount: moneyOut(r.Release.Amount), Status: r.Status}
			if r.EntryID != uuid.Nil {
				x.EntryID = r.EntryID.String()
			}
			out.Releases = append(out.Releases, x)
		}
	}
	if d.CustomerID != uuid.Nil {
		out.Customer = &InvoiceCustomer{ID: d.CustomerID.String()}
		if c, err := u.Contacts().Get(ctx, d.CustomerID); err == nil {
			out.Customer.Name = c.LegalName
		}
	}
	// What it charges, from the version as the rules read it: as issued, the rules in force at its date read the same.
	p, probs, err := invoice.Prepare(ctx, u, doc, v.base)
	if err != nil {
		return Invoice{}, err
	}
	if out.DueDate == "" {
		out.DueDate = dayText(p.DueDate)
	}
	out.GSTCharged = p.Registered
	readable := !probs.Has(ledger.CodeGSTRate) && len(p.Invoice.Lines) == len(d.Lines)

	out.States = InvoiceStates{Approval: "draft", Ledger: "not_posted", Delivery: "not_sent", Settlement: "open", Due: "not_due"}
	switch doc.Status {
	case storage.DocumentPosted:
		out.States.Approval, out.States.Ledger = "approved", "posted"
	case storage.DocumentReversed:
		out.States.Approval, out.States.Ledger = "approved", "voided"
	default:
		if out.ApprovalAsked = v.asked[out.ID]; out.ApprovalAsked != "" {
			out.States.Approval = "awaiting_approval"
		} else if n, ok := v.rejected[out.ID]; ok && n == doc.Version {
			out.States.Approval = "rejected"
		}
	}
	if doc.Status == storage.DocumentDraft {
		c, err := journal.CheckInvoice(ctx, u, doc)
		if err != nil {
			return Invoice{}, err
		}
		if withChecks {
			for _, x := range c.Problems {
				out.Checks = append(out.Checks, InvoiceCheck{Severity: "error", Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
			}
		}
		if len(c.Problems) == 0 {
			r := moneyOut(c.Receivable)
			out.Receivable = &r
			if withChecks {
				out.LedgerImpact = impactOf(c.Entry)
			}
		}
		if readable {
			invoiceTotals(&out, p.Totals)
		}
		return out, a.name(ctx, u, out.PreparedBy, out.PostedBy)
	}
	if withChecks {
		out.ApprovalAsked = v.asked[out.ID]
	}
	e, err := u.Ledger().ByIdentity(ctx, journal.InvoiceIdentityOf(doc))
	if err != nil {
		return Invoice{}, err
	}
	taxed := false
	for _, l := range entryOf(e).Lines {
		if withChecks {
			out.LedgerImpact = append(out.LedgerImpact, l)
		}
		switch l.AccountID {
		case v.receivables.String():
			r := l.BaseAmount
			if invoice.IsCreditNote(doc) { // what it takes off accounts receivable
				r.Amount = strings.TrimPrefix(r.Amount, "-")
			}
			out.Receivable = &r
		case v.gst.String():
			taxed = true
		}
	}
	if readable {
		// What it charged is what its entry posted: an invoice issued before a GST registration recorded later charged none,
		// as its replay reads it.
		t := p.Totals
		if out.GSTCharged = taxed; !taxed && p.Registered {
			for k := range p.Invoice.Lines {
				p.Invoice.Lines[k].GSTRate = ""
			}
			if t, err = ledger.TotalsOf(p.Invoice); err != nil {
				return Invoice{}, err
			}
		}
		invoiceTotals(&out, t)
		if err := invoiceSettled(ctx, u, &out, t.Total); err != nil {
			return Invoice{}, err
		}
	}
	if doc.Status == storage.DocumentPosted && out.States.Settlement != "paid" && out.DueDate != "" && out.DueDate < dateOf(v.today) {
		out.States.Due = "overdue"
	}
	return out, a.name(ctx, u, out.PreparedBy, out.PostedBy)
}

// invoiceTotals fills an invoice's net, GST by code and total, in its own currency.
func invoiceTotals(out *Invoice, t ledger.InvoiceTotals) {
	net, gst, total := moneyOut(t.Net), moneyOut(t.GST), moneyOut(t.Total)
	out.Net, out.GSTTotal, out.Total = &net, &gst, &total
	for _, s := range t.Subtotals {
		out.GST = append(out.GST, InvoiceGST{GSTCode: s.Code, Rate: s.Rate, Net: moneyOut(s.Net), GST: moneyOut(s.GST)})
	}
}

// invoiceLinesOut are an invoice's lines as entered, each with its net under RND-LINE, as the API shows them.
func invoiceLinesOut(d invoice.Draft) []InvoiceLine {
	out := make([]InvoiceLine, 0, len(d.Lines))
	for _, l := range d.Lines {
		x := InvoiceLine{InvoiceLineDraft: InvoiceLineDraft{Description: l.Description, Quantity: l.Quantity.String(), UnitPrice: l.UnitPrice.String(),
			Discount: l.Discount, AccountID: l.AccountID.String(), GSTCode: l.GSTCode, SpreadMonths: l.SpreadMonths, SpreadFrom: dayText(l.SpreadFrom)}}
		if l.ItemID != nil {
			x.ItemID = l.ItemID.String()
		}
		if net, err := l.Net(); err == nil {
			n := moneyOut(net)
			x.Net = &n
		}
		out = append(out, x)
	}
	return out
}

// invoiceSettled fills an issued or voided invoice's settlement: what is open of it and its settlement state, from its
// allocations net of their voids (FR-AR-18). total is what it charges.
func invoiceSettled(ctx context.Context, u storage.UnitOfWork, i *Invoice, total money.Money) error {
	id, err := uuid.Parse(i.ID)
	if err != nil {
		return err
	}
	allocs, err := u.Allocations().OfDocument(ctx, id)
	if err != nil {
		return err
	}
	open := total
	reversed := map[uuid.UUID]bool{}
	for _, x := range allocs {
		if x.ReversesID != nil {
			reversed[*x.ReversesID] = true
		}
		amount, err := money.ParseStored(string(x.Amount), total.Currency())
		if err != nil {
			return err
		}
		if open, err = open.Sub(amount); err != nil {
			return err
		}
	}
	if i.Status != string(storage.DocumentPosted) {
		return nil
	}
	writtenOff := false // a write-off that still stands settles part of it (FR-AR-17)
	for _, x := range allocs {
		if x.ReversesID == nil && !reversed[x.ID] {
			if src, err := u.Documents().Get(ctx, x.SourceID); err == nil && src.Type == storage.DocumentWriteOff {
				writtenOff = true
			}
		}
	}
	o := moneyOut(open)
	i.Open = &o
	switch {
	case open.Sign() <= 0 && writtenOff:
		i.States.Settlement = "written_off"
	case open.Sign() <= 0:
		i.States.Settlement = "paid"
	case !open.Equal(total):
		i.States.Settlement = "part_paid"
	}
	return nil
}

// invoiceProblems turns the posting rules' problems into the API's, naming the invoice.
func invoiceProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The invoice cannot be issued: %d problem(s), each listed in errors.", len(lp)),
		"Correct the invoice and issue it again.", fields...)
}

// gateInvoiceIssue refuses an issue above the threshold (FM-2), and says whether an issue at or below it is a
// Founder-mode self-approval to log.
func (a *API) gateInvoiceIssue(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (selfApproved bool, total money.Money, err error) {
	c, err := a.invoices.Check(ctx, p.Organisation, id, nil)
	if err != nil {
		return false, total, err
	}
	if len(c.Problems) > 0 {
		return false, total, invoiceProblems(c.Problems)
	}
	total = c.Receivable
	threshold, err := selfApprovalThreshold(ctx, a, u, total.Currency())
	if err != nil {
		return false, total, err
	}
	if over, err := total.Cmp(threshold); err != nil {
		return false, total, err
	} else if over > 0 {
		return false, total, problem(CodeInvoiceNeedsApproval,
			fmt.Sprintf("The invoice's total is %s %s, above the self-approval threshold of %s.", total, total.Currency().Code, threshold),
			"Ask for another person's approval to issue it")
	}
	mode, _, _, err := teamModeOf(ctx, u)
	return mode == team.Founder, total, err
}

// logInvoiceSelfApproval records a Founder-mode issue at or below the threshold for the weekly review (FM-2, FM-3).
func (a *API) logInvoiceSelfApproval(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, total money.Money) error {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	var actor *uuid.UUID
	if p.UserID != uuid.Nil {
		actor = &p.UserID
	}
	_, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewSelfApproved, ObjectType: approval.ObjectInvoice, ObjectKey: id.String(),
		Summary: fmt.Sprintf("Issued %s %s of %s %s alone, at or below the self-approval threshold", invoice.Kind(doc), cmpText(doc.Number, "(a draft)"), total,
			total.Currency().Code),
		Actor:   actor, LoggedAt: a.deps.Clock.Now()})
	return err
}

// gateInvoiceVoid refuses the void of an issued invoice in Founder mode: it needs a second person (FM-1).
func gateInvoiceVoid(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) error {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil || doc.Status != storage.DocumentPosted {
		return err
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeInvoiceVoidApproval, "In Founder mode, voiding an issued invoice needs a second person (FM-1).", "Ask for another person's approval to void it")
	}
	return nil
}

// gateInvoiceEdit refuses an edit in Founder mode (FM-1), and in any mode an edit whose new version adds more than the
// self-approval threshold to accounts receivable (FM-2). An invoice that is not issued is left to the edit's own
// refusal; an edit the rules refuse answers their problems.
func (a *API) gateInvoiceEdit(ctx context.Context, u storage.UnitOfWork, p authn.Principal, doc storage.Document, d invoice.Draft) error {
	if doc.Status != storage.DocumentPosted {
		return nil
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	if mode == team.Founder {
		return problem(CodeInvoiceEditApproval, "In Founder mode, editing an issued invoice needs a second person (FM-1).", "Ask for another person's approval to edit it")
	}
	c, err := a.invoices.Check(ctx, p.Organisation, doc.ID, &d)
	if err != nil {
		return err
	}
	if len(c.Problems) > 0 {
		return invoiceProblems(c.Problems)
	}
	threshold, err := selfApprovalThreshold(ctx, a, u, c.Receivable.Currency())
	if err != nil {
		return err
	}
	if over, err := c.Receivable.Cmp(threshold); err != nil {
		return err
	} else if over > 0 {
		return problem(CodeInvoiceNeedsApproval,
			fmt.Sprintf("The edited invoice's total is %s %s, above the self-approval threshold of %s.", c.Receivable, c.Receivable.Currency().Code, threshold),
			"Ask for another person's approval to edit it")
	}
	return nil
}

// invoiceEditDiff is what an edit changes, field by field and line by line, for the approver (FR-APR-02).
func invoiceEditDiff(ctx context.Context, u storage.UnitOfWork, before Invoice, after InvoiceDraft, receivableBefore, receivableAfter string) ([]ApprovalDiff, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, ac := range accounts {
		names[ac.ID.String()] = ac.Code + " " + ac.Name
	}
	line := func(l InvoiceLineDraft) string {
		s := fmt.Sprintf("%s: %s × %s (%s)", names[l.AccountID], l.Quantity, l.UnitPrice, l.GSTCode)
		if l.Discount != "" {
			s += fmt.Sprintf(", less %s%%", l.Discount)
		}
		if l.SpreadMonths != 0 {
			s += fmt.Sprintf(", spread over %d months", l.SpreadMonths)
			if l.SpreadFrom != "" {
				s += " from " + l.SpreadFrom[:min(len(l.SpreadFrom), 7)]
			}
		}
		if l.Description != "" {
			s += " (" + l.Description + ")"
		}
		return s
	}
	var out []ApprovalDiff
	for _, f := range []struct{ field, before, after string }{{"reference", before.Reference, after.Reference}, {"date", before.Date, after.Date},
		{"currency", before.Currency, after.Currency}, {"fx_rate", before.FXRate, after.FXRate}} {
		if f.before != f.after {
			out = append(out, ApprovalDiff{Field: f.field, Before: f.before, After: f.after})
		}
	}
	if before.Customer != nil && before.Customer.ID != after.CustomerID {
		out = append(out, ApprovalDiff{Field: "customer_id", Before: before.Customer.ID, After: after.CustomerID})
	}
	out = append(out, ApprovalDiff{Field: "receivable", Before: receivableBefore, After: receivableAfter})
	for i := 0; i < max(len(before.Lines), len(after.Lines)); i++ {
		var b, f string
		if i < len(before.Lines) {
			b = line(before.Lines[i].InvoiceLineDraft)
		}
		if i < len(after.Lines) {
			f = line(after.Lines[i])
		}
		if b != f {
			out = append(out, ApprovalDiff{Field: fmt.Sprintf("line %d", i+1), Before: b, After: f})
		}
	}
	return out, nil
}

// executeInvoiceEdit runs an approved edit: the content the request kept, checked against the hash its envelope holds,
// posted as the approver, with the requester as the one who prepared the new version.
func (a *API) executeInvoiceEdit(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	text := func(k string) string { v, _ := s.Execute[k].(string); return v }
	content, sum, err := canonicalOf(s.Content)
	if err != nil {
		return err
	}
	if sum != text("content_sha256") {
		return fmt.Errorf("approval: the content of request %s does not match its envelope", r.ID)
	}
	var body InvoiceDraft
	if err := json.Unmarshal(content, &body); err != nil {
		return err
	}
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return err
	}
	d, err := invoiceDraftOf(body, base)
	if err != nil {
		return err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	_, err = a.invoices.Edit(ctx, o.ID, id, d, text("reason"), text("prepared_by"))
	return err
}

// contentOfInvoice is an edit's content as the request keeps it, canonical, and its SHA-256, which the envelope holds.
func contentOfInvoice(d InvoiceDraft) ([]byte, string, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, "", err
	}
	return canonicalOf(raw)
}

// invoiceReceivable is an invoice's total in the base currency, as its draft would post or as it posted.
func (a *API) invoiceReceivable(ctx context.Context, u storage.UnitOfWork, v invoiceView, doc storage.Document) (string, error) {
	i, err := a.invoiceOf(ctx, u, v, doc, false)
	if err != nil {
		return "", err
	}
	if i.Receivable == nil {
		return "", invoiceProblems(errors.New("no receivable"))
	}
	return i.Receivable.Amount, nil
}

// invoiceCommand runs a command that posts for an invoice: If-Match is checked against the invoice locked in the same
// unit of work, and a person whose roles administer users and post does not post in a mode that blocks conflicts
// (SOD-6).
func (a *API) invoiceCommand(ctx context.Context, rawID, key, ifMatch, prefer string, request any,
	command func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Entry, error)) (*invoicePostingOutput, error) {
	id, err := parseID("invoice_id", rawID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	p, _ := authn.From(ctx)
	w, err := write(ctx, a, key, prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoicePosting, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return InvoicePosting{}, err
		}
		v, err := a.invoiceView(ctx, u)
		if err != nil {
			return InvoicePosting{}, err
		}
		before, err := a.invoiceOf(ctx, u, v, doc, false)
		if err != nil {
			return InvoicePosting{}, err
		}
		if err := checkIfMatch(ifMatch, invoiceETag(before)); err != nil {
			return InvoicePosting{}, err
		}
		if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
			return InvoicePosting{}, err
		}
		entry, err := command(ctx, u, p, id)
		if err != nil {
			return InvoicePosting{}, invoiceProblems(err)
		}
		after, err := u.Documents().Get(ctx, id)
		if err != nil {
			return InvoicePosting{}, err
		}
		if v, err = a.invoiceView(ctx, u); err != nil {
			return InvoicePosting{}, err
		}
		i, err := a.invoiceOf(ctx, u, v, after, false)
		return InvoicePosting{Invoice: i, Entry: entryOf(entry)}, err
	})
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &invoicePostingOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	}
	return &invoicePostingOutput{Status: w.Status, ETag: invoiceETag(w.Body.Invoice), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}

// invoiceRequest asks for approval of a change to an invoice at the version read.
func (a *API) invoiceRequest(ctx context.Context, rawID, key, ifMatch, prefer string, request any, status storage.DocumentStatus,
	change func(doc storage.Document, i Invoice, receivable string) pendingChange) (*invoiceRequestOutput, error) {
	id, err := parseID("invoice_id", rawID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	w, err := write(ctx, a, key, prefer, request, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return Approval{}, err
		}
		v, err := a.invoiceView(ctx, u)
		if err != nil {
			return Approval{}, err
		}
		i, err := a.invoiceOf(ctx, u, v, doc, false)
		if err != nil {
			return Approval{}, err
		}
		if err := checkIfMatch(ifMatch, invoiceETag(i)); err != nil {
			return Approval{}, err
		}
		if doc.Status != status {
			code := invoice.CodeNotDraft
			if status == storage.DocumentPosted {
				code = invoice.CodeNotPosted
			}
			return Approval{}, problem(code, fmt.Sprintf("The invoice is %s.", doc.Status), "Ask for approval of an invoice in the state it needs")
		}
		if i.Open != nil && i.Total != nil && i.Open.Amount != i.Total.Amount {
			return Approval{}, problem(invoice.CodeSettled, "Receipts or credits settle the invoice.", "Void them first, then void it")
		}
		receivable, err := a.invoiceReceivable(ctx, u, v, doc)
		if err != nil {
			return Approval{}, err
		}
		return a.requestApproval(ctx, u, change(doc, i, receivable))
	})
	if err != nil {
		return nil, err
	}
	return &invoiceRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
}

func invoiceName(doc storage.Document, i Invoice) string {
	name := cmpText(doc.Number, "the draft "+invoice.Kind(doc))
	if i.Customer != nil && i.Customer.Name != "" {
		name += " to " + i.Customer.Name
	}
	if i.Reference != "" {
		name += " (" + i.Reference + ")"
	}
	return name
}

func registerInvoices(a *API) {
	formCodes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange,
		money.CodeCurrency, money.CodeRate, invoice.CodeTooLong, invoice.CodeLine, storage.CodeRetry, storage.CodeLockTimeout}
	readCodes := []string{CodeNotFound, invoice.CodeNotInvoice, invoice.CodeBadDraft}

	register(a, authz.ListInvoices, huma.Operation{
		OperationID: "listInvoices", Method: http.MethodGet, Path: "/v1/invoices", Tags: []string{"Sales"},
		Summary: "List the invoices",
		Description: "The organisation's invoices at their current version, newest date first (a draft without a date last), a page at a " +
			"time, narrowed by status, dates and text in the number (FR-AR-03). Each has its customer, totals, due date and states. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *invoiceListInput) (*invoiceListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := invoiceListOutput{Body: InvoicePage{Items: []Invoice{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: invoiceType(in.Kind), Status: storage.DocumentStatus(in.Status),
				From: in.From, To: in.To, Search: strings.TrimSpace(in.Q), AfterDate: afterDate, AfterID: afterID, Limit: in.PageSize + 1})
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			v, err := a.invoiceView(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				i, err := a.invoiceOf(ctx, u, v, d, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, i)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateInvoice, huma.Operation{
		OperationID: "createInvoice", Method: http.MethodPost, Path: "/v1/invoices", Tags: []string{"Sales"}, DefaultStatus: http.StatusCreated,
		Summary: "Save a draft invoice",
		Description: "Saves a customer's invoice as a draft (✎) at version 1 (FR-AR-03), without a number: it takes the next from the " +
			"invoice sequence when it is issued. A draft is checked for its form only; getInvoice shows what issuing it would post, " +
			"and issueInvoice checks it all. Side effects: a draft document and its audit event; nothing posts until issueInvoice. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, formCodes...),
		func(ctx context.Context, in *createInvoiceInput) (*invoiceOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Invoice, error) {
				v, err := a.invoiceView(ctx, u)
				if err != nil {
					return Invoice{}, err
				}
				d, err := invoiceDraftOf(in.Body, v.base)
				if err != nil {
					return Invoice{}, err
				}
				create := a.invoices.Create
				if in.Body.Kind == "credit_note" {
					create = a.invoices.CreateCredit
				}
				doc, err := create(ctx, p.Organisation, d)
				if err != nil {
					return Invoice{}, err
				}
				return a.invoiceOf(ctx, u, v, doc, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &invoiceOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &invoiceOutput{Status: w.Status, ETag: invoiceETag(w.Body), Location: "/v1/invoices/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
				Body: w.Body}, nil
		})

	register(a, authz.GetInvoice, huma.Operation{
		OperationID: "getInvoice", Method: http.MethodGet, Path: "/v1/invoices/{invoice_id}", Tags: []string{"Sales"},
		Summary: "Get an invoice",
		Description: "Returns an invoice's current version with its ETag: its customer, lines with their nets, its GST by code and " +
			"totals, its approval, ledger, delivery, settlement and due states, the monthly releases of its spread lines (FR-GL-03), " +
			"what stops a draft being issued (✖), the lines issuing it writes or wrote (ledger_impact), and the pending request to " +
			"issue, void or edit it. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *InvoicePath) (*invoiceReadOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var i Invoice
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			v, err := a.invoiceView(ctx, u)
			if err != nil {
				return err
			}
			i, err = a.invoiceOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &invoiceReadOutput{ETag: invoiceETag(i), Body: i}, nil
	})

	register(a, authz.ReplaceInvoice, huma.Operation{
		OperationID: "replaceInvoice", Method: http.MethodPut, Path: "/v1/invoices/{invoice_id}", Tags: []string{"Sales"},
		Summary: "Change a draft invoice",
		Description: "Saves the draft's new content as its next version, recording who saved it; the earlier versions are kept. Only a " +
			"draft changes this way; an issued invoice is edited with editInvoice. Side effects: the new version and its audit event. " +
			"Needs If-Match with the draft's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, invoice.CodeNotInvoice, invoice.CodeNotDraft, invoice.CodeBadDraft}, formCodes...),
		func(ctx context.Context, in *replaceInvoiceInput) (*invoiceOutput, error) {
			id, err := parseID("invoice_id", in.InvoiceID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Invoice, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return Invoice{}, err
				}
				v, err := a.invoiceView(ctx, u)
				if err != nil {
					return Invoice{}, err
				}
				before, err := a.invoiceOf(ctx, u, v, doc, false)
				if err != nil {
					return Invoice{}, err
				}
				if err := checkIfMatch(in.IfMatch, invoiceETag(before)); err != nil {
					return Invoice{}, err
				}
				if err := sameKind(&in.Body, before); err != nil {
					return Invoice{}, err
				}
				d, err := invoiceDraftOf(in.Body, v.base)
				if err != nil {
					return Invoice{}, err
				}
				after, err := a.invoices.Revise(ctx, p.Organisation, id, d)
				if err != nil {
					return Invoice{}, err
				}
				return a.invoiceOf(ctx, u, v, after, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &invoiceOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &invoiceOutput{Status: http.StatusOK, ETag: invoiceETag(w.Body), Body: w.Body}, nil
		})

	issueCodes := append(append([]string{invoice.CodeNotDraft, invoice.CodeNotPosted, invoice.CodeSettled, CodeInvoiceNeedsApproval, CodeInvoiceVoidApproval,
		CodeMalformed, CodeTooLarge, CodeMediaType, fx.CodeNoRate}, postingCodes()...), append(InvoiceCodes, readCodes...)...)

	register(a, authz.IssueInvoice, huma.Operation{
		OperationID: "issueInvoice", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/issue", Tags: []string{"Sales"},
		Summary: "Issue a draft invoice",
		Description: "Issues a draft invoice: gives it the next number from the invoice sequence and posts it under the rule invoice@1 " +
			"(FR-AR-03): Dr 1100 with the total, Cr each line's revenue account with its net (2400 for a spread line), Cr 2200 with " +
			"the output GST once the organisation is GST-registered (RND-GST-EXCL), and for an invoice in another currency the " +
			"conversion's difference to 7030, written with the invoice's change to posted and its number in one transaction (ENG-02). " +
			"Every problem comes back together, and a refusal uses no number. Approval as for bills: an invoice whose total in the base " +
			"currency is above the self-approval threshold is refused (INVOICE-10): ask with requestInvoiceIssue; at or below it, a " +
			"Founder-mode issue is logged as self-approved for the weekly review (FM-2, FM-3). Side effects: the number, the entry, a " +
			"new ledger revision, balances, audit events. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview posts nothing.",
	}, issueCodes, func(ctx context.Context, in *invoiceCommandInput) (*invoicePostingOutput, error) {
		return a.invoiceCommand(ctx, in.InvoiceID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Entry, error) {
			self, total, err := a.gateInvoiceIssue(ctx, u, p, id)
			if err != nil {
				return storage.Entry{}, err
			}
			e, err := a.invoices.Post(ctx, p.Organisation, id)
			if err == nil && self {
				err = a.logInvoiceSelfApproval(ctx, u, p, id, total)
			}
			return e, err
		})
	})

	register(a, authz.VoidInvoice, huma.Operation{
		OperationID: "voidInvoice", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/void", Tags: []string{"Sales"},
		Summary: "Void an issued invoice",
		Description: "Voids an issued invoice with a reversing entry on the date given, with a reason (FR-GL-12); posted history never " +
			"changes, and the invoice keeps its number. An invoice that receipts or credits settle is voided only once they are " +
			"(INVOICE-13). In Founder mode voiding needs a second person (FM-1): it is refused (INVOICE-11), and requestInvoiceVoid " +
			"asks for the approval. Side effects: the reversing entry, a new ledger revision, balances, audit events with the reason. " +
			"Needs If-Match with the invoice's ETag. Idempotent with Idempotency-Key.",
	}, issueCodes, func(ctx context.Context, in *invoiceVoidInput) (*invoicePostingOutput, error) {
		date, err := time.Parse(time.DateOnly, in.Body.Date)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		return a.invoiceCommand(ctx, in.InvoiceID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Entry, error) {
			if err := gateInvoiceVoid(ctx, u, id); err != nil {
				return storage.Entry{}, err
			}
			return a.invoices.Void(ctx, p.Organisation, id, date, in.Body.Reason)
		})
	})

	requestCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, invoice.CodeNotInvoice, invoice.CodeNotDraft,
		invoice.CodeNotPosted, invoice.CodeBadDraft, invoice.CodeSettled, storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge,
		CodeMediaType, CodeInvalid, fx.CodeNoRate}, append(InvoiceCodes, LedgerCodes...)...)

	register(a, authz.RequestInvoiceIssue, huma.Operation{
		OperationID: "requestInvoiceIssue", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/issue-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to issue an invoice", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to issue a draft invoice at the version read (FM-2, SOD-3): what issueInvoice " +
			"asks for when the invoice's total is above the self-approval threshold (INVOICE-10). The answer is 202 with an approval " +
			"request, which a person who holds A on \"Invoices, credit notes\", did not ask and did not prepare the invoice approves, " +
			"stepped up; on the approval the invoice is issued, as the approver. The request holds the invoice's version, status and " +
			"total, so a change to the draft voids it. Asking needs a person signed in who passed step-up, and If-Match with the " +
			"invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *invoiceCommandInput) (*invoiceRequestOutput, error) {
		return a.invoiceRequest(ctx, in.InvoiceID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, storage.DocumentDraft,
			func(doc storage.Document, i Invoice, receivable string) pendingChange {
				return pendingChange{ObjectType: approval.ObjectInvoice, ObjectKey: doc.ID.String(), Operation: approval.OpInvoicePost,
					Summary: fmt.Sprintf("Issue %s, total %s %s", invoiceName(doc, i), receivable, i.Receivable.Currency),
					Execute: map[string]any{"invoice_id": doc.ID.String(), "version": fmt.Sprint(doc.Version)},
					Diff:    []ApprovalDiff{{Field: "status", Before: "draft", After: "posted"}, {Field: "receivable", After: receivable}}}
			})
	})

	register(a, authz.RequestInvoiceVoid, huma.Operation{
		OperationID: "requestInvoiceVoid", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/void-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to void an invoice", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void an issued invoice with a reversing entry on the date given, with a " +
			"reason (FR-GL-12, FM-1): what voidInvoice asks for in Founder mode (INVOICE-11). The answer is 202 with an approval " +
			"request, which a person who holds A on \"Invoices, credit notes\" and did not ask approves, stepped up; on the approval the " +
			"reversal posts. Asking needs a person signed in who passed step-up, and If-Match with the invoice's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *invoiceVoidInput) (*invoiceRequestOutput, error) {
		if _, err := time.Parse(time.DateOnly, in.Body.Date); err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		reason := strings.TrimSpace(in.Body.Reason)
		return a.invoiceRequest(ctx, in.InvoiceID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, storage.DocumentPosted,
			func(doc storage.Document, i Invoice, receivable string) pendingChange {
				return pendingChange{ObjectType: approval.ObjectInvoice, ObjectKey: doc.ID.String(), Operation: approval.OpInvoiceVoid,
					Summary: fmt.Sprintf("Void %s on %s: %s", invoiceName(doc, i), in.Body.Date, reason),
					Execute: map[string]any{"invoice_id": doc.ID.String(), "version": fmt.Sprint(doc.Version), "date": in.Body.Date, "reason": reason},
					Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "void_date", After: in.Body.Date}}}
			})
	})

	editCodes := append([]string{CodeInvoiceEditApproval, storage.CodeDocumentEdit, CodeInvalid}, issueCodes...)

	register(a, authz.EditInvoice, huma.Operation{
		OperationID: "editInvoice", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/edit", Tags: []string{"Sales"},
		Summary: "Edit an issued invoice",
		Description: "Corrects an issued invoice in an open period (FR-GL-12): saves the content sent as its next version, reverses the " +
			"entry it had on that entry's own date, and posts the new version's entry, in one transaction; the invoice stays issued, " +
			"keeps its number, and records who posted the edit. An invoice that receipts or credits settle is edited only once they " +
			"are voided (INVOICE-13). An edit needs the approval a void needs: in Founder mode it is refused (INVOICE-14), and in any " +
			"mode a new version whose total is above the self-approval threshold is refused (INVOICE-10); requestInvoiceEdit asks for " +
			"the approval. Side effects: a new version, two entries, a new ledger revision, balances, audit events with the reason. " +
			"Needs If-Match with the invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview shows the two entries " +
			"and saves nothing.",
	}, editCodes, func(ctx context.Context, in *invoiceEditInput) (*invoiceEditOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the invoice is edited"}})
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvoiceEdited, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return InvoiceEdited{}, err
			}
			v, err := a.invoiceView(ctx, u)
			if err != nil {
				return InvoiceEdited{}, err
			}
			before, err := a.invoiceOf(ctx, u, v, doc, false)
			if err != nil {
				return InvoiceEdited{}, err
			}
			if err := checkIfMatch(in.IfMatch, invoiceETag(before)); err != nil {
				return InvoiceEdited{}, err
			}
			if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
				return InvoiceEdited{}, err
			}
			if err := sameKind(&in.Body.InvoiceDraft, before); err != nil {
				return InvoiceEdited{}, err
			}
			d, err := invoiceDraftOf(in.Body.InvoiceDraft, v.base)
			if err != nil {
				return InvoiceEdited{}, err
			}
			if err := a.gateInvoiceEdit(ctx, u, p, doc, d); err != nil {
				return InvoiceEdited{}, err
			}
			res, err := a.invoices.Edit(ctx, p.Organisation, id, d, reason, "")
			if err != nil {
				return InvoiceEdited{}, invoiceProblems(err)
			}
			after, err := u.Documents().Get(ctx, id)
			if err != nil {
				return InvoiceEdited{}, err
			}
			i, err := a.invoiceOf(ctx, u, v, after, false)
			return InvoiceEdited{Invoice: i, Reversal: entryOf(res.Reversal), Entry: entryOf(res.Entry)}, err
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &invoiceEditOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &invoiceEditOutput{Status: w.Status, ETag: invoiceETag(w.Body.Invoice), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.RequestInvoiceEdit, huma.Operation{
		OperationID: "requestInvoiceEdit", Method: http.MethodPost, Path: "/v1/invoices/{invoice_id}/edit-approval", Tags: []string{"Sales"},
		Summary: "Ask for approval to edit an issued invoice", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to edit an issued invoice with the content sent (FR-GL-12, FM-1, FM-2): what " +
			"editInvoice asks for in Founder mode (INVOICE-14) or above the self-approval threshold (INVOICE-10). The content must " +
			"pass the posting rules. The answer is 202 with an approval request whose changes list each field and line that changes " +
			"and the total receivable before and after; it keeps the content, and its envelope holds the content's SHA-256 with the " +
			"invoice's version, status and total, so any change to the invoice voids it. A person who holds A on \"Invoices, credit " +
			"notes\" and did not ask approves it, stepped up; on the approval the edit posts as the approver, with the requester as " +
			"the one who prepared the new version. Asking needs a person signed in who passed step-up, and If-Match with the " +
			"invoice's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *invoiceEditInput) (*invoiceRequestOutput, error) {
		id, err := parseID("invoice_id", in.InvoiceID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		reason := strings.TrimSpace(in.Body.Reason)
		if reason == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the invoice is edited"}})
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return Approval{}, err
			}
			v, err := a.invoiceView(ctx, u)
			if err != nil {
				return Approval{}, err
			}
			i, err := a.invoiceOf(ctx, u, v, doc, false)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, invoiceETag(i)); err != nil {
				return Approval{}, err
			}
			if doc.Status != storage.DocumentPosted {
				return Approval{}, problem(invoice.CodeNotPosted, fmt.Sprintf("The invoice is %s.", doc.Status), "Ask for approval of an invoice in the state it needs")
			}
			if i.Open != nil && i.Total != nil && i.Open.Amount != i.Total.Amount {
				return Approval{}, problem(invoice.CodeSettled, "Receipts or credits settle the invoice.", "Void them first, then edit it")
			}
			if err := sameKind(&in.Body.InvoiceDraft, i); err != nil {
				return Approval{}, err
			}
			d, err := invoiceDraftOf(in.Body.InvoiceDraft, v.base)
			if err != nil {
				return Approval{}, err
			}
			c, err := a.invoices.Check(ctx, p.Organisation, doc.ID, &d)
			if err != nil {
				return Approval{}, err
			}
			if len(c.Problems) > 0 {
				return Approval{}, invoiceProblems(c.Problems)
			}
			receivable, err := a.invoiceReceivable(ctx, u, v, doc)
			if err != nil {
				return Approval{}, err
			}
			content, sum, err := contentOfInvoice(in.Body.InvoiceDraft)
			if err != nil {
				return Approval{}, err
			}
			diff, err := invoiceEditDiff(ctx, u, i, in.Body.InvoiceDraft, receivable, c.Receivable.String())
			if err != nil {
				return Approval{}, err
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectInvoice, ObjectKey: doc.ID.String(), Operation: approval.OpInvoiceEdit,
				Summary: fmt.Sprintf("Edit %s: %s", invoiceName(doc, i), reason),
				Execute: map[string]any{"invoice_id": doc.ID.String(), "version": fmt.Sprint(doc.Version), "content_sha256": sum,
					"reason": reason, "prepared_by": (storage.Actor{Type: "user", ID: p.UserID.String()}).String()},
				Content: content, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &invoiceRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
