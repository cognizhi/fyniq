//go:build local

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/brand"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

const hostileLogo = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 132 28" onload="steal()"><script>alert(1)</script>` +
	`<rect width="28" height="28" rx="7" fill="#0F766E"/><image href="https://evil.test/t.png"/><text x="34" y="19">Cognizhi</text></svg>`

// The logo and brand: an SVG logo is cleaned and the answer says what went; each change is the next version, at the
// version read; every member reads it, and an earlier version's file stays readable; the accent is one of the options;
// only the profile's editors change it, and each change is audited.
// req: FR-ORG-09
// req: G4-099
// asvs: V1.3.4, V5.2.2
func TestTheLogoAndBrand(t *testing.T) {
	e := newEnv(t)
	admin, employee := steppedUp(e, e.as(authz.Administrator)), e.as(authz.Employee)

	if r := e.call(http.MethodGet, "/v1/organisation/brand", employee, nil); r.Status != 200 || r.Body["version"] != float64(0) || r.Header.Get("ETag") != `"0"` {
		t.Fatalf("before any brand: %d %s", r.Status, r.Raw)
	}
	up := e.put("/v1/organisation/brand/files/light", admin, nil, "logo.svg", []byte(hostileLogo), "If-Match", `"0"`)
	if up.Status != 200 || up.Body["version"] != float64(1) || up.str("light", "media_type") != "image/svg+xml" ||
		!strings.Contains(up.Raw, `"elements":["image","script"]`) {
		t.Fatalf("uploading the logo: %d %s", up.Status, up.Raw)
	}
	file := e.call(http.MethodGet, "/v1/organisation/brand/files/light", employee, nil)
	svg, _ := base64.StdEncoding.DecodeString(file.str("content_base64"))
	if file.Status != 200 || strings.Contains(string(svg), "script") || strings.Contains(string(svg), "onload") || !strings.Contains(string(svg), "<rect") {
		t.Fatalf("reading the logo: %d %s", file.Status, svg)
	}

	// Stale versions, bad files and colours are refused; the accent is an option.
	e.problemIs(e.put("/v1/organisation/brand/files/light", admin, nil, "logo.svg", []byte(hostileLogo), "If-Match", `"0"`), "uploadBrandFile", 412, CodeStale)
	e.problemIs(e.put("/v1/organisation/brand/files/icon", admin, nil, "icon.gif", []byte("GIF89a"), "If-Match", `"1"`), "uploadBrandFile", 422, CodeInvalid)
	e.problemIs(e.put("/v1/organisation/brand/files/light", admin, nil, "bomb.svg",
		[]byte(`<!DOCTYPE svg [<!ENTITY a "aa">]><svg xmlns="http://www.w3.org/2000/svg">&a;</svg>`), "If-Match", `"1"`), "uploadBrandFile", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPut, "/v1/organisation/brand/accent", admin, map[string]any{"accent": "#7C3AED"}, "If-Match", `"1"`), "setBrandAccent", 422, CodeInvalid)
	if r := e.call(http.MethodPut, "/v1/organisation/brand/accent", admin, map[string]any{"accent": brand.Accents[1]}, "If-Match", `"1"`); r.Status != 200 ||
		r.str("accent") != brand.Accents[1] || r.str("light", "sha256") != up.str("light", "sha256") {
		t.Fatalf("setting the accent: %d %s", r.Status, r.Raw)
	}

	// Removing the logo is the next version; the earlier one's file stays readable.
	if r := e.call(http.MethodDelete, "/v1/organisation/brand/files/light", admin, nil, "If-Match", `"2"`); r.Status != 200 || r.Body["light"] != nil ||
		r.Body["version"] != float64(3) {
		t.Fatalf("removing the logo: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/organisation/brand/files/light", employee, nil), "getBrandFile", 404, CodeNotFound)
	if r := e.call(http.MethodGet, "/v1/organisation/brand/files/light?version=1", employee, nil); r.Status != 200 || r.Body["version"] != float64(1) {
		t.Fatalf("an earlier version's logo: %d %s", r.Status, r.Raw)
	}

	// Only the profile's editors change it.
	e.problemIs(e.call(http.MethodPut, "/v1/organisation/brand/accent", employee, map[string]any{"accent": ""}, "If-Match", `"3"`), "setBrandAccent", 403, CodeForbidden)
	saved := 0
	reader, err := postgres.Open(context.Background(), db.URL("fyniq_readonly"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := reader.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range events {
			saved += strings.Count(ev.Body, `"action":"brand_version.saved"`)
		}
		return err
	}); err != nil || saved != 3 {
		t.Errorf("brand versions in the trail: %d %v", saved, err)
	}
}

// put sends a file to path as multipart/form-data with PUT.
func (e *env) put(path, token string, fields map[string]string, name string, file []byte, headers ...string) response {
	e.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		e.t.Fatal(err)
	}
	_, _ = part.Write(file)
	_ = w.Close()
	return e.call(http.MethodPut, path, token, rawBody{w.FormDataContentType(), body.Bytes()}, headers...)
}

