// Package invoice is a customer's invoice (FR-AR-03; PRD-001 §8.5): its content as each version saves it, and what the
// posting rules need from it. Reading an invoice for the rules happens here: the customer and its terms, each line's
// net under RND-LINE, the GST codes of the sg-gst pack in force at the invoice's date and whether the organisation is
// GST-registered then (no GST before it is), the rate the invoice takes (ENG-40), and its warnings (ENG-21). The posting
// service in package journal writes it (the one posting path).
package invoice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

// The stable codes of an invoice's refusals (ENG-23).
const (
	CodeNotInvoice = "INVOICE-01" // the document is not an invoice
	CodeNotDraft   = "INVOICE-02" // only a draft changes or is issued
	CodeNotPosted  = "INVOICE-03" // only an issued invoice is voided or edited
	CodeBadDraft   = "INVOICE-04" // the saved invoice cannot be read
	CodeCustomer   = "INVOICE-05" // no customer, or one that is not an active customer
	CodeGSTCode    = "INVOICE-06" // a GST code that is not a supply code of the sg-gst pack
	CodeDueDate    = "INVOICE-07" // a due date before the invoice's date
	CodeTooLong    = "INVOICE-08" // the reference or the notes are too long
	CodeLine       = "INVOICE-09" // a line's quantity, price or discount that cannot be one
	CodeSettled    = "INVOICE-13" // an invoice receipts or credits settle is not voided or edited until they are voided
)

// MaxReference and MaxNotes are the longest reference (such as the customer's purchase order) and notes an invoice keeps.
const (
	MaxReference = 60
	MaxNotes     = 2000
)

// Error is an invoice command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is an invoice as entered: the customer and their reference for it, the invoice's date (its tax point) and due
// date, the currency and its rate, the lines and notes.
type Draft struct {
	CustomerID uuid.UUID
	Reference  string
	Date       time.Time
	DueDate    time.Time // zero: the customer's payment terms, else the organisation's
	Currency   money.Currency
	Rate       *money.Rate // as typed; nil takes the cascade's rate for the invoice's date (ENG-40)
	Lines      []Line
	Notes      string
}

// Line is one line of an invoice: what it sells (an item, or a description alone), how many at what price less a
// discount (RND-LINE), the account it earns, its GST code, and an optional spread of its revenue over months from the
// month of SpreadFrom (FR-GL-03; zero, the invoice's month). A single-amount invoice is one line of quantity 1.
type Line struct {
	ItemID       *uuid.UUID
	Description  string
	Quantity     money.Quantity
	UnitPrice    money.UnitPrice
	Discount     string // a percentage, empty for none
	AccountID    uuid.UUID
	GSTCode      string
	SpreadMonths int
	SpreadFrom   time.Time
}

// Net is the line's amount under RND-LINE, its discount taken in its one rounding.
func (l Line) Net() (money.Money, error) {
	return rounding.LineDiscounted(l.Quantity, l.UnitPrice, l.Discount)
}

// Spread is the line's spread as the rules read it, nil for a line that is not spread; date is the invoice's.
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

// payload is an invoice as a document version stores it: dates as text, quantities and prices as their exact text.
type payload struct {
	CustomerID   uuid.UUID     `json:"customer_id"`
	Reference    string        `json:"reference,omitempty"`
	Date         string        `json:"date,omitempty"`
	DueDate      string        `json:"due_date,omitempty"`
	Currency     string        `json:"currency"`
	FXRate       string        `json:"fx_rate,omitempty"`
	FXRateSource string        `json:"fx_rate_source,omitempty"`
	Lines        []payloadLine `json:"lines"`
	Notes        string        `json:"notes,omitempty"`
}

type payloadLine struct {
	ItemID       *uuid.UUID `json:"item_id,omitempty"`
	Description  string     `json:"description,omitempty"`
	Quantity     string     `json:"quantity"`
	UnitPrice    string     `json:"unit_price"`
	Discount     string     `json:"discount,omitempty"`
	AccountID    uuid.UUID  `json:"account_id"`
	GSTCode      string     `json:"gst_code"`
	SpreadMonths int        `json:"spread_months,omitempty"`
	SpreadFrom   string     `json:"spread_from,omitempty"`
}

func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

