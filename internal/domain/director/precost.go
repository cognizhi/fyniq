package director

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a cost before the books start (FR-EQ-03; design D7).
const (
	CodeNotPreCost  = "FOUNDER-05" // the document is not a cost before the books start
	CodePreCost     = "FOUNDER-06" // a cost that does not fit: its date, supplier, description, person or amounts
	CodePreCostDone = "FOUNDER-07" // a cost already decided, which changes no more
	CodeNoReceipt   = "FOUNDER-08" // a cost decided with no receipt attached
	CodeTreatment   = "FOUNDER-09" // a decision with no treatment Fyniq knows
)

// The accountant's income-tax treatments of a cost before the books start (FR-EQ-03): a revenue cost from up to 12
// months before the deemed start of business is deductible (Income Tax Act s14R), an incorporation cost is capital, and
// anything else is not deductible. Founders' revenue costs from before incorporation stay with the tax agent [verify].
const (
	TreatmentDeductible    = "deductible"
	TreatmentCapital       = "capital"
	TreatmentNotDeductible = "not_deductible"
)

// Treatments are the treatments a decision may record.
var Treatments = []string{TreatmentDeductible, TreatmentCapital, TreatmentNotDeductible}

// The longest supplier, description and note a cost keeps.
const (
	MaxSupplier    = 200
	MaxDescription = 500
	MaxNote        = 1000
)

// PreCost is a cost a director or shareholder paid before the books start (FR-EQ-03): its original date, before the
// books start; the supplier and what it was for; the person who paid it; what they paid in the base currency and the GST
// the receipt shows; and, once the accountant decides it, their decision. Until then it needs a decision (⚠) and posts
// nothing.
type PreCost struct {
	Date        time.Time
	Supplier    string
	Description string
	ContactID   uuid.UUID
	Gross       money.Money
	GST         *money.Money
	Decision    *Decision
}

// Decision is the accountant's decision on a cost (design D7): the treatment, the account it posts to, whether the GST
// is claimed as input tax (only while GST-registered on the posting date; a claim from before registration is made in
// the first GST return with IRAS's approval [verify]), a note, the day it posts (the books start, or the day of the
// decision when the books start's period is closed), and who decided it on which day.
type Decision struct {
	Treatment string
	AccountID uuid.UUID
	ClaimGST  bool
	Note      string
	PostOn    time.Time
	DecidedBy string
	DecidedOn time.Time
}

type decisionPayload struct {
	Treatment string    `json:"treatment"`
	AccountID uuid.UUID `json:"account_id"`
	ClaimGST  bool      `json:"claim_gst,omitempty"`
	Note      string    `json:"note,omitempty"`
	PostOn    string    `json:"post_on"`
	DecidedBy string    `json:"decided_by"`
	DecidedOn string    `json:"decided_on"`
}

type preCostPayload struct {
	Date        string           `json:"date,omitempty"`
	Supplier    string           `json:"supplier"`
	Description string           `json:"description"`
	ContactID   uuid.UUID        `json:"contact_id"`
	Gross       *money.Money     `json:"gross,omitempty"`
	GST         *money.Money     `json:"gst,omitempty"`
	Decision    *decisionPayload `json:"decision,omitempty"`
}

func dayOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

// EncodePreCost is a cost as a document version stores it.
func EncodePreCost(c PreCost) ([]byte, error) {
	gross := c.Gross
	p := preCostPayload{Date: dayOf(c.Date), Supplier: c.Supplier, Description: c.Description, ContactID: c.ContactID, Gross: &gross, GST: c.GST}
	if d := c.Decision; d != nil {
		p.Decision = &decisionPayload{Treatment: d.Treatment, AccountID: d.AccountID, ClaimGST: d.ClaimGST, Note: d.Note, PostOn: dayOf(d.PostOn),
			DecidedBy: d.DecidedBy, DecidedOn: dayOf(d.DecidedOn)}
	}
	return json.Marshal(p)
}

// IsPreCost says whether a document is a cost before the books start.
func IsPreCost(doc storage.Document) bool { return doc.Type == storage.DocumentPreIncorporationCost }

// ReadPreCost reads a cost's version as it was saved.
func ReadPreCost(doc storage.Document) (PreCost, error) {
	if !IsPreCost(doc) {
		return PreCost{}, &Error{CodeNotPreCost, fmt.Sprintf("document %s is a %s, not a cost before the books start", doc.ID, doc.Type), "Open it with its own module"}
	}
	bad := func(what string) error {
		return &Error{CodePreCost, "the saved cost cannot be read: " + what, "Record it again"}
	}
	var p preCostPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return PreCost{}, bad(err.Error())
	}
	if p.Gross == nil {
		return PreCost{}, bad("it has no amount")
	}
	parse := func(s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, nil
		}
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return t, bad(fmt.Sprintf("the date %q", s))
		}
		return t, nil
	}
	c := PreCost{Supplier: p.Supplier, Description: p.Description, ContactID: p.ContactID, Gross: *p.Gross, GST: p.GST}
	var err error
	if c.Date, err = parse(p.Date); err != nil {
		return PreCost{}, err
	}
	if d := p.Decision; d != nil {
		c.Decision = &Decision{Treatment: d.Treatment, AccountID: d.AccountID, ClaimGST: d.ClaimGST, Note: d.Note, DecidedBy: d.DecidedBy}
		if c.Decision.PostOn, err = parse(d.PostOn); err != nil {
			return PreCost{}, err
		}
		if c.Decision.DecidedOn, err = parse(d.DecidedOn); err != nil {
			return PreCost{}, err
		}
	}
	return c, nil
}

