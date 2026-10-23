package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/render/typst"
)

// tokens accepts "ok" as the render class's token only.
type tokens struct{}

func (tokens) Token(string) (string, error) { return "ok", nil }
func (tokens) Verify(token, class string) error {
	if token != "ok" || class != Class {
		return ports.ErrUnauthorised
	}
	return nil
}

func call(t *testing.T, h http.Handler, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(b))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// The render service renders for its callers only, checks the template again, and finds the IRAS particulars when
// asked (§12.3 rev 0.96; FR-TPL-02, FR-TPL-03).
// req: FR-TPL-02
// req: FR-TPL-03
func TestTheRenderServiceRendersForItsCallers(t *testing.T) {
	c, err := typst.New(os.Getenv("FYNIQ_TYPST"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := New(c, tokens{})
	req := Request{DocumentType: doctemplate.CreditNote, Content: doctemplate.Default(doctemplate.KindDocument, doctemplate.CreditNote),
		Data: render.Sample(doctemplate.CreditNote), Created: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC), Check: true}

	if w, out := call(t, h, "", req); w.Code != http.StatusUnauthorized || out["code"] != CodeUnauthorised {
		t.Errorf("without a token: %d %v", w.Code, out)
	}
	w, out := call(t, h, "ok", req)
	if w.Code != http.StatusOK || out["missing"] != nil || len(out["sha256"].(string)) != 64 {
		t.Fatalf("a credit note: %d %s", w.Code, w.Body.String()[:200])
	}
	var res Response
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if !bytes.HasPrefix(res.PDF, []byte("%PDF-")) || !slices.Contains(res.Particulars, render.PReason) {
		t.Errorf("the credit note's particulars: %v", res.Particulars)
	}

	// The template is checked again: a hidden particular is refused before anything is laid out.
	bad := req
	bad.Content = doctemplate.Default(doctemplate.KindDocument, doctemplate.CreditNote)
	for i := range bad.Content.Blocks {
		if bad.Content.Blocks[i].Kind == doctemplate.BlockTotals {
			bad.Content.Blocks[i].Shown = false
		}
	}
	if w, out := call(t, h, "ok", bad); w.Code != http.StatusUnprocessableEntity || out["code"] != CodeTemplate || !strings.Contains(w.Body.String(), doctemplate.CodeLocked) {
		t.Errorf("a hidden particular: %d %v", w.Code, out)
	}
	// Data that lacks a particular renders, and says what is missing.
	gap := req
	gap.Data = render.Sample(doctemplate.CreditNote)
	delete(gap.Data.Fields, "document.reason")
	if w, out := call(t, h, "ok", gap); w.Code != http.StatusOK || !slices.Equal(out["missing"].([]any), []any{render.PReason}) {
		t.Errorf("a credit note without its reason: %d %v", w.Code, out["missing"])
	}
	logo := req
	logo.Logo = []byte("<svg/>")
	if w, out := call(t, h, "ok", logo); w.Code != http.StatusBadRequest || out["code"] != CodeRequest {
		t.Errorf("a logo that is not a PNG: %d %v", w.Code, out)
	}
	r := httptest.NewRequest(http.MethodPost, "/render", strings.NewReader(`{"document_type":"invoice","script":"x"}`))
	r.Header.Set("Authorization", "Bearer ok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown member: %d", rec.Code)
	}
	undated := req
	undated.Created = time.Time{}
	if w, _ := call(t, h, "ok", undated); w.Code != http.StatusBadRequest {
		t.Errorf("no creation time: %d", w.Code)
	}
}
