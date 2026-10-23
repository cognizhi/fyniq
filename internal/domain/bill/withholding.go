package bill

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The refusals of withholding tax on a bill (ENG-23; rev 0.90).
const (
	CodeNature      = "BILL-16" // a non-resident supplier's bill without a nature of payment Fyniq reads
	CodeWithholding = "BILL-17" // a withholding decision Fyniq cannot record
)

// WarnWHTUndecided is the ⚠ of a non-resident supplier's bill whose nature has no decision yet: it posts without
// withholding, and a payment of it waits for the decision (FR-AP-19).
const WarnWHTUndecided = "BILL-WHT-UNDECIDED"

// The natures of a payment to a non-resident supplier (FR-AP-19).
const (
	NatureGoods           = "goods"
	NatureSoftwareUse     = "software_use"    // software bought for the company's own use, such as a subscription
	NatureSoftwareRights  = "software_rights" // software rights or royalties
	NatureServicesIn      = "services_in_singapore"
	NatureServicesOutside = "services_outside_singapore" // services performed wholly outside Singapore
	NatureInterest        = "interest"
	NatureMovableRent     = "movable_rent" // rent of movable property
	NatureOther           = "other"
)

// Natures are the natures of payment a bill records.
var Natures = []string{NatureGoods, NatureSoftwareUse, NatureSoftwareRights, NatureServicesIn, NatureServicesOutside, NatureInterest,
	NatureMovableRent, NatureOther}

// NatureWords are the natures of payment in plain words.
var NatureWords = map[string]string{NatureGoods: "goods", NatureSoftwareUse: "software for the company's own use",
	NatureSoftwareRights: "software rights or royalties", NatureServicesIn: "services performed in Singapore",
	NatureServicesOutside: "services performed wholly outside Singapore", NatureInterest: "interest",
	NatureMovableRent: "rent of movable property", NatureOther: "other payments"}

// The outcomes of the tax agent's withholding decision.
const (
	OutcomeNotApplicable = "not_applicable"
	OutcomeWithhold      = "withhold" // at the rate the tax agent advised
	OutcomeTreaty        = "treaty"   // a reduced treaty rate, with a certificate of residence
)

// Outcomes are the outcomes a withholding decision records.
var Outcomes = []string{OutcomeNotApplicable, OutcomeWithhold, OutcomeTreaty}

// Home is where the organisation is resident for tax: Singapore, until other countries come (FR-TAX-09).
const Home = "SG"

// MaxTaxAgent and MaxDecisionNote are the longest tax agent's name and note a decision keeps.
const (
	MaxTaxAgent     = 200
	MaxDecisionNote = 2000
)

// Residence is where a contact is resident for tax: its tax residence, else its country; empty when neither is recorded.
func Residence(c storage.Contact) string {
	if c.TaxResidence != "" {
		return c.TaxResidence
	}
	return c.Country
}

// NonResident reports whether a contact is recorded as resident for tax outside Singapore. A contact with neither a tax
// residence nor a country is taken as resident.
func NonResident(c storage.Contact) bool {
	r := Residence(c)
	return r != "" && r != Home
}

// Withholding is how withholding tax stands for a bill (FR-AP-19): whether its supplier is non-resident, the nature of
// the payment, the tax agent's decision in force when it posts (or posted), and the one in force now.
type Withholding struct {
	NonResident bool
	Residence   string
	Nature      string
	Decision    *storage.WithholdingDecision
	Current     *storage.WithholdingDecision
}

// rateOf is the rate withheld under a decision: "" when it withholds nothing, or there is none.
func rateOf(d *storage.WithholdingDecision) string {
	if d == nil || d.Outcome == OutcomeNotApplicable {
		return ""
	}
	return d.RatePercent
}

// Rate is the rate the bill withholds, as a percentage: "" when it withholds nothing.
func (w Withholding) Rate() string { return rateOf(w.Decision) }

// Held says why a payment of a posted bill waits, "" when it does not: no decision yet for its supplier and nature, or
// one since it posted that withholds at another rate, which an edit of the bill applies (FR-AP-19's hold).
func (w Withholding) Held() string {
	switch {
	case !w.NonResident:
		return ""
	case w.Current == nil:
		return "the tax agent has not decided whether tax is withheld from it"
	case rateOf(w.Current) != w.Rate():
		return "the tax agent's decision changed since it posted"
	}
	return ""
}

