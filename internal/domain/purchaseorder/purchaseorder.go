// Package purchaseorder is an order to a supplier (FR-AP-01; PRD-001 §8.6, rev 0.87; p1-purchases task 8.1): its
// content as each version saves it, the checks it is approved under, and what is left to bill of each line. A purchase
// order posts nothing. Approving it posts the document, with no entry; closing it reverses the document, with no entry.
// On the owner's decisions of 23 Oct 2026, each line has a quantity and a unit price, its amount is RND-LINE, and bills
// take quantities of its lines: a bill's line names the order line and the quantity, and its amount is the part by
// cumulative rounding, so a line's parts add up to its amount exactly.
package purchaseorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a purchase order's refusals (ENG-23).
const (
	CodeNotOrder    = "PO-01" // the document is not a purchase order
	CodeBadDraft    = "PO-02" // the saved order cannot be read
	CodeSupplier    = "PO-03" // no supplier, or one that is not an active vendor
	CodeNoDate      = "PO-04" // the order has no date
	CodeLines       = "PO-05" // no lines, or a total that is not more than zero
	CodeAccount     = "PO-06" // a line's account is not a postable, active expense or asset account
	CodeQuantity    = "PO-07" // a quantity that is not more than zero, or a unit price below zero
	CodeGSTCode     = "PO-08" // a GST code that is not a purchase code of the sg-gst pack
	CodeTooLong     = "PO-09" // the reference, a description or the notes are too long
	CodeNotDraft    = "PO-10" // only a draft changes or is approved
	CodeNotApproved = "PO-11" // only an approved order, not closed, is billed or closed
	CodeLeft        = "PO-12" // more of a line than is left on it to bill, or nothing left to bill
	CodeOrderLine   = "PO-13" // a bill's line names an order, or a line of it, that a bill of its supplier cannot bill
)

// The limits of an order's text.
const (
	MaxReference = 60
	MaxNotes     = 2000
)

// Error is an order command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Line is one line of an order: the account it costs, what it is, the quantity and the unit price, in the order's
// currency, and the GST code the supplier is expected to charge.
type Line struct {
	AccountID   uuid.UUID
	Description string
	Quantity    money.Quantity
	UnitPrice   money.UnitPrice
	GSTCode     string
}

// Draft is an order as entered: the supplier, a reference (such as their quotation's number), its date and when the
// goods or services are expected, the currency, the lines and notes.
type Draft struct {
	SupplierID uuid.UUID
	Reference  string
	Date       time.Time
	ExpectedOn time.Time // zero: none given
	Currency   money.Currency
	Lines      []Line
	Notes      string
}

type payload struct {
	SupplierID uuid.UUID     `json:"supplier_id"`
	Reference  string        `json:"reference,omitempty"`
	Date       string        `json:"date,omitempty"`
	ExpectedOn string        `json:"expected_on,omitempty"`
	Currency   string        `json:"currency"`
	Lines      []payloadLine `json:"lines"`
	Notes      string        `json:"notes,omitempty"`
}

type payloadLine struct {
	AccountID   uuid.UUID `json:"account_id"`
	Description string    `json:"description,omitempty"`
	Quantity    string    `json:"quantity"`
	UnitPrice   string    `json:"unit_price"`
	GSTCode     string    `json:"gst_code"`
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
	p := payload{SupplierID: d.SupplierID, Reference: d.Reference, Date: day(d.Date), ExpectedOn: day(d.ExpectedOn), Currency: d.Currency.Code,
		Notes: d.Notes, Lines: []payloadLine{}}
	for _, l := range d.Lines {
		p.Lines = append(p.Lines, payloadLine{AccountID: l.AccountID, Description: l.Description, Quantity: l.Quantity.String(),
			UnitPrice: l.UnitPrice.String(), GSTCode: l.GSTCode})
	}
	return json.Marshal(p)
}

