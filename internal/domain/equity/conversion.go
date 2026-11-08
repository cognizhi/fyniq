package equity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Round is the priced round a SAFE or note converts at (FR-EQ-18, design D6): its price per share in the base currency,
// and the shares its terms count before it (C₀): the shares issued and held before the round. The option pool joins C₀
// with FR-EQ-19, as a post-money SAFE's terms count it [verify the terms with question 33's lawyer].
type Round struct {
	Price  money.UnitPrice
	Before apd.Decimal
}

// Converting is a recorded instrument converting at a round.
type Converting struct {
	ID         uuid.UUID
	Instrument Instrument
}

// Conversion is what a SAFE or note converts into at a round. CapPrice and DiscountPrice are the two prices its terms
// may give, before its precision and shown to 10 decimals ("" when it has none); Price is the lower of those that apply, or the round's
// price when neither does, rounded down to the instrument's decimals [accountant to confirm the rounding]. Shares is
// RND-SHARE of its amount at Price: Converted is round(shares × price), and Left what stays on 2700 until it is settled
// under its terms. Covered says an enabled template covers every term, so it converts by itself; otherwise Uncovered
// names what the accountant completes in the draft (⚠).
type Conversion struct {
	ID            uuid.UUID
	CapPrice      string
	DiscountPrice string
	Price         money.UnitPrice
	Shares        apd.Decimal
	Converted     money.Money
	Left          money.Money
	Covered       bool
	Uncovered     []string
}

var exact = apd.BaseContext.WithPrecision(60)

// priceExponents are the exponents a conversion price is cut to, by its decimals, 0 to MaxPriceDecimals.
var priceExponents = []int32{0, -1, -2, -3, -4, -5, -6}

func text(d *apd.Decimal) string {
	var r apd.Decimal
	r.Reduce(d)
	return r.Text('f')
}

// shown is a price as a conversion shows it before its precision: cut to 10 decimals, with no trailing zeros.
func shown(d *apd.Decimal) string {
	c := exact.WithPrecision(60)
	c.Rounding = apd.RoundDown
	var r apd.Decimal
	if _, err := c.Quantize(&r, d, -10); err != nil {
		return text(d)
	}
	return text(&r)
}

