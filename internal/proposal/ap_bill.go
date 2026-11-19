package proposal

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ap.draft_bill (PRD-001 Appendix E, FR-AP-04, FR-AP-12; p1-ai-proposals task 1.1): a draft bill of a supplier's document, made through the
// same function a person's draft and a captured document's draft use (journal.CreateBillIn), so the three are the same record with the same
// checks. It is tier 1: a draft posts nothing and moves no money, and the bill follows the ordinary approval policies when a person posts it
// (AI-30). Its input-tax facts stay as a person would leave them, to confirm (FR-AP-20, G4-029): the payload has no field that decides
// eligibility.

// MaxBillLines is how many lines one draft bill proposal holds.
const MaxBillLines = 50

// BillLine is one line of a draft bill, as ap.draft_bill's payload and a captured document's draft carry it: the account by its code, its
// net and the GST the document states, both in the bill's currency.
type BillLine struct {
	Account     string `json:"account"`
	Description string `json:"description"`
	Net         string `json:"net"`
	GSTCode     string `json:"gst_code"`
	GST         string `json:"gst"`
}

// BillDraftPayload is ap.draft_bill's payload.
type BillDraftPayload struct {
	SupplierID  string     `json:"supplier_id"`
	SupplierRef string     `json:"supplier_ref"`
	Date        string     `json:"date"`
	DueDate     string     `json:"due_date"`
	Currency    string     `json:"currency"`
	Lines       []BillLine `json:"lines"`
	Notes       string     `json:"notes"`
	// Document is the fact the document states about itself, for the person who confirms it (FR-AP-20, G4-029): never what is claimable. Left out, it is none: an
	// unknown fact claims nothing.
	Document string `json:"document"`
}

func supplierKey(id uuid.UUID) string { return "supplier:" + id.String() }

// BillDraftOf reads a payload against the records into the draft a person would have typed, refusing with the field and the fix; the supplier it
// names is returned for the dry run's summary and the base version.
func BillDraftOf(ctx context.Context, u storage.UnitOfWork, d BillDraftPayload) (bill.Draft, storage.Contact, error) {
	var out bill.Draft
	id, err := uuid.Parse(d.SupplierID)
	if err != nil {
		return out, storage.Contact{}, refusal("payload.supplier_id is not an ID", "Use the supplier's contact ID as records_search returned it")
	}
	supplier, err := u.Contacts().Get(ctx, id)
	if err != nil || !supplier.Vendor || !supplier.Active {
		return out, storage.Contact{}, refusal("payload.supplier_id is not an active supplier", "Use the ID of an active contact who is a supplier; a new supplier is added by a person first")
	}
	out.SupplierID, out.SupplierRef, out.Notes = id, strings.TrimSpace(d.SupplierRef), d.Notes
	if out.InputTax.Document = d.Document; out.InputTax.Document == "" {
		out.InputTax.Document = bill.DocNone
	}
	if !slices.Contains([]string{bill.DocTaxInvoice, bill.DocSimplified, bill.DocNone}, out.InputTax.Document) {
		return out, storage.Contact{}, refusal("payload.document is not tax_invoice, simplified or none", "Say what the document states itself to be, or leave it out when you are not sure")
	}
	if out.Date, err = time.Parse(time.DateOnly, d.Date); err != nil {
		return out, supplier, refusal("payload.date is not a date", "Send the bill's date as YYYY-MM-DD, as the document prints it")
	}
	if d.DueDate != "" {
		if out.DueDate, err = time.Parse(time.DateOnly, d.DueDate); err != nil {
			return out, supplier, refusal("payload.due_date is not a date", "Send the due date as YYYY-MM-DD, or leave it out for the supplier's terms")
		}
	}
	if out.Currency, err = money.Lookup(d.Currency); err != nil {
		return out, supplier, refusal("payload.currency is not a currency Fyniq knows", "Send an ISO 4217 code such as SGD")
	}
	if len(d.Lines) == 0 || len(d.Lines) > MaxBillLines {
		return out, supplier, refusal(fmt.Sprintf("payload.lines holds %d lines", len(d.Lines)), fmt.Sprintf("Send 1 to %d lines", MaxBillLines))
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return out, supplier, err
	}
	codes, err := bill.PurchaseCodes(ctx, u, out.Date)
	if err != nil {
		return out, supplier, err
	}
	for i, l := range d.Lines {
		at := fmt.Sprintf("payload.lines[%d].", i)
		ai := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.Code == l.Account })
		if ai < 0 || accounts[ai].Group || !accounts[ai].Active || accounts[ai].Type == storage.Revenue || accounts[ai].Type == storage.Equity {
			return out, supplier, refusal(at+"account is not the code of an active account a bill can cost", "Use the code of an active expense or asset account as records_search returned it")
		}
		if len(codes.Codes) > 0 && !slices.ContainsFunc(codes.Codes, func(c pack.GSTCode) bool { return c.Code == l.GSTCode }) {
			return out, supplier, refusal(at+"gst_code is not a purchase GST code of the organisation", "Use one of the purchase GST codes, such as TX or NR")
		}
		net, err := money.Parse(l.Net, out.Currency)
		if err != nil {
			return out, supplier, refusal(at+"net is not an amount in "+out.Currency.Code, "Send the line's net as a decimal string such as 100.00")
		}
		gst, err := money.Parse(l.GST, out.Currency)
		if err != nil {
			return out, supplier, refusal(at+"gst is not an amount in "+out.Currency.Code, "Send the GST the document states as a decimal string, 0.00 for none")
		}
		out.Lines = append(out.Lines, bill.Line{AccountID: accounts[ai].ID, Description: l.Description, Net: net, GSTCode: l.GSTCode, GST: gst})
	}
	return out, supplier, nil
}

