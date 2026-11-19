package main

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// The headers of deploy/docker/security-headers.inc, the same strict policy the web image sends (design D11): same origin only, no
// third-party script, style or font.
var webHeaders = map[string]string{
	"Content-Security-Policy":      "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'",
	"X-Content-Type-Options":       "nosniff",
	"Referrer-Policy":              "strict-origin-when-cross-origin",
	"Permissions-Policy":           "camera=(self), microphone=(), geolocation=()",
	"Cross-Origin-Resource-Policy": "same-origin",
}

// webApp serves the embedded web app: a file when the path names one, else index.html, so the app's own routes load on a refresh. A path
// that looks like an API or sign-in call is never answered with the page: it is a 404, so a missing operation is not hidden behind HTML.
func webApp(files fs.FS) http.Handler {
	index, _ := fs.ReadFile(files, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		for k, v := range webHeaders {
			h.Set(k, v)
		}
		// A name inside the embedded files: fs.FS refuses "..", and the cleaned path only picks the file (it is not a defence on its own).
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/") // #nosec -- an io/fs name, not an OS path
		if name != "" {
			if body, err := fs.ReadFile(files, name); err == nil {
				if strings.HasPrefix(name, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
					h.Set("Content-Type", ct)
				}
				if r.Method == http.MethodGet {
					_, _ = w.Write(body)
				}
				return
			}
			if strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "v1/") || strings.HasPrefix(name, "auth/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
