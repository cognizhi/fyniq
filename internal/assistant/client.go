package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cognizhi/fyniq/internal/egress"
)

// ServiceTokenHeader carries the AI service's own token to the API, beside the turn's delegated token (design D4).
const ServiceTokenHeader = "X-Fyniq-Service-Token" // #nosec G101 -- a header's name, not a credential

// maxResult is the most of an answer from the API the AI service reads.
const maxResult = 8 << 20

// Client is the Backend over HTTP: the API's internal operations at URL, such as http://api:8080/internal/v1, called with
// the AI service's own token and the turn's delegated token.
type Client struct {
	URL          string
	ServiceToken func(ctx context.Context) (string, error)
	HTTP         *http.Client // through the egress guard (cmd/ai); egress.Public when nil
}

var _ Backend = (*Client)(nil)

// CallModel implements Backend.
func (c *Client) CallModel(ctx context.Context, token string, call ModelCall) (ModelResult, error) {
	var out ModelResult
	err := c.post(ctx, "/model-calls", token, call, &out)
	return out, err
}

// CallTool implements Backend.
func (c *Client) CallTool(ctx context.Context, token string, tool string, input json.RawMessage) (ToolResult, error) {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	var out ToolResult
	err := c.post(ctx, "/tool-calls", token, map[string]any{"tool": tool, "input": input}, &out)
	return out, err
}

// post sends body to the internal operation at path and reads its answer into out; a refusal is the API's *Problem.
func (c *Client) post(ctx context.Context, path, token string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	service, err := c.ServiceToken(ctx)
	if err != nil {
		return fmt.Errorf("assistant: the AI service's token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(ServiceTokenHeader, service)
	h := c.HTTP
	if h == nil {
		h = egress.Public()
	}
	resp, err := h.Do(req)
	if err != nil {
		return fmt.Errorf("assistant: the API did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResult))
	if err != nil {
		return fmt.Errorf("assistant: the API's answer: %w", err)
	}
	if resp.StatusCode >= 300 {
		p := &Problem{Status: resp.StatusCode}
		if json.Unmarshal(data, p) != nil || p.Code == "" {
			return fmt.Errorf("assistant: the API answered %d", resp.StatusCode)
		}
		return p
	}
	return json.Unmarshal(data, out)
}
