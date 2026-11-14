// Package aiconnection is a connection to an AI model provider (PRD-001 AI-72, AI-73, AI-79; p1-ai-providers design D2):
// one of the release's model-provider connectors, from one of its presets or the connection's own endpoint, with how
// it authenticates, its configuration, the one host its calls go to, its credential by reference, and how it is
// billed. A connection starts off; creating it, changing it, replacing its credential and enabling it are each asked
// for by one person and approved by the Owner (golden rule 6); switching it off is never held back.
package aiconnection

import (
	"fmt"
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
