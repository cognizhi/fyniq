package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	nemuraweb "github.com/cognizhi/fyniq/apps/nemura-web"
)

var built = fstest.MapFS{
	"index.html":      {Data: []byte("<html>app</html>")},
	"assets/app-1.js": {Data: []byte("console.log(1)")},
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// The binary serves the built web app: a file by its path, the page for the app's own routes so a refresh loads, never the page for a call that
// is not the app's, and always with the strict same-origin headers (NM-ARC-02, NM-UX-02, SEC-14).
// req: NM-ARC-02
// asvs: V3.4.3, V3.4.4, V3.4.5
func TestTheBinaryServesTheWebAppWithTheStrictHeaders(t *testing.T) {
	h := webApp(built)
	for _, tc := range []struct {
		method, path string
		code         int
		body, cache  string
	}{
		{"GET", "/", 200, "app", "no-cache"},
		{"GET", "/sign-in", 200, "app", "no-cache"},
		{"GET", "/me/leave", 200, "app", "no-cache"},
		{"GET", "/assets/app-1.js", 200, "console", "immutable"},
		{"GET", "/assets/gone.js", 404, "", ""},
		{"GET", "/v1/nothing", 404, "", ""},
		{"GET", "/auth/other", 404, "", ""},
		{"GET", "/favicon.ico", 404, "", ""},
		{"HEAD", "/sign-in", 200, "", "no-cache"},
		{"POST", "/sign-in", 405, "", ""},
	} {
		rec := get(h, tc.method, tc.path)
		if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.body) || !strings.Contains(rec.Header().Get("Cache-Control"), tc.cache) {
			t.Errorf("%s %s: %d %q cache %q", tc.method, tc.path, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
		if tc.code == 200 {
			for k := range webHeaders {
				if rec.Header().Get(k) == "" {
					t.Errorf("%s %s: no %s", tc.method, tc.path, k)
				}
			}
		}
	}
	if csp := webHeaders["Content-Security-Policy"]; strings.Contains(csp, "unsafe") || strings.Contains(csp, "http") || !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("the policy allows more than the same origin: %s", csp)
	}
}

// The committed placeholder is the only thing a build with no web stage embeds, it says so, and a release image is never built with it: the
// Dockerfile refuses the combination.
// req: NM-ARC-02
func TestAReleaseImageIsNeverBuiltWithThePlaceholder(t *testing.T) {
	if nemuraweb.IsPlaceholder(built) {
		t.Error("a built app is taken for the placeholder")
	}
	docker, err := os.ReadFile("../../deploy/docker/Dockerfile.go-service")
	if err != nil {
		t.Fatal(err)
	}
	d := string(docker)
	for _, want := range []string{"nemura-web-placeholder", "ARG NEMURA_WEB", "refuses a release build"} {
		if !strings.Contains(d, want) {
			t.Errorf("the Dockerfile does not say %q", want)
		}
	}
}
