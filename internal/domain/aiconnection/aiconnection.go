// Package aiconnection is a connection to an AI model provider (PRD-001 AI-72, AI-73, AI-79; p1-ai-providers design D2):
// one of the release's model-provider connectors, from one of its presets or the connection's own endpoint, with how
// it authenticates, its configuration, the one host its calls go to, its credential by reference, and how it is
// billed. A connection starts off; creating it, changing it, replacing its credential and enabling it are each asked
// for by one person and approved by the Owner (golden rule 6); switching it off is never held back.
package aiconnection

import (
	"fmt"
	"math/big"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The stable codes of a connection's refusals (ENG-23).
const (
	CodeProvider   = "AI-CONN-01" // not a model provider of this release
	CodeAuth       = "AI-CONN-02" // an authentication the provider or its preset does not offer
	CodeConfig     = "AI-CONN-03" // the configuration does not match the provider's
	CodeCredential = "AI-CONN-04" // not an API key the provider issues: a consumer plan's token, a session or a cookie (AI-73)
	CodeEgress     = "AI-CONN-05" // the endpoint is not https on a public host, or a self-hosted one is not registered (SEC-36, SEC-37)
	CodeBilling    = "AI-CONN-06" // the billing mode or quota is not valid (AI-73)
	CodeState      = "AI-CONN-07" // the step cannot be taken in the connection's state
	CodeName       = "AI-CONN-08" // the name is empty, too long or taken
)

// A connection's states: off until an approved request switches it on.
const (
	StateDisabled = "disabled"
	StateEnabled  = "enabled"
)

// The billing modes (AI-73): usage-based, charged per token from the provider's own usage fields; or a subscription
// or commitment with a quota, metered against its window. A user's own plan (AI-73 (c)) is P2 and not offered.
const (
	BillingUsage        = "usage"
	BillingSubscription = "subscription"
)

// Quota is a subscription's quota: Amount tokens or requests per window, for a fee in a currency.
type Quota struct {
	Amount   int64  `json:"amount"`
	Unit     string `json:"unit"`   // tokens or requests
	Window   string `json:"window"` // day or month
	Fee      string `json:"fee"`    // a decimal string, never a binary number (ENG-10)
	Currency string `json:"currency"`
	// Rates are the models the plan draws its quota from at another rate than one unit a unit, as its published terms
	// say (AI-73); a model they do not name draws at 1.
	Rates []QuotaRate `json:"rates,omitempty"`
	// Terms are the plan's published terms the rates come from; required with rates.
	Terms *QuotaTerms `json:"terms,omitempty"`
}

// QuotaRate is how much of the quota one token or request of a model draws: a decimal string, never a binary number.
type QuotaRate struct {
	Model string `json:"model"`
	Rate  string `json:"rate"`
}

// QuotaTerms are a plan's published terms: the official page, its title and the day it was read.
type QuotaTerms struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	CheckedOn string `json:"checked_on"`
}

// Units are what one attempt counts against the quota before its model's rate: its requests, or its tokens, input,
// output and the cache's reads and writes.
func (q Quota) Units(requests, tokens int64) int64 {
	if q.Unit == "requests" {
		return requests
	}
	return tokens
}

// Drawn is the quota a window's use took (AI-73): each model's units at its rate, exactly, rounded down to whole
// units; units maps a model to the units it used.
func (q Quota) Drawn(units map[string]int64) int64 {
	total := new(big.Rat)
	for model, n := range units {
		rate := big.NewRat(1, 1)
		for _, r := range q.Rates {
			if r.Model == model {
				if x, ok := new(big.Rat).SetString(r.Rate); ok {
					rate = x
				}
			}
		}
		total.Add(total, new(big.Rat).Mul(rate, new(big.Rat).SetInt64(n)))
	}
	return new(big.Int).Quo(total.Num(), total.Denom()).Int64()
}

// Period is the window of the quota that now falls in, in loc: its calendar day or month (AI-73), and when it resets.
func (q Quota) Period(now time.Time, loc *time.Location) (start, resets time.Time) {
	local := now.In(loc)
	if q.Window == "day" {
		start = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 0, 1)
	}
	start = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 1, 0)
}

