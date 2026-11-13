package aiprice

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/ports"
)

// catalogue is a LiteLLM catalogue of entries, with enough filler chat models to be a whole one.
func catalogue(entries map[string]map[string]any) []byte {
	all := map[string]any{"sample_spec": map[string]any{"input_cost_per_token": 0, "litellm_provider": "one of https://docs.litellm.ai/docs/providers"}}
	for i := range MinEntries {
		all[fmt.Sprintf("filler-%03d", i)] = map[string]any{"litellm_provider": "filler", "mode": "chat",
			"input_cost_per_token": json.Number("1e-06"), "output_cost_per_token": json.Number("2e-06")}
	}
	for k, v := range entries {
		all[k] = v
	}
	b, err := json.Marshal(all)
	if err != nil {
		panic(err)
	}
	return b
}

func entry(provider string, rates ...string) map[string]any {
	e := map[string]any{"litellm_provider": provider, "mode": "chat"}
	for i := 0; i+1 < len(rates); i += 2 {
		e[rates[i]] = json.Number(rates[i+1])
	}
	return e
}

func dec(s string) apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return *d
}

func rateOf(t *testing.T, p Price, kind string, above int64) string {
	t.Helper()
	r, ok := p.rate(kind, above)
	if !ok {
		return "none"
	}
	return r.Text('f')
}

// Prices are read from the catalogue's text as exact decimals, never binary floats, and kept per million tokens with
// their unit, currency and every tier (AI-85). A rate that is zero, negative, not a number or finer than Fyniq keeps
// leaves its model unpriced; rates Fyniq never uses are listed, not used; a document that is not a whole catalogue is
// refused.
// req: AI-85
func TestCataloguePricesAreExactDecimals(t *testing.T) {
	doc := catalogue(map[string]map[string]any{
		"claude-test-5-20261001": entry("anthropic", "input_cost_per_token", "3e-06", "output_cost_per_token", "1.5e-05",
			"cache_read_input_token_cost", "3e-07", "cache_creation_input_token_cost", "3.75e-06", "cache_creation_input_token_cost_above_1hr", "6e-06",
			"input_cost_per_token_above_200k_tokens", "6e-06", "output_cost_per_token_above_200k_tokens", "2.25e-05",
			"input_cost_per_token_batches", "1.5e-06", "input_cost_per_image", "0.0048"),
		"cheap-1":      entry("anthropic", "input_cost_per_token", "1.25e-07", "output_cost_per_token", "0.0000005"),
		"free-1":       entry("anthropic", "input_cost_per_token", "0", "output_cost_per_token", "0"),
		"negative-1":   entry("anthropic", "input_cost_per_token", "-1e-06", "output_cost_per_token", "1e-06"),
		"too-fine-1":   entry("anthropic", "input_cost_per_token", "1e-17", "output_cost_per_token", "1e-06"),
		"characters-1": entry("anthropic", "input_cost_per_character", "1e-06", "output_cost_per_character", "2e-06"),
		"embed-1":      {"litellm_provider": "anthropic", "mode": "embedding", "input_cost_per_token": json.Number("1e-07")},
	})
	c, err := ParseLiteLLM(doc)
	if err != nil {
		t.Fatal(err)
	}
	f := c.Find(Naming{Provider: "anthropic"}, "claude-test-5-20261001")
	if f.Problem != "" || f.Match != MatchExact {
		t.Fatalf("exact: %+v", f)
	}
	p := f.Entry.Price
	for _, want := range []struct {
		kind  string
		above int64
		rate  string
	}{{KindInput, 0, "3"}, {KindOutput, 0, "15"}, {KindCacheRead, 0, "0.3"}, {KindCacheWrite, 0, "3.75"}, {KindCacheWriteHour, 0, "6"},
		{KindInput, 200000, "6"}, {KindOutput, 200000, "22.5"}, {KindImage, 0, "0.0048"}} {
		if got := rateOf(t, p, want.kind, want.above); got != want.rate {
			t.Errorf("%s above %d: %s, want %s", want.kind, want.above, got, want.rate)
		}
	}
	if p.Currency != "USD" || UnitOf(KindImage) != PerImage || UnitOf(KindInput) != PerMillionTokens ||
		strings.Join(p.Ignored, ",") != "input_cost_per_token_batches" {
		t.Errorf("currency %s, ignored %v", p.Currency, p.Ignored)
	}
	if cheap := c.Find(Naming{Provider: "anthropic"}, "cheap-1"); cheap.Problem != "" || rateOf(t, cheap.Entry.Price, KindInput, 0) != "0.125" ||
		rateOf(t, cheap.Entry.Price, KindOutput, 0) != "0.5" {
		t.Errorf("cheap: %+v", cheap)
	}
	for model, why := range map[string]string{"free-1": "not above zero", "negative-1": "not above zero", "too-fine-1": "finer than 10",
		"characters-1": "no input rate", "embed-1": "no entry of anthropic"} {
		if f := c.Find(Naming{Provider: "anthropic"}, model); f.Entry != nil || !strings.Contains(f.Problem, why) {
			t.Errorf("%s: %+v, want %q", model, f, why)
		}
	}

	for name, doc := range map[string][]byte{
		"not an object":   []byte(`["claude"]`),
		"a truncated one": []byte(`{"claude-test": {"input_cost_per_token": 3e-06`),
		"too few models":  []byte(`{"claude-test": {"litellm_provider": "anthropic", "mode": "chat", "input_cost_per_token": 3e-06, "output_cost_per_token": 1.5e-05}}`),
		"too large":       make([]byte, MaxSize+1),
	} {
		if _, err := ParseLiteLLM(doc); err == nil {
			t.Errorf("%s: read as a catalogue", name)
		}
	}
}