// matrixBrandLogo puts a logo in e's organisation's brand, as its Administrator.
func matrixBrandLogo(e *env) {
	e.put("/v1/organisation/brand/files/light", steppedUp(e, e.as(authz.Administrator)), nil, "logo.svg",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`), "If-Match", "*")
}

// matrixBrand are the authorisation matrix's rows for the logo and brand (FR-ORG-09).
var matrixBrand = map[string]func(e *env, tok string) (response, func() bool){
	"getBrand": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/brand", tok, nil), nil
	},
	"getBrandFile": func(e *env, tok string) (response, func() bool) {
		matrixBrandLogo(e)
		return e.call(http.MethodGet, "/v1/organisation/brand/files/light", tok, nil), nil
	},
	"uploadBrandFile": func(e *env, tok string) (response, func() bool) {
		return e.put("/v1/organisation/brand/files/light", steppedUp(e, tok), nil, "logo.svg",
			[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle r="4"/></svg>`), "If-Match", "*", "Prefer", "return=preview"), nil
	},
	"removeBrandFile": func(e *env, tok string) (response, func() bool) {
		matrixBrandLogo(e)
		return e.call(http.MethodDelete, "/v1/organisation/brand/files/light", steppedUp(e, tok), nil, "If-Match", "*", "Prefer", "return=preview"), nil
	},
	"setBrandAccent": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/organisation/brand/accent", steppedUp(e, tok), map[string]any{"accent": brand.Accents[0]}, "If-Match", "*",
			"Prefer", "return=preview"), nil
	},
}

// recordingRenderer renders through the render service and keeps the last document it was asked for.
type recordingRenderer struct {
	render.Renderer
	mu   sync.Mutex
	last render.Request
}

func (r *recordingRenderer) Render(ctx context.Context, q render.Request) (render.Response, error) {
	if len(q.Raster) == 0 {
		r.mu.Lock()
		r.last = q
		r.mu.Unlock()
	}
	return r.Renderer.Render(ctx, q)
}

// The logo on documents (FR-ORG-09, rev 0.201): uploading the logo for light backgrounds has the render service draw it
// as documents show it, a PNG 192 pixels high kept with the version; an invoice rendered later shows the logo and accent
// of the brand in force when it was issued, not a later one's (FR-DOC-04), the platform's starter taking the brand's
// accent; removing the logo removes its drawing from the next version.
// req: FR-ORG-09
// req: FR-DOC-04
// req: G4-099
func TestAnIssuedDocumentKeepsItsLogo(t *testing.T) {
	e := newEnv(t)
	rec := &recordingRenderer{Renderer: testRenderer(t)}
	e.api.deps.Renderer = rec
	admin := steppedUp(e, e.as(authz.Administrator))

	up := e.put("/v1/organisation/brand/files/light", admin, nil, "logo.svg", []byte(hostileLogo), "If-Match", `"0"`)
	if up.Status != 200 || up.str("print", "media_type") != "image/png" || len(up.str("print", "sha256")) != 64 {
		t.Fatalf("uploading the logo: %d %s", up.Status, up.Raw)
	}
	issuedPrint := up.str("print", "sha256")
	if r := e.call(http.MethodPut, "/v1/organisation/brand/accent", admin, map[string]any{"accent": brand.Accents[1]}, "If-Match", `"1"`); r.Status != 200 ||
		r.str("print", "sha256") != issuedPrint {
		t.Fatalf("the accent keeps the drawing: %d %s", r.Status, r.Raw)
	}
	file := e.call(http.MethodGet, "/v1/organisation/brand/files/print", e.as(authz.Employee), nil)
	drawn, _ := base64.StdEncoding.DecodeString(file.str("content_base64"))
	if p, err := brand.Size(drawn); file.Status != 200 || err != nil || p.Y != 163 || p.X != 768 {
		t.Fatalf("reading the drawing: %d %v %v", file.Status, p, err)
	}

	id, _ := e.issuedInvoice("1", "100.00")
	// The invoice is posted at the store's time; the later versions come after it.
	e.clock.Set(time.Date(2099, 1, 2, 9, 0, 0, 0, time.UTC))
	later := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="5" fill="#9F1239"/></svg>`
	if r := e.put("/v1/organisation/brand/files/light", admin, nil, "later.svg", []byte(later), "If-Match", `"2"`); r.Status != 200 || r.str("print", "sha256") == issuedPrint {
		t.Fatalf("a later logo: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPut, "/v1/organisation/brand/accent", admin, map[string]any{"accent": brand.Accents[5]}, "If-Match", `"3"`); r.Status != 200 {
		t.Fatalf("a later accent: %d %s", r.Status, r.Raw)
	}

	if r := e.call(http.MethodGet, "/v1/invoices/"+id+"/document", e.as(authz.Accountant), nil); r.Status != 200 || r.str("template", "source") != "platform" {
		t.Fatalf("rendering the invoice: %d %.300s", r.Status, r.Raw)
	}
	rec.mu.Lock()
	last := rec.last
	rec.mu.Unlock()
	if sum := sha256.Sum256(last.Logo); hex.EncodeToString(sum[:]) != issuedPrint {
		t.Errorf("the invoice was rendered with another logo than the one in force when it was issued (%d bytes)", len(last.Logo))
	}
	if last.Content.Theme.Accent != brand.Accents[1] {
		t.Errorf("the invoice was rendered in %s, not in the accent in force when it was issued", last.Content.Theme.Accent)
	}

	if r := e.call(http.MethodDelete, "/v1/organisation/brand/files/light", admin, nil, "If-Match", `"4"`); r.Status != 200 || r.Body["print"] != nil {
		t.Fatalf("removing the logo: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/organisation/brand/files/print", admin, nil), "getBrandFile", 404, CodeNotFound)
}
