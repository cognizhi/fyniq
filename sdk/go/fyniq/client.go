// Package fyniq is the Go client of the Fyniq /v1 API (PRD-001 FR-INT-17): the operations a client's scopes can reach, generated from
// contracts/openapi/fyniq.v1.yaml (operations_gen.go), with the conventions of §8.18.1 built in:
//
//   - an Idempotency-Key on every state-changing call that takes one, made once per call and kept across its retries (FR-INT-08);
//   - a retry of a 429 (any call: a refused request saved nothing) or of a read's 503, after the Retry-After the API names when that is short (FR-INT-16);
//   - the RFC 9457 problem of a refusal as a *Problem error with its stable code and its fix (FR-INT-07);
//   - the ETag of a read for If-Match (FR-INT-09), Prefer: return=preview (FR-INT-14) and cursor-paginated lists (FR-INT-10).
//
// It holds no credential of its own: the token is the caller's (an integration client's or a personal access token's, FR-INT-02), and it is sent to the
// base URL only. Money stays a decimal string in the JSON the API sends (ENG-10): Decode into your own types with string amounts, never float64.
// It imports nothing outside the standard library.
package fyniq

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Operation is what the contract says about one operation.
type Operation struct {
	ID, Method, Path, Summary string
	PathParams, Query, Scopes []string
	Body                      bool
	Paged, Preview            bool
	IdempotencyKey, IfMatch   bool
}

