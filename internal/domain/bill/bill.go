// Package bill is a supplier's bill (FR-AP-02; PRD-001 §8.6): its content as each version saves it, and what the
// posting rules need from it. Reading a bill for the rules happens here: the supplier and its terms, the GST codes of
// the sg-gst pack in force at the bill's date, the organisation's GST registration that decides whether input tax is
// claimable (FR-AP-20), the rate the bill takes (ENG-40), and the warnings a bill carries (ENG-21). The posting
// service in package journal writes it (the one posting path).
package bill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/packs"
)

// The stable codes of a bill's refusals (ENG-23).
const (
	CodeNotBill   = "BILL-01" // the document is not a bill or a vendor credit
	CodeNotDraft  = "BILL-02" // only a draft changes or posts
	CodeNotPosted = "BILL-03" // only a posted bill is voided or edited
	CodeBadDraft  = "BILL-04" // the saved bill cannot be read
	CodeSupplier  = "BILL-05" // no supplier, or one that is not an active vendor
	CodeGSTCode   = "BILL-06" // a GST code that is not a purchase code of the sg-gst pack
	CodeDueDate   = "BILL-07" // a due date before the bill's date
	CodeTooLong   = "BILL-08" // the supplier's number or the notes are too long
	CodePaid      = "BILL-13" // a bill payments settle is not voided or edited until they are voided
)

// The codes of a bill's warnings (⚠, ENG-21): it posts, and the approver sees them.
const (
	WarnGSTDiffers = "BILL-GST-DIFFERS" // the stated GST differs from RND-GST-EXCL's (G4-024)
	WarnDuplicate  = "BILL-DUPLICATE"   // another bill of the supplier with its number and total, within DuplicateWindow
	WarnNoSGDGST   = "BILL-NO-SGD-GST"  // a foreign bill's claimable line states no SGD GST: its input tax is not claimed
)

// DuplicateWindow is how far apart two bills of a supplier with the same number and total may be dated and still be
// taken for one (FR-AP-12's date window; 60 days, provisional until the accountant's sign-off).
const DuplicateWindow = 60 * 24 * time.Hour

// MaxSupplierRef and MaxNotes are the longest supplier's number and notes a bill keeps.
const (
	MaxSupplierRef = 60
	MaxNotes       = 2000
)

// Error is a bill command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is a bill as entered: the supplier and its own number for the bill, the bill's date (its tax point) and due
// date, the currency and its rate, the lines and notes.
type Draft struct {
	SupplierID  uuid.UUID
	SupplierRef string
	Date        time.Time
	DueDate     time.Time // zero: the supplier's payment terms, else the organisation's
	Currency    money.Currency
	Rate        *money.Rate // as typed; nil takes the cascade's rate for the bill's date (ENG-40)
	Lines       []Line
	Notes       string
	InputTax    InputTax // the facts its input tax is decided from (FR-AP-20; rev 0.89)
	// Nature is the nature of the payment, one of Natures, for a bill of a supplier resident outside Singapore: what the
	// tax agent's withholding decision is kept by (FR-AP-19; rev 0.90). Empty for a resident supplier.
	Nature string
	// RecurringBill is the recurring bill version the scheduler made it from (FR-AP-09; rev 0.84), nil for one a person
	// made. The database's create_scheduled_bill_draft writes it, and every later version keeps it.
	RecurringBill *RecurringRef
}

// RecurringRef names the recurring bill, and the version of its content, that a bill was made from.
type RecurringRef struct {
	ID      uuid.UUID `json:"id"`
	Version int       `json:"version"`
}

// RecurringOf is the recurring bill a document's version was made from, nil when none (or it cannot be read).
func RecurringOf(doc storage.Document) *RecurringRef {
	var p struct {
		RecurringBill *RecurringRef `json:"recurring_bill"`
	}
	if json.Unmarshal(doc.Payload, &p) != nil {
		return nil
	}
	return p.RecurringBill
}

