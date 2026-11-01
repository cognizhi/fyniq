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
	if token != "ok" || class != render.Class {
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
	req := render.Request{DocumentType: doctemplate.CreditNote, Content: doctemplate.Default(doctemplate.KindDocument, doctemplate.CreditNote),
		Data: render.Sample(doctemplate.CreditNote), Created: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC), Check: true}

	if w, out := call(t, h, "", req); w.Code != http.StatusUnauthorized || out["code"] != render.CodeUnauthorised {
		t.Errorf("without a token: %d %v", w.Code, out)
	}
	w, out := call(t, h, "ok", req)
	if w.Code != http.StatusOK || out["missing"] != nil || len(out["sha256"].(string)) != 64 {
		t.Fatalf("a credit note: %d %s", w.Code, w.Body.String()[:200])
	}
	var res render.Response
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
	if w, out := call(t, h, "ok", bad); w.Code != http.StatusUnprocessableEntity || out["code"] != render.CodeTemplate || !strings.Contains(w.Body.String(), doctemplate.CodeLocked) {
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
	if w, out := call(t, h, "ok", logo); w.Code != http.StatusBadRequest || out["code"] != render.CodeRequest {
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

// A report its caller laid out renders without a template, within its bounds, and names nothing of a document.
// req: FR-RPT-05
func TestTheRenderServiceRendersAReport(t *testing.T) {
	c, err := typst.New(os.Getenv("FYNIQ_TYPST"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := New(c, tokens{})
	report := render.Report{Title: "Balance sheet", Author: "Cognizhi Pte Ltd", Lines: []string{"At 31 October 2026"}, Label: "SGD", Columns: []string{"Oct 2026"},
		Rows: []render.ReportRow{{Kind: render.RowSection, Label: "Current assets"}, {Kind: render.RowLine, Code: "1010", Label: "Bank", Cells: []string{"1,000.00"}},
			{Kind: render.RowTotal, Label: "Total current assets", Cells: []string{"1,000.00"}}}, Footer: "ledger revision 7"}
	req := render.Request{Report: &report, Created: time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)}
	w, _ := call(t, h, "ok", req)
	var res render.Response
	if err := json.Unmarshal(w.Body.Bytes(), &res); w.Code != http.StatusOK || err != nil || !bytes.HasPrefix(res.PDF, []byte("%PDF-")) || len(res.SHA256) != 64 {
		t.Fatalf("a report: %d %s", w.Code, w.Body.String())
	}
	both := req
	both.DocumentType = doctemplate.Invoice
	if w, out := call(t, h, "ok", both); w.Code != http.StatusBadRequest || out["code"] != render.CodeRequest {
		t.Errorf("a report that names a document type: %d %v", w.Code, out)
	}
	short := report
	short.Rows = append(slices.Clone(report.Rows), render.ReportRow{Kind: render.RowLine, Label: "Cash", Cells: []string{"1", "2"}})
	if w, out := call(t, h, "ok", render.Request{Report: &short, Created: req.Created}); w.Code != http.StatusUnprocessableEntity || out["code"] != render.CodeRequest ||
		!strings.Contains(out["message"].(string), "row 4 has 2 figures for 1 columns") {
		t.Errorf("a row with a figure too many: %d %v", w.Code, out)
	}
	if w, _ := call(t, h, "", req); w.Code != http.StatusUnauthorized {
		t.Errorf("a report without a token: %d", w.Code)
	}
}