// withholdingOf reads how withholding stands for a bill of supplier with nature, posting at at (zero: now): the decision
// in force then, and the one in force now.
func withholdingOf(ctx context.Context, u storage.UnitOfWork, supplier storage.Contact, nature string, at time.Time) (Withholding, error) {
	w := Withholding{NonResident: NonResident(supplier), Residence: Residence(supplier), Nature: nature}
	if !w.NonResident || !slices.Contains(Natures, nature) {
		return w, nil
	}
	find := func(at time.Time) (*storage.WithholdingDecision, error) {
		d, err := u.WithholdingDecisions().InForce(ctx, supplier.ID, nature, at)
		if errors.Is(err, ports.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &d, nil
	}
	var err error
	if w.Current, err = find(time.Time{}); err != nil {
		return Withholding{}, err
	}
	w.Decision = w.Current
	if !at.IsZero() {
		if w.Decision, err = find(at); err != nil {
			return Withholding{}, err
		}
	}
	return w, nil
}

// HeldPayment says why a payment of a posted bill waits for withholding (FR-AP-19's hold), "" when it does not; at is
// when its entry was recorded.
func HeldPayment(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency, at time.Time) (string, error) {
	d, err := Read(doc, base)
	if err != nil {
		return "", err
	}
	c, err := u.Contacts().Get(ctx, d.SupplierID)
	if err != nil {
		return "", err
	}
	w, err := withholdingOf(ctx, u, c, d.Nature, at)
	if err != nil {
		return "", err
	}
	return w.Held(), nil
}

// RecordWithholding records the tax agent's decision for a non-resident supplier and a nature of payment (FR-AP-19):
// not applicable, or withhold at the rate they advised (a reduced treaty rate with a certificate of residence), with
// their name, and the bill whose files hold their advice. It replaces the supplier's earlier decision for the nature
// from now; bills posted before keep theirs.
func RecordWithholding(ctx context.Context, u storage.UnitOfWork, d storage.NewWithholdingDecision, base money.Currency) (storage.WithholdingDecision, error) {
	bad := func(msg, fix string) (storage.WithholdingDecision, error) {
		return storage.WithholdingDecision{}, &Error{CodeWithholding, msg, fix}
	}
	c, err := u.Contacts().Get(ctx, d.SupplierID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return bad("the supplier does not exist in this organisation", "Choose a supplier from your contacts")
	case err != nil:
		return storage.WithholdingDecision{}, err
	case !c.Vendor:
		return bad(fmt.Sprintf("%s is not a supplier", c.LegalName), "Mark the contact as a supplier, or choose another")
	case !NonResident(c):
		return bad(fmt.Sprintf("%s is not recorded as resident outside Singapore, so nothing is withheld from it", c.LegalName),
			"Record the supplier's tax residence first")
	}
	d.TaxAgent, d.Note = strings.TrimSpace(d.TaxAgent), strings.TrimSpace(d.Note)
	switch {
	case !slices.Contains(Natures, d.Nature):
		return bad(fmt.Sprintf("the nature of payment %q is not one of %s", d.Nature, strings.Join(Natures, ", ")), "Choose one of them")
	case !slices.Contains(Outcomes, d.Outcome):
		return bad(fmt.Sprintf("the decision %q is not one of %s", d.Outcome, strings.Join(Outcomes, ", ")), "Choose one of them")
	case d.Outcome == OutcomeNotApplicable && d.RatePercent != "":
		return bad("a decision that nothing is withheld has no rate", "Leave the rate out")
	case d.Outcome != OutcomeNotApplicable && !rounding.IsWHTRate(d.RatePercent):
		return bad(fmt.Sprintf("the rate %q is not a percentage above 0 and up to 100 with at most two decimals", d.RatePercent),
			"Enter the rate the tax agent advised, such as 17")
	case d.TaxAgent == "" || len(d.TaxAgent) > MaxTaxAgent:
		return bad(fmt.Sprintf("the tax agent's name has %d characters", len(d.TaxAgent)), fmt.Sprintf("Name the tax agent who decided, in 1 to %d characters", MaxTaxAgent))
	case len(d.Note) > MaxDecisionNote:
		return bad(fmt.Sprintf("the note has %d characters; the most is %d", len(d.Note), MaxDecisionNote), "Shorten it")
	}
	if d.BillID != nil {
		doc, err := u.Documents().Get(ctx, *d.BillID)
		if errors.Is(err, ports.ErrNotFound) {
			return bad("the bill does not exist in this organisation", "Record the decision from one of the supplier's bills")
		}
		if err != nil {
			return storage.WithholdingDecision{}, err
		}
		b, err := Read(doc, base)
		if err != nil || doc.Type != storage.DocumentBill || b.SupplierID != d.SupplierID {
			return bad("the bill is not one of the supplier's bills", "Record the decision from one of the supplier's bills")
		}
	}
	return u.WithholdingDecisions().Record(ctx, d)
}