// Line is one line of a bill: the account it costs, its net, its GST code, and its GST as the supplier states it, all
// in the bill's currency. StatedGST is the GST the tax invoice states in the base currency, for a bill in another
// currency (ENG-42). SpreadMonths spreads the line's cost over months from the month of SpreadFrom (FR-GL-03; zero,
// the bill's month): 0 for a line that is not spread.
type Line struct {
	AccountID    uuid.UUID
	Description  string
	Net          money.Money
	GSTCode      string
	GST          money.Money
	StatedGST    *money.Money
	SpreadMonths int
	SpreadFrom   time.Time
	Order        *OrderRef // the purchase order line it bills, nil for a line not made from one (FR-AP-01; rev 0.87)
	// BusinessShare is the percentage of the purchase used for the business (FR-AP-20; rev 0.89): only that share of
	// its GST is claimed, under RND-GST-SHARE. Empty is 100.
	BusinessShare string
}

// OrderRef names the line of a purchase order a bill's line bills, and the quantity of it (FR-AP-01; rev 0.87).
type OrderRef struct {
	PurchaseOrderID uuid.UUID
	Line            int // from 1
	Quantity        money.Quantity
}

// payloadOrder is an order reference as a document version stores it: the quantity as its exact text.
type payloadOrder struct {
	PurchaseOrderID uuid.UUID `json:"purchase_order_id"`
	Line            int       `json:"line"`
	Quantity        string    `json:"quantity"`
}

// Spread is the line's spread as the rules read it, nil for a line that is not spread; date is the bill's.
func (l Line) Spread(date time.Time) *ledger.Spread {
	if l.SpreadMonths == 0 && l.SpreadFrom.IsZero() {
		return nil
	}
	from := l.SpreadFrom
	if from.IsZero() {
		from = date
	}
	if !from.IsZero() {
		y, m, _ := from.Date()
		from = time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	}
	return &ledger.Spread{From: from, Months: l.SpreadMonths}
}

// payload is a bill as a document version stores it: dates as text, amounts with their currency.
type payload struct {
	SupplierID    uuid.UUID     `json:"supplier_id"`
	SupplierRef   string        `json:"supplier_ref,omitempty"`
	Date          string        `json:"date,omitempty"`
	DueDate       string        `json:"due_date,omitempty"`
	Currency      string        `json:"currency"`
	FXRate        string        `json:"fx_rate,omitempty"`
	FXRateSource  string        `json:"fx_rate_source,omitempty"`
	Lines         []payloadLine `json:"lines"`
	Notes         string        `json:"notes,omitempty"`
	RecurringBill *RecurringRef `json:"recurring_bill,omitempty"`
	InputTax      *payloadTax   `json:"input_tax,omitempty"`
	Nature        string        `json:"nature_of_payment,omitempty"`
}

// payloadTax are a bill's input-tax facts as a document version stores them: only those that are not the usual, so a
// bill entered before them keeps its payload.
type payloadTax struct {
	SupplierRegistered *bool  `json:"supplier_registered,omitempty"`
	Document           string `json:"document,omitempty"`
	AddressedTo        string `json:"addressed_to,omitempty"`
	Repaid             string `json:"repaid,omitempty"`
	TaxAgent           string `json:"tax_agent,omitempty"`
}

type payloadLine struct {
	AccountID    uuid.UUID     `json:"account_id"`
	Description  string        `json:"description,omitempty"`
	Net          money.Money   `json:"net"`
	GSTCode      string        `json:"gst_code"`
	GST          money.Money   `json:"gst"`
	StatedGST    *money.Money  `json:"stated_gst,omitempty"`
	SpreadMonths int           `json:"spread_months,omitempty"`
	SpreadFrom   string        `json:"spread_from,omitempty"`
	Order        *payloadOrder `json:"order,omitempty"`
	// BusinessShare is kept as typed, empty for 100.
	BusinessShare string `json:"business_share,omitempty"`
}