// Encode is a draft as a document version stores it.
func Encode(d Draft) ([]byte, error) {
	if len(d.Reference) > MaxReference {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the reference has %d characters; the most is %d", len(d.Reference), MaxReference), "Shorten it"}
	}
	if len(d.Notes) > MaxNotes {
		return nil, &Error{CodeTooLong, fmt.Sprintf("the notes have %d characters; the most is %d", len(d.Notes), MaxNotes), "Shorten them"}
	}
	p := payload{CustomerID: d.CustomerID, Reference: d.Reference, Date: day(d.Date), DueDate: day(d.DueDate), Currency: d.Currency.Code, Notes: d.Notes,
		Lines: []payloadLine{}}
	if d.Rate != nil {
		p.FXRate, p.FXRateSource = d.Rate.String(), d.Rate.Source
	}
	for i, l := range d.Lines {
		if l.Discount != "" && !rounding.IsShare(l.Discount) {
			return nil, &Error{CodeLine, fmt.Sprintf("lines[%d].discount: %q is not a percentage from 0 to 100 with at most two decimals", i+1, l.Discount),
				"Enter the discount as a percentage, such as 10 or 12.5"}
		}
		if l.UnitPrice.Currency() != d.Currency {
			return nil, &Error{CodeLine, fmt.Sprintf("lines[%d].unit_price is in %s; the invoice is in %s", i+1, l.UnitPrice.Currency(), d.Currency),
				"Enter every price of the invoice in its currency"}
		}
		p.Lines = append(p.Lines, payloadLine{ItemID: l.ItemID, Description: l.Description, Quantity: l.Quantity.String(), UnitPrice: l.UnitPrice.String(),
			Discount: l.Discount, AccountID: l.AccountID, GSTCode: l.GSTCode, SpreadMonths: l.SpreadMonths, SpreadFrom: day(l.SpreadFrom)})
	}
	return json.Marshal(p)
}

// IsInvoice says whether a document is an invoice: what this package reads.
func IsInvoice(doc storage.Document) bool { return doc.Type == storage.DocumentInvoice }

// Read reads an invoice version as it was saved; base is the organisation's base currency, which a typed rate converts
// into.
func Read(doc storage.Document, base money.Currency) (Draft, error) {
	if !IsInvoice(doc) {
		return Draft{}, &Error{CodeNotInvoice, fmt.Sprintf("document %s is a %s, not an invoice", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, fmt.Sprintf("the saved invoice cannot be read: %s", what), "Save the invoice again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, bad(err.Error())
	}
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Draft{}, bad(err.Error())
	}
	d := Draft{CustomerID: p.CustomerID, Reference: p.Reference, Currency: cur, Notes: p.Notes}
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
		q, err := money.ParseQuantity(l.Quantity, "quantity")
		if err != nil {
			return Draft{}, bad(fmt.Sprintf("the quantity %q", l.Quantity))
		}
		price, err := money.ParseUnitPrice(l.UnitPrice, cur, "unit_price")
		if err != nil {
			return Draft{}, bad(fmt.Sprintf("the unit price %q", l.UnitPrice))
		}
		line := Line{ItemID: l.ItemID, Description: l.Description, Quantity: q, UnitPrice: price, Discount: l.Discount, AccountID: l.AccountID, GSTCode: l.GSTCode,
			SpreadMonths: l.SpreadMonths}
		if l.SpreadFrom != "" {
			if line.SpreadFrom, err = time.Parse(time.DateOnly, l.SpreadFrom); err != nil {
				return Draft{}, bad(fmt.Sprintf("the spread's first month %q", l.SpreadFrom))
			}
		}
		d.Lines = append(d.Lines, line)
	}
	return d, nil
}

// Prepared is an invoice version as the rules read it, with what it shows beside its entry: its customer, its due
// date, its totals in its own currency, whether it bears GST, and its warnings.
type Prepared struct {
	Draft      Draft
	Invoice    ledger.Invoice
	Customer   storage.Contact
	DueDate    time.Time
	Totals     ledger.InvoiceTotals
	Registered bool // the organisation is GST-registered on the invoice's date, so its standard-rated lines bear GST
	GSTVersion string
}

