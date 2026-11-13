// Package httpserver is the HTTP shell every Fyniq service runs in: /healthz (liveness),
// /readyz (the database and other dependencies) and /version (NFR-IMG-06), plus the service's
// own routes. Cloud Run's start-up and liveness probes call the first two.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/telemetry"
	"github.com/cognizhi/fyniq/internal/version"
)

// Check reports whether one dependency is ready.
type Check func(ctx context.Context) error

// Server is a service's HTTP server. Routes are added through Handle and HandleFunc, which remember
// their patterns, so a test can check every route a service serves (QA-API-03).
type Server struct {
	Mux    *http.ServeMux
	info   version.Info
	mu     sync.Mutex
	checks map[string]Check
	routes []string
	wrap   []func(http.Handler) http.Handler
}

// New returns a server that already answers /healthz, /readyz and /version.
func New(info version.Info) *Server {
	s := &Server{Mux: http.NewServeMux(), info: info, checks: map[string]Check{}}
	s.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.HandleFunc("GET /readyz", s.readyz)
	s.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.info)
	})
	return s
}

// Handle registers a route.
func (s *Server) Handle(pattern string, h http.Handler) {
	s.mu.Lock()
	s.routes = append(s.routes, pattern)
	s.mu.Unlock()
	s.Mux.Handle(pattern, h)
}

// HandleFunc registers a route.
func (s *Server) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	s.Handle(pattern, http.HandlerFunc(h))
}

// Routes returns the patterns of every route, in the order they were added.
func (s *Server) Routes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.routes...)
}

// HSTS is the Strict-Transport-Security every answer carries (SEC-10, rev 0.244): a browser reaches Fyniq over HTTPS
// only, for two years, subdomains included. TLS ends at Cloud Run and at Caddy locally; a browser ignores the header
// over plain HTTP.
const HSTS = "max-age=63072000; includeSubDomains"

// ServeHTTP serves the routes through what Use added, each answer with HSTS and nosniff: a browser never guesses an
// answer's type, so a JSON body is never run as a script or a page (SEC-23).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Strict-Transport-Security", HSTS)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	s.mu.Lock()
	h := telemetry.Routed(s.Mux) // each request's span named by its route (NFR-OBS-01)
	for i := len(s.wrap) - 1; i >= 0; i-- {
		h = s.wrap[i](h)
	}
	s.mu.Unlock()
	h.ServeHTTP(w, r)
}

// Use puts mw in front of every route, after those Use added before: a check that may answer a request itself, such as
// the per-IP limits.
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrap = append(s.wrap, mw)
}

// AddReadiness registers a dependency that /readyz checks.
func (s *Server) AddReadiness(name string, c Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks[name] = c
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	names := make([]string, 0, len(s.checks))
	for n := range s.checks {
		names = append(names, n)
	}
	s.mu.Unlock()
	sort.Strings(names)

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	failing := map[string]string{}
	for _, n := range names {
		s.mu.Lock()
		c := s.checks[n]
		s.mu.Unlock()
		if err := c(ctx); err != nil {
			failing[n] = err.Error()
		}
	}
	if len(failing) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "failing": failing})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "checks": names})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Serve runs the server on l until ctx is done, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return err
		}
		if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
