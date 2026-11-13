package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

const armorTemplateName = "projects/fyniq-staging/locations/asia-southeast1/templates/fyniq-ai"

// fakeArmor answers sanitizeUserPrompt and sanitizeModelResponse as Google's page shows Model Armor doing, from the
// text it is given: "inject" matches prompt injection, "S1234567D" sensitive data, "evil.example" a malicious link,
// "harm" the dangerous filter; "partial" answers an invocation that did not finish, "fail" a server error.
type fakeArmor struct {
	paths []string
	auth  string
	field string
}

func (f *fakeArmor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.paths = append(f.paths, r.URL.Path)
	f.auth = r.Header.Get("Authorization")
	b, _ := io.ReadAll(r.Body)
	var req map[string]map[string]string
	_ = json.Unmarshal(b, &req)
	var text string
	for field, data := range req {
		f.field, text = field, data["text"]
	}
	if strings.Contains(text, "fail") {
		http.Error(w, "the text was "+text, http.StatusInternalServerError)
		return
	}
	match := func(on bool, level string) map[string]any {
		state := "NO_MATCH_FOUND"
		if on {
			state = "MATCH_FOUND"
		}
		m := map[string]any{"executionState": "EXECUTION_SUCCESS", "matchState": state}
		if on && level != "" {
			m["confidenceLevel"] = level
		}
		return m
	}
	sdp := match(strings.Contains(text, "S1234567D"), "")
	if strings.Contains(text, "S1234567D") {
		sdp["findings"] = []map[string]string{{"infoType": "SINGAPORE_NATIONAL_REGISTRATION_ID_NUMBER", "likelihood": "VERY_LIKELY"}}
	}
	harm := strings.Contains(text, "harm")
	rai := match(harm, "MEDIUM_AND_ABOVE")
	rai["raiFilterTypeResults"] = map[string]any{"dangerous": match(harm, "MEDIUM_AND_ABOVE"), "harassment": match(false, "")}
	results := map[string]any{
		"pi_and_jailbreak": map[string]any{"piAndJailbreakFilterResult": match(strings.Contains(text, "inject"), "HIGH")},
		"sdp":              map[string]any{"sdpFilterResult": map[string]any{"inspectResult": sdp}},
		"malicious_uris":   map[string]any{"maliciousUriFilterResult": match(strings.Contains(text, "evil.example"), "")},
		"rai":              map[string]any{"raiFilterResult": rai},
		"csam":             map[string]any{"csamFilterFilterResult": match(false, "")},
	}
	overall, invocation := "NO_MATCH_FOUND", "SUCCESS"
	for _, k := range []string{"inject", "S1234567D", "evil.example", "harm"} {
		if strings.Contains(text, k) {
			overall = "MATCH_FOUND"
		}
	}
	if strings.Contains(text, "partial") {
		invocation = "PARTIAL"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"sanitizationResult": map[string]any{
		"filterMatchState": overall, "filterResults": results, "invocationResult": invocation}})
}

// Model Armor screens prompts and answers through the template in Singapore: each matched filter is one finding with
// its confidence and info types, never the matched text; a clean text has none; a screening that did not finish, or a
// server error, is ports.ErrScreeningIncomplete or an error that holds no text, so the guardrails fail closed. A name
// that is not a template's is refused. It becomes AI-18's and AI-20's screen when the guardrails wire it in
// (p1-ai-assistant); [live-verify] against real Model Armor in the dev cloud.
// req: platform/local-mode Local AI in replay and live mode
func TestModelArmorScreensPromptsAndAnswers(t *testing.T) {
	fake := &fakeArmor{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "armor-token"})
	a, err := NewModelArmor(srv.URL, armorTemplateName, tokens, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := a.ScreenPrompt(ctx, "inject: the director is S1234567D, see evil.example")
	if err != nil {
		t.Fatal(err)
	}
	want := []ports.ScreenFinding{
		{Filter: ports.ScreenInjection, Detail: "prompt injection or jailbreak", Confidence: ports.ConfidenceHigh},
		{Filter: ports.ScreenSensitive, Detail: "SINGAPORE_NATIONAL_REGISTRATION_ID_NUMBER"},
		{Filter: ports.ScreenMaliciousURL, Detail: "a malicious link"},
	}
	if !reflect.DeepEqual(got.Findings, want) {
		t.Errorf("prompt findings: %+v", got.Findings)
	}
	if fake.paths[0] != "/v1/"+armorTemplateName+":sanitizeUserPrompt" || fake.field != "userPromptData" || fake.auth != "Bearer armor-token" {
		t.Errorf("prompt request: %s, %s, %q", fake.paths[0], fake.field, fake.auth)
	}

	got, err = a.ScreenResponse(ctx, "a harmful answer")
	if err != nil || !reflect.DeepEqual(got.Findings, []ports.ScreenFinding{{Filter: ports.ScreenHarmful, Detail: "dangerous", Confidence: ports.ConfidenceMedium}}) {
		t.Errorf("answer findings: %+v %v", got.Findings, err)
	}
	if fake.paths[1] != "/v1/"+armorTemplateName+":sanitizeModelResponse" || fake.field != "modelResponseData" {
		t.Errorf("answer request: %s, %s", fake.paths[1], fake.field)
	}

	if got, err := a.ScreenPrompt(ctx, "Revenue in October was 5,000.00."); err != nil || got.Flagged() {
		t.Errorf("a clean prompt: %+v %v", got, err)
	}
	if _, err := a.ScreenPrompt(ctx, "partial"); !errors.Is(err, ports.ErrScreeningIncomplete) {
		t.Errorf("a partial screening: %v", err)
	}
	if _, err := a.ScreenResponse(ctx, "fail S1234567D"); err == nil || strings.Contains(err.Error(), "S1234567D") {
		t.Errorf("a server error must fail and hold no text: %v", err)
	}

	if _, err := NewModelArmor("", "projects/x/templates/y", tokens, nil); err == nil {
		t.Error("a name that is not a template's was accepted")
	}
	if b, _ := NewModelArmor("", armorTemplateName, tokens, nil); b.Endpoint != "https://modelarmor.asia-southeast1.rep.googleapis.com" {
		t.Errorf("the regional endpoint: %s", b.Endpoint)
	}
}
