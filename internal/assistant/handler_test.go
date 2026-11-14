package assistant_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
)

// callers accepts one token, of one class.
type callers struct{}

func (callers) Token(string) (string, error) { return "", errors.New("the API mints its own") }
func (callers) Verify(token, class string) error {
	if token == "the-api" && class == assistant.Class {
		return nil
	}
	return errors.New("not the API")
}

// POST /turns answers for the API alone (design D3): without the API's service token, or without a turn's token and a
// question, nothing is answered; a turn streams its steps as server-sent events and ends with done, or with error in
// the API's words when it could not finish.
// req: AI-P2
// asvs: V8.2.1
func TestTurnsAreTheAPIsAlone(t *testing.T) {
	b := &api{answers: []ports.ModelResponse{text("Revenue was in the fact cited.")}}
	h := assistant.Handler{Assistant: &assistant.Assistant{Backend: b, Prompt: prompt(t)}, Callers: callers{}}
	post := func(token string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/turns", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	question := map[string]any{"token": "fyai_turn", "question": "What was revenue?"}
	for _, token := range []string{"", "someone-else"} {
		if w := post(token, question); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), assistant.CodeNotTheAPI) || len(b.calls) != 0 {
			t.Errorf("token %q: %d %s", token, w.Code, w.Body)
		}
	}
	if w := post("the-api", map[string]any{"question": "What was revenue?"}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("no turn's token: %d", w.Code)
	}
	w := post("the-api", question)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(w.Body.String(), "event: status") ||
		!strings.Contains(w.Body.String(), "event: done") || b.tokens[0] != "fyai_turn" {
		t.Errorf("a turn: %d %s", w.Code, w.Body)
	}
	b.refuse = map[string]*assistant.Problem{"model": {Status: 409, Code: "AI-ROUTE-05", Detail: "The AI is switched off."}}
	if w := post("the-api", question); !strings.Contains(w.Body.String(), "event: error") || !strings.Contains(w.Body.String(), "AI-ROUTE-05") ||
		strings.Contains(w.Body.String(), "event: done") {
		t.Errorf("the AI switched off: %s", w.Body)
	}
}
