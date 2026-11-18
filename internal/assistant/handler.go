package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The codes of the AI service's own refusals.
const (
	CodeNotTheAPI  = "AI-SERVICE-01" // the call is not from the API
	CodeNoQuestion = "AI-SERVICE-02" // the turn has no token or no question
	CodeUnfinished = "AI-SERVICE-03" // the answer could not be finished
)

// Handler is POST /turns, for the API alone (design D3): a turn's question in, its answer out as server-sent events,
// one per Event, then "error" in place of "done" when it could not finish.
type Handler struct {
	Assistant *Assistant
	Callers   ports.TaskTokens // verifies the API's service tokens, of Class
	Log       *slog.Logger
}

// ServeHTTP implements http.Handler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if h.Callers == nil || token == "" || h.Callers.Verify(token, Class) != nil {
		refuse(w, &Problem{Status: http.StatusUnauthorized, Code: CodeNotTheAPI, Title: "Not the API", Detail: "The call is not from the API.",
			Fix: "Only the API asks the AI service to answer a turn, with its service token."})
		return
	}
	var q Question
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&q); err != nil || q.Token == "" || strings.TrimSpace(q.Text) == "" {
		refuse(w, &Problem{Status: http.StatusUnprocessableEntity, Code: CodeNoQuestion, Title: "No question", Detail: "The turn has no token or no question.",
			Fix: "Send the turn's delegated token and the person's question."})
		return
	}
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	send := func(kind string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	_, err := h.Assistant.Answer(r.Context(), q, func(e Event) error { return send(e.Kind, e) })
	if err == nil {
		return
	}
	var p *Problem
	if !errors.As(err, &p) {
		if h.Log != nil {
			h.Log.ErrorContext(r.Context(), "ai.turn.failed", "error", err.Error())
		}
		p = &Problem{Status: http.StatusInternalServerError, Code: CodeUnfinished, Title: "Not finished", Detail: "The answer could not be finished.",
			Fix: "Ask again in a moment."}
	}
	_ = send("error", p)
}

func refuse(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
