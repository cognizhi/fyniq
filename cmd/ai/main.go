// Command ai is the AI service (PRD-001 §9.3, golden rule 5; p1-ai-assistant design D3): POST /turns answers an
// assistant's turn for the API alone, streaming its steps. It holds no database role, no provider key and no write
// credential: every read and every model call goes through the API's internal operations with the turn's delegated
// token, as the person the turn is for.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
)

func main() {
	os.Exit(app.Serve("ai", func(s *httpserver.Server, deps app.Deps) error {
		prompt, err := assistant.DefaultPrompt(assistant.Agent)
		if err != nil {
			return err
		}
		callers, backend, err := wire(deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ai: answering no turns:", err)
			return nil
		}
		s.Handle("POST /turns", assistant.Handler{Assistant: &assistant.Assistant{Backend: backend, Prompt: prompt, Clock: deps.Clock}, Callers: callers,
			Log: logging.New(os.Stderr, slog.LevelInfo)})
		return nil
	}))
}
