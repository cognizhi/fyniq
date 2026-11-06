package director

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a conversion's effective step (FR-EQ-06; design D11, D12).
const (
	CodeNotApproved = "FOUNDER-35" // only an approved conversion with no share issue yet becomes effective
	CodeEffective   = "FOUNDER-36" // an effective date that is in the future, before the approval, or after the approval lapsed
)

// The price bases of a conversion (FR-EQ-06): the latest priced round, an independent valuation, or a price every
// shareholder approves. The equity register's rounds are not built, so the price is entered with its basis.
const (
	PriceBasisRound     = "latest_priced_round"
	PriceBasisValuation = "valuation"
	PriceBasisUnanimous = "unanimous"
)

// The capacities in which a founder converts (FR-EQ-06): as a lender, an employee or a shareholder. Converting as an
// employee may give rise to pay; the accountant confirms [verify].
const (
	CapacityLender   = "lender"
	CapacityEmployee = "employee"
)

// The measurement bases of a conversion (FR-EQ-06): the carrying amount, the default, or the shares' fair value with the
// difference in profit or loss (IFRIC 19 ¶9; SFRS for Small Entities ¶22.15A–C), which needs a note.
const (
	MeasurementCarrying  = "carrying"
	MeasurementFairValue = "fair_value"
)

// ConversionDocuments are the four documents a conversion needs before it is asked for (FR-EQ-06, FR-DOC-06): the
// shareholders' approval (Companies Act s161; a written resolution, s184A), the board resolution, the written conversion
// agreement and the founder's signed balance confirmation, each attached as its evidence type.
var ConversionDocuments = []string{"shareholders' approval", "board resolution", "conversion agreement", "balance confirmation"}

// Conversion is what a balance converted to shares is (FR-EQ-06, design D11): the class (a share-capital account), the
// price per share in the base currency with its basis, the founder's capacity, and the measurement basis; under fair
// value, the shares' fair value per share, the profit-or-loss account the difference goes to, and the accountant's note.
type Conversion struct {
	ClassAccountID      uuid.UUID
	Price               string
	PriceBasis          string
	Capacity            string
	Measurement         string
	FairValuePrice      string
	DifferenceAccountID uuid.UUID
	Note                string
}

// checkConversion is what is wrong with a conversion's details, every problem at once.
func checkConversion(s Settlement, ls ledger.Settings) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeSettlement, Field: field, Message: msg, Fix: fix})
	}
	c := s.Conversion
	if c == nil {
		add("conversion", "a conversion needs its class, price, price basis, capacity and measurement basis", "Fill in the conversion")
		return probs
	}
	if a, ok := ls.Accounts[c.ClassAccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
		add("conversion.class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	if !slices.Contains([]string{PriceBasisRound, PriceBasisValuation, PriceBasisUnanimous}, c.PriceBasis) {
		add("conversion.price_basis", fmt.Sprintf("the price basis %q is not one Fyniq knows", c.PriceBasis),
			"Choose latest_priced_round, valuation or unanimous (a price every shareholder approves)")
	}
	if !slices.Contains([]string{CapacityLender, CapacityEmployee, CapacityShareholder}, c.Capacity) {
		add("conversion.capacity", fmt.Sprintf("the capacity %q is not lender, employee or shareholder", c.Capacity), "Choose the capacity in which they convert")
	}
	if shares, _, err := s.Shares(ls.Base); err != nil {
		add("conversion.price", fmt.Sprintf("the price per share %q is not above zero in %s with at most 6 decimals", c.Price, ls.Base), "Enter the price per share, such as 1.20")
	} else if shares.Count.IsZero() {
		add("amount", fmt.Sprintf("%s %s buys no whole share at %s", s.Amount, s.Amount.Currency(), c.Price), "Convert more, or at a lower price")
	}
	switch c.Measurement {
	case MeasurementCarrying:
	case MeasurementFairValue:
		if p, err := money.ParseUnitPrice(c.FairValuePrice, ls.Base, "conversion.fair_value_price"); err != nil || p.Sign() <= 0 {
			add("conversion.fair_value_price", "the shares' fair value per share is not above zero", "Enter the fair value per share the accountant measured")
		}
		if a, ok := ls.Accounts[c.DifferenceAccountID]; !ok || a.Group || !a.Active || (a.Type != "revenue" && a.Type != "expense") || a.Money {
			add("conversion.difference_account_id", "the difference goes to an active income or expense account", "Choose the profit-or-loss account the accountant picked")
		}
		if strings.TrimSpace(c.Note) == "" {
			add("conversion.note", "a basis other than the carrying amount needs the accountant's note", "Say why the shares are measured at fair value")
		}
	default:
		add("conversion.measurement", fmt.Sprintf("the measurement basis %q is not carrying or fair_value", c.Measurement), "Choose carrying (the default) or fair_value")
	}
	if n := utf8.RuneCountInString(c.Note); n > MaxNote {
		add("conversion.note", fmt.Sprintf("the note has %d characters; the most is %d", n, MaxNote), "Shorten it")
	}
	return probs
}

