// Package convertservice is the private converter service's one operation and its client (FR-DOC-08, SEC-24; PRD-001 rev
// 0.292, the owner's decision of 18 Nov 2026). Converting a HEIC photo or rendering a PDF runs a parser of a hostile
// format, so it never runs in the API or the worker: a service of its own takes the file, converts it in its sandbox and
// answers the picture. Like the scanner service it accepts only its callers' tokens (the api's and the worker's), holds no
// database role and no secret, and keeps nothing of the file. Its engine is a ports.Converter beside it
// (cmd/converter, [live-verify]: no engine is in the pinned toolchain yet).
package convertservice

import (
	"bytes"
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

// Class is the task-token class of the converter service's callers.
const Class = "converter"

// MaxBody is the largest file the service converts: Cloud Run's limit on an HTTP/1.1 request, above FR-DOC-08's 25 MB.
const MaxBody = 32 << 20

// MaxPicture is the largest picture the client accepts back.
const MaxPicture = 64 << 20

// The service's refusals, as their codes.
const (
	CodeUnauthorised   = "CONV-01"
	CodeTooLarge       = "CONV-02"
	CodeFailed         = "CONV-03" // the engine was not available
	CodeNotConvertible = "CONV-04" // the file is the problem
	CodeKind           = "CONV-05"
)

// Problem is a refusal.
type Problem struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (p *Problem) Error() string {
	return fmt.Sprintf("the converter service: %s (%s)", p.Message, p.Code)
}

// Handler serves POST /convert?kind=heic_jpeg|preview_png: the file is the body; the answer, the picture.
type Handler struct {
	Engine ports.Converter // the converter beside it
	Tokens ports.TaskTokens
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
		reply(w, http.StatusUnauthorized, Problem{Code: CodeUnauthorised, Message: "the request carries no token of the converter service's callers"})
		return
	}
	kind := ports.ConvertKind(r.URL.Query().Get("kind"))
	if kind != ports.ConvertHEICToJPEG && kind != ports.ConvertPreviewPNG {
		reply(w, http.StatusUnprocessableEntity, Problem{Code: CodeKind, Message: "kind is heic_jpeg or preview_png"})
		return
	}
	if r.ContentLength > MaxBody {
		reply(w, http.StatusRequestEntityTooLarge, Problem{Code: CodeTooLarge, Message: fmt.Sprintf("a file of at most %d bytes", MaxBody)})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		reply(w, http.StatusRequestEntityTooLarge, Problem{Code: CodeTooLarge, Message: fmt.Sprintf("a file of at most %d bytes", MaxBody)})
		return
	}
	if err != nil {
		reply(w, http.StatusBadRequest, Problem{Code: CodeFailed, Message: "the file could not be read in full"})
		return
	}
	out, err := h.Engine.Convert(r.Context(), kind, data)
	switch {
	case errors.Is(err, ports.ErrNotConvertible):
		reply(w, http.StatusUnprocessableEntity, Problem{Code: CodeNotConvertible, Message: "the file cannot be converted"})
	case err != nil: // the answer names no part of the file
		reply(w, http.StatusBadGateway, Problem{Code: CodeFailed, Message: "the converter could not convert the file"})
	default:
		w.Header().Set("Content-Type", out.MediaType)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out.Data)
	}
}

// Client is ports.Converter through the converter service, as its callers use it.
type Client struct {
	URL   string
	Token func(ctx context.Context) (string, error)
	HTTP  *http.Client // through the egress guard (wiring); egress.Public when nil
}

var _ ports.Converter = (*Client)(nil)

// Convert implements ports.Converter. A file the service reports as the problem is ports.ErrNotConvertible; anything
// else that is not a picture is an error, so a file is never passed because the service could not decide.
func (c *Client) Convert(ctx context.Context, kind ports.ConvertKind, data []byte) (ports.Converted, error) {
	token, err := c.Token(ctx)
	if err != nil {
		return ports.Converted{}, fmt.Errorf("a token for the converter service: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+"/convert?kind="+string(kind), bytes.NewReader(data))
	if err != nil {
		return ports.Converted{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	h := c.HTTP
	if h == nil {
		h = egress.Public()
	}
	resp, err := h.Do(req)
	if err != nil {
		return ports.Converted{}, fmt.Errorf("the converter service did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxPicture+1))
	if err != nil {
		return ports.Converted{}, err
	}
	if resp.StatusCode != http.StatusOK {
		p := &Problem{Status: resp.StatusCode}
		if json.Unmarshal(raw, p) != nil || p.Code == "" {
			p.Code, p.Message = CodeFailed, "the converter service answered "+resp.Status
		}
		if p.Code == CodeNotConvertible {
			return ports.Converted{}, ports.ErrNotConvertible
		}
		return ports.Converted{}, p
	}
	if len(raw) > MaxPicture {
		return ports.Converted{}, errors.New("the converter service's picture is too large")
	}
	mt := resp.Header.Get("Content-Type")
	if mt != "image/jpeg" && mt != "image/png" {
		return ports.Converted{}, fmt.Errorf("the converter service's answer is not a picture (%q)", mt)
	}
	return ports.Converted{MediaType: mt, Data: raw}, nil
}
