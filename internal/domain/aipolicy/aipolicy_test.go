package aipolicy_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
)

var today = time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

func code(err error) string {
	var e *aipolicy.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func days(n int) *int { return &n }

func source(on string) aipolicy.Evidence {
	return aipolicy.Evidence{Kind: "source", URL: "https://cloud.google.com/vertex-ai/generative-ai/docs/learn/locations", Title: "Locations", CheckedOn: on}
}

// Claude on Agent Platform in Singapore for the 4.6 models, and on the global endpoint for the newest.
func singapore() aipolicy.Declaration {
	return aipolicy.Declaration{Inventory: "inv-1", Handling: []aipolicy.Handling{
		{Model: "*", Endpoint: "aiplatform.googleapis.com", Feature: "*", ProcessedIn: []string{"US", "EU"}, StoredIn: []string{"US"},
			RetentionDays: days(0), Training: "no", Evidence: source("2026-11-01")},
		{Model: "claude-opus-4-6", Endpoint: "asia-southeast1-aiplatform.googleapis.com", Feature: "*", ProcessedIn: []string{"SG"},
			StoredIn: []string{"SG"}, RetentionDays: days(0), Training: "no", Evidence: source("2026-11-01")},
		{Model: "claude-opus-4-6", Endpoint: "asia-southeast1-aiplatform.googleapis.com", Feature: "batch", ProcessedIn: []string{"SG"},
			Unclear: true, Training: "no", Evidence: source("2026-11-01")},
	}}
}

// Each connection declares, per model, endpoint and feature, where prompts are processed and stored, their retention
// and training, with dated evidence; an incomplete or undated entry is refused with its field (AI-75, AI-88).
// req: AI-75
// req: AI-88
func TestDeclarations(t *testing.T) {
	if err := aipolicy.Check(singapore(), today); err != nil {
		t.Fatalf("a complete declaration: %v", err)
	}
	for name, c := range map[string]struct {
		change func(h *aipolicy.Handling)
		field  string
	}{
		"no model":                 {func(h *aipolicy.Handling) { h.Model = "" }, "handling[0].model"},
		"nowhere processed":        {func(h *aipolicy.Handling) { h.ProcessedIn = nil }, "handling[0].processed_in"},
		"a region, not a country":  {func(h *aipolicy.Handling) { h.ProcessedIn = []string{"asia-southeast1"} }, "handling[0].processed_in"},
		"training not said":        {func(h *aipolicy.Handling) { h.Training = "" }, "handling[0].training"},
		"a negative retention":     {func(h *aipolicy.Handling) { h.RetentionDays = days(-1) }, "handling[0].retention_days"},
		"evidence from the future": {func(h *aipolicy.Handling) { h.Evidence.CheckedOn = "2026-12-01" }, "handling[0].evidence.checked_on"},
		"evidence over plain HTTP": {func(h *aipolicy.Handling) { h.Evidence.URL = "http://example.com" }, "handling[0].evidence.url"},
		"a contract not kept": {func(h *aipolicy.Handling) {
			h.Evidence = aipolicy.Evidence{Kind: "contract", Title: "DPA s.4", CheckedOn: "2026-11-01"}
		}, "handling[0].evidence.evidence_id"},
		"evidence of no kind": {func(h *aipolicy.Handling) { h.Evidence.Kind = "blog" }, "handling[0].evidence.kind"},
		"a model, endpoint, twice": {func(h *aipolicy.Handling) {
			h.Model, h.Endpoint, h.Feature = "claude-opus-4-6", "asia-southeast1-aiplatform.googleapis.com", "*"
		}, "handling[1].model"},
	} {
		d := singapore()
		c.change(&d.Handling[0])
		var e *aipolicy.Error
		err := aipolicy.Check(d, today)
		if !errors.As(err, &e) || e.Code != aipolicy.CodeDeclaration || e.Field != c.field {
			t.Errorf("%s: %v; want %s", name, err, c.field)
		}
	}
}

// Where prompts are processed decides where the sensitive classes go: by default personal data and payroll only where
// Singapore processing is evidenced (clear, Singapore only) for the model and feature; restricted data nowhere; the
// rest anywhere the policy does not exclude. An Owner widens a class only to a connection whose transfer is recorded
// (CMP-PD-06), and never restricted data (AI-75, AI-83, AI-88).
// req: AI-75
// req: AI-83
// req: AI-88
// req: CMP-PD-06
func TestDataGoesWhereThePolicyAllows(t *testing.T) {
	sg := aipolicy.Connection{ID: "c-sg", Enabled: true, Declaration: singapore()}
	us := aipolicy.Connection{ID: "c-us", Enabled: true, Declaration: aipolicy.Declaration{Handling: []aipolicy.Handling{{Model: "*",
		Endpoint: "api.openai.com", Feature: "*", ProcessedIn: []string{"US"}, StoredIn: []string{"SG"}, Training: "no", Evidence: source("2026-11-01")}}}}
	none := aipolicy.Policy{}
	for name, c := range map[string]struct {
		conn    aipolicy.Connection
		model   string
		feature string
		classes []string
		ok      bool
	}{
		"payroll to Claude in Singapore":               {sg, "claude-opus-4-6", "messages", []string{aipolicy.Payroll, aipolicy.KnowHow}, true},
		"payroll to the newest Claude, global":         {sg, "claude-opus-5-5", "messages", []string{aipolicy.Payroll}, false},
		"payroll in a batch, unclear":                  {sg, "claude-opus-4-6", "batch", []string{aipolicy.Payroll}, false},
		"personal data to OpenAI, stored in Singapore": {us, "gpt-6", "messages", []string{aipolicy.Personal}, false},
		"business figures to OpenAI":                   {us, "gpt-6", "messages", []string{aipolicy.Business, aipolicy.KnowHow}, true},
		"restricted data anywhere":                     {sg, "claude-opus-4-6", "messages", []string{aipolicy.Restricted}, false},
		"anything to a connection switched off":        {aipolicy.Connection{ID: "c-off", Declaration: singapore()}, "claude-opus-4-6", "messages", []string{aipolicy.KnowHow}, false},
	} {
		err := aipolicy.Allowed(none, c.conn, c.model, c.feature, c.classes)
		if (err == nil) != c.ok || (err != nil && code(err) != aipolicy.CodeNotAllowed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if aipolicy.Allowed(aipolicy.Policy{Exclude: map[string][]string{aipolicy.Business: {"c-us"}}}, us, "gpt-6", "messages", []string{aipolicy.Business}) == nil {
		t.Error("an excluded connection took business figures")
	}

	connections := map[string]aipolicy.Connection{"c-sg": sg, "c-us": us}
	if err := aipolicy.CheckPolicy(aipolicy.Policy{Allow: map[string][]string{aipolicy.Personal: {"c-us"}}}, connections); code(err) != aipolicy.CodePolicy {
		t.Errorf("personal data overseas without a transfer: %v", err)
	}
	us.Declaration.Transfer = "transfer-1"
	connections["c-us"] = us
	widened := aipolicy.Policy{Allow: map[string][]string{aipolicy.Personal: {"c-us"}}}
	if err := aipolicy.CheckPolicy(widened, connections); err != nil {
		t.Errorf("personal data overseas with the transfer assessed: %v", err)
	}
	if err := aipolicy.Allowed(widened, us, "gpt-6", "messages", []string{aipolicy.Personal}); err != nil {
		t.Errorf("the widened policy: %v", err)
	}
	for name, p := range map[string]aipolicy.Policy{
		"restricted allowed":   {Allow: map[string][]string{aipolicy.Restricted: {"c-sg"}}},
		"an unknown class":     {Allow: map[string][]string{"secrets": {"c-sg"}}},
		"another's connection": {Allow: map[string][]string{aipolicy.KnowHow: {"c-elsewhere"}}},
	} {
		if code(aipolicy.CheckPolicy(p, connections)) != aipolicy.CodePolicy {
			t.Errorf("%s was accepted", name)
		}
	}
	if !us.Declaration.Overseas() || sg.Declaration.Handling[1:2] == nil || (aipolicy.Declaration{Handling: singapore().Handling[1:2]}).Overseas() {
		t.Error("Overseas")
	}
}

// The Singapore table chooses the presets' Singapore endpoints against each cloud's availability (§12.12): Claude on
// Google Cloud processes the 4.6 and 4.5 models in asia-southeast1 and not the newest; OpenAI's Singapore region only
// stores there.
// req: platform/portability-and-cost Claude in Singapore tie
func TestSingaporeEndpoints(t *testing.T) {
	sg := aipolicy.SingaporeModels("anthropic", "asia-southeast1-aiplatform.googleapis.com")
	if !slices.Contains(sg, "claude-opus-4-6") || !slices.Contains(sg, "claude-sonnet-4-6") || slices.Contains(sg, "claude-opus-5-5") {
		t.Errorf("Claude in Singapore: %v", sg)
	}
	if aipolicy.SingaporeModels("openai", "api.openai.com") != nil {
		t.Error("OpenAI listed as processing in Singapore")
	}
	for _, e := range append(aipolicy.Singapore, aipolicy.OnlyStoredInSingapore...) {
		if err := aipolicy.Check(aipolicy.Declaration{Handling: []aipolicy.Handling{{Model: "*", Endpoint: e.Endpoint, Feature: "*", ProcessedIn: []string{"SG"},
			Training: "unknown", Evidence: e.Evidence}}}, today); err != nil {
			t.Errorf("%s's evidence: %v", e.Endpoint, err)
		}
	}
}