func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	if len(d.SupplierRef) > MaxSupplierRef {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the supplier's number has %d characters; the most is %d", len(d.SupplierRef), MaxSupplierRef), "Shorten it"}
	}
	if len(d.Notes) > MaxNotes {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the notes have %d characters; the most is %d", len(d.Notes), MaxNotes), "Shorten them"}
	}
	d = d.usual()
	if probs := checkFacts(d); len(probs) > 0 {
		x := probs[0]
		return nil, &Error{x.Code, x.Field + ": " + x.Message, x.Fix}
	}
	if d.Nature != "" && !slices.Contains(Natures, d.Nature) {
		return nil, &Error{CodeNature, fmt.Sprintf("nature_of_payment: %q is not one of %s", d.Nature, strings.Join(Natures, ", ")), "Choose one of them"}
	}
	p := payload{SupplierID: d.SupplierID, SupplierRef: d.SupplierRef, Date: day(d.Date), DueDate: day(d.DueDate), Currency: d.Currency.Code, Notes: d.Notes,
		Lines: []payloadLine{}, RecurringBill: d.RecurringBill, Nature: d.Nature}
	if f := d.InputTax; f != (InputTax{}) {
		p.InputTax = &payloadTax{Document: f.Document, AddressedTo: f.AddressedTo, Repaid: f.Repaid, TaxAgent: f.TaxAgent}
		if f.SupplierNotRegistered {
			no := false
			p.InputTax.SupplierRegistered = &no
		}
	}
	if d.Rate != nil {
		p.FXRate, p.FXRateSource = d.Rate.String(), d.Rate.Source
	}
	for _, l := range d.Lines {
		pl := payloadLine{AccountID: l.AccountID, Description: l.Description, Net: l.Net, GSTCode: l.GSTCode, GST: l.GST, StatedGST: l.StatedGST,
			SpreadMonths: l.SpreadMonths, SpreadFrom: day(l.SpreadFrom), BusinessShare: l.BusinessShare}
		if o := l.Order; o != nil {
			pl.Order = &payloadOrder{PurchaseOrderID: o.PurchaseOrderID, Line: o.Line, Quantity: o.Quantity.String()}
		}
		p.Lines = append(p.Lines, pl)
	}
	return json.Marshal(p)
}

// Kind is what a document of this package is: a bill, or a supplier's credit note (FR-AP-03), whose content is a bill's
// and whose entry is a bill's legs reversed.
func Kind(doc storage.Document) string {
	if doc.Type == storage.DocumentVendorCredit {
		return "credit note"
	}
	return "bill"
}

// IsBill says whether a document is a bill or a vendor credit: what this package reads.
func IsBill(doc storage.Document) bool {
	return doc.Type == storage.DocumentBill || doc.Type == storage.DocumentVendorCredit
}

