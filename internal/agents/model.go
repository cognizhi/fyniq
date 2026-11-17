package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ModelClass is the class of the worker's service tokens for the API's one internal operation it calls, a run's model
// step (PRD-001 rev 0.281: the worker asks the API).
const ModelClass = "agent_runs"

// ServiceTokenHeader carries the worker's service token to the API, as the AI service's carries its own.
const ServiceTokenHeader = "X-Fyniq-Service-Token" // #nosec G101 -- a header's name, not a credential

// ModelStepRequest is what the worker sends the API for a run's model step: the run's organisation and its findings.
type ModelStepRequest struct {
	Organisation uuid.UUID `json:"organisation_id"`
	Findings     []Finding `json:"findings"`
}

// ModelStepOutcome is the API's answer: the model's words as written, grounded, the model, what the step used and how
// many proposals it made, and how it ended.
type ModelStepOutcome struct {
	Model     string `json:"model"`
	Text      string `json:"text"`
	Tokens    int    `json:"tokens"`
	ToolCalls int    `json:"tool_calls"`
	Proposals int    `json:"proposals"`
	Stop      string `json:"stop"`
}

// APIModel is the run's model step through the API (design D5 step 5): POST <URL>/agent-runs/{id}/model with the
// worker's own service token. The API checks the run, issues its delegated token and asks the AI service; the worker
// holds no delegated token, no provider key and no route to the AI service.
type APIModel struct {
	URL   string // the API's internal operations, such as http://api:8080/internal/v1
	Token func(ctx context.Context) (string, error)
	HTTP  *http.Client
}

// Step implements ModelStep.
func (m APIModel) Step(ctx context.Context, org uuid.UUID, run storage.AgentRun, findings []Finding) (ModelOutcome, error) {
	body, err := json.Marshal(ModelStepRequest{Organisation: org, Findings: findings})
	if err != nil {
		return ModelOutcome{}, err
	}
	token, err := m.Token(ctx)
	if err != nil {
		return ModelOutcome{}, fmt.Errorf("the worker's token for the API: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(m.URL, "/")+"/agent-runs/"+run.ID.String()+"/model", bytes.NewReader(body))
	if err != nil {
		return ModelOutcome{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ServiceTokenHeader, token)
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return ModelOutcome{}, fmt.Errorf("the API did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ModelOutcome{}, err
	}
	if resp.StatusCode != http.StatusOK {
		var p struct{ Code, Detail string }
		if json.Unmarshal(data, &p) == nil && p.Code != "" {
			return ModelOutcome{}, fmt.Errorf("%s (%s)", strings.TrimSuffix(p.Detail, "."), p.Code)
		}
		return ModelOutcome{}, fmt.Errorf("the API answered %d", resp.StatusCode)
	}
	var out ModelStepOutcome
	if err := json.Unmarshal(data, &out); err != nil {
		return ModelOutcome{}, fmt.Errorf("the API's answer: %w", err)
	}
	return ModelOutcome{Model: out.Model, Text: out.Text, Tokens: out.Tokens, ToolCalls: out.ToolCalls, Proposals: out.Proposals}, nil
}