// CheckPreCost is what is wrong with a cost as recorded, every problem at once (ENG-22): a date on or after the books
// start (or no books start set), no supplier or description, a person who is not a director or shareholder, or an
// amount or GST that is not in the base currency, the GST less than the amount.
func CheckPreCost(ctx context.Context, u storage.UnitOfWork, c PreCost, booksStart *time.Time, base money.Currency) (ledger.Problems, error) {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodePreCost, Field: field, Message: msg, Fix: fix})
	}
	switch {
	case booksStart == nil:
		add("date", "the organisation has no books start, so nothing is before it", "Set the books start in Settings › Organisation first")
	case c.Date.IsZero():
		add("date", "the cost has no date", "Enter the date on the receipt")
	case !c.Date.Before(*booksStart):
		add("date", fmt.Sprintf("the cost is dated %s, not before the books start on %s", dayOf(c.Date), dayOf(*booksStart)),
			"Record a cost from the books start on as a claim or a bill instead")
	}
	text := func(field, value, what string, most int) {
		switch n := utf8.RuneCountInString(value); {
		case strings.TrimSpace(value) == "":
			add(field, fmt.Sprintf("the cost has no %s", what), fmt.Sprintf("Enter the %s from the receipt", what))
		case n > most:
			add(field, fmt.Sprintf("the %s has %d characters; the most is %d", what, n, most), "Shorten it")
		}
	}
	text("supplier", c.Supplier, "supplier", MaxSupplier)
	text("description", c.Description, "description", MaxDescription)
	ct, err := u.Contacts().Get(ctx, c.ContactID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		probs = append(probs, ledger.Problem{Code: CodeNotPerson, Field: "contact_id", Message: "the contact does not exist in this organisation",
			Fix: "Choose the director or shareholder who paid it"})
	case err != nil:
		return nil, err
	case !ct.Director && !ct.Shareholder:
		probs = append(probs, ledger.Problem{Code: CodeNotPerson, Field: "contact_id", Message: ct.LegalName + " is not a director or shareholder",
			Fix: "Mark the contact as a director or shareholder in Contacts, or choose another"})
	}
	if c.Gross.Currency() != base || c.Gross.Sign() <= 0 {
		add("gross", fmt.Sprintf("the amount is %s %s", c.Gross, c.Gross.Currency()), fmt.Sprintf("Enter what they paid in %s, more than zero", base))
	}
	if g := c.GST; g != nil {
		more := 0
		if g.Currency() == base && c.Gross.Currency() == base {
			more, _ = g.Cmp(c.Gross)
		}
		if g.Currency() != base || g.Sign() < 0 || (c.Gross.Sign() > 0 && more >= 0) {
			add("gst", fmt.Sprintf("the GST shown is %s %s", g, g.Currency()), fmt.Sprintf("Enter the GST the receipt shows in %s: zero or more, less than the amount", base))
		}
	}
	return probs, nil
}

// CheckDecision is what is wrong with a decision as made, every problem at once: a treatment Fyniq does not know, no
// account, or a note too long. The rule checks the account and the GST claim when it posts.
func CheckDecision(d Decision) ledger.Problems {
	var probs ledger.Problems
	known := false
	for _, t := range Treatments {
		known = known || d.Treatment == t
	}
	if !known {
		probs = append(probs, ledger.Problem{Code: CodeTreatment, Field: "treatment", Message: fmt.Sprintf("the treatment %q is not one Fyniq knows", d.Treatment),
			Fix: "Choose deductible, capital or not_deductible"})
	}
	if d.AccountID == uuid.Nil {
		probs = append(probs, ledger.Problem{Code: CodePreCost, Field: "account_id", Message: "the decision names no account", Fix: "Choose the account the cost posts to"})
	}
	if n := utf8.RuneCountInString(d.Note); n > MaxNote {
		probs = append(probs, ledger.Problem{Code: CodePreCost, Field: "note", Message: fmt.Sprintf("the note has %d characters; the most is %d", n, MaxNote), Fix: "Shorten it"})
	}
	return probs
}

// PostOn is the day a decided cost posts (design D7): the books start, or the day of the decision when the books start's
// period is closed.
func PostOn(periods []storage.Period, booksStart, decidedOn time.Time) time.Time {
	for _, p := range periods {
		if !booksStart.Before(p.StartsOn) && !booksStart.After(p.EndsOn) && p.Status == storage.PeriodClosed {
			return decidedOn
		}
	}
	return booksStart
}

// Rule is a decided cost as the rule reads it.
func (c PreCost) Rule(doc storage.Document, base money.Currency, registered bool) ledger.PreIncorporationCost {
	r := ledger.PreIncorporationCost{ID: doc.ID, Version: doc.Version, Memo: strings.TrimSpace(doc.Number + " " + c.Description), Party: c.ContactID,
		Gross: c.Gross, GST: money.Zero(base), Registered: registered}
	if c.GST != nil {
		r.GST = *c.GST
	}
	if d := c.Decision; d != nil {
		r.PostOn, r.Account, r.ClaimGST = d.PostOn, d.AccountID, d.ClaimGST
	}
	return r
}