// Read reads a bill or vendor credit version as it was saved; base is the organisation's base currency, which a typed
// rate converts into.
func Read(doc storage.Document, base money.Currency) (Draft, error) {
	if !IsBill(doc) {
		return Draft{}, &Error{CodeNotBill, fmt.Sprintf("document %s is a %s, not a bill", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, fmt.Sprintf("the saved bill cannot be read: %s", what), "Save the bill again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, bad(err.Error())
	}
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Draft{}, bad(err.Error())
	}
	d := Draft{SupplierID: p.SupplierID, SupplierRef: p.SupplierRef, Currency: cur, Notes: p.Notes, RecurringBill: p.RecurringBill, Nature: p.Nature}
	if t := p.InputTax; t != nil {
		d.InputTax = InputTax{SupplierNotRegistered: t.SupplierRegistered != nil && !*t.SupplierRegistered, Document: t.Document, AddressedTo: t.AddressedTo,
			Repaid: t.Repaid, TaxAgent: t.TaxAgent}
	}
	for _, x := range []struct {
		text string
		to   *time.Time
	}{{p.Date, &d.Date}, {p.DueDate, &d.DueDate}} {
		if x.text == "" {
			continue
		}
		if *x.to, err = time.Parse(time.DateOnly, x.text); err != nil {
			return Draft{}, bad(fmt.Sprintf("the date %q", x.text))
		}
	}
	if p.FXRate != "" {
		r, err := money.ParseRate(p.FXRate, cur, base, p.FXRateSource)
		if err != nil {
			return Draft{}, bad(err.Error())
		}
		d.Rate = &r
	}
	for _, l := range p.Lines {
		line := Line{AccountID: l.AccountID, Description: l.Description, Net: l.Net, GSTCode: l.GSTCode, GST: l.GST, StatedGST: l.StatedGST, SpreadMonths: l.SpreadMonths,
			BusinessShare: l.BusinessShare}
		if l.SpreadFrom != "" {
			if line.SpreadFrom, err = time.Parse(time.DateOnly, l.SpreadFrom); err != nil {
				return Draft{}, bad(fmt.Sprintf("the spread's first month %q", l.SpreadFrom))
			}
		}
		if o := l.Order; o != nil {
			q, err := money.ParseQuantity(o.Quantity, "quantity")
			if err != nil {
				return Draft{}, bad(fmt.Sprintf("the quantity %q billed of an order line", o.Quantity))
			}
			line.Order = &OrderRef{PurchaseOrderID: o.PurchaseOrderID, Line: o.Line, Quantity: q}
		}
		d.Lines = append(d.Lines, line)
	}
	return d, nil
}

// Total is the bill's total in its currency: every line's net and GST.
func (d Draft) Total() (money.Money, error) {
	out := money.Zero(d.Currency)
	for _, l := range d.Lines {
		var err error
		if out, err = out.Add(l.Net); err != nil {
			return money.Money{}, err
		}
		if out, err = out.Add(l.GST); err != nil {
			return money.Money{}, err
		}
	}
	return out, nil
}

// Warning is one ⚠ of a bill: it posts, and the approver sees why it is flagged.
type Warning struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// Prepared is a bill version as the rules read it, with what it shows beside its entry: its supplier, its due date,
// how each line's input tax was decided, and its warnings.
type Prepared struct {
	Draft    Draft
	Bill     ledger.Bill
	Supplier storage.Contact
	DueDate  time.Time
	InputTax []Decision // one a line, in order (FR-AP-20)
	// Withholding is how withholding tax stands for it (FR-AP-19): only a bill, never a vendor credit, withholds.
	Withholding Withholding
	Warnings    []Warning
}

// Prepare reads one version of a bill for the rules: its supplier, its due date, its rate (the typed one, else the
// cascade's for its date), and each line's input tax, decided from the bill's facts, the line's GST code and business
// share, and the organisation's GST registration on the bill's date (FR-AP-20; decide). Problems the rules would
// refuse come back as ledger.Problems with the engine's own, from PostBill; the warnings come back on the Prepared.
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency) (Prepared, ledger.Problems, error) {
	return PrepareAt(ctx, u, doc, base, time.Time{})
}

