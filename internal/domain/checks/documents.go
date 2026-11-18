package checks

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D rows whose view opens the documents' items (design D7).
const (
	viewInvoices = "Invoices, credit notes"
	viewBills    = "Bills, vendor credits"
)

// The audit object types whose change reruns the documents' checks (design D5).
var (
	onSales     = []string{"document", "journal_entry", "period", "period_close", "period_reopen", "contact", "item", "fx_rate", "account", "quote_outcome", "gst_registration"}
	onPurchases = []string{"document", "journal_entry", "period", "period_close", "period_reopen", "contact", "fx_rate", "account", "evidence", "withholding_decision", "gst_registration"}
	onJournals  = []string{"document", "journal_entry", "period", "period_close", "period_reopen", "account", "fx_rate"}
)

// overdueDays is how long past its due date an invoice is flagged (C.1 #15); repayAfter how long past its due date an
// unpaid bill's claimed input tax is to be repaid (C.2 #48: twelve months, IRAS).
const (
	overdueDays = 90
	repayAfter  = 12 // months
)

// followUpDays is how close to its validity date a sent quote with no answer is to follow up (C.1 #25).
const followUpDays = 7

// The checks of the sales, purchases and journals: p1-checks slice 2 (tasks 2.3 to 2.6).
var documentChecks = []Definition{
	{Key: "sales.invoices-cannot-post", Ref: "C.1 #13", Area: "Sales", Worst: Error, Title: "Every draft invoice can be issued",
		Explain: "A draft invoice or credit note the rules refuse cannot be issued, so the month's sales are incomplete until " +
			"it is fixed or deleted. Only drafts dated in the month to close next or before count.",
		Rules: "ENG-21, FR-AR-03", Owner: "accountant", View: viewInvoices, Qualifier: "sales", Triggers: onSales,
		Run: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
			return draftsThatCannotPost(ctx, u, e, []storage.DocumentType{storage.DocumentInvoice, storage.DocumentCreditNote}, "invoice",
				func(doc storage.Document) (ledger.Problems, error) {
					c, err := journal.CheckInvoice(ctx, u, doc)
					return c.Problems, err
				})
		}},
	{Key: "sales.invoices-to-review", Ref: "C.1 #14", Area: "Sales", Worst: Warning, Title: "No issued invoice waits for a review",
		Explain: "An invoice issued with a warning posted, but someone should look at it: a tax invoice issued more than 30 days " +
			"after its date (FR-AR-05). Mark it reviewed once looked at. Invoices of closed months were reviewed at the close.",
		Rules: "ENG-21, FR-AR-05", Owner: "accountant", View: viewInvoices, Qualifier: "sales", Triggers: onSales, Run: invoicesToReview},
	{Key: "sales.invoices-overdue", Ref: "C.1 #15", Area: "Sales", Worst: Warning, Title: "No invoice is overdue by more than 90 days",
		Explain: "An invoice more than 90 days past its due date is unlikely to be paid without a call: chase it, agree a plan, " +
			"or write it off (FR-AR-11).",
		Rules: "FR-AR-10, FR-AR-11", Owner: "accountant", View: viewInvoices, Qualifier: "sales", Triggers: onSales, Run: invoicesOverdue},
	{Key: "sales.document-lines", Ref: "C.1 #24", Area: "Sales", Worst: Warning, Title: "Sales documents' lines have no problems",
		Explain: "A line worth nothing, or naming an item no longer sold, on a draft invoice or an open quote or order is usually " +
			"a slip (rev 0.164).",
		Rules: "FR-AR-01, FR-ITM-01", Owner: "accountant", View: viewInvoices, Qualifier: "sales", Triggers: onSales, Run: salesLines},
	{Key: "sales.quotes-to-follow-up", Ref: "C.1 #25", Area: "Sales", Worst: Warning, Title: "No quote or order waits for a follow-up",
		Explain: "A quote sent with no answer whose validity ends within 7 days, or has ended, is worth a call; an accepted quote " +
			"or order not yet invoiced in full is work done or due to bill.",
		Rules: "FR-AR-01, FR-AR-02", Owner: "accountant", View: viewInvoices, Qualifier: "sales", Triggers: onSales, Run: quotesToFollowUp},
	{Key: "purchases.bills-cannot-post", Ref: "C.1 #16", Area: "Purchases", Worst: Error, Title: "Every draft bill can post",
		Explain: "A draft bill or vendor credit the rules refuse cannot post, so the month's costs are incomplete until it is " +
			"fixed or deleted. Only drafts dated in the month to close next or before count.",
		Rules: "ENG-21, FR-AP-02", Owner: "accountant", View: viewBills, Qualifier: "purchases", Triggers: onPurchases,
		Run: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
			return draftsThatCannotPost(ctx, u, e, []storage.DocumentType{storage.DocumentBill, storage.DocumentVendorCredit}, "bill",
				func(doc storage.Document) (ledger.Problems, error) {
					c, err := journal.CheckBill(ctx, u, doc)
					return c.Problems, err
				})
		}},
	{Key: "purchases.bills-to-review", Ref: "C.1 #17", Area: "Purchases", Worst: Warning, Title: "No posted bill waits for a review",
		Explain: "A bill posted with a warning is in the books, but someone should look at it: GST that differs from the " +
			"rounding rule, input tax waiting for the tax agent, a withholding decision not recorded. Bills of closed months " +
			"were reviewed at the close; possible duplicates are C.2 #10's.",
		Rules: "ENG-21, FR-AP-19, FR-AP-20", Owner: "accountant", View: viewBills, Qualifier: "purchases", Triggers: onPurchases, Run: billsToReview},
	{Key: "purchases.possible-duplicates", Ref: "C.2 #10", Area: "Purchases", Worst: Warning, Title: "No bill looks like another",
		Explain: "Two bills of one supplier with the same number and total within the window may be one bill entered twice: " +
			"void one, or mark it reviewed if both are real.",
		Rules: "FR-AP-02", Owner: "accountant", View: viewBills, Qualifier: "purchases", Triggers: onPurchases, Run: duplicateBills},
	{Key: "purchases.without-a-document", Ref: "C.2 #20", Area: "Purchases", Worst: Warning, Title: "Every posted bill and claim has its document",
		Explain: "A posted bill or claim needs its receipt or invoice (FR-DOC-06): entry asks for one, so this catches the " +
			"imported and migrated ones, whatever their amount.",
		Rules: "FR-DOC-06", Owner: "accountant", View: viewBills, Qualifier: "purchases", Triggers: onPurchases, Run: withoutADocument},
	{Key: "purchases.input-tax-to-repay", Ref: "C.2 #48", Area: "Purchases", Worst: Warning, Title: "No unpaid bill's input tax is due back",
		Explain: "Input tax claimed on a bill still unpaid 12 months after its due date must be repaid to IRAS in the return " +
			"after those 12 months end [accountant to confirm].",
		Rules: "G4-037, IRAS", Owner: "accountant", View: viewBills, Qualifier: "purchases", Triggers: onPurchases, Run: inputTaxToRepay},
	{Key: "journals.problems", Ref: "C.1 #18", Area: "Journals", Worst: Error, Title: "Every journal can post, and none waits for a review",
		Explain: "A draft journal the rules refuse cannot post (✖), so the month is incomplete until it is fixed or deleted; a " +
			"posted journal whose automatic reversal waits for a period to open is for review (⚠). Only drafts dated in the " +
			"month to close next or before count.",
		Rules: "ENG-20, ENG-21, FR-GL-02", Owner: "accountant", View: viewJournals, Triggers: onJournals, Run: journalProblems},
}

