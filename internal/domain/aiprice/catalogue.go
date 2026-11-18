package aiprice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

// MinEntries is the fewest chat models a valid catalogue prices: LiteLLM's prices thousands, so fewer means a
// truncated or replaced document, refused whole (AI-85).
const MinEntries = 100

// MaxSize is the largest catalogue Fyniq reads: LiteLLM's is about 4 MB.
const MaxSize = 32 << 20

// Entry is one model of a catalogue: its key, the provider it names, and the price it gives, or why it gives none.
type Entry struct {
	Key      string
	Provider string
	Mode     string
	Price    Price
	Problem  string // why the entry cannot price a call: a rate that is not a number, not above zero or too fine
}

// Catalogue is a validated price catalogue: its entries by provider, for matching.
type Catalogue struct {
	Source  string
	Entries int                            // the chat models it prices
	byKey   map[string]map[string][]*Entry // provider, then key
	byBase  map[string]map[string][]*Entry // provider, then the key normalised
}

// costKey reads LiteLLM's names of the per-token rates Fyniq prices with (AI-85): the kind, a one-hour cache write,
// and the prompt length in thousands of tokens above which it applies.
var costKey = regexp.MustCompile(`^(input_cost_per_token|output_cost_per_token|cache_read_input_token_cost|cache_creation_input_token_cost|output_cost_per_reasoning_token)(_above_1hr)?(?:_above_([0-9]{1,6})k_tokens)?$`)

var kindOf = map[string]string{
	"input_cost_per_token":            KindInput,
	"output_cost_per_token":           KindOutput,
	"cache_read_input_token_cost":     KindCacheRead,
	"cache_creation_input_token_cost": KindCacheWrite,
	"output_cost_per_reasoning_token": KindReasoning,
}

// chatModes are the modes of the models Fyniq calls; an entry of another mode (embeddings, images, speech) is not
// matched.
var chatModes = []string{"", "chat", "responses", "completion"}

// ParseLiteLLM reads LiteLLM's model_prices_and_context_window.json (AI-85): every number from its text, never as a
// binary float, per-token rates kept per million tokens, and each entry's other rates listed as ignored. A document
// that is not a JSON object of entries, or prices fewer than MinEntries chat models, is refused whole.
func ParseLiteLLM(doc []byte) (Catalogue, error) {
	if len(doc) > MaxSize {
		return Catalogue{}, fmt.Errorf("the catalogue is %d bytes, more than the %d Fyniq reads", len(doc), MaxSize)
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return Catalogue{}, fmt.Errorf("the catalogue is not a JSON object of models: %w", err)
	}
	c := Catalogue{Source: SourceLiteLLM, byKey: map[string]map[string][]*Entry{}, byBase: map[string]map[string][]*Entry{}}
	for key, body := range raw {
		if key == "sample_spec" {
			continue
		}
		e, err := liteLLMEntry(key, body)
		if err != nil {
			return Catalogue{}, fmt.Errorf("the catalogue's entry %q: %w", key, err)
		}
		if !slices.Contains(chatModes, e.Mode) || e.Provider == "" {
			continue
		}
		if e.Problem == "" {
			c.Entries++
		}
		c.add(e)
	}
	if c.Entries < MinEntries {
		return Catalogue{}, fmt.Errorf("the catalogue prices %d chat models, fewer than %d: it is not a whole catalogue", c.Entries, MinEntries)
	}
	return c, nil
}

func (c *Catalogue) add(e *Entry) {
	for _, idx := range []struct {
		m   map[string]map[string][]*Entry
		key string
	}{{c.byKey, e.Key}, {c.byBase, Normalise(e.Key)}} {
		if idx.m[e.Provider] == nil {
			idx.m[e.Provider] = map[string][]*Entry{}
		}
		idx.m[e.Provider][idx.key] = append(idx.m[e.Provider][idx.key], e)
	}
}

// liteLLMEntry reads one entry. An entry that is not an object is an error that refuses the catalogue; a rate it
// cannot keep is the entry's problem, which leaves that model unpriced.
func liteLLMEntry(key string, body json.RawMessage) (*Entry, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var fields map[string]any
	if err := dec.Decode(&fields); err != nil {
		return nil, fmt.Errorf("it is not a JSON object: %w", err)
	}
	e := &Entry{Key: key, Price: Price{Currency: "USD"}} // LiteLLM's prices are in US dollars
	e.Provider, _ = fields["litellm_provider"].(string)
	e.Mode, _ = fields["mode"].(string)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r, ok, err := liteLLMRate(name, fields[name])
		switch {
		case err != nil:
			if e.Problem == "" {
				e.Problem = fmt.Sprintf("%s %v", name, err)
			}
		case ok:
			e.Price.Rates = append(e.Price.Rates, r)
		case strings.Contains(name, "cost"):
			e.Price.Ignored = append(e.Price.Ignored, name)
		}
	}
	if e.Problem == "" {
		if err := e.Price.Valid(); err != nil {
			e.Problem = err.Error()
		}
	}
	return e, nil
}