// PrepareAt is Prepare for a bill that posted at at: the withholding decision it takes is the one in force then
// (FR-AP-19), as a replay or a posted bill's view reads it. A zero at is now.
func PrepareAt(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency, at time.Time) (Prepared, ledger.Problems, error) {
	d, err := Read(doc, base)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}

	// The supplier.
	if d.SupplierID == uuid.Nil {
		add(CodeSupplier, "supplier_id", "the bill has no supplier", "Choose the supplier who sent the bill")
	} else {
		c, err := u.Contacts().Get(ctx, d.SupplierID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeSupplier, "supplier_id", "the supplier does not exist in this organisation", "Choose a supplier from your contacts")
		case err != nil:
			return Prepared{}, nil, err
		case !c.Vendor:
			add(CodeSupplier, "supplier_id", fmt.Sprintf("%s is not a supplier", c.LegalName), "Mark the contact as a supplier, or choose another")
		case !c.Active:
			add(CodeSupplier, "supplier_id", fmt.Sprintf("%s is inactive", c.LegalName), "Reactivate the contact, or choose another")
		default:
			out.Supplier = c
		}
	}

	// Withholding tax on a non-resident supplier's bill (FR-AP-19): the nature of the payment, and the tax agent's
	// decision in force when it posts.
	if out.Supplier.ID != uuid.Nil && doc.Type == storage.DocumentBill {
		if out.Withholding, err = withholdingOf(ctx, u, out.Supplier, d.Nature, at); err != nil {
			return Prepared{}, nil, err
		}
		if out.Withholding.NonResident && !slices.Contains(Natures, d.Nature) {
			add(CodeNature, "nature_of_payment", fmt.Sprintf("%s is resident for tax in %s, so the bill records the nature of the payment", out.Supplier.LegalName,
				out.Withholding.Residence), "Choose what the payment is for, such as software for the company's own use or services performed in Singapore")
		}
	}

	// The due date: as entered, else the supplier's terms, else the organisation's (FR-ORG-04).
	if out.DueDate, err = Due(ctx, u, d, out.Supplier); err != nil {
		return Prepared{}, nil, err
	}
	if !d.Date.IsZero() && !out.DueDate.IsZero() && out.DueDate.Before(d.Date) {
		add(CodeDueDate, "due_date", fmt.Sprintf("the bill falls due on %s, before its date %s", day(out.DueDate), day(d.Date)), "Enter a due date on or after the bill's date")
	}

	// The rate: as typed, else the cascade's for the bill's date (ENG-40).
	b := ledger.Bill{ID: doc.ID, Version: doc.Version, Date: d.Date, Currency: d.Currency, Rate: d.Rate, Memo: memo(out.Supplier, d)}
	if d.Currency != base && d.Rate == nil && !d.Date.IsZero() {
		f, err := fx.Lookup(ctx, u, base, d.Currency, d.Date)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			add(fe.Code, "fx_rate", fe.Message, fe.Fix)
		case err != nil:
			return Prepared{}, nil, err
		default:
			r := f.Rate
			b.Rate = &r
		}
	}

	// Each line's GST code, and how its input tax is decided.
	probs = append(probs, checkFacts(d)...)
	version, err := gstVersion(ctx, u, d.Date)
	if err != nil {
		return Prepared{}, nil, err
	}
	registered := false
	if !d.Date.IsZero() {
		regs, err := u.GSTRegistrations().List(ctx)
		if err != nil {
			return Prepared{}, nil, err
		}
		_, registered = gst.StatusOn(regs, d.Date)
	}
	x := facts{registered: registered, version: version, date: d.Date, base: base}
	if total, err := d.Total(); err == nil {
		if d.Currency == base {
			x.total = &total
		} else if b.Rate != nil {
			if t, err := rounding.FX(total, *b.Rate); err == nil {
				x.total = &t
			}
		}
	}
	for i, l := range d.Lines {
		code, ok := pack.Code(version, l.GSTCode)
		if !ok || code.Direction != pack.DirPurchase {
			add(CodeGSTCode, fmt.Sprintf("lines[%d].gst_code", i+1), fmt.Sprintf("%q is not a GST code for purchases", l.GSTCode),
				"Choose the GST code on the supplier's invoice, such as TX, BL or NR")
		}
		dec := decide(d.InputTax, l, code, ok, x)
		out.InputTax = append(out.InputTax, dec)
		bl := ledger.BillLine{AccountID: l.AccountID, Description: l.Description, Net: l.Net, GST: l.GST, StatedBase: l.StatedGST,
			Claimable: dec.Condition == Claimable && dec.Reason != ReasonNoGST, Spread: l.Spread(d.Date)}
		if dec.Share != "" {
			bl.Shares = []string{dec.Share}
		}
		b.Lines = append(b.Lines, bl)
	}
	if r := out.Withholding.Rate(); r != "" {
		b.Withholding = &ledger.Withholding{RatePercent: r}
	}
	out.Bill = b
	if out.Withholding.NonResident && slices.Contains(Natures, d.Nature) && out.Withholding.Decision == nil {
		out.Warnings = append(out.Warnings, Warning{WarnWHTUndecided, "nature_of_payment",
			fmt.Sprintf("the tax agent has not decided whether tax is withheld from %s's payments for %s: it posts without withholding, and its payment waits for the decision",
				out.Supplier.LegalName, NatureWords[d.Nature]),
			"Record the tax agent's decision for the supplier and nature, with their advice attached"})
	}
	if len(probs) == 0 && !d.Date.IsZero() {
		more, err := warnings(ctx, u, doc, d, b, base, version, out.InputTax)
		if err != nil {
			return Prepared{}, nil, err
		}
		out.Warnings = append(out.Warnings, more...)
	}
	return out, probs, nil
}