// Definition is what a person asks a connection to be.
type Definition struct {
	Name          string
	Connector     string
	Preset        string
	Auth          string
	Configuration map[string]any
	Billing       string
	Quota         *Quota
}

// Target is where a connection's calls go: its one host, and whether that is a self-hosted server, which it reaches
// only as a registered private endpoint (SEC-37).
type Target struct {
	Host       string
	SelfHosted bool
}

// Error is a refusal: its code, the field it is about and how to fix it.
type Error struct {
	Code, Field, Message, Fix string
}

func (e *Error) Error() string { return e.Code + ": " + e.Field + " " + e.Message }

func refuse(code, field, message, fix string) error {
	return &Error{Code: code, Field: field, Message: message, Fix: fix}
}

// probed are the configuration's keys only a probe writes, never a person (AI-71).
var probed = []string{"proven"}

// Check checks d against the catalogue: a model provider of this release, an authentication it and its preset offer
// (none only for a self-hosted server), a configuration its manifest's schema accepts, an endpoint on https, and a
// billing mode with a valid quota. It returns where the connection's calls go.
func Check(cat *connector.Catalogue, d Definition) (Target, error) {
	if n := strings.TrimSpace(d.Name); n == "" || n != d.Name || len(n) > 80 {
		return Target{}, refuse(CodeName, "name", "is empty, longer than 80 characters or padded with spaces", "Name the connection in up to 80 characters")
	}
	b, ok := cat.Get(d.Connector)
	if !ok || b.Manifest.Model == nil {
		return Target{}, refuse(CodeProvider, "connector", "is not a model provider of this release", "Choose one of the AI providers the catalogue lists")
	}
	m := b.Manifest
	var preset *connector.ModelPreset
	if d.Preset != "" {
		i := slices.IndexFunc(m.Model.Presets, func(p connector.ModelPreset) bool { return p.ID == d.Preset })
		if i < 0 {
			return Target{}, refuse(CodeProvider, "preset", "is not one of "+m.Name+"'s presets", "Choose one of its presets, or none")
		}
		preset = &m.Model.Presets[i]
	}
	for _, k := range probed {
		if _, ok := d.Configuration[k]; ok {
			return Target{}, refuse(CodeConfig, "configuration."+k, "is written by the connection's probe, never typed", "Leave it out")
		}
	}
	if err := m.Validate(d.Configuration); err != nil {
		return Target{}, refuse(CodeConfig, "configuration", err.Error(), "Correct the field the message names, as "+m.Name+"'s configuration describes it")
	}
	target, err := targetOf(m, preset, d.Configuration)
	if err != nil {
		return Target{}, err
	}
	allowed := m.Model.Auth
	if preset != nil {
		allowed = preset.Auth
	}
	switch {
	case d.Auth == "oauth-grant":
		return Target{}, refuse(CodeAuth, "auth", "is a person's own plan, which is not offered yet (AI-73 (c), P2)", "Use an API key or the service's own account")
	case !slices.Contains(allowed, d.Auth):
		return Target{}, refuse(CodeAuth, "auth", fmt.Sprintf("is not one of %s", strings.Join(allowed, ", ")), "Choose how this provider authenticates")
	case d.Auth == "none" && !target.SelfHosted:
		return Target{}, refuse(CodeAuth, "auth", "is none, which only a self-hosted server on a registered private endpoint may be", "Give the provider's API key")
	case d.Connector == "anthropic" && d.Auth != authFor(d.Configuration):
		return Target{}, refuse(CodeAuth, "auth", "does not match the endpoint: the Anthropic API takes an API key, Agent Platform the service's own account",
			"Choose the authentication of the endpoint")
	}
	if err := checkBilling(d.Billing, d.Quota); err != nil {
		return Target{}, err
	}
	return target, nil
}

// authFor is the authentication the Anthropic connector's endpoint takes.
func authFor(config map[string]any) string {
	if e, _ := config["endpoint"].(string); e == "anthropic-api" {
		return "api-key"
	}
	return "workload-identity"
}