// Read reads an order version as it was saved.
func Read(doc storage.Document) (Draft, error) {
	if doc.Type != storage.DocumentPurchaseOrder {
		return Draft{}, &Error{CodeNotOrder, fmt.Sprintf("document %s is a %s, not a purchase order", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, "the saved order cannot be read: " + what, "Save the order again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, bad(err.Error())
	}
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Draft{}, bad(err.Error())
	}
	d := Draft{SupplierID: p.SupplierID, Reference: p.Reference, Currency: cur, Notes: p.Notes}
	for _, x := range []struct {
		text string
		to   *time.Time
	}{{p.Date, &d.Date}, {p.ExpectedOn, &d.ExpectedOn}} {
		if x.text == "" {
			continue
		}
		if *x.to, err = time.Parse(time.DateOnly, x.text); err != nil {
			return Draft{}, bad(fmt.Sprintf("the date %q", x.text))
		}
	}
	for _, l := range p.Lines {
		q, err := money.ParseQuantity(l.Quantity, "quantity")
		if err != nil {
			return Draft{}, bad(err.Error())
		}
		price, err := money.ParseUnitPrice(l.UnitPrice, cur, "unit_price")
		if err != nil {
			return Draft{}, bad(err.Error())
		}
		d.Lines = append(d.Lines, Line{AccountID: l.AccountID, Description: l.Description, Quantity: q, UnitPrice: price, GSTCode: l.GSTCode})
	}
	return d, nil
}

// PreparedLine is a line as the order shows it: its amount under RND-LINE and the GST expected on it.
type PreparedLine struct {
	Line
	Amount money.Money
	GST    money.Money
}

// Prepared is an order version with what it comes to: each line's amount and GST, the net, GST and total in its
// currency, and the total in the base currency (at the rate for its date), on which its approval is judged (FM-2).
type Prepared struct {
	Draft     Draft
	Supplier  storage.Contact
	Lines     []PreparedLine
	Net       money.Money
	GST       money.Money
	Total     money.Money
	TotalBase money.Money
}

// Prepare reads one version of an order and checks it: the supplier, the date, each line's account, quantity, price
// and GST code, and a total above zero; problems come back together (ENG-22). The GST expected is RND-GST-EXCL of each
// standard-rated line at the rate in force on the order's date; the total in the base currency is RND-FX at the
// cascade's rate for that date (ENG-40).
func Prepare(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency) (Prepared, ledger.Problems, error) {
	d, err := Read(doc)
	if err != nil {
		return Prepared{}, nil, err
	}
	out := Prepared{Draft: d, Net: money.Zero(d.Currency), GST: money.Zero(d.Currency), Total: money.Zero(d.Currency), TotalBase: money.Zero(base)}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	if d.SupplierID == uuid.Nil {
		add(CodeSupplier, "supplier_id", "the order has no supplier", "Choose the supplier it is for")
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
	if d.Date.IsZero() {
		add(CodeNoDate, "date", "the order has no date", "Enter the date of the order")
	}
	if len(d.Lines) == 0 {
		add(CodeLines, "lines", "the order has no lines", "Add a line for what is ordered")
	}
	codes, err := bill.PurchaseCodes(ctx, u, d.Date)
	if err != nil {
		return Prepared{}, nil, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Prepared{}, nil, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	for i, l := range d.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		a, ok := byID[l.AccountID]
		switch {
		case !ok:
			add(CodeAccount, f("account_id"), "the account does not exist in this organisation", "Choose an expense or asset account from the chart")
		case a.Group || !a.Active || (a.Type != storage.Expense && a.Type != storage.Asset):
			add(CodeAccount, f("account_id"), fmt.Sprintf("account %s is not an active expense or asset account a line can post to", a.Code),
				"Choose an expense or asset account under a heading")
		}
		if len(l.Description) > ledger.MaxDescription {
			add(CodeTooLong, f("description"), fmt.Sprintf("the description has %d characters; the most is %d", len(l.Description), ledger.MaxDescription), "Shorten it")
		}
		if l.Quantity.Sign() <= 0 {
			add(CodeQuantity, f("quantity"), fmt.Sprintf("the quantity is %s", l.Quantity), "Enter how many are ordered, more than zero")
		}
		if l.UnitPrice.Sign() < 0 {
			add(CodeQuantity, f("unit_price"), fmt.Sprintf("the unit price is %s", l.UnitPrice), "Enter the price of one, zero or more; a discount is a lower price")
		}
		var code *gstCode
		for _, c := range codes.Codes {
			if c.Code == l.GSTCode {
				code = &gstCode{standard: c.StandardRated}
			}
		}
		if code == nil {
			add(CodeGSTCode, f("gst_code"), fmt.Sprintf("%q is not a GST code for purchases", l.GSTCode), "Choose the GST code the supplier will charge, such as TX, BL or NR")
		}
		amount, err := rounding.Line(l.Quantity, l.UnitPrice)
		if err != nil {
			add(CodeQuantity, f("quantity"), err.Error(), "Check the quantity and the price")
			continue
		}
		gst := money.Zero(d.Currency)
		if code != nil && code.standard && codes.Rate != "" {
			sub, err := rounding.GSTExcl([]rounding.GSTNet{{Code: l.GSTCode, Rate: codes.Rate, Net: amount}}, rounding.GSTPerSubtotal)
			if err != nil {
				return Prepared{}, nil, err
			}
			gst = sub[0].GST
		}
		out.Lines = append(out.Lines, PreparedLine{Line: l, Amount: amount, GST: gst})
		out.Net, _ = out.Net.Add(amount)
		out.GST, _ = out.GST.Add(gst)
	}
	out.Total, _ = out.Net.Add(out.GST)
	if len(d.Lines) > 0 && len(out.Lines) == len(d.Lines) && out.Total.Sign() <= 0 {
		add(CodeLines, "lines", fmt.Sprintf("the order's total is %s %s", out.Total, d.Currency), "An order's total is more than zero")
	}
	if len(probs) > 0 {
		return out, probs, nil
	}
	if d.Currency == base {
		out.TotalBase = out.Total
		return out, nil, nil
	}
	r, err := fx.Lookup(ctx, u, base, d.Currency, d.Date)
	var fe *fx.Error
	switch {
	case errors.As(err, &fe):
		return out, ledger.Problems{{Code: fe.Code, Field: "date", Message: fe.Message, Fix: fe.Fix}}, nil
	case err != nil:
		return Prepared{}, nil, err
	}
	if out.TotalBase, err = rounding.FX(out.Total, r.Rate); err != nil {
		return Prepared{}, nil, err
	}
	return out, nil, nil
}

type gstCode struct{ standard bool }

// LineState is what an order line has been billed: the quantity ordered, on posted bills, and on draft bills, and
// what is left to bill once the drafts post.
type LineState struct {
	Ordered, Billed, Drafted money.Quantity
}

// Left is what of the line no bill, posted or draft, takes yet.
func (s LineState) Left() money.Quantity { return s.Ordered.Sub(s.Billed).Sub(s.Drafted) }

// Billing is each line's state, from the bills made from the order, posted and draft, except the bill except (uuid.Nil
// for none): what is left for that bill to take.
func Billing(ctx context.Context, u storage.UnitOfWork, orderID uuid.UUID, d Draft, base money.Currency, except uuid.UUID) ([]LineState, []storage.Document, error) {
	out := make([]LineState, len(d.Lines))
	for i, l := range d.Lines {
		out[i].Ordered = l.Quantity
	}
	bills, err := u.Documents().BillsOfOrder(ctx, orderID)
	if err != nil {
		return nil, nil, err
	}
	var kept []storage.Document
	for _, doc := range bills {
		if doc.ID == except {
			continue
		}
		b, err := bill.Read(doc, base)
		if err != nil {
			return nil, nil, err
		}
		for _, l := range b.Lines {
			if l.Order == nil || l.Order.PurchaseOrderID != orderID || l.Order.Line < 1 || l.Order.Line > len(out) {
				continue
			}
			s := &out[l.Order.Line-1]
			if doc.Status == storage.DocumentPosted {
				s.Billed = s.Billed.Add(l.Order.Quantity)
			} else {
				s.Drafted = s.Drafted.Add(l.Order.Quantity)
			}
		}
		kept = append(kept, doc)
	}
	return out, kept, nil
}

// CheckBillLines checks the lines of a bill made from purchase orders, as the bill would post (FR-AP-01): each names a
// line of an approved order, not closed, of the bill's supplier and currency, bills a quantity above zero, and takes
// no more of the line than posted bills have left of it. A vendor credit's line never names an order.
func CheckBillLines(ctx context.Context, u storage.UnitOfWork, doc storage.Document, d bill.Draft, base money.Currency) (ledger.Problems, error) {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeOrderLine, Field: field, Message: msg, Fix: fix})
	}
	taken := map[uuid.UUID][]money.Quantity{} // what this bill takes of each order's lines
	orders := map[uuid.UUID]Draft{}
	for i, l := range d.Lines {
		if l.Order == nil {
			continue
		}
		f := fmt.Sprintf("lines[%d].order", i+1)
		if doc.Type == storage.DocumentVendorCredit {
			add(f, "a credit note's line is not made from a purchase order", "Remove the order from the line")
			continue
		}
		o, ok := orders[l.Order.PurchaseOrderID]
		if !ok {
			od, err := u.Documents().Get(ctx, l.Order.PurchaseOrderID)
			if errors.Is(err, ports.ErrNotFound) || (err == nil && od.Type != storage.DocumentPurchaseOrder) {
				add(f, "the line names a purchase order that does not exist in this organisation", "Make the bill from the order again")
				continue
			}
			if err != nil {
				return nil, err
			}
			if od.Status != storage.DocumentPosted {
				add(f, fmt.Sprintf("purchase order %s is %s, not approved", od.Number, statusWord(od.Status)), "Bill an approved order that is not closed")
				continue
			}
			if o, err = Read(od); err != nil {
				return nil, err
			}
			if o.SupplierID != d.SupplierID || o.Currency != d.Currency {
				add(f, fmt.Sprintf("purchase order %s is for another supplier or currency", od.Number), "Bill an order of the bill's own supplier, in its currency")
				continue
			}
			orders[l.Order.PurchaseOrderID] = o
			taken[l.Order.PurchaseOrderID] = make([]money.Quantity, len(o.Lines))
		}
		if l.Order.Line < 1 || l.Order.Line > len(o.Lines) {
			add(f, fmt.Sprintf("the order has no line %d", l.Order.Line), "Make the bill from the order again")
			continue
		}
		if l.Order.Quantity.Sign() <= 0 {
			add(f, fmt.Sprintf("the line bills a quantity of %s", l.Order.Quantity), "Bill a quantity above zero, or remove the line")
			continue
		}
		t := taken[l.Order.PurchaseOrderID]
		t[l.Order.Line-1] = t[l.Order.Line-1].Add(l.Order.Quantity)
	}
	for id, t := range taken {
		states, _, err := Billing(ctx, u, id, orders[id], base, doc.ID)
		if err != nil {
			return nil, err
		}
		for k, q := range t {
			if q.IsZero() {
				continue
			}
			left := states[k].Ordered.Sub(states[k].Billed)
			if q.Cmp(left) > 0 {
				add("lines", fmt.Sprintf("the bill takes %s of the order's line %d, but posted bills leave %s of it", q, k+1, left),
					"Bill at most what is left; an order that grew is changed by closing it and raising another")
			}
		}
	}
	return probs, nil
}

func statusWord(s storage.DocumentStatus) string {
	switch s {
	case storage.DocumentReversed:
		return "closed"
	case storage.DocumentDraft:
		return "a draft"
	}
	return string(s)
}