// Shares are what a conversion allots under RND-SHARE (D11): the whole shares, the amount converted, and the remainder
// that stays on the director's account; with the price per share.
func (s Settlement) Shares(base money.Currency) (rounding.Shares, money.UnitPrice, error) {
	if s.Conversion == nil {
		return rounding.Shares{}, money.UnitPrice{}, &Error{CodeSettlement, "the settlement converts nothing", "Choose the convert route"}
	}
	price, err := money.ParseUnitPrice(s.Conversion.Price, base, "conversion.price")
	if err != nil {
		return rounding.Shares{}, price, err
	}
	shares, err := rounding.Share(s.Amount, price)
	return shares, price, err
}

// ConversionRule is a conversion as the rule reads it, for the share issue id on the day ACRA registered it: the
// converted amount off the director's account, and share capital credited with it, or with the shares' fair value
// (round(shares × fair value per share)) with the difference to the account chosen.
func (s Settlement) ConversionRule(id uuid.UUID, on time.Time, memo string, base money.Currency) (ledger.Conversion, rounding.Shares, error) {
	shares, _, err := s.Shares(base)
	if err != nil {
		return ledger.Conversion{}, shares, err
	}
	c := s.Conversion
	out := ledger.Conversion{ID: id, PostOn: on, Memo: memo, Party: s.ContactID, Account: s.AccountID, Class: c.ClassAccountID,
		Converted: shares.Converted, Equity: shares.Converted}
	if c.Measurement == MeasurementFairValue {
		fair, err := money.ParseUnitPrice(c.FairValuePrice, base, "conversion.fair_value_price")
		if err != nil {
			return ledger.Conversion{}, shares, err
		}
		var q apd.Decimal
		q.Set(&shares.Count)
		n, err := money.ParseQuantity(q.Text('f'), "shares")
		if err != nil {
			return ledger.Conversion{}, shares, err
		}
		if out.Equity, err = rounding.Line(n, fair); err != nil {
			return ledger.Conversion{}, shares, err
		}
		out.Difference = c.DifferenceAccountID
	}
	return out, shares, nil
}

// MissingDocuments are the conversion documents not attached to a settlement, in ConversionDocuments' order.
func MissingDocuments(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) ([]string, error) {
	files, err := u.Evidence().OfDocument(ctx, id, uuid.Nil, 100)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, f := range files {
		if f.State != storage.EvidenceRemoved {
			have[strings.ToLower(strings.TrimSpace(f.EvidenceType))] = true
		}
	}
	var out []string
	for _, d := range ConversionDocuments {
		if !have[d] {
			out = append(out, d)
		}
	}
	return out, nil
}

// LapsesOn is the day the shareholders' approval of a conversion resolved on a day lapses, the date by which the next
// AGM must be held (Companies Act s161(3); design D12): six months after the end of the latest financial year that ended
// on or before the resolution, or, when that date has passed or no year has ended, six months after the end of the year
// the resolution falls in [verify]. False when the financial years are not set.
func LapsesOn(p storage.OrganisationProfile, resolved time.Time) (time.Time, bool) {
	cal, err := period.CalendarOf(p)
	if err != nil {
		return time.Time{}, false
	}
	if end, ok := LastYearEnd(p, resolved); ok {
		if due := sixMonthsAfter(end); !due.Before(resolved) {
			return due, true
		}
	}
	n := cal.Number(resolved)
	if n == 0 {
		if p.FirstYearEnd == nil {
			return time.Time{}, false
		}
		return sixMonthsAfter(*p.FirstYearEnd), true
	}
	return sixMonthsAfter(cal.Year(n).EndsOn), true
}

// sixMonthsAfter is the day six months after a year's end: the month's last day when the year ends on one.
func sixMonthsAfter(end time.Time) time.Time {
	if end.AddDate(0, 0, 1).Day() == 1 {
		return time.Date(end.Year(), end.Month()+7, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	}
	return end.AddDate(0, 6, 0)
}