// cutoff is the last day a draft counts by (C.1 #13, #16, #18): the end of the month to close next, else today.
func (e *Env) cutoff() time.Time {
	if p := e.ToClose(); p != nil && p.EndsOn.After(e.Today) {
		return p.EndsOn
	}
	return e.Today
}

// openSpan are the first and last days of the periods not closed: the documents of closed months were reviewed at the
// close. ok is false when every period is closed.
func (e *Env) openSpan() (from, to string, ok bool) {
	for _, p := range e.Periods {
		if p.Status == storage.PeriodClosed {
			continue
		}
		if !ok || p.StartsOn.Format(time.DateOnly) < from {
			from = p.StartsOn.Format(time.DateOnly)
		}
		if !ok || p.EndsOn.Format(time.DateOnly) > to {
			to = p.EndsOn.Format(time.DateOnly)
		}
		ok = true
	}
	return from, to, ok
}

// documents are the documents of types with status dated from to (inclusive, "" for no bound), read a page at a time.
func documents(ctx context.Context, u storage.UnitOfWork, types []storage.DocumentType, status storage.DocumentStatus, from, to string) ([]storage.Document, error) {
	var out []storage.Document
	for _, t := range types {
		if err := eachDocumentPage(ctx, u, t, status, from, to, func(page []storage.Document) error {
			out = append(out, page...)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// eachDocumentPage calls each with the documents of a type with status dated from to, a page of 500 at a time, so a
// check that needs no more than a page at once holds no more (NFR-PERF-01).
func eachDocumentPage(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, status storage.DocumentStatus, from, to string,
	each func([]storage.Document) error) error {
	f := storage.DocumentFilter{Type: t, Status: status, From: from, To: to, Limit: 500}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return err
		}
		if err := each(page); err != nil {
			return err
		}
		if len(page) < f.Limit {
			return nil
		}
		last := page[len(page)-1]
		f.AfterDate, f.AfterID = last.Date, &last.ID
	}
}

// docItem is an item naming a document, at its version.
func docItem(kind string, doc storage.Document, who, detail string) Item {
	label := cmp.Or(doc.Number, "Draft")
	if doc.Date != "" {
		label += " of " + doc.Date
	}
	if who != "" {
		label += ", " + who
	}
	return Item{Kind: kind, ID: doc.ID.String(), Label: label, Detail: detail, Version: fmt.Sprint(doc.Version)}
}

// problemsText are a document's problems as one line: the first, and how many more.
func problemsText(ps ledger.Problems) string {
	if len(ps) == 0 {
		return ""
	}
	out := ps[0].Message
	if ps[0].Fix != "" {
		out += ": " + ps[0].Fix
	}
	if len(ps) > 1 {
		out += fmt.Sprintf(" (and %d more)", len(ps)-1)
	}
	return out
}

// draftsThatCannotPost are the drafts of types dated by the cutoff that the rules refuse: ✖ (C.1 #13, #16).
func draftsThatCannotPost(ctx context.Context, u storage.UnitOfWork, e *Env, types []storage.DocumentType, kind string,
	check func(storage.Document) (ledger.Problems, error)) (Result, error) {
	drafts, err := documents(ctx, u, types, storage.DocumentDraft, "", e.cutoff().Format(time.DateOnly))
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, doc := range drafts {
		ps, err := check(doc)
		if err != nil {
			return Result{}, err
		}
		if len(ps) > 0 {
			items = append(items, docItem(kind, doc, "", problemsText(ps)))
		}
	}
	what := map[string]string{"invoice": "invoice(s) or credit note(s)", "bill": "bill(s) or vendor credit(s)"}[kind]
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Each of the %d draft %s dated by %s can post", len(drafts), what, e.cutoff().Format("2 Jan 2006"))), nil
	}
	return Found(Error, items, fmt.Sprintf("%d draft %s cannot post", len(items), what),
		"Open each, fix what it names, or delete a draft that is not needed"), nil
}

// invoicesToReview: invoices issued in the open months more than 30 days after their date, as tax invoices (C.1 #14,
// FR-AR-05).
func invoicesToReview(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	from, to, ok := e.openSpan()
	if !ok {
		return Fine("Every month is closed: their invoices were reviewed at the close"), nil
	}
	docs, err := documents(ctx, u, []storage.DocumentType{storage.DocumentInvoice}, storage.DocumentPosted, from, to)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, doc := range docs {
		if doc.PostedAt == nil {
			continue
		}
		p, _, err := invoice.Prepare(ctx, u, doc, e.Base)
		if err != nil {
			return Result{}, err
		}
		if invoice.Late(p, *doc.PostedAt) {
			items = append(items, docItem("invoice", doc, p.Customer.LegalName, fmt.Sprintf("Issued %s, more than %d days after its date: IRAS expects a tax invoice within %d days of the time of supply",
				doc.PostedAt.Format(time.DateOnly), invoice.LateDays, invoice.LateDays)))
		}
	}
	if len(items) == 0 {
		return Fine("No invoice of the open months was issued with a warning"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d issued invoice(s) to review", len(items)), "Look at each, then mark it reviewed"), nil
}

// controlAccount is the account of a system role, or nil.
func controlAccount(ctx context.Context, u storage.UnitOfWork, role string) (*storage.Account, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if string(a.Role) == role {
			return &a, nil
		}
	}
	return nil, nil
}

// invoicesOverdue: issued invoices with something still open more than 90 days after their due date (C.1 #15).
func invoicesOverdue(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	ar, err := controlAccount(ctx, u, ledger.RoleAccountsReceivable)
	if err != nil {
		return Result{}, err
	}
	if ar == nil {
		return Missing("the accounts receivable account (1100)", "Accounting › Chart of accounts"), nil
	}
	l, err := receivables.Open(ctx, u, e.Base, ar.ID, e.Today)
	if err != nil {
		return Result{}, err
	}
	limit := e.Today.AddDate(0, 0, -overdueDays)
	var items []Item
	for _, c := range l.Customers {
		for _, it := range c.Items {
			if it.Kind != receivables.KindInvoice || it.Open.Sign() <= 0 || it.DueDate.IsZero() || !it.DueDate.Before(limit) {
				continue
			}
			days := int(e.Today.Sub(it.DueDate).Hours() / 24)
			items = append(items, Item{Kind: "invoice", ID: it.DocumentID.String(), Label: cmp.Or(it.Number, "Invoice") + ", " + c.Name,
				Detail:  fmt.Sprintf("%s %s open, due %s: %d days overdue", it.Open, it.Open.Currency().Code, it.DueDate.Format(time.DateOnly), days),
				Version: Fingerprint(it.Open.String())})
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("No invoice is more than %d days overdue", overdueDays)), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d invoice(s) more than %d days overdue", len(items), overdueDays),
		"Chase each, agree a plan, or write it off; mark it reviewed once the customer has been called"), nil
}

// salesLines: on draft invoices and open quotes and orders, a line worth nothing or naming an item no longer sold
// (C.1 #24, rev 0.164).
func salesLines(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	inactive := map[uuid.UUID]string{}
	for after := ""; ; {
		page, err := u.Items().Page(ctx, after, 500)
		if err != nil {
			return Result{}, err
		}
		for _, it := range page {
			if !it.Active {
				inactive[it.ID] = cmp.Or(it.Name, it.Code)
			}
		}
		if len(page) < 500 {
			break
		}
		after = page[len(page)-1].Code
	}
	var items []Item
	look := func(kind string, doc storage.Document, lines []invoice.Line) {
		var found []string
		for i, l := range lines {
			switch {
			case l.Quantity.IsZero() || l.UnitPrice.Sign() == 0:
				found = append(found, fmt.Sprintf("line %d is worth nothing", i+1))
			case l.ItemID != nil && inactive[*l.ItemID] != "":
				found = append(found, fmt.Sprintf("line %d is %s, which is no longer sold", i+1, inactive[*l.ItemID]))
			}
		}
		if len(found) > 0 {
			items = append(items, docItem(kind, doc, "", strings.Join(found, "; ")))
		}
	}
	drafts, err := documents(ctx, u, []storage.DocumentType{storage.DocumentInvoice}, storage.DocumentDraft, "", "")
	if err != nil {
		return Result{}, err
	}
	for _, doc := range drafts {
		d, err := invoice.Read(doc, e.Base)
		if err != nil {
			return Result{}, err
		}
		look("invoice", doc, d.Lines)
	}
	open, err := openQuotes(ctx, u, e)
	if err != nil {
		return Result{}, err
	}
	for _, q := range open {
		look(string(q.doc.Type), q.doc, q.draft.Content.Lines)
	}
	if len(items) == 0 {
		return Fine("No draft invoice or open quote has a line worth nothing or an item no longer sold"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d sales document(s) with a line to look at", len(items)),
		"Correct the line, or mark the document reviewed if it is meant"), nil
}

// openQuote is a quote or order sent or accepted, with its status and invoicing.
type openQuote struct {
	doc    storage.Document
	draft  quote.Draft
	status string
	states []quote.LineState
}

// openQuotes are the quotes and orders sent with no answer yet, or accepted and not invoiced in full.
func openQuotes(ctx context.Context, u storage.UnitOfWork, e *Env) ([]openQuote, error) {
	docs, err := documents(ctx, u, []storage.DocumentType{storage.DocumentQuote, storage.DocumentSalesOrder}, storage.DocumentPosted, "", "")
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	outcomes, err := u.QuoteOutcomes().Of(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []openQuote
	for _, doc := range docs {
		var outcome *storage.QuoteOutcome
		if o, ok := outcomes[doc.ID]; ok {
			outcome = &o
		}
		status := quote.Status(doc, outcome)
		if status != quote.StatusSent && status != quote.StatusAccepted {
			continue
		}
		d, err := quote.Read(doc, e.Base)
		if err != nil {
			return nil, err
		}
		states, _, err := quote.Invoicing(ctx, u, doc.ID, d, e.Base, uuid.Nil)
		if err != nil {
			return nil, err
		}
		if status = quote.StatusOf(doc, outcome, states); status == quote.StatusSent || status == quote.StatusAccepted {
			out = append(out, openQuote{doc, d, status, states})
		}
	}
	return out, nil
}

// quotesToFollowUp: a quote or order sent with no answer whose validity ends within 7 days or has ended, or accepted
// and not invoiced in full (C.1 #25).
func quotesToFollowUp(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	open, err := openQuotes(ctx, u, e)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	soon := e.Today.AddDate(0, 0, followUpDays)
	for _, q := range open {
		kind := string(q.doc.Type)
		switch {
		case q.status == quote.StatusAccepted:
			items = append(items, Item{Kind: kind, ID: q.doc.ID.String(), Label: docItem(kind, q.doc, "", "").Label,
				Detail: "Accepted, and not invoiced in full: invoice what is delivered", Version: Fingerprint(fmt.Sprintf("accepted %d %v", q.doc.Version, q.states))})
		case !q.draft.ValidUntil.IsZero() && !q.draft.ValidUntil.After(soon):
			when := "ends " + q.draft.ValidUntil.Format(time.DateOnly)
			if q.draft.ValidUntil.Before(e.Today) {
				when = "ended " + q.draft.ValidUntil.Format(time.DateOnly)
			}
			items = append(items, Item{Kind: kind, ID: q.doc.ID.String(), Label: docItem(kind, q.doc, "", "").Label,
				Detail: "Sent with no answer; its validity " + when + ": ask the customer", Version: Fingerprint(fmt.Sprintf("sent %d", q.doc.Version))})
		}
	}
	if len(items) == 0 {
		return Fine("No quote or order waits for a follow-up"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d quote(s) or order(s) to follow up", len(items)),
		"Ask the customer, or invoice what is delivered; mark it reviewed once followed up"), nil
}

// postedBills are the bills posted in the open months, each prepared as it posted.
func postedBills(ctx context.Context, u storage.UnitOfWork, e *Env) ([]storage.Document, []bill.Prepared, error) {
	from, to, ok := e.openSpan()
	if !ok {
		return nil, nil, nil
	}
	docs, err := documents(ctx, u, []storage.DocumentType{storage.DocumentBill}, storage.DocumentPosted, from, to)
	if err != nil {
		return nil, nil, err
	}
	out := make([]bill.Prepared, 0, len(docs))
	for _, doc := range docs {
		var at time.Time
		if doc.PostedAt != nil {
			at = *doc.PostedAt
		}
		p, _, err := bill.PrepareAt(ctx, u, doc, e.Base, at)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, p)
	}
	return docs, out, nil
}

// billsToReview: bills posted in the open months with a warning other than a possible duplicate (C.1 #17).
func billsToReview(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	docs, prepared, err := postedBills(ctx, u, e)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for i, doc := range docs {
		var words []string
		for _, w := range prepared[i].Warnings {
			if w.Code != bill.WarnDuplicate {
				words = append(words, w.Message)
			}
		}
		if len(words) > 0 {
			items = append(items, docItem("bill", doc, prepared[i].Supplier.LegalName, strings.Join(words, "; ")))
		}
	}
	if len(items) == 0 {
		return Fine("No bill of the open months posted with a warning"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d posted bill(s) to review", len(items)), "Look at each, settle what it names, then mark it reviewed"), nil
}

// duplicateBills: bills of the open months, posted or draft, that look like another of their supplier (C.2 #10).
func duplicateBills(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	docs, prepared, err := postedBills(ctx, u, e)
	if err != nil {
		return Result{}, err
	}
	if from, to, ok := e.openSpan(); ok {
		drafts, err := documents(ctx, u, []storage.DocumentType{storage.DocumentBill}, storage.DocumentDraft, from, to)
		if err != nil {
			return Result{}, err
		}
		for _, doc := range drafts {
			p, _, err := bill.Prepare(ctx, u, doc, e.Base)
			if err != nil {
				return Result{}, err
			}
			docs, prepared = append(docs, doc), append(prepared, p)
		}
	}
	var items []Item
	for i, doc := range docs {
		for _, w := range prepared[i].Warnings {
			if w.Code == bill.WarnDuplicate {
				items = append(items, docItem("bill", doc, prepared[i].Supplier.LegalName, w.Message))
				break
			}
		}
	}
	if len(items) == 0 {
		return Fine("No bill of the open months looks like another of its supplier"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d bill(s) that may be entered twice", len(items)),
		"Void the copy, or mark it reviewed if both are real"), nil
}

// withoutADocument: posted bills and expense claims with no file attached, whatever their date (C.2 #20, FR-DOC-06).
func withoutADocument(ctx context.Context, u storage.UnitOfWork, _ *Env) (Result, error) {
	docs, err := documents(ctx, u, []storage.DocumentType{storage.DocumentBill, storage.DocumentExpenseClaim}, storage.DocumentPosted, "", "")
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, doc := range docs {
		files, err := u.Evidence().OfDocument(ctx, doc.ID, uuid.Nil, 1)
		if err != nil {
			return Result{}, err
		}
		if len(files) == 0 {
			kind := map[storage.DocumentType]string{storage.DocumentBill: "bill", storage.DocumentExpenseClaim: "expense_claim"}[doc.Type]
			items = append(items, docItem(kind, doc, "", "Posted with no receipt or invoice attached"))
		}
	}
	if len(items) == 0 {
		return Fine("Every posted bill and claim has its document attached"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d posted bill(s) or claim(s) without their document", len(items)),
		"Attach the receipt or invoice to each, or mark it reviewed with why there is none"), nil
}

// inputTaxToRepay: bills with input tax claimed still open more than 12 months after their due date (C.2 #48, G4-037).
func inputTaxToRepay(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	ap, err := controlAccount(ctx, u, ledger.RoleAccountsPayable)
	if err != nil {
		return Result{}, err
	}
	if ap == nil {
		return Missing("the accounts payable account (2010)", "Accounting › Chart of accounts"), nil
	}
	l, err := payables.Open(ctx, u, e.Base, ap.ID, e.Today)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, s := range l.Suppliers {
		for _, it := range s.Items {
			if it.Kind != payables.KindBill || it.Open.Sign() <= 0 || it.DueDate.IsZero() || !it.DueDate.AddDate(0, repayAfter, 0).Before(e.Today) {
				continue
			}
			doc, err := u.Documents().Get(ctx, it.DocumentID)
			if err != nil {
				return Result{}, err
			}
			p, _, err := bill.Prepare(ctx, u, doc, e.Base)
			if err != nil {
				return Result{}, err
			}
			if !slices.ContainsFunc(p.InputTax, func(d bill.Decision) bool { return d.Condition == bill.Claimable && d.Reason != bill.ReasonNoGST }) {
				continue
			}
			items = append(items, Item{Kind: "bill", ID: it.DocumentID.String(), Label: cmp.Or(it.Number, "Bill") + ", " + s.Name,
				Detail: fmt.Sprintf("%s %s unpaid, due %s: the input tax claimed on it is to be repaid", it.Open, it.Open.Currency().Code,
					it.DueDate.Format(time.DateOnly)), Version: Fingerprint(it.Open.String())})
		}
	}
	if len(items) == 0 {
		return Fine("No bill with input tax claimed is unpaid 12 months after its due date"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d unpaid bill(s) whose input tax is due back", len(items)),
		"Pay the bill, or repay its input tax in the next GST return; mark it reviewed once the tax agent has it"), nil
}

// journalProblems: ✖ draft journals dated by the cutoff that the rules refuse; ⚠ posted journals whose automatic
// reversal waits for a period to open (C.1 #18).
func journalProblems(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	drafts, err := documents(ctx, u, []storage.DocumentType{storage.DocumentJournal}, storage.DocumentDraft, "", e.cutoff().Format(time.DateOnly))
	if err != nil {
		return Result{}, err
	}
	waitingDocs, err := u.Documents().WaitingReversals(ctx, "", nil, 1000)
	if err != nil {
		return Result{}, err
	}
	statuses, err := journal.Statuses(ctx, u, append(slices.Clone(drafts), waitingDocs...))
	if err != nil {
		return Result{}, err
	}
	var items []Item
	errs := 0
	for _, doc := range drafts {
		var bad []string
		for _, s := range statuses[doc.ID] {
			if s.Severity == journal.SeverityError {
				bad = append(bad, cmp.Or(s.Detail, s.Title))
			}
		}
		if len(bad) > 0 {
			it := docItem("journal", doc, "", bad[0])
			if len(bad) > 1 {
				it.Detail += fmt.Sprintf(" (and %d more)", len(bad)-1)
			}
			it.Severity = string(Error)
			items = append(items, it)
			errs++
		}
	}
	for _, doc := range waitingDocs {
		for _, s := range statuses[doc.ID] {
			if s.Severity == journal.SeverityWarning {
				it := docItem("journal", doc, "", cmp.Or(s.Detail, s.Title))
				it.Severity = string(Warning)
				items = append(items, it)
				break
			}
		}
	}
	if len(items) == 0 {
		return Fine("Every draft journal dated by " + e.cutoff().Format("2 Jan 2006") + " can post, and no reversal waits"), nil
	}
	status := Warning
	if errs > 0 {
		status = Error
	}
	return Found(status, items, fmt.Sprintf("%d journal(s) to fix or review", len(items)),
		"Fix or delete each draft named; open the period a waiting reversal needs"), nil
}
