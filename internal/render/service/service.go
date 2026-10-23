// Package service is the render service's one operation (PRD-001 §12.3, rev 0.96; the owner's decision of 23 Oct
// 2026): a private service that compiles a template version and a document's data to PDF for the api (previews and a
// publication's test documents) and the worker (issued documents). It accepts only its callers' tokens, holds no
// database role and no secret, and checks the template again before it lays anything out (FR-TPL-02).
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/render/typst"
)

// Class is the token class the render service accepts: its callers ask for a token of it (ports.TaskTokens).
const Class = "render"

// The limits of a request.
const (
	MaxBody     = 4 << 20   // the request, logo included
	MaxLogo     = 512 << 10 // a PNG
	MaxParallel = 4         // compiles at once in one instance; Cloud Run's concurrency matches it
)

// Request is a document to render: a template version's content, the document's data and its issue time.
type Request struct {
	DocumentType string              `json:"document_type"`
	Content      doctemplate.Content `json:"content"`
	Data         render.Data         `json:"data"`
	Logo         []byte              `json:"logo,omitempty"`  // a PNG, base64 in JSON
	Created      time.Time           `json:"created"`         // the PDF's creation date, so a reprint is the same bytes
	Check        bool                `json:"check,omitempty"` // also find the IRAS particulars in the document
}

// Response is the rendered document.
type Response struct {
	PDF         []byte   `json:"pdf"`
	SHA256      string   `json:"sha256"`
	Particulars []string `json:"particulars,omitempty"` // with check: those the document holds
	Missing     []string `json:"missing,omitempty"`     // with check: those IRAS requires and it lacks
}

// Problem is a refusal: its code says why.
type Problem struct {
	Code    string                `json:"code"`
	Message string                `json:"message"`
	Errors  []doctemplate.Problem `json:"errors,omitempty"`
}

// The render service's codes.
const (
	CodeUnauthorised = "RENDER-01" // no token of the render class
	CodeRequest      = "RENDER-02" // not a request: malformed, too large, or a logo that is not a PNG
	CodeTemplate     = "RENDER-03" // the template breaks its rules (TPL-…)
	CodeFailed       = "RENDER-04" // Typst could not compile the document
	CodeBusy         = "RENDER-05" // every compile slot is taken; try again
)

// Handler serves POST /render.
type Handler struct {
	Compiler *typst.Compiler
	Tokens   ports.TaskTokens
	slots    chan struct{}
}

// New is a handler with MaxParallel compile slots.
func New(c *typst.Compiler, tokens ports.TaskTokens) *Handler {
	return &Handler{Compiler: c, Tokens: tokens, slots: make(chan struct{}, MaxParallel)}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// pngMagic opens every PNG file.
const pngMagic = "\x89PNG\r\n\x1a\n"

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := h.Tokens.Verify(token, Class); err != nil {
		reply(w, http.StatusUnauthorized, Problem{Code: CodeUnauthorised, Message: "the request carries no token of the render service's callers"})
		return
	}
	var in Request
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		reply(w, http.StatusBadRequest, Problem{Code: CodeRequest, Message: "the request is not a render request: " + err.Error()})
		return
	}
	if len(in.Logo) > MaxLogo || (len(in.Logo) > 0 && !strings.HasPrefix(string(in.Logo), pngMagic)) {
		reply(w, http.StatusBadRequest, Problem{Code: CodeRequest, Message: fmt.Sprintf("the logo is not a PNG of at most %d KB", MaxLogo>>10)})
		return
	}
	if in.Created.IsZero() {
		reply(w, http.StatusBadRequest, Problem{Code: CodeRequest, Message: "the request names no creation time"})
		return
	}
	if ps := doctemplate.Check(doctemplate.KindDocument, in.DocumentType, in.Content); len(ps) > 0 {
		reply(w, http.StatusUnprocessableEntity, Problem{Code: CodeTemplate, Message: "the template breaks its rules", Errors: ps})
		return
	}
	in.Data.Logo = len(in.Logo) > 0
	layout, err := render.Build(in.DocumentType, in.Content, in.Data)
	if err != nil {
		reply(w, http.StatusUnprocessableEntity, Problem{Code: CodeRequest, Message: err.Error()})
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-r.Context().Done():
		return
	default:
		w.Header().Set("Retry-After", "1")
		reply(w, http.StatusServiceUnavailable, Problem{Code: CodeBusy, Message: "every compile slot is taken"})
		return
	}
	out, err := h.render(r.Context(), typst.Input{Layout: layout, Logo: in.Logo, Created: in.Created.UTC()}, in)
	if err != nil {
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}
		reply(w, http.StatusInternalServerError, Problem{Code: CodeFailed, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, out)
}

func (h *Handler) render(ctx context.Context, x typst.Input, in Request) (Response, error) {
	pdf, err := h.Compiler.PDF(ctx, x)
	if err != nil {
		return Response{}, err
	}
	sum := sha256.Sum256(pdf)
	out := Response{PDF: pdf, SHA256: hex.EncodeToString(sum[:])}
	if in.Check {
		if out.Particulars, err = h.Compiler.Particulars(ctx, x); err != nil {
			return Response{}, err
		}
		out.Missing = typst.Missing(render.Required(in.DocumentType, in.Data), out.Particulars)
	}
	return out, nil
}
