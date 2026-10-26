// Package claim is a member's expense claim (FR-AP-07, FR-AP-14, FR-AP-17; PRD-001 §8.6; p1-expense-claims): its
// content as each version saves it, and what the posting rule expense_claim@1 needs from it. Reading a claim for the
// rule happens here: the claimant, each receipt's claim category and the account it costs, the GST codes of the sg-gst
// pack in force on the receipt's date, the organisation's GST registration that decides whether input tax is claimable
// (FR-AP-20, as a bill line's is), and each foreign-currency receipt's rate (ENG-40). The expense policy's categories
// are here too (FR-AP-15).
package claim

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a claim's refusals (ENG-23).
const (
	CodeNotClaim = "CLAIM-01" // the document is not an expense claim
	CodeNotDraft = "CLAIM-02" // only a draft changes or is submitted
	CodeBadDraft = "CLAIM-03" // the saved claim cannot be read
	CodeClaimant = "CLAIM-04" // no claimant, or one who is not an active member
	CodeCategory = "CLAIM-05" // a receipt's category that does not exist, is inactive, or is the mileage category
	CodeTooLong  = "CLAIM-06" // a title, merchant, description, attendees or notes longer than the most kept
	CodeReceipt  = "CLAIM-07" // a receipt without its date or merchant, dated after the claim, or with facts Fyniq cannot read
	CodeGSTCode  = "CLAIM-08" // a GST code that is not a purchase code of the sg-gst pack
	// CLAIM-09 to CLAIM-11 are the API's, CLAIM-12 to CLAIM-16 a reimbursement's, CLAIM-17 the API's.
	CodeMileage     = "CLAIM-18" // a mileage line without its vehicle, places or distance, or one Fyniq cannot read
	CodeMileageRate = "CLAIM-19" // no rate per km set for the vehicle on the trip's date
)

// The longest texts a claim keeps.
const (
	MaxTitle     = 120
	MaxMerchant  = 120
	MaxAttendees = 500
	MaxNotes     = 2000
)

// The kinds of line: a receipt, what was bought as the receipt shows it; or a trip in the member's own vehicle (FR-AP-16).
const (
	LineReceipt = "receipt"
	LineMileage = "mileage"
)

// The vehicles a mileage line is driven in, each with the setting that holds its rate per km.
var Vehicles = map[string]string{"car": "claims.mileage_rate_car", "motorcycle": "claims.mileage_rate_motorcycle"}

// MaxPlace is the longest place a mileage line keeps: a saved place's address, or one typed.
const MaxPlace = 300

// Error is a claim command's refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is a claim as its claimant enters it: who claims (a member's user ID), the claim's date (its entry's posting
// date), what it is for, its lines and notes.
type Draft struct {
	ClaimantID uuid.UUID
	Date       time.Time
	Title      string
	Lines      []Line
	Notes      string
}

// Line is one line of a claim. A mileage line (Kind LineMileage) is a trip: its date, its category (the mileage
// category), what it was for (Description), where from and to, whether it was a return trip, the vehicle (one of
// Vehicles) and the distance driven in km, as typed: the whole trip's (UC-EXP-02); its amount is RND-MILEAGE's at the
// vehicle's rate on its date, with no GST. A receipt line (Kind LineReceipt, or empty) is: its date, the merchant, its claim category, what it was for, its amount including GST
// and the GST it states, both in its currency, and the GST code. For a receipt in another currency, Rate is the rate
// as typed (nil takes the cascade's for the receipt's date, ENG-40), StatedGST the GST its tax invoice states in the
// base currency (ENG-42; nil: none is claimed), and Paid what the claimant paid in the base currency, as their card
// statement shows it (nil: the amount at the rate). Attendees name who attended a meal or entertainment (FR-AP-15).
// Document and AddressedTo are the facts its input tax is decided from (FR-AP-20, bill.Documents and bill.AddressedTo;
// empty is a tax invoice addressed to the company).
type Line struct {
	Kind        string
	Date        time.Time
	Merchant    string
	CategoryID  uuid.UUID
	Description string
	Amount      money.Money
	GSTCode     string
	GST         money.Money
	StatedGST   *money.Money
	Paid        *money.Money
	Rate        *money.Rate
	Attendees   string
	Document    string
	AddressedTo string
	From, To    string
	Return      bool
	Vehicle     string
	Distance    money.Quantity
}