// Client calls the API.
type Client struct {
	// BaseURL is the API's origin, such as https://app.example (paths already start with /v1).
	BaseURL string
	// Token returns the access token, afresh at each call when it can.
	Token func(ctx context.Context) (string, error)
	// HTTP is the client to use; http.DefaultClient when nil. It should not follow redirects to another host.
	HTTP *http.Client
	// MaxRetries is how many times a 429, or a read's 503, is retried: 2 when 0; negative never retries.
	MaxRetries int
	// MaxWait is the longest the client waits to retry: 10 seconds when 0. A longer Retry-After is returned to the caller.
	MaxWait time.Duration
	// NoIdempotencyKeys turns off the key the client makes for a write that takes one.
	NoIdempotencyKeys bool
	// Sleep waits; the timer by default. For tests.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Response is an answer, whatever its status.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Decode reads the JSON body into v.
func (r *Response) Decode(v any) error { return json.Unmarshal(r.Body, v) }

// ETag is the version of the record the answer shows, for If-Match (WithIfMatch).
func (r *Response) ETag() string { return r.Header.Get("ETag") }

// Previewed says whether the call was a preview: nothing was saved (FR-INT-14).
func (r *Response) Previewed() bool {
	return strings.Contains(r.Header.Get("Preference-Applied"), "return=preview")
}

// RateLimit is the RateLimit header: what is left of the client's limit and the seconds until the window ends (FR-INT-16).
func (r *Response) RateLimit() string { return r.Header.Get("RateLimit") }

// Problem is the RFC 9457 problem details of a refusal, with the API's stable code and the plain-words fix (FR-INT-07). It is the error of a call the API refused.
type Problem struct {
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Status    int            `json:"status"`
	Detail    string         `json:"detail"`
	Code      string         `json:"code"`
	Fix       string         `json:"fix"`
	Instance  string         `json:"instance"`
	RequestID string         `json:"request_id"`
	Errors    []FieldProblem `json:"errors"`
}

// FieldProblem is one invalid field of a refusal: all of them come back at once.
type FieldProblem struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

func (p *Problem) Error() string {
	return fmt.Sprintf("fyniq: %d %s: %s (%s)", p.Status, p.Code, p.Detail, p.Fix)
}

type callOptions struct {
	query          url.Values
	ifMatch        string
	preview        bool
	idempotencyKey string
	headers        http.Header
}

// CallOption adjusts one call.
type CallOption func(*callOptions)

// WithQuery adds a query parameter, such as page_size or a filter.
func WithQuery(name, value string) CallOption {
	return func(o *callOptions) {
		if o.query == nil {
			o.query = url.Values{}
		}
		o.query.Add(name, value)
	}
}

// WithIfMatch sends the ETag of the version being changed (FR-INT-09).
func WithIfMatch(etag string) CallOption { return func(o *callOptions) { o.ifMatch = etag } }

// WithPreview asks the write to check itself and save nothing (FR-INT-14).
func WithPreview() CallOption { return func(o *callOptions) { o.preview = true } }

// WithIdempotencyKey sends your own key instead of the one the client makes.
func WithIdempotencyKey(key string) CallOption {
	return func(o *callOptions) { o.idempotencyKey = key }
}

// NewKey is an Idempotency-Key: send the same one when a call is retried, so it runs at most once (FR-INT-08).
func NewKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80 // a version 4 UUID
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// Do calls an operation with its path arguments in the path's order, a body (nil for none) and options. A refusal is a *Problem error and the Response too.
func (c *Client) Do(ctx context.Context, op Operation, pathArgs []string, body any, opts []CallOption) (*Response, error) {
	var o callOptions
	for _, f := range opts {
		f(&o)
	}
	if len(pathArgs) != len(op.PathParams) {
		return nil, fmt.Errorf("fyniq: %s takes %d path arguments, got %d", op.ID, len(op.PathParams), len(pathArgs))
	}
	path := op.Path
	for i, name := range op.PathParams {
		path = strings.Replace(path, "{"+name+"}", url.PathEscape(pathArgs[i]), 1)
	}
	target := strings.TrimRight(c.BaseURL, "/") + path
	if len(o.query) > 0 {
		target += "?" + o.query.Encode()
	}
	var payload []byte
	if body != nil {
		switch b := body.(type) {
		case json.RawMessage:
			payload = b
		case []byte:
			payload = b
		default:
			var err error
			if payload, err = json.Marshal(body); err != nil {
				return nil, fmt.Errorf("fyniq: the body cannot be written as JSON: %w", err)
			}
		}
	}
	write := op.Method != http.MethodGet
	key := o.idempotencyKey
	if write && key == "" && op.IdempotencyKey && !c.NoIdempotencyKeys && !o.preview {
		key = NewKey() // once for the call: every retry sends the same one
	}
	retries := c.MaxRetries
	switch {
	case retries == 0:
		retries = 2
	case retries < 0:
		retries = 0
	}
	maxWait := c.MaxWait
	if maxWait == 0 {
		maxWait = 10 * time.Second
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, op.Method, target, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.Token != nil {
			tok, err := c.Token(ctx)
			if err != nil {
				return nil, fmt.Errorf("fyniq: the token: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if o.ifMatch != "" {
			req.Header.Set("If-Match", o.ifMatch)
		}
		if o.preview {
			req.Header.Set("Prefer", "return=preview")
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		answer, err := readAll(resp)
		if err != nil {
			return nil, err
		}
		retry := resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusServiceUnavailable && !write)
		if wait, ok := retryAfter(resp.Header); retry && ok && attempt < retries && wait <= maxWait {
			if err := c.sleep(ctx, wait); err != nil {
				return answer, err
			}
			continue
		}
		if resp.StatusCode >= 400 {
			p := &Problem{Status: resp.StatusCode}
			if json.Unmarshal(answer.Body, p) != nil || p.Code == "" {
				p = &Problem{Status: resp.StatusCode, Code: "UNKNOWN", Detail: strings.TrimSpace(string(answer.Body)), Title: http.StatusText(resp.StatusCode)}
			}
			return answer, p
		}
		return answer, nil
	}
}

func readAll(resp *http.Response) (*Response, error) {
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: b}, err
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryAfter is the wait a 429 or 503 asks for in Retry-After (seconds or an HTTP date).
func retryAfter(h http.Header) (time.Duration, bool) {
	raw := strings.TrimSpace(h.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return time.Duration(n) * time.Second, true
	}
	// An HTTP date is read against the answer's own Date header, so the client needs no clock of its own.
	if t, err := http.ParseTime(raw); err == nil {
		if now, err := http.ParseTime(h.Get("Date")); err == nil {
			return max(t.Sub(now), 0), true
		}
	}
	return 0, false
}

// Paginate walks a cursor-paginated list: fetch is called with the cursor ("" for the first page), each item of every page's items is handed to each, and the walk ends at the
// last page (no next_cursor) or when each returns an error.
func Paginate(ctx context.Context, fetch func(ctx context.Context, cursor string) (*Response, error), each func(item json.RawMessage) error) error {
	cursor := ""
	for {
		r, err := fetch(ctx, cursor)
		if err != nil {
			return err
		}
		var page struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor string            `json:"next_cursor"`
		}
		if err := r.Decode(&page); err != nil {
			return errors.New("fyniq: the answer is not a page of items")
		}
		for _, it := range page.Items {
			if err := each(it); err != nil {
				return err
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}
