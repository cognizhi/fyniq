// Package scanservice is the private scanner service's one operation and its client (FR-DOC-08, SEC-24; PRD-001 rev
// 0.247, the owner's decision of 11 Nov 2026). clamd speaks raw TCP, which a Cloud Run service does not accept, so an
// HTTP front, in the same Cloud Run service as clamd's sidecar, takes a file and asks clamd. It accepts only its
// callers' tokens (the api's and the worker's), holds no database role and no secret, and keeps nothing of the file.
package scanservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Class is the task-token class of the scanner service's callers.
const Class = "scanner"

// MaxBody is the largest file the service scans: Cloud Run's limit on an HTTP/1.1 request, above FR-DOC-08's 25 MB.
const MaxBody = 32 << 20

// The service's refusals, as their codes.
const (
	CodeUnauthorised = "SCAN-01"
	CodeTooLarge     = "SCAN-02"
	CodeFailed       = "SCAN-03"
)

// Verdict is clamd's answer for one file.
type Verdict struct {
	Clean     bool   `json:"clean"`
	Signature string `json:"signature,omitempty"` // what was found, when not clean
}

// Problem is a refusal.
type Problem struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (p *Problem) Error() string {
	return fmt.Sprintf("the scanner service: %s (%s)", p.Message, p.Code)
}

// Handler serves POST /scan: the file is the body; the answer, its Verdict.
type Handler struct {
	Scanner ports.Scanner // clamd beside it
	Tokens  ports.TaskTokens
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := h.Tokens.Verify(token, Class); err != nil {
		reply(w, http.StatusUnauthorized, Problem{Code: CodeUnauthorised, Message: "the request carries no token of the scanner service's callers"})
		return
	}
	if r.ContentLength > MaxBody {
		reply(w, http.StatusRequestEntityTooLarge, Problem{Code: CodeTooLarge, Message: fmt.Sprintf("a file of at most %d bytes", MaxBody)})
		return
	}
	res, err := h.Scanner.Scan(r.Context(), http.MaxBytesReader(w, r.Body, MaxBody))
	var big *http.MaxBytesError
	switch {
	case errors.As(err, &big):
		reply(w, http.StatusRequestEntityTooLarge, Problem{Code: CodeTooLarge, Message: fmt.Sprintf("a file of at most %d bytes", MaxBody)})
	case err != nil:
		// Never clean when clamd could not decide; the answer names no part of the file.
		reply(w, http.StatusBadGateway, Problem{Code: CodeFailed, Message: "clamd could not scan the file"})
	default:
		reply(w, http.StatusOK, Verdict{Clean: res.Clean, Signature: res.Signature})
	}
}

// Client is ports.Scanner through the scanner service, as its callers use it.
type Client struct {
	URL   string
	Token func(ctx context.Context) (string, error)
	HTTP  *http.Client // through the egress guard (wiring); egress.Public when nil
}

var _ ports.Scanner = (*Client)(nil)

// Scan implements ports.Scanner. Anything but a verdict is an error, so a file is never passed because the service could
// not decide.
func (c *Client) Scan(ctx context.Context, r io.Reader) (ports.ScanResult, error) {
	token, err := c.Token(ctx)
	if err != nil {
		return ports.ScanResult{}, fmt.Errorf("a token for the scanner service: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+"/scan", r)
	if err != nil {
		return ports.ScanResult{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	h := c.HTTP
	if h == nil {
		h = egress.Public()
	}
	resp, err := h.Do(req)
	if err != nil {
		return ports.ScanResult{}, fmt.Errorf("the scanner service did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return ports.ScanResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		p := &Problem{Status: resp.StatusCode}
		if json.Unmarshal(raw, p) != nil || p.Code == "" {
			p.Code, p.Message = CodeFailed, "the scanner service answered "+resp.Status
		}
		return ports.ScanResult{}, p
	}
	var v Verdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return ports.ScanResult{}, fmt.Errorf("the scanner service's answer is not a verdict: %w", err)
	}
	return ports.ScanResult{Clean: v.Clean, Signature: v.Signature}, nil
}
