// Package aiprice prices model calls from a named source (PRD-001 AI-82, AI-85; p1-ai-providers task 4.2, design D4).
// A price comes from the provider's own list, Google's billing catalogue, LiteLLM's public model price catalogue, or
// an Owner with a second approver, in that order; it keeps its source, how it matched, its currency, and every rate
// the source gives by kind and by the prompt length above which it applies. Rates are exact decimals read from the
// source's text, never binary floats (golden rule 1), kept per million tokens, per image or per request. A model
// nothing matches stays unpriced, never priced by a guess; a match found only after normalising its name, and a rate
// that falls by more than half, wait for an Owner's review; an Owner's price is never overwritten by a read.
package aiprice

import (
	"fmt"
	"slices"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The codes of a refused price step (ENG-23).
const (
	CodeUnpriced = "AI-PRICE-01" // a usage-based connection's model with no price in force cannot serve a route (AI-82)
	CodeReview   = "AI-PRICE-02" // the version is not waiting for a review, or the decision is not confirm or decline
)

// Error is a refused price step, in Fyniq's words (ENG-23).
type Error struct {
	Code, Field, Message, Fix string
}

func (e *Error) Error() string { return e.Code + ": " + e.Field + " " + e.Message }

func refuse(code, field, message, fix string) *Error {
	return &Error{Code: code, Field: field, Message: message, Fix: fix}
}

// The sources of a price, in the order a model's price is taken from them (AI-82).
const (
	SourceProviderList  = "provider_list"  // the provider's own list: OpenRouter, xAI, a LiteLLM gateway's /model/info
	SourceGoogleBilling = "google_billing" // Google's Cloud Billing Catalog, for Agent Platform
	SourceLiteLLM       = "litellm"        // LiteLLM's public model price catalogue
	SourceOwner         = "owner"          // entered by an Owner with a second approver
)

// How a price matched its model (AI-82).
const (
	MatchExact      = "exact"      // the source names this provider and this model
	MatchNormalised = "normalised" // only after removing a provider prefix or a version-date suffix: an Owner confirms it
	MatchEntered    = "entered"    // an Owner's price
)

// The states of a price version.
const (
	Active     = "active"     // in force
	Pending    = "pending"    // waiting for an Owner's review; the version before it, if any, stays in force
	Superseded = "superseded" // a later version took its place
	Declined   = "declined"   // an Owner declined it on review
)

// Why a version waits for an Owner's review (AI-82, AI-85).
const (
	ReviewNormalised = "normalised" // matched only after normalising
	ReviewDrop       = "drop"       // a rate fell by more than half, or the currency changed
)

// The decisions of an Owner's review.
const (
	Confirm = "confirm"
	Decline = "decline"
)

// The kinds of rate (AI-85).
const (
	KindInput          = "input"
	KindOutput         = "output"
	KindCacheRead      = "cache_read"
	KindCacheWrite     = "cache_write"    // a cache write that lives the provider's default time
	KindCacheWriteHour = "cache_write_1h" // a cache write that lives an hour
	KindReasoning      = "reasoning"      // when the source prices reasoning apart from output; otherwise it is output
	KindImage          = "input_image"
	KindRequest        = "request"
)

// Kinds are the kinds of rate a price may hold.
var Kinds = []string{KindInput, KindOutput, KindCacheRead, KindCacheWrite, KindCacheWriteHour, KindReasoning, KindImage, KindRequest}

// The units a rate is kept in (AI-85).
const (
	PerMillionTokens = "per_million_tokens"
	PerImage         = "per_image"
	PerRequest       = "per_request"
)

// UnitOf is the unit a kind of rate is kept in.
func UnitOf(kind string) string {
	switch kind {
	case KindImage:
		return PerImage
	case KindRequest:
		return PerRequest
	}
	return PerMillionTokens
}

// Scale is how many decimal places a rate keeps: NUMERIC(24,10). A finer rate is not one Fyniq can keep exactly.
const Scale = 10

// DropReview is the fall that holds a new rate for an Owner's review: more than half (AI-85).
const DropReview = 2

// calc is the context of every computation: exact for any rate and token count Fyniq keeps.
var calc = apd.BaseContext.WithPrecision(60)

// Rate is one rate of a price: Above is the prompt length in tokens above which it applies, 0 for the base rate.
type Rate struct {
	Kind   string
	Above  int64
	Amount apd.Decimal
}

// Unit is the unit the rate is kept in.
func (r Rate) Unit() string { return UnitOf(r.Kind) }

func (r Rate) label() string {
	if r.Above == 0 {
		return r.Kind
	}
	return fmt.Sprintf("%s above %d tokens", r.Kind, r.Above)
}

// Price is a model's rates in one currency.
type Price struct {
	Currency string
	Rates    []Rate
	// Ignored are the source's rates Fyniq does not price with (batch, flex, priority, audio, search, per character):
	// shown with the price, never used, as Fyniq makes no such request.
	Ignored []string
}

// rate is the rate of kind at exactly above, if the price has one.
func (p Price) rate(kind string, above int64) (*apd.Decimal, bool) {
	for i := range p.Rates {
		if p.Rates[i].Kind == kind && p.Rates[i].Above == above {
			return &p.Rates[i].Amount, true
		}
	}
	return nil, false
}

// Valid reports why p cannot price a call: it needs a base input and output rate, and every rate above zero, of a
// kind Fyniq knows, in its unit, within Scale (AI-85).
func (p Price) Valid() error {
	if len(p.Currency) != 3 {
		return fmt.Errorf("its currency %q is not a currency code", p.Currency)
	}
	seen := map[string]bool{}
	for _, r := range p.Rates {
		key := r.label()
		switch {
		case !slices.Contains(Kinds, r.Kind):
			return fmt.Errorf("%s is not a kind of rate Fyniq knows", r.Kind)
		case seen[key]:
			return fmt.Errorf("%s is given twice", key)
		case r.Above < 0 || (r.Above > 0 && r.Unit() != PerMillionTokens):
			return fmt.Errorf("%s has a prompt-length tier it cannot have", key)
		case r.Amount.Form != apd.Finite || r.Amount.Sign() <= 0:
			return fmt.Errorf("%s is %s, not above zero", key, r.Amount.Text('f'))
		case !exactAt(&r.Amount, Scale):
			return fmt.Errorf("%s is %s, finer than %d decimal places", key, r.Amount.Text('f'), Scale)
		}
		seen[key] = true
	}
	if !seen[KindInput] {
		return fmt.Errorf("it has no input rate")
	}
	if !seen[KindOutput] {
		return fmt.Errorf("it has no output rate")
	}
	return nil
}

// exactAt reports whether d has no digits beyond places.
func exactAt(d *apd.Decimal, places int32) bool {
	if d.Exponent >= -places {
		return true
	}
	var q apd.Decimal
	if _, err := calc.Quantize(&q, d, -places); err != nil {
		return false
	}
	return q.Cmp(d) == 0
}

// Drop compares a newly read price with the one in force (AI-85): why the new one waits for an Owner's review, "" when
// it may take effect. A rate that falls by more than half, or a change of currency, waits; a rise, a new tier or a
// dropped one takes effect at once, as none lowers a call's charge silently.
func Drop(was, now Price) string {
	if was.Currency != now.Currency {
		return fmt.Sprintf("the currency changed from %s to %s", was.Currency, now.Currency)
	}
	for _, r := range was.Rates {
		n, ok := now.rate(r.Kind, r.Above)
		if !ok {
			continue
		}
		var times apd.Decimal
		if _, err := calc.Mul(&times, n, apd.New(DropReview, 0)); err != nil || times.Cmp(&r.Amount) < 0 {
			return fmt.Sprintf("%s fell from %s to %s", r.label(), r.Amount.Text('f'), n.Text('f'))
		}
	}
	return ""
}

// Same reports whether two prices hold the same rates in the same currency, whatever their order.
func Same(a, b Price) bool {
	if a.Currency != b.Currency || len(a.Rates) != len(b.Rates) {
		return false
	}
	for _, r := range a.Rates {
		n, ok := b.rate(r.Kind, r.Above)
		if !ok || n.Cmp(&r.Amount) != 0 {
			return false
		}
	}
	return true
}

// tier is the prompt-length tier a prompt of n tokens is priced at: the highest threshold of the price below n, 0 for
// the base rates.
func (p Price) tier(n int64) int64 {
	var t int64
	for _, r := range p.Rates {
		if r.Above > t && n > r.Above {
			t = r.Above
		}
	}
	return t
}

// million is 10^6, for rates per million tokens.
var million = apd.New(1, 6)

// Cost is what a call's usage costs under p, exactly, in p's currency (AI-77, AI-85). The prompt is its fresh input,
// cache reads and cache writes; a prompt above a threshold the price gives is priced at that tier's rates, and a kind
// the call used that the price has no rate for at that tier leaves the call unpriced for that use: missing names it.
// Reasoning is output unless the price gives reasoning its own rate. Images and requests are counted by the caller.
func (p Price) Cost(u ports.ModelUsage, images, requests int64) (cost apd.Decimal, missing string, err error) {
	at := p.tier(u.Input + u.CacheRead + u.CacheWrite)
	output, reasoning := u.Output, int64(0)
	if _, ok := p.rate(KindReasoning, at); ok {
		if u.Reasoning > u.Output {
			return apd.Decimal{}, "", fmt.Errorf("the usage has %d reasoning tokens of %d output tokens", u.Reasoning, u.Output)
		}
		output, reasoning = u.Output-u.Reasoning, u.Reasoning
	}
	parts := []struct {
		kind string
		n    int64
	}{{KindInput, u.Input}, {KindCacheRead, u.CacheRead}, {KindCacheWrite, u.CacheWrite}, {KindOutput, output},
		{KindReasoning, reasoning}, {KindImage, images}, {KindRequest, requests}}
	for _, part := range parts {
		if part.n == 0 {
			continue
		}
		above := at
		if UnitOf(part.kind) != PerMillionTokens {
			above = 0
		}
		r, ok := p.rate(part.kind, above)
		if !ok {
			return apd.Decimal{}, Rate{Kind: part.kind, Above: above}.label(), nil
		}
		var amount apd.Decimal
		if _, err := calc.Mul(&amount, apd.New(part.n, 0), r); err != nil {
			return apd.Decimal{}, "", err
		}
		if UnitOf(part.kind) == PerMillionTokens {
			if _, err := calc.Quo(&amount, &amount, million); err != nil {
				return apd.Decimal{}, "", err
			}
		}
		if _, err := calc.Add(&cost, &cost, &amount); err != nil {
			return apd.Decimal{}, "", err
		}
	}
	_, _, err = calc.Reduce(&cost, &cost)
	return cost, "", err
}

// Trim is a stored amount without the zeros its column pads it with, such as 3 for 3.0000000000.
func Trim(s string) string {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		return s
	}
	if _, _, err := calc.Reduce(d, d); err != nil {
		return s
	}
	return d.Text('f')
}

// Serves refuses a model with no price in force on a usage-based connection: its cost could not be budgeted (AI-82,
// AI-77). A subscription and a self-hosted server need no per-token price.
func Serves(billing string, selfHosted, priced bool, model string) error {
	if billing != "usage" || selfHosted || priced {
		return nil
	}
	return refuse(CodeUnpriced, "model", model+" has no price in force, so its cost on this usage-based connection could not be budgeted",
		"Wait for the daily price read, review its pending price, or have an Owner enter its price with a second approver")
}

// Decide checks an Owner's review of a version in status: confirm puts it in force, decline keeps the one before.
func Decide(status, decision string) error {
	if decision != Confirm && decision != Decline {
		return refuse(CodeReview, "decision", "is not confirm or decline", "Confirm the price to put it in force, or decline it to keep the one before")
	}
	if status != Pending {
		return refuse(CodeReview, "price", "is "+status+", not waiting for a review", "Review a pending price: a normalised match or a held drop")
	}
	return nil
}
