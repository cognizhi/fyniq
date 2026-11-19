package kbcompile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// ServiceTokenHeader carries the compiler's service token to the API.
const ServiceTokenHeader = "X-Fyniq-Service-Token" // #nosec G101 -- a header's name, not a credential

// Class is the class of the compiler's service tokens: the API's four compiler operations and nothing else (AI-57).
const Class = "kb_compiler"

// APIClient is Client over the API's internal operations (design D7): the compiler's service token, the API's internal URL, and
// nothing else. It holds no database credential and no provider key.
type APIClient struct {
	URL   string // the API's internal operations, such as http://api:8080/internal/v1
	Token func(ctx context.Context) (string, error)
	HTTP  *http.Client
}

var _ Client = APIClient{}

func (c APIClient) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	token, err := c.Token(ctx)
	if err != nil {
		return fmt.Errorf("the compiler's token for the API: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+"/kb-compiler/"+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ServiceTokenHeader, token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("the API did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var p struct {
			Code, Detail string
			Errors       []struct{ Field, Message string }
		}
		if json.Unmarshal(data, &p) == nil && p.Code != "" {
			msg := strings.TrimSuffix(p.Detail, ".")
			for _, e := range p.Errors {
				msg += "; " + e.Field + " " + e.Message
			}
			return fmt.Errorf("%s (%s)", msg, p.Code)
		}
		return fmt.Errorf("the API answered %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

// Work implements Client.
func (c APIClient) Work(ctx context.Context, org uuid.UUID) ([]Item, error) {
	var out struct {
		Items []Item `json:"items"`
	}
	return out.Items, c.post(ctx, "work", map[string]any{"organisation_id": org}, &out)
}

// Compile implements Client.
func (c APIClient) Compile(ctx context.Context, org uuid.UUID, note string, version int, feedback []string) (Draft, error) {
	var out Draft
	if feedback == nil {
		feedback = []string{}
	}
	return out, c.post(ctx, "compile", map[string]any{"organisation_id": org, "note_key": note, "note_version": version, "feedback": feedback}, &out)
}

// SubmitDraft implements Client.
func (c APIClient) SubmitDraft(ctx context.Context, org uuid.UUID, note string, version int, d Draft) error {
	var out struct{}
	if d.Aliases == nil {
		d.Aliases = []string{}
	}
	return c.post(ctx, "drafts", map[string]any{"organisation_id": org, "note_key": note, "note_version": version, "draft": d}, &out)
}

// RaiseFinding implements Client.
func (c APIClient) RaiseFinding(ctx context.Context, org uuid.UUID, f Finding) error {
	var out struct{}
	return c.post(ctx, "findings", map[string]any{"organisation_id": org, "kind": f.Kind, "page_key": f.PageKey, "version": f.Version, "detail": f.Detail}, &out)
}
