//go:build local

package local

import (
	"context"
	"regexp"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Screen is local mode's ports.Screening, standing in for Model Armor (PRD-001 §12.8, "Local Model Armor stub";
// p1-ai-providers design D12): it flags the red-team corpus's patterns (QA-AI-03), so the guardrails' handling of a
// finding is tested offline. It is a stub, not a screen: it knows the corpus, and real Model Armor is tested in staging
// [live-verify]. It screens prompts and answers alike, and does not judge harmful content.
type Screen struct{}

var _ ports.Screening = Screen{}

type pattern struct {
	filter, detail, confidence string
	re                         *regexp.Regexp
}

// screenPatterns are the red-team corpus's patterns (testdata/screening-corpus.json holds a case for each).
var screenPatterns = []pattern{
	{ports.ScreenInjection, "an instruction to ignore earlier instructions", ports.ConfidenceHigh,
		regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget|override)\b[^.\n]{0,40}\b(?:previous|prior|above|earlier|all|your|the|any)\b[^.\n]{0,20}\b(?:instructions?|prompts?|rules|guidelines|directions)\b`)},
	{ports.ScreenInjection, "a request for the system prompt", ports.ConfidenceHigh,
		regexp.MustCompile(`(?i)\b(?:system|developer|hidden)\s+(?:prompt|message|instructions?)\b`)},
	{ports.ScreenInjection, "new instructions planted in data", ports.ConfidenceHigh,
		regexp.MustCompile(`(?i)\b(?:new|updated|additional|real)\s+instructions?\s*:|</?\s*(?:system|assistant|instructions?)\s*>`)},
	{ports.ScreenInjection, "an attempt to change the model's role", ports.ConfidenceHigh,
		regexp.MustCompile(`(?i)\byou\s+are\s+now\b|\b(?:jailbreak|do anything now|developer mode)\b`)},
	{ports.ScreenInjection, "a message addressed to the AI", ports.ConfidenceMedium,
		regexp.MustCompile(`(?i)\b(?:dear|attention|note to|message to|hey|hello)\s+(?:the\s+)?(?:ai|assistant|model|llm|claude|gpt|chatgpt|gemini)\b`)},
	{ports.ScreenInjection, "a request to act without approval", ports.ConfidenceHigh,
		regexp.MustCompile(`(?i)\b(?:approve|execute|transfer|pay|release|post)\b[^.\n]{0,40}\bwithout\b[^.\n]{0,20}\b(?:approval|review|asking|confirmation|checking)\b`)},
	{ports.ScreenMaliciousURL, "a Markdown image from another site", ports.ConfidenceHigh,
		regexp.MustCompile(`!\[[^\]]*\]\(\s*<?(?:https?:)?//`)},
	{ports.ScreenMaliciousURL, "an HTML image from another site", ports.ConfidenceHigh,
		regexp.MustCompile(`(?i)<img\b[^>]*\bsrc\s*=\s*["']?(?:https?:)?//`)},
	{ports.ScreenMaliciousURL, "a link that carries data in its address", ports.ConfidenceMedium,
		regexp.MustCompile(`\]\(\s*<?https?://[^)\s]*\?[^)\s]*=`)},
	{ports.ScreenMaliciousURL, "a script or data link", ports.ConfidenceHigh,
		regexp.MustCompile(`(?i)\bjavascript:|\bdata:text/html`)},
	{ports.ScreenSensitive, "a bank account number", ports.ConfidenceMedium,
		regexp.MustCompile(`(?i)(?:\b(?:a/c|acct|account)(?:\s+(?:no|number))?\.?\s*[:#=]?\s*\d[\d -]{7,18}\d\b|\b\d{3}-\d{5,6}-\d{1,3}\b)`)},
}

func screen(text string) ports.Screened {
	var out ports.Screened
	for _, p := range screenPatterns {
		if p.re.MatchString(text) {
			out.Findings = append(out.Findings, ports.ScreenFinding{Filter: p.filter, Detail: p.detail, Confidence: p.confidence})
		}
	}
	// What the logs may never hold (SEC-15), screened the same way: an NRIC or FIN, a token or a key.
	for _, kind := range audit.Leaks(text) {
		out.Findings = append(out.Findings, ports.ScreenFinding{Filter: ports.ScreenSensitive, Detail: kind, Confidence: ports.ConfidenceHigh})
	}
	return out
}

// ScreenPrompt implements ports.Screening.
func (Screen) ScreenPrompt(_ context.Context, text string) (ports.Screened, error) {
	return screen(text), nil
}

// ScreenResponse implements ports.Screening.
func (Screen) ScreenResponse(_ context.Context, text string) (ports.Screened, error) {
	return screen(text), nil
}