// liteLLMRate reads a field that is a rate Fyniq prices with: ok false for any other field.
func liteLLMRate(name string, value any) (Rate, bool, error) {
	m := costKey.FindStringSubmatch(name)
	if m == nil {
		if name == "input_cost_per_image" {
			d, err := decimalOf(value, false)
			return Rate{Kind: KindImage, Amount: d}, err == nil, err
		}
		return Rate{}, false, nil
	}
	kind := kindOf[m[1]]
	if m[2] != "" {
		if kind != KindCacheWrite {
			return Rate{}, false, nil
		}
		kind = KindCacheWriteHour
	}
	r := Rate{Kind: kind}
	if m[3] != "" {
		k, _ := strconv.ParseInt(m[3], 10, 64)
		r.Above = k * 1000
	}
	d, err := decimalOf(value, true)
	r.Amount = d
	return r, err == nil, err
}

// decimalOf is a JSON number as an exact decimal from its text, per million when perToken.
func decimalOf(value any, perToken bool) (apd.Decimal, error) {
	n, ok := value.(json.Number)
	if !ok {
		return apd.Decimal{}, fmt.Errorf("is not a number")
	}
	d, _, err := apd.NewFromString(string(n))
	if err != nil || d.Form != apd.Finite {
		return apd.Decimal{}, fmt.Errorf("is %q, not a decimal number", string(n))
	}
	if perToken {
		if _, err := calc.Mul(d, d, million); err != nil {
			return apd.Decimal{}, err
		}
	}
	if _, _, err := calc.Reduce(d, d); err != nil {
		return apd.Decimal{}, err
	}
	if d.Sign() <= 0 {
		return apd.Decimal{}, fmt.Errorf("is %s, not above zero", d.Text('f'))
	}
	if !exactAt(d, Scale) {
		return apd.Decimal{}, fmt.Errorf("is %s, finer than %d decimal places", d.Text('f'), Scale)
	}
	return *d, nil
}

// Naming is how a source names a connection's provider and its models: the provider's name in the catalogue, and
// the prefix its keys carry, such as "vertex_ai/" (from the connector's manifest).
type Naming struct {
	Provider string
	Prefix   string
}

// dateSuffix is a version date at the end of a model's name: -20250805, @20250805 or -2025-08-05.
var dateSuffix = regexp.MustCompile(`(?:[-@][0-9]{8}|-[0-9]{4}-[0-9]{2}-[0-9]{2})$`)

// Normalise is a model's name without any provider prefix or version-date suffix (AI-82).
func Normalise(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		key = key[i+1:]
	}
	return dateSuffix.ReplaceAllString(key, "")
}

// Found is what a catalogue gives a model: its entry and how it matched, or why it gives none.
type Found struct {
	Entry   *Entry
	Match   string
	Problem string
}

// Find matches model under naming (AI-82): exact when an entry of the provider names the model, with or without the
// provider's own prefix; otherwise normalised when the provider's entries give one price after removing prefixes and
// version-date suffixes. Entries that give different prices, an entry that cannot price, or no entry leave the model
// unpriced, with why: never a guess.
func (c Catalogue) Find(n Naming, model string) Found {
	if n.Provider == "" {
		return Found{Problem: "the catalogue does not name this connection's provider"}
	}
	exact := append(slices.Clone(c.byKey[n.Provider][model]), c.byKey[n.Provider][n.Prefix+model]...)
	if n.Prefix == "" {
		exact = c.byKey[n.Provider][model]
	}
	if f, ok := one(exact, MatchExact); ok {
		return f
	}
	return oneOr(c.byBase[n.Provider][Normalise(model)], MatchNormalised, fmt.Sprintf("no entry of %s names %s", n.Provider, model))
}

func one(es []*Entry, match string) (Found, bool) {
	if len(es) == 0 {
		return Found{}, false
	}
	return oneOr(es, match, ""), true
}

func oneOr(es []*Entry, match, none string) Found {
	if len(es) == 0 {
		return Found{Problem: none}
	}
	keys := make([]string, 0, len(es))
	for _, e := range es {
		keys = append(keys, e.Key)
	}
	slices.Sort(keys)
	first := es[0]
	for _, e := range es {
		if e.Problem != "" {
			return Found{Problem: fmt.Sprintf("the catalogue's entry %s cannot price it: %s", e.Key, e.Problem)}
		}
		if !Same(e.Price, first.Price) {
			return Found{Problem: fmt.Sprintf("the catalogue's entries %s give it different prices", strings.Join(keys, ", "))}
		}
	}
	for _, e := range es {
		if e.Key < first.Key {
			first = e
		}
	}
	return Found{Entry: first, Match: match}
}

// The licence LiteLLM's repository grants: MIT, outside its enterprise/ folder (PRD-001 AI-82, checked 14 Oct 2026).
const (
	licenceName  = "MIT License"
	licenceGrant = "Permission is hereby granted, free of charge"
	licenceOuter = "enterprise/"
)

// CheckLicence checks the repository's licence before its catalogue is used (AI-85): the MIT grant, and the
// catalogue's path outside the folder under another licence. It returns what was found, for the record.
func CheckLicence(text []byte, path string) (string, error) {
	if !bytes.Contains(text, []byte(licenceName)) || !bytes.Contains(text, []byte(licenceGrant)) {
		return "", fmt.Errorf("the repository's licence no longer grants the MIT licence")
	}
	if strings.HasPrefix(strings.TrimPrefix(path, "/"), licenceOuter) {
		return "", fmt.Errorf("%s is under %s, which another licence covers", path, licenceOuter)
	}
	return "MIT, outside " + licenceOuter, nil
}