// CodePlan is the refusal of a payment date Fyniq cannot plan (FR-AP-13; rev 0.93).
const CodePlan = "BILL-18"

// EarlyDiscount is the early-payment discount a bill offers (FR-AP-13; rev 0.93): its supplier's terms, the last day to
// take it, and its amount on the bill's total under RND-DISCOUNT. Taking it is a supplier credit note for the amount.
type EarlyDiscount struct {
	Percent string
	Until   time.Time
	Amount  money.Money
}

// DiscountOf is the early-payment discount a bill of supplier offers: nil when the supplier has no terms, or the bill
// has no date.
func DiscountOf(supplier storage.Contact, d Draft) (*EarlyDiscount, error) {
	if supplier.DiscountPercent == "" || supplier.DiscountDays == nil || d.Date.IsZero() {
		return nil, nil
	}
	total, err := d.Total()
	if err != nil {
		return nil, err
	}
	amount, err := rounding.Discount(total, supplier.DiscountPercent)
	if err != nil {
		return nil, err
	}
	return &EarlyDiscount{Percent: supplier.DiscountPercent, Until: d.Date.AddDate(0, 0, *supplier.DiscountDays), Amount: amount}, nil
}

// memo names a bill in its entry: the supplier and the supplier's number.
func memo(c storage.Contact, d Draft) string {
	switch {
	case c.LegalName != "" && d.SupplierRef != "":
		return c.LegalName + " " + d.SupplierRef
	case c.LegalName != "":
		return c.LegalName
	}
	return d.SupplierRef
}

// Due is when a bill falls due: its due date as entered, else its date plus the supplier's payment terms, else the
// organisation's in force on its date (FR-ORG-04); zero for a bill with no date. supplier is the bill's, as read.
func Due(ctx context.Context, u storage.UnitOfWork, d Draft, supplier storage.Contact) (time.Time, error) {
	if !d.DueDate.IsZero() || d.Date.IsZero() {
		return d.DueDate, nil
	}
	days := 0
	if supplier.PaymentTermsDays != nil {
		days = *supplier.PaymentTermsDays
	} else {
		var err error
		if days, err = termsDays(ctx, u, d.Date); err != nil {
			return time.Time{}, err
		}
	}
	return d.Date.AddDate(0, 0, days), nil
}

// termsDays is the organisation's payment terms for bills in force on day (FR-ORG-04).
func termsDays(ctx context.Context, u storage.UnitOfWork, on time.Time) (int, error) {
	d, _ := settings.Lookup("purchases.payment_terms_days")
	changes, err := u.Settings().Changes(ctx, d.Key)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscan(settings.On(d, changes, on).Value, &n); err != nil {
		return 0, err
	}
	return n, nil
}