// Convert is each instrument's conversion at a priced round (FR-EQ-18, design D6, Appendix G.3):
//   - a post-money SAFE's cap price is its cap × (1 − Σ amount ÷ cap over every post-money SAFE converting) ÷ C₀: its
//     cap divided by the company's capitalisation once the SAFEs have converted, which counts their own shares. With one
//     SAFE, or caps alike, it is (cap − Σ amount) ÷ C₀, as design D6 writes it;
//   - a pre-money SAFE's cap price is its cap ÷ C₀;
//   - the discounted round price is the round's price × (100 − discount) ÷ 100, the round's price itself with no discount;
//   - the conversion price is the lower of the cap price and the discounted round price (FR-EQ-18).
//
// enabled says whether the organisation enabled a template.
func Convert(r Round, safes []Converting, enabled func(key string) bool) ([]Conversion, error) {
	if r.Before.Sign() <= 0 {
		return nil, &Error{CodeInstrument, "no share is held before the round, so no cap price can be worked out",
			"Record the shares issued before the round first"}
	}
	share, err := PostMoneyPart(safes)
	if err != nil {
		return nil, err
	}
	if share.Cmp(apd.New(1, 0)) >= 0 {
		return nil, &Error{CodeInstrument, "the post-money SAFEs convert into all of the company or more", "Check their amounts and caps"}
	}
	out := make([]Conversion, 0, len(safes))
	for _, s := range safes {
		c, err := convertOne(r, s, &share, enabled)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// PostMoneyPart is f, the part of the company the post-money SAFEs among safes convert into: Σ amount ÷ cap, exact to 60
// digits (design D6).
func PostMoneyPart(safes []Converting) (apd.Decimal, error) {
	var share apd.Decimal
	for _, s := range safes {
		if s.Instrument.Form != FormPostMoneySAFE {
			continue
		}
		cap, err := capOf(s.Instrument)
		if err != nil {
			return share, err
		}
		var part apd.Decimal
		if _, err := exact.Quo(&part, s.Instrument.Amount.Decimal(), cap); err != nil {
			return share, err
		}
		if _, err := exact.Add(&share, &share, &part); err != nil {
			return share, err
		}
	}
	return share, nil
}

func capOf(i Instrument) (*apd.Decimal, error) {
	cap, _, err := apd.NewFromString(i.ValuationCap)
	if err != nil || cap.Sign() <= 0 {
		return nil, &Error{CodeInstrument, fmt.Sprintf("the valuation cap %q is not an amount above zero", i.ValuationCap), "Correct the instrument's cap"}
	}
	return cap, nil
}

func convertOne(r Round, s Converting, share *apd.Decimal, enabled func(string) bool) (Conversion, error) {
	i := s.Instrument
	out := Conversion{ID: s.ID}
	var prices []*apd.Decimal
	if i.ValuationCap != "" && (i.Form == FormPostMoneySAFE || i.Form == FormPreMoneySAFE) {
		cap, err := capOf(i)
		if err != nil {
			return out, err
		}
		price := new(apd.Decimal)
		if i.Form == FormPostMoneySAFE {
			var rest apd.Decimal
			if _, err := exact.Sub(&rest, apd.New(1, 0), share); err != nil {
				return out, err
			}
			if _, err := exact.Mul(price, cap, &rest); err != nil {
				return out, err
			}
			if _, err := exact.Quo(price, price, &r.Before); err != nil {
				return out, err
			}
		} else if _, err := exact.Quo(price, cap, &r.Before); err != nil {
			return out, err
		}
		out.CapPrice = shown(price)
		prices = append(prices, price)
	}
	{ // the round's price, less the discount when there is one: always a price the instrument may convert at
		d := apd.New(0, 0)
		if i.Discount != "" {
			var err error
			if d, _, err = apd.NewFromString(i.Discount); err != nil {
				return out, err
			}
		}
		var rest apd.Decimal
		if _, err := exact.Sub(&rest, apd.New(100, 0), d); err != nil {
			return out, err
		}
		price := new(apd.Decimal)
		if _, err := exact.Mul(price, r.Price.Decimal(), &rest); err != nil {
			return out, err
		}
		if _, err := exact.Quo(price, price, apd.New(100, 0)); err != nil {
			return out, err
		}
		if i.Discount != "" {
			out.DiscountPrice = shown(price)
		}
		prices = append(prices, price)
	}
	chosen := prices[0]
	for _, p := range prices[1:] {
		if p.Cmp(chosen) < 0 {
			chosen = p
		}
	}
	// A post-money SAFE converting below its cap price no longer holds the part of the company its cap gave it, which the
	// other SAFEs' cap prices counted: the accountant checks the capitalisation [verify with question 33's lawyer].
	roundWins := i.Form == FormPostMoneySAFE && out.CapPrice != "" && chosen != prices[0]
	if i.PriceDecimals < 0 || i.PriceDecimals >= len(priceExponents) {
		return out, &Error{CodeInstrument, fmt.Sprintf("the price's precision of %d decimals is outside 0 to %d", i.PriceDecimals, MaxPriceDecimals),
			"Correct the instrument's decimals"}
	}
	down := exact.WithPrecision(60)
	down.Rounding = apd.RoundDown
	var rounded apd.Decimal
	if _, err := down.Quantize(&rounded, chosen, priceExponents[i.PriceDecimals]); err != nil {
		return out, err
	}
	price, err := money.ParseUnitPrice(text(&rounded), i.Amount.Currency(), "price")
	if err != nil || price.Sign() <= 0 {
		return out, &Error{CodeInstrument, fmt.Sprintf("the conversion price %s is not a price above zero", text(&rounded)),
			"Check the instrument's terms and the round's price"}
	}
	out.Price = price
	allotted, err := rounding.Share(i.Amount, price)
	if err != nil {
		return out, err
	}
	out.Shares, out.Converted, out.Left = allotted.Count, allotted.Converted, allotted.Remainder
	out.Uncovered = uncovered(i, enabled)
	if roundWins {
		out.Uncovered = append(out.Uncovered, "the round's price is below its cap price")
	}
	out.Covered = len(out.Uncovered) == 0
	return out, nil
}

// uncovered are the terms of an instrument no enabled template covers: what the accountant completes in its draft
// (FR-EQ-18: "any other form or term creates a draft that the accountant completes").
func uncovered(i Instrument, enabled func(string) bool) []string {
	var out []string
	t := TemplateOf(i.Template)
	switch {
	case i.Template == "":
		out = append(out, "it follows no template")
	case t == nil || t.Form != i.Form:
		out = append(out, "its template does not fit its form")
	case !enabled(i.Template):
		out = append(out, "its template is not enabled")
	}
	if i.Discount != "" {
		out = append(out, "a discount")
	}
	if i.MFN {
		out = append(out, "a most-favoured-nation clause")
	}
	if i.InterestRate != "" || i.InterestConverts {
		out = append(out, "interest")
	}
	if i.Denominator == "as_terms" {
		out = append(out, "the terms' own count of the shares")
	}
	return out
}

// UncoveredText is what a conversion leaves the accountant, in a sentence.
func UncoveredText(c Conversion) string {
	return "the accountant completes it: " + strings.Join(c.Uncovered, ", ")
}

// checkConversion checks an instrument conversion as drafted (FR-EQ-18), adding each problem: its instrument is a
// recorded SAFE or note, whose holder and class it takes; its round is an approved cash issue; its shares are whole and
// its price above zero; and its amount, shares × price under RND-LINE, is no more than the instrument's amount.
func checkConversion(ctx context.Context, u storage.UnitOfWork, t *Transaction, s ledger.Settings, add func(field, msg, fix string)) error {
	doc, err := u.Documents().Get(ctx, t.InstrumentID)
	switch {
	case errors.Is(err, ports.ErrNotFound) || err == nil && (doc.Type != storage.DocumentInstrument || doc.Status != storage.DocumentPosted):
		add("instrument_id", "is not a recorded SAFE or convertible note", "Choose a recorded instrument")
		return nil
	case err != nil:
		return err
	}
	i, err := ReadInstrument(doc)
	if err != nil {
		return err
	}
	t.HolderID, t.ClassAccountID = i.HolderID, i.ClassAccountID
	if t.ClassID == nil {
		t.ClassID = i.ClassID
	}
	if t.ClassID == nil {
		if t.ClassID, err = DefaultClass(ctx, u, t.ClassAccountID); err != nil {
			return err
		}
	}
	round, err := u.Documents().Get(ctx, t.RoundID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		add("round_id", "names no priced round", "Draft the conversions from an approved issue for cash")
	case err != nil:
		return err
	default:
		r, err := ReadTransaction(round)
		if err != nil || r.Kind != KindIssueCash || round.Status != storage.DocumentPosted {
			add("round_id", "is not an approved issue for cash", "Draft the conversions from an approved issue for cash, the priced round")
		}
	}
	if !wholeShares.MatchString(t.Shares) {
		add("shares", fmt.Sprintf("%q is not a whole number of shares above zero", t.Shares), "Enter the number of shares, such as 52631")
		return nil
	}
	price, err := money.ParseUnitPrice(t.Price, s.Base, "price")
	if err != nil || price.Sign() <= 0 {
		add("price", fmt.Sprintf("%q is not a conversion price above zero in %s", t.Price, s.Base), "Enter the conversion price, such as 2.375")
		return nil
	}
	q, err := money.ParseQuantity(t.Shares, "shares")
	if err != nil {
		return err
	}
	amount, err := rounding.Line(q, price)
	if err != nil {
		return err
	}
	if c, err := amount.Cmp(i.Amount); err != nil {
		return err
	} else if c > 0 {
		add("shares", fmt.Sprintf("%s shares at %s convert %s %s, more than the instrument's %s", t.Shares, price, amount, amount.Currency().Code, i.Amount),
			"Convert no more than the instrument's amount: RND-SHARE keeps the shares within it")
	}
	t.Amount, t.Price = amount.String(), price.String()
	return nil
}

// ConvertingAt are the instruments a round converts: every recorded SAFE and note its next priced round converts, that no
// conversion still standing names (a draft, an approved one, or an effective one not reversed, of a round not reversed).
func ConvertingAt(ctx context.Context, u storage.UnitOfWork) ([]Converting, error) {
	docs, err := Instruments(ctx, u)
	if err != nil {
		return nil, err
	}
	taken, err := converted(ctx, u)
	if err != nil {
		return nil, err
	}
	var out []Converting
	for _, d := range docs {
		if d.Status != storage.DocumentPosted || taken[d.ID] {
			continue
		}
		i, err := ReadInstrument(d)
		if err != nil {
			return nil, err
		}
		if i.Trigger == "priced_round" {
			out = append(out, Converting{ID: d.ID, Instrument: i})
		}
	}
	return out, nil
}

// converted are the instruments a conversion still standing names.
func converted(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]bool, error) {
	standing, err := StandingConversions(ctx, u)
	out := map[uuid.UUID]bool{}
	for id := range standing {
		out[id] = true
	}
	return out, err
}

// StandingConversions are the conversions still standing, by the instrument each converts: a draft, an approved one, or
// an effective one not reversed, of a round not reversed.
func StandingConversions(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]storage.Document, error) {
	docs, err := Transactions(ctx, u)
	if err != nil {
		return nil, err
	}
	reversed, err := reversedIn(ctx, u, docs)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]storage.Document{}
	for _, d := range docs {
		t, err := ReadTransaction(d)
		if err != nil {
			return nil, err
		}
		if t.Kind == KindInstrumentConversion && !reversed[d.ID] && !reversed[t.RoundID] { // a reversed round's conversions stand no more
			out[t.InstrumentID] = d
		}
	}
	return out, nil
}