// A model's price is matched exactly when the catalogue names its provider and the model, with or without the
// provider's own prefix; otherwise only after removing a prefix or a version-date suffix, which an Owner confirms; and
// entries that disagree, or no entry, leave it unpriced, never guessed (AI-82). Another provider's entry never prices it.
// req: AI-82
func TestModelsMatchExactlyOrNormalised(t *testing.T) {
	c, err := ParseLiteLLM(catalogue(map[string]map[string]any{
		"claude-test-5-20261001":           entry("anthropic", "input_cost_per_token", "3e-06", "output_cost_per_token", "1.5e-05"),
		"vertex_ai/claude-test-5@20261001": entry("vertex_ai-anthropic_models", "input_cost_per_token", "3.3e-06", "output_cost_per_token", "1.65e-05"),
		"openrouter/vendor-a/shared-1":     entry("openrouter", "input_cost_per_token", "1e-06", "output_cost_per_token", "2e-06"),
		"openrouter/vendor-b/shared-1":     entry("openrouter", "input_cost_per_token", "5e-06", "output_cost_per_token", "9e-06"),
		"openrouter/vendor-a/solo-1":       entry("openrouter", "input_cost_per_token", "1e-06", "output_cost_per_token", "2e-06"),
		"gpt-test-6":                       entry("openai", "input_cost_per_token", "2e-06", "output_cost_per_token", "8e-06"),
		"azure/gpt-test-7":                 entry("azure", "input_cost_per_token", "2e-06", "output_cost_per_token", "8e-06"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	anthropic, vertex := Naming{Provider: "anthropic"}, Naming{Provider: "vertex_ai-anthropic_models", Prefix: "vertex_ai/"}
	openrouter, openai := Naming{Provider: "openrouter", Prefix: "openrouter/"}, Naming{Provider: "openai"}
	for _, tc := range []struct {
		name   string
		naming Naming
		model  string
		match  string
		key    string
		why    string
	}{
		{"the provider's own key", anthropic, "claude-test-5-20261001", MatchExact, "claude-test-5-20261001", ""},
		{"with the provider's prefix", vertex, "claude-test-5@20261001", MatchExact, "vertex_ai/claude-test-5@20261001", ""},
		{"an OpenRouter model", openrouter, "vendor-a/solo-1", MatchExact, "openrouter/vendor-a/solo-1", ""},
		{"an alias without its date", anthropic, "claude-test-5", MatchNormalised, "claude-test-5-20261001", ""},
		{"Agent Platform's alias", vertex, "claude-test-5", MatchNormalised, "vertex_ai/claude-test-5@20261001", ""},
		{"another vendor's prefix", openrouter, "vendor-c/solo-1", MatchNormalised, "openrouter/vendor-a/solo-1", ""},
		{"entries that disagree", openrouter, "vendor-c/shared-1", "", "", "different prices"},
		{"nothing matches", anthropic, "claude-test-9", "", "", "no entry of anthropic"},
		{"another provider's entry", openai, "gpt-test-7", "", "", "no entry of openai"},
		{"a provider the catalogue does not name", Naming{}, "gpt-test-6", "", "", "does not name"},
	} {
		f := c.Find(tc.naming, tc.model)
		key := ""
		if f.Entry != nil {
			key = f.Entry.Key
		}
		if f.Match != tc.match || key != tc.key || (tc.why == "") != (f.Problem == "") || !strings.Contains(f.Problem, tc.why) {
			t.Errorf("%s: %+v (key %s), want %s %s %q", tc.name, f, key, tc.match, tc.key, tc.why)
		}
	}
}

// A rate that falls by more than half, or a change of currency, waits for an Owner's review; a rise, or a fall of
// half or less, takes effect (AI-85).
// req: AI-85
func TestSuspiciousDropsWaitForTheOwner(t *testing.T) {
	was := Price{Currency: "USD", Rates: []Rate{{Kind: KindInput, Amount: dec("3")}, {Kind: KindOutput, Amount: dec("15")},
		{Kind: KindInput, Above: 200000, Amount: dec("6")}}}
	with := func(kind string, above int64, amount, currency string) Price {
		p := Price{Currency: currency}
		for _, r := range was.Rates {
			if r.Kind == kind && r.Above == above {
				r.Amount = dec(amount)
			}
			p.Rates = append(p.Rates, r)
		}
		return p
	}
	for _, tc := range []struct {
		name string
		now  Price
		why  string
	}{
		{"the same", with(KindInput, 0, "3", "USD"), ""},
		{"a rise", with(KindOutput, 0, "30", "USD"), ""},
		{"a fall of half", with(KindInput, 0, "1.5", "USD"), ""},
		{"a fall of more than half", with(KindInput, 0, "1.4999", "USD"), "input fell from 3 to 1.4999"},
		{"a tier's fall", with(KindInput, 200000, "2", "USD"), "input above 200000 tokens fell"},
		{"another currency", with(KindInput, 0, "3", "EUR"), "currency changed"},
	} {
		if got := Drop(was, tc.now); (tc.why == "") != (got == "") || !strings.Contains(got, tc.why) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.why)
		}
	}
	if !Same(was, with(KindInput, 0, "3.000", "USD")) || Same(was, with(KindInput, 0, "3.0001", "USD")) {
		t.Error("Same compares the amounts, not their text")
	}
}

// A call is priced exactly at its prompt's tier; a kind it used that the price has no rate for at that tier leaves it
// unpriced for that use; reasoning is output unless the price gives it its own rate (AI-85, AI-77).
// req: AI-85
func TestCallsArePricedAtTheirTier(t *testing.T) {
	p := Price{Currency: "USD", Rates: []Rate{{Kind: KindInput, Amount: dec("3")}, {Kind: KindOutput, Amount: dec("15")},
		{Kind: KindCacheRead, Amount: dec("0.3")}, {Kind: KindInput, Above: 200000, Amount: dec("6")},
		{Kind: KindOutput, Above: 200000, Amount: dec("22.5")}, {Kind: KindImage, Amount: dec("0.0048")}}}
	for _, tc := range []struct {
		name    string
		usage   ports.ModelUsage
		images  int64
		cost    string
		missing string
	}{
		// 1,000 × 3 + 10,000 × 0.3 + 500 × 15 per million, and two images at 0.0048.
		{"a short call", ports.ModelUsage{Input: 1000, CacheRead: 10000, Output: 500, Reasoning: 200}, 2, "0.0231", ""},
		// 250,000 × 6 + 1,000 × 22.5 per million.
		{"a long call", ports.ModelUsage{Input: 250000, Output: 1000}, 0, "1.5225", ""},
		{"exactly at the threshold", ports.ModelUsage{Input: 200000, Output: 1}, 0, "0.600015", ""},
		{"a long call that read the cache", ports.ModelUsage{Input: 150000, CacheRead: 60000, Output: 1}, 0, "", "cache_read above 200000 tokens"},
		{"a cache write it has no rate for", ports.ModelUsage{Input: 10, CacheWrite: 10, Output: 1}, 0, "", "cache_write"},
	} {
		got, missing, err := p.Cost(tc.usage, tc.images, 0)
		if err != nil || missing != tc.missing || (tc.cost != "" && got.Text('f') != tc.cost) {
			t.Errorf("%s: %s, missing %q, %v; want %s %q", tc.name, got.Text('f'), missing, err, tc.cost, tc.missing)
		}
	}
	reasoned := Price{Currency: "USD", Rates: []Rate{{Kind: KindInput, Amount: dec("1")}, {Kind: KindOutput, Amount: dec("2")},
		{Kind: KindReasoning, Amount: dec("10")}}}
	if got, _, err := reasoned.Cost(ports.ModelUsage{Input: 1000000, Output: 1000000, Reasoning: 400000}, 0, 0); err != nil || got.Text('f') != "6.2" {
		t.Errorf("reasoning at its own rate: %s %v", got.Text('f'), err)
	}
	if _, _, err := reasoned.Cost(ports.ModelUsage{Output: 1, Reasoning: 2}, 0, 0); err == nil {
		t.Error("more reasoning than output was priced")
	}
}

// A model with no price in force cannot serve a usage-based connection; a subscription or a self-hosted server needs
// none (AI-82). An Owner's review confirms or declines a pending version only.
// req: AI-82
func TestNoPriceNoUsageBasedRoute(t *testing.T) {
	var e *Error
	if err := Serves("usage", false, false, "gpt-test-6"); !errors.As(err, &e) || e.Code != CodeUnpriced {
		t.Errorf("unpriced on a usage-based connection: %v", err)
	}
	for _, ok := range []error{Serves("usage", false, true, "m"), Serves("subscription", false, false, "m"), Serves("usage", true, false, "m")} {
		if ok != nil {
			t.Errorf("refused: %v", ok)
		}
	}
	if err := Decide(Pending, Confirm); err != nil {
		t.Error(err)
	}
	for _, bad := range [][2]string{{Active, Confirm}, {Pending, "approve"}, {Declined, Decline}} {
		if err := Decide(bad[0], bad[1]); !errors.As(err, &e) || e.Code != CodeReview {
			t.Errorf("%v: %v", bad, err)
		}
	}
}

// An Owner's price names a currency Fyniq knows and at least an input and an output rate, each a plain decimal above
// zero within ten places, once per tier; only an Owner's price in force can be withdrawn (AI-82).
// req: AI-82
func TestAnOwnersPriceIsChecked(t *testing.T) {
	p, err := Entered("USD", []EnteredRate{{Kind: KindInput, Amount: "3"}, {Kind: KindOutput, Amount: "15.5"}, {Kind: KindInput, Above: 200000, Amount: "6"}})
	if err != nil || p.Currency != "USD" || len(p.Rates) != 3 || rateOf(t, p, KindOutput, 0) != "15.5" {
		t.Fatalf("a valid price: %v %+v", err, p)
	}
	var e *Error
	for name, tc := range map[string]struct {
		currency string
		rates    []EnteredRate
		field    string
	}{
		"an unknown currency": {"ZZZ", []EnteredRate{{Kind: KindInput, Amount: "3"}, {Kind: KindOutput, Amount: "15"}}, "currency"},
		"no output rate":      {"USD", []EnteredRate{{Kind: KindInput, Amount: "3"}}, "rates"},
		"a zero rate":         {"USD", []EnteredRate{{Kind: KindInput, Amount: "0"}, {Kind: KindOutput, Amount: "15"}}, "rates"},
		"an exponent":         {"USD", []EnteredRate{{Kind: KindInput, Amount: "3e-6"}, {Kind: KindOutput, Amount: "15"}}, "rates[0].amount"},
		"a negative rate":     {"USD", []EnteredRate{{Kind: KindInput, Amount: "-3"}, {Kind: KindOutput, Amount: "15"}}, "rates[0].amount"},
		"eleven places":       {"USD", []EnteredRate{{Kind: KindInput, Amount: "0.00000000001"}, {Kind: KindOutput, Amount: "15"}}, "rates[0].amount"},
		"a rate twice":        {"USD", []EnteredRate{{Kind: KindInput, Amount: "3"}, {Kind: KindInput, Amount: "4"}, {Kind: KindOutput, Amount: "15"}}, "rates"},
		"an image tier":       {"USD", []EnteredRate{{Kind: KindInput, Amount: "3"}, {Kind: KindOutput, Amount: "15"}, {Kind: KindImage, Above: 10, Amount: "1"}}, "rates"},
		"an unknown kind":     {"USD", []EnteredRate{{Kind: KindInput, Amount: "3"}, {Kind: KindOutput, Amount: "15"}, {Kind: "audio", Amount: "1"}}, "rates"},
	} {
		if _, err := Entered(tc.currency, tc.rates); !errors.As(err, &e) || e.Code != CodeEntered || e.Field != tc.field {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := Withdrawable(SourceOwner, Active); err != nil {
		t.Error(err)
	}
	for _, bad := range [][2]string{{SourceLiteLLM, Active}, {SourceOwner, Superseded}} {
		if err := Withdrawable(bad[0], bad[1]); !errors.As(err, &e) || e.Code != CodeWithdraw {
			t.Errorf("%v: %v", bad, err)
		}
	}
}

// The catalogue is used only while its repository grants the MIT licence and the file lies outside the folder another
// licence covers (AI-85).
// req: AI-85
func TestTheCataloguesLicenceIsChecked(t *testing.T) {
	mit := []byte("Portions of this software are licensed as follows:\n* All content under enterprise/ ...\n\nMIT License\n\n" +
		"Copyright (c) 2023 Berri AI\n\nPermission is hereby granted, free of charge, to any person obtaining a copy")
	if got, err := CheckLicence(mit, "model_prices_and_context_window.json"); err != nil || got != "MIT, outside enterprise/" {
		t.Errorf("MIT: %q %v", got, err)
	}
	if _, err := CheckLicence([]byte("All rights reserved."), "model_prices_and_context_window.json"); err == nil {
		t.Error("another licence passed")
	}
	if _, err := CheckLicence(mit, "enterprise/model_prices.json"); err == nil {
		t.Error("a file under enterprise/ passed")
	}
}
