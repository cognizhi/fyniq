// Package service is the render service's one operation (PRD-001 §12.3, rev 0.96; the owner's decision of 23 Oct
// 2026): a private service that compiles a template version and a document's data to PDF for the api (previews and a
// publication's test documents) and the worker (issued documents), and a report the api laid out, such as a financial
// statement (FR-RPT-05); and that rasterises the organisation's logo for documents and emails (FR-ORG-09). It accepts
// only its callers' tokens, holds no database role and no secret, and checks the template, the report's bounds or the
// logo again before it lays anything out (FR-TPL-02, G4-099).
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

	"github.com/cognizhi/fyniq/internal/domain/brand"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/render/typst"
)

// The limits of a request.
const (
	MaxBody     = 4 << 20   // the request, logo included
	MaxLogo     = 512 << 10 // a PNG
	MaxParallel = 4         // compiles at once in one instance; Cloud Run's concurrency matches it
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
	if err := h.Tokens.Verify(token, render.Class); err != nil {
		reply(w, http.StatusUnauthorized, render.Problem{Code: render.CodeUnauthorised, Message: "the request carries no token of the render service's callers"})
		return
	}
	var in render.Request
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		reply(w, http.StatusBadRequest, render.Problem{Code: render.CodeRequest, Message: "the request is not a render request: " + err.Error()})
		return
	}
	if len(in.Raster) > 0 {
		h.raster(w, r, in)
		return
	}
	if len(in.Logo) > MaxLogo || (len(in.Logo) > 0 && !strings.HasPrefix(string(in.Logo), pngMagic)) {
		reply(w, http.StatusBadRequest, render.Problem{Code: render.CodeRequest, Message: fmt.Sprintf("the logo is not a PNG of at most %d KB", MaxLogo>>10)})
		return
	}
	if in.Created.IsZero() {
		reply(w, http.StatusBadRequest, render.Problem{Code: render.CodeRequest, Message: "the request names no creation time"})
		return
	}
	x := typst.Input{Logo: in.Logo, Created: in.Created.UTC()}
	if in.Report != nil {
		// A report is laid out by its caller: it names no document type, and has no particulars to find.
		if in.DocumentType != "" || len(in.Logo) > 0 || in.Check {
			reply(w, http.StatusBadRequest, render.Problem{Code: render.CodeRequest, Message: "a report names no document type, logo or particulars check"})
			return
		}
		if err := in.Report.Check(); err != nil {
			reply(w, http.StatusUnprocessableEntity, render.Problem{Code: render.CodeRequest, Message: err.Error()})
			return
		}
		x.Report = in.Report
	} else {
		if ps := doctemplate.Check(doctemplate.KindDocument, in.DocumentType, in.Content); len(ps) > 0 {
			reply(w, http.StatusUnprocessableEntity, render.Problem{Code: render.CodeTemplate, Message: "the template breaks its rules", Errors: ps})
			return
		}
		in.Data.Logo = len(in.Logo) > 0
		layout, err := render.Build(in.DocumentType, in.Content, in.Data)
		if err != nil {
			reply(w, http.StatusUnprocessableEntity, render.Problem{Code: render.CodeRequest, Message: err.Error()})
			return
		}
		x.Layout = layout
	}
	done, ok := h.slot(w, r)
	if !ok {
		return
	}
	defer done()
	out, err := h.render(r.Context(), x, in)
	if err != nil {
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}
		reply(w, http.StatusInternalServerError, render.Problem{Code: render.CodeFailed, Message: err.Error()})
		return
	}
	reply(w, http.StatusOK, out)
}

// slot takes one of the compile slots, answering 503 when every one is taken; done gives it back.
func (h *Handler) slot(w http.ResponseWriter, r *http.Request) (done func(), ok bool) {
	select {
	case h.slots <- struct{}{}:
		return func() { <-h.slots }, true
	case <-r.Context().Done():
		return nil, false
	default:
		w.Header().Set("Retry-After", "1")
		reply(w, http.StatusServiceUnavailable, render.Problem{Code: render.CodeBusy, Message: "every compile slot is taken"})
		return nil, false
	}
}

// raster draws the logo in.Raster as the PNG documents and emails show (FR-ORG-09): the logo alone, made safe again as
// the brand makes it (an SVG through the sanitiser, a PNG decoded and written again), and the PNG no larger than a
// document's logo may be.
func (h *Handler) raster(w http.ResponseWriter, r *http.Request, in render.Request) {
	if in.DocumentType != "" || in.Report != nil || len(in.Logo) > 0 || in.Check || in.Pages {
		reply(w, http.StatusBadRequest, render.Problem{Code: render.CodeRequest, Message: "a logo to rasterise comes alone, with no document, report or logo"})
		return
	}
	clean, err := brand.Check(brand.Light, in.Raster)
	if err != nil {
		reply(w, http.StatusUnprocessableEntity, render.Problem{Code: render.CodeLogo, Message: err.Error()})
		return
	}
	done, ok := h.slot(w, r)
	if !ok {
		return
	}
	defer done()
	out, err := h.Compiler.Raster(r.Context(), clean.Data, clean.MediaType == "image/svg+xml")
	switch {
	case errors.Is(r.Context().Err(), context.Canceled):
		return
	case err != nil:
		reply(w, http.StatusUnprocessableEntity, render.Problem{Code: render.CodeLogo, Message: "the logo could not be drawn: " + err.Error()})
		return
	case len(out) > MaxLogo:
		reply(w, http.StatusUnprocessableEntity, render.Problem{Code: render.CodeLogo,
			Message: fmt.Sprintf("drawn for documents, the logo is larger than %d KB; use a simpler logo, with fewer colours and details", MaxLogo>>10)})
		return
	}
	sum := sha256.Sum256(out)
	reply(w, http.StatusOK, render.Response{PNG: out, SHA256: hex.EncodeToString(sum[:])})
}

func (h *Handler) render(ctx context.Context, x typst.Input, in render.Request) (render.Response, error) {
	pdf, err := h.Compiler.PDF(ctx, x)
	if err != nil {
		return render.Response{}, err
	}
	sum := sha256.Sum256(pdf)
	out := render.Response{PDF: pdf, SHA256: hex.EncodeToString(sum[:])}
	if in.Check {
		if out.Particulars, err = h.Compiler.Particulars(ctx, x); err != nil {
			return render.Response{}, err
		}
		out.Missing = typst.Missing(render.Required(in.DocumentType, in.Data), out.Particulars)
	}
	if in.Pages {
		if out.Pages, err = h.Compiler.Pages(ctx, x); err != nil {
			return render.Response{}, err
		}
	}
	return out, nil
}
