package aiconnection_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/domain/aiconnection"
)

func code(err error) string {
	var e *aiconnection.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// A connection is one of the release's model providers, from a preset or its own endpoint, with an authentication the
// provider and its preset offer; its calls go to one https host, a self-hosted server's only as a registered private
// endpoint; it is billed by usage or by a subscription with a quota (AI-72, AI-73, AI-79). Each wrong part is refused
// with its field and code.
// req: AI-72
// req: AI-73
func TestConnectionDefinitions(t *testing.T) {
	cat, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	quota := &aiconnection.Quota{Amount: 5000000, Unit: "tokens", Window: "month", Fee: "30.00", Currency: "USD"}
	for name, c := range map[string]struct {
		d          aiconnection.Definition
		host       string
		selfHosted bool
	}{
		"Claude on Agent Platform in Singapore": {aiconnection.Definition{Name: "Claude SG", Connector: "anthropic", Auth: "workload-identity",
			Configuration: map[string]any{"endpoint": "agent-platform", "project": "fyniq-dev"}, Billing: "usage"}, "asia-southeast1-aiplatform.googleapis.com", false},
		"the Anthropic API": {aiconnection.Definition{Name: "Claude API", Connector: "anthropic", Auth: "api-key",
			Configuration: map[string]any{"endpoint": "anthropic-api"}, Billing: "usage"}, "api.anthropic.com", false},
		"OpenAI": {aiconnection.Definition{Name: "OpenAI", Connector: "openai", Auth: "api-key", Configuration: map[string]any{}, Billing: "usage"}, "api.openai.com", false},
		"Gemini globally": {aiconnection.Definition{Name: "Gemini", Connector: "gemini", Auth: "workload-identity",
			Configuration: map[string]any{"project": "fyniq-dev", "location": "global"}, Billing: "usage"}, "aiplatform.googleapis.com", false},
		"Kimi on a plan": {aiconnection.Definition{Name: "Kimi", Connector: "openai-compatible", Preset: "kimi", Auth: "api-key",
			Configuration: map[string]any{"preset": "kimi"}, Billing: "subscription", Quota: quota}, "api.moonshot.ai", false},
		"Ollama in the office": {aiconnection.Definition{Name: "Ollama", Connector: "openai-compatible", Preset: "ollama", Auth: "none",
			Configuration: map[string]any{"preset": "ollama", "base_url": "https://10.20.0.5:11434/v1", "auth": "none"}, Billing: "usage"}, "10.20.0.5", true},
	} {
		got, err := aiconnection.Check(cat, c.d)
		if err != nil || got.Host != c.host || got.SelfHosted != c.selfHosted {
			t.Errorf("%s: %+v %v", name, got, err)
		}
	}

	ok := aiconnection.Definition{Name: "OpenAI", Connector: "openai", Auth: "api-key", Configuration: map[string]any{}, Billing: "usage"}
	for name, c := range map[string]struct {
		change func(d *aiconnection.Definition)
		code   string
		field  string
	}{
		"no name":                       {func(d *aiconnection.Definition) { d.Name = " " }, aiconnection.CodeName, "name"},
		"not a model provider":          {func(d *aiconnection.Definition) { d.Connector = "resend" }, aiconnection.CodeProvider, "connector"},
		"an unknown preset":             {func(d *aiconnection.Definition) { d.Preset = "kimi" }, aiconnection.CodeProvider, "preset"},
		"a person's own plan":           {func(d *aiconnection.Definition) { d.Auth = "oauth-grant" }, aiconnection.CodeAuth, "auth"},
		"an authentication not offered": {func(d *aiconnection.Definition) { d.Auth = "workload-identity" }, aiconnection.CodeAuth, "auth"},
		"what the probe writes":         {func(d *aiconnection.Definition) { d.Configuration = map[string]any{"proven": []any{"tools"}} }, aiconnection.CodeConfig, "configuration.proven"},
		"a field the provider lacks":    {func(d *aiconnection.Definition) { d.Configuration = map[string]any{"colour": "teal"} }, aiconnection.CodeConfig, "configuration"},
		"plain HTTP": {func(d *aiconnection.Definition) {
			d.Configuration = map[string]any{"base_url": "http://api.openai.com/v1"}
		}, aiconnection.CodeConfig, "configuration"},
		"a private host on a hosted provider": {func(d *aiconnection.Definition) {
			d.Connector, d.Configuration = "openai-compatible", map[string]any{"base_url": "https://192.168.1.10/v1"}
		}, aiconnection.CodeEgress, "configuration.base_url"},
		"localhost": {func(d *aiconnection.Definition) {
			d.Connector, d.Configuration = "openai-compatible", map[string]any{"base_url": "https://localhost:8080/v1"}
		}, aiconnection.CodeEgress, "configuration.base_url"},
		"no key on a hosted provider": {func(d *aiconnection.Definition) {
			d.Connector, d.Preset, d.Auth, d.Configuration = "openai-compatible", "openrouter", "none", map[string]any{"preset": "openrouter"}
		}, aiconnection.CodeAuth, "auth"},
		"a key for Agent Platform": {func(d *aiconnection.Definition) {
			d.Connector, d.Configuration = "anthropic", map[string]any{"endpoint": "agent-platform", "project": "fyniq-dev"}
		}, aiconnection.CodeAuth, "auth"},
		"an unknown billing mode":      {func(d *aiconnection.Definition) { d.Billing = "free" }, aiconnection.CodeBilling, "billing.mode"},
		"a subscription with no quota": {func(d *aiconnection.Definition) { d.Billing = "subscription" }, aiconnection.CodeBilling, "billing.quota"},
		"a quota on usage":             {func(d *aiconnection.Definition) { d.Quota = quota }, aiconnection.CodeBilling, "billing.quota"},
		"a fee in floating point": {func(d *aiconnection.Definition) {
			d.Billing, d.Quota = "subscription", &aiconnection.Quota{Amount: 1, Unit: "tokens", Window: "month", Fee: "1e3", Currency: "USD"}
		}, aiconnection.CodeBilling, "billing.quota.fee"},
		"a negative fee": {func(d *aiconnection.Definition) {
			d.Billing, d.Quota = "subscription", &aiconnection.Quota{Amount: 1, Unit: "tokens", Window: "month", Fee: "-1.00", Currency: "USD"}
		}, aiconnection.CodeBilling, "billing.quota.fee"},
		"a quota by the hour": {func(d *aiconnection.Definition) {
			d.Billing, d.Quota = "subscription", &aiconnection.Quota{Amount: 1, Unit: "tokens", Window: "hour", Fee: "1.00", Currency: "USD"}
		}, aiconnection.CodeBilling, "billing.quota.window"},
	} {
		d := ok
		d.Configuration = map[string]any{}
		c.change(&d)
		_, err := aiconnection.Check(cat, d)
		var e *aiconnection.Error
		if !errors.As(err, &e) || e.Code != c.code || e.Field != c.field || e.Fix == "" {
			t.Errorf("%s: %v; want %s on %s", name, err, c.code, c.field)
		}
	}
}

// What is not an API key the provider issues to an application is refused (AI-73, AI-79): a chat app's sign-in token,
// a browser session or cookie, a Claude plan's OAuth token, or a key pasted with a prefix or spaces. Every value here
// is synthetic and built from parts.
// req: AI-73
// req: AI-79
func TestCredentialsThatAreNotAPIKeys(t *testing.T) {
	jwt := "eyJ" + "hbGciOiJIUzI1NiJ9" + "." + "eyJzdWIiOiJ1c2VyIn0" + "." + "c2lnbmF0dXJl"
	for name, c := range map[string]struct{ connector, value string }{
		"a sign-in token":       {"openai", jwt},
		"a browser cookie":      {"openai", "__Secure-next-auth.session-token" + "=abcdef0123456789"},
		"a session key":         {"anthropic", "sessionKey=" + "sk-ant-sid01-abcdef0123456789"},
		"a cookie header":       {"openai", "Cookie: a=" + "1; b=2; c=0123456789"},
		"a Claude plan's token": {"anthropic", "sk-ant-oat01-" + "abcdefghijklmnopqrstuvwxyz"},
		"a bearer prefix":       {"openai", "Bearer " + "sk-proj-abcdefghijklmnop"},
		"spaces":                {"openai", "sk-proj-abc " + "defghijklmnopqrs"},
		"too short":             {"openai", "sk-" + "abc"},
	} {
		err := aiconnection.CheckCredential(c.connector, c.value)
		if code(err) != aiconnection.CodeCredential || strings.Contains(err.Error(), c.value) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for connector, key := range map[string]string{"anthropic": "sk-ant-api03-" + "abcdefghijklmnopqrstuvwxyz", "openai": "sk-proj-" + "abcdefghijklmnopqrstuvwxyz",
		"openai-compatible": "sk-or-v1-" + "0123456789abcdef0123456789abcdef"} {
		if err := aiconnection.CheckCredential(connector, key); err != nil {
			t.Errorf("%s's API key: %v", connector, err)
		}
	}
}
