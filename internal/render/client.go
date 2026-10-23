package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
)

// Class is the token class the render service accepts: its callers ask for a token of it (ports.TaskTokens).
const Class = "render"

// Request is a document to render: a template version's content, the document's data and its issue time.
type Request struct {
	DocumentType string              `json:"document_type"`
	Content      doctemplate.Content `json:"content"`
	Data         Data                `json:"data"`
	Logo         []byte              `json:"logo,omitempty"`  // a PNG, base64 in JSON
	Created      time.Time           `json:"created"`         // the PDF's creation date, so a reprint is the same bytes
	Check        bool                `json:"check,omitempty"` // also find the IRAS particulars in the document
	Pages        bool                `json:"pages,omitempty"` // also each page as a PNG, for a preview
}

// Response is the rendered document.
type Response struct {
	PDF         []byte   `json:"pdf"`
	SHA256      string   `json:"sha256"`
	Particulars []string `json:"particulars,omitempty"` // with check: those the document holds
	Missing     []string `json:"missing,omitempty"`     // with check: those IRAS requires and it lacks
	Pages       [][]byte `json:"pages,omitempty"`       // with pages: each page as a PNG
}

// Problem is the render service's refusal: its code says why.
type Problem struct {
	Status  int                   `json:"-"`
	Code    string                `json:"code"`
	Message string                `json:"message"`
	Errors  []doctemplate.Problem `json:"errors,omitempty"`
}

func (p *Problem) Error() string { return fmt.Sprintf("render: %s %s", p.Code, p.Message) }

// The render service's codes.
const (
	CodeUnauthorised = "RENDER-01" // no token of the render class
	CodeRequest      = "RENDER-02" // not a request: malformed, too large, or a logo that is not a PNG
	CodeTemplate     = "RENDER-03" // the template breaks its rules (TPL-…)
	CodeFailed       = "RENDER-04" // Typst could not compile the document
	CodeBusy         = "RENDER-05" // every compile slot is taken; try again
)

// Renderer renders documents: the render service, for the api and the worker.
type Renderer interface {
	Render(ctx context.Context, r Request) (Response, error)
}

// Client calls the render service at URL with a token of its callers (PRD-001 §12.3, rev 0.96): locally a task token
// of the render class, in Google Cloud an ID token of the caller's own account for the service's audience.
type Client struct {
	URL   string
	Token func(ctx context.Context) (string, error)
	HTTP  *http.Client // http.DefaultClient when nil
}

var _ Renderer = (*Client)(nil)

// Render implements Renderer. A refusal is a *Problem.
func (c *Client) Render(ctx context.Context, r Request) (Response, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return Response{}, err
	}
	token, err := c.Token(ctx)
	if err != nil {
		return Response{}, fmt.Errorf("render: a token for the render service: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+"/render", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	resp, err := h.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("render: the render service did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode != http.StatusOK {
		p := &Problem{Status: resp.StatusCode}
		if json.Unmarshal(raw, p) != nil || p.Code == "" {
			p.Code, p.Message = CodeFailed, "the render service answered "+resp.Status
		}
		return Response{}, p
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("render: the render service's answer: %w", err)
	}
	return out, nil
}