// releasePacks are the pack versions this release carries.
var releasePacks = func() ([]pack.Version, error) { return pack.LoadAll(packs.FS) }

// gstVersion is the sg-gst version in force on the bill's date among the organisation's active versions, else among
// the versions this release carries: the GST codes and rates the bill is read with (ENG-31).
func gstVersion(ctx context.Context, u storage.UnitOfWork, on time.Time) (pack.Version, error) {
	all, err := releasePacks()
	if err != nil {
		return pack.Version{}, err
	}
	if on.IsZero() { // a bill without a date has no tax point yet: its codes are read from the newest version
		for _, v := range all {
			if v.Pack == "sg-gst" && v.EffectiveFrom.After(on) {
				on = v.EffectiveFrom
			}
		}
	}
	acts, err := u.Packs().Activations(ctx)
	if err != nil {
		return pack.Version{}, err
	}
	var active []pack.Version
	for _, v := range all {
		if slices.ContainsFunc(acts, func(a storage.PackActivation) bool { return a.VersionID == v.ID }) {
			active = append(active, v)
		}
	}
	if v, ok := pack.At(active, "sg-gst", on); ok {
		return v, nil
	}
	if v, ok := pack.At(all, "sg-gst", on); ok {
		return v, nil
	}
	return pack.Version{}, fmt.Errorf("this release carries no sg-gst version in force on %s", day(on))
}

// Codes are the GST codes a bill dated on a day is read with: the sg-gst version in force then, its standard rate, and
// its codes for purchases.
type Codes struct {
	Version string
	Rate    string // the standard rate then, as a fraction; empty before GST
	Codes   []pack.GSTCode
}

// PurchaseCodes are the GST codes for purchases a bill dated on would be read with (ENG-31): from the organisation's
// active sg-gst version in force then, else the one this release carries.
func PurchaseCodes(ctx context.Context, u storage.UnitOfWork, on time.Time) (Codes, error) {
	v, err := gstVersion(ctx, u, on)
	if err != nil {
		return Codes{}, err
	}
	out := Codes{Version: v.ID}
	if !on.IsZero() {
		out.Rate, _ = pack.RateOn(v, on)
	}
	for _, row := range v.Tables["codes"].Rows {
		if c, ok := pack.Code(v, row[0]); ok && c.Direction == pack.DirPurchase {
			out.Codes = append(out.Codes, c)
		}
	}
	return out, nil
}