// DraftBill is ap.draft_bill's command: a draft bill of a supplier's document for a person to review and post. The dry run makes the draft in
// the unit of work that is rolled back and shows what posting it would do and what would stop it; executed, it makes the draft only while the
// supplier is as the dry run read it (AI-P5).
type DraftBill struct{}

// Policy implements Command: the right to enter a bill.
func (DraftBill) Policy() authz.Policy { return authz.CreateBill }

// Measure implements Command: one draft, no amount (a draft moves no money).
func (DraftBill) Measure(payload json.RawMessage) (Measure, error) {
	var d BillDraftPayload
	if err := json.Unmarshal(payload, &d); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command.
func (DraftBill) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var d BillDraftPayload
	if err := json.Unmarshal(payload, &d); err != nil {
		return Impact{}, err
	}
	draft, supplier, err := BillDraftOf(ctx, u, d)
	if err != nil {
		return Impact{}, err
	}
	doc, err := journal.CreateBillIn(ctx, u, draft)
	if err != nil {
		return Impact{}, billRefusal(err)
	}
	checked, err := journal.CheckBill(ctx, u, doc)
	if err != nil {
		return Impact{}, err
	}
	total, err := draft.Total()
	if err != nil {
		return Impact{}, err
	}
	lines := make([]map[string]any, 0, len(draft.Lines))
	for _, l := range draft.Lines {
		lines = append(lines, map[string]any{"description": l.Description, "net": l.Net.String(), "gst_code": l.GSTCode, "gst": l.GST.String()})
	}
	var stops, warns, tax []string
	for _, x := range checked.Prepared.InputTax {
		tax = append(tax, x.Text)
	}
	for _, p := range checked.Problems {
		stops = append(stops, p.Field+": "+p.Message)
	}
	for _, w := range checked.Prepared.Warnings {
		warns = append(warns, w.Message)
	}
	changes, err := json.Marshal(map[string]any{"bills": []map[string]any{{"change": "draft", "supplier": supplier.LegalName, "supplier_ref": draft.SupplierRef, "currency": draft.Currency.Code,
		"date": draft.Date.Format(time.DateOnly), "lines": lines, "total": total.String(), "input_tax": tax, "stops_posting": stops, "warnings": warns}},
		"ledger": "A draft posts nothing: the bill is checked and posted, and its approval policy applies, by a person. Its input tax stays to confirm"})
	if err != nil {
		return Impact{}, err
	}
	return Impact{Summary: fmt.Sprintf("Drafts a bill from %s for %s %s, dated %s; nothing is posted and its input tax is left to confirm", supplier.LegalName, draft.Currency.Code, total.String(),
		draft.Date.Format(time.DateOnly)), Changes: changes, Base: map[string]int64{supplierKey(supplier.ID): int64(supplier.Version)}}, nil
}

// Execute implements Command.
func (DraftBill) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	var d BillDraftPayload
	if err := json.Unmarshal(payload, &d); err != nil {
		return Execution{}, err
	}
	draft, supplier, err := BillDraftOf(ctx, u, d)
	if err != nil {
		return Execution{}, err
	}
	want, ok := base[supplierKey(supplier.ID)]
	if !ok {
		return Execution{}, fmt.Errorf("the dry run did not read %s", supplierKey(supplier.ID))
	}
	if int64(supplier.Version) != want {
		return Execution{}, ErrStale // the supplier changed since the dry run
	}
	doc, err := journal.CreateBillIn(ctx, u, draft)
	if err != nil {
		return Execution{}, billRefusal(err)
	}
	return Execution{Records: []string{"bill:" + doc.ID.String()}, Entries: []string{}}, nil
}

// billRefusal is a bill's own refusal in the proposal's words.
func billRefusal(err error) error {
	if be, ok := err.(*bill.Error); ok {
		return refusal(be.Message, be.Fix)
	}
	return err
}