// targetOf is the host a connection's calls go to: the endpoint's for a provider on Agent Platform, else its base URL's
// or its preset's, which must be https; a self-hosted server's host may be private, and is then reached only as a
// registered private endpoint.
func targetOf(m connector.Manifest, preset *connector.ModelPreset, config map[string]any) (Target, error) {
	str := func(k string) string { s, _ := config[k].(string); return s }
	location := str("location")
	if location == "" {
		location = "asia-southeast1"
	}
	platformHost := location + "-aiplatform.googleapis.com"
	if location == "global" {
		platformHost = "aiplatform.googleapis.com"
	}
	switch {
	case m.ID == "gemini", m.ID == "anthropic" && str("endpoint") != "anthropic-api":
		return Target{Host: platformHost}, nil
	}
	base := str("base_url")
	if base == "" && preset != nil {
		base = preset.BaseURL
	}
	if base == "" {
		base = defaultBase(m)
	}
	if base == "" {
		return Target{}, refuse(CodeEgress, "configuration.base_url", "is missing", "Give the endpoint's base URL")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Target{}, refuse(CodeEgress, "configuration.base_url", "is not an https address without credentials or a query", "Give the base URL as https://host/path")
	}
	host := strings.ToLower(u.Hostname())
	selfHosted := preset != nil && preset.SelfHosted
	if !selfHosted && private(host) {
		return Target{}, refuse(CodeEgress, "configuration.base_url", "is a private or local address, which only a self-hosted server's preset may use",
			"Use the provider's public endpoint, or a self-hosted preset with a registered private endpoint")
	}
	return Target{Host: host, SelfHosted: selfHosted}, nil
}

// defaultBase is a provider's own endpoint, from its configuration's default.
func defaultBase(m connector.Manifest) string {
	return m.Default("base_url")
}

// private reports whether host is a literal private, loopback or link-local address, or a local name.
func private(host string) bool {
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return !a.IsGlobalUnicast() || a.IsPrivate()
	}
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal")
}

var currency = regexp.MustCompile(`^[A-Z]{3}$`)

func checkBilling(mode string, q *Quota) error {
	switch mode {
	case BillingUsage:
		if q != nil {
			return refuse(CodeBilling, "billing.quota", "is set on usage-based billing", "Leave the quota out, or choose a subscription")
		}
		return nil
	case BillingSubscription:
	default:
		return refuse(CodeBilling, "billing.mode", "is not usage or subscription", "Choose usage-based billing or a subscription with a quota")
	}
	if q == nil {
		return refuse(CodeBilling, "billing.quota", "is missing: a subscription has a quota", "Give the quota, its window and its fee")
	}
	switch {
	case q.Amount <= 0:
		return refuse(CodeBilling, "billing.quota.amount", "is not more than 0", "Give the tokens or requests the plan allows each window")
	case q.Unit != "tokens" && q.Unit != "requests":
		return refuse(CodeBilling, "billing.quota.unit", "is not tokens or requests", "Count the quota in tokens or requests")
	case q.Window != "day" && q.Window != "month":
		return refuse(CodeBilling, "billing.quota.window", "is not day or month", "Give the window the quota resets on")
	case !currency.MatchString(q.Currency):
		return refuse(CodeBilling, "billing.quota.currency", "is not a currency code", "Give the fee's currency, such as SGD or USD")
	}
	cur, err := money.Lookup(q.Currency)
	if err != nil {
		return refuse(CodeBilling, "billing.quota.currency", "is not a currency Fyniq knows", "Give the fee's currency, such as SGD or USD")
	}
	fee, err := money.ParseField(q.Fee, cur, "billing.quota.fee")
	if err != nil || fee.Sign() < 0 {
		return refuse(CodeBilling, "billing.quota.fee", "is not an amount of 0 or more in its currency", "Give the fee as a decimal, such as 30.00")
	}
	return checkRates(q)
}