// payload is a claim as a document version stores it: dates as text, amounts with their currency.
type payload struct {
	ClaimantID uuid.UUID     `json:"claimant_id"`
	Date       string        `json:"date,omitempty"`
	Title      string        `json:"title,omitempty"`
	Lines      []payloadLine `json:"lines"`
	Notes      string        `json:"notes,omitempty"`
}

type payloadLine struct {
	Kind         string       `json:"kind"`
	Date         string       `json:"date,omitempty"`
	Merchant     string       `json:"merchant,omitempty"`
	CategoryID   uuid.UUID    `json:"category_id"`
	Description  string       `json:"description,omitempty"`
	Amount       *money.Money `json:"amount,omitempty"`
	GSTCode      string       `json:"gst_code,omitempty"`
	GST          *money.Money `json:"gst,omitempty"`
	StatedGST    *money.Money `json:"stated_gst,omitempty"`
	Paid         *money.Money `json:"paid,omitempty"`
	FXRate       string       `json:"fx_rate,omitempty"`
	FXRateSource string       `json:"fx_rate_source,omitempty"`
	Attendees    string       `json:"attendees,omitempty"`
	Document     string       `json:"document,omitempty"`
	AddressedTo  string       `json:"addressed_to,omitempty"`
	From         string       `json:"from,omitempty"`
	To           string       `json:"to,omitempty"`
	Return       bool         `json:"return,omitempty"`
	Vehicle      string       `json:"vehicle,omitempty"`
	Distance     string       `json:"distance,omitempty"`
}

func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

// Encode is a draft as a document version stores it; texts longer than the most kept, and input-tax facts Fyniq cannot
// read, are refused.
func Encode(d Draft) ([]byte, error) {
	for _, x := range []struct {
		field, text string
		most        int
	}{{"title", d.Title, MaxTitle}, {"notes", d.Notes, MaxNotes}} {
		if len(x.text) > x.most {
			return nil, &Error{CodeTooLong, fmt.Sprintf("%s: %d characters; the most is %d", x.field, len(x.text), x.most), "Shorten it"}
		}
	}
	p := payload{ClaimantID: d.ClaimantID, Date: day(d.Date), Title: d.Title, Notes: d.Notes, Lines: []payloadLine{}}
	for i, r := range d.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		if r.Kind == LineMileage {
			pl, err := encodeTrip(r, f)
			if err != nil {
				return nil, err
			}
			p.Lines = append(p.Lines, pl)
			continue
		}
		if r.Kind != "" && r.Kind != LineReceipt {
			return nil, &Error{CodeReceipt, fmt.Sprintf("%s: %q is not a kind of line Fyniq reads", f("kind"), r.Kind), "Enter a receipt or a mileage line"}
		}
		for _, x := range []struct {
			field, text string
			most        int
		}{{"merchant", r.Merchant, MaxMerchant}, {"description", r.Description, MaxDescription}, {"attendees", r.Attendees, MaxAttendees}} {
			if len(x.text) > x.most {
				return nil, &Error{CodeTooLong, fmt.Sprintf("%s: %d characters; the most is %d", f(x.field), len(x.text), x.most), "Shorten it"}
			}
		}
		for _, x := range []struct {
			field, value string
			known        []string
		}{{"document", r.Document, bill.Documents}, {"addressed_to", r.AddressedTo, bill.AddressedTo}} {
			if x.value != "" && !slices.Contains(x.known, x.value) {
				return nil, &Error{CodeReceipt, fmt.Sprintf("%s: %q is not one of %s", f(x.field), x.value, strings.Join(x.known, ", ")), "Choose one of them"}
			}
		}
		if r.Document == bill.DocImportPermit {
			return nil, &Error{CodeReceipt, fmt.Sprintf("%s: a receipt is not an import permit", f("document")),
				"Choose a tax invoice, a simplified tax invoice, or none; record an import as a bill"}
		}
		amount, gst := r.Amount, r.GST
		pl := payloadLine{Kind: LineReceipt, Date: day(r.Date), Merchant: r.Merchant, CategoryID: r.CategoryID, Description: r.Description,
			Amount: &amount, GSTCode: r.GSTCode, GST: &gst, StatedGST: r.StatedGST, Paid: r.Paid, Attendees: r.Attendees,
			Document: or(r.Document, bill.DocTaxInvoice, ""), AddressedTo: or(r.AddressedTo, bill.ToCompany, "")}
		if r.Rate != nil {
			pl.FXRate, pl.FXRateSource = r.Rate.String(), r.Rate.Source
		}
		p.Lines = append(p.Lines, pl)
	}
	return json.Marshal(p)
}

