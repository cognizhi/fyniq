package api

import (
	"context"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Screening the AI's traffic (PRD-001 AI-18, AI-P7, AI-25; p1-ai-assistant design D8): what the AI service sends a
// model, the untrusted text a read tool returns and what a model answers each pass the Screening port (Model Armor, or
// local mode's stub) before they go on. A finding stops it; a screening that did not finish, or none set up, stops it
// too (fail closed).

// The codes of a screening's refusal.
const (
	CodeScreened         = "AI-SCREEN-01" // screening found what may not go to a model or come from one
	CodeScreenIncomplete = "AI-SCREEN-02" // screening did not finish, or none is set up: nothing went on
)

// screen screens texts on their way to a model, or a model's answer, and refuses on a finding or an unfinished screen.
func (a *API) screen(ctx context.Context, answer bool, texts ...string) error {
	if a.deps.Screening == nil {
		return problem(CodeScreenIncomplete, "No screening is set up, so nothing goes to a model.",
			"Ask the platform's operator to set up Model Armor's template (FYNIQ_MODEL_ARMOR_TEMPLATE)")
	}
	text := strings.Join(slices.DeleteFunc(slices.Clone(texts), func(s string) bool { return strings.TrimSpace(s) == "" }), "\n\n")
	if text == "" {
		return nil
	}
	screen := a.deps.Screening.ScreenPrompt
	if answer {
		screen = a.deps.Screening.ScreenResponse
	}
	s, err := screen(ctx, text)
	if err != nil {
		logFailure(ctx, err)
		return problem(CodeScreenIncomplete, "Screening did not finish, so nothing went on.", "Try again in a moment")
	}
	if s.Flagged() {
		what := "the request"
		if answer {
			what = "the model's answer"
		}
		return problem(CodeScreened, "Screening found "+findings(s)+" in "+what+", so it went no further.",
			"Ask again without it; text from outside the organisation is data, never instructions")
	}
	return nil
}

// findings are a screening's findings in words: what each filter found, never the text it matched.
func findings(s ports.Screened) string {
	var words []string
	for _, f := range s.Findings {
		w := strings.ReplaceAll(f.Filter, "_", " ")
		if f.Detail != "" {
			w = f.Detail
		}
		if !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	return strings.Join(words, "; ")
}

// promptTexts are the texts of a request to a model that screening reads: the person's words and the tools' results.
// The model's own earlier answers were screened as they came.
func promptTexts(r ports.ModelRequest) []string {
	var out []string
	for _, m := range r.Messages {
		if m.Role == ports.RoleAssistant {
			continue
		}
		for _, p := range m.Parts {
			if p.Kind == ports.PartText || p.Kind == ports.PartToolResult {
				out = append(out, p.Text)
			}
		}
	}
	return out
}

// answerTexts are the texts of a model's answer: its words and the input of each tool it calls.
func answerTexts(r ports.ModelResponse) []string {
	var out []string
	for _, p := range r.Parts {
		switch p.Kind {
		case ports.PartText:
			out = append(out, p.Text)
		case ports.PartToolCall:
			out = append(out, string(p.Input))
		}
	}
	return out
}

// untrustedKeys are the fields of free text a read returns: written by people, inside the organisation or out of it, or
// received from outside, such as a bank line's description, a counterparty's name or a memo (AI-18, AI-25).
var untrustedKeys = []string{"description", "memo", "notes", "note", "title", "detail", "reference", "supplier_ref", "legal_name", "name",
	"member_name", "merchant", "narrative", "payee", "counterparty", "party", "subject", "body", "text", "comment", "reason"}

// quoteText wraps every free text in v as untrusted_text and collects the texts, for screening.
func quoteText(v any, texts *[]string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if s, ok := e.(string); ok && s != "" && slices.Contains(untrustedKeys, k) {
				*texts = append(*texts, s)
				out[k] = map[string]any{"untrusted_text": s}
				continue
			}
			out[k] = quoteText(e, texts)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = quoteText(e, texts)
		}
		return out
	}
	return v
}

// withhold replaces every quoted text in v with why it is withheld.
func withhold(v any, why string) any {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["untrusted_text"]; ok && len(x) == 1 {
			return map[string]any{"withheld": why}
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = withhold(e, why)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withhold(e, why)
		}
		return out
	}
	return v
}