// Reversed are the equity transactions an effective reversal undid.
func Reversed(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]bool, error) {
	docs, err := Transactions(ctx, u)
	if err != nil {
		return nil, err
	}
	return reversedIn(ctx, u, docs)
}

func reversedIn(ctx context.Context, u storage.UnitOfWork, docs []storage.Document) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, d := range docs {
		t, err := ReadTransaction(d)
		if err != nil {
			return nil, err
		}
		if t.Kind != KindReversal || d.Status != storage.DocumentPosted {
			continue
		}
		moved, err := u.Holdings().OfTransaction(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		if len(moved) > 0 { // effective
			out[t.ReversesID] = true
		}
	}
	return out, nil
}

// SharesBefore are the shares issued and held at the end of the day before a round takes effect, or now while it is
// not effective: the shares its conversions' terms count before it (C₀).
func SharesBefore(ctx context.Context, u storage.UnitOfWork, round uuid.UUID, today time.Time) (apd.Decimal, error) {
	day := today
	moved, err := u.Holdings().OfTransaction(ctx, round)
	if err != nil {
		return apd.Decimal{}, err
	}
	if len(moved) > 0 {
		day = moved[0].EffectiveOn.AddDate(0, 0, -1)
	}
	t, err := TableIn(ctx, u, day)
	return t.Issued, err
}