var (
	quotaRate = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,4})?$`)
	day       = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

// checkRates checks a quota's per-model rates and their terms (AI-73): at most 100 models, each named once, each rate a
// decimal above 0 with at most four places, and the plan's published terms with them.
func checkRates(q *Quota) error {
	if len(q.Rates) > 100 {
		return refuse(CodeBilling, "billing.quota.rates", "names more than 100 models", "Give the rates of at most 100 models")
	}
	seen := map[string]bool{}
	for i, r := range q.Rates {
		field := fmt.Sprintf("billing.quota.rates[%d]", i)
		x, ok := new(big.Rat).SetString(r.Rate)
		switch {
		case strings.TrimSpace(r.Model) == "" || len(r.Model) > 200:
			return refuse(CodeBilling, field+".model", "is empty or longer than 200 characters", "Give the model's ID as the provider lists it")
		case seen[r.Model]:
			return refuse(CodeBilling, field+".model", "is named twice", "Give each model's rate once")
		case !quotaRate.MatchString(r.Rate) || !ok || x.Sign() <= 0:
			return refuse(CodeBilling, field+".rate", "is not a decimal above 0 with at most four places", "Give the rate as the terms do, such as 0.33 or 10")
		}
		seen[r.Model] = true
	}
	if q.Terms == nil {
		if len(q.Rates) > 0 {
			return refuse(CodeBilling, "billing.quota.terms", "is missing: rates come from the plan's published terms", "Give the terms' page, its title and the day you read it")
		}
		return nil
	}
	t := q.Terms
	u, err := url.Parse(t.URL)
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "":
		return refuse(CodeBilling, "billing.quota.terms.url", "is not an https address", "Give the address of the plan's published terms")
	case strings.TrimSpace(t.Title) == "" || len(t.Title) > 300:
		return refuse(CodeBilling, "billing.quota.terms.title", "is empty or longer than 300 characters", "Give the page's title")
	case !day.MatchString(t.CheckedOn):
		return refuse(CodeBilling, "billing.quota.terms.checked_on", "is not a day as YYYY-MM-DD", "Give the day you read the terms")
	}
	if _, err := time.Parse(time.DateOnly, t.CheckedOn); err != nil {
		return refuse(CodeBilling, "billing.quota.terms.checked_on", "is not a day as YYYY-MM-DD", "Give the day you read the terms")
	}
	return nil
}

var (
	jwtShape    = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*$`)
	cookieShape = regexp.MustCompile(`(?i)^(cookie:|set-cookie:|sessionkey=|session=|sid=|__secure-|__host-)|;\s*\w+=`)
)

// CheckCredential refuses what is not an API key the provider issues to an application (AI-73, AI-79): a sign-in
// token of a chat app or a consumer plan, a session, a cookie, or a key pasted with a prefix or spaces. Anthropic's
// OAuth tokens (sk-ant-oat…), which it reserves for Claude Code and Claude.ai, are refused [verify: the prefix].
func CheckCredential(connectorID, value string) error {
	fix := "Paste the API key the provider issued to your organisation's project, alone"
	switch {
	case len(value) < 16 || len(value) > 4096:
		return refuse(CodeCredential, "value", "is not between 16 and 4,096 characters", fix)
	case strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return refuse(CodeCredential, "value", "has spaces or line breaks", fix)
	case strings.HasPrefix(strings.ToLower(value), "bearer"):
		return refuse(CodeCredential, "value", "starts with Bearer", fix)
	case jwtShape.MatchString(value):
		return refuse(CodeCredential, "value", "is a sign-in token, such as a chat app's, not an API key; consumer plans cannot be connected (AI-73)", fix)
	case cookieShape.MatchString(value):
		return refuse(CodeCredential, "value", "is a browser session or cookie, which cannot be connected (AI-73)", fix)
	case connectorID == "anthropic" && strings.HasPrefix(value, "sk-ant-oat"):
		return refuse(CodeCredential, "value", "is a Claude plan's sign-in token, which Anthropic reserves for its own apps (AI-73)",
			"Create an API key in the Claude Console and paste it")
	}
	return nil
}