// or is s, but empty when it is the usual value def; empty when s is: the payload keeps only what differs.
func or(s, def, empty string) string {
	if s == def {
		return empty
	}
	return s
}

// MaxDescription is the longest description a receipt line keeps: the ledger's (db/migrations/000004).
const MaxDescription = 500

// IsClaim says whether a document is an expense claim.
func IsClaim(doc storage.Document) bool { return doc.Type == storage.DocumentExpenseClaim }

// Read reads an expense claim version as it was saved; base is the organisation's base currency, which a typed rate
// converts into.
func Read(doc storage.Document, base money.Currency) (Draft, error) {
	if !IsClaim(doc) {
		return Draft{}, &Error{CodeNotClaim, fmt.Sprintf("document %s is a %s, not an expense claim", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodeBadDraft, fmt.Sprintf("the saved claim cannot be read: %s", what), "Save the claim again"}
	}
	var p payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Draft{}, bad(err.Error())
	}
	d := Draft{ClaimantID: p.ClaimantID, Title: p.Title, Notes: p.Notes}
	var err error
	if p.Date != "" {
		if d.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
			return Draft{}, bad(fmt.Sprintf("the date %q", p.Date))
		}
	}
	for _, l := range p.Lines {
		if l.Kind == LineMileage {
			t, err := readTrip(l)
			if err != nil {
				return Draft{}, bad(err.Error())
			}
			d.Lines = append(d.Lines, t)
			continue
		}
		if l.Kind != LineReceipt || l.Amount == nil || l.GST == nil {
			return Draft{}, bad(fmt.Sprintf("a line of kind %q", l.Kind))
		}
		r := Line{Kind: LineReceipt, Merchant: l.Merchant, CategoryID: l.CategoryID, Description: l.Description, Amount: *l.Amount, GSTCode: l.GSTCode, GST: *l.GST,
			StatedGST: l.StatedGST, Paid: l.Paid, Attendees: l.Attendees, Document: l.Document, AddressedTo: l.AddressedTo}
		if l.Date != "" {
			if r.Date, err = time.Parse(time.DateOnly, l.Date); err != nil {
				return Draft{}, bad(fmt.Sprintf("the receipt's date %q", l.Date))
			}
		}
		if l.FXRate != "" {
			x, err := money.ParseRate(l.FXRate, l.Amount.Currency(), base, l.FXRateSource)
			if err != nil {
				return Draft{}, bad(err.Error())
			}
			r.Rate = &x
		}
		d.Lines = append(d.Lines, r)
	}
	return d, nil
}

// encodeTrip is a mileage line as a document version stores it; its places and vehicle are checked.
func encodeTrip(r Line, f func(string) string) (payloadLine, error) {
	for _, x := range []struct {
		field, text string
		most        int
	}{{"description", r.Description, MaxDescription}, {"from", r.From, MaxPlace}, {"to", r.To, MaxPlace}} {
		if len(x.text) > x.most {
			return payloadLine{}, &Error{CodeTooLong, fmt.Sprintf("%s: %d characters; the most is %d", f(x.field), len(x.text), x.most), "Shorten it"}
		}
	}
	if _, ok := Vehicles[r.Vehicle]; !ok {
		return payloadLine{}, &Error{CodeMileage, fmt.Sprintf("%s: %q is not a vehicle Fyniq has a rate for", f("vehicle"), r.Vehicle), "Choose car or motorcycle"}
	}
	return payloadLine{Kind: LineMileage, Date: day(r.Date), CategoryID: r.CategoryID, Description: r.Description, From: r.From, To: r.To,
		Return: r.Return, Vehicle: r.Vehicle, Distance: r.Distance.String()}, nil
}

// readTrip reads a mileage line as it was saved.
func readTrip(l payloadLine) (Line, error) {
	t := Line{Kind: LineMileage, CategoryID: l.CategoryID, Description: l.Description, From: l.From, To: l.To, Return: l.Return, Vehicle: l.Vehicle}
	if l.Date != "" {
		d, err := time.Parse(time.DateOnly, l.Date)
		if err != nil {
			return Line{}, fmt.Errorf("the trip's date %q", l.Date)
		}
		t.Date = d
	}
	q, err := money.ParseQuantity(l.Distance, "distance")
	if err != nil {
		return Line{}, fmt.Errorf("the distance %q", l.Distance)
	}
	t.Distance = q
	return t, nil
}