// Preview is what a priced round converts (FR-EQ-18, design D6): the round, when it is an approved cash issue; its price
// per share; the shares its terms count before it (C₀); f, the part of the company the post-money SAFEs take; each
// instrument converting, and its conversion, in the same order. A preview posts and saves nothing.
type Preview struct {
	Round       uuid.UUID
	Price       money.UnitPrice
	Before      apd.Decimal
	Part        apd.Decimal
	Safes       []Converting
	Conversions []Conversion
}

// RoundPreviewIn is what the approved cash issue round converts: at its price, over the shares before it.
func RoundPreviewIn(ctx context.Context, u storage.UnitOfWork, round uuid.UUID, s ledger.Settings, today time.Time) (Preview, error) {
	doc, err := u.Documents().Get(ctx, round)
	if err != nil {
		return Preview{}, err
	}
	r, err := ReadTransaction(doc)
	if err != nil {
		return Preview{}, err
	}
	if r.Kind != KindIssueCash || doc.Status != storage.DocumentPosted {
		return Preview{}, &Error{CodeTransaction, "the round is not an approved issue for cash", "Draft the conversions from an approved issue for cash, the priced round"}
	}
	if reversed, err := Reversed(ctx, u); err != nil {
		return Preview{}, err
	} else if reversed[round] {
		return Preview{}, &Error{CodeTransaction, "the round was reversed", "Convert at the round that replaces it"}
	}
	p := Preview{Round: round}
	if p.Price, err = money.ParseUnitPrice(r.Price, s.Base, "price"); err != nil {
		return Preview{}, err
	}
	if p.Before, err = SharesBefore(ctx, u, round, today); err != nil {
		return Preview{}, err
	}
	return previewIn(ctx, u, p)
}

