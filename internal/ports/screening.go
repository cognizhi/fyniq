package ports

import (
	"context"
	"errors"
)

// Screening screens what goes to a model and what comes back (PRD-001 AI-18, AI-20; p1-ai-providers design D12):
// prompts, documents' text and tool results for prompt injection, jailbreaks, sensitive data and malicious links; a
// model's answer for sensitive data and links before anyone sees it. Production: Model Armor in Singapore (the gcp
// adapter) [live-verify]. Local: a stub that flags the red-team corpus's patterns (§12.8). The guardrails decide what a
// finding does; screening only reports.
type Screening interface {
	// ScreenPrompt screens text on its way to a model.
	ScreenPrompt(ctx context.Context, text string) (Screened, error)
	// ScreenResponse screens a model's answer.
	ScreenResponse(ctx context.Context, text string) (Screened, error)
}

// Screened is what screening found: nothing, or one finding per filter that matched.
type Screened struct {
	Findings []ScreenFinding `json:"findings,omitempty"`
}

// Flagged reports whether any filter matched.
func (s Screened) Flagged() bool { return len(s.Findings) > 0 }

// Has reports whether the filter matched.
func (s Screened) Has(filter string) bool {
	for _, f := range s.Findings {
		if f.Filter == filter {
			return true
		}
	}
	return false
}

// The filters a finding comes from.
const (
	ScreenInjection    = "prompt_injection" // instructions planted in data, or an attempt to jailbreak the model
	ScreenSensitive    = "sensitive_data"   // an NRIC or FIN, a bank account number, a token or a key (AI-02, AI-20)
	ScreenMaliciousURL = "malicious_url"    // a link or an image that would carry data away (QA-AI-03)
	ScreenHarmful      = "harmful"          // harmful content: hate, harassment, sexual, dangerous
	ScreenOther        = "other"            // a filter matched that the adapter cannot place
)

// The confidence of a finding, as the screen reports it.
const (
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
	ConfidenceHigh   = "high"
)

// ScreenFinding is one filter that matched. Detail names what matched, such as "an NRIC or FIN number" or an info type,
// and never holds the matched text, which may be the very value screening protects.
type ScreenFinding struct {
	Filter     string `json:"filter"`
	Detail     string `json:"detail,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

// ErrScreeningIncomplete is a screen that did not finish screening, so nothing is known about the text: the guardrails
// treat it as a finding and send nothing (fail closed).
var ErrScreeningIncomplete = errors.New("screening did not finish")