// warnings are a readable bill's ⚠: its stated GST against RND-GST-EXCL's, a duplicate of another bill, a foreign
// claimable line with no SGD GST stated, a line whose input tax waits for the tax agent, and an agent's invoice claimed
// with no file attached (the owner's decision of 23 Oct 2026).
func warnings(ctx context.Context, u storage.UnitOfWork, doc storage.Document, d Draft, b ledger.Bill, base money.Currency, v pack.Version,
	decisions []Decision) ([]Warning, error) {
	var out []Warning
	agentClaimed := false
	for i, dec := range decisions {
		if dec.Condition == ForTaxAgent {
			out = append(out, Warning{WarnTaxAgent, fmt.Sprintf("lines[%d].gst_code", i+1), fmt.Sprintf("line %d's input tax is not claimed: %s", i+1, dec.Text),
				"Ask the tax agent; record their decision on the bill, and attach their advice"})
		}
		agentClaimed = agentClaimed || (dec.Condition == Claimable && dec.Reason != ReasonNoGST && d.InputTax.addressedTo() == ToAgent)
	}
	if agentClaimed {
		files, err := u.Evidence().OfDocument(ctx, doc.ID, uuid.Nil, 50)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(files, func(e storage.Evidence) bool { return e.RemovedAt == nil }) {
			out = append(out, Warning{WarnRepaidEvidence, "input_tax.addressed_to",
				"the invoice is in an agent's name and its input tax is claimed, but no evidence is attached that the company paid them back",
				"Attach the invoice and the record of the repayment"})
		}
	}
	// RND-GST-EXCL over the claimable lines' nets in the base currency, by the organisation's method, against the
	// GST the supplier stated (in SGD for a foreign bill).
	method := rounding.GSTPerSubtotal
	if perLine, err := setting(ctx, u, "gst.round_per_line", d.Date); err != nil {
		return nil, err
	} else if perLine == "true" {
		method = rounding.GSTPerLine
	}
	rate, ok := pack.RateOn(v, d.Date)
	var nets []rounding.GSTNet
	stated := money.Zero(base)
	for i, l := range b.Lines {
		if !l.Claimable || l.GST.IsZero() {
			continue
		}
		if b.Currency != base && l.StatedBase == nil {
			out = append(out, Warning{WarnNoSGDGST, fmt.Sprintf("lines[%d].stated_gst", i+1), "no SGD GST is stated on the tax invoice: the input tax is not claimed, and stays in the cost",
				"Enter the GST in SGD as the supplier's tax invoice states it"})
			continue
		}
		net, s := l.Net, l.GST
		if b.Currency != base {
			if b.Rate == nil {
				continue
			}
			var err error
			if net, err = rounding.FX(l.Net, *b.Rate); err != nil {
				return nil, err
			}
			s = *l.StatedBase
		}
		nets = append(nets, rounding.GSTNet{Code: d.Lines[i].GSTCode, Rate: rate, Net: net})
		stated, _ = stated.Add(s)
	}
	if ok && len(nets) > 0 {
		subtotals, err := rounding.GSTExcl(nets, method)
		if err != nil {
			return nil, err
		}
		recomputed := money.Zero(base)
		for _, s := range subtotals {
			recomputed, _ = recomputed.Add(s.GST)
		}
		if diff, _ := stated.Sub(recomputed); !diff.IsZero() {
			if diff.Sign() < 0 {
				diff = diff.Neg()
			}
			out = append(out, Warning{WarnGSTDiffers, "lines", fmt.Sprintf("the stated GST %s %s differs by %s %s from the recomputation (%s, %s)",
				stated, base, diff, base, rounding.GSTExclName, method),
				"Check the GST against the supplier's invoice: input tax is the GST as stated, and the approver acknowledges the difference"})
		}
	}
	// A duplicate: another document of its kind from the supplier with its number and total, dated within the window, not
	// reversed.
	if d.SupplierRef != "" && d.SupplierID != uuid.Nil {
		total, err := d.Total()
		if err != nil {
			return nil, err
		}
		others, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: doc.Type,
			From: day(d.Date.Add(-DuplicateWindow)), To: day(d.Date.Add(DuplicateWindow)), Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, o := range others {
			if o.ID == doc.ID || o.Status == storage.DocumentReversed {
				continue
			}
			od, err := Read(o, base)
			if err != nil || od.SupplierID != d.SupplierID || od.SupplierRef != d.SupplierRef || od.Currency != d.Currency {
				continue
			}
			if ot, err := od.Total(); err == nil && ot.Equal(total) {
				label := o.Number
				if label == "" {
					label = "a draft"
				}
				out = append(out, Warning{WarnDuplicate, "supplier_ref", fmt.Sprintf("%s %s has the same supplier, number %s and total, dated %s", Kind(doc), label, d.SupplierRef, day(od.Date)),
					fmt.Sprintf("Check it is not the same %s entered twice; void one if it is", Kind(doc))})
				break
			}
		}
	}
	return out, nil
}

// setting is a setting's value on a day.
func setting(ctx context.Context, u storage.UnitOfWork, key string, on time.Time) (string, error) {
	d, ok := settings.Lookup(key)
	if !ok {
		return "", fmt.Errorf("no setting %s", key)
	}
	changes, err := u.Settings().Changes(ctx, d.Key)
	if err != nil {
		return "", err
	}
	return settings.On(d, changes, on).Value, nil
}