// PricePreviewIn is what a round at price would convert, over the shares issued and held today: a round not yet
// recorded, as a sandbox.
func PricePreviewIn(ctx context.Context, u storage.UnitOfWork, price money.UnitPrice, today time.Time) (Preview, error) {
	t, err := TableIn(ctx, u, today)
	if err != nil {
		return Preview{}, err
	}
	return previewIn(ctx, u, Preview{Price: price, Before: t.Issued})
}

func previewIn(ctx context.Context, u storage.UnitOfWork, p Preview) (Preview, error) {
	var err error
	if p.Safes, err = ConvertingAt(ctx, u); err != nil || len(p.Safes) == 0 {
		return p, err
	}
	enabled := map[string]bool{}
	for _, t := range Templates {
		if enabled[t.Key], err = Enabled(ctx, u, t.Key); err != nil {
			return p, err
		}
	}
	if p.Part, err = PostMoneyPart(p.Safes); err != nil {
		return p, err
	}
	p.Conversions, err = Convert(Round{Price: p.Price, Before: p.Before}, p.Safes, func(k string) bool { return enabled[k] })
	return p, err
}

// DraftConversionsIn drafts, in u, the conversion of every instrument the approved cash issue round converts
// (FR-EQ-18: "creates the share issues as drafts for approval"): one instrument_conversion transaction each, its shares
// and price from Convert. One that an enabled template does not cover keeps, as its review, what the accountant completes
// (⚠). An instrument that a conversion still standing names is left out, so asking again drafts nothing twice.
func DraftConversionsIn(ctx context.Context, u storage.UnitOfWork, round uuid.UUID, s ledger.Settings, today time.Time) ([]storage.Document, error) {
	p, err := RoundPreviewIn(ctx, u, round, s, today)
	if err != nil {
		return nil, err
	}
	doc, err := u.Documents().Get(ctx, round)
	if err != nil {
		return nil, err
	}
	r, err := ReadTransaction(doc)
	if err != nil {
		return nil, err
	}
	conversions := p.Conversions
	out := make([]storage.Document, 0, len(conversions))
	for _, c := range conversions {
		t := Transaction{Kind: KindInstrumentConversion, InstrumentID: c.ID, RoundID: round, Shares: c.Shares.Text('f'), Price: c.Price.String(),
			ResolutionRefs: r.ResolutionRefs, Date: today}
		if !c.Covered {
			t.Review = UncoveredText(c)
		}
		d, _, err := DraftTransactionIn(ctx, u, t, s)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}
