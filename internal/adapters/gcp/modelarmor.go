package gcp

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// ModelArmorAPI is Model Armor's regional endpoint for location, such as asia-southeast1: the text is screened where
// it is processed (PRD-001 §12.5).
func ModelArmorAPI(location string) string {
	return "https://modelarmor." + location + ".rep.googleapis.com"
}

// ModelArmor is ports.Screening on Model Armor (PRD-001 AI-18, AI-20; p1-ai-providers design D12): each prompt and
// answer goes to a template's sanitizeUserPrompt or sanitizeModelResponse, and each filter that matched becomes a
// finding. A screening that did not finish is ports.ErrScreeningIncomplete, so the guardrails fail closed.
// [live-verify]: the endpoint, the request and the answer's fields follow Google's page "Sanitize prompts and
// responses" (retrieved 14 Nov 2026) and are tested against a fake until the dev cloud runs them.
type ModelArmor struct {
	Endpoint string
	Template string // projects/<project>/locations/<location>/templates/<template>
	r        rest
}

var _ ports.Screening = (*ModelArmor)(nil)

var armorTemplate = regexp.MustCompile(`^projects/[a-z0-9-]+/locations/([a-z0-9-]+)/templates/[A-Za-z0-9_-]+$`)

// NewModelArmor returns the adapter for template, on endpoint, or on the template's own regional endpoint when
// endpoint is "".
func NewModelArmor(endpoint, template string, tokens oauth2.TokenSource, client *http.Client) (*ModelArmor, error) {
	m := armorTemplate.FindStringSubmatch(template)
	if m == nil {
		return nil, fmt.Errorf("model armor: %q is not a template's name (projects/…/locations/…/templates/…)", template)
	}
	if endpoint == "" {
		endpoint = ModelArmorAPI(m[1])
	}
	return &ModelArmor{Endpoint: strings.TrimSuffix(endpoint, "/"), Template: template, r: rest{tokens: tokens, client: client}}, nil
}

// ScreenPrompt implements ports.Screening.
func (a *ModelArmor) ScreenPrompt(ctx context.Context, text string) (ports.Screened, error) {
	return a.sanitize(ctx, "sanitizeUserPrompt", map[string]any{"userPromptData": map[string]string{"text": text}})
}

// ScreenResponse implements ports.Screening.
func (a *ModelArmor) ScreenResponse(ctx context.Context, text string) (ports.Screened, error) {
	return a.sanitize(ctx, "sanitizeModelResponse", map[string]any{"modelResponseData": map[string]string{"text": text}})
}

// armorMatch is one filter's result.
type armorMatch struct {
	ExecutionState  string `json:"executionState"`
	MatchState      string `json:"matchState"`
	ConfidenceLevel string `json:"confidenceLevel"`
}

func (m *armorMatch) found() bool { return m != nil && m.MatchState == "MATCH_FOUND" }

type armorSDP struct {
	armorMatch
	Findings []struct {
		InfoType string `json:"infoType"`
	} `json:"findings"`
}

type armorAnswer struct {
	SanitizationResult struct {
		FilterMatchState string `json:"filterMatchState"`
		InvocationResult string `json:"invocationResult"`
		FilterResults    map[string]struct {
			PI   *armorMatch `json:"piAndJailbreakFilterResult"`
			URI  *armorMatch `json:"maliciousUriFilterResult"`
			CSAM *armorMatch `json:"csamFilterFilterResult"`
			RAI  *struct {
				armorMatch
				Types map[string]armorMatch `json:"raiFilterTypeResults"`
			} `json:"raiFilterResult"`
			SDP *struct {
				Inspect    *armorSDP `json:"inspectResult"`
				Deidentify *armorSDP `json:"deidentifyResult"`
			} `json:"sdpFilterResult"`
		} `json:"filterResults"`
	} `json:"sanitizationResult"`
}

// confidence is a finding's confidence from the filter's level.
func confidence(level string) string {
	switch level {
	case "LOW_AND_ABOVE":
		return ports.ConfidenceLow
	case "MEDIUM_AND_ABOVE":
		return ports.ConfidenceMedium
	case "HIGH":
		return ports.ConfidenceHigh
	}
	return ""
}

func (a *ModelArmor) sanitize(ctx context.Context, method string, req any) (ports.Screened, error) {
	body, err := jsonBody(req)
	if err != nil {
		return ports.Screened{}, err
	}
	var ans armorAnswer
	if err := a.r.do(ctx, http.MethodPost, a.Endpoint+"/v1/"+a.Template+":"+method, "application/json", body, &ans); err != nil {
		return ports.Screened{}, fmt.Errorf("model armor %s: %w", method, err)
	}
	res := ans.SanitizationResult
	if res.InvocationResult != "SUCCESS" {
		return ports.Screened{}, fmt.Errorf("model armor %s answered %q: %w", method, res.InvocationResult, ports.ErrScreeningIncomplete)
	}
	var out ports.Screened
	add := func(filter, detail string, m armorMatch) {
		out.Findings = append(out.Findings, ports.ScreenFinding{Filter: filter, Detail: detail, Confidence: confidence(m.ConfidenceLevel)})
	}
	// In a fixed order, whatever order the answer's map has.
	if r, ok := res.FilterResults["pi_and_jailbreak"]; ok && r.PI.found() {
		add(ports.ScreenInjection, "prompt injection or jailbreak", *r.PI)
	}
	if r, ok := res.FilterResults["sdp"]; ok && r.SDP != nil {
		for _, s := range []*armorSDP{r.SDP.Inspect, r.SDP.Deidentify} {
			if s == nil || !s.found() {
				continue
			}
			var types []string
			for _, f := range s.Findings {
				if f.InfoType != "" && !slices.Contains(types, f.InfoType) {
					types = append(types, f.InfoType)
				}
			}
			slices.Sort(types)
			detail := "sensitive data"
			if len(types) > 0 {
				detail = strings.Join(types, ", ")
			}
			add(ports.ScreenSensitive, detail, s.armorMatch)
			break
		}
	}
	if r, ok := res.FilterResults["malicious_uris"]; ok && r.URI.found() {
		add(ports.ScreenMaliciousURL, "a malicious link", *r.URI)
	}
	if r, ok := res.FilterResults["rai"]; ok && r.RAI != nil && r.RAI.found() {
		var types []string
		for name, m := range r.RAI.Types {
			if m.found() {
				types = append(types, name)
			}
		}
		slices.Sort(types)
		detail := "harmful content"
		if len(types) > 0 {
			detail = strings.Join(types, ", ")
		}
		add(ports.ScreenHarmful, detail, r.RAI.armorMatch)
	}
	if r, ok := res.FilterResults["csam"]; ok && r.CSAM.found() {
		add(ports.ScreenHarmful, "child sexual abuse material", *r.CSAM)
	}
	if res.FilterMatchState == "MATCH_FOUND" && !out.Flagged() {
		// A match the adapter cannot place is still a match.
		out.Findings = append(out.Findings, ports.ScreenFinding{Filter: ports.ScreenOther, Detail: "a filter this adapter does not know"})
	}
	return out, nil
}
