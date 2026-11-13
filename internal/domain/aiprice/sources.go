package aiprice

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports"
)

// FromList is the price a provider's own list gives, as Fyniq keeps it (AI-82 (a)): per-token rates per million tokens,
// exact from the provider's text. A price the list says changes under conditions it does not state in full is not used,
// and a rate of zero leaves the model unpriced, as from any source.
func FromList(lp ports.ListedPrice) (Price, error) {
	if lp.Conditional {
		return Price{}, fmt.Errorf("the provider's list gives a price that changes under conditions (a discount or overrides)")
	}
	p := Price{Currency: lp.Currency}
	kinds := make([]string, 0, len(lp.PerToken))
	for k := range lp.PerToken {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		d, err := decimalOf(json.Number(lp.PerToken[kind]), true)
		if err != nil {
			return Price{}, fmt.Errorf("the provider's list gives %s that %v", kind, err)
		}
		p.Rates = append(p.Rates, Rate{Kind: kind, Amount: d})
	}
	for _, x := range []struct{ kind, v string }{{KindImage, lp.PerImage}, {KindRequest, lp.PerRequest}} {
		if x.v == "" {
			continue
		}
		d, err := decimalOf(json.Number(x.v), false)
		if err != nil {
			return Price{}, fmt.Errorf("the provider's list gives %s that %v", x.kind, err)
		}
		p.Rates = append(p.Rates, Rate{Kind: x.kind, Amount: d})
	}
	if err := p.Valid(); err != nil {
		return Price{}, fmt.Errorf("the provider's list gives a price that cannot be used: %v", err)
	}
	return p, nil
}

// billingUnit is the usage unit of a SKU priced per token [live-verify: the unit Agent Platform's token SKUs give].
const billingUnit = "count"

// billingTiered are words of a SKU's description that mean a tier Fyniq cannot place, such as a higher rate above a
// context length: a model with such a SKU is not priced from the billing catalogue, so a long call is never priced
// low [live-verify].
var billingTiered = []string{"long context", "long-context", ">", "above", "over 200k", "over 128k"}

// billingOther are words of a SKU's description that mean a use Fyniq does not price from the billing catalogue.
var billingOther = []string{"batch", "tuning", "tuned", "audio", "video", "image", "cache", "cached", "thinking", "grounding", "priority",
	"flex", "provisioned", "character"}

// FromBilling finds a model's price among Agent Platform's SKUs in Google's Cloud Billing Catalog (AI-82 (b))
// [live-verify]: the SKUs offered in region, or globally, whose description names the model by its listed name and says
// input or output tokens, priced per token at one price. A SKU names a model in words, not by its ID, so the match is
// always normalised and an Owner confirms it. A model with a tiered SKU Fyniq cannot place, several SKUs that disagree,
// or no input and output SKU, is not priced from the catalogue, saying why.
func FromBilling(skus []ports.BillingSKU, region, name string) Found {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Found{Problem: "the model list gives no name to find it by in Google's billing catalogue"}
	}
	byKind := map[string][]ports.BillingSKU{}
	for _, s := range skus {
		d := strings.ToLower(s.Description)
		if !strings.Contains(d, name) || (!slices.Contains(s.Regions, region) && !slices.Contains(s.Regions, "global")) || !strings.Contains(d, "token") {
			continue
		}
		if slices.ContainsFunc(billingTiered, func(w string) bool { return strings.Contains(d, w) }) {
			return Found{Problem: "Google's billing catalogue prices " + name + " at a tier Fyniq cannot place (" + s.Description + ")"}
		}
		if slices.ContainsFunc(billingOther, func(w string) bool { return strings.Contains(d, w) }) {
			continue
		}
		switch {
		case strings.Contains(d, "input"):
			byKind[KindInput] = append(byKind[KindInput], s)
		case strings.Contains(d, "output"):
			byKind[KindOutput] = append(byKind[KindOutput], s)
		}
	}
	if len(byKind[KindInput]) == 0 || len(byKind[KindOutput]) == 0 {
		return Found{Problem: "Google's billing catalogue gives no input and output token SKUs for " + name + " in " + region}
	}
	p := Price{Currency: "USD"}
	var ids []string
	for _, kind := range []string{KindInput, KindOutput} {
		var rate *Rate
		for _, s := range byKind[kind] {
			r, err := billingRate(kind, s)
			if err != nil {
				return Found{Problem: "Google's billing catalogue's SKU " + s.ID + " " + err.Error()}
			}
			if rate != nil && rate.Amount.Cmp(&r.Amount) != 0 {
				return Found{Problem: "Google's billing catalogue gives " + name + " several " + kind + " prices"}
			}
			if rate == nil {
				rate = &r
				ids = append(ids, s.ID)
			}
		}
		p.Rates = append(p.Rates, *rate)
	}
	if err := p.Valid(); err != nil {
		return Found{Problem: "Google's billing catalogue's price cannot be used: " + err.Error()}
	}
	return Found{Entry: &Entry{Key: "sku:" + strings.Join(ids, "+"), Provider: SourceGoogleBilling, Price: p}, Match: MatchNormalised}
}

// billingRate is a SKU's one price per token, per million tokens.
func billingRate(kind string, s ports.BillingSKU) (Rate, error) {
	if s.UsageUnit != billingUnit {
		return Rate{}, fmt.Errorf("is priced per %q, a unit Fyniq does not read", s.UsageUnit)
	}
	if len(s.Tiers) == 0 {
		return Rate{}, fmt.Errorf("gives no price")
	}
	for _, t := range s.Tiers {
		if t.Currency != "USD" || t.Price != s.Tiers[len(s.Tiers)-1].Price {
			return Rate{}, fmt.Errorf("gives volume tiers or another currency, which Fyniq does not price with")
		}
	}
	d, err := decimalOf(json.Number(s.Tiers[0].Price), true)
	if err != nil {
		return Rate{}, fmt.Errorf("gives a price that %v", err)
	}
	return Rate{Kind: kind, Amount: d}, nil
}