// Prepare reads one version of an invoice for the rules: its customer, its due date, its rate (the typed one, else the
// cascade's for its date), each line's net under RND-LINE, and each line's GST rate from its code in the sg-gst version
// in force then, none before the organisation is GST-registered (FR-ORG-03). Problems the rules would refuse come back
// as ledger.Problems with the engine's own, from PostInvoice.
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency) (Prepared, ledger.Problems, error) {
	d, err := Read(doc, base)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}

	// The customer.
	if d.CustomerID == uuid.Nil {
		add(CodeCustomer, "customer_id", "the invoice has no customer", "Choose the customer it is for")
	} else {
		c, err := u.Contacts().Get(ctx, d.CustomerID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeCustomer, "customer_id", "the customer does not exist in this organisation", "Choose a customer from your contacts")
		case err != nil:
			return Prepared{}, nil, err
		case !c.Customer:
			add(CodeCustomer, "customer_id", fmt.Sprintf("%s is not a customer", c.LegalName), "Mark the contact as a customer, or choose another")
		case !c.Active:
			add(CodeCustomer, "customer_id", fmt.Sprintf("%s is inactive", c.LegalName), "Reactivate the contact, or choose another")
		default:
			out.Customer = c
		}
	}

	// The due date: as entered, else the customer's terms, else the organisation's (FR-ORG-04).
	if out.DueDate, err = Due(ctx, u, d, out.Customer); err != nil {
		return Prepared{}, nil, err
	}
	if !d.Date.IsZero() && !out.DueDate.IsZero() && out.DueDate.Before(d.Date) {
		add(CodeDueDate, "due_date", fmt.Sprintf("the invoice falls due on %s, before its date %s", day(out.DueDate), day(d.Date)), "Enter a due date on or after the invoice's date")
	}

	// The rate: as typed, else the cascade's for the invoice's date (ENG-40).
	inv := ledger.Invoice{ID: doc.ID, Version: doc.Version, Date: d.Date, Currency: d.Currency, Rate: d.Rate, Memo: memo(out.Customer, doc)}
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
			inv.Rate = &r
		}
	}

	// GST: the codes of the version in force on the invoice's date, and the standard rate only once registered.
	version, err := gst.Version(ctx, u, d.Date)
	if err != nil {
		return Prepared{}, nil, err
	}
	out.GSTVersion = version.ID
	if !d.Date.IsZero() {
		regs, err := u.GSTRegistrations().List(ctx)
		if err != nil {
			return Prepared{}, nil, err
		}
		_, out.Registered = gst.StatusOn(regs, d.Date)
	}
	standard, _ := pack.RateOn(version, d.Date)
	if perLine, err := setting(ctx, u, "gst.round_per_line", d.Date); err != nil {
		return Prepared{}, nil, err
	} else if perLine == "true" {
		inv.GSTMethod = rounding.GSTPerLine
	}
	for i, l := range d.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		net, err := l.Net()
		if err != nil {
			add(CodeLine, f("quantity"), err.Error(), "Check the quantity, the unit price and the discount")
			continue
		}
		code, ok := pack.Code(version, l.GSTCode)
		if !ok || code.Direction != pack.DirSupply {
			add(CodeGSTCode, f("gst_code"), fmt.Sprintf("%q is not a GST code for supplies", l.GSTCode), "Choose a GST code for sales, such as SR, ZR, ES or OS")
		}
		line := ledger.InvoiceLine{AccountID: l.AccountID, Description: l.Description, Net: net, GSTCode: l.GSTCode, Spread: l.Spread(d.Date)}
		if out.Registered && ok {
			switch {
			case code.StandardRated:
				line.GSTRate = standard
			case code.ValueBox != 0: // zero-rated and exempt supplies are reported at no GST
				line.GSTRate = "0"
			}
		}
		inv.Lines = append(inv.Lines, line)
	}
	out.Invoice = inv
	if out.Totals, err = ledger.TotalsOf(inv); err != nil {
		add(ledger.CodeGSTRate, "lines", err.Error(), "Choose each line's GST code again: its rate comes from the sg-gst pack")
	}
	return out, probs, nil
}

// memo names an invoice in its entry: the customer and the invoice's number, once it has one.
func memo(c storage.Contact, doc storage.Document) string {
	switch {
	case c.LegalName != "" && doc.Number != "":
		return c.LegalName + " " + doc.Number
	case c.LegalName != "":
		return c.LegalName
	}
	return doc.Number
}

// Due is when an invoice falls due: its due date as entered, else its date plus the customer's payment terms, else the
// organisation's in force on its date (FR-ORG-04); zero for an invoice with no date.
func Due(ctx context.Context, u storage.UnitOfWork, d Draft, customer storage.Contact) (time.Time, error) {
	if !d.DueDate.IsZero() || d.Date.IsZero() {
		return d.DueDate, nil
	}
	if customer.PaymentTermsDays != nil {
		return d.Date.AddDate(0, 0, *customer.PaymentTermsDays), nil
	}
	v, err := setting(ctx, u, "sales.payment_terms_days", d.Date)
	if err != nil {
		return time.Time{}, err
	}
	var n int
	if _, err := fmt.Sscan(v, &n); err != nil {
		return time.Time{}, err
	}
	return d.Date.AddDate(0, 0, n), nil
}

// setting is a setting's value on a day.
func setting(ctx context.Context, u storage.UnitOfWork, key string, on time.Time) (string, error) {
	def, ok := settings.Lookup(key)
	if !ok {
		return "", fmt.Errorf("no setting %s", key)
	}
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return "", err
	}
	return settings.On(def, changes, on).Value, nil
}
